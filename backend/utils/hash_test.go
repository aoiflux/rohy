package utils

import "testing"

func TestHashDeterministicAndHex(t *testing.T) {
	a := HashString("hello")
	b := HashString("hello")
	if a != b {
		t.Fatal("hash not deterministic")
	}
	if len(a) != 64 {
		t.Fatalf("sha256 hex length = %d, want 64", len(a))
	}
	if HashString("hello") == HashString("world") {
		t.Fatal("distinct inputs hashed equal")
	}
}

// HashFields must be sensitive to field boundaries: joining with the separator
// prevents ("ab","c") and ("a","bc") from colliding.
func TestHashFieldsBoundary(t *testing.T) {
	if HashFields("ab", "c") == HashFields("a", "bc") {
		t.Fatal("field boundary collision")
	}
	if HashFields("x", "y", "z") != HashFields("x", "y", "z") {
		t.Fatal("HashFields not deterministic")
	}
}
