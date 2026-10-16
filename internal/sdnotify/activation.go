package sdnotify

import (
	"net"
	"os"
	"strconv"
)

// listenFDsStart is the first file descriptor systemd passes (SD_LISTEN_FDS_START).
const listenFDsStart = 3

// Listeners returns the sockets systemd passed through socket activation
// (LISTEN_PID/LISTEN_FDS), or nil when the process was not socket-activated.
// Side effects: unsets the LISTEN_* variables so children do not inherit them.
func Listeners() ([]net.Listener, error) {
	pid, err := strconv.Atoi(os.Getenv("LISTEN_PID"))
	if err != nil || pid != os.Getpid() {
		return nil, nil
	}
	n, err := strconv.Atoi(os.Getenv("LISTEN_FDS"))
	if err != nil || n <= 0 {
		return nil, nil
	}
	os.Unsetenv("LISTEN_PID")
	os.Unsetenv("LISTEN_FDS")
	os.Unsetenv("LISTEN_FDNAMES")
	var out []net.Listener
	for fd := listenFDsStart; fd < listenFDsStart+n; fd++ {
		f := os.NewFile(uintptr(fd), "LISTEN_FD_"+strconv.Itoa(fd))
		ln, err := net.FileListener(f)
		f.Close()
		if err != nil {
			for _, l := range out {
				l.Close()
			}
			return nil, err
		}
		out = append(out, ln)
	}
	return out, nil
}
