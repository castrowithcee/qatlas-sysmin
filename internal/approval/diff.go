package approval

import (
	"fmt"
	"sort"
	"strings"
)

// Kind says what an open approval is about.
type Kind string

// The kinds of open approval.
const (
	// KindNew is a project that was never approved.
	KindNew Kind = "new"
	// KindChanged is an approved project whose definition differs from the approved one.
	KindChanged Kind = "changed"
	// KindDeleted is an approved project whose file is gone.
	KindDeleted Kind = "deleted"
	// KindCredential is an approved project whose definition is unchanged but whose credential is another
	// vault entry now, for instance one created anew under the same name.
	KindCredential Kind = "credential"
)

// Change is one differing field. Path is dotted with [i] for list items, and credentials.<name> for the
// vault id of a credential. Old and New are empty when the field is absent on that side. Free marks a free
// command line, which no typed building block constrains.
type Change struct {
	Path string
	Old  string
	New  string
	Free bool
}

// Item is one open approval.
type Item struct {
	Kind    Kind
	Project string
	// Fingerprint is the current state's; empty for a deleted project. Hand it to Approve to approve
	// exactly the state that was shown.
	Fingerprint string
	Changes     []Change
	// Unverified means the approved snapshot is missing or does not match the vault, so Changes list the
	// whole current state instead of a difference.
	Unverified bool
}

// FreeCommands reports whether a free command line was added, changed or removed.
func (i Item) FreeCommands() bool {
	for _, c := range i.Changes {
		if c.Free {
			return true
		}
	}
	return false
}

// freeKeys are the fields that hold a shell command line.
var freeKeys = map[string]bool{"run": true, "command": true, "restore_command": true}

// flatten writes the leaves of v into out under dotted paths. The project's own step list is left out; the
// effective steps carry it.
func flatten(prefix string, v any, out map[string]string) {
	switch x := v.(type) {
	case map[string]any:
		for k, sub := range x {
			p := k
			if prefix != "" {
				p = prefix + "." + k
			}
			if p == keyProject+"."+keySteps {
				continue
			}
			flatten(p, sub, out)
		}
	case []any:
		for i, sub := range x {
			flatten(fmt.Sprintf("%s[%d]", prefix, i), sub, out)
		}
	case nil:
	default:
		out[prefix] = fmt.Sprint(x)
	}
}

func leaves(s state) (map[string]string, error) {
	doc, err := s.canonical()
	if err != nil {
		return nil, err
	}
	out := map[string]string{}
	flatten("", doc, out)
	return out, nil
}

func isFree(path string) bool {
	i := strings.LastIndex(path, ".")
	return freeKeys[path[i+1:]] && !strings.HasPrefix(path, keyCredentials+".")
}

// diff lists the differences between two states, sorted by path. A nil old side lists everything as new.
func diff(old *state, cur *state) ([]Change, error) {
	oldLeaves := map[string]string{}
	if old != nil {
		l, err := leaves(*old)
		if err != nil {
			return nil, err
		}
		oldLeaves = l
	}
	newLeaves := map[string]string{}
	if cur != nil {
		l, err := leaves(*cur)
		if err != nil {
			return nil, err
		}
		newLeaves = l
	}
	paths := map[string]bool{}
	for p := range oldLeaves {
		paths[p] = true
	}
	for p := range newLeaves {
		paths[p] = true
	}
	var changes []Change
	for p := range paths {
		o, oi := oldLeaves[p]
		n, ni := newLeaves[p]
		if oi && ni && o == n {
			continue
		}
		changes = append(changes, Change{Path: p, Old: o, New: n, Free: isFree(p)})
	}
	sort.Slice(changes, func(i, j int) bool { return changes[i].Path < changes[j].Path })
	return changes, nil
}

// onlyCredentials reports whether every change is a credential id.
func onlyCredentials(changes []Change) bool {
	for _, c := range changes {
		if !strings.HasPrefix(c.Path, keyCredentials+".") {
			return false
		}
	}
	return len(changes) > 0
}
