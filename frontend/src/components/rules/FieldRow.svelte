<script>
  // One field in the guided editor: its label, what it is, and — behind a disclosure — how
  // to choose a value and an example of one.
  //
  // The split is deliberate. Description is always visible because a form whose labels need
  // decoding is not guided at all. Guidance and Example are one click away because they are
  // where the format's genuinely surprising rules live (the id is a slug of the name;
  // labels[i] sits between two steps; a higher format_version is refused outright) and
  // printing all of that beside every control would bury the controls.
  //
  // Both strings come from the schema the backend serves, so the help here cannot drift from
  // the rules the loader enforces.
  import { UI } from '../../lib/consts/index.js';

  let {
    field,
    /** Problems concerning this field, already filtered by the caller. */
    problems = [],
    /** An extra note under the control — the live rule id, a step count. */
    note = '',
    children,
  } = $props();

  let showHelp = $state(false);

  const errors = $derived(problems.filter((p) => !p.warning));
  const warnings = $derived(problems.filter((p) => p.warning));
  const example = $derived(
    field?.example === undefined ? '' : typeof field.example === 'string' ? field.example : JSON.stringify(field.example),
  );
</script>

<div class="row" class:bad={errors.length > 0}>
  <div class="head">
    <label class="name" for={`field-${field.name}`}>
      {field.name}
      {#if field.required}<span class="req" title={UI.RULE_EDITOR_REQUIRED}>*</span>{/if}
      {#if field.read_only}<span class="ro">{UI.RULE_EDITOR_READONLY}</span>{/if}
    </label>
    <button
      type="button"
      class="help"
      aria-expanded={showHelp}
      title={UI.RULE_EDITOR_HELP_TOGGLE}
      onclick={() => (showHelp = !showHelp)}
    >
      {showHelp ? '−' : '?'}
    </button>
  </div>

  <p class="desc">{field.description}</p>

  {@render children?.()}

  {#if note}<p class="note">{note}</p>{/if}

  {#each errors as problem}
    <p class="problem error">{problem.message}</p>
  {/each}
  {#each warnings as problem}
    <p class="problem warn">{problem.message}</p>
  {/each}

  {#if showHelp}
    <div class="helpbox">
      <p>{field.guidance}</p>
      {#if example}
        <p class="example"><span>{UI.RULE_EDITOR_EXAMPLE}</span> <code>{example}</code></p>
      {/if}
      {#if field.enum?.length}
        <p class="example"><span>{UI.RULE_EDITOR_ALLOWED}</span> <code>{field.enum.join(', ')}</code></p>
      {/if}
    </div>
  {/if}
</div>

<style>
  .row {
    padding: var(--space-3) 0;
    border-bottom: 1px solid var(--color-outline);
  }
  .row:last-child {
    border-bottom: none;
  }
  .row.bad .name {
    color: var(--color-error);
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .name {
    font-family: var(--font-mono);
    font-size: 0.8rem;
    font-weight: 700;
    color: var(--color-on-surface);
  }
  .req {
    color: var(--color-error);
    margin-left: 2px;
  }
  .ro {
    font-family: var(--font-sans);
    font-size: 0.65rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-on-surface-muted);
    margin-left: var(--space-2);
  }
  .help {
    flex: 0 0 auto;
    width: 20px;
    height: 20px;
    border-radius: 50%;
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    color: var(--color-on-surface-muted);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    line-height: 1;
    cursor: pointer;
  }
  .help:hover,
  .help[aria-expanded='true'] {
    color: var(--color-primary);
    border-color: var(--color-primary);
  }
  .desc {
    margin: var(--space-1) 0 var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
  }
  .note {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
  }
  .problem {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.74rem;
  }
  .problem.error {
    color: var(--color-error);
  }
  .problem.warn {
    color: var(--color-warning);
  }
  .helpbox {
    margin-top: var(--space-3);
    padding: var(--space-3);
    background: var(--color-surface-variant);
    border-left: 3px solid var(--color-primary);
    border-radius: var(--radius-sm);
  }
  .helpbox p {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.76rem;
    line-height: 1.5;
    color: var(--color-on-surface);
  }
  .example {
    margin-top: var(--space-2) !important;
  }
  .example span {
    color: var(--color-on-surface-muted);
  }
  .example code {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--color-accent);
    word-break: break-all;
  }
</style>
