package app

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/egoist/mygo"
	"github.com/egoist/mygo/ui"
)

const diagnosticSizeLimit = 256 * 1024

func encodeDiagnosticPreview(d *localapi.Diagnostics) ([]byte, error) {
	if d == nil {
		return nil, errors.New("管理服务未返回诊断数据")
	}
	b, err := json.MarshalIndent(d, "", "  ")
	if err != nil {
		return nil, err
	}
	if len(b) > diagnosticSizeLimit {
		return nil, errors.New("诊断数据超过 256 KiB 限制")
	}
	return append(b, '\n'), nil
}

func (a *App) loadDiagnosticPreview() {
	a.loadDiagnosticScope(false)
}

func (a *App) loadDiagnosticScope(all bool) {
	client := a.currentClient()
	if a.diagnosticLoading || a.diagnosticSaving {
		return
	}
	a.diagnosticLoading = true
	a.diagnosticPreview = nil
	a.diagnosticBuffer = nil
	a.diagnosticMessage = "正在获取诊断预览…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		var preview any
		var err error
		if all {
			preview, err = client.DiagnosticNetworks(ctx)
		} else {
			preview, err = client.Diagnostics(ctx)
		}
		var data []byte
		if err == nil {
			data, err = json.MarshalIndent(preview, "", "  ")
			if err == nil {
				data = append(data, '\n')
				if len(data) > diagnosticSizeLimit {
					err = errors.New("诊断数据超过 256 KiB 限制")
				}
			}
		}
		a.publishFor(client, func() {
			a.diagnosticLoading = false
			if err != nil {
				a.diagnosticMessage = "诊断获取失败：" + err.Error()
				return
			}
			a.diagnosticPreview = data
			a.diagnosticBuffer = ui.NewTextBuffer(string(data))
			a.diagnosticMessage = "预览已生成，保存时使用这份数据；刷新可重新获取。"
		})
	}()
}

// writeDiagnosticFile atomically replaces the selected file with restricted permissions.
func writeDiagnosticFile(path string, data []byte) error {
	if len(data) == 0 || len(data) > diagnosticSizeLimit {
		return errors.New("invalid diagnostic export size")
	}
	f, err := os.CreateTemp(filepath.Dir(path), ".diagnostic-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err = f.Chmod(0o600); err == nil {
		_, err = f.Write(data)
	}
	if err == nil {
		err = f.Sync()
	}
	closeErr := f.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	return os.Rename(f.Name(), path)
}

func (a *App) saveDiagnosticPreview() {
	if len(a.diagnosticPreview) == 0 || a.diagnosticLoading || a.diagnosticSaving {
		return
	}
	data := append([]byte(nil), a.diagnosticPreview...)
	a.diagnosticSaving = true
	a.mu.Lock()
	w := a.window
	a.mu.Unlock()
	go func() {
		path, err := mygo.Dialog.Save(mygo.SaveDialogOptions{Parent: w, Title: "保存诊断预览", DefaultPath: "easytier-mygo-diagnostics.json", Filters: []mygo.FileFilter{{Name: "JSON 诊断", Extensions: []string{"json"}}}})
		if err == nil && path != "" {
			if !strings.HasSuffix(strings.ToLower(path), ".json") {
				path += ".json"
			}
			err = writeDiagnosticFile(path, data)
		}
		a.publish(func() {
			a.diagnosticSaving = false
			switch {
			case err != nil:
				a.diagnosticMessage = "诊断保存失败：" + err.Error()
			case path != "":
				a.diagnosticMessage = "已保存诊断预览：" + path
			default:
				a.diagnosticMessage = "已取消保存，预览仍可查看。"
			}
		})
	}()
}

func (a *App) diagnosticCard(c *ui.Context) {
	a.probeSettingsCard(c)
	a.historyCard(c)
	t := c.Theme()
	ui.Column(c.Key("diagnostics")).Background(t.Surface).Radius(8).Padding(12).Gap(6).FillWidth().Children(func() {
		ui.Text(c, "诊断预览与导出").Bold()
		ui.Text(c, "包含版本、模式、策略、采样时间、健康状态和事件类型；不包含配置原文、凭据、路径、节点名称或核心日志。").FontSize(12).TextColor(t.TextMuted)
		ui.Row(c).Gap(8).Children(func() {
			ui.Button(c, "获取诊断预览").Disabled(a.diagnosticLoading || a.diagnosticSaving).OnClick(a.loadDiagnosticPreview)
			ui.Button(c, "预览全部网络诊断").Disabled(a.diagnosticLoading || a.diagnosticSaving).OnClick(func() { a.loadDiagnosticScope(true) })
			ui.Button(c, "保存预览…").Disabled(len(a.diagnosticPreview) == 0 || a.diagnosticLoading || a.diagnosticSaving).OnClick(a.saveDiagnosticPreview)
		})
		if a.diagnosticMessage != "" {
			ui.Text(c, a.diagnosticMessage).FontSize(12).TextColor(t.TextMuted)
		}
		if len(a.diagnosticPreview) > 0 {
			c.Key("diagnostic-preview")
			ui.TextAreaBuffer(c, a.diagnosticBuffer).ReadOnly(true).Font("monospace").Height(180).FillWidth()
		}
	})
}
