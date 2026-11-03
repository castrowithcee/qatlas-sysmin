package control

import (
	"fmt"
	"path/filepath"
	"strings"

	"github.com/castrowithcee/qatlas-kit/localipc"
)

// socketSuffix is the ending the channel gives every socket file.
const socketSuffix = ".sock"

// Endpoint is the service channel whose socket is at path. The path must be absolute and clean and end in
// .sock, so the socket the channel opens is the one the path names.
func Endpoint(path string) (localipc.ServiceEndpoint, error) {
	base, ok := strings.CutSuffix(filepath.Base(path), socketSuffix)
	ep := localipc.ServiceEndpoint{
		Dir: filepath.Dir(path), SocketBase: base, ServiceUser: ServiceUser, Group: ServiceGroup,
		ErrorName: "control socket",
	}
	if !ok {
		return localipc.ServiceEndpoint{}, fmt.Errorf("the socket path %q does not end in %s", path, socketSuffix)
	}
	got, err := ep.Path()
	if err != nil {
		return localipc.ServiceEndpoint{}, err
	}
	if got != path {
		return localipc.ServiceEndpoint{}, fmt.Errorf("the socket path %q is not an absolute, clean path", path)
	}
	return ep, nil
}
