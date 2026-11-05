package utils

import (
	"strings"
	"unicode"
	"unicode/utf8"
)

// DisplayName reports whether s, trimmed, is fit to show as a name or label: valid UTF-8, 1 to max
// runes, and no character that is invisible or that moves other text.
//
// Control characters break a line or a log. Format characters are the subtler threat: a
// right-to-left override (U+202E) or a zero-width joiner makes a name render as something it is
// not, which matters wherever a name is evidence of who did something.
func DisplayName(s string, max int) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" || !utf8.ValidString(s) || utf8.RuneCountInString(s) > max {
		return s, false
	}
	for _, r := range s {
		if unicode.Is(unicode.Cc, r) || unicode.Is(unicode.Cf, r) ||
			unicode.Is(unicode.Zl, r) || unicode.Is(unicode.Zp, r) || r == utf8.RuneError {
			return s, false
		}
	}
	return s, true
}
