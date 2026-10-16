package helper

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/fsutil"
)

// ServiceName is the Windows service of the helper.
const ServiceName = "EasyTierMyGoHelper"

// Elevated reports whether this process has administrator rights (an
// elevated token; the service runs as SYSTEM).
func Elevated() bool { return windows.GetCurrentProcessToken().IsElevated() }

// Install copies the binaries, locks down the state directory, records the
// operator and registers an automatically started service. The helper
// always runs; Follow GUI is enforced by the daemon, which runs networks
// only while a GUI session is attached unless background mode is on.
func Install(o InstallOptions) error {
	if !Elevated() {
		return errors.New("installing the helper needs administrator rights")
	}
	stopService()
	if err := os.MkdirAll(BinDir, 0o755); err != nil {
		return err
	}
	for _, name := range append(append([]string{}, Binaries...), Optional...) {
		file := name
		if name != "upx-COPYING" {
			file = exeName(name)
		}
		src := filepath.Join(o.From, file)
		if !fileExists(src) {
			if contains(Binaries, name) {
				return fmt.Errorf("install %s: missing", file)
			}
			continue
		}
		if err := copyRootOwned(src, filepath.Join(BinDir, file)); err != nil {
			return fmt.Errorf("install %s: %w", file, err)
		}
	}
	// The core needs the WinTun and packet capture DLLs next to it.
	for _, dll := range []string{"wintun.dll", "Packet.dll", "WinDivert64.sys"} {
		if src := filepath.Join(o.From, dll); fileExists(src) {
			if err := copyRootOwned(src, filepath.Join(BinDir, dll)); err != nil {
				return err
			}
		}
	}
	root := filepath.Dir(StateDir)
	if err := os.MkdirAll(StateDir, 0o700); err != nil {
		return err
	}
	// SYSTEM and Administrators only, except reading the directory that
	// holds the socket; the daemon authorizes each caller by SID.
	if out, err := exec.Command("icacls", root, "/inheritance:r", "/grant:r",
		"*S-1-5-18:(OI)(CI)F", "*S-1-5-32-544:(OI)(CI)F", "*S-1-5-32-545:RX").CombinedOutput(); err != nil {
		return fmt.Errorf("icacls: %v: %s", err, out)
	}
	if err := fsutil.WriteJSON(filepath.Join(StateDir, "helper.json"), map[string]any{"operator_sid": o.OperatorSID}); err != nil {
		return err
	}
	m, err := mgr.Connect()
	if err != nil {
		return err
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		s, err = m.CreateService(ServiceName, svcPath(), mgr.Config{
			DisplayName: "EasyTier MyGo helper",
			Description: "Runs EasyTier networks for EasyTier MyGo.",
			StartType:   mgr.StartAutomatic,
		}, "run", "--host", "helper")
		if err != nil {
			return err
		}
	}
	defer s.Close()
	// Restart after a failure, and after a self-update (which exits).
	_ = s.SetRecoveryActions([]mgr.RecoveryAction{{Type: mgr.ServiceRestart, Delay: 2 * time.Second}}, 60)
	return s.Start()
}

// stopService stops the helper service if it runs.
func stopService() {
	m, err := mgr.Connect()
	if err != nil {
		return
	}
	defer m.Disconnect()
	s, err := m.OpenService(ServiceName)
	if err != nil {
		return
	}
	defer s.Close()
	if st, err := s.Control(svc.Stop); err == nil {
		for i := 0; i < 50 && st.State != svc.Stopped; i++ {
			time.Sleep(200 * time.Millisecond)
			st, _ = s.Query()
		}
	}
}

// Uninstall stops and deletes the service and its files, and the
// `EasyTier MyGo` folders above them once they are empty.
func Uninstall(purge bool) error {
	if !Elevated() {
		return errors.New("removing the helper needs administrator rights")
	}
	stopService()
	if m, err := mgr.Connect(); err == nil {
		if s, err := m.OpenService(ServiceName); err == nil {
			_ = s.Delete()
			s.Close()
		}
		m.Disconnect()
	}
	if err := removeWithParent(BinDir); err != nil {
		return err
	}
	if purge {
		return removeWithParent(StateDir)
	}
	return nil
}

// SetBackground needs nothing from the service manager: the helper always
// runs and the daemon's setting decides.
func SetBackground(bool) error { return nil }

// Inspect reports the installed helper. It asks only for query rights, so
// it works for a user without administrator rights (the GUI).
func Inspect() Status {
	var st Status
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return st
	}
	defer windows.CloseServiceHandle(scm)
	name, _ := windows.UTF16PtrFromString(ServiceName)
	h, err := windows.OpenService(scm, name, windows.SERVICE_QUERY_STATUS)
	if err != nil {
		return st
	}
	defer windows.CloseServiceHandle(h)
	st.Installed = true
	var q windows.SERVICE_STATUS
	if windows.QueryServiceStatus(h, &q) == nil {
		st.Active = q.CurrentState == windows.SERVICE_RUNNING
	}
	if out, err := exec.Command(svcPath(), "version").Output(); err == nil {
		st.Version = strings.TrimSpace(string(out))
	}
	return st
}

// SelfUpdate replaces the binaries from a signed bundle. A running
// executable cannot be overwritten on Windows, so the old one is renamed
// aside first; the helper then exits and the service manager restarts it.
// Notes: installs the bytes that were verified, read once from the
// user-writable app directory (installBundle).
func SelfUpdate(from, running string) error {
	b, err := verifiedBundle(from, running)
	if err != nil {
		return err
	}
	if err := installBundle(BinDir, b); err != nil {
		return err
	}
	go func() {
		time.Sleep(time.Second)
		os.Exit(3) // recovery action: restart with the new executable
	}()
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
