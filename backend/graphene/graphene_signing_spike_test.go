package graphene

// Spikes for signing. Each test pins an upstream behaviour the design depends on,
// so a graphene upgrade that changes it fails here rather than silently changing what rohy claims.

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/aoiflux/graphene"
	"github.com/aoiflux/graphene/disk"
	"github.com/aoiflux/graphene/signing"
	"github.com/aoiflux/graphene/store"
)

func strictTestOptions(t *testing.T) disk.Options {
	t.Helper()
	key, pub, err := signing.GenerateKey(1)
	if err != nil {
		t.Fatal(err)
	}
	ring := signing.NewKeyring()
	if err := ring.Add(1, pub); err != nil {
		t.Fatal(err)
	}
	return disk.Options{Signer: key, Verifier: ring, RequireSignedCommits: true}
}

// A write made without the key replays under RequireSignedCommits. Plain writes are bare WAL
// records, and replay checks a signature only on a batch commit (disk/wal.go:567 vs :607).
func TestSpikeAWriteWithoutTheKeyReplaysUnderRequiredSignatures(t *testing.T) {
	dir := t.TempDir()
	strict := strictTestOptions(t)

	g, err := graphene.OpenWithOptions(dir, strict)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddNodes([]*store.Node{{Labels: []store.NodeType{1}, Properties: []byte("signed")}}); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}

	// Whoever holds the files but not the key.
	g, err = graphene.OpenWithOptions(dir, disk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	forged, err := g.AddNode(&store.Node{Labels: []store.NodeType{1}, Properties: []byte("forged")})
	if err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}

	g, err = graphene.OpenWithOptions(dir, strict)
	if err != nil {
		t.Fatalf("strict open refused the log: %v (upstream now rejects bare records; revisit signing)", err)
	}
	defer g.Close()
	n, err := g.GetNode(forged)
	if err != nil || !bytes.Equal(n.Properties, []byte("forged")) {
		t.Fatalf("forged node %v: %v", n, err)
	}
}

// A redacted record survives in a retained WAL segment: redaction never touches segments.
func TestSpikeRetentionKeepsWhatARedactionRemoved(t *testing.T) {
	for _, tc := range []struct {
		name   string
		retain disk.RetentionPolicy
		want   bool
	}{
		{"without retention", disk.RetentionPolicy{}, false},
		{"with retention", disk.RetentionPolicy{MaxSegments: 100}, true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			dir := t.TempDir()
			needle := []byte("needle-4f2a9c-personal-data")
			g, err := graphene.OpenWithOptions(dir, disk.Options{Redaction: true, Retention: tc.retain})
			if err != nil {
				t.Fatal(err)
			}
			ids, err := g.AddNodes([]*store.Node{{Labels: []store.NodeType{1}, Properties: needle}})
			if err != nil {
				t.Fatal(err)
			}
			if err := g.Compact(); err != nil {
				t.Fatal(err)
			}
			fs, _ := g.Forensics()
			if _, err := fs.RedactNodeProperties(ids[0], disk.RedactionRequest{Reason: "spike"}); err != nil {
				t.Fatal(err)
			}
			if err := g.Compact(); err != nil {
				t.Fatal(err)
			}
			if err := g.Close(); err != nil {
				t.Fatal(err)
			}

			var holding []string
			err = filepath.WalkDir(dir, func(p string, d os.DirEntry, err error) error {
				if err != nil || d.IsDir() {
					return err
				}
				b, err := os.ReadFile(p)
				if err != nil {
					return err
				}
				if bytes.Contains(b, needle) {
					holding = append(holding, filepath.Base(p))
				}
				return nil
			})
			if err != nil {
				t.Fatal(err)
			}
			if got := len(holding) > 0; got != tc.want {
				t.Fatalf("content found in %v, want found=%v", holding, tc.want)
			}
			t.Logf("content in %v", holding)
		})
	}
}

// An image whose attestation is absent opens under a Verifier with VerifyOnOpen: the engine has no
// "require attestation". Pinned with an image compacted unsigned, then opened strictly.
func TestSpikeAnUnattestedImageOpensUnderAVerifier(t *testing.T) {
	dir := t.TempDir()
	g, err := graphene.OpenWithOptions(dir, disk.Options{})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := g.AddNodes([]*store.Node{{Labels: []store.NodeType{1}, Properties: []byte("x")}}); err != nil {
		t.Fatal(err)
	}
	if err := g.Compact(); err != nil {
		t.Fatal(err)
	}
	if err := g.Close(); err != nil {
		t.Fatal(err)
	}
	strict := strictTestOptions(t)
	strict.VerifyOnOpen = true
	g, err = graphene.OpenWithOptions(dir, strict)
	if err != nil {
		t.Fatalf("strict open refused an unattested image: %v (upstream now requires one; revisit signing)", err)
	}
	g.Close()
}
