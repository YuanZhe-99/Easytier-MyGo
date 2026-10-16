// easytier-mygo is the desktop GUI. It renders the resident management
// daemon's health snapshots and sends bounded commands; the network keeps
// running without it.
//
// Closing the window hides the GUI into the tray when the desktop has
// one; "退出 EasyTier MyGo" in the tray (or the app menu, or closing the
// window on a desktop without a tray) ends it. The management service and
// the core are independent of all of this.
package main

import (
	"log"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/app"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

func main() {
	mygo.App.SetName("EasyTier MyGo")
	mygo.App.SetVersion(version.Version)
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}

	a := app.New()
	mygo.App.OnSecondInstance(func([]string, string) { a.Focus() })
	// Clicking the Dock icon (macOS) with the window hidden shows it again.
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.Focus()
		}
	})
	// Any quit path (tray, app menu, Ctrl+Q, last window without a tray)
	// goes through OnBeforeQuit: mark it so the close handler does not
	// hide the window into the tray.
	mygo.App.OnBeforeQuit(a.BeforeQuit)

	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleViewMenu},
			{Role: mygo.RoleWindowMenu},
		}))

		w := mygo.NewWindow(mygo.WindowOptions{
			Title:     "EasyTier MyGo",
			Width:     1060,
			Height:    720,
			MinWidth:  860,
			MinHeight: 560,
			StateKey:  "main",
			Content:   ui.View(a.View),
		})
		a.SetWindow(w)

		// The tray is best-effort: without one the close button quits the
		// GUI, exactly as before trays existed.
		a.TraySetup()

		// Closing the window hides it into the tray; without a tray it
		// quits (the window manager closes the window, App quits when the
		// last one is gone). App.Quit must always pass through.
		w.OnClose(func(e *mygo.CloseEvent) {
			if a.CloseWindow(e) {
				w.Hide()
				a.WindowHidden()
			}
		})
		// A hidden or background window is not watching the curve: lower
		// the daemon's traffic sampling tier with it.
		w.OnHide(func() { a.SyncWatching() })
		w.OnShow(func() { a.SyncWatching() })

		a.Start()
	})

	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	a.Stop()
	a.TrayDestroy()
}
