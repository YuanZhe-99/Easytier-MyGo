// Package app is the GUI: it renders the daemon's health snapshots with
// MyGo's native UI and sends the user's limited commands back. The GUI
// holds no network state of its own; when the daemon cannot be reached it
// says so instead of showing its last data as current.
package app

import (
	"context"
	"sync"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

// PollInterval is how often the GUI asks the daemon for a snapshot. The
// daemon's own check cadence is independent; this only refreshes the
// view.
const PollInterval = time.Second

// staleAfter is when received data stops being considered current for
// the "connected" banner. It is a little longer than the poll interval so
// one slow round does not flash a warning.
const staleAfter = 4 * time.Second

// App is the GUI state, created once and held for the life of the
// process.
type App struct {
	router         *ui.Router
	client         *localapi.Client
	profileID      string
	profileClients map[string]*localapi.Client
	networks       map[string]*localapi.Status
	networksAt     time.Time
	// Main-thread state for the selected profile's display-name editor.
	profileNameDraft                                       string
	profileRenameBusy                                      bool
	profileRenameMessage                                   string
	loggingLoaded                                          bool
	loggingBusy                                            bool
	loggingConsole, loggingFile, loggingSize, loggingCount string
	loggingMessage                                         string
	loggingCheck                                           *localapi.LoggingSinkCheck
	loggingBase                                            localapi.CoreLogging

	mu         sync.Mutex
	window     *mygo.Window
	haveStatus bool
	status     localapi.Status
	statusAt   time.Time
	connErr    string
	actionErr  string
	started    bool

	peersState  ui.ListState
	eventsState ui.ListState

	// Diagnostic preview is main-thread state; workers publish through the window.
	diagnosticPreview    []byte
	diagnosticBuffer     *ui.TextBuffer
	diagnosticLoading    bool
	diagnosticSaving     bool
	diagnosticMessage    string
	probeSettingsLoaded  bool
	probeEnabled         bool
	probeInterval        string
	probeTargets         string
	probeSettingsMessage string
	probeSettingsBase    string
	historyPreview       *localapi.TrafficHistory
	historyMessage       string

	// The config editor's state is owned by the main thread: view
	// callbacks mutate it, and asynchronous results are published through
	// the window so cross-goroutine writes cannot race the view. The
	// poll goroutine never touches these fields.
	configSel           int // selected row in the list
	configTab           int // 0 = form, 1 = raw text
	configList          ui.ListState
	editorBuf           *configEditor // raw TOML of the loaded config
	editorName          string        // name the buffer holds
	editorRev           string        // revision the buffer was loaded from
	editorDirty         bool          // buffer changed since load/save
	editorLoading       bool
	configMsg           string // last operation result
	configMsgErr        bool
	configValid         bool
	configParseErr      string
	configCoreNote      string
	newConfigName       string // inline "new config" name input
	configSelectPending string // name to select once it appears in the list
	// Form fields (tab 0). formBase is what was loaded; the save path
	// diffs against it and only writes changed keys. All of it is
	// main-thread state (see the comment above).
	formBase       configdoc.Values
	formScroll     ui.ListState
	formExtractErr string
	// basic
	formName, formHost, formIPv4, formListeners, formNetName, formSecret string
	formDHCP                                                             bool
	formPeers                                                            []peerRow
	// advanced
	formFlagVals                                             []bool // shown value per covered flag, in flagFields order
	formDevName                                              string
	formMTU                                                  string
	formBps                                                  string
	formRelayWL                                              string
	formIPv6Auto                                             bool
	formSecureMode                                           bool
	formSecurePrivate, formSecurePublic                      string
	formSocks5                                               string
	formRoutes                                               string
	formExitNodes                                            string
	formMapped                                               string
	formStun, formStunV6, formTCPWhitelist, formUDPWhitelist string
	formCredential, formNetNS, formIPv6Prefix                string
	formProxyNets                                            []proxyRow
	formPortFwds                                             []forwardRow
	formVpnOn                                                bool
	formVpnCIDR                                              string
	formVpnListen                                            string

	// Tray state: created once on the main thread when the app is ready
	// (trayAvailable is false when the desktop has no tray), and the quit
	// flag that lets the close handler tell "hide to tray" from "quit for
	// real".
	tray          *mygo.Tray
	trayMenu      *mygo.Menu
	trayAvailable bool
	trayNotified  bool
	quitting      bool
	quitPending   bool // main-thread request/dialog guard

	done chan struct{}
}

// New creates the app. Call Start once the window exists.
func New() *App {
	return &App{
		router: ui.NewRouter("/overview"),
		client: localapi.NewClient(""),
		done:   make(chan struct{}),
	}
}

// SetWindow lets background updates request redraws.
func (a *App) SetWindow(w *mygo.Window) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.window = w
}

