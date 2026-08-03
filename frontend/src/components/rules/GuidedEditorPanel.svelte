<script>
  // Guided mode: the rule as a form, generated from the schema descriptor.
  //
  // Nothing about the format is hardcoded here. Which fields exist, what they mean, which
  // values they allow, and how they are grouped all come from the descriptor the backend
  // serves — so this panel gains a control the day a field is added to the format, and can
  // never offer one the loader would refuse.
  //
  // Fields this build does not interpret get their own read-only section rather than being
  // hidden. A form that silently omitted a field would look exactly like one that had
  // deleted it, and RULES.md §3 promises the opposite: an unrecognized field is ignored, not
  // rejected, and it survives a save.
  import { RULE_FIELD_GROUPS, UI } from '../../lib/consts/index.js';
  import { slug } from '../../lib/rules/document.js';
  import { algorithmOf, listValue, visibleFields } from '../../lib/rules/fields.js';
  import FieldRow from './FieldRow.svelte';
  import ListField from './ListField.svelte';
  import SequenceBuilder from './SequenceBuilder.svelte';

  let {
    schema,
    /** The parsed rule object. */
    value = {},
    unknown = {},
    problems = { errors: [], warnings: [] },
    /** (key, value) => void — patches one field. */
    onfield = undefined,
    /** (changes) => void — patches several as one edit. */
    onfields = undefined,
  } = $props();

  const GROUP_TITLES = {
    [RULE_FIELD_GROUPS.IDENTITY]: UI.RULE_EDITOR_GROUP_IDENTITY,
    [RULE_FIELD_GROUPS.MATCHER]: UI.RULE_EDITOR_GROUP_MATCHER,
    [RULE_FIELD_GROUPS.METADATA]: UI.RULE_EDITOR_GROUP_METADATA,
  };
  const GROUP_BLURBS = {
    [RULE_FIELD_GROUPS.IDENTITY]: UI.RULE_EDITOR_GROUP_IDENTITY_BLURB,
    [RULE_FIELD_GROUPS.MATCHER]: UI.RULE_EDITOR_GROUP_MATCHER_BLURB,
    [RULE_FIELD_GROUPS.METADATA]: UI.RULE_EDITOR_GROUP_METADATA_BLURB,
  };

  // Which algorithm the rule selects decides which controls exist at all — a lineage rule has
  // no sequence and no window, and offering them would invite an author to fill in fields that
  // do nothing. A field the algorithm does not read but the FILE already sets stays visible,
  // marked inert: the value is still there and still saved, and a control that vanished along
  // with its value would leave no way to remove it.
  const algorithm = $derived(algorithmOf(value, schema));

  const groups = $derived(
    (schema?.group_order || []).map((group) => ({
      key: group,
      title: GROUP_TITLES[group] || group,
      blurb: GROUP_BLURBS[group] || '',
      fields: visibleFields(
        (schema?.fields || []).filter((f) => f.group === group),
        algorithm,
        value,
      ),
    })),
  );

  /** The prose for the selected algorithm, served alongside the field descriptors. */
  const algorithmSummary = $derived(
    (schema?.algorithms || []).find((a) => a.name === algorithm)?.summary || '',
  );

  const unknownKeys = $derived(Object.keys(unknown || {}));

  /** Problems concerning one field, flattened so FieldRow can render both severities. */
  function problemsFor(name) {
    return [
      ...(problems.errors || []).filter((p) => p.field === name).map((p) => ({ ...p, warning: false })),
      ...(problems.warnings || []).filter((p) => p.field === name).map((p) => ({ ...p, warning: true })),
    ];
  }

  /** The id this name would produce. Shown live because the id is not cosmetic: it is the
   *  rule's identity, and changing the name replaces the rule rather than editing it. */
  const ruleId = $derived(slug(value?.name || ''));

  function text(name) {
    const v = value?.[name];
    return typeof v === 'string' ? v : v === undefined || v === null ? '' : String(v);
  }
</script>

