package gui

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/helper"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// runChoice is what the run-mode cards offer.
type runChoice int

const (
	choiceUser runChoice = iota
	choiceFollow
	choiceBackground
)

// currentChoice derives the active card (caller holds a.mu).
func (a *App) currentChoice() runChoice {
	switch {
	case a.prefs.Mode != ModeHelper:
		return choiceUser
	case a.settings.Background:
		return choiceBackground
	}
	return choiceFollow
}

// chooseMode switches the run mode: installs the helper when needed
// (one administrator prompt), moves the networks along, and reconnects.
func (a *App) chooseMode(to runChoice) {
	a.mu.Lock()
	from := a.currentChoice()
	unset := a.prefs.Mode == ModeUnset
	a.mu.Unlock()
	if to == from && !unset {
		return
	}
	a.update(func() { a.busy["mode"] = true })
	go func() {
		err := a.switchMode(from, to, unset)
		a.update(func() {
			delete(a.busy, "mode")
			if err != nil {
				a.setToast(err.Error(), true)
				return
			}
			if a.page == pageWelcome {
				a.page = pageNetwork
			}
		})
	}()
}

func (a *App) switchMode(from, to runChoice, unset bool) error {
	ctx, cancel := context.WithTimeout(a.ctx, 5*time.Minute)
	defer cancel()
	userSock, helperSock := localapi.UserSocketPath(), localapi.SystemSocketPath
	if to != choiceUser && !helper.Inspect().Installed {
		if err := runPrivileged("install", "--from", bundleDir()); err != nil {
			return err
		}
	}
	moving := !unset && (from == choiceUser) != (to == choiceUser)
	src, dst := userSock, helperSock
	if to == choiceUser {
		src, dst = helperSock, userSock
		if err := a.user.ensure(ctx, userSock); err != nil {
			return err
		}
	}
	// 1. Copy the networks (not connected yet) when the target has none.
	var toConnect []string
	if moving {
		ids, err := copyNetworks(ctx, src, dst)
		if err != nil {
			return fmt.Errorf("%s: %w", L.ModeCopyFailed(), err)
		}
		toConnect = ids
	}
	// 2. Stop the source's networks before the target starts its copies:
	// both would claim the same ports and devices.
	if moving {
		if from == choiceUser {
			a.user.stop()
			waitGone(ctx, userSock)
		} else {
			disconnectAll(ctx, helperSock)
		}
	}
	// 3. The helper's mode: background or Follow GUI.
	if to != choiceUser || from != choiceUser {
		hc := localapi.NewClient(helperSock)
		if cur, err := hc.Do(ctx, localapi.Request{Type: localapi.RequestSettingsGet}); err == nil {
			st := *cur.Settings
			st.Background = to == choiceBackground
			if _, err := hc.Do(ctx, localapi.Request{Type: localapi.RequestSettingsSet, Settings: &st}); err != nil {
				return err
			}
			a.update(func() { a.settings = st })
		} else if to != choiceUser {
			return err
		}
	}
	// 4. Connect the copies the user had connected.
	dc := localapi.NewClient(dst)
	for _, id := range toConnect {
		_, _ = dc.Do(ctx, localapi.Request{Type: localapi.RequestConnect, Network: id})
	}
	a.mu.Lock()
	if to == choiceUser {
		a.prefs.Mode = ModeUser
	} else {
		a.prefs.Mode = ModeHelper
	}
	p := a.prefs
	a.mu.Unlock()
	if err := savePrefs(p); err != nil {
		return err
	}
	if to != choiceUser {
		a.user.stop()
	}
	a.reconnect()
	return nil
}

// waitGone waits until nothing answers on a socket (the user daemon
// stopped its networks and exited).
func waitGone(ctx context.Context, sock string) {
	c := localapi.NewClient(sock)
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := c.Do(ctx, localapi.Request{Type: localapi.RequestInfo}); err != nil {
			return
		}
		time.Sleep(200 * time.Millisecond)
	}
}

// disconnectAll stops every network of a daemon (the user's intent moves
// with the networks to the other daemon).
func disconnectAll(ctx context.Context, sock string) {
	c := localapi.NewClient(sock)
	list, err := c.Do(ctx, localapi.Request{Type: localapi.RequestNetworks})
	if err != nil {
		return
	}
	for _, n := range list.Networks {
		if n.Run {
			_, _ = c.Do(ctx, localapi.Request{Type: localapi.RequestDisconnect, Network: n.ID})
		}
	}
}

