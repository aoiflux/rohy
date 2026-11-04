import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { fileURLToPath } from 'node:url';
import { canonicalChoice, selectedChoices, toggleChoice } from './choices.js';

// The served descriptor, so the vocabulary and the aliases are the ones the loader uses.
const SCHEMA = JSON.parse(
  readFileSync(
    fileURLToPath(new URL('../../../../backend/rules/testdata/schema-golden.json', import.meta.url)),
    'utf8',
  ),
);
const MATCH_FIELDS = SCHEMA.fields.find((f) => f.name === 'match_fields');
const OPTIONS = MATCH_FIELDS.enum;
const ALIASES = MATCH_FIELDS.aliases;
const toggle = (items, option) => toggleChoice(items, option, OPTIONS, ALIASES);

describe('choices — closed list selection', () => {
  it('serves src_ip as an alias of ip_address', () => {
    // If the descriptor stops serving it, every test below passes vacuously.
    expect(ALIASES).toEqual({ src_ip: 'ip_address' });
  });

  it('lights the chip an alias stands for', () => {
    expect(canonicalChoice('src_ip', ALIASES)).toBe('ip_address');
    expect(selectedChoices(['src_ip'], ALIASES).has('ip_address')).toBe(true);
  });

  it('keeps an alias as written when another chip is clicked', () => {
    // The regression: rebuilding from the vocabulary alone dropped src_ip, leaving a global
    // rule with no shared field at all.
    expect(toggle(['src_ip'], 'dst_port')).toEqual(['src_ip', 'dst_port']);
  });

  it('removes an alias when the chip it lights is clicked off', () => {
    expect(toggle(['src_ip', 'dst_port'], 'ip_address')).toEqual(['dst_port']);
  });

  it('keeps values outside the vocabulary so the validator can still report them', () => {
    expect(toggle(['logon_idd', 'logon_id'], 'dst_ip')).toEqual(['logon_id', 'dst_ip', 'logon_idd']);
  });

  it('orders known values by the vocabulary, not by click order', () => {
    const a = toggle(toggle([], 'dst_ip'), 'logon_id');
    const b = toggle(toggle([], 'logon_id'), 'dst_ip');
    expect(a).toEqual(b);
    expect(a).toEqual(['logon_id', 'dst_ip']);
  });

  it('collapses a second spelling of a field already listed', () => {
    // One chip is one field; the validator calls the pair a duplicate.
    expect(toggle(['ip_address', 'src_ip'], 'dst_port')).toEqual(['ip_address', 'dst_port']);
  });

  it('adds the canonical name when a chip is switched on', () => {
    expect(toggle([], 'ip_address')).toEqual(['ip_address']);
  });
});
