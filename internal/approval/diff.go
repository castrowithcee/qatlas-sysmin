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

func leaves(doc map[string]any) map[string]string {
	out := map[string]string{}
	flatten("", doc, out)
	return out
}

func isFree(path string) bool {
	i := strings.LastIndex(path, ".")
	return freeKeys[path[i+1:]] && !strings.HasPrefix(path, keyCredentials+".")
}

// diff lists the differences between two canonical documents, sorted by path. A nil side lists
// everything of the other as new or removed.
func diff(old, cur map[string]any) []Change {
	oldLeaves := leaves(old)
	newLeaves := leaves(cur)
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
	return changes
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

// diffStates compares two project states; a nil side is absent.
func diffStates(old, cur *state) ([]Change, error) {
	var o, c map[string]any
	var err error
	if old != nil {
		if o, err = old.canonical(); err != nil {
			return nil, err
		}
	}
	if cur != nil {
		if c, err = cur.canonical(); err != nil {
			return nil, err
		}
	}
	return diff(o, c), nil
}
