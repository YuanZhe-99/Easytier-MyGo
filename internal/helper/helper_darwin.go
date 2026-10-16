package helper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/bundlesig"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/version"
)

// Label is the launchd job of the helper.
const Label = "com.yuanzhe.easytiermygo.helper"

func plistPath() string { return "/Library/LaunchDaemons/" + Label + ".plist" }

// plist is the LaunchDaemon. The helper always runs (launchd socket
// activation needs a C API); Follow GUI is enforced by the daemon, which
// runs networks only while a GUI session is attached unless background
// mode is on.
func plist() string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0">
<dict>
	<key>Label</key><string>%s</string>
	<key>ProgramArguments</key>
	<array>
		<string>%s</string><string>run</string><string>--host</string><string>helper</string>
	</array>
	<key>RunAtLoad</key><true/>
	<key>KeepAlive</key><true/>
	<key>StandardErrorPath</key><string>%s</string>
</dict>
</plist>
`, Label, svcPath(), filepath.Join(StateDir, "helper.log"))
}

// Install copies the binaries, records the operator and loads the daemon.
func Install(o InstallOptions) error {
	if os.Geteuid() != 0 {
		return errors.New("installing the helper needs administrator rights")
	}
	if err := os.MkdirAll(BinDir, 0o755); err != nil {
		return err
	}
	for _, name := range append(append([]string{}, Binaries...), Optional...) {
		src := filepath.Join(o.From, name)
		if !fileExists(src) {
			if contains(Binaries, name) {
				return fmt.Errorf("install %s: missing", name)
			}
			continue
		}
		if err := copyRootOwned(src, filepath.Join(BinDir, name)); err != nil {
			return fmt.Errorf("install %s: %w", name, err)
		}
	}
	if err := os.MkdirAll(StateDir, 0o700); err != nil {
		return err
	}
	if err := fsutil.WriteJSON(filepath.Join(StateDir, "helper.json"), map[string]any{"operator": o.Operator}); err != nil {
		return err
	}
	if err := fsutil.WriteFileAtomic(plistPath(), []byte(plist()), 0o644); err != nil {
		return err
	}
	_ = exec.Command("launchctl", "bootout", "system/"+Label).Run()
	return launchctl("bootstrap", "system", plistPath())
}

// Uninstall unloads and removes the helper.
func Uninstall(purge bool) error {
	if os.Geteuid() != 0 {
		return errors.New("removing the helper needs administrator rights")
	}
	_ = exec.Command("launchctl", "bootout", "system/"+Label).Run()
	if err := os.Remove(plistPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	if err := os.RemoveAll(BinDir); err != nil {
		return err
	}
	if purge {
		return os.RemoveAll(StateDir)
	}
	return nil
}

// SetBackground needs nothing from launchd: the helper always runs and the
// daemon's setting decides whether networks run without a session.
func SetBackground(bool) error { return nil }

// Inspect reports the installed helper.
func Inspect() Status {
	var s Status
	if !fileExists(plistPath()) {
		return s
	}
	s.Installed = true
	s.Active = exec.Command("launchctl", "print", "system/"+Label).Run() == nil
	if out, err := exec.Command(svcPath(), "version").Output(); err == nil {
		s.Version = strings.TrimSpace(string(out))
	}
	return s
}

// SelfUpdate replaces the binaries from a signed bundle and restarts.
func SelfUpdate(from, running string) error {
	key, err := bundlesig.PublicKey()
	if err != nil {
		return err
	}
	m, err := bundlesig.Verify(from, key)
	if err != nil {
		return err
	}
	if version.Older(m.Version, running) {
		return fmt.Errorf("bundle %s is older than the helper %s", m.Version, running)
	}
	for name := range m.Files {
		if err := copyRootOwned(filepath.Join(from, name), filepath.Join(BinDir, name)); err != nil {
			return err
		}
	}
	go func() {
		time.Sleep(time.Second)
		_ = exec.Command("launchctl", "kickstart", "-k", "system/"+Label).Run()
	}()
	return nil
}

func launchctl(args ...string) error {
	if out, err := exec.Command("launchctl", args...).CombinedOutput(); err != nil {
		return fmt.Errorf("launchctl %s: %v: %s", strings.Join(args, " "), err, strings.TrimSpace(string(out)))
	}
	return nil
}

func contains(xs []string, x string) bool {
	for _, y := range xs {
		if y == x {
			return true
		}
	}
	return false
}
