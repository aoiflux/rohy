package graphreg

import (
	"testing"
	"time"

	"rohy/backend/consts"
)

var t0 = time.Date(2026, 7, 20, 12, 0, 0, 0, time.UTC)

func TestEnsureDefaultSeedsFirstGraph(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}

	g, err := s.EnsureDefault(consts.DefaultGraphName, t0)
	if err != nil {
		t.Fatalf("ensure: %v", err)
	}
	if g.ID != consts.DefaultGraphID {
		t.Errorf("default id = %d, want %d", g.ID, consts.DefaultGraphID)
	}
	if g.Name != consts.DefaultGraphName {
		t.Errorf("default name = %q", g.Name)
	}
	if s.Active() != consts.DefaultGraphID {
		t.Errorf("active = %d, want default", s.Active())
	}

	// Idempotent: a second call does not create a second graph.
	if _, err := s.EnsureDefault(consts.DefaultGraphName, t0); err != nil {
		t.Fatalf("ensure 2: %v", err)
	}
	if n := len(s.List()); n != 1 {
		t.Fatalf("graphs = %d, want 1", n)
	}
}

func TestCreateRenameDeleteAndPersistence(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	if _, err := s.EnsureDefault(consts.DefaultGraphName, t0); err != nil {
		t.Fatalf("ensure: %v", err)
	}

	g2, err := s.Create("Investigation A", "logons", t0)
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if g2.ID == consts.DefaultGraphID {
		t.Fatalf("new graph reused default id")
	}
	if s.Active() != g2.ID {
		t.Errorf("create did not activate the new graph")
	}

	if _, err := s.Rename(g2.ID, "Investigation B", "renamed", t0.Add(time.Hour)); err != nil {
		t.Fatalf("rename: %v", err)
	}

	// Reopen from disk: the second store must see the same graphs + active id.
	s2, err := Open(dir)
	if err != nil {
		t.Fatalf("reopen: %v", err)
	}
	if len(s2.List()) != 2 {
		t.Fatalf("reopened graphs = %d, want 2", len(s2.List()))
	}
	if s2.Active() != g2.ID {
		t.Errorf("reopened active = %d, want %d", s2.Active(), g2.ID)
	}
	var found *Graph
	for _, g := range s2.List() {
		if g.ID == g2.ID {
			found = g
		}
	}
	if found == nil || found.Name != "Investigation B" {
		t.Fatalf("rename not persisted: %+v", found)
	}

	// Delete the active graph → active falls back to the remaining (default) graph.
	if err := s2.Delete(g2.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if s2.Active() != consts.DefaultGraphID {
		t.Errorf("after delete active = %d, want default", s2.Active())
	}
}

func TestDeleteLastGraphRefused(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	def, _ := s.EnsureDefault(consts.DefaultGraphName, t0)

	if err := s.Delete(def.ID); err != ErrLastGraph {
		t.Fatalf("delete last = %v, want ErrLastGraph", err)
	}
	if len(s.List()) != 1 {
		t.Errorf("graph was removed despite being the last")
	}
}

func TestSetActiveUnknownFails(t *testing.T) {
	dir := t.TempDir()
	s, _ := Open(dir)
	s.EnsureDefault(consts.DefaultGraphName, t0)
	if err := s.SetActive(9999); err != ErrNotFound {
		t.Fatalf("set active unknown = %v, want ErrNotFound", err)
	}
}

func TestEnsureForRuleBindsAdoptsAndPersists(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	def, err := s.EnsureDefault(consts.DefaultGraphName, t0)
	if err != nil {
		t.Fatal(err)
	}

	// First call creates the rule graph WITHOUT stealing the active graph.
	g, err := s.EnsureForRule("logon-chain", "Logon Chain", "desc", t0)
	if err != nil {
		t.Fatalf("ensure for rule: %v", err)
	}
	if g.RuleID != "logon-chain" {
		t.Errorf("rule binding = %q, want logon-chain", g.RuleID)
	}
	if s.Active() != def.ID {
		t.Errorf("active graph changed to %d, want %d", s.Active(), def.ID)
	}

	// Second call is idempotent: same graph, no new id.
	again, err := s.EnsureForRule("logon-chain", "Logon Chain", "desc", t0)
	if err != nil {
		t.Fatal(err)
	}
	if again.ID != g.ID {
		t.Errorf("ensure created a second graph (%d vs %d)", again.ID, g.ID)
	}

	// Renaming the graph must not orphan the binding.
	if _, err := s.Rename(g.ID, "My Renamed Graph", "", t0); err != nil {
		t.Fatal(err)
	}
	afterRename, err := s.EnsureForRule("logon-chain", "Logon Chain", "desc", t0)
	if err != nil {
		t.Fatal(err)
	}
	if afterRename.ID != g.ID {
		t.Errorf("rename orphaned the rule graph (%d vs %d)", afterRename.ID, g.ID)
	}
}

func TestEnsureForRuleAdoptsSameNamedUnboundGraph(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.EnsureDefault(consts.DefaultGraphName, t0); err != nil {
		t.Fatal(err)
	}
	// A graph created before rule binding existed (or by hand) with the rule's name.
	manual, err := s.Create("Logon Chain", "", t0)
	if err != nil {
		t.Fatal(err)
	}

	adopted, err := s.EnsureForRule("logon-chain", "Logon Chain", "", t0)
	if err != nil {
		t.Fatal(err)
	}
	if adopted.ID != manual.ID {
		t.Errorf("adopted id = %d, want the existing %d", adopted.ID, manual.ID)
	}
	if adopted.RuleID != "logon-chain" {
		t.Errorf("adoption did not stamp the rule id")
	}
}
