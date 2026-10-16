// Package gui is the EasyTier MyGo desktop interface. It is a client of
// the daemon (internal/daemon): it starts the no-privileges daemon or
// connects to the privileged helper, keeps one session attached while it
// runs, and renders what the session pushes.
//
// Views are functions of App's state; background results reach them
// through Window.Update under App.mu (.agents/skills/mygo-maintenance).
package gui

import (
	"context"
	"errors"
	"slices"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/helper"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

// trafficKeep is how many traffic points the GUI keeps per network
// (15 minutes at one per second).
const trafficKeep = 900

// staleAfter marks pushed data as old when no frame arrived for this long
// (sessions push at least every few seconds).
const staleAfter = 6 * time.Second

// App is the GUI's state.
type App struct {
	mu sync.Mutex

	prefs  Prefs
	window *mygo.Window
	user   userDaemon

	// Connection.
	socket    string
	connected bool
	connErr   string
	lastPush  time.Time
	session   string
	info      localapi.Info
	helperSt  helper.Status

	// Data pushed by the daemon.
	networks []localapi.NetworkStatus
	traffic  map[string][]localapi.TrafficPoint
	events   []health.Event
	details  map[string]*localapi.NetworkDetail
	settings localapi.Settings

	// View state (main thread).
	page      page
	selected  string
	netTab    int // 0 peers, 1 routes, 2 events
	window5m  int // chart window: 0 1 min, 1 5 min, 2 15 min
	chartSel  int
	busy      map[string]bool
	expanded  map[string]bool // peer rows showing their connections
	toast     string
	toastErr  bool
	toastAt   time.Time
	editor    *configEditor
	createDlg *createDialog
	coresView coresState
	// coreUpdate is a newer stable EasyTier found by the daily check.
	coreUpdate string
	// appUpdate is a newer signed version of the app; updating pauses the
	// user daemon while it is installed (updateProgress 0–1).
	appUpdate      *mygo.Update
	updating       bool
	updateProgress float64
	// helperOutdated: the helper is another version and could not update
	// itself from a signed bundle; updating it needs approval.
	helperOutdated bool
	quitting       bool
	visible        bool
	tray           *Tray
	// openAtLogin caches the login item (reading it needs the main thread).
	openAtLogin bool

	pendingNotes []func()

	ctx    context.Context
	cancel context.CancelFunc
	wake   chan struct{}
}

// page is the main area's content.
type page int

const (
	pageNetwork page = iota
	pageWelcome
	pageSettings
	pageLogs
	pageEdit
)

// New creates the GUI state; Start connects it.
func New() *App {
	ctx, cancel := context.WithCancel(context.Background())
	a := &App{
		prefs: loadPrefs(), traffic: map[string][]localapi.TrafficPoint{}, details: map[string]*localapi.NetworkDetail{},
		busy: map[string]bool{}, expanded: map[string]bool{}, chartSel: -1, window5m: 1, ctx: ctx, cancel: cancel, wake: make(chan struct{}, 1),
		visible: true,
	}
	if a.prefs.Mode == ModeUnset {
		a.page = pageWelcome
	}
	return a
}

// SetWindow lets background updates redraw the window.
func (a *App) SetWindow(w *mygo.Window) {
	a.mu.Lock()
	a.window = w
	a.mu.Unlock()
}

// Start runs the connection loop (main thread, once the app is ready).
func (a *App) Start() {
	login := mygo.App.OpenAtLogin()
	a.mu.Lock()
	a.openAtLogin = login
	a.mu.Unlock()
	go a.connectLoop()
	go a.appUpdateLoop()
}

// Stop ends the session (Follow GUI networks stop) and the user daemon.
func (a *App) Stop() {
	a.cancel()
	a.user.stop()
}

// update runs fn under the lock, redraws the window and refreshes the
// tray on the main thread.
func (a *App) update(fn func()) {
	a.mu.Lock()
	fn()
	w, tr := a.window, a.tray
	notes := a.pendingNotes
	a.pendingNotes = nil
	a.mu.Unlock()
	for _, n := range notes {
		n()
	}
	if w != nil {
		w.Update(func() { tr.Refresh() })
	}
}

// reconnect makes the connection loop start over (after a mode change).
func (a *App) reconnect() {
	select {
	case a.wake <- struct{}{}:
	default:
	}
}

// connectLoop keeps a session attached to the daemon of the chosen mode,
// retrying with a short backoff.
func (a *App) connectLoop() {
	backoff := time.Second
	for a.ctx.Err() == nil {
		a.mu.Lock()
		mode := a.prefs.Mode
		a.mu.Unlock()
		var err error
		switch mode {
		case ModeUnset:
			a.update(func() { a.connected, a.connErr = false, "" })
			err = errWaitForChoice
		case ModeHelper:
			st := helper.Inspect()
			a.update(func() { a.helperSt = st })
			if !st.Installed {
				err = errHelperMissing
			} else {
				err = a.runSession(localapi.SystemSocketPath)
			}
		default:
			a.mu.Lock()
			updating := a.updating
			a.mu.Unlock()
			if updating {
				err = errWaitForChoice // the update replaces the daemon's files
				break
			}
			sock := localapi.UserSocketPath()
			if err = a.user.ensure(a.ctx, sock); err == nil {
				err = a.runSession(sock)
			}
		}
		if a.ctx.Err() != nil {
			return
		}
		msg := ""
		if err != nil && !errors.Is(err, errWaitForChoice) {
			msg = err.Error()
		}
		a.update(func() { a.connected, a.connErr, a.session = false, msg, "" })
		select {
		case <-a.ctx.Done():
			return
		case <-a.wake:
			backoff = time.Second
		case <-time.After(backoff):
			backoff = min(backoff*2, 10*time.Second)
		}
	}
}

var (
	errWaitForChoice = errors.New("no run mode chosen")
	errHelperMissing = errors.New("the privileged helper is not installed")
)

// runSession attaches one session and applies its frames until it ends.
func (a *App) runSession(socket string) error {
	c := localapi.NewClient(socket)
	info, err := c.Do(a.ctx, localapi.Request{Type: localapi.RequestInfo})
	if err != nil {
		return err
	}
	settings, _ := c.Do(a.ctx, localapi.Request{Type: localapi.RequestSettingsGet})
	a.update(func() {
		a.socket, a.info, a.connected, a.connErr = socket, *info.Info, true, ""
		if settings != nil && settings.Settings != nil {
			a.settings = *settings.Settings
		}
	})
	if socket == localapi.SystemSocketPath && info.Info.Version != version.Version {
		go a.updateHelper(c)
	}
	ctx, cancel := context.WithCancel(a.ctx)
	defer cancel()
	go a.detailLoop(ctx, c)
	a.maybeCheckCores(c)
	return c.Attach(ctx, a.watchList(), func(p *localapi.Push) error {
		a.update(func() { a.applyPush(p) })
		if p.Session != "" {
			a.syncWatch()
		}
		select {
		case <-a.wake:
			return errors.New("reconnecting")
		default:
		}
		return nil
	})
}

// applyPush merges one frame into the state (caller holds a.mu).
func (a *App) applyPush(p *localapi.Push) {
	a.lastPush = time.Now()
	if p.Session != "" {
		a.session = p.Session
	}
	a.pendingNotes = append(a.pendingNotes, a.noticeChanges(a.networks, p.Networks)...)
	a.networks = p.Networks
	for id, pts := range p.Traffic {
		cur := append(a.traffic[id], pts...)
		if len(cur) > trafficKeep {
			cur = cur[len(cur)-trafficKeep:]
		}
		a.traffic[id] = cur
	}
	for id := range a.traffic {
		if !slices.ContainsFunc(a.networks, func(n localapi.NetworkStatus) bool { return n.ID == id }) {
			delete(a.traffic, id)
			delete(a.details, id)
		}
	}
	a.events = append(a.events, p.Events...)
	if len(a.events) > 300 {
		a.events = a.events[len(a.events)-300:]
	}
	if a.selected == "" && len(a.networks) > 0 && a.page == pageNetwork {
		a.selected = a.networks[0].ID
	}
}

// detailLoop refreshes the peers of the selected network while visible.
func (a *App) detailLoop(ctx context.Context, c *localapi.Client) {
	t := time.NewTicker(2 * time.Second)
	defer t.Stop()
	for {
		a.mu.Lock()
		id, visible := a.selected, a.visible
		a.mu.Unlock()
		if id != "" && visible {
			if r, err := c.Do(ctx, localapi.Request{Type: localapi.RequestDetail, Network: id}); err == nil {
				a.update(func() { a.details[id] = r.Detail })
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// watchList is what the window displays now (the fast check cadence).
func (a *App) watchList() []string {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.visible || a.selected == "" {
		return nil
	}
	return []string{a.selected}
}

// syncWatch tells the daemon what is on screen.
func (a *App) syncWatch() {
	a.mu.Lock()
	sock, sess := a.socket, a.session
	a.mu.Unlock()
	if sock == "" || sess == "" {
		return
	}
	watch := a.watchList()
	go func() {
		_, _ = localapi.NewClient(sock).Do(a.ctx, localapi.Request{Type: localapi.RequestSessionWatch, Session: sess, Watch: watch})
	}()
}

// SetVisible records whether the window is on screen.
func (a *App) SetVisible(v bool) {
	a.mu.Lock()
	a.visible = v
	a.mu.Unlock()
	a.syncWatch()
}

// client returns a client of the connected daemon, or nil.
func (a *App) client() *localapi.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	if !a.connected {
		return nil
	}
	return localapi.NewClient(a.socket)
}

// do runs a daemon request in the background; busyKey marks a control as
// working meanwhile, and the result shows as a toast on failure (or ok,
// when given).
func (a *App) do(busyKey string, req localapi.Request, ok string, then func(*localapi.Response)) {
	c := a.client()
	if c == nil {
		a.showToast(msgNotConnected(), true)
		return
	}
	a.update(func() { a.busy[busyKey] = true })
	go func() {
		resp, err := c.Do(a.ctx, req)
		a.update(func() {
			delete(a.busy, busyKey)
			if err != nil {
				a.setToast(describeError(err), true)
				return
			}
			if ok != "" {
				a.setToast(ok, false)
			}
			if then != nil {
				then(resp)
			}
		})
	}()
}

func (a *App) showToast(msg string, isErr bool) { a.update(func() { a.setToast(msg, isErr) }) }

// setToast shows a short message (caller holds a.mu).
func (a *App) setToast(msg string, isErr bool) {
	a.toast, a.toastErr, a.toastAt = msg, isErr, time.Now()
}

// networkByID finds a network (caller holds a.mu).
func (a *App) networkByID(id string) (localapi.NetworkStatus, bool) {
	i := slices.IndexFunc(a.networks, func(n localapi.NetworkStatus) bool { return n.ID == id })
	if i < 0 {
		return localapi.NetworkStatus{}, false
	}
	return a.networks[i], true
}

// stale reports pushed data that stopped arriving (caller holds a.mu).
func (a *App) stale() bool {
	return !a.connected || time.Since(a.lastPush) > staleAfter
}

// View is the window's content.
func (a *App) View(c *ui.Context) { a.view(c) }
