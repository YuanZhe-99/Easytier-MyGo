package daemon

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"
)

// Explicit checks are independent of health/recovery and use loaded launch choices.
// Results are not cached as a current snapshot: writability can change immediately.
func (d *Daemon) handleLoggingCheck(ctx context.Context) localapi.Response {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	r := localapi.Response{Version: localapi.ProtocolVersion}
	if err := ctx.Err(); err != nil {
		r.Error = err.Error()
		return r
	}
	check := &localapi.LoggingSinkCheck{CheckedAt: time.Now().UTC()}
	switch {
	case !d.managed():
		check.State, check.Reason = "unmanaged", "no-managed-sink"
	case d.opts.Logging.FileLevel == "off" || d.opts.Logging.FileLevel == "":
		check.State, check.Reason = "disabled", "no-explicit-file-sink"
	default:
		err := checkLoggingSink(filepath.Join(d.opts.StateDir, "core-logs"))
		if err == nil {
			check.State, check.Reason = "writable", "path-probe-passed"
		} else {
			check.State = "unavailable"
			switch {
			case errors.Is(err, os.ErrPermission):
				check.Reason = "permission-denied"
			case errors.Is(err, syscall.ENOSPC):
				check.Reason = "disk-full"
			case errors.Is(err, os.ErrNotExist):
				check.Reason = "path-missing"
			default:
				check.Reason = "path-probe-failed"
			}
		}
	}
	r.OK, r.LoggingSinkCheck = true, check
	return r
}

func loadLoggingSettings(opts *Options) error {
	b, err := os.ReadFile(filepath.Join(opts.StateDir, "logging.json"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		return err
	}
	var settings LoggingOptions
	if err := json.Unmarshal(b, &settings); err != nil {
		return errors.New("invalid saved logging settings")
	}
	if err := settings.Validate(); err != nil {
		return err
	}
	opts.Logging = settings
	return nil
}

// Saved launch choices never mutate a live process; initialization loads them.
func (d *Daemon) handleLoggingSettings(ctx context.Context, req localapi.Request) localapi.Response {
	d.operationMu.Lock()
	defer d.operationMu.Unlock()
	r := localapi.Response{Version: localapi.ProtocolVersion}
	fail := func(err error) localapi.Response { r.Error = err.Error(); return r }
	if err := ctx.Err(); err != nil {
		return fail(err)
	}
	settings := d.opts.Logging
	if req.Type == localapi.RequestLoggingSave {
		if !d.managed() {
			return fail(errors.New("logging settings require managed mode"))
		}
		if req.Logging == nil {
			return fail(errors.New("logging settings required"))
		}
		settings = LoggingOptions{ConsoleLevel: req.Logging.ConsoleLevel, FileLevel: req.Logging.FileLevel, FileSizeMB: req.Logging.FileSizeMB, FileCount: req.Logging.FileCount}
		if err := settings.Validate(); err != nil {
			return fail(err)
		}
		b, err := json.Marshal(settings)
		if err != nil {
			return fail(err)
		}
		if err := atomicState(filepath.Join(d.opts.StateDir, "logging.json"), b); err != nil {
			return fail(err)
		}
	} else {
		opts := d.opts
		if err := loadLoggingSettings(&opts); err != nil {
			return fail(err)
		}
		settings = opts.Logging
	}
	r.OK = true
	r.Logging = &localapi.CoreLogging{ConsoleLevel: settings.ConsoleLevel, FileLevel: settings.FileLevel, FileSizeMB: settings.FileSizeMB, FileCount: settings.FileCount}
	r.LoggingRestartRequired = settings != d.opts.Logging
	return r
}

// LoggingOptions are launch settings, not EasyTier network TOML. An unset
// option preserves the pinned core default; explicit "off" disables a sink.
type LoggingOptions struct {
	ConsoleLevel string `json:"console_level,omitempty"`
	FileLevel    string `json:"file_level,omitempty"`
	FileSizeMB   int    `json:"file_size_mb,omitempty"`
	FileCount    int    `json:"file_count,omitempty"`
}

func (o LoggingOptions) Validate() error {
	for _, level := range []string{o.ConsoleLevel, o.FileLevel} {
		switch level {
		case "", "off", "error", "warn", "info", "debug", "trace":
		default:
			return errors.New("invalid core log level")
		}
	}
	if o.FileSizeMB < 0 || o.FileSizeMB > 100 || o.FileCount < 0 || o.FileCount > 10 {
		return errors.New("core log limits must be 1–100 MiB and 1–10 files (zero uses core defaults)")
	}
	return nil
}

func (o LoggingOptions) args(logPath string) ([]string, error) {
	if err := o.Validate(); err != nil {
		return nil, err
	}
	var args []string
	if o.ConsoleLevel != "" {
		args = append(args, "--console-log-level", o.ConsoleLevel)
	}
	if o.FileLevel != "" {
		args = append(args, "--file-log-level", o.FileLevel)
		if o.FileLevel != "off" {
			dir := filepath.Join(filepath.Dir(logPath), "core-logs")
			if err := os.MkdirAll(dir, 0o700); err != nil {
				return nil, err
			}
			if err := checkLoggingSink(dir); err != nil {
				return nil, fmt.Errorf("file log sink unavailable: %w", err)
			}
			args = append(args, "--file-log-dir", dir)
		}
	}
	if o.FileSizeMB != 0 {
		args = append(args, "--file-log-size", strconv.Itoa(o.FileSizeMB))
	}
	if o.FileCount != 0 {
		args = append(args, "--file-log-count", strconv.Itoa(o.FileCount))
	}
	return args, nil
}

// checkLoggingSink checks launch-time capacity and permissions without writing
// to existing logs. It is not a delivery guarantee: permissions/capacity can
// change after this probe, and the upstream logger can fail independently.
func checkLoggingSink(dir string) error {
	info, err := os.Lstat(dir)
	if err != nil {
		return err
	}
	if !info.IsDir() {
		return errors.New("file log directory must be a directory, not a link")
	}
	active := filepath.Join(dir, "easytier.log")
	if info, err := os.Lstat(active); err == nil {
		if !info.Mode().IsRegular() {
			return errors.New("active file log must be a regular file, not a link")
		}
		file, err := os.OpenFile(active, os.O_WRONLY|os.O_APPEND, 0)
		if err != nil {
			return err
		}
		if err := file.Close(); err != nil {
			return err
		}
	} else if !os.IsNotExist(err) {
		return err
	}
	probe, err := os.CreateTemp(dir, ".logging-probe-*")
	if err != nil {
		return err
	}
	defer os.Remove(probe.Name())
	_, writeErr := probe.Write([]byte("log sink probe\n"))
	if writeErr == nil {
		writeErr = probe.Sync()
	}
	closeErr := probe.Close()
	if writeErr != nil {
		return writeErr
	}
	return closeErr
}
