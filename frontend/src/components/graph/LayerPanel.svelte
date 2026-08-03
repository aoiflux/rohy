<script>
  // Annotation layers: add, rename, colour, show/hide, reorder, delete.
  //
  // The panel is also where the distinction between an annotation and a finding is stated, once,
  // in words. The two look alike from the outside and are easy to reach for interchangeably —
  // but a layer marks up THIS graph, while a finding is about the event itself and follows it
  // everywhere. Leaving that to be discovered means notes ending up in the wrong place.
  import { graph } from '../../stores/graph.js';
  import { annotations } from '../../stores/annotations.js';
  import { snackbar } from '../../stores/snackbar.js';
  import { UI } from '../../lib/consts/index.js';
  import { describe, nextLayerColour, placed, KIND, ANCHOR } from '../../lib/graph/annotations.js';
  import { GRAPH } from '../../lib/consts/index.js';
  import Button from '../material/Button.svelte';
  import TextField from '../material/TextField.svelte';
  import Dialog from '../material/Dialog.svelte';

  let newLayer = $state('');
  let noteText = $state('');
  let confirmDelete = $state(/** @type {{open:boolean, layer:any, count:number}} */ ({ open: false, layer: null, count: 0 }));

  const geom = { width: GRAPH.NODE_WIDTH, height: GRAPH.NODE_HEIGHT };
  const drawn = $derived(
    placed({ layers: $annotations.layers, items: $annotations.items }, $annotations.nodeOf, $graph.nodes, geom),
  );
  const notes = $derived(describe(drawn.unplaceable, $annotations.orphaned));
  // The selected canvas node, which a note attaches to. Its hash is the anchor — never its id.
  const selected = $derived(
    $graph.selection.length === 1 ? $graph.nodes[String($graph.selection[0])] : null,
  );

  function fail(e) {
    snackbar.error(`${UI.ANN_FAILED} ${String(e && e.message ? e.message : e)}`);
  }

  async function addLayer() {
    const name = newLayer.trim();
    if (!name) return;
    try {
      await annotations.saveLayer($graph.activeGraphId, {
        name,
        colour: nextLayerColour($annotations.layers),
        visible: true,
      });
      newLayer = '';
    } catch (e) {
      fail(e);
    }
  }

  async function setVisible(layer, visible) {
    try {
      await annotations.saveLayer($graph.activeGraphId, { ...layer, visible });
    } catch (e) {
      fail(e);
    }
  }

  async function move(layer, delta) {
    try {
      await annotations.saveLayer($graph.activeGraphId, { ...layer, z: layer.z + delta });
    } catch (e) {
      fail(e);
    }
  }

  function askDelete(layer) {
    // Counted from what is loaded rather than by a round trip: the store already holds every
    // item, so a second call would only add latency to a confirmation.
    const count = $annotations.items.filter((i) => i.layer === layer.id).length;
    confirmDelete = { open: true, layer, count };
  }

  async function doDelete() {
    try {
      await annotations.deleteLayer($graph.activeGraphId, confirmDelete.layer.id);
    } catch (e) {
      fail(e);
    } finally {
      confirmDelete = { open: false, layer: null, count: 0 };
    }
  }

  async function addNote() {
    if (!selected) {
      snackbar.info(UI.ANN_SELECT_FIRST);
      return;
    }
    const text = noteText.trim();
    if (!text) return;
    try {
      // 🔒 Anchored by the event's content hash, never by its node id: ids are assignment-order
      // and a re-ingest hands the same one to a different event.
      await annotations.save($graph.activeGraphId, {
        kind: KIND.NOTE,
        anchor: { kind: ANCHOR.EVENT, hash: selected.event.hash_normalized },
        text,
      });
      noteText = '';
    } catch (e) {
      fail(e);
    }
  }

  // Reload whenever the active graph changes. Reading only the id keeps this off every drag.
  $effect(() => {
    void $graph.activeGraphId;
    annotations.load($graph.activeGraphId);
  });
</script>

