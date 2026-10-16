package daemon

import (
	"context"
	"errors"
	"slices"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/cores"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/profiles"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/supervisor"
	"github.com/YuanZhe-99/Easytier-MyGo/third_party"
)

// Handle answers one request (localapi.Handler).
func (d *Daemon) Handle(ctx context.Context, req localapi.Request, access localapi.Access) localapi.Response {
	d.touch()
	if !access.Allows(req.Type) {
		return localapi.Fail(localapi.CodeForbidden, "permission denied")
	}
	switch req.Type {
	case localapi.RequestInfo:
		return localapi.Response{OK: true, Info: d.info(access)}
	case localapi.RequestNetworks:
		return localapi.Response{OK: true, Networks: d.statuses()}
	case localapi.RequestDetail:
		e, p, fail := d.lookup(req.Network)
		if fail != nil {
			return *fail
		}
		detail := e.w.Detail()
		d.decorate(&detail.Status, p)
		return localapi.Response{OK: true, Detail: &detail}
	case localapi.RequestTraffic:
		e, _, fail := d.lookup(req.Network)
		if fail != nil {
			return *fail
		}
		return localapi.Response{OK: true, Traffic: e.w.Traffic(req.Since)}
	case localapi.RequestEvents:
		return localapi.Response{OK: true, Events: d.events.recent(200)}
	case localapi.RequestConfigGet:
		text, entry, err := d.store.Config(req.Network)
		if err != nil {
			return failOf(err)
		}
		p, _ := d.store.Get(req.Network)
		return localapi.Response{OK: true, Config: &localapi.ConfigDoc{Text: text, Revision: entry.Revision, Applied: p.Applied}}
	case localapi.RequestConfigSave:
		entry, err := d.store.SaveConfig(req.Network, req.Text, req.Expected)
		if err != nil {
			return failOf(err)
		}
		d.notify()
		p, _ := d.store.Get(req.Network)
		return localapi.Response{OK: true, Config: &localapi.ConfigDoc{Revision: entry.Revision, Applied: p.Applied}}
	case localapi.RequestNetworkCreate:
		p, err := d.store.Create(req.Name, req.Text)
		if err != nil {
			return failOf(err)
		}
		d.addWorker(p.ID)
		d.reconcile()
		return localapi.Response{OK: true, Networks: d.statuses()}
	case localapi.RequestNetworkDelete:
		if _, err := d.store.Get(req.Network); err != nil {
			return failOf(err)
		}
		d.removeWorker(req.Network)
		if err := d.store.Delete(req.Network); err != nil {
			return failOf(err)
		}
		d.notify()
		return localapi.Response{OK: true, Networks: d.statuses()}
	case localapi.RequestNetworkRename:
		if _, err := d.store.Rename(req.Network, req.Name); err != nil {
			return failOf(err)
		}
		d.notify()
		return localapi.Response{OK: true, Networks: d.statuses()}
	case localapi.RequestConnect, localapi.RequestDisconnect:
		run := req.Type == localapi.RequestConnect
		if run {
			if err := d.checkPrivilege(req.Network); err != nil {
				return failOf(err)
			}
		}
		if _, err := d.store.SetRun(req.Network, run); err != nil {
			return failOf(err)
		}
		if err := d.applyIntent(req.Network); err != nil {
			return failOf(err)
		}
		return localapi.Response{OK: true, Networks: d.statuses()}
	case localapi.RequestApply:
		if err := d.apply(ctx, req.Network); err != nil {
			return failOf(err)
		}
		return localapi.Response{OK: true, Networks: d.statuses()}
	case localapi.RequestRetry:
		e, _, fail := d.lookup(req.Network)
		if fail != nil {
			return *fail
		}
		if err := e.w.GrantRetry(ctx); err != nil {
			return failOf(err)
		}
		return localapi.Response{OK: true}
	case localapi.RequestSettingsGet:
		d.mu.Lock()
		s := d.settings
		d.mu.Unlock()
		return localapi.Response{OK: true, Settings: &s}
	case localapi.RequestSettingsSet:
		if req.Settings == nil {
			return localapi.Fail("", "no settings")
		}
		if req.Settings.Background && d.opts.Host != localapi.HostHelper {
			return localapi.Fail(localapi.CodeNeedsHelper, "background mode needs the privileged helper")
		}
		if tag := req.Settings.Core; tag != "" && tag != cores.BundledTag {
			if d.opts.Cores == nil {
				return localapi.Fail("", "core management is not available")
			}
			if _, ok := d.opts.Cores.Find(tag); !ok {
				return localapi.Fail(localapi.CodeNotFound, "core %q is not installed", tag)
			}
		}
		d.mu.Lock()
		old := d.settings
		d.mu.Unlock()
		if d.opts.OnBackground != nil && req.Settings.Background != old.Background {
			if err := d.opts.OnBackground(req.Settings.Background); err != nil {
				return failOf(err)
			}
		}
		d.mu.Lock()
		d.settings = *req.Settings
		s := d.settings
		d.mu.Unlock()
		if err := fsutil.WriteJSON(d.settingsPath(), s); err != nil {
			return failOf(err)
		}
		if s.Core != old.Core {
			d.switchCore(ctx)
		}
		d.reconcile()
		return localapi.Response{OK: true, Settings: &s}
	case localapi.RequestCores:
		return localapi.Response{OK: true, Cores: d.coreList()}
	case localapi.RequestReleases:
		if d.opts.Cores == nil {
			return localapi.Fail("", "core management is not available")
		}
		rels, err := d.opts.Cores.Releases(ctx, req.Pre)
		if err != nil {
			return failOf(err)
		}
		installed := map[string]bool{}
		for _, c := range d.opts.Cores.List() {
			installed[c.Tag] = true
		}
		man, _ := third_party.EasyTier()
		var out []localapi.ReleaseInfo
		for _, r := range rels {
			out = append(out, localapi.ReleaseInfo{Tag: r.Tag, Prerelease: r.Prerelease, Published: r.Published,
				Size: r.Size, Installed: installed[r.Tag], Tested: slices.Contains(man.Tested, r.Tag)})
		}
		return localapi.Response{OK: true, Releases: out}
	case localapi.RequestCoreInstall:
		if d.opts.Cores == nil {
			return localapi.Fail("", "core management is not available")
		}
		rels, err := d.opts.Cores.Releases(ctx, true)
		if err != nil {
			return failOf(err)
		}
		i := slices.IndexFunc(rels, func(r cores.Release) bool { return r.Tag == req.Tag })
		if i < 0 {
			return localapi.Fail(localapi.CodeNotFound, "no release %q for this platform", req.Tag)
		}
		if _, err := d.opts.Cores.Install(ctx, rels[i]); err != nil {
			return failOf(err)
		}
		return localapi.Response{OK: true, Cores: d.coreList()}
	case localapi.RequestCoreAdd:
		if d.opts.Cores == nil {
			return localapi.Fail("", "core management is not available")
		}
		if d.opts.Host == localapi.HostHelper {
			// Root must not take a binary from a user's directory without
			// an administrator's approval: `easytier-mygo-svc helper add-core`.
			return localapi.Fail(localapi.CodeNeedsAdmin, "adding a custom core to the helper needs administrator approval")
		}
		if _, err := d.opts.Cores.AddCustom(req.Name, req.Text); err != nil {
			return failOf(err)
		}
		return localapi.Response{OK: true, Cores: d.coreList()}
	case localapi.RequestCoreRemove:
		if d.opts.Cores == nil {
			return localapi.Fail("", "core management is not available")
		}
		d.mu.Lock()
		selected := d.settings.Core
		d.mu.Unlock()
		if req.Tag == selected {
			return localapi.Fail(localapi.CodeBusy, "the selected core cannot be removed")
		}
		if err := d.opts.Cores.Remove(req.Tag); err != nil {
			return failOf(err)
		}
		return localapi.Response{OK: true, Cores: d.coreList()}
	case localapi.RequestHelperUpdate:
		if d.opts.OnSelfUpdate == nil {
			return localapi.Fail(localapi.CodeNeedsAdmin, "this daemon cannot update itself")
		}
		if err := d.opts.OnSelfUpdate(req.Text); err != nil {
			return localapi.Fail(localapi.CodeNeedsAdmin, "%s", supervisor.ScrubDetail(err.Error()))
		}
		return localapi.Response{OK: true}
	case localapi.RequestSessionWatch:
		d.setWatch(req.Session, req.Watch)
		return localapi.Response{OK: true}
	case localapi.RequestShutdown:
		d.cancel()
		return localapi.Response{OK: true}
	}
	return localapi.Fail("", "unknown request %q", req.Type)
}

