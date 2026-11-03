package tui

import (
	"go/ast"
	"go/token"
	"os"
	"slices"
	"testing"

	"github.com/castrowithcee/qatlas-kit/archtest"
)

// forbidPackageState reports every package-level variable and every init function. A variable that holds a
// sentinel error or a compiled regular expression is a constant in all but name and stays allowed.
func forbidPackageState(file *archtest.File) []archtest.Hit {
	immutable := func(value ast.Expr) bool {
		call, ok := value.(*ast.CallExpr)
		if !ok {
			return false
		}
		sel, ok := call.Fun.(*ast.SelectorExpr)
		if !ok {
			return false
		}
		pkg, ok := sel.X.(*ast.Ident)
		if !ok {
			return false
		}
		paths := file.Imports()[pkg.Name]
		return slices.Contains(paths, "errors") && sel.Sel.Name == "New" ||
			slices.Contains(paths, "regexp") && sel.Sel.Name == "MustCompile"
	}
	var hits []archtest.Hit
	for _, decl := range file.Syntax.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if d.Recv == nil && d.Name.Name == "init" {
				hits = append(hits, archtest.Hit{Node: d, Item: "init"})
			}
		case *ast.GenDecl:
			if d.Tok != token.VAR {
				continue
			}
			for _, spec := range d.Specs {
				vs := spec.(*ast.ValueSpec)
				for i, name := range vs.Names {
					if i < len(vs.Values) && len(vs.Names) == len(vs.Values) && immutable(vs.Values[i]) {
						continue
					}
					hits = append(hits, archtest.Hit{Node: name, Item: name.Name})
				}
			}
		}
	}
	return hits
}

// The interface keeps its state in the model: no package-level variable and no init function in the editor
// and its preferences.
func TestNoPackageState(t *testing.T) {
	root, err := archtest.ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	archtest.Check(t, os.DirFS(root), []archtest.Rule{{
		Name:  "package-state",
		Files: archtest.Files{Under: []string{"internal/tui", "internal/uiprefs"}},
		Check: forbidPackageState,
		Hint:  "hold the state in the Model or its uiContext; a fixed table is a function or a constant",
	}}, nil)
}

// Every text ID the package declares has an English and a German text, and no two constants share an ID.
func TestCatalogCoversEveryTextID(t *testing.T) {
	root, err := archtest.ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	if err := archtest.CheckCatalog(os.DirFS(root), "internal/tui", catalog(), archtest.Type{Name: "textID"}); err != nil {
		t.Error(err)
	}
}

// The package-state rule finds what it forbids and lets the immutable values pass.
func TestPackageStateRuleHasTeeth(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(dir+"/go.mod", []byte("module example.com/demo\n\ngo 1.24\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(dir+"/internal/tui", 0o700); err != nil {
		t.Fatal(err)
	}
	src := `package tui

import (
	"errors"
	"regexp"
)

var errDemo = errors.New("demo")
var demoForm = regexp.MustCompile("^demo$")
var demoTable = []string{"demo"}

func init() {}
`
	if err := os.WriteFile(dir+"/internal/tui/demo.go", []byte(src), 0o600); err != nil {
		t.Fatal(err)
	}
	findings, err := archtest.Find(os.DirFS(dir), archtest.Rule{
		Name: "package-state", Files: archtest.Files{Under: []string{"internal/tui"}}, Check: forbidPackageState,
	})
	if err != nil {
		t.Fatal(err)
	}
	var items []string
	for _, f := range findings {
		items = append(items, f.Item)
	}
	if !slices.Equal(items, []string{"demoTable", "init"}) {
		t.Errorf("findings = %v, want demoTable and init", items)
	}
}
