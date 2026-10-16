package gui

import (
	"context"
	"runtime"
	"time"

	"github.com/egoist/mygo"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// SetTray hands the GUI its status icon (nil when the desktop has none).
func (a *App) SetTray(t *Tray) {
	a.mu.Lock()
	a.tray = t
	a.mu.Unlock()
}

// Focus shows and raises the window, bringing the Dock icon back on macOS.
func (a *App) Focus() {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	if w == nil {
		return
	}
	if runtime.GOOS == "darwin" {
		mygo.App.Dock.Show()
	}
	w.Show()
	w.Focus()
	a.SetVisible(true)
}

// Quit ends the GUI. In Follow GUI and No privileges modes the networks
// stop with the session; disconnect also stops them in background mode.
func (a *App) Quit(disconnect bool) {
	if disconnect {
		if c := a.client(); c != nil {
			a.mu.Lock()
			nets := append([]localapi.NetworkStatus(nil), a.networks...)
			a.mu.Unlock()
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			for _, n := range nets {
				if n.Run {
					_, _ = c.Do(ctx, localapi.Request{Type: localapi.RequestDisconnect, Network: n.ID})
				}
			}
			cancel()
		}
	}
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
	mygo.App.Quit()
}

// HandleClose decides what the close button does: hide into the tray, or
// let the window close (and the app quit).
// Side effects: hiding also hides the macOS Dock icon when HideDock is set.
func (a *App) HandleClose(e *mygo.CloseEvent) {
	a.mu.Lock()
	quit := a.quitting || a.prefs.CloseQuits || a.tray == nil
	w := a.window
	first := !a.prefs.TrayHintShown
	a.prefs.TrayHintShown = true
	p := a.prefs
	a.mu.Unlock()
	if quit {
		return
	}
	e.PreventDefault()
	w.Hide()
	a.SetVisible(false)
	if a.HidesDock() {
		mygo.App.Dock.Hide()
	}
	if first {
		_ = savePrefs(p)
		a.notify("tray-hint", L.NotifyStillRunningTitle(), L.NotifyStillRunningBody())
	}
}

// notify shows a desktop notification when allowed.
func (a *App) notify(id, title, body string) {
	if !mygo.NotificationsSupported() {
		return
	}
	_ = mygo.NewNotification(mygo.NotificationOptions{ID: id, Title: title, Body: body}).Show()
}

// noticeChanges notifies about networks that connected or failed (caller
// holds a.mu; the notification is sent outside it).
func (a *App) noticeChanges(old, cur []localapi.NetworkStatus) []func() {
	if !a.prefs.Notify {
		return nil
	}
	prev := map[string]localapi.State{}
	for _, n := range old {
		prev[n.ID] = n.State
	}
	var out []func()
	for _, n := range cur {
		p, seen := prev[n.ID]
		if !seen || p == n.State {
			continue
		}
		name := n.Name
		switch {
		case n.State == localapi.StateConnected && p != localapi.StateConnected:
			out = append(out, func() { a.notify("net-"+n.ID, name, L.NotifyConnected()) })
		case n.State == localapi.StateFailed:
			out = append(out, func() { a.notify("net-"+n.ID, name, L.NotifyFailed()) })
		}
	}
	return out
}
