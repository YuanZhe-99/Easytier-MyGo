// Package sdnotify speaks the systemd notification protocol — enough of
// it for Type=notify services and WatchdogSec: READY=1, WATCHDOG=1 and
// STOPPING=1.
//
// The point of the watchdog is that it is *external*: systemd (a separate
// process) kills and restarts the service when the notifications stop,
// which is what catches a whole process that is alive but hung. An
// in-process timer cannot cover that case, so this code only ever reports
// progress; the decision belongs to systemd.
package sdnotify

import (
	"net"
	"os"
	"sync"
)

// Notifier sends notifications to the service manager. A nil Notifier is
// valid and does nothing, so callers never branch on the environment.
type Notifier struct {
	addr string
	mu   sync.Mutex
	conn net.Conn
}

// EnvSocket is the environment variable systemd sets.
const EnvSocket = "NOTIFY_SOCKET"

// FromEnv returns a notifier for the service manager that started this
// process, or nil when it was not started by one (a foreground run, a
// different init, tests).
func FromEnv() *Notifier {
	addr := os.Getenv(EnvSocket)
	if addr == "" {
		return nil
	}
	return &Notifier{addr: addr}
}

// Ready reports that startup finished and the service can serve.
func (n *Notifier) Ready() error { return n.send("READY=1") }

// Watchdog reports continued progress: the check loop is still advancing.
func (n *Notifier) Watchdog() error { return n.send("WATCHDOG=1") }

// Stopping reports an orderly shutdown in progress.
func (n *Notifier) Stopping() error { return n.send("STOPPING=1") }

func (n *Notifier) send(msg string) error {
	if n == nil {
		return nil
	}
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.conn == nil {
		conn, err := dial(n.addr)
		if err != nil {
			return err
		}
		n.conn = conn
	}
	if _, err := n.conn.Write([]byte(msg)); err != nil {
		// The service manager may have restarted its socket; reconnect
		// once on the next call.
		n.conn.Close()
		n.conn = nil
		return err
	}
	return nil
}

// dial connects to the notification socket. systemd passes either an
// absolute path or an abstract socket name beginning with '@'.
func dial(addr string) (net.Conn, error) {
	return net.Dial("unixgram", addr)
}
