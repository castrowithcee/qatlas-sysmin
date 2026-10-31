package hostadmin

import (
	"errors"
	"io/fs"
	"strconv"
	"strings"
	"time"
)

type shadowEntry struct {
	locked bool
	// expire is the day since 1970-01-01 an account ends; empty or unusable means none.
	expire string
}

// expired reports whether the account has ended; nil when the field cannot be read.
func (s shadowEntry) expired(now time.Time) *bool {
	if s.expire == "" {
		no := false
		return &no
	}
	day, err := strconv.ParseInt(s.expire, 10, 64)
	if err != nil {
		return nil
	}
	if day < 0 {
		no := false
		return &no
	}
	yes := now.UTC().Unix()/86400 >= day
	return &yes
}

// readShadow returns the entries by user name. Without privilege or without a shadow file it returns an
// empty map, so the fields stay unknown.
func (h *Host) readShadow() (map[string]shadowEntry, error) {
	if !h.Privileged {
		return nil, nil
	}
	rows, err := readLines(h.ShadowPath)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := map[string]shadowEntry{}
	for _, f := range rows {
		if len(f) < 2 {
			continue
		}
		e := shadowEntry{
			// A leading "!" is a locked password and a leading "*" a password that can never match; either
			// way no password login is possible.
			locked: strings.HasPrefix(f[1], "!") || strings.HasPrefix(f[1], "*"),
		}
		if len(f) > 7 {
			e.expire = f[7]
		}
		out[f[0]] = e
	}
	return out, nil
}