// Focus brings the window to the front (second instance gesture, tray
// "show"). A hidden window is shown again first.
func (a *App) Focus() {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	if w == nil {
		return
	}
	mygo.RunOnMain(func() {
		if !w.IsVisible() {
			w.Show()
		}
		w.Restore()
		w.Focus()
	})
}

// Start begins polling the daemon.
func (a *App) Start() {
	a.mu.Lock()
	if a.started {
		a.mu.Unlock()
		return
	}
	a.started = true
	a.mu.Unlock()
	go a.pollLoop()
}

// Stop ends polling (window closed for good).
func (a *App) Stop() {
	select {
	case <-a.done:
	default:
		close(a.done)
	}
}

func (a *App) pollLoop() {
	// Ask promptly once, then on the interval.
	a.poll()
	ticker := time.NewTicker(PollInterval)
	defer ticker.Stop()
	for {
		select {
		case <-a.done:
			return
		case <-ticker.C:
			a.poll()
		}
	}
}

func (a *App) poll() {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	networks, networkErr := a.client.Networks(ctx)
	if networkErr == nil && len(networks) > 0 {
		a.mu.Lock()
		a.networks = networks
		a.networksAt = time.Now()
		if a.profileID == "" {
			ids := networkIDs(networks)
			a.profileID = ids[0]
		}
		a.mu.Unlock()
	}
	client := a.currentClient()
	st, err := client.Status(ctx)

	a.mu.Lock()
	if client != a.currentClientLocked() {
		a.mu.Unlock()
		return
	}
	if err != nil {
		a.connErr = err.Error()
	} else {
		a.status = *st
		a.statusAt = time.Now()
		a.haveStatus = true
		a.connErr = ""
	}
	w := a.window
	a.mu.Unlock()

	// Menu item setters lock and post to the main thread themselves, so
	// the tray stays current even while the window is hidden.
	a.trayRefresh()
	if w != nil {
		w.Invalidate()
	}
}

// clipboardWrite puts text on the system clipboard.
func clipboardWrite(text string) { mygo.Clipboard.WriteText(text) }

// triggerPoll refreshes immediately (button press).
func (a *App) triggerPoll() {
	go a.poll()
}

// confirmThen asks the user before a disruptive command (stopping the
// core, stopping the daemon). It runs the dialog off the main thread.
func (a *App) confirmThen(message, detail string, fn func(ctx context.Context) error) {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		res, err := mygo.Dialog.Message(mygo.MessageOptions{
			Parent:       w,
			Type:         mygo.MessageQuestion,
			Message:      message,
			Detail:       detail,
			Buttons:      []string{"继续", "取消"},
			CancelButton: 1,
		})
		if err != nil || res.Button != 0 {
			return
		}
		a.do(fn)
	}()
}

// do runs a daemon command in the background and refreshes afterwards.
func (a *App) do(fn func(ctx context.Context) error) {
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		err := fn(ctx)
		a.mu.Lock()
		if err != nil {
			a.actionErr = err.Error()
		} else {
			a.actionErr = ""
		}
		a.mu.Unlock()
		a.poll()
	}()
}

// snapshotCopy returns the current view state under the lock.
type snapshotCopy struct {
	haveStatus bool
	status     localapi.Status
	statusAt   time.Time
	connErr    string
	actionErr  string
	now        time.Time
	live       bool
}

func (a *App) snapshotCopy() snapshotCopy {
	a.mu.Lock()
	defer a.mu.Unlock()
	now := time.Now()
	return snapshotCopy{
		haveStatus: a.haveStatus,
		status:     a.status,
		statusAt:   a.statusAt,
		connErr:    a.connErr,
		actionErr:  a.actionErr,
		now:        now,
		live:       a.haveStatus && now.Sub(a.statusAt) <= staleAfter && a.connErr == "" && checkEvidenceFresh(a.status, now),
	}
}

func checkEvidenceFresh(status localapi.Status, now time.Time) bool {
	interval := time.Duration(status.Daemon.CheckIntervalMS) * time.Millisecond
	if interval <= 0 {
		return true
	} // legacy/test snapshots without cadence
	return !status.CheckedAt.IsZero() && now.Sub(status.CheckedAt) <= 3*interval+3*time.Second
}

