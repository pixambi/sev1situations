package app

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/pixambi/sev1situations/internal/primitive"
)

// TestEveryPrimitiveIsTestedAndRunForReal holds every primitive the application ships to the bar a scenario author relies on
// (R-CONTRIB-4): a unit test names it, and something that runs on real containers uses it, which is an integration test or a
// built-in scenario that is played for real before it merges. It is a check on the names, so it cannot say the tests are good,
// but it does catch a primitive added with nothing behind it.
func TestEveryPrimitiveIsTestedAndRunForReal(t *testing.T) {
	products, err := DefaultProducts()
	if err != nil {
		t.Fatal(err)
	}
	reg := primitive.NewRegistry()
	if err := primitive.RegisterGeneric(reg); err != nil {
		t.Fatal(err)
	}
	if err := products.RegisterPrimitives(reg); err != nil {
		t.Fatal(err)
	}

	root := filepath.Join("..", "..")
	var unit, real []string // the text of the files that name a primitive, by kind
	walk := func(dir string, f func(path string, text string)) {
		err := filepath.WalkDir(filepath.Join(root, dir), func(p string, d fs.DirEntry, err error) error {
			if err != nil || d.IsDir() {
				return err
			}
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			f(p, string(b))
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
	}
	walk("internal", func(p, text string) {
		if !strings.HasSuffix(p, "_test.go") {
			return
		}
		if strings.Contains(text, "//go:build integration") {
			real = append(real, text)
		} else {
			unit = append(unit, text)
		}
	})
	walk("scenarios", func(p, text string) {
		if strings.HasSuffix(p, "scenario.yaml") {
			real = append(real, text)
		}
	})

	named := func(files []string, name string) bool {
		for _, f := range files {
			if strings.Contains(f, `"`+name+`"`) || strings.Contains(f, "primitive: "+name+"\n") {
				return true
			}
		}
		return false
	}
	for _, name := range reg.Names() {
		if !named(unit, name) {
			t.Errorf("%s is named in no unit test", name)
		}
		if !named(real, name) {
			t.Errorf("%s is used by no integration test and no built-in scenario, so nothing runs it on real containers", name)
		}
	}
}
