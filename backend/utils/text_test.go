package utils

import (
	"strings"
	"testing"
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