// configCopy is the config editor's view of the state.
type configCopy struct {
	haveStatus      bool
	live            bool
	access          string // "read" or "write"
	configs         []localapi.ConfigEntry
	activeConfig    string
	appliedRevision string
	activationState string
	activationError string
	configDir       string
	pendingSelect   string
	selected        int
	tab             int
	editorName      string
	editorRev       string
	editorDirty     bool
	editorLoading   bool
	editorBuf       *configEditor
	msg             string
	msgErr          bool
	valid           bool
	parseErr        string
	coreNote        string
	formExtractErr  string
}

func (a *App) configCopy() configCopy {
	// Status fields belong to the poll goroutine (locked); the editor's
	// fields belong to the main thread and are read without a lock.
	a.mu.Lock()
	haveStatus := a.haveStatus
	status := a.status
	statusAt := a.statusAt
	connErr := a.connErr
	a.mu.Unlock()

	now := time.Now()
	return configCopy{
		haveStatus:      haveStatus,
		live:            haveStatus && now.Sub(statusAt) <= staleAfter && connErr == "",
		access:          status.Daemon.Access,
		configs:         status.Configs,
		activeConfig:    status.ActiveConfig,
		appliedRevision: status.AppliedRevision,
		activationState: status.ActivationState,
		activationError: status.ActivationError,
		configDir:       status.ConfigDir,
		pendingSelect:   a.configSelectPending,
		selected:        a.configSel,
		tab:             a.configTab,
		editorName:      a.editorName,
		editorRev:       a.editorRev,
		editorDirty:     a.editorDirty,
		editorLoading:   a.editorLoading,
		editorBuf:       a.editorBuf,
		msg:             a.configMsg,
		msgErr:          a.configMsgErr,
		valid:           a.configValid,
		parseErr:        a.configParseErr,
		coreNote:        a.configCoreNote,
		formExtractErr:  a.formExtractErr,
	}
}

// setConfigMsg records the last operation result shown on the page. It
// runs on the main thread (view callbacks or published results).
func (a *App) setConfigMsg(err error, ok string) {
	if err != nil {
		a.configMsg = err.Error()
		a.configMsgErr = true
		return
	}
	a.configMsg = ok
	a.configMsgErr = false
}

// publish runs fn on the main thread when a window exists (MyGo's
// Window.Update), or inline otherwise (tests). Asynchronous results go
// through it so the main-thread-owned editor state never races the view.
func (a *App) publish(fn func()) {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	if w != nil {
		w.Update(fn)
		return
	}
	fn()
}

// SyncWatching tells the daemon what this GUI currently has on screen, so
// it can pick its sampling tier: the detail queries every round while a
// window is up, the traffic curve every round while the overview is up.
// It runs on the main thread (a view build, or a show/hide event).
func (a *App) SyncWatching() {
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	visible := w != nil && w.IsVisible()
	client := a.currentClient()
	client.SetWantDetails(watchingDetails(visible))
	client.SetWantTraffic(watchingTraffic(a.router.Path(), visible))
}

// watchingDetails is the pure decision behind SyncWatching's detail tier:
// any visible window.
func watchingDetails(visible bool) bool { return visible }

// watchingTraffic is the pure decision for the curve: only the overview,
// and only while the window is on screen.
func watchingTraffic(path string, visible bool) bool {
	return visible && path == "/overview"
}

// View renders the whole window: sidebar, page, and a status bar that
// always tells whether the data shown is current.
func (a *App) View(c *ui.Context) {
	// The detail queries and the traffic curve are sampled on demand:
	// say what this window is showing right now.
	a.SyncWatching()
	ui.Column(c).Fill().Children(func() {
		a.networkSelector(c)
		ui.Row(c).Fill().Grow(1).AlignItems(ui.Stretch).Children(func() {
			a.sidebar(c)
			ui.Column(c).Fill().Grow(1).Children(func() {
				a.banner(c)
				if err := a.snapshotCopy().status.EventPersistenceError; err != "" {
					ui.Text(c, "健康事件未可靠保存："+err).TextColor(c.Theme().Warning).Padding(8)
				}
				a.router.View(c, func(r *ui.Route) {
					switch {
					case r.Match("/overview"):
						r.Title("总览")
						a.overview(c)
					case r.Match("/networks"):
						r.Title("网络")
						a.configPage(c)
					case r.Match("/nodes"):
						r.Title("节点与路由")
						a.nodes(c)
					case r.Match("/health"):
						r.Title("服务与健康")
						a.healthPage(c)
					case r.Match("/settings"):
						r.Title("设置")
						a.settingsPage(c)
					default:
						ui.Text(c, "页面不存在")
					}
				})
			})
		})
		a.statusBar(c)
	})
}

