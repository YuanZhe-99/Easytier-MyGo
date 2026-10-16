package app

// The tray: an icon in the menu bar (macOS) or notification area (Linux,
// Windows) that keeps the GUI reachable after its window closes. It is a
// convenience layer over the same daemon commands the pages use — the
// daemon and the core keep running whether the icon exists or not.
//
// Closing the window hides it when a tray exists and quits the GUI when
// one does not (see the window's close handler in cmd/easytier-mygo).
// That keeps the promise the other way round too: a desktop without tray
// support never leaves a windowless process behind.

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"runtime"
	"sort"
	"strings"
	"time"

	"github.com/egoist/mygo"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// trayMenuIDs are the stable IDs of the tray's menu items.
const (
	trayIDStatus = "tray.status"
	trayIDShow   = "tray.show"
	trayIDStart  = "tray.start"
	trayIDStop   = "tray.stop"
	trayIDQuit   = "tray.quit"
)

// TraySetup creates the tray icon and menu, or leaves the app without a
// tray when the desktop has none (no AppIndicator library, no
// StatusNotifier host, ...). Without a tray the window's close button
// keeps quitting the GUI, which is the safe fallback.
//
// It runs on the main thread (App.WhenReady).
func (a *App) TraySetup() {
	tray, err := mygo.NewTray(mygo.TrayOptions{
		Icon:    trayIconPNG(),
		ToolTip: "EasyTier MyGo",
	})
	if err != nil {
		a.mu.Lock()
		a.trayAvailable = false
		a.mu.Unlock()
		return
	}
	menu := a.newTrayMenu()
	tray.SetMenu(menu)
	a.mu.Lock()
	a.tray = tray
	a.trayMenu = menu
	a.trayAvailable = true
	a.mu.Unlock()
	a.trayRefresh()
}

// newTrayMenu builds the menu template. Separated from traySetup so the
// refresh logic is testable without a native icon.
func (a *App) newTrayMenu() *mygo.Menu {
	return mygo.NewMenu([]*mygo.MenuItem{
		{ID: trayIDStatus, Label: "尚未连接管理服务", Disabled: true},
		mygo.Separator(),
		{ID: trayIDShow, Label: "显示主界面", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.Focus()
		}},
		{ID: trayIDStart, Label: "启动网络", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.trayStart()
		}},
		{ID: trayIDStop, Label: "停止网络", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.trayStop()
		}},
		mygo.Separator(),
		{ID: trayIDQuit, Label: "退出 EasyTier MyGo", Click: func(*mygo.MenuItem, *mygo.Window) {
			a.TrayQuit()
		}},
	})
}

// trayStart starts the managed core from the tray. It follows the same
// path as the page button: the command goes to the daemon, which owns the
// desired state and the recovery rules.
func (a *App) trayStart() {
	client := a.currentClient()
	a.do(func(ctx context.Context) error {
		_, err := client.StartCore(ctx)
		return err
	})
}

// trayStop stops the managed core from the tray. Stopping is destructive,
// so it asks first; the dialog is the same confirmation the page uses.
func (a *App) trayStop() {
	client := a.currentClient()
	a.confirmThen("停止网络？",
		"核心将停止，本机网络会断开。手动停止不会被自动恢复重新启动。",
		func(ctx context.Context) error {
			_, err := client.StopCore(ctx)
			return err
		})
}

// TrayQuit asks what to do with the network before leaving the GUI. The
// three answers keep the network policy explicit: the user either leaves
// it running (the daemon and core keep working), stops it first, or
// cancels and keeps the GUI open.
func (a *App) TrayQuit() {
	a.confirmQuit()
}

// confirmQuit shows the quit dialog and carries out the chosen action. It
// runs off the main thread like the other confirmations.
func (a *App) confirmQuit() {
	if a.quitPending {
		return
	}
	if a.hasUnsaved() {
		a.Focus()
		a.configMsg = "请先保存或放弃修改，再退出"
		a.configMsgErr = true
		return
	}
	targets := a.quitTargets()
	a.mu.Lock()
	w := a.window
	haveStatus := a.haveStatus
	mode := a.status.Daemon.Mode
	a.mu.Unlock()

	// With no snapshot, or in monitor mode (the daemon did not start the
	// core and cannot stop it), there is nothing to offer but quitting.
	if !quitOffersStop(haveStatus, mode) {
		a.Quit()
		return
	}
	a.quitPending = true

	go func() {
		ids := make([]string, 0, len(targets))
		for id := range targets {
			ids = append(ids, id)
		}
		sort.Strings(ids)
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:  w,
			Type:    mygo.MessageQuestion,
			Message: "退出 EasyTier MyGo",
			Detail:  "退出界面后，网络可以继续运行。停止范围：" + strings.Join(ids, "、"),
			Buttons: []string{"保留网络并退出", "停止全部受管网络并退出", "取消"},
			// Enter keeps the network running: the least destructive
			// choice, and the one the tray already implies.
			DefaultButton: 0,
			CancelButton:  2,
		})
		if err != nil {
			a.publish(func() { a.quitPending = false })
			return
		}
		a.publish(func() {
			if res.Button == 1 {
				a.quitAfterTargets(targets)
			} else {
				a.quitPending = false
				a.quitAction(res.Button)
			}
		})
	}()
}

