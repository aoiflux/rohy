package graphene

import (
	"path/filepath"
	"strconv"
	"strings"

	"rohy/backend/consts"
)

// This file is the correlation-key projection: the small, bounded slice of an event's
// EventData that field, temporal and lineage correlation actually match on.
//
// WHY IT LIVES HERE, beside ComputeNormalizedHash, rather than in each parser:
//
//   - Same reason identity does. There are three normalizers (binary-XML file, live XML,
//     SQLite row) and three copies of an extraction rule would drift, which would show up as
//     the same event correlating from one source and not from another — the exact failure the
//     hashing rules were centralized to prevent.
//   - And a harder reason: the BACKFILL that projects existing cases lives in this package,
//     and this package cannot import evtx (the dependency runs evtx → graphene). If extraction
//     lived up in the parsers, the backfill could not reach it, and a second implementation
//     would be guaranteed to disagree with the first on some field eventually.
//
// WHAT IT IS NOT: it is not a second copy of the event. ParsedFields stays in the cold store
// untouched. This is a projection of a dozen scalars chosen because rules match on them, and
// it is capped so its cost is a property of the vocabulary rather than of how verbose a
// particular log happens to be.

// ComputeCorrelationKeys fills the event's correlation projection from its ParsedFields and
// stamps the recipe version that produced it.
//
// Callers must set ParsedFields first. An event with no parsed fields gets a nil projection
// and the current version — that is a correct answer ("this event has nothing to correlate
// on"), distinct from version 0, which means "never projected".
func (e *Event) ComputeCorrelationKeys() {
	e.CorrKeys = ExtractCorrelationKeys(e.ParsedFields)
	e.CorrKeyVersion = consts.CorrelationKeyVersion
}

// CorrelationKey returns the value stored in a slot, or "" when the slot is empty or out of
// range. Out of range is not an error: an event written by an older, shorter vocabulary
// simply has no value there.
func (e *Event) CorrelationKey(slot int) string {
	if slot < 0 || slot >= len(e.CorrKeys) {
		return ""
	}
	return e.CorrKeys[slot]
}

// HasCurrentCorrelationKeys reports whether this event was projected by the recipe this build
// uses. A false answer means a build correlating on fields would under-report, which is why
// runs surface the count rather than quietly returning a small result.
func (e *Event) HasCurrentCorrelationKeys() bool {
	return e.CorrKeyVersion == consts.CorrelationKeyVersion
}

// ExtractCorrelationKeys projects EventData onto the fixed slot vocabulary.
//
// The returned slice is positional (index == consts.Slot*) with trailing empties trimmed, so
// an event that only carries a logon id costs one entry rather than twelve. It is nil when
// nothing was found.
func ExtractCorrelationKeys(fields map[string]string) []string {
	if len(fields) == 0 {
		return nil
	}

	// EventData field names vary in case between providers and between the binary-XML and
	// rendered-XML representations of the same record, so lookup is case-insensitive. Building
	// the folded index once is what keeps this a single pass rather than a scan per slot.
	folded := make(map[string]string, len(fields))
	for k, v := range fields {
		lk := strings.ToLower(strings.TrimSpace(k))
		if lk == "" {
			continue
		}
		// First spelling wins. Two keys differing only in case is pathological; picking
		// deterministically matters more than which one is picked, and map iteration order
		// would make the choice vary between runs on the same input.
		if _, seen := folded[lk]; !seen {
			folded[lk] = v
		}
	}

	out := make([]string, consts.CorrelationSlotCount)
	last := -1
	for slot := 0; slot < consts.CorrelationSlotCount; slot++ {
		class := consts.CorrelationSlotClasses[slot]
		maxLen := consts.CorrelationSlotMaxLen[slot]
		for _, source := range consts.CorrelationSlotSources[slot] {
			raw, ok := folded[strings.ToLower(source)]
			if !ok {
				continue
			}
			value := canonicalizeSlot(raw, class, maxLen)
			if value == "" {
				continue // present but meaningless; try the next spelling
			}
			out[slot] = value
			last = slot
			break
		}
	}
	if last < 0 {
		return nil
	}
	return out[:last+1]
}

// canonicalizeSlot turns one raw EventData value into the form stored in a slot, or "" when
// the value carries no information. maxLen is the slot's own limit — each slot is sized to
// its actual domain rather than sharing one generous cap, which is what keeps the projection
// within budget (see consts.CorrelationSlotMaxLen).
func canonicalizeSlot(raw, class string, maxLen int) string {
	v := strings.ToLower(strings.TrimSpace(raw))
	if v == "" {
		return ""
	}
	for _, absent := range consts.CorrelationAbsentValues {
		if v == absent {
			return ""
		}
	}

	switch class {
	case consts.SlotClassIdentifier:
		if n, ok := parseIdentifier(v); ok {
			if n == 0 && consts.CorrelationZeroIsAbsent(class) {
				return "" // logon id 0x0 / process id 0 mean "none", not "zero"
			}
			return "0x" + strconv.FormatUint(n, 16)
		}
		// Not a number. Kept verbatim rather than dropped, so a provider using an opaque
		// identifier still correlates with itself.
	case consts.SlotClassImage:
		v = imageBasename(v)
		if v == "" {
			return ""
		}
	}
	if maxLen <= 0 {
		maxLen = consts.CorrelationValueMaxLen
	}
	return truncateRunes(v, maxLen)
}

// parseIdentifier reads a Windows identifier written as either decimal or 0x-prefixed hex.
// Both spellings occur for the same field across providers, and unifying them is what lets a
// process id correlate across a Security record and a Sysmon record.
func parseIdentifier(v string) (uint64, bool) {
	if strings.HasPrefix(v, "0x") {
		n, err := strconv.ParseUint(v[2:], 16, 64)
		return n, err == nil
	}
	n, err := strconv.ParseUint(v, 10, 64)
	return n, err == nil
}

// imageBasename reduces an image path to its final component. It handles both separators
// explicitly rather than relying on the host's, because a case is routinely analysed on a
// different platform from the one it was captured on — filepath.Base alone would leave
// "c:\windows\system32\cmd.exe" untouched on Linux.
func imageBasename(v string) string {
	if i := strings.LastIndexAny(v, `\/`); i >= 0 {
		v = v[i+1:]
	}
	return strings.TrimSpace(filepath.Base(v))
}

// truncateRunes caps a value at n bytes without splitting a rune, so a truncated value is
// still valid UTF-8 and still renders in a relation's basis.
func truncateRunes(v string, n int) string {
	if len(v) <= n {
		return v
	}
	cut := n
	for cut > 0 && !utf8Start(v[cut]) {
		cut--
	}
	return v[:cut]
}

// utf8Start reports whether b begins a UTF-8 sequence (i.e. is not a continuation byte).
func utf8Start(b byte) bool { return b&0xC0 != 0x80 }