// copyNetworks copies every network (document and name) from one daemon
// to another that has none yet, without connecting them.
// Returns: the target IDs of the networks that were connected at the source.
func copyNetworks(ctx context.Context, srcSock, dstSock string) ([]string, error) {
	src, dst := localapi.NewClient(srcSock), localapi.NewClient(dstSock)
	have, err := dst.Do(ctx, localapi.Request{Type: localapi.RequestNetworks})
	if err != nil {
		return nil, err
	}
	list, err := src.Do(ctx, localapi.Request{Type: localapi.RequestNetworks})
	if err != nil {
		if errors.Is(err, localapi.ErrNotRunning) {
			return nil, nil // nothing to bring along
		}
		return nil, err
	}
	if len(have.Networks) > 0 {
		// The target keeps its own networks; connect those whose names the
		// user had connected at the source.
		var ids []string
		for _, n := range list.Networks {
			for _, h := range have.Networks {
				if n.Run && h.Name == n.Name {
					ids = append(ids, h.ID)
				}
			}
		}
		return ids, nil
	}
	var ids []string
	for _, n := range list.Networks {
		doc, err := src.Do(ctx, localapi.Request{Type: localapi.RequestConfigGet, Network: n.ID})
		if err != nil {
			return nil, err
		}
		created, err := dst.Do(ctx, localapi.Request{Type: localapi.RequestNetworkCreate, Name: n.Name, Text: doc.Config.Text})
		if err != nil {
			return nil, err
		}
		if n.Run && len(created.Networks) > 0 {
			ids = append(ids, created.Networks[len(created.Networks)-1].ID)
		}
	}
	return ids, nil
}

// bundleDir is where the daemon binary and the bundled core live.
func bundleDir() string {
	if p, err := svcBinary(); err == nil {
		return filepath.Dir(p)
	}
	return ""
}

// runPrivileged runs `easytier-mygo-svc helper <args>` through pkexec:
// the one administrator prompt of a run-mode change.
func runPrivileged(args ...string) error {
	svc, err := svcBinary()
	if err != nil {
		return err
	}
	pkexec, err := exec.LookPath("pkexec")
	if err != nil {
		return errors.New(L.ErrorNoPkexec())
	}
	out, err := exec.Command(pkexec, append([]string{svc, "helper"}, args...)...).CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if ee, ok := err.(*exec.ExitError); ok && (ee.ExitCode() == 126 || ee.ExitCode() == 127) {
			return errors.New(L.ErrorAuthCancelled())
		}
		if msg == "" {
			msg = err.Error()
		}
		return errors.New(msg)
	}
	return nil
}

// uninstallHelper removes the helper after moving the networks to the
// user daemon.
func (a *App) uninstallHelper(purge bool) {
	a.update(func() { a.busy["mode"] = true })
	go func() {
		err := func() error {
			a.mu.Lock()
			choice := a.currentChoice()
			a.mu.Unlock()
			if choice != choiceUser {
				if err := a.switchMode(choice, choiceUser, false); err != nil {
					return err
				}
			}
			args := []string{"uninstall"}
			if purge {
				args = append(args, "--purge")
			}
			return runPrivileged(args...)
		}()
		st := helper.Inspect()
		a.update(func() {
			delete(a.busy, "mode")
			a.helperSt = st
			if err != nil {
				a.setToast(err.Error(), true)
			} else {
				a.setToast(L.ToastHelperRemoved(), false)
			}
		})
	}()
}

// updateHelper brings the helper to this app's version: silently from a
// signed bundle, else it asks the user (banner) to approve a reinstall.
func (a *App) updateHelper(c *localapi.Client) {
	_, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestHelperUpdate, Text: bundleDir()})
	a.update(func() {
		a.helperOutdated = err != nil
		if err == nil {
			a.setToast(L.ToastHelperUpdated(), false)
		}
	})
}

// reinstallHelper updates the helper with an administrator prompt.
func (a *App) reinstallHelper() {
	a.update(func() { a.busy["mode"] = true })
	go func() {
		err := runPrivileged("install", "--from", bundleDir())
		a.update(func() {
			delete(a.busy, "mode")
			if err != nil {
				a.setToast(err.Error(), true)
				return
			}
			a.helperOutdated = false
			a.setToast(L.ToastHelperUpdated(), false)
		})
		a.reconnect()
	}()
}