// coreList reports the installed cores and which one is selected.
func (d *Daemon) coreList() []localapi.CoreInfo {
	if d.opts.Cores == nil {
		return []localapi.CoreInfo{{Tag: cores.BundledTag, Source: string(cores.SourceBundled), Tested: true, Selected: true}}
	}
	d.mu.Lock()
	sel := d.settings.Core
	d.mu.Unlock()
	if sel == "" {
		sel = cores.BundledTag
	}
	var out []localapi.CoreInfo
	for _, c := range d.opts.Cores.List() {
		out = append(out, localapi.CoreInfo{Tag: c.Tag, Source: string(c.Source), Tested: c.Tested, Pre: c.Pre, Selected: c.Tag == sel})
	}
	return out
}

// switchCore moves every network to the selected core; running ones
// restart on it.
func (d *Daemon) switchCore(ctx context.Context) {
	core, cli := d.binaries()
	d.mu.Lock()
	var ws []*supervisor.Worker
	for _, e := range d.workers {
		ws = append(ws, e.w)
	}
	d.mu.Unlock()
	for _, w := range ws {
		if err := w.SetBinaries(ctx, core, cli); err != nil {
			d.opts.Logf("switch core: %v", err)
		}
	}
	d.notify()
}

// applyIntent makes one network follow its stored intent now.
func (d *Daemon) applyIntent(id string) error {
	p, err := d.store.Get(id)
	if err != nil {
		return err
	}
	d.mu.Lock()
	e := d.workers[id]
	want := p.Run && d.allowsRunningLocked()
	if e != nil {
		e.running = want
	}
	d.mu.Unlock()
	if e == nil {
		return profiles.ErrNotFound
	}
	defer d.notify()
	if want {
		err := d.startWorker(id, e.w)
		if err != nil {
			d.mu.Lock()
			e.running = false
			d.mu.Unlock()
		}
		return err
	}
	return e.w.Disconnect(d.ctx)
}

