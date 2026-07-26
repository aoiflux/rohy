package rules

import (
	"bytes"
	"encoding/json"
	"fmt"
	"strings"
	"unicode/utf8"

	"rohy/backend/consts"
)

// Rule-file formatting.
//
// Pretty and Minify work on BYTES, never on a Spec. Round-tripping through the struct would
// be far less code, and would quietly destroy the two things the format promises to keep:
// field order, and any field this build does not interpret (RULES.md §3). The rule
// inspector already refuses that trade — it reads a file verbatim rather than
// re-serializing it — and an editor that reformats a file must be at least as careful, or
// pressing "Pretty" on a rule written for a newer rohy would delete the fields that make it
// work.
//
// The output matches the built-in library's house style rather than encoding/json's: one
// field per line, and short arrays kept on one line, because a sequence reads as a sequence
// when its steps sit side by side and stops reading as one when each step is its own line.

// Pretty re-indents rule text in rohy's house style.
func Pretty(data []byte) ([]byte, error) {
	value, err := decodeOrdered(data)
	if err != nil {
		return nil, fmt.Errorf(consts.MsgRuleParseFailed, err)
	}
	var b strings.Builder
	// The root is always expanded, even when it would fit on one line: a rule file is read
	// field by field, and collapsing a two-field rule onto a single line would make the
	// shortest rules the hardest ones to scan.
	value.render(&b, 0, true, 0)
	b.WriteByte('\n') // files end with a newline
	return []byte(b.String()), nil
}

// Minify strips insignificant whitespace without touching values or their order.
func Minify(data []byte) ([]byte, error) {
	var out bytes.Buffer
	if err := json.Compact(&out, data); err != nil {
		return nil, fmt.Errorf(consts.MsgRuleParseFailed, err)
	}
	return out.Bytes(), nil
}

// value is a parsed JSON value that remembers the order its object keys were written in.
// encoding/json's map[string]any does not, which is the whole reason this type exists.
type value struct {
	kind   byte // 'o' object, 'a' array, 's' scalar
	keys   []string
	fields map[string]*value
	items  []*value
	raw    string // a scalar rendered back to JSON text
}

// decodeOrdered parses JSON into the ordered representation above.
func decodeOrdered(data []byte) (*value, error) {
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.UseNumber() // keep 1.0 as "1.0" rather than promoting it to a float and back
	v, err := decodeValue(dec)
	if err != nil {
		return nil, err
	}
	// Trailing content is a syntax error, not something to silently drop.
	if dec.More() {
		return nil, fmt.Errorf("unexpected content after the top-level value")
	}
	return v, nil
}

func decodeValue(dec *json.Decoder) (*value, error) {
	tok, err := dec.Token()
	if err != nil {
		return nil, err
	}
	delim, isDelim := tok.(json.Delim)
	if !isDelim {
		return &value{kind: 's', raw: encodeScalar(tok)}, nil
	}
	switch delim {
	case '{':
		out := &value{kind: 'o', fields: map[string]*value{}}
		for dec.More() {
			keyTok, err := dec.Token()
			if err != nil {
				return nil, err
			}
			key, ok := keyTok.(string)
			if !ok {
				return nil, fmt.Errorf("object key is not a string")
			}
			child, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			// A duplicate key keeps its first position but takes the last value, which is
			// what encoding/json does when it unmarshals the same file.
			if _, seen := out.fields[key]; !seen {
				out.keys = append(out.keys, key)
			}
			out.fields[key] = child
		}
		_, err := dec.Token() // '}'
		return out, err
	case '[':
		out := &value{kind: 'a'}
		for dec.More() {
			child, err := decodeValue(dec)
			if err != nil {
				return nil, err
			}
			out.items = append(out.items, child)
		}
		_, err := dec.Token() // ']'
		return out, err
	default:
		return nil, fmt.Errorf("unexpected %q", delim)
	}
}

// encodeScalar renders a scalar token back to JSON text with HTML escaping OFF. A rule
// description containing "&" or "<" — Windows event prose contains both — must read back as
// the author typed it, not as &.
func encodeScalar(tok json.Token) string {
	switch v := tok.(type) {
	case nil:
		return "null"
	case bool:
		if v {
			return "true"
		}
		return "false"
	case json.Number:
		return v.String() // the original digits, not a re-derived float
	case string:
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		_ = enc.Encode(v)
		return strings.TrimRight(buf.String(), "\n")
	default:
		encoded, _ := json.Marshal(v)
		return string(encoded)
	}
}

// inline renders a value on one line. It is both the compact form and the measurement used
// to decide whether the expanded form is needed.
func (v *value) inline() string {
	switch v.kind {
	case 's':
		return v.raw
	case 'a':
		parts := make([]string, len(v.items))
		for i, item := range v.items {
			parts[i] = item.inline()
		}
		return "[" + strings.Join(parts, ", ") + "]"
	default:
		parts := make([]string, len(v.keys))
		for i, key := range v.keys {
			parts[i] = encodeScalar(key) + ": " + v.fields[key].inline()
		}
		return "{" + strings.Join(parts, ", ") + "}"
	}
}

// render writes v at the given indent depth. Containers stay on one line when they fit the
// width budget, and expand when they do not — so a four-step sequence reads as a chain while
// a two-hundred-step one becomes a list instead of a line nobody can scroll.
//
// used is how many columns the current line has already consumed, INCLUDING the "key": that
// precedes an object value. Measuring only the value would let a short value hanging off a
// long key overflow the budget, which is precisely the case the width limit exists for.
// Columns are counted in runes: an em-dash is one column wide, not three.
func (v *value) render(b *strings.Builder, depth int, forceExpand bool, used int) {
	pad := strings.Repeat(consts.RuleFormatIndent, depth)
	inner := pad + consts.RuleFormatIndent

	if v.kind == 's' {
		b.WriteString(v.raw)
		return
	}
	if !forceExpand && used+utf8.RuneCountInString(v.inline()) <= consts.RuleFormatWidth {
		b.WriteString(v.inline())
		return
	}

	if v.kind == 'a' {
		if len(v.items) == 0 {
			b.WriteString("[]")
			return
		}
		b.WriteString("[\n")
		for i, item := range v.items {
			b.WriteString(inner)
			item.render(b, depth+1, false, utf8.RuneCountInString(inner))
			if i < len(v.items)-1 {
				b.WriteByte(',')
			}
			b.WriteByte('\n')
		}
		b.WriteString(pad + "]")
		return
	}

	if len(v.keys) == 0 {
		b.WriteString("{}")
		return
	}
	b.WriteString("{\n")
	for i, key := range v.keys {
		prefix := inner + encodeScalar(key) + ": "
		b.WriteString(prefix)
		v.fields[key].render(b, depth+1, false, utf8.RuneCountInString(prefix))
		if i < len(v.keys)-1 {
			b.WriteByte(',')
		}
		b.WriteByte('\n')
	}
	b.WriteString(pad + "}")
}
