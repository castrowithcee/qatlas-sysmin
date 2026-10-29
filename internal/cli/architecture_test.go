package cli

// Architecture guard: surfaces (the CLI and the TUI, including subpackages) reach the vault, the
// configuration store and the state only through the client package. The checker reads the source of the
// module as an fs.FS and looks at imports and qualified identifiers; build tags are ignored so every platform
// checks the same files. Test files are not checked: they may build fixtures from the guarded packages.
// Known violations are listed as exceptions. The list can only shrink: a new violation fails the test, and so
// does an exception that no longer matches anything.
//
// Rules:
//  1. Surfaces import none of the guarded packages.
//  2. Surfaces use neither config.Store nor config.NewStore.

import (
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"testing"
	"testing/fstest"
)

const (
	ruleImports = 1
	ruleHandles = 2
)

type archFinding struct {
	Rule int
	File string
	Item string
}

type archException struct {
	Rule   int
	File   string
	Item   string
	Reason string
}

var surfaceDirs = []string{"internal/cli", "internal/tui"}

var surfaceForbidden = map[string]bool{
	"internal/vault":      true,
	"internal/admin":      true,
	"internal/state":      true,
	"internal/service":    true,
	"internal/autounlock": true,
	"internal/daemon":     true,
}

// Qualified identifiers surfaces must not use, by module-relative package path.
var storeHandles = map[string]map[string]bool{
	"internal/config": {"Store": true, "NewStore": true},
}

var architectureExceptions = []archException{
	{ruleImports, "internal/cli/run.go", "internal/service", "entry point of the service itself"},
	{ruleImports, "internal/cli/cli.go", "internal/daemon", "Options field that lets tests replace the service's peer resolver"},
	{ruleImports, "internal/cli/cli.go", "internal/autounlock", "Options field that lets tests replace the machine-binding helper"},
	{ruleImports, "internal/cli/vault.go", "internal/autounlock", "root-local machine binding works without the service"},
}

func TestArchitecture(t *testing.T) {
	findings, err := architectureFindings(os.DirFS(moduleRoot(t)))
	if err != nil {
		t.Fatal(err)
	}
	for _, problem := range architectureProblems(findings, architectureExceptions) {
		t.Error(problem)
	}
}

func TestArchitectureChecksDetectViolations(t *testing.T) {
	const mod = "module example.com/m\n\ngo 1.26\n"
	fsys := fstest.MapFS{
		"go.mod":                 {Data: []byte(mod)},
		"internal/cli/a.go":      {Data: []byte("package cli\n\nimport \"example.com/m/internal/vault\"\n\nvar _ = vault.X\n")},
		"internal/tui/sub/b.go":  {Data: []byte("package sub\n\nimport (\n\tc \"example.com/m/internal/config\"\n)\n\nvar _ = c.NewStore\n")},
		"internal/cli/ok.go":     {Data: []byte("package cli\n\nimport \"example.com/m/internal/client\"\n\nvar _ = client.X\n")},
		"internal/cli/a_test.go": {Data: []byte("package cli\n\nimport \"example.com/m/internal/vault\"\n\nvar _ = vault.X\n")},
		"internal/other/c.go":    {Data: []byte("package other\n\nimport \"example.com/m/internal/vault\"\n")},
	}
	findings, err := architectureFindings(fsys)
	if err != nil {
		t.Fatal(err)
	}
	want := []archFinding{
		{ruleImports, "internal/cli/a.go", "internal/vault"},
		{ruleHandles, "internal/tui/sub/b.go", "config.NewStore"},
	}
	if len(findings) != len(want) {
		t.Fatalf("findings = %v, want %v", findings, want)
	}
	for i := range want {
		if findings[i] != want[i] {
			t.Errorf("finding %d = %v, want %v", i, findings[i], want[i])
		}
	}
	if got := architectureProblems(findings, nil); len(got) != 2 {
		t.Errorf("unexcepted findings gave %d problems, want 2: %v", len(got), got)
	}
	excepted := []archException{
		{ruleImports, "internal/cli/a.go", "internal/vault", "reason"},
		{ruleHandles, "internal/tui/sub/b.go", "config.NewStore", "reason"},
	}
	if got := architectureProblems(findings, excepted); len(got) != 0 {
		t.Errorf("excepted findings gave problems: %v", got)
	}
	stale := append(excepted, archException{ruleImports, "internal/cli/gone.go", "internal/vault", "reason"})
	if got := architectureProblems(findings, stale); len(got) != 1 || !strings.Contains(got[0], "stale") {
		t.Errorf("stale exception not reported: %v", got)
	}
	noReason := []archException{{ruleImports, "internal/cli/a.go", "internal/vault", " "}}
	if got := architectureProblems(findings[:1], noReason); len(got) != 1 || !strings.Contains(got[0], "no reason") {
		t.Errorf("missing reason not reported: %v", got)
	}
}

