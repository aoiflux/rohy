<script>
  // Grouping the canvas: pick a mode, see the groups, fold the ones you are not reading.
  //
  // Two things this panel is deliberate about:
  //
  //  - Outlines are OFF until asked for. A permanent set of hulls is visual noise, and noise
  //    that is always present is noise nobody reads.
  //  - Every group in the list shows its size, and folding never removes that number from the
  //    screen. A folded group must never be able to hide how much it contains.
  import { onMount } from 'svelte';
  import { graph } from '../../stores/graph.js';
  import { clusters } from '../../stores/clusters.js';
  import * as api from '../../lib/api/index.js';
  import { UI } from '../../lib/consts/index.js';
  import Button from '../material/Button.svelte';
  import Select from '../material/Select.svelte';

  let modes = $state([]);
  let fields = $state([]);
  let mode = $state('');
  let field = $state('');

  const nodeCount = $derived(Object.keys($graph.nodes || {}).length);
  const showsField = $derived(mode === 'slot');
  const list = $derived($clusters.list || []);
  const overlapping = $derived(list.some((c) => c.overlapping));
  const foldedCount = $derived($clusters.collapsed.size);

  const modeOptions = $derived(
    modes.map((m) => ({ value: m, label: UI.CLUSTER_MODE_LABEL[m] ?? m })),
  );
  const fieldOptions = $derived(fields.map((f) => ({ value: f, label: f })));

  // onMount, not $effect: this loads once and assigns the state it reads to pick a default.
  onMount(async () => {
    try {
      const [ms, fs] = await Promise.all([api.clusterModes(), api.layoutFields()]);
      modes = ms || [];
      fields = fs || [];
      if (modes.length) mode = modes[0];
      if (fields.length) field = fields[0];
    } catch (_) {
      // No options rather than a broken control. The canvas is unaffected.
    }
  });

  async function group() {
    await clusters.load($graph.activeGraphId, mode, showsField ? field : '');
  }
</script>

<div class="clusters">
  <h3>{UI.CLUSTER_TITLE}</h3>

  <Select label={UI.CLUSTER_MODE} options={modeOptions} bind:value={mode} />
  {#if showsField}
    <Select label={UI.CLUSTER_FIELD} options={fieldOptions} bind:value={field} />
  {/if}

  <div class="btns">
    <Button variant="tonal" onclick={group} disabled={$clusters.loading || !mode || nodeCount === 0}>
      {UI.CLUSTER_SHOW}
    </Button>
    {#if $clusters.visible}
      <Button variant="text" onclick={() => clusters.hide()}>{UI.CLUSTER_HIDE}</Button>
    {/if}
  </div>

  {#if nodeCount === 0}
    <p class="hint">{UI.CLUSTER_NONE}</p>
  {/if}

  {#if $clusters.error}
    <p class="note">{UI.CLUSTER_FAILED} {$clusters.error}</p>
  {/if}

  {#if $clusters.visible && list.length > 0}
    <div class="btns">
      <Button variant="text" onclick={() => clusters.collapseAll()}>{UI.CLUSTER_COLLAPSE_ALL}</Button>
      <Button variant="text" onclick={() => clusters.expandAll()} disabled={foldedCount === 0}>
        {UI.CLUSTER_EXPAND_ALL}
      </Button>
    </div>

    <!-- Rule grouping genuinely overlaps: an event can be a step in two chains. Said once, here,
         rather than left for the analyst to infer from a fold that took more than it looked
         like it would. -->
    {#if overlapping}
      <p class="note">{UI.CLUSTER_OVERLAPS}</p>
    {/if}
    {#if foldedCount > 0}
      <p class="hint">{UI.CLUSTER_INTERNAL_HIDDEN}</p>
    {/if}

    <ul class="list">
      {#each list as c (c.id)}
        <li>
          <button class="row" class:folded={$clusters.collapsed.has(c.id)} onclick={() => clusters.toggle(c.id)}>
            <span class="chev">{$clusters.collapsed.has(c.id) ? '▸' : '▾'}</span>
            <span class="name">{c.label}</span>
            <span class="size">{c.size}</span>
          </button>
        </li>
      {/each}
    </ul>
  {/if}
</div>

<style>
  .clusters {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 12px;
    border-bottom: 1px solid var(--md-sys-color-outline-variant);
  }
  h3 {
    margin: 0;
    font-size: 0.78rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--md-sys-color-on-surface-variant);
  }
  .btns {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .hint {
    margin: 0;
    font-size: 0.78rem;
    line-height: 1.4;
    color: var(--md-sys-color-on-surface-variant);
  }
  .note {
    margin: 0;
    padding: 6px 8px;
    font-size: 0.78rem;
    line-height: 1.4;
    border-radius: 6px;
    background: var(--md-sys-color-surface-container-high);
    border-left: 3px solid var(--md-sys-color-tertiary);
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    max-height: 220px;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }
  .row {
    display: flex;
    align-items: center;
    gap: 8px;
    width: 100%;
    padding: 5px 8px;
    font: inherit;
    font-size: 0.8rem;
    color: var(--md-sys-color-on-surface);
    text-align: left;
    background: none;
    border: none;
    border-radius: 6px;
    cursor: pointer;
  }
  .row:hover {
    background: var(--md-sys-color-surface-container-high);
  }
  .row.folded .name {
    color: var(--md-sys-color-on-surface-variant);
  }
  .chev {
    width: 10px;
    color: var(--md-sys-color-on-surface-variant);
  }
  .name {
    flex: 1;
    overflow: hidden;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  /* The count is never truncated and never hidden — it is the disclosure a fold depends on. */
  .size {
    flex: none;
    padding: 1px 7px;
    font-size: 0.72rem;
    font-weight: 700;
    font-variant-numeric: tabular-nums;
    border-radius: 999px;
    background: var(--md-sys-color-surface-container-highest);
  }
</style>
