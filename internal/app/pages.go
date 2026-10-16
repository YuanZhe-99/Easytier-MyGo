package app

import (
	"context"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/coreadapter"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/health"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/egoist/mygo/ui"
)

func (a *App) overview(c *ui.Context) {
	s := a.snapshotCopy()
	t := c.Theme()
	contentColumn(c).Children(func() {
		sectionTitle(c, "总览", "")
		if !s.haveStatus {
			ui.Text(c, "尚未收到管理服务的状态。").TextColor(t.TextMuted)
			return
		}

		agg := aggregate(s.status.Health)
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			levelDot(c, t, agg, 16)
			ui.Text(c, levelWord(agg)).FontSize(20).Bold().TextColor(levelColor(t, agg))
			ui.Text(c, "最近检查 "+relTime(s.now, s.status.CheckedAt)).FontSize(12).TextColor(t.TextMuted)
			if !s.live {
				ui.Text(c, "（数据可能已过期）").FontSize(12).TextColor(t.Warning)
			}
			ui.Spacer(c)
			a.coreControl(c, s)
		})

		if len(s.status.Health) > 0 {
			ui.Row(c).Gap(12).Children(func() {
				for _, st := range s.status.Health {
					a.componentCard(c, st, s.now)
				}
			})
		}

		if n := s.status.Node; n != nil {
			ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(6).FillWidth().Children(func() {
				ui.Text(c, "网络信息").Bold()
				ui.Text(c, sampleLabel(s.status, "核心", s.status.NodeSampledAt, s.now, 1)).FontSize(11).TextColor(t.TextMuted)
				ui.Text(c, sampleLabel(s.status, "对端", s.status.PeerSampledAt, s.now, 2)).FontSize(11).TextColor(t.TextMuted)
				ui.Row(c).Gap(24).Children(func() {
					a.kv(c, "主机名", n.Hostname)
					a.kv(c, "虚拟地址", orDash(n.IPv4Addr))
					a.kv(c, "核心版本", n.Version)
					a.kv(c, "节点数", peerCountLabel(s.status, s.now))
					a.kv(c, "监听器", itoa(len(n.Listeners))+" 个")
				})
			})
		}

		a.trafficCard(c, s)

		ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(6).FillWidth().Children(func() {
			ui.Row(c).AlignItems(ui.Center).Children(func() {
				ui.Text(c, "最近事件").Bold()
				ui.Spacer(c)
				ui.Link(c, "查看全部", "/health")
			})
			events := tail(s.status.RecentEvents, 5)
			if len(events) == 0 {
				ui.Text(c, "暂无事件。核心与网络平稳运行时不会写事件。").FontSize(12).TextColor(t.TextMuted)
				return
			}
			for _, ev := range events {
				ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
					levelDot(c, t, ev.To, 9)
					ui.Text(c, clockTime(ev.Time)+"  "+componentLabel(ev.Component)+"  "+eventLine(ev)).
						FontSize(12).SingleLine()
				})
			}
		})
	})
}

func (a *App) componentCard(c *ui.Context, st health.State, now time.Time) {
	t := c.Theme()
	ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).MinWidth(210).Children(func() {
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			levelDot(c, t, st.Level, 10)
			ui.Text(c, componentLabel(st.Component)).Bold()
			ui.Text(c, levelLabel(st.Level)).Bold().TextColor(levelColor(t, st.Level))
		})
		ui.Text(c, "期望："+desiredWord(st.Desired)+" · 实际："+lifecycleWord(st.Lifecycle)).
			FontSize(12).TextColor(t.TextMuted)
		if st.Reason != "" {
			ui.Text(c, "原因："+st.Reason).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		ui.Text(c, "最近检查 "+relTime(now, st.LastCheck)).FontSize(12).TextColor(t.TextMuted)
	})
}

