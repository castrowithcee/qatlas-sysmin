package tui

import (
	"go/ast"
	"go/token"
	"os"
	"path"
	"regexp"
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

// ruleTableKeys are the key names of the key table, the only place the TUI names a key.
func ruleTableKeys() []string {
	var keys []string
	for _, a := range keyTable() {
		for _, k := range a.DefaultKeys {
			if !slices.Contains(keys, k) {
				keys = append(keys, k)
			}
		}
	}
	return keys
}

// Packages the kit replaced or the product dropped; nothing comes back as a local copy.
var (
	forbiddenModules = []string{
		"internal/admin", "internal/adopt", "internal/approval", "internal/autounlock", "internal/changelog",
		"internal/config/...", "internal/engine", "internal/execute", "internal/filelock", "internal/i18n",
		"internal/notify", "internal/output/...", "internal/redact", "internal/selfupdate/...", "internal/source/...",
		"internal/vault", "internal/webhook", "internal/writecheck",
	}
	forbiddenLibraries = archtest.Pattern(regexp.MustCompile(`^(charm\.land|github\.com/charmbracelet)/bubbles(/|$)`))
)

// ownModalFiles, ownModalFuncs and ownModalTypes are what an own modal or dialog mechanic is made of: the
// kit's Dialog, Form and List draw every dialog over the screen and in the workspace.
var (
	ownModalFiles = []string{"modal.go", "inputmodal.go", "confirm.go", "dialogpanel.go", "dialogbuttons.go", "widgets.go"}
	ownModalFuncs = []string{
		"overlay", "dim", "modalView", "questionView", "questionScreen", "questionText", "questionHeading",
		"promptView", "promptText", "promptScreen", "askConfirm", "updateConfirm", "dialogButtons", "withButtons",
	}
	ownModalTypes = []string{"promptDialog", "question", "dialogPanel", "dialogButton", "panelOpts"}
)

// forbidOwnModal reports a file, function or type of an own modal or dialog mechanic.
func forbidOwnModal(file *archtest.File) []archtest.Hit {
	var hits []archtest.Hit
	if base := path.Base(file.Path); slices.Contains(ownModalFiles, base) {
		hits = append(hits, archtest.Hit{Node: file.Syntax.Name, Item: base})
	}
	for _, decl := range file.Syntax.Decls {
		switch d := decl.(type) {
		case *ast.FuncDecl:
			if slices.Contains(ownModalFuncs, d.Name.Name) {
				hits = append(hits, archtest.Hit{Node: d, Item: d.Name.Name})
			}
		case *ast.GenDecl:
			for _, spec := range d.Specs {
				if ts, ok := spec.(*ast.TypeSpec); ok && slices.Contains(ownModalTypes, ts.Name.Name) {
					hits = append(hits, archtest.Hit{Node: ts, Item: ts.Name.Name})
				}
			}
		}
	}
	return hits
}

// archRules are the rules that keep a copy of a kit building block from coming back. Key names live in the
// key table, colours in the theme, and no widget library or dropped package returns.
func archRules() []archtest.Rule {
	return []archtest.Rule{
		{
			Name:  "key literals",
			Files: archtest.Files{Under: []string{"internal/tui", "internal/uiprefs"}, Except: []string{"internal/tui/keymap.go"}},
			Check: archtest.ForbidKeyLiterals(ruleTableKeys(), "String"),
			Hint:  "ask the Keymap for the action instead of naming its key",
		},
		{
			Name:  "colour literals",
			Files: archtest.Files{Under: []string{"internal/tui", "internal/uiprefs"}},
			Check: archtest.ForbidSelectors("charm.land/lipgloss/v2", "Color"),
			Hint:  "colours come from the palette of the theme",
		},
		{
			Name:  "own modal",
			Files: archtest.Files{Under: []string{"internal/tui"}},
			Check: forbidOwnModal,
			Hint:  "the kit's Dialog, Form and List draw dialogs over the screen and in the workspace",
		},
		{
			Name:  "widget libraries",
			Files: archtest.Files{Under: []string{"."}, Tests: archtest.WithTests},
			Check: archtest.ForbidImportPaths(forbiddenLibraries),
			Hint:  "use the building blocks of the kit",
		},
		{
			Name:  "dropped packages",
			Files: archtest.Files{Under: []string{"."}, Tests: archtest.WithTests},
			Check: archtest.ForbidImports(forbiddenModules...),
			Hint:  "the package is gone; the kit or the client carries its job",
		},
	}
}

// archExceptions can only shrink.
var archExceptions = []archtest.Exception{
	{Rule: "colour literals", File: "internal/tui/ui.go", Item: "lipgloss.Color", Reason: "the one bridge from a palette colour to Lip Gloss"},
}

func TestKitBuildingBlocksDoNotComeBack(t *testing.T) {
	root, err := archtest.ModuleRoot(".")
	if err != nil {
		t.Fatal(err)
	}
	archtest.Check(t, os.DirFS(root), archRules(), archExceptions)
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
