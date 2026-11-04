// Selection logic for a CLOSED list field (match_fields): a fixed vocabulary shown as toggles.
//
// Pure, and out of the component, because the component had a bug that only shows on files the
// form did not write: a toggle rebuilt the list from the vocabulary alone, so any value not in
// it — an alias such as src_ip, or a typo the validator is reporting — vanished on the next
// click. For a global-scope rule losing its only shared field, that turns "same host" into
// "anything in the case".
//
// Three rules:
//
//   - An alias lights the chip of the field it stands for, using the SERVED alias map, so the
//     form accepts exactly what the loader does.
//   - A click changes only the chip clicked. Every other value keeps the spelling the author
//     wrote, and values outside the vocabulary are kept, after the known ones, so the validator
//     can still report them instead of the form quietly deleting them.
//   - Known values follow vocabulary order rather than click order, so two rules selecting the
//     same fields are byte-identical however they were assembled. One chip is one field: a
//     second spelling of a field already listed is collapsed into the first.

/**
 * canonicalChoice resolves one list item to the vocabulary value it stands for.
 *
 * @param {string} item
 * @param {Record<string,string>} [aliases]
 * @returns {string}
 */
export function canonicalChoice(item, aliases = {}) {
  const name = String(item ?? '').trim();
  return Object.hasOwn(aliases, name) ? aliases[name] : name;
}

/**
 * selectedChoices is the set of vocabulary values a list selects, aliases resolved.
 *
 * @param {string[]} items
 * @param {Record<string,string>} [aliases]
 * @returns {Set<string>}
 */
export function selectedChoices(items, aliases = {}) {
  return new Set((items || []).map((item) => canonicalChoice(item, aliases)));
}

/**
 * toggleChoice returns the list after clicking one option's chip.
 *
 * @param {string[]} items the list as the file holds it
 * @param {string} option the vocabulary value whose chip was clicked
 * @param {string[]} options the vocabulary, in served order
 * @param {Record<string,string>} [aliases]
 * @returns {string[]}
 */
export function toggleChoice(items, option, options, aliases = {}) {
  const current = items || [];
  const known = new Set(options);

  // The author's spelling for each selected value, first occurrence wins.
  const spelling = new Map();
  const unknown = [];
  for (const item of current) {
    const canonical = canonicalChoice(item, aliases);
    if (!known.has(canonical)) {
      unknown.push(item);
    } else if (!spelling.has(canonical)) {
      spelling.set(canonical, String(item).trim());
    }
  }

  if (spelling.has(option)) spelling.delete(option);
  else spelling.set(option, option);

  return [...options.filter((o) => spelling.has(o)).map((o) => spelling.get(o)), ...unknown];
}
