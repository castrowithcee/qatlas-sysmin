package notify

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

// EventType names what happened.
type EventType string

// The events that are reported. EventTest is only for a manual test of a channel.
const (
	EventDeploySucceeded EventType = "deploy.succeeded"
	EventDeployFailed    EventType = "deploy.failed"
	EventDeployRolled    EventType = "deploy.rolled_back"
	EventRollbackFailed  EventType = "rollback.failed"
	EventUpdateAvailable EventType = "update.available"
	EventApprovalsOpen   EventType = "approvals.pending"
	EventVaultLocked     EventType = "vault.locked"
	EventTest            EventType = "notify.test"
)

func (t EventType) valid() bool {
	switch t {
	case EventDeploySucceeded, EventDeployFailed, EventDeployRolled, EventRollbackFailed,
		EventUpdateAvailable, EventApprovalsOpen, EventVaultLocked, EventTest:
		return true
	}
	return false
}

// ImageVersion is the state of one image: the version running now, the one before and the target.
type ImageVersion struct {
	Image    string `json:"image"`
	Current  string `json:"current,omitempty"`
	Previous string `json:"previous,omitempty"`
	Target   string `json:"target,omitempty"`
}

// Event is what is reported. Time defaults to the moment of sending.
type Event struct {
	Type       EventType
	Project    string
	Images     []ImageVersion
	FailedStep string
	// Error is the failure text; it is redacted, cleaned and shortened before it leaves the process.
	Error string
	Time  time.Time
}

const (
	maxErrorRunes = 500
	maxFieldRunes = 128
	maxImages     = 20
)

// payload is the cleaned content of one message. It is built per send, after the credential is known, so
// the failure text can be redacted with it.
type payload struct {
	Event      EventType      `json:"event"`
	Host       string         `json:"host"`
	Project    string         `json:"project,omitempty"`
	Versions   []ImageVersion `json:"versions,omitempty"`
	FailedStep string         `json:"failed_step,omitempty"`
	Error      string         `json:"error,omitempty"`
	Time       time.Time      `json:"time"`
}

func (n *Notifier) payload(ev Event, redact func(string) string) payload {
	t := ev.Time
	if t.IsZero() {
		t = n.now()
	}
	p := payload{
		Event: ev.Type, Host: clean(n.hostname(), maxFieldRunes), Project: clean(ev.Project, maxFieldRunes),
		FailedStep: clean(ev.FailedStep, maxFieldRunes), Time: t.UTC().Truncate(time.Second),
		// Redact before shortening, so a cut can never leave half a secret behind.
		Error: clean(redact(ev.Error), maxErrorRunes),
	}
	for i, v := range ev.Images {
		if i == maxImages {
			break
		}
		p.Versions = append(p.Versions, ImageVersion{
			Image: clean(v.Image, maxFieldRunes), Current: clean(v.Current, maxFieldRunes),
			Previous: clean(v.Previous, maxFieldRunes), Target: clean(v.Target, maxFieldRunes),
		})
	}
	return p
}

// clean replaces control characters by spaces and cuts s to at most limit runes.
func clean(s string, limit int) string {
	s = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) {
			return ' '
		}
		return r
	}, strings.ToValidUTF8(s, "?"))
	s = strings.Join(strings.Fields(s), " ")
	if utf8.RuneCountInString(s) <= limit {
		return s
	}
	return string([]rune(s)[:limit-3]) + "..."
}

func (p payload) subject() string {
	s := fmt.Sprintf("[%s] %s", p.Host, p.Event)
	if p.Project != "" {
		s += " " + p.Project
	}
	return s
}

func (p payload) text() string {
	var b strings.Builder
	b.WriteString(p.subject() + "\n")
	if p.FailedStep != "" {
		fmt.Fprintf(&b, "Failed step: %s\n", p.FailedStep)
	}
	for _, v := range p.Versions {
		fmt.Fprintf(&b, "%s: current %s", v.Image, orDash(v.Current))
		if v.Previous != "" {
			fmt.Fprintf(&b, ", previous %s", v.Previous)
		}
		if v.Target != "" {
			fmt.Fprintf(&b, ", target %s", v.Target)
		}
		b.WriteString("\n")
	}
	if p.Error != "" {
		fmt.Fprintf(&b, "Error: %s\n", p.Error)
	}
	fmt.Fprintf(&b, "Time: %s\n", p.Time.Format(time.RFC3339))
	return b.String()
}

func orDash(s string) string {
	if s == "" {
		return "-"
	}
	return s
}
