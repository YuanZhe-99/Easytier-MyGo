package gui

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/egoist/mygo"

	"github.com/YuanZhe-99/Easytier-MyGo/internal/localapi"
)

// SvcEnv overrides where the daemon binary is (development).
const SvcEnv = "EASYTIER_MYGO_SVC"

// svcBinary finds easytier-mygo-svc: $EASYTIER_MYGO_SVC, the app bundle's
// resources, the GUI's own directory, then PATH.
func svcBinary() (string, error) {
	name := "easytier-mygo-svc"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if p := os.Getenv(SvcEnv); p != "" {
		return p, nil
	}
	var dirs []string
	if res, err := mygo.App.Path(mygo.PathResources); err == nil && res != "" {
		dirs = append(dirs, filepath.Join(res, "bin"), res)
	}
	if exe, err := os.Executable(); err == nil {
		dirs = append(dirs, filepath.Dir(exe), filepath.Join(filepath.Dir(exe), "bin"))
	}
	for _, d := range dirs {
		if p := filepath.Join(d, name); fileExists(p) {
			return p, nil
		}
	}
	if p, err := exec.LookPath(name); err == nil {
		return p, nil
	}
	return "", errors.New("easytier-mygo-svc not found")
}

func fileExists(p string) bool {
	_, err := os.Stat(p)
	return err == nil
}

// userDaemon runs the no-privileges daemon as a child tied to this process
// by a lifeline pipe: when the GUI exits, even by a crash, the pipe closes
// and the daemon stops its networks and exits.
type userDaemon struct {
	mu       sync.Mutex
	cmd      *exec.Cmd
	lifeline *os.File // write end, held for the GUI's lifetime
	starts   []time.Time
}

// ensure starts the daemon unless one already answers on the socket.
func (u *userDaemon) ensure(ctx context.Context, socket string) error {
	if _, err := localapi.NewClient(socket).Do(ctx, localapi.Request{Type: localapi.RequestInfo}); err == nil {
		return nil
	}
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.cmd != nil && u.cmd.ProcessState == nil {
		return nil // starting
	}
	now := time.Now()
	recent := u.starts[:0]
	for _, t := range u.starts {
		if now.Sub(t) < time.Minute {
			recent = append(recent, t)
		}
	}
	u.starts = recent
	if len(u.starts) >= 3 {
		return errors.New("the background process keeps stopping")
	}
	bin, err := svcBinary()
	if err != nil {
		return err
	}
	r, w, err := os.Pipe()
	if err != nil {
		return err
	}
	cmd := exec.Command(bin, "run", "--host", "user", "--socket", socket, "--lifeline-fd", "3")
	cmd.ExtraFiles = []*os.File{r}
	if log, err := openDaemonLog(); err == nil {
		cmd.Stdout, cmd.Stderr = log, log
	}
	if err := cmd.Start(); err != nil {
		r.Close()
		w.Close()
		return fmt.Errorf("start background process: %w", err)
	}
	r.Close()
	if u.lifeline != nil {
		u.lifeline.Close()
	}
	u.cmd, u.lifeline = cmd, w
	u.starts = append(u.starts, now)
	go func() { _ = cmd.Wait() }()
	// Wait until it answers.
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if _, err := localapi.NewClient(socket).Do(ctx, localapi.Request{Type: localapi.RequestInfo}); err == nil {
			return nil
		}
		time.Sleep(100 * time.Millisecond)
	}
	return errors.New("the background process did not start")
}

// stop closes the lifeline; the daemon stops its networks and exits.
func (u *userDaemon) stop() {
	u.mu.Lock()
	defer u.mu.Unlock()
	if u.lifeline != nil {
		u.lifeline.Close()
		u.lifeline = nil
	}
}

// openDaemonLog appends the child's output to the user state directory.
func openDaemonLog() (*os.File, error) {
	dir := os.Getenv("XDG_STATE_HOME")
	if dir == "" {
		home, _ := os.UserHomeDir()
		dir = filepath.Join(home, ".local", "state")
	}
	dir = filepath.Join(dir, "easytier-mygo")
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return nil, err
	}
	return os.OpenFile(filepath.Join(dir, "daemon.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
}
