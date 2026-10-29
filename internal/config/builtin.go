package config

import (
	"embed"
	"fmt"
	"sort"
	"sync"
)

//go:embed templates/*.yaml
var builtinFS embed.FS

var (
	builtinOnce      sync.Once
	builtinTemplates []Template
)

// BuiltinTemplates returns the embedded templates (image-tag, git-tag), sorted by name. They are starting
// points: a project may replace their steps with its own.
func BuiltinTemplates() []Template {
	builtinOnce.Do(func() {
		entries, err := builtinFS.ReadDir("templates")
		if err != nil {
			panic(err)
		}
		for _, e := range entries {
			data, err := builtinFS.ReadFile("templates/" + e.Name())
			if err != nil {
				panic(err)
			}
			t, err := DecodeTemplate(data)
			if err != nil {
				panic(fmt.Sprintf("embedded template %s: %v", e.Name(), err))
			}
			builtinTemplates = append(builtinTemplates, t)
		}
		sort.Slice(builtinTemplates, func(i, j int) bool { return builtinTemplates[i].Name < builtinTemplates[j].Name })
	})
	out := make([]Template, len(builtinTemplates))
	copy(out, builtinTemplates)
	return out
}
