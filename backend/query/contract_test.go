package query

import (
	"encoding/json"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

// The console's contract with its page. Two committed files carry it across to the frontend, whose
// tests read them (frontend/src/lib/query/spec.test.js): the vocabulary the page builds from, and
// the specs both sides must accept or refuse alike.

var updateVocabulary = flag.Bool("update-vocabulary-golden", false,
	"rewrite testdata/vocabulary-golden.json from Describe()")

func TestVocabularyGolden(t *testing.T) {
	path := filepath.Join("testdata", "vocabulary-golden.json")
	current, err := json.MarshalIndent(Describe(), "", "  ")
	if err != nil {
		t.Fatal(err)
	}
	current = append(current, '\n')
	if *updateVocabulary {
		if err := os.WriteFile(path, current, 0o644); err != nil {
			t.Fatal(err)
		}
		t.Logf("rewrote %s — check the diff", path)
		return
	}
	want, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden (run with -update-vocabulary-golden to create it): %v", err)
	}
	if string(current) != string(want) {
		t.Errorf("the query vocabulary changed. The Query page builds itself from it, and its tests "+
			"check the page against this file.\n\nIf the change is intended, run:\n"+
			"    go test ./backend/query/ -run TestVocabularyGolden -update-vocabulary-golden\n\ngot:\n%s", current)
	}
}

func TestTheSharedSpecCasesAgreeWithValidate(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("testdata", "spec-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var file struct {
		Cases []struct {
			Name string `json:"name"`
			OK   bool   `json:"ok"`
			Spec Spec   `json:"spec"`
		} `json:"cases"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		t.Fatal(err)
	}
	if len(file.Cases) < 20 {
		t.Fatalf("%d cases", len(file.Cases))
	}
	for _, c := range file.Cases {
		if err := Validate(c.Spec); (err == nil) != c.OK {
			t.Errorf("%s: Validate says %v, the case says ok=%v", c.Name, err, c.OK)
		}
	}
}
