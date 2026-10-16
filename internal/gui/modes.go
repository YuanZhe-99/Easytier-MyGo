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
	// The helper starts on demand; set its background mode first, so a
	// helper in background mode stops running networks the user daemon is
	// about to take over.
	if to != choiceUser || from != choiceUser {
		hc := localapi.NewClient(helperSock)
		if cur, err := hc.Do(ctx, localapi.Request{Type: localapi.RequestSettingsGet}); err == nil {
			s := *cur.Settings
			s.Background = to == choiceBackground
			if _, err := hc.Do(ctx, localapi.Request{Type: localapi.RequestSettingsSet, Settings: &s}); err != nil {
				return err
			}
			a.update(func() { a.settings = s })
		} else if to != choiceUser {
			return err
		}
	}
	// Bring the networks along when the target has none.
	if !unset && (from == choiceUser) != (to == choiceUser) {
		src, dst := userSock, helperSock
		if to == choiceUser {
			src, dst = helperSock, userSock
			if err := a.user.ensure(ctx, userSock); err != nil {
				return err
			}
		}
		if err := copyNetworks(ctx, src, dst); err != nil {
			return fmt.Errorf("%s: %w", L.ModeCopyFailed(), err)
		}
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
		a.user.stop() // its networks stop; the helper runs them now
	}
	a.reconnect()
	return nil
}

// copyNetworks copies every network (document, name, intent) from one
// daemon to another that has none yet.
func copyNetworks(ctx context.Context, srcSock, dstSock string) error {
	src, dst := localapi.NewClient(srcSock), localapi.NewClient(dstSock)
	have, err := dst.Do(ctx, localapi.Request{Type: localapi.RequestNetworks})
	if err != nil {
		return err
	}
	if len(have.Networks) > 0 {
		return nil
	}
	list, err := src.Do(ctx, localapi.Request{Type: localapi.RequestNetworks})
	if err != nil {
		if errors.Is(err, localapi.ErrNotRunning) {
			return nil // nothing to bring along
		}
		return err
	}
	for _, n := range list.Networks {
		doc, err := src.Do(ctx, localapi.Request{Type: localapi.RequestConfigGet, Network: n.ID})
		if err != nil {
			return err
		}
		created, err := dst.Do(ctx, localapi.Request{Type: localapi.RequestNetworkCreate, Name: n.Name, Text: doc.Config.Text})
		if err != nil {
			return err
		}
		if n.Run && len(created.Networks) > 0 {
			id := created.Networks[len(created.Networks)-1].ID
			_, _ = dst.Do(ctx, localapi.Request{Type: localapi.RequestConnect, Network: id})
		}
	}
	return nil
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
