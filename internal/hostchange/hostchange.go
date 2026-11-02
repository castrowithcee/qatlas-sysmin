// Package hostchange is the framework for changes to the host that run as root: an action describes itself
// in plain words and exact commands, the runner records the previous state in a root-only journal before it
// changes anything, checks the result, and restores the previous state when the change or its check fails.
// Default registers the production actions, which are the user actions of users.go.
package hostchange

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
)

// DefaultSecretLabel stands for a secret input wherever a command is shown.
const DefaultSecretLabel = "<password>"

// Command is one program call or one built-in step. Argv is shown and run exactly as it is. A secret goes to
// the program's standard input only; it is never part of Argv, of the displayed text or of any output.
type Command struct {
	Argv []string
	// Builtin marks a step that sysmin runs itself instead of a program; Argv[0] names one of builtinSteps.
	Builtin bool
	// Secret is the standard input of the program, shown as SecretLabel.
	Secret []byte
	// SecretLabel replaces the secret in the displayed command; empty means DefaultSecretLabel.
	SecretLabel string
}

func (c Command) label() string {
	if c.SecretLabel != "" {
		return c.SecretLabel
	}
	return DefaultSecretLabel
}

// Display is the command as the confirmation, the journal and the audit entry show it.
func (c Command) Display() string {
	parts := make([]string, len(c.Argv))
	for i, a := range c.Argv {
		parts[i] = shellQuote(a)
	}
	text := strings.Join(parts, " ")
	if c.Builtin {
		text = builtinPrefix + text
	}
	if len(c.Secret) > 0 {
		text += " < " + c.label()
	}
	return text
}

// String, GoString and MarshalJSON keep the secret out of every formatting verb and encoder.
func (c Command) String() string   { return c.Display() }
func (c Command) GoString() string { return c.Display() }
func (c Command) MarshalJSON() ([]byte, error) {
	return json.Marshal(c.Display())
}

var plainWord = regexp.MustCompile(`^[A-Za-z0-9_@%+=:,./-]+$`)

func shellQuote(s string) string {
	if plainWord.MatchString(s) {
		return s
	}
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// Plan is what an action does with one set of arguments.
type Plan struct {
	// Sentence says in one sentence of plain words what happens.
	Sentence string
	// Commands run in this order, exactly as shown.
	Commands []Command
	// Capture reads the previous state of everything the change touches. The result is journaled before the
	// first command runs and is the only input of the inverse. It may hold sensitive data.
	Capture func(ctx context.Context) (json.RawMessage, error)
	// Seal, when set, runs after the commands that ran, whether or not they all succeeded, and returns the state
	// that replaces the captured one: it holds what is known only now, such as the UID the system assigned. An
	// error counts as a failed change.
	Seal func(ctx context.Context) (json.RawMessage, error)
	// Check runs after the commands; an error means the change is not acceptable and is taken back.
	Check func(ctx context.Context) error
	// NotReversible is the reason the change cannot be undone; empty means the definition's Inverse applies.
	NotReversible string
	// Redact lists further values that must never appear in an error text, beyond the command secrets: a
	// password inside a larger standard input, for example.
	Redact [][]byte
}

// Definition is a registered action.
type Definition struct {
	// ID is the fixed identifier, for example "group.add-member".
	ID string
	// WantsSecret makes the CLI read the secret input from standard input and pass it to Plan.
	WantsSecret bool
	// Plan validates the arguments and builds the plan. It reads nothing from the host: the TUI calls it to
	// show the confirmation before it asks for root.
	Plan func(args []string, secret []byte) (Plan, error)
	// Inverse builds the plan that returns to the captured state; nil means the action is never reversible.
	Inverse func(previous json.RawMessage) (Plan, error)
}

// ArgumentError marks arguments an action refuses.
type ArgumentError struct{ Msg string }

func (e *ArgumentError) Error() string { return e.Msg }

// Argf builds an ArgumentError.
func Argf(format string, a ...any) error { return &ArgumentError{fmt.Sprintf(format, a...)} }

// RefusedError marks a change that a protection rule or the state of the host refuses before anything runs.
type RefusedError struct{ Msg string }

func (e *RefusedError) Error() string { return e.Msg }

// Refusef builds a RefusedError.
func Refusef(format string, a ...any) error { return &RefusedError{fmt.Sprintf(format, a...)} }

type actorKey struct{}

// withActor hands the human behind the sudo call to Capture and Check.
func withActor(ctx context.Context, uid int) context.Context {
	return context.WithValue(ctx, actorKey{}, uid)
}

// ActingUID is the human behind the sudo call, as the runner received it; 0 means root itself or unknown.
func ActingUID(ctx context.Context) int {
	uid, _ := ctx.Value(actorKey{}).(int)
	return uid
}

func isRefusal(err error) (*RefusedError, bool) {
	var r *RefusedError
	ok := errors.As(err, &r)
	return r, ok
}

var idPattern = regexp.MustCompile(`^[a-z][a-z0-9-]*\.[a-z][a-z0-9-]*$`)

// Registry maps action identifiers to definitions.
type Registry struct{ defs map[string]Definition }

func NewRegistry() *Registry { return &Registry{defs: map[string]Definition{}} }

// Register adds an action; an invalid or duplicate identifier is a programming error and is returned.
func (r *Registry) Register(d Definition) error {
	if !idPattern.MatchString(d.ID) {
		return fmt.Errorf("invalid action id %q", d.ID)
	}
	if d.Plan == nil {
		return fmt.Errorf("action %s has no plan", d.ID)
	}
	if _, dup := r.defs[d.ID]; dup {
		return fmt.Errorf("action %s is registered twice", d.ID)
	}
	r.defs[d.ID] = d
	return nil
}

func (r *Registry) Lookup(id string) (Definition, bool) {
	d, ok := r.defs[id]
	return d, ok
}

// IDs lists the registered identifiers in order.
func (r *Registry) IDs() []string {
	ids := make([]string, 0, len(r.defs))
	for id := range r.defs {
		ids = append(ids, id)
	}
	sort.Strings(ids)
	return ids
}

// Describe builds the plan of an action without running or reading anything.
func (r *Registry) Describe(id string, args []string, secret []byte) (Plan, error) {
	d, ok := r.defs[id]
	if !ok {
		return Plan{}, fmt.Errorf("unknown action %q", id)
	}
	return d.Plan(args, secret)
}

// Default is the registry of the production actions.
func Default() *Registry {
	r := NewRegistry()
	if err := RegisterUserActions(r, DefaultUserHost()); err != nil {
		panic(err)
	}
	return r
}
