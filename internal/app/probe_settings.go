package app

import (
	"context"
	"fmt"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/diagnostics"
	"github.com/egoist/mygo/ui"
	"strconv"
	"strings"
	"time"
)

func parseProbeSettings(enabled bool, interval, targets string) (diagnostics.Schedule, error) {
	seconds, err := strconv.Atoi(strings.TrimSpace(interval))
	if err != nil {
		return diagnostics.Schedule{}, fmt.Errorf("间隔须为秒数")
	}
	s := diagnostics.Schedule{Enabled: enabled, IntervalSeconds: seconds}
	for _, line := range strings.Split(targets, "\n") {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) != 2 {
			return s, fmt.Errorf("每行使用 tcp 主机:端口 或 dns 域名")
		}
		s.Targets = append(s.Targets, diagnostics.Target{Protocol: fields[0], Address: fields[1]})
	}
	return s, s.Validate()
}

func (a *App) loadProbeSettings() {
	if a.probeSettingsDirty() {
		a.probeSettingsMessage = "请先保存或放弃探测设置修改"
		return
	}
	client := a.currentClient()
	a.probeSettingsMessage = "正在读取探测设置…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		s, err := client.ProbeSettings(ctx)
		a.publishFor(client, func() {
			if err != nil {
				a.probeSettingsMessage = err.Error()
				return
			}
			a.probeEnabled = s.Enabled
			a.probeInterval = strconv.Itoa(s.IntervalSeconds)
			var lines []string
			for _, target := range s.Targets {
				lines = append(lines, target.Protocol+" "+target.Address)
			}
			a.probeTargets = strings.Join(lines, "\n")
			a.probeSettingsLoaded = true
			a.probeSettingsBase = a.probeSettingsText()
			a.probeSettingsMessage = "设置已读取"
		})
	}()
}

func (a *App) saveProbeSettings() {
	s, err := parseProbeSettings(a.probeEnabled, a.probeInterval, a.probeTargets)
	if err != nil {
		a.probeSettingsMessage = err.Error()
		return
	}
	client := a.currentClient()
	submitted := a.probeSettingsText()
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		err := client.SaveProbeSettings(ctx, s)
		a.publishFor(client, func() {
			if err != nil {
				a.probeSettingsMessage = err.Error()
			} else {
				a.probeSettingsBase = submitted
				a.probeSettingsMessage = "已保存并生效；网络运行不受影响"
			}
		})
	}()
}

func (a *App) probeSettingsText() string {
	return fmt.Sprintf("%t\n%s\n%s", a.probeEnabled, a.probeInterval, a.probeTargets)
}
func (a *App) probeSettingsDirty() bool {
	return a.probeSettingsLoaded && a.probeSettingsText() != a.probeSettingsBase
}

func (a *App) probeSettingsCard(c *ui.Context) {
	if a.readOnly() {
		return
	}
	ui.Column(c.Key("probe-settings")).FillWidth().Gap(6).Padding(12).Children(func() {
		ui.Text(c, "可选 TCP / DNS 探测").Bold()
		ui.Text(c, "默认关闭；结果仅说明宿主路由可达性，隧道路径未核实，不触发网络恢复。").FontSize(12)
		ui.Button(c, "读取探测设置").OnClick(a.loadProbeSettings)
		if a.probeSettingsLoaded {
			ui.Checkbox(c, &a.probeEnabled, "启用后台探测")
			ui.TextInput(c, &a.probeInterval).Label("探测间隔（30–3600 秒）")
			ui.Text(c, "每行：tcp 主机:端口 或 dns 域名，最多 8 个目标").FontSize(12)
			ui.TextArea(c, &a.probeTargets).Height(100).FillWidth()
			ui.Button(c, "保存探测设置").OnClick(a.saveProbeSettings)
			if a.probeSettingsDirty() {
				ui.Text(c, "探测设置未保存").TextColor(c.Theme().Warning)
				ui.Button(c, "放弃探测设置修改").OnClick(func() { a.probeSettingsLoaded = false; a.loadProbeSettings() })
			}
		}
		if a.probeSettingsMessage != "" {
			ui.Text(c, a.probeSettingsMessage).FontSize(12)
		}
	})
}
