package utils

import (
	"strings"
	"testing"
	"unicode/utf8"
)

// cp spells a code point out, so the source never holds the invisible character it tests.
func cp(r rune) string { return string(r) }

func TestDisplayNameRefusesWhatRendersAsSomethingElse(t *testing.T) {
	for _, tc := range []struct {
		in   string
		want string
		ok   bool
	}{
		{"Ada Lovelace", "Ada Lovelace", true},
		{"  padded  ", "padded", true},
		{"Zo" + cp(0xEB) + " " + cp(0xD8) + "deg" + cp(0xE5) + "rd", "Zo" + cp(0xEB) + " " + cp(0xD8) + "deg" + cp(0xE5) + "rd", true},
		{cp(0x5C71) + cp(0x7530), cp(0x5C71) + cp(0x7530), true},
		{"", "", false},
		{"   ", "", false},
		{"line" + cp(0x0A) + "break", "", false},
		{"tab" + cp(0x09) + "here", "", false},
		{"evil" + cp(0x202E) + "gnp.exe", "", false}, // right-to-left override
		{"zero" + cp(0x200D) + "width", "", false},   // zero-width joiner
		{"bom" + cp(0xFEFF), "", false},
		{"para" + cp(0x2029) + "sep", "", false},
		{"bad" + string([]byte{0xff}) + "utf8", "", false},
		{strings.Repeat("a", 129), "", false},
		{strings.Repeat(cp(0xE9), 128), strings.Repeat(cp(0xE9), 128), true},
	} {
		got, ok := DisplayName(tc.in, 128)
		if ok != tc.ok || (ok && got != tc.want) {
			t.Errorf("DisplayName(%q) = %q, %v; want %q, %v", tc.in, got, ok, tc.want, tc.ok)
		}
	}
}

// A cap that cuts between bytes can split a character, and encoding/json then writes the broken
// tail as U+FFFD, so the value held in memory stops matching what the UI and the disk were given.
// The cap counts characters and cuts only between them.
func TestCapRunesCutsBetweenRunes(t *testing.T) {
	invalid := "a" + string([]byte{0xff}) + "b"
	for _, tc := range []struct {
		in    string
		limit int
		want  string
	}{
		{"abc", 2, "ab"},
		{"abc", 3, "abc"},
		{"abc", 9, "abc"},
		{"abc", 0, ""},
		{"abc", -1, ""},
		{"", 3, ""},
		{"a" + strings.Repeat(cp(0x436), 3), 2, "a" + cp(0x436)},
		{strings.Repeat(cp(0x1F600), 3), 2, strings.Repeat(cp(0x1F600), 2)},
		// An invalid byte counts as one rune, as utf8.RuneCountInString counts it.
		{invalid, 2, invalid[:2]},
	} {
		got := CapRunes(tc.in, tc.limit)
		if got != tc.want {
			t.Errorf("CapRunes(%q, %d) = %q, want %q", tc.in, tc.limit, got, tc.want)
		}
		if !strings.HasPrefix(tc.in, got) {
			t.Errorf("CapRunes(%q, %d) = %q, which is not a prefix of the input", tc.in, tc.limit, got)
		}
		if n := utf8.RuneCountInString(got); n > max(tc.limit, 0) {
			t.Errorf("CapRunes(%q, %d) holds %d runes", tc.in, tc.limit, n)
		}
		if utf8.ValidString(tc.in) && !utf8.ValidString(got) {
			t.Errorf("CapRunes(%q, %d) = %q, which split a character", tc.in, tc.limit, got)
		}
	}
}
