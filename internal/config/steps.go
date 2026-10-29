package config

import (
	"sort"
	"time"
)

// StepType names a typed building block.
type StepType string

// The building blocks. Everything except command is typed: its placeholders are filled into single
// arguments and never pass through a shell.
const (
	StepSetEnv         StepType = "set-env"
	StepComposePull    StepType = "compose-pull"
	StepComposeUp      StepType = "compose-up"
	StepGitCheckout    StepType = "git-checkout"
	StepBackup         StepType = "backup"
	StepCheckContainer StepType = "check-container"
	StepCheckHTTP      StepType = "check-http"
	StepCommand        StepType = "command"
)

// StepTypes lists every building block in a stable order.
func StepTypes() []StepType {
	return []StepType{StepSetEnv, StepComposePull, StepComposeUp, StepGitCheckout, StepBackup,
		StepCheckContainer, StepCheckHTTP, StepCommand}
}

// Known reports whether t is a building block.
func (t StepType) Known() bool {
	_, ok := stepFields[t]
	return ok
}

// DefaultTimeout is the time limit of a step that sets none.
func DefaultTimeout(t StepType) time.Duration {
	switch t {
	case StepSetEnv:
		return 10 * time.Second
	case StepComposePull:
		return 10 * time.Minute
	case StepComposeUp:
		return 5 * time.Minute
	case StepGitCheckout:
		return 2 * time.Minute
	case StepBackup:
		return 30 * time.Minute
	case StepCheckContainer:
		return 2 * time.Minute
	case StepCheckHTTP:
		return time.Minute
	case StepCommand:
		return 10 * time.Minute
	}
	return 0
}

// Step is one building block as written in a template or a project. Which fields apply depends on Type.
type Step struct {
	Type    StepType `yaml:"type,omitempty"`
	Timeout Duration `yaml:"timeout,omitempty"`
	// set-env: KEY=value assignments.
	Assign []string `yaml:"assign,omitempty"`
	// compose-pull, compose-up, check-container.
	Services []string `yaml:"services,omitempty"`
	// check-container.
	Containers []string `yaml:"containers,omitempty"`
	// git-checkout.
	Tag string `yaml:"tag,omitempty"`
	// backup: Command (command mode) or Volume (volume mode), the number of copies to keep, and an
	// optional restore command.
	Command        string `yaml:"command,omitempty"`
	Volume         string `yaml:"volume,omitempty"`
	Keep           int    `yaml:"keep,omitempty"`
	RestoreCommand string `yaml:"restore_command,omitempty"`
	// check-http.
	URL      string `yaml:"url,omitempty"`
	Status   int    `yaml:"status,omitempty"`
	Contains string `yaml:"contains,omitempty"`
	// command: a free shell command line.
	Run string `yaml:"run,omitempty"`
}

// stepFields lists the fields each type accepts besides type and timeout.
var stepFields = map[StepType][]string{
	StepSetEnv:         {"assign"},
	StepComposePull:    {"services"},
	StepComposeUp:      {"services"},
	StepGitCheckout:    {"tag"},
	StepBackup:         {"command", "volume", "keep", "restore_command"},
	StepCheckContainer: {"services", "containers"},
	StepCheckHTTP:      {"url", "status", "contains"},
	StepCommand:        {"run"},
}

// setFields returns the names of the type-specific fields that carry a value, in a stable order.
func (s Step) setFields() []string {
	var out []string
	add := func(name string, set bool) {
		if set {
			out = append(out, name)
		}
	}
	add("assign", len(s.Assign) > 0)
	add("services", len(s.Services) > 0)
	add("containers", len(s.Containers) > 0)
	add("tag", s.Tag != "")
	add("command", s.Command != "")
	add("volume", s.Volume != "")
	add("keep", s.Keep != 0)
	add("restore_command", s.RestoreCommand != "")
	add("url", s.URL != "")
	add("status", s.Status != 0)
	add("contains", s.Contains != "")
	add("run", s.Run != "")
	sort.Strings(out)
	return out
}

// bare reports whether a step carries no parameters of its own.
func (s Step) bare() bool { return len(s.setFields()) == 0 }

// Assignment is one KEY=value pair for the env file.
type Assignment struct {
	Key   string
	Value string
}

// BackupParams are the resolved parameters of a backup step: exactly one of Command and Volume is set.
type BackupParams struct {
	Command        string
	Volume         string
	Keep           int
	RestoreCommand string
}

// ResolvedStep is a step with every placeholder filled in and every default applied. It carries all
// parameters an executor needs. Command, Shell and the backup commands are shell command lines whose
// placeholder values are single-quoted; all other fields are plain arguments for direct execution.
type ResolvedStep struct {
	Type           StepType
	Timeout        time.Duration
	Dir            string
	EnvFile        string
	ComposeFiles   []string
	ComposeProject string
	Services       []string
	Assignments    []Assignment
	Tag            string
	Backup         *BackupParams
	Containers     []string
	URL            string
	Status         int
	Contains       string
	Shell          string
}

// IsFree reports whether the step runs a free shell command line, which no typed block constrains.
func (s ResolvedStep) IsFree() bool {
	if s.Type == StepCommand {
		return true
	}
	return s.Backup != nil && (s.Backup.Command != "" || s.Backup.RestoreCommand != "")
}
