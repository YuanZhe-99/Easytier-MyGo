package app

import (
	"context"
	"errors"
	"fmt"
	"sort"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/egoist/mygo/ui"
)

func networkIDs(states map[string]*localapi.Status) []string {
	ids := make([]string, 0, len(states))
	for id := range states {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

func (a *App) currentClientLocked() *localapi.Client {
	if a.profileID == "" {
		return a.client
	}
	if a.profileClients == nil {
		a.profileClients = make(map[string]*localapi.Client)
	}
	client := a.profileClients[a.profileID]
	if client == nil {
		client = a.client.ForProfile(a.profileID)
		a.profileClients[a.profileID] = client
	}
	return client
}

func (a *App) currentClient() *localapi.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.currentClientLocked()
}

func (a *App) clientSelected(client *localapi.Client) bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return client == a.currentClientLocked()
}

func (a *App) publishFor(client *localapi.Client, fn func()) {
	a.publish(func() {
		if a.clientSelected(client) {
			fn()
		}
	})
}

// Switching selection changes neither intent nor runtime. Unsaved work requires
// an explicit discard before clearing the editor and diagnostic evidence.
func (a *App) selectNetwork(id string) {
	a.mu.Lock()
	_, known := a.networks[id]
	same := id == a.profileID
	a.mu.Unlock()
	if !known || same {
		return
	}
	if a.diagnosticSaving {
		a.configMsg = "诊断保存完成后再切换网络"
		a.configMsgErr = true
		return
	}
	if a.loggingBusy || a.loggingDirty() {
		a.configMsg = "请先保存或重新读取日志设置，再切换网络"
		a.configMsgErr = true
		return
	}
	if a.hasUnsaved() {
		a.configMsg = "请先保存或放弃修改，再切换网络"
		a.configMsgErr = true
		return
	}
	a.mu.Lock()
	old := a.currentClientLocked()
	old.SetWantDetails(false)
	old.SetWantTraffic(false)
	a.profileID = id
	// Returning to a profile starts a fresh view generation. Existing work
	// retains the old immutable client and must not publish into the new view.
	a.profileClients[id] = a.client.ForProfile(id)
	a.haveStatus = false
	a.status = localapi.Status{}
	a.statusAt = time.Time{}
	a.connErr = ""
	a.actionErr = ""
	a.mu.Unlock()
	a.editorBuf = nil
	a.editorName = ""
	a.editorRev = ""
	a.editorDirty = false
	a.editorLoading = false
	a.configSelectPending = ""
	a.configMsg = ""
	a.populateForm("")
	a.diagnosticPreview = nil
	a.diagnosticBuffer = nil
	a.diagnosticLoading = false
	a.diagnosticMessage = ""
	a.probeSettingsLoaded = false
	a.probeSettingsMessage = ""
	a.historyPreview = nil
	a.historyMessage = ""
	a.profileNameDraft = ""
	a.profileRenameBusy = false
	a.profileRenameMessage = ""
	a.loggingLoaded = false
	a.loggingBusy = false
	a.loggingMessage = ""
	a.loggingCheck = nil
	a.loggingConsole = ""
	a.loggingFile = ""
	a.loggingSize = ""
	a.loggingCount = ""
	a.triggerPoll()
}

func (a *App) networkSelector(c *ui.Context) {
	a.mu.Lock()
	selected := a.profileID
	states := a.networks
	networksAt := a.networksAt
	a.mu.Unlock()
	if len(states) == 0 {
		return
	}
	ui.Row(c).Padding(6, 12).Gap(8).Children(func() {
		ui.Text(c, "网络目标").Bold()
		for _, id := range networkIDs(states) {
			label := id
			if state := states[id]; state != nil && state.ActiveConfig != "" {
				label = state.ActiveConfig + " · " + id[:min(8, len(id))]
			}
			if state := states[id]; state != nil && state.ProfileName != "" {
				label = state.ProfileName + " · " + id[:min(8, len(id))]
			}
			c.Key("network-target-" + id)
			ui.Button(c, label).Disabled(id == selected).OnClick(func() { a.selectNetwork(id) })
			if state := states[id]; state != nil {
				if time.Since(networksAt) > staleAfter || !checkEvidenceFresh(*state, time.Now()) {
					ui.Text(c, "检查过期").TextColor(c.Theme().Warning)
				} else {
					ui.Text(c, levelWord(aggregate(state.Health))).FontSize(11)
				}
			}
		}
		if a.hasUnsaved() {
			ui.Text(c, "有未保存修改，保存后切换").TextColor(c.Theme().Warning)
			ui.Button(c, "放弃修改").OnClick(func() {
				a.confirmThen("放弃未保存的修改？", "配置文件不会改变。", func(context.Context) error {
					a.publish(func() {
						a.editorBuf = nil
						a.editorName = ""
						a.editorDirty = false
						a.populateForm("")
						a.probeSettingsLoaded = false
					})
					return nil
				})
			})
		}
	})
	if selected != "" {
		ui.Row(c).Padding(6, 12).Gap(8).Children(func() {
			c.Key("profile-name-" + selected)
			ui.TextInput(c, &a.profileNameDraft).Placeholder("新的显示名称").Width(220).Disabled(a.profileRenameBusy || a.readOnly())
			c.Key("profile-rename-" + selected)
			ui.Button(c, "重命名网络").Disabled(a.profileRenameBusy || a.readOnly() || a.profileNameDraft == "").OnClick(a.renameNetwork)
			if a.profileRenameMessage != "" {
				ui.Text(c, a.profileRenameMessage)
			}
		})
	}
}

// Capture both stable target and view generation before starting I/O. A later
// selection change must not retarget the request or publish into the new view.
func (a *App) renameNetwork() {
	if a.profileRenameBusy || a.readOnly() {
		return
	}
	name := a.profileNameDraft
	if err := configdoc.ValidName(name); err != nil {
		a.profileRenameMessage = "名称无效：" + err.Error()
		return
	}
	a.mu.Lock()
	id := a.profileID
	client := a.currentClientLocked()
	a.mu.Unlock()
	if id == "" {
		return
	}
	a.profileRenameBusy = true
	a.profileRenameMessage = "正在重命名…"
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_, err := client.RenameProfile(ctx, id, name)
		a.publishFor(client, func() { a.finishProfileRename(name, err) })
	}()
}

func (a *App) finishProfileRename(name string, err error) {
	a.profileRenameBusy = false
	if err != nil {
		a.profileRenameMessage = "重命名失败：" + err.Error()
		return
	}
	a.profileRenameMessage = "已重命名为 " + name
	a.profileNameDraft = ""
	a.triggerPoll()
}

func (a *App) quitTargets() map[string]*localapi.Client {
	a.mu.Lock()
	defer a.mu.Unlock()
	targets := make(map[string]*localapi.Client)
	if len(a.networks) == 0 {
		targets["当前网络"] = a.currentClientLocked()
		return targets
	}
	for id, state := range a.networks {
		if state != nil && state.Daemon.Mode == "managed" {
			targets[id] = a.client.ForProfile(id)
		}
	}
	return targets
}

// Try every target and retain named partial errors. Never claim stop-all
// succeeded after only the selected worker stopped.
func stopNetworkTargets(ctx context.Context, targets map[string]*localapi.Client) error {
	ids := make([]string, 0, len(targets))
	for id := range targets {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	var failures []error
	for _, id := range ids {
		if _, err := targets[id].StopCore(ctx); err != nil {
			failures = append(failures, fmt.Errorf("%s: %w", id, err))
		}
	}
	return errors.Join(failures...)
}