// apply freezes the saved document and restarts the network with it; a
// stopped network just records it as applied for its next start.
func (d *Daemon) apply(ctx context.Context, id string) error {
	e, p, fail := d.lookup(id)
	if fail != nil {
		return errors.New(fail.Error)
	}
	text, _, err := d.store.Config(id)
	if err != nil {
		return err
	}
	if _, err := configdoc.Parse(text); err != nil {
		return &supervisor.ConfigError{Detail: supervisor.ScrubDetail(err.Error())}
	}
	if d.opts.Host == localapi.HostHelper {
		if err := checkHelperSafety(text); err != nil {
			return &supervisor.ConfigError{Detail: err.Error()}
		}
	}
	path, rev, err := d.store.Freeze(id)
	if err != nil {
		return err
	}
	if err := supervisor.Preflight(ctx, d.opts.CoreBin, path); err != nil {
		return err
	}
	d.mu.Lock()
	running := e.running
	d.mu.Unlock()
	if !running {
		_, err := d.store.MarkApplied(id, rev)
		d.notify()
		return err
	}
	// Roll back to what the core runs now, which may not be confirmed (and
	// so not yet recorded as applied) when the user applies again quickly.
	prevRev := e.w.Status().Applied
	if prevRev == "" {
		prevRev = p.Applied
	}
	prevPath := ""
	if prevRev != "" {
		prevPath, _ = d.store.FrozenPath(id, prevRev)
	}
	return e.w.Apply(ctx, path, rev, prevPath, prevRev)
}

// lookup finds a network's worker and metadata.
func (d *Daemon) lookup(id string) (*workerEntry, profiles.Profile, *localapi.Response) {
	p, err := d.store.Get(id)
	if err != nil {
		r := failOf(err)
		return nil, p, &r
	}
	d.mu.Lock()
	e := d.workers[id]
	d.mu.Unlock()
	if e == nil {
		r := localapi.Fail(localapi.CodeNotFound, "network not found")
		return nil, p, &r
	}
	return e, p, nil
}

// statuses returns every network's status, in store order.
func (d *Daemon) statuses() []localapi.NetworkStatus {
	var out []localapi.NetworkStatus
	for _, p := range d.store.List() {
		d.mu.Lock()
		e := d.workers[p.ID]
		d.mu.Unlock()
		if e == nil {
			continue
		}
		s := e.w.Status()
		d.decorate(&s, p)
		out = append(out, s)
	}
	return out
}

// decorate adds the store's view (name, intent, saved revision, document
// facts) to a worker status.
func (d *Daemon) decorate(s *localapi.NetworkStatus, p profiles.Profile) {
	s.ID, s.Name, s.Run = p.ID, p.Name, p.Run
	if s.Applied == "" {
		s.Applied = p.Applied
	}
	if text, entry, err := d.store.Config(p.ID); err == nil {
		s.Saved = entry.Revision
		if sum, err := configdoc.Parse(text); err == nil {
			s.NetworkName, s.NoTUN = sum.NetworkName, sum.NoTun
		}
	}
	if p.Run && s.State == localapi.StateStopped && s.Error == "" {
		d.mu.Lock()
		allowed := d.allowsRunningLocked()
		d.mu.Unlock()
		if !allowed {
			s.Error = ""
		} else if err := d.checkPrivilege(p.ID); err != nil {
			s.Error = err.Error()
		}
	}
}

func (d *Daemon) info(access localapi.Access) *localapi.Info {
	return &localapi.Info{
		Version: d.opts.Version, Host: d.opts.Host, Access: access,
		StartedAt: d.startedAt, Privileged: d.opts.Privileged,
	}
}

// failOf maps store and worker errors to coded responses.
func failOf(err error) localapi.Response {
	var cfgErr *supervisor.ConfigError
	switch {
	case errors.Is(err, profiles.ErrNotFound):
		return localapi.Fail(localapi.CodeNotFound, "%s", err)
	case errors.Is(err, configdoc.ErrConflict):
		return localapi.Fail(localapi.CodeConflict, "%s", err)
	case errors.As(err, &cfgErr), configdoc.IsInvalid(err):
		return localapi.Fail(localapi.CodeInvalidConfig, "%s", supervisor.ScrubDetail(err.Error()))
	case errors.Is(err, errNeedsHelper):
		return localapi.Fail(localapi.CodeNeedsHelper, "%s", err)
	}
	msg := err.Error()
	if strings.Contains(msg, "network_secret") {
		msg = supervisor.ScrubDetail(msg)
	}
	return localapi.Fail("", "%s", msg)
}
