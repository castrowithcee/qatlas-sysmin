package config

import (
	"fmt"
	"strings"
)

// Problem codes. Each names one rule; Hint says how to resolve it.
const (
	CodeInvalidName        = "invalid-name"
	CodeDuplicateName      = "duplicate-name"
	CodeUnknownTemplate    = "unknown-template"
	CodeUnknownStepType    = "unknown-step-type"
	CodeFieldNotAllowed    = "field-not-allowed"
	CodeMissingField       = "missing-field"
	CodeInvalidValue       = "invalid-value"
	CodeInvalidDuration    = "invalid-duration"
	CodeInvalidPlaceholder = "invalid-placeholder"
	CodeUnknownPlaceholder = "unknown-placeholder"
	CodeListPlaceholder    = "list-placeholder"
	CodeReservedValueName  = "reserved-value-name"
	CodeRelativePath       = "relative-path"
	CodeInvalidPath        = "invalid-path"
	CodeUnknownCredential  = "unknown-credential"
	CodeInvalidVersionRule = "invalid-version-rule"
	CodeInvalidTagFilter   = "invalid-tag-filter"
	CodeSourceMissing      = "source-missing"
	CodeSourceConflict     = "source-conflict"
	CodeDuplicateEnvVar    = "duplicate-env-var"
	CodeUnknownChannel     = "unknown-channel"
	CodeInvalidChannel     = "invalid-channel"
	CodeInvalidWebhook     = "invalid-webhook"
	CodeSecretInURL        = "secret-in-url"
	CodeStepsMissing       = "steps-missing"
	CodeMissingEnvFile     = "missing-env-file"
	CodeMissingEnvVar      = "missing-env-var"
	CodeInvalidTarget      = "invalid-target"
	CodeQuotedPlaceholder  = "quoted-placeholder"
	CodeRegistryCredential = "registry-credential-conflict"
)

var hints = map[string]string{
	CodeInvalidName:        "Use lower-case letters, digits and hyphens, starting with a letter or digit.",
	CodeDuplicateName:      "Give each entry a unique name.",
	CodeUnknownTemplate:    "Use an embedded template (image-tag, git-tag) or add a template file with that name.",
	CodeUnknownStepType:    "Use one of: set-env, compose-pull, compose-up, git-checkout, backup, check-container, check-http, command.",
	CodeFieldNotAllowed:    "Remove the field; this step type does not use it.",
	CodeMissingField:       "Add the missing field.",
	CodeInvalidValue:       "Correct the value.",
	CodeInvalidDuration:    "Use a duration such as \"30s\" or \"5m\" within the allowed range.",
	CodeInvalidPlaceholder: "Write placeholders as {name}; write a literal brace as {{ or }}.",
	CodeUnknownPlaceholder: "Use {version}, {previous_version}, {dir}, {env_file}, {var}, {services} or a key from the project values.",
	CodeListPlaceholder:    "Use {services} only as a whole list element, or inside a command line.",
	CodeReservedValueName:  "Rename the value; the name is a built-in placeholder.",
	CodeRelativePath:       "Use an absolute path.",
	CodeInvalidPath:        "Use an absolute path without \"..\" elements.",
	CodeUnknownCredential:  "Add the credential to the vault, or correct the name.",
	CodeInvalidVersionRule: "Use mode auto|manual, max_update patch|minor|major, major_steps direct|stepwise.",
	CodeInvalidTagFilter:   "Use a valid RE2 regular expression.",
	CodeSourceMissing:      "Add images or a git source.",
	CodeSourceConflict:     "Keep either images or a git source, not both.",
	CodeDuplicateEnvVar:    "Give each image its own environment variable.",
	CodeUnknownChannel:     "Add the channel to sysmin.yaml, or correct the name.",
	CodeInvalidChannel:     "Correct the channel definition.",
	CodeInvalidWebhook:     "Use kind github|gitlab|gitea|hmac and a secret credential name.",
	CodeSecretInURL:        "Remove the secret from the URL; keep it in the vault and reference its name.",
	CodeStepsMissing:       "Set a template or give the project its own steps.",
	CodeMissingEnvFile:     "Set env_file to an absolute path.",
	CodeMissingEnvVar:      "Set env_var on every image, or stop using {var}.",
	CodeQuotedPlaceholder:  "Placeholders are quoted automatically; remove the quotes around them.",
	CodeRegistryCredential: "Use the same credential for all images of this project on that registry.",
	CodeInvalidTarget:      "Pass exactly the images (or the git source) of the project, each with a version and a previous version.",
}

// Problem is one finding of the validator.
type Problem struct {
	// File is the file the problem belongs to, relative to the configuration root.
	File string
	// Project and Template name the entry inside the file, when there is one.
	Project  string
	Template string
	// Field is the path of the field, with list indices: images[1].version_rule.mode.
	Field   string
	Code    string
	Message string
	// Hint says how to resolve the problem.
	Hint string
}

// Error makes a Problem usable as an error.
func (p Problem) Error() string {
	var b strings.Builder
	b.WriteString(p.File)
	if p.Field != "" {
		b.WriteString(": ")
		b.WriteString(p.Field)
	}
	fmt.Fprintf(&b, ": %s (%s)", p.Message, p.Code)
	return b.String()
}

// scope adds problems for one file and entry.
type scope struct {
	out      *[]Problem
	file     string
	project  string
	template string
}

func (s scope) add(field, code, format string, args ...any) {
	*s.out = append(*s.out, Problem{
		File: s.file, Project: s.project, Template: s.template, Field: field,
		Code: code, Message: fmt.Sprintf(format, args...), Hint: hints[code],
	})
}

// ProblemsError reports a list of problems as one error.
type ProblemsError struct{ Problems []Problem }

func (e *ProblemsError) Error() string {
	lines := make([]string, len(e.Problems))
	for i, p := range e.Problems {
		lines[i] = p.Error()
	}
	return strings.Join(lines, "\n")
}
