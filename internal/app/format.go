package app

import (
	"fmt"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/egoist/mygo/ui"
)

// levelLabel is the Chinese label shown for a health level.
func levelLabel(l health.Level) string {
	switch l {
	case health.LevelHealthy:
		return "正常"
	case health.LevelDegraded:
		return "降级"
	case health.LevelFailed:
		return "异常"
	default:
		return "未知"
	}
}

// levelColor maps a level onto the theme so green never outlives its
// evidence: unknown is muted, not green.
func levelColor(t *ui.Theme, l health.Level) ui.Color {
	switch l {
	case health.LevelHealthy:
		return t.Success
	case health.LevelDegraded:
		return t.Warning
	case health.LevelFailed:
		return t.Danger
	default:
		return t.TextMuted
	}
}

// levelWord describes a level in a sentence.
func levelWord(l health.Level) string {
	switch l {
	case health.LevelHealthy:
		return "正常"
	case health.LevelDegraded:
		return "降级运行"
	case health.LevelFailed:
		return "异常"
	default:
		return "状态未知"
	}
}

// componentLabel names a component for the interface.
func componentLabel(c health.Component) string {
	switch c.Kind {
	case health.KindService:
		return "系统服务"
	case health.KindCore:
		return "EasyTier 核心"
	case health.KindInstance:
		return "网络 " + c.ID
	case health.KindProbe:
		return "探测 " + c.ID
	default:
		return c.String()
	}
}

// aggregate summarizes several component states into one level. Failed
// is loudest, then degraded, then unknown; an empty set is unknown.
func aggregate(states []health.State) health.Level {
	if len(states) == 0 {
		return health.LevelUnknown
	}
	worst := health.LevelHealthy
	rank := func(l health.Level) int {
		switch l {
		case health.LevelFailed:
			return 3
		case health.LevelDegraded:
			return 2
		case health.LevelUnknown:
			return 1
		default:
			return 0
		}
	}
	for _, s := range states {
		if rank(s.Level) > rank(worst) {
			worst = s.Level
		}
	}
	return worst
}

// levelDot draws the colored status dot used next to labels.
func levelDot(c *ui.Context, t *ui.Theme, l health.Level, size float32) {
	ui.Text(c, "●").FontSize(size).TextColor(levelColor(t, l))
}

// relTime renders a moment as "刚刚", "N 秒前" or an absolute time once it
// is old enough that a clock reading is clearer.
func relTime(now, t time.Time) string {
	if t.IsZero() {
		return "从未"
	}
	d := now.Sub(t)
	switch {
	case d < 0:
		return t.Format("15:04:05")
	case d < 5*time.Second:
		return "刚刚"
	case d < time.Minute:
		return fmt.Sprintf("%d 秒前", int(d.Seconds()))
	case d < time.Hour:
		return fmt.Sprintf("%d 分钟前", int(d.Minutes()))
	default:
		return t.Format("01-02 15:04")
	}
}

// clockTime is the absolute short form used in timelines.
func clockTime(t time.Time) string {
	if t.IsZero() {
		return "--:--:--"
	}
	return t.Format("15:04:05")
}

// eventLine renders one durable event as a timeline row.
func eventLine(ev health.Event) string {
	switch ev.Kind {
	case health.EventLevelChanged:
		return fmt.Sprintf("%s → %s：%s", levelWord(ev.From), levelWord(ev.To), ev.Reason)
	case health.EventRecoveryScheduled:
		return fmt.Sprintf("已安排恢复（第 %d 次）：%s", ev.Attempt, ev.Reason)
	case health.EventRecoveryStarted:
		return fmt.Sprintf("开始恢复（第 %d 次）", ev.Attempt)
	case health.EventRecoveryFinished:
		if ev.Detail != "" {
			return "恢复动作结束：" + ev.Detail
		}
		return "恢复动作结束"
	case health.EventRecoveryExhausted:
		return fmt.Sprintf("自动恢复次数用尽（%d 次），等待人工处理", ev.Attempt)
	default:
		return ev.Kind.String()
	}
}

// recoveryLabel describes the recovery pipeline state in Chinese.
func recoveryLabel(s health.RecoveryState) string {
	switch s {
	case health.RecoveryScheduled:
		return "已安排"
	case health.RecoveryRunning:
		return "执行中"
	case health.RecoveryExhausted:
		return "已用尽"
	default:
		return "空闲"
	}
}