func (a *App) nodes(c *ui.Context) {
	s := a.snapshotCopy()
	t := c.Theme()
	contentColumn(c).Children(func() {
		sectionTitle(c, "节点与路由", "数据来自 easytier-cli 的 peer 输出")
		if s.haveStatus {
			ui.Text(c, sampleLabel(s.status, "对端", s.status.PeerSampledAt, s.now, 2)).FontSize(12).TextColor(t.TextMuted)
			if s.status.PeerCollectorError != "" {
				ui.Text(c, "对端采集失败，列表为缓存："+s.status.PeerCollectorError).FontSize(12).TextColor(t.Warning)
			}
			if s.status.CollectorError != "" {
				ui.Text(c, "采集异常："+s.status.CollectorError).FontSize(12).TextColor(t.Warning)
			}
		}
		if !s.haveStatus || len(s.status.Peers) == 0 {
			ui.Text(c, "暂无节点数据。").TextColor(t.TextMuted)
			return
		}
		peers := s.status.Peers
		cols := []ui.TableColumn{
			{Title: "主机"},
			{Title: "虚拟 IP", Width: 130},
			{Title: "连接", Width: 80},
			{Title: "延迟", Width: 80, Align: ui.End},
			{Title: "丢包", Width: 80, Align: ui.End},
			{Title: "隧道", Width: 70},
			{Title: "NAT", Width: 110},
			{Title: "版本", Width: 130},
			{Title: "接收", Width: 90, Align: ui.End},
			{Title: "发送", Width: 90, Align: ui.End},
		}
		ui.Table(c, &a.peersState, cols, len(peers), func(row, col int) {
			p := peers[row]
			switch col {
			case 0:
				ui.Text(c, p.Hostname).SingleLine()
			case 1:
				ui.Text(c, orDash(p.IPv4)).SingleLine()
			case 2:
				ui.Text(c, p.Cost)
			case 3:
				ui.Text(c, p.LatencyMS)
			case 4:
				ui.Text(c, p.LossRate)
			case 5:
				ui.Text(c, p.TunnelProto)
			case 6:
				ui.Text(c, orDash(p.NatType)).SingleLine()
			case 7:
				ui.Text(c, orDash(p.Version)).SingleLine()
			case 8:
				ui.Text(c, p.RxBytes)
			case 9:
				ui.Text(c, p.TxBytes)
			}
		}).Grow(1)
		ui.Text(c, "“延迟”“丢包”来自 EasyTier 的探测统计，不代表应用流量的实际体验。").
			FontSize(12).TextColor(t.TextMuted)
	})
}

func (a *App) healthPage(c *ui.Context) {
	s := a.snapshotCopy()
	t := c.Theme()
	contentColumn(c).Children(func() {
		sectionTitle(c, "服务与健康", "检测在后台持续运行；关闭界面不影响它")

		// Freshness of the whole picture, before any per-component claim.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			switch {
			case !s.haveStatus:
				levelDot(c, t, health.LevelUnknown, 10)
				ui.Text(c, "尚无数据").FontSize(12).TextColor(t.TextMuted)
			case s.live:
				levelDot(c, t, health.LevelHealthy, 10)
				ui.Text(c, "监控在线，每 "+intervalWord(s.status.Daemon.CheckIntervalMS)+" 一轮，最近检查 "+
					relTime(s.now, s.status.CheckedAt)).FontSize(12).TextColor(t.TextMuted)
			default:
				levelDot(c, t, health.LevelUnknown, 10)
				ui.Text(c, "监控数据过期："+relTime(s.now, s.statusAt)+"未收到更新").
					FontSize(12).TextColor(t.Warning)
			}
		})

		// Controls act on the daemon's state; every one is a bounded,
		// named operation, no free-form input.
		ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
			ui.Button(c, "立即检查").OnClick(func() {
				client := a.currentClient()
				a.do(func(ctx context.Context) error {
					_, err := client.Check(ctx)
					return err
				})
			})
			ui.Button(c, "重新授权自动恢复").OnClick(func() {
				client := a.currentClient()
				states := s.status.Health
				a.do(func(ctx context.Context) error {
					for _, st := range states {
						if _, err := client.GrantRetry(ctx, st.Component.String()); err != nil {
							return err
						}
					}
					return nil
				})
			})
			a.coreControl(c, s)
			if s.actionErr != "" {
				ui.Text(c, "上一操作失败："+s.actionErr).FontSize(12).TextColor(t.Danger).SingleLine()
			}
		})

		if !s.haveStatus {
			ui.Text(c, "尚未收到管理服务的状态。").TextColor(t.TextMuted)
			return
		}

		ui.Text(c, "组件").Bold()
		if len(s.status.Health) == 0 {
			ui.Text(c, "还没有观测记录。").TextColor(t.TextMuted)
		}
		for _, st := range s.status.Health {
			a.healthRow(c, st, s.now)
		}

		ui.Row(c).AlignItems(ui.Center).Gap(8).Children(func() {
			ui.Text(c, "事件时间线").Bold()
			ui.Text(c, "（由后台持久化，界面关闭期间的事件也在）").FontSize(12).TextColor(t.TextMuted)
		})
		events := s.status.RecentEvents
		if len(events) == 0 {
			ui.Text(c, "暂无事件。").FontSize(12).TextColor(t.TextMuted)
			return
		}
		ui.List(c, &a.eventsState, len(events), func(i int) {
			ev := events[i]
			ui.Row(c).Gap(6).AlignItems(ui.Center).Children(func() {
				levelDot(c, t, ev.To, 8)
				row := clockTime(ev.Time) + "  " + componentLabel(ev.Component) + "  " + eventLine(ev)
				if ev.Detail != "" {
					row += "（" + ev.Detail + "）"
				}
				ui.Text(c, row).FontSize(12).SingleLine()
			})
		}).Grow(1).ClipX()
	})
}

