package helper

import (
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"os/user"
	"path/filepath"
	"strings"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

const unitDir = "/etc/systemd/system"

func socketUnitPath() string  { return filepath.Join(unitDir, Unit+".socket") }
func serviceUnitPath() string { return filepath.Join(unitDir, Unit+".service") }

// Install copies the binaries, creates the group, records the operator,
// writes the units and enables the on-demand socket.
// Side effects: root-only changes to /usr/local/libexec, /var/lib,
// /etc/systemd/system and /etc/group.
func Install(o InstallOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("installing the helper needs root")
	}
	if err := os.MkdirAll(BinDir, 0o755); err != nil {
		return err
	}
	for _, name := range Binaries {
		if err := copyRootOwned(filepath.Join(o.From, name), filepath.Join(BinDir, name)); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	if _, err := user.LookupGroup(Group); err != nil {
		if out, err := exec.Command("groupadd", "--system", Group).CombinedOutput(); err != nil {
			return fmt.Errorf("groupadd: %v: %s", err, strings.TrimSpace(string(out)))
		}
	}
	if err := os.MkdirAll(StateDir, 0o700); err != nil {
		return err
	}
	op := o.Operator
	if err := fsutil.WriteJSON(filepath.Join(StateDir, "helper.json"), map[string]any{"operator": op}); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(socketUnitPath(), []byte(SocketUnit()), 0o644); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(serviceUnitPath(), []byte(ServiceUnit()), 0o644); err != nil {
		return err
	}
	if err := systemctl("daemon-reload"); err != nil {
		return err
	}
	// A reinstall replaces a running helper with the new binary.
	_ = systemctl("try-restart", Unit+".service")
	return systemctl("enable", "--now", Unit+".socket")
}

// Uninstall stops and removes the helper; purge also deletes its state
// (networks, events) and the group.
func Uninstall(purge bool) error {
	if os.Geteuid() != 0 {
		return errors.New("removing the helper needs root")
	}
	_ = systemctl("disable", "--now", Unit+".service", Unit+".socket")
	for _, p := range []string{serviceUnitPath(), socketUnitPath()} {
		if err := os.Remove(p); err != nil && !errors.Is(err, os.ErrNotExist) {
			return err
		}
	}
	_ = systemctl("daemon-reload")
	if err := os.RemoveAll(BinDir); err != nil {
		return err
	}
	if purge {
		if err := os.RemoveAll(StateDir); err != nil {
			return err
		}
		if _, err := user.LookupGroup(Group); err == nil {
			_ = exec.Command("groupdel", Group).Run()
		}
	}
	return nil
}

// SetBackground enables or disables starting the helper at boot.
func SetBackground(on bool) error {
	if on {
		return systemctl("enable", Unit+".service")
	}
	return systemctl("disable", Unit+".service")
}

// Inspect reports the installed helper without privileges.
func Inspect() Status {
	var s Status
	if _, err := os.Stat(socketUnitPath()); err != nil {
		return s
	}
	s.Installed = true
	s.Active = exec.Command("systemctl", "is-active", "--quiet", Unit+".socket").Run() == nil
	s.Background = exec.Command("systemctl", "is-enabled", "--quiet", Unit+".service").Run() == nil
	if out, err := exec.Command(svcPath(), "version").Output(); err == nil {
		s.Version = strings.TrimSpace(string(out))
	}
	return s
}

func systemctl(args ...string) error {
	out, err := exec.Command("systemctl", args...).CombinedOutput()
	if err != nil {
		return fmt.Errorf("systemctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

// copyRootOwned copies an executable to dst as root:root 0755, atomically.
func copyRootOwned(src, dst string) error {
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()
	data, err := io.ReadAll(in)
	if err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(dst, data, 0o755); err != nil {
		return err
	}
	return os.Chown(dst, 0, 0)
}
