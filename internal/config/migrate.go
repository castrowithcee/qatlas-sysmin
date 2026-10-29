package config

import (
	"bytes"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// schemaVersion is the version files are written with and the newest one read. It is a variable only so
// tests can simulate a later schema.
var schemaVersion = CurrentSchema

// migration rewrites the top-level mapping of a file from schema version N to N+1. It must not change what
// the file resolves to. The schema key is set by migrate.
type migration func(doc *yaml.Node) error

// migrations maps a schema version to the step that upgrades files of that version. The map is empty while
// version 1 is the only schema; tests install steps.
var migrations = map[int]migration{}

// needsMigration reports whether data is a file of an older, known schema version. Files of version 0
// (missing) or of a newer version are not migrated; decoding reports them.
func needsMigration(data []byte) (int, bool) {
	var head struct {
		Schema int `yaml:"schema"`
	}
	if yaml.Unmarshal(data, &head) != nil {
		return 0, false
	}
	return head.Schema, head.Schema >= 1 && head.Schema < schemaVersion
}

// migrate runs the step chain from the file's version up to the current one and returns the YAML of the
// migrated file.
func migrate(data []byte, from int) ([]byte, error) {
	var doc yaml.Node
	if err := yaml.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("invalid YAML: %w", err)
	}
	if doc.Kind != yaml.DocumentNode || len(doc.Content) != 1 || doc.Content[0].Kind != yaml.MappingNode {
		return nil, fmt.Errorf("cannot migrate: the file is not a YAML mapping")
	}
	root := doc.Content[0]
	for v := from; v < schemaVersion; v++ {
		step, ok := migrations[v]
		if !ok {
			return nil, fmt.Errorf("no migration from schema version %d to %d", v, v+1)
		}
		if err := step(root); err != nil {
			return nil, fmt.Errorf("migration from schema version %d to %d: %w", v, v+1, err)
		}
		setSchema(root, v+1)
	}
	var buf bytes.Buffer
	enc := yaml.NewEncoder(&buf)
	enc.SetIndent(2)
	if err := enc.Encode(&doc); err != nil {
		return nil, err
	}
	if err := enc.Close(); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func setSchema(root *yaml.Node, v int) {
	val := &yaml.Node{Kind: yaml.ScalarNode, Tag: "!!int", Value: fmt.Sprint(v)}
	for i := 0; i+1 < len(root.Content); i += 2 {
		if root.Content[i].Value == "schema" {
			root.Content[i+1] = val
			return
		}
	}
	root.Content = append([]*yaml.Node{{Kind: yaml.ScalarNode, Tag: "!!str", Value: "schema"}, val}, root.Content...)
}