// statusBar is the always-present footer: connection state, freshness and
// where the daemon runs from.
func (a *App) statusBar(c *ui.Context) {
	s := a.snapshotCopy()
	t := c.Theme()
	ui.Row(c).Padding(4, 12).Gap(12).AlignItems(ui.Center).Background(t.Surface).Children(func() {
		switch {
		case !s.haveStatus:
			ui.Text(c, "●").FontSize(10).TextColor(t.Danger)
			ui.Text(c, "管理服务未连接").FontSize(11).TextColor(t.TextMuted)
		case !s.live:
			ui.Text(c, "●").FontSize(10).TextColor(t.Warning)
			ui.Text(c, "监控数据过期（"+relTime(s.now, s.statusAt)+"）").FontSize(11).TextColor(t.TextMuted)
		default:
			ui.Text(c, "●").FontSize(10).TextColor(t.Success)
			ui.Text(c, "监控在线 · 第 "+itoa(int(s.status.Round))+" 次检查 · "+relTime(s.now, s.status.CheckedAt)).
				FontSize(11).TextColor(t.TextMuted)
		}
		ui.Spacer(c)
		if s.haveStatus {
			mode := modeWord(s.status.Daemon.Mode)
			if s.status.Daemon.ServiceManaged {
				mode += " · 系统服务"
			}
			ui.Text(c, mode).FontSize(11).TextColor(t.TextMuted)
			if v := s.status.Daemon.CoreVersion; v != "" {
				ui.Text(c, "核心 "+v).FontSize(11).TextColor(t.TextMuted)
			}
			// On a system socket other local users are read-only: say so
			// rather than letting them press buttons that will be refused.
			if s.status.Daemon.Access == "read" {
				ui.Text(c, "只读访问").FontSize(11).TextColor(t.Warning)
			}
		}
		ui.Button(c, "退出…").OnClick(func() { a.TrayQuit() })
	})
}

// readOnly reports whether the daemon granted this GUI read access only
// (a system socket reached by a user outside the authorized group).
func (a *App) readOnly() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.haveStatus && a.status.Daemon.Access == "read"
}

func (a *App) sidebar(c *ui.Context) {
	page := a.router.Path()
	if ui.Sidebar(c, &page, func() {
		ui.SidebarSection(c, "网络", nil, func() {
			ui.SidebarItem(c, "/overview", iconOverview, "总览")
			ui.SidebarItem(c, "/networks", iconNetworks, "网络")
			ui.SidebarItem(c, "/nodes", iconNodes, "节点与路由")
		})
		ui.SidebarSection(c, "后台", nil, func() {
			ui.SidebarItem(c, "/health", iconHealth, "服务与健康")
		})
		ui.SidebarSection(c, "应用", nil, func() {
			ui.SidebarItem(c, "/settings", iconSettings, "设置")
		})
	}).Width(190).Changed() {
		a.router.Push(page)
	}
}

// banner shows the daemon connection state: a quiet strip when live, a
// loud one when the monitor cannot be reached, so stale data is never
// mistaken for current health.
func (a *App) banner(c *ui.Context) {
	s := a.snapshotCopy()
	t := c.Theme()
	switch {
	case s.live:
		return
	case s.haveStatus:
		ui.Row(c).Padding(10, 16).Gap(8).Background(t.Surface).Children(func() {
			ui.Text(c, "监控数据可能已过期").Bold().TextColor(t.Warning)
			ui.Text(c, "无法连接管理服务，显示的是 "+relTime(s.now, s.statusAt)+"的数据。").TextColor(t.TextMuted)
			ui.Button(c, "重试").OnClick(func() { a.triggerPoll() })
		})
	case s.connErr != "":
		ui.Row(c).Padding(10, 16).Gap(8).Background(t.Surface).Children(func() {
			ui.Text(c, "管理服务未运行").Bold().TextColor(t.Danger)
			ui.Text(c, "启动 easytier-mygo-svc 后，核心与网络健康状态会显示在这里。").TextColor(t.TextMuted)
			ui.Button(c, "重试").OnClick(func() { a.triggerPoll() })
		})
	}
}

// contentColumn is the shared page frame.
func contentColumn(c *ui.Context) ui.Element {
	// ClipX keeps a long single-line row from turning the page into a
	// horizontal scroller; tables scroll inside themselves.
	return ui.Column(c).Fill().Padding(20).Gap(14).ClipX()
}

func sectionTitle(c *ui.Context, title, note string) {
	t := c.Theme()
	ui.Text(c, title).FontSize(20).Bold()
	if note != "" {
		ui.Text(c, note).TextColor(t.TextMuted).FontSize(12)
	}
}
