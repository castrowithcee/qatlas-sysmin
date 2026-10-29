package config

import (
	"bytes"
	"errors"
	"fmt"
	"io"

	"go.yaml.in/yaml/v3"
)

// SchemaError reports a file whose schema version this package does not read.
type SchemaError struct {
	Got       int
	Supported int
}

func (e *SchemaError) Error() string {
	switch {
	case e.Got == 0:
		return fmt.Sprintf("missing schema version: add \"schema: %d\"", e.Supported)
	case e.Got > e.Supported:
		return fmt.Sprintf("schema version %d is newer than the supported version %d: upgrade sysmin", e.Got, e.Supported)
	}
	return fmt.Sprintf("schema version %d is not supported (supported: %d)", e.Got, e.Supported)
}

// decode reads one YAML document into a T. The schema version is checked on its own first, so a newer file
// reports its version instead of a list of unknown fields. Unknown fields and extra documents are errors.
func decode[T any](data []byte) (T, error) {
	var zero, out T
	var head struct {
		Schema int `yaml:"schema"`
	}
	if err := yaml.Unmarshal(data, &head); err != nil {
		return zero, fmt.Errorf("invalid YAML: %w", err)
	}
	if head.Schema != CurrentSchema {
		return zero, &SchemaError{Got: head.Schema, Supported: CurrentSchema}
	}
	dec := yaml.NewDecoder(bytes.NewReader(data))
	dec.KnownFields(true)
	if err := dec.Decode(&out); err != nil {
		return zero, fmt.Errorf("invalid configuration: %w", err)
	}
	var extra yaml.Node
	if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
		return zero, errors.New("invalid configuration: more than one YAML document")
	}
	return out, nil
}

// encode writes v as readable YAML with the schema version in place.
func encode(v any) ([]byte, error) {
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// DecodeSettings reads sysmin.yaml.
func DecodeSettings(data []byte) (Settings, error) { return decode[Settings](data) }

// DecodeProject reads a project file.
func DecodeProject(data []byte) (Project, error) { return decode[Project](data) }

// DecodeTemplate reads a template file.
func DecodeTemplate(data []byte) (Template, error) { return decode[Template](data) }

// EncodeSettings writes sysmin.yaml; a zero schema becomes the current one.
func EncodeSettings(s Settings) ([]byte, error) {
	if s.Schema == 0 {
		s.Schema = CurrentSchema
	}
	return encode(s)
}

// EncodeProject writes a project file; a zero schema becomes the current one.
func EncodeProject(p Project) ([]byte, error) {
	if p.Schema == 0 {
		p.Schema = CurrentSchema
	}
	return encode(p)
}

// EncodeTemplate writes a template file; a zero schema becomes the current one.
func EncodeTemplate(t Template) ([]byte, error) {
	if t.Schema == 0 {
		t.Schema = CurrentSchema
	}
	return encode(t)
}
