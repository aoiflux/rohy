package graphene

import (
	"testing"

	"rohy/backend/consts"
)

// Forensic verification cost (§14.2) — the 🟡 gate.
//
// WHAT THESE DECIDE. Two questions, and neither should be answered from a sentence:
//
//  1. Can `VerifyOnOpen` be a default? It re-derives every record hash at open. Upstream
//     measures roughly 5× on a 10k-node store and is explicit that an evidence store should
//     turn it on. rohy's scale target is 100k events, and PERFORMANCE.md exists because a
//     textbook optimisation (the declared timestamp key) made cold start 10× worse and was
//     reverted on the number. Open is paid by every user on every launch.
//
//  2. What does on-demand verification cost instead? `VerifyImage` reads the image directly
//     and opens no store, so it can be a button in the deep integrity check at ZERO startup
//     cost. If it is in the same ballpark as VerifyOnOpen's overhead, then the on-demand
//     route gets the same evidence for none of the tax, and question 1 answers itself.
//
// 🔒 The response to a bad number is the on-demand route plus a reported posture — never a
// silent skip, and never raising the threshold. Same discipline as the correlation-projection
// budget: shrink the work, do not move the line.
//
// Compare within a size:
//
//	go test ./backend/graphene/ -run XXX -bench 'OpenVerify|OpenPlain|VerifyImage' -benchtime 3x

// benchmarkOpenVerified opens a prepared directory with image verification on.
func benchmarkOpenVerified(b *testing.B, n int) {
	dir := seedDiskStore(b, n, true) // compacted: there must BE an image to verify
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s := OpenLazy(dir).WithVerifyOnOpen(true)
		if err := s.Warm(); err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// benchmarkOpenPlain is the same compacted store opened under the default posture. It is the
// baseline the verified number is only meaningful against — BenchmarkOpenCompacted* cannot serve,
// because it measures a differently-seeded store.
func benchmarkOpenPlain(b *testing.B, n int) {
	dir := seedDiskStore(b, n, true)
	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		s, err := Open(dir)
		if err != nil {
			b.Fatal(err)
		}
		b.StopTimer()
		if err := s.Close(); err != nil {
			b.Fatal(err)
		}
		b.StartTimer()
	}
}

// benchmarkVerifyImage measures the on-demand check on an already-open store, which is how the
// integrity report will call it.
func benchmarkVerifyImage(b *testing.B, n int) {
	dir := seedDiskStore(b, n, true)
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ResetTimer()
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		check, err := s.VerifyImage()
		if err != nil {
			b.Fatal(err)
		}
		// Guard against measuring a no-op: a seeding change that stopped producing an image would
		// otherwise turn this into a very fast benchmark of nothing.
		if check.Digest != DigestMatch {
			b.Fatalf("digest = %q, want %q — nothing was verified", check.Digest, DigestMatch)
		}
	}
}

func BenchmarkOpenPlain10k(b *testing.B)     { benchmarkOpenPlain(b, 10000) }
func BenchmarkOpenVerified10k(b *testing.B)  { benchmarkOpenVerified(b, 10000) }
func BenchmarkOpenPlain100k(b *testing.B)    { benchmarkOpenPlain(b, 100000) }
func BenchmarkOpenVerified100k(b *testing.B) { benchmarkOpenVerified(b, 100000) }

func BenchmarkVerifyImage10k(b *testing.B)  { benchmarkVerifyImage(b, 10000) }
func BenchmarkVerifyImage100k(b *testing.B) { benchmarkVerifyImage(b, 100000) }

// benchmarkProveEvent measures the drawer's "Save proof…" inside the store: graphene's proof, which
// recomputes every node leaf (one pass over the node records), the payload read, and Assemble's
// verification of its own document. It runs on a click, not on a hot path; the number says
// whether that click needs a progress state.
//
//	go test ./backend/graphene/ -run XXX -bench ProveEvent -benchtime 5x -count 3
func benchmarkProveEvent(b *testing.B, n int) {
	dir := seedDiskStore(b, n, true)
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	id := uint64(n / 2)

	b.ReportAllocs()
	for b.Loop() {
		p, err := s.ProveEvent(id)
		if err != nil {
			b.Fatal(err)
		}
		if p.State != consts.EventProofOK {
			b.Fatalf("state = %q: the benchmark would be timing a refusal, not a proof", p.State)
		}
	}
}

func BenchmarkProveEvent10k(b *testing.B)  { benchmarkProveEvent(b, 10000) }
func BenchmarkProveEvent100k(b *testing.B) { benchmarkProveEvent(b, 100000) }

// benchmarkUnsealed is what closing an unchanged case now costs instead of a compaction (plan
// §15.5): the question "was anything written since the last seal", asked of a sealed case.
func benchmarkUnsealed(b *testing.B, n int) {
	dir := seedDiskStore(b, n, true)
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()

	b.ReportAllocs()
	for b.Loop() {
		unsealed, err := s.Unsealed()
		if err != nil {
			b.Fatal(err)
		}
		if unsealed {
			b.Fatal("a freshly reopened sealed case reads as unsealed: the benchmark would be timing the wrong branch")
		}
	}
}

func BenchmarkUnsealed10k(b *testing.B)  { benchmarkUnsealed(b, 10000) }
func BenchmarkUnsealed100k(b *testing.B) { benchmarkUnsealed(b, 100000) }

// benchmarkEventCustody measures the drawer's "Account for this event" inside the store: graphene's
// custody walk (one proof, plus the ledgers rohy does not keep), then ProveEvent for the record and
// raw layers. Two proofs, so roughly twice ProveEvent; it runs on a click.
//
//	go test ./backend/graphene/ -run XXX -bench EventCustody -benchtime 5x -count 3
func benchmarkEventCustody(b *testing.B, n int) {
	dir := seedDiskStore(b, n, true)
	s, err := Open(dir)
	if err != nil {
		b.Fatal(err)
	}
	defer s.Close()
	id := uint64(n / 2)

	b.ReportAllocs()
	for b.Loop() {
		c, err := s.EventCustody(id)
		if err != nil {
			b.Fatal(err)
		}
		if !c.InSnapshot || c.ProofState != consts.EventProofOK {
			b.Fatalf("in snapshot %v, proof %q: the benchmark would be timing a short path", c.InSnapshot, c.ProofState)
		}
	}
}

func BenchmarkEventCustody10k(b *testing.B)  { benchmarkEventCustody(b, 10000) }
func BenchmarkEventCustody100k(b *testing.B) { benchmarkEventCustody(b, 100000) }