func (a *App) healthRow(c *ui.Context, st health.State, now time.Time) {
	t := c.Theme()
	ui.Column(c).Background(t.Surface).Radius(8).Padding(10).Gap(3).FillWidth().Children(func() {
		ui.Row(c).Gap(10).AlignItems(ui.Center).Children(func() {
			levelDot(c, t, st.Level, 10)
			ui.Text(c, componentLabel(st.Component)).Bold()
			ui.Text(c, levelWord(st.Level)).TextColor(levelColor(t, st.Level)).Bold().FontSize(13)
			ui.Text(c, "期望 "+desiredWord(st.Desired)+" · 实际 "+lifecycleWord(st.Lifecycle)).
				FontSize(12).TextColor(t.TextMuted)
		})
		if st.Reason != "" {
			ui.Text(c, "原因："+st.Reason).FontSize(12).TextColor(t.TextMuted)
		}
		if st.Detail != "" {
			ui.Text(c, "细节："+st.Detail).FontSize(12).TextColor(t.TextMuted).SingleLine()
		}
		if st.Level == health.LevelFailed || st.Recovery != health.RecoveryIdle || st.AttemptsInWindow > 0 {
			ui.Text(c, "恢复："+recoveryWord(st)).FontSize(12).TextColor(t.TextMuted)
		}
		ui.Text(c, "最近检查 "+relTime(now, st.LastCheck)+" · 最近正常 "+relTime(now, st.LastOK)).
			FontSize(12).TextColor(t.TextMuted)
	})
}

