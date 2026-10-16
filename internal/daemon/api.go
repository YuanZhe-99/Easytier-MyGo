package daemon

import (
	"context"
	"errors"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/configdoc"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/profiles"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/supervisor"
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
		d.mu.Lock()
		d.settings = *req.Settings
		s := d.settings
		d.mu.Unlock()
		if err := fsutil.WriteJSON(d.settingsPath(), s); err != nil {
			return failOf(err)
		}
		d.reconcile()
		return localapi.Response{OK: true, Settings: &s}
	case localapi.RequestSessionWatch:
		d.setWatch(req.Session, req.Watch)
		return localapi.Response{OK: true}
	case localapi.RequestShutdown:
		d.cancel()
		return localapi.Response{OK: true}
	}
	return localapi.Fail("", "unknown request %q", req.Type)
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