<div class="guided">
  {#each groups as group (group.key)}
    {#if group.fields.length}
      <section class="group">
        <h3>{group.title}</h3>
        {#if group.blurb}<p class="blurb">{group.blurb}</p>{/if}

        {#each group.fields as field (field.name)}
          <FieldRow
            {field}
            problems={problemsFor(field.name)}
            inert={field.inert}
            note={field.name === 'name' && ruleId ? `${UI.RULE_EDITOR_ID_PREVIEW} ${ruleId}` : ''}
          >
            {#if field.name === 'sequence'}
              <SequenceBuilder
                sequence={Array.isArray(value?.sequence) ? value.sequence : []}
                labels={Array.isArray(value?.labels) ? value.labels : []}
                maxSteps={field.max_items}
                problems={problemsFor('sequence')}
                onchange={(changes) => onfields?.(changes)}
              />
            {:else if field.name === 'labels'}
              <!-- Labels have no control of their own: they are edited between the steps
                   they join, in the sequence builder above, which is what makes the
                   off-by-one in labels[i] impossible to make. -->
              <p class="deferred">{UI.RULE_EDITOR_LABELS_INLINE}</p>
            {:else if field.kind === 'string[]'}
              <!-- A list field, whether closed (match_fields picks from the served correlation
                   vocabulary) or open (channels, lineage_create_ids). Rendering these as the
                   single-value <select> below would write a STRING into an array field, which
                   the loader then refuses as a type error. -->
              <ListField
                {field}
                items={listValue(value, field.name)}
                onchange={(next) => onfield?.(field.name, next)}
              />
            {:else if field.enum?.length}
              <select
                id={`field-${field.name}`}
                value={text(field.name) || field.default || ''}
                onchange={(e) => onfield?.(field.name, e.currentTarget.value)}
              >
                {#each field.enum as option (option)}
                  <option value={option}>{option}{option === field.default ? ` (${UI.RULE_EDITOR_DEFAULT})` : ''}</option>
                {/each}
              </select>
              {#if field.name === 'algorithm' && algorithmSummary}
                <!-- What the choice MEANS, beside the choice. The algorithm decides what a
                     match establishes, which is the one thing a rule author most needs and
                     cannot infer from a name in a dropdown. -->
                <p class="algo-summary">{algorithmSummary}</p>
              {/if}
            {:else if field.read_only}
              <!-- format_version is shown, not edited. Declaring a version this build does
                   not understand makes the file refuse to load here, and there is no reason
                   for a form to make that easy to do by accident. Raw mode still allows it. -->
              <input id={`field-${field.name}`} class="ro" value={value?.[field.name] ?? schema.format_version} readonly />
            {:else if field.kind === 'integer'}
              <input
                id={`field-${field.name}`}
                type="number"
                value={value?.[field.name] ?? ''}
                oninput={(e) => onfield?.(field.name, Number(e.currentTarget.value))}
              />
            {:else if field.name === 'description'}
              <textarea
                id={`field-${field.name}`}
                rows="3"
                value={text('description')}
                oninput={(e) => onfield?.('description', e.currentTarget.value)}
              ></textarea>
            {:else}
              <input
                id={`field-${field.name}`}
                value={text(field.name)}
                oninput={(e) => onfield?.(field.name, e.currentTarget.value)}
              />
            {/if}
          </FieldRow>
        {/each}
      </section>
    {/if}
  {/each}

  {#if unknownKeys.length}
    <section class="group unknown">
      <h3>{UI.RULE_EDITOR_GROUP_UNKNOWN}</h3>
      <p class="blurb">{UI.RULE_EDITOR_GROUP_UNKNOWN_BLURB}</p>
      {#each unknownKeys as key (key)}
        <div class="unknownrow">
          <code class="ukey">{key}</code>
          <code class="uval">{JSON.stringify(unknown[key])}</code>
        </div>
      {/each}
    </section>
  {/if}
</div>

<style>
  .guided {
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
    overflow-y: auto;
    padding-right: var(--space-2);
  }
  .group {
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-4);
    background: var(--color-surface);
  }
  h3 {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.78rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-primary);
  }
  .blurb {
    margin: var(--space-2) 0 var(--space-3);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    color: var(--color-on-surface-muted);
  }
  .deferred {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.75rem;
    font-style: italic;
    color: var(--color-on-surface-muted);
  }
  /* What the selected algorithm MEANS, beside the selector — the one thing a rule author most
     needs and cannot infer from a name in a dropdown. */
  .algo-summary {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.78rem;
    line-height: 1.5;
    color: var(--color-on-surface-muted);
    border-left: 2px solid var(--color-primary);
    padding-left: var(--space-3);
  }

  input,
  select,
  textarea {
    width: 100%;
    box-sizing: border-box;
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-2) var(--space-3);
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.85rem;
    outline: none;
  }
  textarea {
    resize: vertical;
    line-height: 1.5;
  }
  input:focus,
  select:focus,
  textarea:focus {
    border-color: var(--color-primary);
  }
  input.ro {
    color: var(--color-on-surface-muted);
    font-family: var(--font-mono);
    cursor: default;
  }

  /* Carried but not read. Marked as inert so it cannot be mistaken for something this build
     acts on, while still being visibly present rather than quietly dropped. */
  .unknown {
    border-style: dashed;
    background: none;
  }
  .unknown h3 {
    color: var(--color-on-surface-muted);
  }
  .unknownrow {
    display: flex;
    gap: var(--space-3);
    padding: var(--space-2) 0;
    border-top: 1px solid var(--color-outline);
  }
  .ukey {
    font-family: var(--font-mono);
    font-size: 0.78rem;
    color: var(--color-on-surface);
    flex: 0 0 auto;
  }
  .uval {
    font-family: var(--font-mono);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
    word-break: break-all;
  }
</style>
