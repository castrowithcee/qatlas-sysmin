// Package output turns typed core results into the output formats, applies projection, and defines the
// provider-independent error codes. Encoders never reach into providers: they only render what the core
// produced.
package output

import (
	"fmt"
	"strings"
)

// Format is a rendering of a result.
type Format string

// The output formats this build renders. They render scalar data.
const (
	FormatTable   Format = "table"
	FormatJSON    Format = "json"
	FormatCompact Format = "compact"
)

// ParseFormat validates a format name.
func ParseFormat(s string) (Format, error) {
	switch Format(s) {
	case FormatTable, FormatJSON, FormatCompact:
		return Format(s), nil
	}
	return "", fmt.Errorf("unknown output format %q, want %s, %s, or %s",
		s, FormatTable, FormatJSON, FormatCompact)
}

// Field is one named value of an object. The formats expect scalars: string, bool, int64, float64, or nil.
type Field struct {
	Name  string
	Value any
}

// Object is a single record with a stable field order.
type Object struct {
	Fields []Field
}

// Row holds the values of one collection record, addressed by column name. A missing key renders as an
// empty field and as JSON null.
type Row map[string]any

// Collection is an ordered list of records sharing one column order.
type Collection struct {
	Columns []string
	Rows    []Row
}

// Result is what a command hands to the encoders.
type Result interface{ isResult() }

func (Object) isResult()     {}
func (Collection) isResult() {}

// ProjectionError reports a field in --fields that cannot be used. It is a usage problem.
type ProjectionError struct {
	Field     string
	Available []string
	Duplicate bool
}

func (e *ProjectionError) Error() string {
	if e.Duplicate {
		return fmt.Sprintf("field %q is requested more than once", e.Field)
	}
	return fmt.Sprintf("unknown field %q, available fields are %s", e.Field, strings.Join(e.Available, ", "))
}

// Project restricts a result to the named fields, in the order given. An empty list keeps everything.
// Validation is central so the same field names can later be pushed down to a provider.
func Project(result Result, fields []string) (Result, error) {
	if len(fields) == 0 {
		return result, nil
	}

	// A repeated name would produce a duplicate key in the JSON output.
	seen := make(map[string]bool, len(fields))
	for _, f := range fields {
		if seen[f] {
			return nil, &ProjectionError{Field: f, Duplicate: true}
		}
		seen[f] = true
	}

	switch r := result.(type) {
	case Collection:
		available := r.Columns
		columns := make([]string, 0, len(fields))
		for _, f := range fields {
			if !contains(available, f) {
				return nil, &ProjectionError{Field: f, Available: available}
			}
			columns = append(columns, f)
		}
		return Collection{Columns: columns, Rows: r.Rows}, nil

	case Object:
		available := make([]string, len(r.Fields))
		byName := make(map[string]Field, len(r.Fields))
		for i, f := range r.Fields {
			available[i] = f.Name
			byName[f.Name] = f
		}
		out := make([]Field, 0, len(fields))
		for _, name := range fields {
			f, ok := byName[name]
			if !ok {
				return nil, &ProjectionError{Field: name, Available: available}
			}
			out = append(out, f)
		}
		return Object{Fields: out}, nil
	}
	return result, nil
}

// Code is an error class that callers can branch on instead of parsing messages.
type Code string

// The error codes this build can emit.
const (
	CodeUsage          Code = "usage"
	CodeInvalidRequest Code = "invalid-request"
	CodePermission     Code = "permission"
	CodeNotFound       Code = "not-found"
	CodeTimeout        Code = "timeout"
	CodeRuntime        Code = "runtime"
)

// AllCodes lists every error code this build can emit.
func AllCodes() []Code {
	return []Code{CodeUsage, CodeInvalidRequest, CodePermission, CodeNotFound, CodeTimeout, CodeRuntime}
}

func contains(haystack []string, needle string) bool {
	for _, s := range haystack {
		if s == needle {
			return true
		}
	}
	return false
}