// quitAction carries out the quit dialog's answer. Button indices match the
// buttons above; anything else (including Cancel) keeps the GUI open.
func (a *App) quitAction(button int) {
	switch button {
	case 1:
		// Stop the core first, then quit. The stop is explicit and
		// recorded as the desired state, so the daemon will not restart
		// it.
		a.quitAfterStop()
	case 0:
		a.Quit()
	}
}

// quitOffersStop reports whether the quit dialog offers the "stop the
// network too" choice. Only a managed daemon that started the core can
// stop it, and only a known snapshot can say whether it is running.
func quitOffersStop(haveStatus bool, mode string) bool {
	return haveStatus && mode == "managed"
}

// quitAfterStop stops the managed core and then ends the GUI. It runs in
// the background; the user sees the tray disappear once the daemon has the
// stop. A failed stop leaves the GUI running so the error stays visible
// instead of quitting with the network still up.
func (a *App) quitAfterStop() {
	a.quitAfterTargets(a.quitTargets())
}

func (a *App) quitAfterTargets(targets map[string]*localapi.Client) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second*time.Duration(max(1, len(targets))))
		defer cancel()
		if err := stopNetworkTargets(ctx, targets); err != nil {
			a.publish(func() { a.quitPending = false })
			a.mu.Lock()
			a.actionErr = err.Error()
			a.mu.Unlock()
			a.poll()
			return
		}
		a.mu.Lock()
		a.actionErr = ""
		a.mu.Unlock()
		a.Quit()
	}()
}

// Quit leaves the GUI for good. Callers that need the network decision
// first go through TrayQuit. The app's quit sequence closes the window,
// which must not hide itself this time — MarkQuitting is called from
// App.OnBeforeQuit before that happens.
func (a *App) Quit() {
	a.MarkQuitting()
	mygo.App.Quit()
}

// BeforeQuit handles application-menu and OS quit requests before any window
// closes. Only a previously confirmed exit may pass without asking again.
func (a *App) BeforeQuit(e *mygo.QuitEvent) {
	a.mu.Lock()
	approved := a.quitting
	a.mu.Unlock()
	if approved {
		return
	}
	e.PreventDefault()
	a.confirmQuit()
}

// CloseWindow keeps tray hiding separate from quitting. Without a tray the
// window stays alive until the shared quit decision finishes.
func (a *App) CloseWindow(e *mygo.CloseEvent) bool {
	a.mu.Lock()
	approved := a.quitting
	tray := a.trayAvailable
	a.mu.Unlock()
	if approved {
		return false
	}
	e.PreventDefault()
	if tray {
		return true
	}
	a.confirmQuit()
	return false
}

// MarkQuitting records that the application is quitting, so the window's
// close handler stops hiding it into the tray.
func (a *App) MarkQuitting() {
	a.mu.Lock()
	a.quitting = true
	a.mu.Unlock()
}

// QuittingToTray reports whether closing the window should hide it
// instead of ending the GUI. Only a live tray makes that true.
func (a *App) QuittingToTray() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.trayAvailable && !a.quitting
}

// WindowHidden records that the window went to the tray and shows a
// one-time notification so a user who closed the window knows the GUI is
// still running.
func (a *App) WindowHidden() {
	a.mu.Lock()
	first := !a.trayNotified
	a.trayNotified = true
	a.mu.Unlock()
	if !first {
		return
	}
	// Off the main thread: both the support check and Show may wait on
	// D-Bus, which must not stall the UI.
	go func() {
		if !mygo.NotificationsSupported() {
			return
		}
		n := mygo.NewNotification(mygo.NotificationOptions{
			ID:    "easytier-mygo-tray",
			Title: "EasyTier MyGo 仍在运行",
			Body:  "关闭窗口只是隐藏到托盘；网络服务与健康监控继续工作。",
		})
		_ = n.Show()
	}()
}

