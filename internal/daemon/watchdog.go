package daemon

// The progress watchdog.
//
// The daemon can fail in two ways: it can exit (systemd's Restart= covers
// that) or it can stay alive but stop making progress — a deadlock, a
// wedged syscall, a runtime stuck on one goroutine. Only an *external*
// watcher can see the second case, so systemd owns the decision: the unit
// sets WatchdogSec=, the daemon reports progress after every completed
// check round, and when the rounds stop advancing the notifications stop,
// systemd kills the service and starts it again.
//
// The report is progress, not liveness: a process that is alive but
// wedged must not keep the watchdog fed. The unit's WatchdogSec is
// derived from the check interval at install time (see
// platformservice.WatchdogTimeout), so a legitimately slow round has room
// while a wedged one does not.

import "context"

// sdNotifier is the slice of the systemd notification protocol the
// daemon uses. sdnotify.Notifier implements it; tests fake it.
type sdNotifier interface {
	Ready() error
	Watchdog() error
	Stopping() error
}

// notifierReady sends READY=1, so a Type=notify unit finishes starting
// (and starts its watchdog clock). Failures are logged, never fatal: the
// daemon works the same when it was started by hand.
func (d *Daemon) notifierReady() {
	if d.notifier == nil {
		return
	}
	if err := d.notifier.Ready(); err != nil {
		d.opts.Logf("daemon: service manager notification failed: %v", err)
	}
}

// reportProgress tells the service manager that a check round completed.
// A failure is logged once: a manager that went away must not flood the
// log from every round.
func (d *Daemon) reportProgress() {
	if d.notifier == nil {
		return
	}
	if err := d.notifier.Watchdog(); err != nil {
		d.notifyFailOnce.Do(func() {
			d.opts.Logf("daemon: watchdog notification failed: %v", err)
		})
	}
}

// notifierStopping tells the service manager the shutdown is orderly, so
// it does not treat a slow stop as a watchdog failure.
func (d *Daemon) notifierStopping() {
	if d.notifier == nil {
		return
	}
	if err := d.notifier.Stopping(); err != nil {
		d.opts.Logf("daemon: service manager notification failed: %v", err)
	}
}

// watchdogStopHook reports STOPPING=1 when the context is cancelled. It
// runs as its own goroutine so the notification is sent before the core
// is stopped, without complicating the shutdown path.
func (d *Daemon) watchdogStopHook(ctx context.Context) {
	<-ctx.Done()
	d.notifierStopping()
}