func moduleRoot(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return dir
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			t.Fatal("no go.mod above the working directory")
		}
		dir = parent
	}
}

// architectureProblems reports every finding without an exception and every exception that is stale,
// duplicated or has no reason.
func architectureProblems(findings []archFinding, exceptions []archException) []string {
	type key struct {
		rule       int
		file, item string
	}
	found := map[key]bool{}
	for _, f := range findings {
		found[key{f.Rule, f.File, f.Item}] = true
	}
	excepted := map[key]bool{}
	var problems []string
	for _, e := range exceptions {
		k := key{e.Rule, e.File, e.Item}
		switch {
		case strings.TrimSpace(e.Reason) == "":
			problems = append(problems, fmt.Sprintf("%s: rule %d exception for %q has no reason", e.File, e.Rule, e.Item))
		case excepted[k]:
			problems = append(problems, fmt.Sprintf("%s: rule %d exception for %q is listed twice", e.File, e.Rule, e.Item))
		case !found[k]:
			problems = append(problems, fmt.Sprintf("%s: rule %d exception for %q is stale; remove it", e.File, e.Rule, e.Item))
		}
		excepted[k] = true
	}
	for _, f := range findings {
		if excepted[key{f.Rule, f.File, f.Item}] {
			continue
		}
		switch f.Rule {
		case ruleImports:
			problems = append(problems, fmt.Sprintf("%s: rule 1: imports %s; go through internal/client", f.File, f.Item))
		default:
			problems = append(problems, fmt.Sprintf("%s: rule 2: uses %s; go through internal/client", f.File, f.Item))
		}
	}
	sort.Strings(problems)
	return problems
}

func architectureFindings(fsys fs.FS) ([]archFinding, error) {
	module, err := modulePath(fsys)
	if err != nil {
		return nil, err
	}
	var findings []archFinding
	err = fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && skipDir(d.Name()) {
				return fs.SkipDir
			}
			return nil
		}
		if !strings.HasSuffix(p, ".go") || strings.HasSuffix(p, "_test.go") || !isSurface(p) {
			return nil
		}
		src, err := fs.ReadFile(fsys, p)
		if err != nil {
			return err
		}
		file, err := parser.ParseFile(token.NewFileSet(), p, src, parser.SkipObjectResolution)
		if err != nil {
			return fmt.Errorf("parse %s: %w", p, err)
		}
		byLocal := map[string]string{}
		for _, imp := range file.Imports {
			target, err := strconv.Unquote(imp.Path.Value)
			if err != nil {
				return fmt.Errorf("%s: bad import path %s", p, imp.Path.Value)
			}
			rel, ok := strings.CutPrefix(target, module+"/")
			if !ok {
				continue
			}
			if surfaceForbidden[rel] {
				findings = append(findings, archFinding{ruleImports, p, rel})
			}
			if storeHandles[rel] != nil {
				local := path.Base(rel)
				if imp.Name != nil {
					local = imp.Name.Name
				}
				byLocal[local] = rel
			}
		}
		ast.Inspect(file, func(n ast.Node) bool {
			sel, ok := n.(*ast.SelectorExpr)
			if !ok {
				return true
			}
			if id, ok := sel.X.(*ast.Ident); ok {
				if rel, ok := byLocal[id.Name]; ok && storeHandles[rel][sel.Sel.Name] {
					findings = append(findings, archFinding{ruleHandles, p, path.Base(rel) + "." + sel.Sel.Name})
				}
			}
			return true
		})
		return nil
	})
	if err != nil {
		return nil, err
	}
	sort.Slice(findings, func(i, j int) bool {
		a, b := findings[i], findings[j]
		if a.Rule != b.Rule {
			return a.Rule < b.Rule
		}
		if a.File != b.File {
			return a.File < b.File
		}
		return a.Item < b.Item
	})
	out := findings[:0]
	for i, f := range findings {
		if i == 0 || f != findings[i-1] {
			out = append(out, f)
		}
	}
	return out, nil
}

func isSurface(p string) bool {
	for _, dir := range surfaceDirs {
		if strings.HasPrefix(p, dir+"/") {
			return true
		}
	}
	return false
}

// skipDir reports directories the go tool ignores.
func skipDir(name string) bool {
	return name == "testdata" || name == "vendor" || strings.HasPrefix(name, ".") || strings.HasPrefix(name, "_")
}

func modulePath(fsys fs.FS) (string, error) {
	data, err := fs.ReadFile(fsys, "go.mod")
	if err != nil {
		return "", err
	}
	for _, line := range strings.Split(string(data), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "module "); ok {
			return strings.Trim(strings.TrimSpace(rest), `"`), nil
		}
	}
	return "", fmt.Errorf("go.mod declares no module")
}
