<script>
  // The control for a `string[]` rule field.
  //
  // It has two shapes because the format has two kinds of list, and rendering both the same way
  // would be wrong in opposite directions:
  //
  //   - A CLOSED list (match_fields) draws from a fixed vocabulary the backend serves, so it is
  //     a set of toggles. Typing a correlation field name by hand is a spelling test with a
  //     validation error as the prize, and the vocabulary is short enough to show in full.
  //   - An OPEN list (channels, lineage_create_ids) takes values only the author knows, so it
  //     is editable rows.
  //
  // Both write back the whole array in one change, so a list edit is one undo step rather than
  // one per row.
  import { UI } from '../../lib/consts/index.js';

  let {
    field,
    /** @type {string[]} */
    items = [],
    /** (next: string[]) => void */
    onchange = undefined,
  } = $props();

  const options = $derived(field?.enum ?? []);
  const closed = $derived(options.length > 0);
  const selected = $derived(new Set(items));

  function toggle(option) {
    // Order follows the vocabulary rather than click order, so two rules selecting the same
    // fields are byte-identical regardless of how they were assembled.
    const next = options.filter((o) => (o === option ? !selected.has(o) : selected.has(o)));
    onchange?.(next);
  }

  function setAt(index, text) {
    const next = [...items];
    next[index] = text;
    onchange?.(next);
  }
  const removeAt = (index) => onchange?.(items.filter((_, i) => i !== index));
  const add = () => onchange?.([...items, '']);
</script>

{#if closed}
  <div class="chips" role="group" aria-labelledby={`field-${field.name}`}>
    {#each options as option (option)}
      <button
        type="button"
        class="chip"
        class:on={selected.has(option)}
        aria-pressed={selected.has(option)}
        onclick={() => toggle(option)}
      >
        {option}
      </button>
    {/each}
  </div>
{:else}
  <div class="rows">
    {#each items as item, i (i)}
      <div class="row">
        <input
          value={item}
          aria-label={`${field.name} ${i + 1}`}
          oninput={(e) => setAt(i, e.currentTarget.value)}
        />
        <button type="button" class="remove" onclick={() => removeAt(i)} aria-label={UI.RULE_EDITOR_LIST_REMOVE}>×</button>
      </div>
    {/each}
    <button type="button" class="add" onclick={add}>{UI.RULE_EDITOR_LIST_ADD}</button>
  </div>
{/if}

<style>
  .chips {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
  }
  .chip {
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    color: var(--color-on-surface-muted);
    border-radius: var(--radius-md);
    padding: var(--space-1) var(--space-3);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    cursor: pointer;
    transition:
      background var(--motion-fast) var(--motion-ease),
      border-color var(--motion-fast) var(--motion-ease),
      color var(--motion-fast) var(--motion-ease);
  }
  .chip:hover {
    background: var(--color-surface-variant);
  }
  .chip.on {
    background: var(--color-primary);
    border-color: var(--color-primary);
    color: var(--color-on-primary);
    font-weight: 600;
  }

  .rows {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .row {
    display: flex;
    gap: var(--space-2);
  }
  input {
    flex: 1;
    min-width: 0;
    background: var(--color-surface);
    color: var(--color-on-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    padding: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.85rem;
  }
  .remove,
  .add {
    border: 1px solid var(--color-outline);
    background: var(--color-surface);
    color: var(--color-on-surface-muted);
    border-radius: var(--radius-sm);
    cursor: pointer;
    font-family: var(--font-sans);
  }
  .remove {
    width: 32px;
    font-size: 1rem;
    line-height: 1;
  }
  .add {
    align-self: flex-start;
    padding: var(--space-1) var(--space-3);
    font-size: 0.8rem;
  }
  .remove:hover,
  .add:hover {
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
  }
</style>
