// easytier-mygo is the desktop GUI. It starts (or connects to) the daemon
// of the chosen run mode and keeps a session attached while it runs; see
// doc/en-us/gui.md and doc/en-us/run-modes.md.
//
// Closing the window hides it into the tray when the desktop has one;
// quitting from the tray ends the session, which stops Follow GUI networks.
package main

import (
	"log"
	"os"
	"os/signal"
	"syscall"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/gui"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

func main() {
	mygo.App.SetName("EasyTier MyGo")
	mygo.App.SetVersion(version.Version)
	if !mygo.App.RequestSingleInstanceLock() {
		return
	}
	a := gui.New()
	gui.ApplyTheme(a.ThemePref())
	mygo.App.OnSecondInstance(func([]string, string) { a.Focus() })
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			a.Focus()
		}
	})
	hidden := mygo.App.WasOpenedAtLogin() && a.StartHidden()

	mygo.App.WhenReady(func() {
		mygo.App.SetMenu(mygo.NewMenu([]*mygo.MenuItem{
			{Role: mygo.RoleAppMenu},
			{Role: mygo.RoleEditMenu},
			{Role: mygo.RoleWindowMenu},
		}))
		// The strip at the top of the view is the title bar (gui.TitleBarOptions).
		w := mygo.NewWindow(gui.TitleBarOptions(mygo.WindowOptions{
			Title:     "EasyTier MyGo",
			Width:     1100,
			Height:    720,
			MinWidth:  880,
			MinHeight: 580,
			StateKey:  "main",
			Hidden:    hidden,
			Content:   ui.View(a.View),
		}))
		a.SetWindow(w)
		a.WatchFullScreen(w)
		a.SetVisible(!hidden)
		if hidden && a.HidesDock() {
			mygo.App.Dock.Hide()
		}
		tray := a.NewTray()
		a.SetTray(tray)
		w.OnClose(a.HandleClose)
		w.OnHide(func() { a.SetVisible(false) })
		w.OnShow(func() { a.SetVisible(true) })
		mygo.App.OnLocaleChanged(func(string) { tray.Refresh() })
		a.Start()
	})
	// A logout or `kill` sends SIGTERM: quit as from the tray, so the
	// session ends and Follow GUI networks stop cleanly.
	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-sigs
		a.Quit(false)
	}()
	_ = L.PageSettings // the catalogs register the app's languages
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
	a.Stop()
}