<div class="layers">
  <h3>{UI.ANN_TITLE}</h3>

  <div class="btns">
    <Button variant="text" onclick={() => annotations.toggleVisible()}>
      {$annotations.visible ? UI.ANN_HIDE : UI.ANN_SHOW}
    </Button>
  </div>

  <div class="add">
    <TextField label={UI.ANN_LAYER_NAME} bind:value={newLayer} />
    <Button variant="tonal" onclick={addLayer} disabled={!newLayer.trim()}>{UI.ANN_ADD_LAYER}</Button>
  </div>

  {#if $annotations.layers.length === 0}
    <p class="hint">{UI.ANN_NONE}</p>
  {:else}
    <ul class="list">
      {#each $annotations.layers as l (l.id)}
        <li>
          <span class="swatch" style="background: {l.colour || 'var(--color-accent)'}"></span>
          <span class="name" title={l.name}>{l.name}</span>
          <span class="count">{$annotations.items.filter((i) => i.layer === l.id).length}</span>
          <button class="mini" type="button" onclick={() => setVisible(l, !l.visible)} aria-pressed={l.visible}>
            {l.visible ? '👁' : '⃠'}
          </button>
          <button class="mini" type="button" onclick={() => move(l, -1)} aria-label="down">↓</button>
          <button class="mini" type="button" onclick={() => move(l, 1)} aria-label="up">↑</button>
          <button class="mini" type="button" onclick={() => askDelete(l)} aria-label={UI.ANN_DELETE_LAYER}>×</button>
        </li>
      {/each}
    </ul>
  {/if}

  <div class="note">
    <TextField label={UI.ANN_NOTE_TEXT} placeholder={UI.ANN_NOTE_PLACEHOLDER} bind:value={noteText} />
    <Button variant="text" onclick={addNote} disabled={!noteText.trim()}>{UI.ANN_ADD_NOTE}</Button>
    {#if !selected}
      <p class="hint">{UI.ANN_SELECT_FIRST}</p>
    {/if}
  </div>

  <!-- Stated rather than counted: a mark that is not drawn looks like a mark that was never made,
       and the two reasons have different fixes. -->
  {#each notes as n (n.kind)}
    <p class="caveat">
      <b>{n.count}</b>
      {n.kind === 'orphaned' ? UI.ANN_ORPHANED : UI.ANN_OFFCANVAS}
    </p>
  {/each}

  <p class="hint quiet">{UI.ANN_VS_FINDING}</p>
</div>

<Dialog bind:open={confirmDelete.open} title={UI.ANN_DELETE_LAYER}>
  <p class="confirm">{UI.ANN_DELETE_LAYER_BODY}</p>
  {#if confirmDelete.count > 0}
    <p class="confirm"><b>{confirmDelete.count}</b> {UI.ANN_DELETE_LAYER_COUNT}</p>
  {/if}
  {#snippet actions()}
    <Button variant="text" onclick={() => (confirmDelete = { open: false, layer: null, count: 0 })}>
      {UI.ANN_CANCEL}
    </Button>
    <Button onclick={doDelete}>{UI.ANN_DELETE}</Button>
  {/snippet}
</Dialog>

<style>
  .layers {
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
  .btns,
  .add,
  .note {
    display: flex;
    flex-direction: column;
    gap: 6px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 0;
    list-style: none;
  }
  .list li {
    display: flex;
    align-items: center;
    gap: 6px;
    padding: 3px 4px;
    border-radius: 6px;
  }
  .list li:hover {
    background: var(--md-sys-color-surface-container-high);
  }
  .swatch {
    flex: none;
    width: 10px;
    height: 10px;
    border-radius: 3px;
  }
  .name {
    flex: 1;
    overflow: hidden;
    font-size: 0.8rem;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .count {
    flex: none;
    font-size: 0.72rem;
    font-variant-numeric: tabular-nums;
    color: var(--md-sys-color-on-surface-variant);
  }
  .mini {
    flex: none;
    padding: 1px 5px;
    font: inherit;
    font-size: 0.72rem;
    color: var(--md-sys-color-on-surface-variant);
    background: none;
    border: none;
    border-radius: 4px;
    cursor: pointer;
  }
  .mini:hover {
    background: var(--md-sys-color-surface-container-highest);
  }
  .hint,
  .caveat,
  .confirm {
    margin: 0;
    font-size: 0.78rem;
    line-height: 1.4;
    color: var(--md-sys-color-on-surface-variant);
  }
  .quiet {
    padding-top: 4px;
    border-top: 1px solid var(--md-sys-color-outline-variant);
  }
  .caveat {
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--md-sys-color-surface-container-high);
    border-left: 3px solid var(--md-sys-color-tertiary);
  }
</style>
