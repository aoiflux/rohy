<script>
  // What this edit has changed, against the file as it was loaded.
  //
  // Saving a rule overwrites a file, and for an imported rule that file may have come from
  // someone else. Being able to see exactly which lines move before committing is the
  // difference between editing a rule and hoping.
  import { diffLines } from '../../lib/rules/diff.js';
  import { UI } from '../../lib/consts/index.js';

  let { before = '', after = '' } = $props();

  const result = $derived(diffLines(before, after));
  const unchanged = $derived(!result.truncated && result.added === 0 && result.removed === 0);
</script>

<div class="diff">
  <h3>
    {UI.RULE_EDITOR_DIFF}
    {#if !unchanged && !result.truncated}
      <span class="tally">
        <span class="plus">+{result.added}</span>
        <span class="minus">−{result.removed}</span>
      </span>
    {/if}
  </h3>

  {#if result.truncated}
    <p class="msg">{UI.RULE_EDITOR_DIFF_TOO_LARGE}</p>
  {:else if unchanged}
    <p class="msg">{UI.RULE_EDITOR_DIFF_NONE}</p>
  {:else}
    <div class="rows">
      {#each result.rows as row, i (i)}
        <div class="row {row.op}">
          <span class="ln">{row.left ?? ''}</span>
          <span class="ln">{row.right ?? ''}</span>
          <span class="mark" aria-hidden="true">{row.op === 'add' ? '+' : row.op === 'remove' ? '−' : ' '}</span>
          <span class="text">{row.text}</span>
        </div>
      {/each}
    </div>
  {/if}
</div>

<style>
  .diff {
    display: flex;
    flex-direction: column;
    min-height: 0;
    flex: 1;
  }
  h3 {
    display: flex;
    align-items: baseline;
    gap: var(--space-3);
    margin: 0 0 var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  .tally {
    display: flex;
    gap: var(--space-2);
    font-family: var(--font-mono);
    letter-spacing: 0;
  }
  .plus {
    color: var(--color-success);
  }
  .minus {
    color: var(--color-error);
  }
  .msg {
    margin: 0;
    padding: var(--space-4);
    font-family: var(--font-sans);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
    text-align: center;
  }
  .rows {
    flex: 1;
    min-height: 0;
    overflow: auto;
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-2) 0;
  }
  .row {
    display: grid;
    grid-template-columns: 3ch 3ch 2ch 1fr;
    gap: var(--space-2);
    font-family: var(--font-mono);
    font-size: 0.74rem;
    line-height: 1.6;
    white-space: pre;
  }
  .ln {
    text-align: right;
    color: var(--color-on-surface-muted);
    opacity: 0.7;
  }
  .text {
    padding-right: var(--space-3);
  }
  .row.same .text {
    color: var(--color-on-surface-muted);
  }
  /* Colour AND a +/− marker: colour alone would leave the diff unreadable to anyone who
     cannot distinguish the two hues. */
  .row.add {
    background: color-mix(in srgb, var(--color-success) 14%, transparent);
  }
  .row.add .text,
  .row.add .mark {
    color: var(--color-success);
  }
  .row.remove {
    background: color-mix(in srgb, var(--color-error) 14%, transparent);
  }
  .row.remove .text,
  .row.remove .mark {
    color: var(--color-error);
  }
</style>
