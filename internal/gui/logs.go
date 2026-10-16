package gui

import (
	"errors"
	"slices"

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
	ui.Scroll(c.Key("logs-page")).Grow(1).Padding(24).Gap(16).Children(func() {
		ui.Text(c, L.PageLogs()).FontSize(sizeTitle).Bold()
		eventTimeline(c, evs, true)
	})
}

// eventTimeline lists events newest first: a dot and line on the left, the
// time and a sentence on the right. withNetwork prefixes the network.
func eventTimeline(c *ui.Context, evs []health.Event, withNetwork bool) {
	t := c.Theme()
	k := tok(c)
	if len(evs) == 0 {
		ui.Text(c, L.EventsEmpty()).TextColor(t.TextMuted)
		return
	}
	card(c).Gap(0).Children(func() {
		for i := len(evs) - 1; i >= 0 && i >= len(evs)-100; i-- {
			e := evs[i]
			text, col := eventSentence(e, k)
			ui.Row(c.Key(i)).Gap(12).PaddingY(6).AlignItems(ui.Start).Children(func() {
				ui.Column(c).Width(10).AlignItems(ui.Center).PaddingY(5).Children(func() { dot(c, col, 8) })
				ui.Column(c).Grow(1).Gap(2).Children(func() {
					head := e.Time.Local().Format("01-02 15:04:05")
					if withNetwork {
						head += "  ·  " + e.Component.ID
					}
					ui.Text(c, head).FontSize(sizeSmall).TextColor(t.TextMuted)
					ui.Text(c, text).Selectable()
					if e.Detail != "" {
						ui.Text(c, e.Detail).FontSize(sizeSmall).TextColor(t.TextMuted).Selectable().MaxLines(3).Ellipsis("…")
					}
				})
			})
		}
	})
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
