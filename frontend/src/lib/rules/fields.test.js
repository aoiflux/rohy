import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { algorithmOf, appliesTo, isSet, listValue, visibleFields } from './fields.js';

// The descriptor is read from the backend's schema golden rather than hand-written, so these
// tests run against the real field set — including `applies_to`, which is the whole subject.
// A local copy would pass happily while the form showed the wrong controls.
const SCHEMA = JSON.parse(
  readFileSync(
    fileURLToPath(new URL('../../../../backend/rules/testdata/schema-golden.json', import.meta.url)),
    'utf8',
  ),
);

const field = (name) => SCHEMA.fields.find((f) => f.name === name);
const namesFor = (algorithm, value = {}) =>
  visibleFields(SCHEMA.fields, algorithm, value).map((f) => f.name);

describe('algorithmOf', () => {
  it('defaults the way the loader does when the rule omits it', () => {
    expect(algorithmOf({}, SCHEMA)).toBe('sequence');
    expect(algorithmOf({ algorithm: '  ' }, SCHEMA)).toBe('sequence');
  });

  it('reads what the rule selects', () => {
    expect(algorithmOf({ algorithm: 'lineage' }, SCHEMA)).toBe('lineage');
  });
});

describe('visibility', () => {
  it('hides the sequence for lineage, which does not match one', () => {
    // Offering a sequence on a lineage rule invites an author to fill in a field that has no
    // effect, and then to conclude the rule is broken when it ignores it.
    expect(namesFor('lineage')).not.toContain('sequence');
    expect(namesFor('lineage')).not.toContain('labels');
    expect(namesFor('sequence')).toContain('sequence');
  });

  it('shows match_fields only where an algorithm reads it', () => {
    expect(namesFor('field')).toContain('match_fields');
    expect(namesFor('temporal')).toContain('match_fields');
    expect(namesFor('sequence')).not.toContain('match_fields');
    expect(namesFor('lineage')).not.toContain('match_fields');
  });

  it('shows the window only for temporal', () => {
    expect(namesFor('temporal')).toContain('window_within');
    for (const algorithm of ['sequence', 'field', 'lineage']) {
      expect(namesFor(algorithm)).not.toContain('window_within');
    }
  });

  it('shows the lineage settings only for lineage', () => {
    expect(namesFor('lineage')).toContain('lineage_create_ids');
    expect(namesFor('sequence')).not.toContain('lineage_create_ids');
  });

  it('always shows the fields every algorithm shares', () => {
    for (const algorithm of SCHEMA.algorithms.map((a) => a.name)) {
      const shown = namesFor(algorithm);
      for (const always of ['name', 'description', 'algorithm', 'channels', 'format_version']) {
        expect(shown, `${algorithm} must show ${always}`).toContain(always);
      }
    }
  });

  // The rule that matters most: hiding a field is fine, hiding a VALUE is not.
  it('keeps an inapplicable field visible when the file sets it', () => {
    const rule = { algorithm: 'sequence', match_fields: ['logon_id'] };
    const shown = visibleFields(SCHEMA.fields, 'sequence', rule);
    const mf = shown.find((f) => f.name === 'match_fields');
    expect(mf, 'a set value must never be hidden — it is still saved, and hiding it leaves no way to remove it').toBeTruthy();
    expect(mf.inert).toBe(true);
  });

  it('marks applicable fields as not inert', () => {
    const shown = visibleFields(SCHEMA.fields, 'field', { algorithm: 'field' });
    expect(shown.find((f) => f.name === 'match_fields').inert).toBe(false);
  });

  it('does not resurrect a field that is merely present-but-empty', () => {
    // An empty array is not a value an author would lose, so there is nothing to keep visible.
    const shown = visibleFields(SCHEMA.fields, 'sequence', { match_fields: [] });
    expect(shown.find((f) => f.name === 'match_fields')).toBeUndefined();
  });
});

describe('appliesTo', () => {
  it('treats a field with no applies_to as universal', () => {
    expect(appliesTo({ name: 'name' }, 'lineage')).toBe(true);
    expect(appliesTo({ name: 'x', applies_to: [] }, 'lineage')).toBe(true);
  });

  it('reads the served list rather than hardcoding membership', () => {
    expect(appliesTo(field('window_within'), 'temporal')).toBe(true);
    expect(appliesTo(field('window_within'), 'sequence')).toBe(false);
  });
});

describe('isSet', () => {
  it('distinguishes a real value from an absent one', () => {
    expect(isSet({ a: 'x' }, { name: 'a' })).toBe(true);
    expect(isSet({ a: '' }, { name: 'a' })).toBe(false);
    expect(isSet({ a: '  ' }, { name: 'a' })).toBe(false);
    expect(isSet({ a: [] }, { name: 'a' })).toBe(false);
    expect(isSet({ a: ['x'] }, { name: 'a' })).toBe(true);
    expect(isSet({}, { name: 'a' })).toBe(false);
    expect(isSet({ a: null }, { name: 'a' })).toBe(false);
  });

  it('counts zero as set, because a numeric field can legitimately be zero', () => {
    // lineage_depth 0 is the DEFAULT and a meaningful choice, not an absence.
    expect(isSet({ lineage_depth: 0 }, { name: 'lineage_depth' })).toBe(true);
  });
});

describe('format version', () => {
  // The format has ONE version, and that is a decision rather than an accident: the three
  // algorithms added in v0.2.0 looked like a breaking change, but a build that does not
  // implement one refuses it BY NAME, which is both sufficient and more useful than a version
  // number. A second version would have bought nothing and cost a concept.
  it('is a single version the whole descriptor shares', () => {
    expect(SCHEMA.format_version).toBe(1);
  });

  it('does not vary by algorithm', () => {
    for (const a of SCHEMA.algorithms) {
      expect(a.min_format_version, `${a.name} still carries a per-algorithm version`).toBeUndefined();
    }
  });

  it('does not vary by field', () => {
    for (const f of SCHEMA.fields) {
      expect(f.requires_format_version, `${f.name} still carries a per-field version`).toBeUndefined();
    }
  });
});

describe('listValue', () => {
  it('returns the array a list control edits', () => {
    expect(listValue({ channels: ['Security'] }, 'channels')).toEqual(['Security']);
  });

  it('degrades a malformed value to an empty list rather than throwing', () => {
    // A rule carrying a string where an array belongs is a type error the validator reports;
    // the form still has to render something the author can fix.
    expect(listValue({ channels: 'Security' }, 'channels')).toEqual([]);
    expect(listValue({}, 'channels')).toEqual([]);
    expect(listValue(null, 'channels')).toEqual([]);
  });

  it('stringifies non-string entries instead of dropping them', () => {
    expect(listValue({ sequence: [4624, '1102'] }, 'sequence')).toEqual(['4624', '1102']);
  });
});
