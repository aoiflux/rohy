package graphbuild

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"rohy/backend/consts"
	"rohy/backend/graphene"
	"rohy/backend/graphreg"
	"rohy/backend/rules"
)

// 🔒 A build never sees a redacted event: it reads through the timestamp
// index, which a stripped event has left, so it neither relates the event again nor copies a value
// back into a basis.
func TestABuildLeavesARedactedEventOut(t *testing.T) {
	store, err := graphene.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { store.Close() })
	graphs, err := graphreg.Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := graphs.EnsureDefault(consts.DefaultGraphName, now); err != nil {
		t.Fatal(err)
	}
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "chain.json"), []byte(`{"name":"Chain","sequence":["4625","4624"]}`), 0o644); err != nil {
		t.Fatal(err)
	}
	registry, err := rules.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, r := range registry.List() {
		if r.Source == consts.RuleSourceBuiltin {
			_ = registry.SetEnabled(r.ID, false)
		}
	}
	ids := seed(t, store, "H", "4625", "4624", "4625", "4624")
	first, err := store.GetEvent(ids[0])
	if err != nil {
		t.Fatal(err)
	}
	if err := store.RedactEventRecord(ids[0], first.HashNormalized, "erasure request"); err != nil {
		t.Fatal(err)
	}

	res, err := New(store, graphs, registry).Run(context.Background(), Request{}, now)
	if err != nil {
		t.Fatal(err)
	}
	if res.Events != 3 {
		t.Errorf("the build read %d events, want the 3 left", res.Events)
	}
	if rels, err := store.RelationsOf(ids[0]); err != nil || len(rels) != 0 {
		t.Errorf("the redacted event was related again: %+v %v", rels, err)
	}
	if e, _ := store.GetEvent(ids[0]); e.HashNormalized != "" {
		t.Errorf("the build wrote the record back: %+v", e)
	}
}