func (a *App) settingsPage(c *ui.Context) {
	s := a.snapshotCopy()
	t := c.Theme()
	contentColumn(c).Children(func() {
		sectionTitle(c, "设置", "策略编辑与登录启动在后续版本提供")
		if !s.haveStatus {
			ui.Text(c, "尚未收到管理服务的状态。").TextColor(t.TextMuted)
			return
		}
		d := s.status.Daemon
		ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).FillWidth().Children(func() {
			ui.Text(c, "核心日志启动选项").Bold()
			level := func(value string) string {
				if value == "" {
					return "核心默认"
				}
				return value
			}
			limit := func(value int) string {
				if value == 0 {
					return "核心默认"
				}
				return itoa(value)
			}
			ui.Text(c, "控制台："+level(d.Logging.ConsoleLevel)+" · 轮转文件："+level(d.Logging.FileLevel)).FontSize(12)
			ui.Text(c, "每文件 MiB："+limit(d.Logging.FileSizeMB)+" · 文件数量："+limit(d.Logging.FileCount)).FontSize(12)
			ui.Text(c, "保存后重启管理服务生效；网络 TOML 不配置日志。日志仅保存在本地，不进入诊断导出。").FontSize(11).TextColor(t.TextMuted)
			a.loggingEditor(c, d.Mode == "managed")
		})
		a.diagnosticCard(c)
		if len(s.status.ProbeResults) > 0 {
			ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).FillWidth().Children(func() {
				ui.Text(c, "可选目标探测").Bold()
				ui.Text(c, "仅说明目标可达性；隧道路径未核实，失败不触发核心恢复。").FontSize(12).TextColor(t.TextMuted)
				for _, result := range s.status.ProbeResults {
					verdict := "目标不可达：" + result.Failure
					if result.Reachable {
						verdict = "目标可达"
					}
					ui.Text(c, result.Target.Protocol+" "+result.Target.Address+" · "+verdict+" · "+relTime(s.now, result.StartedAt)).FontSize(12).TextColor(t.TextMuted)
				}
			})
		}
		ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).FillWidth().Children(func() {
			ui.Text(c, "管理服务").Bold()
			ui.Row(c).Gap(24).Children(func() {
				a.kv(c, "版本", d.Version)
				a.kv(c, "模式", modeWord(d.Mode))
				a.kv(c, "运行方式", serviceManagedWord(d.ServiceManaged))
				a.kv(c, "检查间隔", itoa(d.CheckIntervalMS/1000)+" 秒")
				a.kv(c, "核心版本", orDash(d.CoreVersion))
			})
			ui.Text(c, "套接字："+d.SocketPath).FontSize(12).TextColor(t.TextMuted)
		})

		ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).FillWidth().Children(func() {
			ui.Text(c, "开机运行与系统服务").Bold()
			// What the active config needs: user mode (no TUN) needs no
			// elevated rights; a TUN core needs the system scope.
			needsRoot := d.CoreNeedsRoot != nil && *d.CoreNeedsRoot
			userMode := d.CoreUserMode != nil && *d.CoreUserMode
			switch {
			case d.CoreUserMode == nil:
				// Unknown (no active config, or it could not be read).
			case userMode:
				ui.Text(c, "当前配置以用户态运行（no_tun：不创建 TUN 设备），无需提权；用户级服务即可。").
					FontSize(12).TextColor(t.Success)
			default:
				ui.Text(c, "当前配置需要 TUN 设备（no_tun 未开启），必须以系统级服务运行，安装需要 sudo。").
					FontSize(12).TextColor(t.Warning)
			}
			if d.ServiceManaged {
				ui.Text(c, "管理服务由系统服务管理器启动；核心与网络的检测、恢复不依赖是否打开界面。").
					FontSize(12).TextColor(t.TextMuted)
				ui.Text(c, "服务管理器还监视界面之外的守护进程：检查轮次停止推进超过设定的看门狗时间后，"+
					"它会被自动重启（恢复预算跨重启保留）。").FontSize(12).TextColor(t.TextMuted)
			} else {
				ui.Text(c, "当前管理服务是手动启动的。安装为系统服务后可在登录/开机时自动运行：").
					FontSize(12).TextColor(t.TextMuted)
			}
			cmd := installCommand(needsRoot)
			ui.Text(c, cmd).FontSize(12).TextColor(t.TextMuted)
			ui.Row(c).Gap(8).AlignItems(ui.Center).Children(func() {
				ui.Button(c, "复制安装命令").OnClick(func() {
					clipboardWrite(cmd)
				})
				if userMode {
					ui.Text(c, "用户级服务在登录时启动；开机即用需 sudo loginctl enable-linger $USER").
						FontSize(12).TextColor(t.TextMuted)
				} else {
					ui.Text(c, "系统级服务在开机时启动，无需登录；它还需要 --socket/--state-dir（例如 /run/easytier-mygo/ctl.sock、/var/lib/easytier-mygo）。").
						FontSize(12).TextColor(t.TextMuted)
				}
			})
		})

		p := d.Policy
		ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(4).FillWidth().Children(func() {
			ui.Text(c, "健康与恢复策略").Bold()
			ui.Row(c).Gap(24).Children(func() {
				a.kv(c, "失败确认", itoa(p.FailThreshold)+" 次")
				a.kv(c, "恢复确认", itoa(p.OKThreshold)+" 次")
				a.kv(c, "启动宽限", itoa(p.GraceSeconds)+" 秒")
				a.kv(c, "自动恢复", boolWord(p.RecoveryEnabled))
			})
			ui.Row(c).Gap(24).Children(func() {
				a.kv(c, "退避", itoa(p.BackoffBaseMS/1000)+"–"+itoa(p.BackoffMaxMS/1000)+" 秒")
				a.kv(c, "尝试上限", itoa(p.MaxAttempts)+" 次 / "+itoa(p.WindowSeconds/60)+" 分钟")
			})
			ui.Text(c, "数值为规划默认值，将随实测调整；“仅监控”模式关闭自动恢复。").
				FontSize(12).TextColor(t.TextMuted)
		})

		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "停止管理服务（开发用）").OnClick(func() {
				a.confirmThen("停止管理服务？",
					"停止后核心与监控也会退出，直到下次启动管理服务。",
					func(ctx context.Context) error { return a.client.Shutdown(ctx) })
			})
		})
	})
}