// trayRefresh re-labels the tray menu from the latest snapshot. It runs on
// the main thread; the poll goroutine only asks for it through the window.
func (a *App) trayRefresh() {
	a.mu.Lock()
	menu := a.trayMenu
	haveStatus := a.haveStatus
	st := a.status
	statusAt := a.statusAt
	connErr := a.connErr
	profileID := a.profileID
	networks := a.networks
	networksAt := a.networksAt
	a.mu.Unlock()
	if menu == nil {
		return
	}
	status := menu.ItemByID(trayIDStatus)
	start := menu.ItemByID(trayIDStart)
	stop := menu.ItemByID(trayIDStop)
	if status == nil || start == nil || stop == nil {
		return
	}

	now := time.Now()
	live := haveStatus && now.Sub(statusAt) <= staleAfter && connErr == ""
	coreSt, coreOK := findComponent(st.Health, health.KindCore)
	manuallyStopped := coreOK && coreSt.Desired == health.DesiredStop

	switch {
	case !haveStatus:
		status.SetLabel("尚未连接管理服务")
	case !live:
		status.SetLabel("监控数据过期 · " + levelWord(aggregate(st.Health)))
	case manuallyStopped:
		status.SetLabel("已停止（手动）· " + relTime(now, st.CheckedAt))
	default:
		status.SetLabel(levelWord(aggregate(st.Health)) + " · " + relTime(now, st.CheckedAt))
	}
	if len(networks) > 0 {
		level := health.LevelHealthy
		fresh := live && now.Sub(networksAt) <= staleAfter
		for _, network := range networks {
			if network == nil || !checkEvidenceFresh(*network, now) {
				fresh = false
				continue
			}
			candidate := aggregate(network.Health)
			level = aggregate([]health.State{{Level: level}, {Level: candidate}})
		}
		label := levelWord(level) + " · " + itoa(len(networks)) + " 个网络"
		if !fresh {
			label = "监控数据过期 · " + label
		}
		status.SetLabel(label)
		start.SetLabel("启动当前网络 · " + profileID)
		stop.SetLabel("停止当前网络 · " + profileID)
	}

	// The start/stop pair follows the desired state, exactly like the
	// overview control. Monitor mode has nothing to start or stop; a
	// read-only connection (a system socket seen by a user outside the
	// authorized group) is not offered actions it cannot perform.
	managed := haveStatus && st.Daemon.Mode == "managed"
	readOnly := haveStatus && st.Daemon.Access == "read"
	switch {
	case !managed || !live || readOnly:
		start.SetVisible(false)
		stop.SetVisible(false)
	case manuallyStopped:
		start.SetVisible(true)
		stop.SetVisible(false)
	default:
		start.SetVisible(false)
		stop.SetVisible(true)
	}
}

// TrayDestroy removes the icon (quit).
func (a *App) TrayDestroy() {
	a.mu.Lock()
	tray := a.tray
	a.tray = nil
	a.trayMenu = nil
	a.mu.Unlock()
	if tray != nil {
		tray.Destroy()
	}
}

// trayIconPNG draws the tray icon: three connected nodes (the network
// shape), drawn at 4x and box-filtered down so the small panel size stays
// legible. macOS gets a black template icon to tint; elsewhere a blue one
// stays visible on light and dark panels.
func trayIconPNG() []byte {
	ink := color.NRGBA{R: 79, G: 124, B: 255, A: 255}
	if runtime.GOOS == "darwin" {
		ink = color.NRGBA{A: 255}
	}

	const scale = 4
	const size = 32
	big := image.NewNRGBA(image.Rect(0, 0, size*scale, size*scale))
	// A ring with a horizontal bar (the sidebar's network glyph,
	// simplified): a shape that stays legible when the panel scales it
	// down to 16 px, where three small dots would merge.
	cx, cy := size*scale/2, size*scale/2
	outer, inner := 11*scale, 7*scale
	for y := 0; y < size*scale; y++ {
		for x := 0; x < size*scale; x++ {
			dx, dy := x-cx, y-cy
			d2 := dx*dx + dy*dy
			onRing := d2 <= outer*outer && d2 >= inner*inner
			onBar := abs(dy) <= scale && abs(dx) <= outer
			if onRing || onBar {
				big.SetNRGBA(x, y, ink)
			}
		}
	}

	// Box filter 4x4 -> 1: keeps the glyph readable at panel sizes.
	out := image.NewNRGBA(image.Rect(0, 0, size, size))
	for y := 0; y < size; y++ {
		for x := 0; x < size; x++ {
			var r, g, b, a int
			for dy := 0; dy < scale; dy++ {
				for dx := 0; dx < scale; dx++ {
					c := big.NRGBAAt(x*scale+dx, y*scale+dy)
					r += int(c.R) * int(c.A)
					g += int(c.G) * int(c.A)
					b += int(c.B) * int(c.A)
					a += int(c.A)
				}
			}
			if a == 0 {
				continue
			}
			out.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r / a),
				G: uint8(g / a),
				B: uint8(b / a),
				A: uint8(a / (scale * scale)),
			})
		}
	}
	var buf bytes.Buffer
	_ = png.Encode(&buf, out)
	return buf.Bytes()
}

func abs(v int) int {
	if v < 0 {
		return -v
	}
	return v
}
