<script>
  // What this edit changed about the RULE, as opposed to about the text.
  //
  // It sits beside the line diff rather than replacing it, because the two answer different
  // questions and an author needs both. Reformatting a file is a large line diff and no change
  // to the rule; inserting one sequence step is a small line diff that shifts every label after
  // it. Neither view can tell you what the other does.
  import { fieldDiff, sequenceDiff, render } from '../../lib/rules/fielddiff.js';
  import { UI } from '../../lib/consts/index.js';

  let {
    /** Parsed rule as loaded, or null when the text does not parse. */
    before = null,
    /** Parsed rule as currently edited, or null. */
    after = null,
    schema = null,
  } = $props();

  const result = $derived(fieldDiff(before, after, schema));
  const steps = $derived(sequenceDiff(before ?? {}, after ?? {}));
  const stepsChanged = $derived(steps.some((s) => s.op !== 'same'));

  // The sequence has its own aligned view below, so showing it as a one-line value change as
  // well would say the same thing twice and less clearly.
  const rows = $derived(result.rows.filter((r) => r.name !== 'sequence' && r.name !== 'labels'));
</script>

<div class="fdiff">
  <h3>
    {UI.RULE_EDITOR_FIELD_DIFF}
    {#if result.comparable && result.changed > 0}
      <span class="tally">{result.changed} {UI.RULE_EDITOR_FIELDS_CHANGED}</span>
    {/if}
  </h3>

  {#if !result.comparable}
    <!-- Said rather than shown as an empty table, which would read as "nothing changed". -->
    <p class="msg">{UI.RULE_EDITOR_FIELD_DIFF_UNAVAILABLE}</p>
  {:else if result.changed === 0}
    <p class="msg">{UI.RULE_EDITOR_DIFF_NONE}</p>
  {:else}
    <table>
      <tbody>
        {#each rows as r (r.name)}
          <tr class={r.op}>
            <th>{r.name}</th>
            <td class="was">{render(r.before)}</td>
            <td class="arrow">{r.op === 'same' ? '' : '→'}</td>
            <td class="now">{render(r.after)}</td>
          </tr>
        {/each}
      </tbody>
    </table>

    {#if stepsChanged}
      <!-- Steps aligned as a chain, so an inserted step reads as an insertion rather than as
           every step after it changing. Each row carries the label on the connection LEAVING
           it, which is how the format defines labels. -->
      <h4>{UI.RULE_EDITOR_SEQUENCE_DIFF}</h4>
      <ol class="steps">
        {#each steps as s, i (i)}
          <li class={s.op}>
            <span class="mark">{s.op === 'added' ? '+' : s.op === 'removed' ? '−' : s.op === 'changed' ? '~' : ''}</span>
            <span class="step">
              {#if s.op === 'added'}{s.after}
              {:else if s.op === 'removed'}{s.before}
              {:else}{s.after}{/if}
            </span>
            {#if s.beforeLabel !== s.afterLabel}
              <span class="lbl">
                {#if s.beforeLabel}<s>{s.beforeLabel}</s>{/if}
                {#if s.afterLabel}<em>{s.afterLabel}</em>{/if}
              </span>
            {:else if s.afterLabel}
              <span class="lbl quiet">{s.afterLabel}</span>
            {/if}
          </li>
        {/each}
      </ol>
    {/if}
  {/if}
</div>

<style>
  .fdiff {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    padding: var(--space-3);
    overflow-y: auto;
  }
  h3 {
    margin: 0;
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.78rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  h4 {
    margin: var(--space-3) 0 0;
    font-family: var(--font-sans);
    font-size: 0.66rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  .tally {
    font-size: 0.7rem;
    text-transform: none;
    letter-spacing: 0;
    color: var(--color-primary);
  }
  .msg {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
    line-height: 1.5;
  }

  table {
    width: 100%;
    border-collapse: collapse;
    font-family: var(--font-sans);
    font-size: 0.74rem;
  }
  th {
    text-align: left;
    font-weight: 600;
    color: var(--color-on-surface-muted);
    padding: 2px var(--space-2) 2px 0;
    white-space: nowrap;
    vertical-align: top;
  }
  td {
    padding: 2px 0;
    color: var(--color-on-surface);
    word-break: break-word;
    vertical-align: top;
  }
  .arrow {
    padding: 0 var(--space-2);
    color: var(--color-on-surface-muted);
    width: 1em;
  }
  tr.same th,
  tr.same td {
    opacity: 0.55;
  }
  tr.removed .now,
  tr.added .was {
    color: var(--color-on-surface-muted);
  }
  tr.removed .was {
    text-decoration: line-through;
    color: var(--color-error);
  }
  tr.added .now,
  tr.changed .now {
    color: var(--color-success);
  }
  tr.changed .was {
    text-decoration: line-through;
    color: var(--color-on-surface-muted);
  }

  .steps {
    margin: 0;
    padding: 0;
    list-style: none;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .steps li {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.76rem;
  }
  .mark {
    width: 1ch;
    font-weight: 700;
    color: var(--color-on-surface-muted);
  }
  li.added .mark,
  li.added .step {
    color: var(--color-success);
  }
  li.removed .mark,
  li.removed .step {
    color: var(--color-error);
    text-decoration: line-through;
  }
  li.changed .mark {
    color: var(--color-warning);
  }
  li.same .step {
    opacity: 0.6;
  }
  .step {
    font-weight: 600;
    color: var(--color-on-surface);
  }
  .lbl {
    font-size: 0.7rem;
    color: var(--color-on-surface-muted);
    display: inline-flex;
    gap: var(--space-2);
  }
  .lbl.quiet {
    opacity: 0.6;
  }
  .lbl em {
    font-style: italic;
    color: var(--color-success);
  }
  .lbl s {
    color: var(--color-error);
  }
</style>