// kv renders one label and value pair.
func (a *App) kv(c *ui.Context, label, value string) {
	t := c.Theme()
	ui.Column(c).Gap(2).Children(func() {
		ui.Text(c, label).FontSize(11).TextColor(t.TextMuted)
		ui.Text(c, value).FontSize(13).SingleLine()
	})
}

// installCommand builds the copyable install command for the recommended
// scope: a user-mode config gets a user service (no root), a TUN config a
// system service.
func installCommand(systemScope bool) string {
	if !systemScope {
		return "easytier-mygo-svc install --scope user \\\n" +
			"  --core-bin /绝对路径/easytier-core --cli-bin /绝对路径/easytier-cli \\\n" +
			"  --config /绝对路径/网络.toml"
	}
	return "sudo easytier-mygo-svc install --scope system \\\n" +
		"  --core-bin /绝对路径/easytier-core --cli-bin /绝对路径/easytier-cli \\\n" +
		"  --config /绝对路径/网络.toml \\\n" +
		"  --socket /run/easytier-mygo/ctl.sock --state-dir /var/lib/easytier-mygo"
}

// trafficCard shows the instance's receive/send rate curve, or explains
// why there is none yet. Gaps and partial intervals are called out, so a
// break is never mistaken for a quiet network.
func (a *App) trafficCard(c *ui.Context, s snapshotCopy) {
	t := c.Theme()
	pts := s.status.Traffic
	ui.Column(c).Background(t.Surface).Radius(8).Padding(12).Gap(8).FillWidth().Children(func() {
		ui.Row(c).AlignItems(ui.Center).Gap(12).Children(func() {
			ui.Text(c, "流量速率").Bold()
			rxNow, txNow, peak, current, measured := trafficSummary(pts)
			switch {
			case !measured && len(pts) > 0:
				// Intervals exist but none was measured: say so instead of
				// printing "0 B/s", which would read as a measured zero.
				ui.Text(c, "尚无实测值").FontSize(12).TextColor(t.TextMuted)
			case measured:
				if current && s.live && sampleFresh(s.status, s.status.TrafficSampledAt, s.now, 6) {
					ui.Text(c, "接收 "+humanRate(rxNow)).FontSize(12).TextColor(t.Accent)
					ui.Text(c, "发送 "+humanRate(txNow)).FontSize(12).TextColor(t.Success)
				} else {
					// The latest interval was a gap: the last measurement
					// is not "now".
					ui.Text(c, "接收 ——").FontSize(12).TextColor(t.TextMuted)
					ui.Text(c, "发送 ——").FontSize(12).TextColor(t.TextMuted)
				}
				ui.Text(c, "峰值 "+humanRate(peak)).FontSize(12).TextColor(t.TextMuted)
				if w := trafficWindow(pts); w > 0 {
					ui.Text(c, "窗口 "+itoa(int(w.Seconds()))+" 秒").FontSize(12).TextColor(t.TextMuted)
				}
			}
			ui.Spacer(c)
			// Distinguish "waiting for data" from "measured zero", name
			// the actual reason when nothing can be measured, and never
			// leave the last state looking current when the daemon
			// stopped answering.
			switch {
			case !s.live || (!s.status.TrafficSampledAt.IsZero() && !sampleFresh(s.status, s.status.TrafficSampledAt, s.now, 6)):
				ui.Text(c, "数据过期").FontSize(11).TextColor(t.Warning)
			case len(pts) == 0:
				ui.Text(c, "等待采样…").FontSize(11).TextColor(t.TextMuted)
			case s.status.PeerCollectorError != "" || !sampleFresh(s.status, s.status.PeerSampledAt, s.now, 2):
				ui.Text(c, "对端状态未知").FontSize(11).TextColor(t.Warning)
			case !hasRemotePeers(s.status.Peers):
				// No connections is a reason, not a collector fault: say
				// which one it is.
				ui.Text(c, "暂无对端连接").FontSize(11).TextColor(t.TextMuted)
			default:
				if gaps := countGaps(pts); gaps > 0 {
					ui.Text(c, itoa(gaps)+" 个区间缺测").FontSize(11).TextColor(t.Warning)
				}
			}
		})
		ui.Text(c, sampleLabel(s.status, "流量", s.status.TrafficSampledAt, s.now, 6)).FontSize(11).TextColor(t.TextMuted)

		if len(pts) < 2 {
			ui.Text(c, "核心连接计数需要两次成功采样才能算出速率；后台按显示需求分层采样，缺测不填零。曲线只保存在后台内存中。").
				FontSize(12).TextColor(t.TextMuted)
			return
		}
		trafficChart(c, pts, 96)
		ui.Text(c, "口径：EasyTier 逐连接隧道计数（rx/tx 字节）按实际间隔差分；"+
			"连接重建或计数回退会标记缺测，不画尖峰。").FontSize(11).TextColor(t.TextMuted)
	})
}

