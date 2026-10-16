package app

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/egoist/mygo/ui"
)

func parseLoggingDraft(console, file, size, count string) (localapi.CoreLogging, error) {
	v := localapi.CoreLogging{ConsoleLevel: console, FileLevel: file}
	for _, level := range []string{console, file} {
		switch level {
		case "", "off", "error", "warn", "info", "debug", "trace":
		default:
			return v, errors.New("日志级别须为空或 off/error/warn/info/debug/trace")
		}
	}
	var err error
	if size != "" {
		v.FileSizeMB, err = strconv.Atoi(size)
		if err != nil {
			return v, errors.New("文件大小须为整数")
		}
	}
	if count != "" {
		v.FileCount, err = strconv.Atoi(count)
		if err != nil {
			return v, errors.New("文件数量须为整数")
		}
	}
	if v.FileSizeMB < 0 || v.FileSizeMB > 100 || v.FileCount < 0 || v.FileCount > 10 {
		return v, errors.New("大小范围 0–100 MiB，数量范围 0–10；0 使用核心默认")
	}
	return v, nil
}

func (a *App) loggingDirty() bool {
	if !a.loggingLoaded {
		return false
	}
	v, err := parseLoggingDraft(a.loggingConsole, a.loggingFile, a.loggingSize, a.loggingCount)
	return err != nil || v != a.loggingBase
}

func (a *App) loggingEditor(c *ui.Context, managed bool) {
	if managed {
		c.Key("logging-check")
		ui.Button(c, "检查已加载的文件日志路径").Disabled(a.loggingBusy || a.readOnly()).OnClick(a.requestLoggingCheck)
	}
	if a.loggingCheck != nil {
		ui.Text(c, loggingCheckText(a.loggingCheck)).FontSize(11)
	}
	c.Key("logging-load")
	ui.Button(c, "读取已保存日志设置").Disabled(a.loggingBusy).OnClick(func() { a.requestLogging(false) })
	if a.loggingLoaded && managed {
		ui.Text(c, "空级别或 0 使用核心默认；级别 off/error/warn/info/debug/trace").FontSize(11)
		ui.Row(c).Gap(8).Children(func() {
			c.Key("logging-console")
			ui.TextInput(c, &a.loggingConsole).Label("控制台级别").Width(140).Disabled(a.loggingBusy || a.readOnly())
			c.Key("logging-file")
			ui.TextInput(c, &a.loggingFile).Label("文件级别").Width(140).Disabled(a.loggingBusy || a.readOnly())
			c.Key("logging-size")
			ui.TextInput(c, &a.loggingSize).Label("每文件 MiB").Width(110).Disabled(a.loggingBusy || a.readOnly())
			c.Key("logging-count")
			ui.TextInput(c, &a.loggingCount).Label("文件数量").Width(110).Disabled(a.loggingBusy || a.readOnly())
		})
		c.Key("logging-save")
		ui.Button(c, "保存日志设置（重启服务后生效）").Disabled(a.loggingBusy || a.readOnly()).OnClick(func() { a.requestLogging(true) })
	}
	if a.loggingMessage != "" {
		ui.Text(c, a.loggingMessage).FontSize(11)
	}
}

func loggingCheckText(check *localapi.LoggingSinkCheck) string {
	return "日志路径检查（" + check.CheckedAt.Local().Format("2006-01-02 15:04:05") + "）：" + check.State + " / " + check.Reason + "；仅该时刻证据，不代表日志送达或当前可写，不会触发核心恢复"
}

func (a *App) requestLoggingCheck() {
	if a.loggingBusy || a.readOnly() {
		return
	}
	client := a.currentClient()
	a.loggingBusy = true
	a.loggingCheck = nil
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		check, err := client.CheckLoggingSink(ctx)
		a.publishFor(client, func() {
			a.loggingBusy = false
			if err != nil {
				a.loggingMessage = "日志路径检查失败：" + err.Error()
				return
			}
			a.loggingCheck = check
		})
	}()
}

func (a *App) requestLogging(save bool) {
	if a.loggingBusy || (save && a.readOnly()) {
		return
	}
	var value *localapi.CoreLogging
	if save {
		parsed, err := parseLoggingDraft(a.loggingConsole, a.loggingFile, a.loggingSize, a.loggingCount)
		if err != nil {
			a.loggingMessage = err.Error()
			return
		}
		value = &parsed
	}
	client := a.currentClient()
	a.loggingBusy = true
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		settings, pending, err := client.LoggingSettings(ctx, value)
		a.publishFor(client, func() {
			a.loggingBusy = false
			if err != nil {
				a.loggingMessage = "日志设置失败：" + err.Error()
				return
			}
			a.loggingLoaded = true
			a.loggingBase = *settings
			a.loggingConsole = settings.ConsoleLevel
			a.loggingFile = settings.FileLevel
			a.loggingSize = strconv.Itoa(settings.FileSizeMB)
			a.loggingCount = strconv.Itoa(settings.FileCount)
			a.loggingMessage = "已读取保存值"
			if save {
				a.loggingMessage = "已保存"
			}
			if pending {
				a.loggingMessage += "；重启管理服务后生效"
			}
		})
	}()
}
