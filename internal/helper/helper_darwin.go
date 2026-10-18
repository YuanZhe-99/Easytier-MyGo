package helper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
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
	return reload()
}

// reload replaces the loaded daemon job with the one in plistPath.
// Notes: launchctl bootout returns before launchd has removed the job (the
// daemon first stops its networks), and a bootstrap until then fails with
// "Input/output error", leaving no helper running (regression: a reinstall
// from the update banner installed the files and then lost the job). So it
// waits for the old job to go and retries the bootstrap briefly.
func reload() error {
	_ = exec.Command("launchctl", "bootout", "system/"+Label).Run()
	for deadline := time.Now().Add(20 * time.Second); loaded() && time.Now().Before(deadline); {
		time.Sleep(200 * time.Millisecond)
	}
	var err error
	for range 10 {
		if err = launchctl("bootstrap", "system", plistPath()); err == nil {
			return nil
		}
		time.Sleep(500 * time.Millisecond)
	}
	return err
}

// loaded reports whether launchd has the daemon job.
func loaded() bool { return exec.Command("launchctl", "print", "system/"+Label).Run() == nil }

// Uninstall unloads and removes the helper, its empty socket directory and
// the `EasyTier MyGo` folder above its files once that is empty.
func Uninstall(purge bool) error {
	if os.Geteuid() != 0 {
		return errors.New("removing the helper needs administrator rights")
	}
	_ = exec.Command("launchctl", "bootout", "system/"+Label).Run()
	if err := os.Remove(plistPath()); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	// The daemon removes its socket on exit; the directory stays behind.
	_ = os.Remove(filepath.Dir(localapi.SystemSocketPath))
	if err := removeWithParent(BinDir); err != nil {
		return err
	}
	if purge {
		return removeWithParent(StateDir)
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
	s.Active = loaded()
	if out, err := exec.Command(svcPath(), "version").Output(); err == nil {
		s.Version = strings.TrimSpace(string(out))
	}
	return s
}

// SelfUpdate replaces the binaries with the app's when each one's code
// signature satisfies the designated requirement of the installed copy (the
// same signing identity), and restarts.
// Notes: a release build re-signs every executable inside the app, so the
// bundle manifest (internal/bundlesig) never matches on macOS; the code
// signature is checked instead. An ad hoc signed helper designates its own
// hash, so updating it needs the administrator prompt.
func SelfUpdate(from, running string) error {
	if err := replaceSigned(BinDir, from, running); err != nil {
		return err
	}
	go func() {
		time.Sleep(time.Second)
		_ = exec.Command("launchctl", "kickstart", "-k", "system/"+Label).Run()
	}()
	return nil
}

// replaceSigned installs the app's copies of Binaries into binDir.
//
// Inputs: binDir, the root-owned helper directory; from, the user-writable
// app directory; running, the helper's version.
// Side effects: replaces the files in binDir, or leaves them untouched and
// removes its staged copies on any error.
// Notes: each file is copied into binDir first and that root-owned copy is
// checked and installed, so a file swapped in the app directory after the
// check is not installed. The staged daemon must report a version that is
// not older than running.
func replaceSigned(binDir, from, running string) error {
	staged := map[string]string{}
	defer func() {
		for _, p := range staged {
			_ = os.Remove(p)
		}
	}()
	for _, name := range Binaries {
		dst := filepath.Join(binDir, name)
		req, err := designatedRequirement(dst)
		if err != nil {
			return err
		}
		data, err := os.ReadFile(filepath.Join(from, name))
		if err != nil {
			return err
		}
		tmp := dst + ".new"
		if err := writeRootOwned(tmp, data); err != nil {
			return err
		}
		staged[name] = tmp
		if out, err := exec.Command("codesign", "--verify", "--strict", "-R="+req, tmp).CombinedOutput(); err != nil {
			return fmt.Errorf("%s is not signed like the helper: %s", name, strings.TrimSpace(string(out)))
		}
	}
	out, err := exec.Command(staged["easytier-mygo-svc"], "version").Output()
	v := strings.TrimSpace(string(out))
	if err != nil || v == "" {
		return fmt.Errorf("read the new helper's version: %v", err)
	}
	if version.Older(v, running) {
		return fmt.Errorf("bundle %s is older than the helper %s", v, running)
	}
	for _, name := range Binaries {
		if err := os.Rename(staged[name], filepath.Join(binDir, name)); err != nil {
			return err
		}
		delete(staged, name)
	}
	return nil
}

// designatedRequirement is the code requirement a file's signature
// designates (for a real identity: its identifier and certificate; ad hoc:
// its hash, which codesign prints as a comment).
func designatedRequirement(path string) (string, error) {
	out, err := exec.Command("codesign", "-d", "-r-", path).CombinedOutput()
	if err != nil {
		return "", fmt.Errorf("codesign: %v: %s", err, strings.TrimSpace(string(out)))
	}
	for _, line := range strings.Split(string(out), "\n") {
		if r, ok := strings.CutPrefix(strings.TrimPrefix(line, "# "), "designated => "); ok {
			return r, nil
		}
	}
	return "", fmt.Errorf("%s designates no requirement", path)
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