// countGaps counts the gap intervals in a curve.
func countGaps(points []localapi.TrafficPoint) int {
	n := 0
	for _, pt := range points {
		if pt.Gap {
			n++
		}
	}
	return n
}

// hasRemotePeers reports whether any connected peer is not this core
// itself: without one there can be no tunnel traffic to measure.
func hasRemotePeers(peers []coreadapter.Peer) bool {
	for _, p := range peers {
		if !p.IsLocal() {
			return true
		}
	}
	return false
}

// pollSeconds is how long the daemon takes to produce a second sample.
func (a *App) pollSeconds() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.haveStatus && a.status.Daemon.CheckIntervalMS > 0 {
		s := a.status.Daemon.CheckIntervalMS / 1000
		if s < 1 {
			s = 1
		}
		return s
	}
	return 1
}

func findComponent(states []health.State, kind health.Kind) (health.State, bool) {
	for _, st := range states {
		if st.Component.Kind == kind {
			return st, true
		}
	}
	return health.State{}, false
}

func tail(events []health.Event, n int) []health.Event {
	if n > len(events) {
		n = len(events)
	}
	return events[len(events)-n:]
}

func orDash(s string) string {
	if s == "" {
		return "—"
	}
	return s
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	neg := n < 0
	if neg {
		n = -n
	}
	var buf [20]byte
	i := len(buf)
	for n > 0 {
		i--
		buf[i] = byte('0' + n%10)
		n /= 10
	}
	if neg {
		i--
		buf[i] = '-'
	}
	return string(buf[i:])
}

func desiredWord(d health.Desired) string {
	if d == health.DesiredStop {
		return "停止"
	}
	return "运行"
}

func lifecycleWord(l health.Lifecycle) string {
	switch l {
	case health.LifecycleStarting:
		return "启动中"
	case health.LifecycleRunning:
		return "运行中"
	case health.LifecycleStopping:
		return "停止中"
	case health.LifecycleStopped:
		return "已停止"
	default:
		return "未知"
	}
}

func modeWord(mode string) string {
	switch mode {
	case "managed":
		return "托管运行"
	case "monitor":
		return "仅监控"
	default:
		return mode
	}
}

func serviceManagedWord(managed bool) string {
	if managed {
		return "系统服务"
	}
	return "手动启动"
}

func boolWord(b bool) string {
	if b {
		return "已启用"
	}
	return "已关闭"
}

// intervalWord renders the check cadence.
func intervalWord(ms int) string {
	if ms <= 0 {
		return "数秒"
	}
	seconds := ms / 1000
	if seconds < 1 {
		return itoa(ms) + " 毫秒"
	}
	return itoa(seconds) + " 秒"
}

// recoveryWord summarizes the recovery pipeline of a state.
func recoveryWord(st health.State) string {
	word := recoveryLabel(st.Recovery) + " · 近期尝试 " + itoa(st.AttemptsInWindow) + " 次"
	if !st.NextRetry.IsZero() {
		word += " · 下次 " + clockTime(st.NextRetry)
	}
	return word
}
