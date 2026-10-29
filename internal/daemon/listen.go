package daemon

import (
	"errors"
	"fmt"
	"io/fs"
	"net"
	"os"
	"time"
)

// SocketMode is the mode of the socket file; the group comes from the directory the service runs in.
const SocketMode = 0o660

// maxSocketPath is the longest path a Unix socket address holds, with its terminating byte.
const maxSocketPath = 107

// ErrRunning reports a live service already listening at the path.
var ErrRunning = errors.New("a service is already listening at the socket path")

// Listen opens the control socket at path. A socket left at the path is replaced when nothing answers on
// it; anything else at the path, or a socket that answers, is left alone and refused. The directory is not
// created: the service manager provides it.
func Listen(path string) (*net.UnixListener, error) {
	if len(path) > maxSocketPath {
		return nil, fmt.Errorf("the socket path is longer than %d bytes", maxSocketPath)
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return nil, fmt.Errorf("cannot inspect the socket path: %w", err)
	case info.Mode().Type() != fs.ModeSocket:
		return nil, errors.New("the socket path exists and is not a socket; it is left alone")
	default:
		if c, err := net.DialTimeout("unix", path, time.Second); err == nil {
			_ = c.Close()
			return nil, ErrRunning
		}
		if err := os.Remove(path); err != nil {
			return nil, fmt.Errorf("cannot replace the stale socket: %w", err)
		}
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, fmt.Errorf("cannot listen on the socket path: %w", err)
	}
	// Closing the listener removes the socket file.
	ln.SetUnlinkOnClose(true)
	if err := os.Chmod(path, SocketMode); err != nil {
		_ = ln.Close()
		return nil, fmt.Errorf("cannot set the socket's mode: %w", err)
	}
	return ln, nil
}
