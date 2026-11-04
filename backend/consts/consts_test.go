package consts

import (
	"go/ast"
	"go/parser"
	"go/token"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// TestEverySourceTypeHasADeliberateIdentityRecipe is the guard IdentityRecipeFor's default relies
// on. The default is the Windows recipe, so a new non-Windows source type that is never added to
// identityRecipes would silently collapse its records on a coarse instant. Reading the declared
// constants from source — rather than from a hand-kept list — is what makes forgetting fail here
// instead of in someone's case.
func TestEverySourceTypeHasADeliberateIdentityRecipe(t *testing.T) {
	files, err := filepath.Glob("*.go")
	if err != nil {
		t.Fatalf("list package files: %v", err)
	}
	fset := token.NewFileSet()
	declared := map[string]string{} // constant name → value
	for _, path := range files {
		if strings.HasSuffix(path, "_test.go") {
			continue
		}
		file, err := parser.ParseFile(fset, path, nil, 0)
		if err != nil {
			t.Fatalf("parse %s: %v", path, err)
		}
		ast.Inspect(file, func(n ast.Node) bool {
			spec, ok := n.(*ast.ValueSpec)
			if !ok {
				return true
			}
			for i, name := range spec.Names {
				if !strings.HasPrefix(name.Name, "SourceType") || i >= len(spec.Values) {
					continue
				}
				lit, ok := spec.Values[i].(*ast.BasicLit)
				if !ok || lit.Kind != token.STRING {
					continue
				}
				value, err := strconv.Unquote(lit.Value)
				if err != nil {
					t.Fatalf("%s: %v", name.Name, err)
				}
				declared[name.Name] = value
			}
			return true
		})
	}

	if len(declared) == 0 {
		t.Fatal("found no SourceType* constants — the scan is broken, and an empty scan passes vacuously")
	}
	for name, value := range declared {
		recipe, ok := identityRecipes[value]
		if !ok {
			t.Errorf("%s (%q) has no entry in identityRecipes: choose its identity recipe deliberately", name, value)
			continue
		}
		if recipe != IdentityRecipeWindows && recipe != IdentityRecipeRecordBytes {
			t.Errorf("%s maps to unknown recipe %q", name, recipe)
		}
	}
}
