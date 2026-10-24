package gui

import (
	"errors"
	"slices"
	"time"

	"github.com/egoist/mygo/ui"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	L "github.com/YuanZhe-99/Easytier-MyGo/locales"
)

// logsView is the timeline of every network's events.
func (a *App) logsView(c *ui.Context) {
	a.mu.Lock()
	evs := slices.Clone(a.events)
	names := map[string]string{}
	for _, n := range a.networks {
		names[n.ID] = n.Name
	}
	a.mu.Unlock()
	for i := range evs {
		if name := names[evs[i].Component.ID]; name != "" {
			evs[i].Component.ID = name
		}
	}
	head := paneHead{icon: iconActivity, title: L.PageLogs(), sub: L.ActivitySub(), actions: func() {
		if ui.Button(c.Key("export"), L.ActionExportDiagnostics()).Clicked() {
			a.exportDiagnostics()
		}
	}}
	a.pane(c, head, func() {
		pageColumn(c, "logs-page", activityW, func() { eventTimeline(c, evs, true) })
	})
}

// activityW is the Activity page's column, at most; eventTimeW its times'.
const (
	activityW  float32 = 820
	eventTimeW float32 = 64
)

// eventTimeline lists the newest 100 events, newest first, by day: a
// muted day over a rounded list whose rows hold the time, the state's
// dot, the sentence with its detail and, when withNetwork, the network.
func eventTimeline(c *ui.Context, evs []health.Event, withNetwork bool) {
	t := c.Theme()
	k := tok(c)
	if len(evs) == 0 {
		ui.Text(c, L.EventsEmpty()).TextColor(t.TextMuted).Margin(14, 4, 0, 4)
		return
	}
	bg := t.Background
	if t.Dark {
		bg = t.Surface
	}
	now := time.Now()
	for i := len(evs) - 1; i >= 0 && i >= len(evs)-100; {
		day := evs[i].Time.Local().Format(time.DateOnly)
		ui.Text(c, dayName(evs[i].Time, now)).FontSize(sizeSmall).Bold().TextColor(t.TextMuted).Padding(18, 4, 8, 4)
		ui.Column(c.Key("day-"+day)).FillWidth().Radius(listRadius).Border(1, t.Border).Background(bg).Clip().Children(func() {
			first := true
			for ; i >= 0 && i >= len(evs)-100 && evs[i].Time.Local().Format(time.DateOnly) == day; i-- {
				e := evs[i]
				if !first {
					ui.Box(c).Height(1).FillWidth().Background(t.Border)
				}
				first = false
				text, col := eventSentence(e, k)
				ui.Row(c.Key(i)).FillWidth().Gap(14).Padding(11, 16).AlignItems(ui.Start).Children(func() {
					ui.Text(c, e.Time.Local().Format("15:04:05")).FontSize(sizeSmall).TextColor(t.TextMuted).Width(eventTimeW).Shrink(0)
					ui.Column(c).Width(10).Shrink(0).AlignItems(ui.Center).PaddingY(4).Children(func() { dot(c, col, 8) })
					ui.Column(c).Grow(1).MinWidth(0).Gap(2).Children(func() {
						ui.Text(c, text).Selectable()
						if e.Detail != "" {
							ui.Text(c, e.Detail).FontSize(sizeSmall).TextColor(t.TextMuted).Selectable().MaxLines(3).Ellipsis("…")
						}
					})
					if withNetwork {
						ui.Box(c).Padding(2, 8).Radius(6).Background(t.SurfaceHover).Shrink(0).Children(func() {
							ui.Text(c, e.Component.ID).FontSize(sizeSmall).TextColor(t.TextMuted).NoWrap()
						})
					}
				})
			}
		})
	}
}

// dayName names an event's day: Today, Yesterday, else its date.
func dayName(at, now time.Time) string {
	y, m, d := at.Local().Date()
	ny, nm, nd := now.Local().Date()
	today := time.Date(ny, nm, nd, 0, 0, 0, 0, time.Local)
	switch time.Date(y, m, d, 0, 0, 0, 0, time.Local) {
	case today:
		return L.DayToday()
	case today.AddDate(0, 0, -1):
		return L.DayYesterday()
	}
	return at.Local().Format(time.DateOnly)
}

// eventSentence turns a health event into words and a color.
func eventSentence(e health.Event, k tokens) (string, ui.Color) {
	core := e.Component.Kind == health.KindCore
	switch e.Kind {
	case health.EventLevelChanged:
		switch e.To {
		case health.LevelHealthy:
			if core {
				return L.EventCoreHealthy(), k.good
			}
			return L.EventConnected(), k.good
		case health.LevelFailed:
			return L.EventFailed(reasonText(e.Reason)), k.bad
		case health.LevelDegraded:
			return L.EventDegraded(reasonText(e.Reason)), k.warn
		default:
			return L.EventUnknown(reasonText(e.Reason)), k.muted
		}
	case health.EventRecoveryScheduled:
		return L.EventRecoveryScheduled(), k.warn
	case health.EventRecoveryStarted:
		return L.EventRecoveryStarted(e.Attempt), k.warn
	case health.EventRecoveryFinished:
		return L.EventRecoveryFinished(), k.good
	case health.EventRecoveryExhausted:
		return L.EventRecoveryExhausted(), k.bad
	case health.EventConfigApplied:
		return L.EventConfigApplied(), k.good
	case health.EventConfigRollback:
		return L.EventConfigRollback(), k.bad
	}
	return e.Kind.String(), k.muted
}

// reasonText explains a machine reason in words.
func reasonText(r string) string {
	switch r {
	case "process-exited":
		return L.ReasonExited()
	case "rpc-timeout":
		return L.ReasonTimeout()
	case "rpc-unreachable", "no-instances":
		return L.ReasonUnreachable()
	case "starting", "core-starting":
		return L.ReasonStarting()
	case "no-peers":
		return L.ReasonNoPeers()
	case "awaiting-address":
		return L.ReasonAwaitingAddress()
	case "", "manual-stop", "core-not-running", "core-not-answering":
		return L.ReasonStopped()
	}
	return L.ReasonCollector()
}

// msgNotConnected is the toast when no daemon is connected.
func msgNotConnected() string { return L.ToastNotConnected() }

// describeError makes a daemon error readable.
func describeError(err error) string {
	switch localapi.CodeOf(err) {
	case localapi.CodeNeedsHelper:
		return L.ErrorNeedsHelper()
	case localapi.CodeConflict:
		return L.EditConflict()
	case localapi.CodeInvalidConfig:
		return L.ErrorInvalidConfig(err.Error())
	case localapi.CodeNeedsAdmin:
		return L.ErrorNeedsAdmin()
	}
	if errors.Is(err, localapi.ErrNotRunning) {
		return L.ToastNotConnected()
	}
	return err.Error()
}
