<script>
  // The rule file as it stands, read-only and highlighted.
  //
  // It exists so guided mode is not a black box: someone filling in a form can watch the
  // file they are actually producing, which is the file that will be read back by anyone
  // they send the rule to. It is also what makes the switch to raw mode unsurprising.
  import { segments } from '../../lib/rules/highlight.js';
  import { UI } from '../../lib/consts/index.js';

  let { text = '', title = UI.RULE_EDITOR_PREVIEW } = $props();

  const parts = $derived(segments(text));
</script>

<div class="preview">
  <h3>{title}</h3>
  <pre class="code">{#each parts as part}{#if part.type}<span class="t-{part.type}">{part.text}</span>{:else}{part.text}{/if}{/each}</pre>
</div>

<style>
  .preview {
    display: flex;
    flex-direction: column;
    min-height: 0;
    flex: 1;
  }
  h3 {
    margin: 0 0 var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.72rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  /* Horizontal scroll rather than wrapping: wrapping would misrepresent how the file is
     actually written, which is the one thing a preview must not do. */
  .code {
    flex: 1;
    min-height: 0;
    margin: 0;
    padding: var(--space-3);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    font-family: var(--font-mono);
    font-size: 0.76rem;
    line-height: 1.55;
    overflow: auto;
    white-space: pre;
    tab-size: 2;
    color: var(--color-on-surface);
  }

  .t-key {
    color: var(--color-primary);
    font-weight: 700;
  }
  .t-string {
    color: var(--color-success);
  }
  .t-number {
    color: var(--color-accent);
  }
  .t-boolean,
  .t-null {
    color: var(--color-warning);
  }
  .t-punct {
    color: var(--color-on-surface-muted);
  }
  .t-error {
    color: var(--color-error);
  }
</style>
