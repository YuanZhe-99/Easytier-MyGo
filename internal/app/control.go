package app

import (
	"context"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/egoist/mygo/ui"
)

// coreControl renders the network start/stop control. It follows the
// core's desired state: while the user wants it running the button
// stops it (with a confirmation), and while it is stopped the button
// starts it. In monitor mode nothing is shown because there is nothing
// to start or stop here; a read-only connection (a system socket seen by
// a user outside the authorized group) says so instead of offering
// buttons that would be refused.
func (a *App) coreControl(c *ui.Context, s snapshotCopy) {
	client := a.currentClient()
	if s.status.Daemon.Mode != "managed" {
		return
	}
	coreSt, ok := findComponent(s.status.Health, health.KindCore)
	if !ok {
		return
	}
	if s.status.Daemon.Access == "read" {
		ui.Button(c, "需要授权").Disabled(true)
		ui.Text(c, "只读访问：你不在授权组，不能更改网络状态").FontSize(11).
			TextColor(c.Theme().TextMuted)
		return
	}
	if coreSt.Desired == health.DesiredStop {
		ui.PrimaryButton(c, "启动网络").OnClick(func() {
			a.do(func(ctx context.Context) error {
				_, err := client.StartCore(ctx)
				return err
			})
		})
		return
	}
	ui.Button(c, "停止网络").OnClick(func() {
		a.confirmThen("停止网络？",
			"核心将停止，本机网络会断开。手动停止不会被自动恢复重新启动。",
			func(ctx context.Context) error {
				_, err := client.StopCore(ctx)
				return err
			})
	})
}
