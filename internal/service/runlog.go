package service

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/castrowithcee/qatlas-sysmin/internal/auditlog"
	"github.com/castrowithcee/qatlas-sysmin/internal/config"
	"github.com/castrowithcee/qatlas-sysmin/internal/control"
	"github.com/castrowithcee/qatlas-sysmin/internal/daemon"
)

func (s *Service) registerRunLog() error {
	return s.server.Register(control.OpDeployLog, daemon.Handler{Action: auditlog.OperationDeployRead, Func: s.handleDeployLog})
}

// handleDeployLog returns the end of the log of one run. The engine writes the log through the redactor of
// the run's approved credentials, so no secret of that run is in the file; the service holds no redactor of
// its own. The file is named only by the engine from the project and the run ID, never from a path of the
// request, and neither the path nor an operating system error leaves the service.
func (s *Service) handleDeployLog(_ context.Context, c *daemon.Conn, params json.RawMessage) (any, error) {
	var p control.DeployLogParams
	if err := json.Unmarshal(params, &p); err != nil || !config.ValidName(p.Project) || p.Run == "" || len(p.Run) > 64 {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	path, err := s.engine.RunLogPath(p.Project, p.Run+".log")
	if err != nil {
		return nil, &control.Error{Code: control.CodeBadRequest}
	}
	if err := s.existing(p.Project); err != nil {
		return nil, audited(c, p.Project, err)
	}
	text, truncated, err := readLogTail(path, control.RunLogMaxBytes)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return nil, audited(c, p.Project, &control.Error{Code: control.CodeNotFound})
	case err != nil:
		return nil, audited(c, p.Project, internalError("cannot read a run log", err))
	}
	c.Audit(p.Project, "")
	return control.DeployLogResult{Project: p.Project, Run: p.Run, Text: text, Truncated: truncated}, nil
}

// readLogTail reads at most limit bytes from the end of a regular file. A cut log starts at the next line,
// so no half line or half character is shown.
func readLogTail(path string, limit int64) (string, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", false, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return "", false, err
	}
	if !info.Mode().IsRegular() {
		return "", false, fs.ErrNotExist
	}
	size := info.Size()
	truncated := size > limit
	if truncated {
		if _, err := f.Seek(size-limit, io.SeekStart); err != nil {
			return "", false, err
		}
	}
	data, err := io.ReadAll(io.LimitReader(f, limit))
	if err != nil {
		return "", false, err
	}
	if truncated {
		if i := strings.IndexByte(string(data), '\n'); i >= 0 {
			data = data[i+1:]
		} else {
			data = nil
		}
	}
	return cleanLogText(string(data)), truncated, nil
}

// cleanLogText drops everything a terminal would act on: escape sequences and control characters other than
// the line break and the tab, and invalid UTF-8.
func cleanLogText(s string) string {
	s = strings.ToValidUTF8(s, "�")
	var b strings.Builder
	b.Grow(len(s))
	for i := 0; i < len(s); {
		r, n := utf8.DecodeRuneInString(s[i:])
		switch {
		case r == 0x1b:
			i += skipEscape(s[i:])
			continue
		case r == '\n' || r == '\t':
			b.WriteRune(r)
		case r == '\r':
			if !strings.HasPrefix(s[i+n:], "\n") {
				b.WriteByte('\n')
			}
		case r < ' ' || (r >= 0x7f && r < 0xa0):
		default:
			b.WriteRune(r)
		}
		i += n
	}
	return b.String()
}

// skipEscape is the length of the escape sequence at the start of s: CSI up to its final byte, OSC up to
// BEL or ST, else the escape and one following byte.
func skipEscape(s string) int {
	if len(s) < 2 {
		return len(s)
	}
	switch s[1] {
	case '[':
		for i := 2; i < len(s); i++ {
			if s[i] >= 0x40 && s[i] <= 0x7e {
				return i + 1
			}
		}
	case ']':
		for i := 2; i < len(s); i++ {
			if s[i] == 0x07 {
				return i + 1
			}
			if s[i] == 0x1b && i+1 < len(s) && s[i+1] == '\\' {
				return i + 2
			}
		}
	default:
		return 2
	}
	return len(s)
}
