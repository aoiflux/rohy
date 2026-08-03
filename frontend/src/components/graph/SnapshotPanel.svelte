<script>
  // Snapshots, and the restore preview.
  //
  // 🔒 Nothing restores silently. Choosing a snapshot shows a PLAN — what will be put back, what
  // is offered for re-creation, what is gone — and the restore button only appears alongside it.
  // The one act that adds a claim to the case (re-creating a link) is opt-in per link and is
  // labelled as the analyst's own assertion, because that is what it becomes.
  import { graph } from '../../stores/graph.js';
  import { snackbar } from '../../stores/snackbar.js';
  import * as api from '../../lib/api/index.js';
  import { UI } from '../../lib/consts/index.js';
  import {
    summarise,
    recreatable,
    toggle,
    unresolvedNodes,
    title,
    when,
  } from '../../lib/graph/snapshots.js';
  import Button from '../material/Button.svelte';
  import TextField from '../material/TextField.svelte';

  let list = $state([]);
  let label = $state('');
  let busy = $state(false);
  /** The snapshot being previewed, and its plan. Both null means the list is just a list. */
  let previewing = $state(null);
  let plan = $state(null);
  let chosen = $state(new Set());

  const read = $derived(summarise(plan));
  const offers = $derived(recreatable(plan));
  const gone = $derived(unresolvedNodes(plan));

  async function refresh() {
    try {
      list = (await api.listSnapshots($graph.activeGraphId)) || [];
    } catch (_) {
      // A missing backend leaves an empty list rather than a broken panel.
      list = [];
    }
  }

  function fail(e) {
    snackbar.error(`${UI.SNAP_FAILED} ${String(e && e.message ? e.message : e)}`);
  }

  async function take() {
    busy = true;
    try {
      await api.takeSnapshot({ graph_id: $graph.activeGraphId, label });
      label = '';
      snackbar.success(UI.SNAP_TAKEN);
      await refresh();
    } catch (e) {
      fail(e);
    } finally {
      busy = false;
    }
  }

  async function preview(snap) {
    busy = true;
    try {
      plan = await api.previewRestore($graph.activeGraphId, snap.id);
      previewing = snap;
      // Nothing is pre-selected. Re-creating a link is an act the analyst performs, not a
      // default they have to notice and undo.
      chosen = new Set();
    } catch (e) {
      fail(e);
      closePreview();
    } finally {
      busy = false;
    }
  }

  function closePreview() {
    previewing = null;
    plan = null;
    chosen = new Set();
  }

  async function apply() {
    busy = true;
    try {
      await api.applyRestore({
        graph_id: $graph.activeGraphId,
        id: previewing.id,
        recreate: [...chosen],
      });
      snackbar.success(UI.SNAP_APPLIED);
      closePreview();
      // The canvas still holds the old positions; re-reading the sidecar is what makes the
      // restore visible without a page change.
      graph.applyLayout(await graph.loadLayout());
      await graph.loadRelations();
    } catch (e) {
      fail(e);
    } finally {
      busy = false;
    }
  }

  async function remove(snap) {
    try {
      await api.deleteSnapshot($graph.activeGraphId, snap.id);
      if (previewing && previewing.id === snap.id) closePreview();
      snackbar.info(UI.SNAP_DELETED);
      await refresh();
    } catch (e) {
      fail(e);
    }
  }

  // Refresh whenever the active graph changes. Reading only the id — not the whole store — keeps
  // this from re-firing on every node drag.
  $effect(() => {
    void $graph.activeGraphId;
    refresh();
  });
</script>

<div class="snaps">
  <h3>{UI.SNAP_TITLE}</h3>

  <TextField label={UI.SNAP_LABEL} placeholder={UI.SNAP_LABEL_HINT} bind:value={label} />
  <div class="btns">
    <Button variant="tonal" onclick={take} disabled={busy}>{UI.SNAP_TAKE}</Button>
  </div>

  {#if list.length === 0}
    <p class="hint">{UI.SNAP_NONE}</p>
  {:else}
    <ul class="list">
      {#each list as snap (snap.id)}
        <li class:on={previewing?.id === snap.id}>
          <div class="row">
            <span class="name" title={when(snap.taken_at)}>{title(snap)}</span>
            <span class="counts">{snap.nodes} {UI.SNAP_NODES} · {snap.relations} {UI.SNAP_RELATIONS}</span>
          </div>
          <div class="btns">
            <Button variant="text" onclick={() => preview(snap)} disabled={busy}>{UI.SNAP_PREVIEW}</Button>
            <Button variant="text" onclick={() => remove(snap)}>{UI.SNAP_DELETE}</Button>
          </div>
        </li>
      {/each}
    </ul>
  {/if}

  {#if plan}
    <div class="preview">
      <h4>{UI.SNAP_PREVIEW_TITLE}</h4>

      {#if read.empty}
        <p class="note">{UI.SNAP_NOTHING}</p>
      {:else}
        <ul class="rows">
          {#each read.rows as r (r.kind)}
            <li><b>{r.count}</b> {UI.SNAP_ROW[r.kind]}</li>
          {/each}
        </ul>
      {/if}

      <!-- The two facts that change what the result MEANS, rather than how much of it there is. -->
      {#each read.notes as n (n.kind)}
        <p class="note">{UI.SNAP_NOTE[n.kind]}</p>
      {/each}

      {#if gone.length > 0}
        <div class="gonelist">
          <h5>{UI.SNAP_GONE_TITLE}</h5>
          <ul>
            {#each gone as n (n.snapshot_id)}
              <li>{n.descriptor || n.hash}</li>
            {/each}
          </ul>
        </div>
      {/if}

      {#if offers.length > 0}
        <div class="offers">
          <h5>{UI.SNAP_OFFER_TITLE}</h5>
          <p class="hint">{UI.SNAP_OFFER_HINT}</p>
          {#each offers as r (r.snapshot_id)}
            <label class="offer">
              <input
                type="checkbox"
                checked={chosen.has(r.snapshot_id)}
                onchange={() => (chosen = toggle(chosen, r.snapshot_id))}
              />
              <span>{r.relation_label || r.relation_type}</span>
              <span class="ids">#{r.from_id} → #{r.to_id}</span>
            </label>
          {/each}
        </div>
      {/if}

      <div class="btns">
        <Button variant="tonal" onclick={apply} disabled={busy || read.empty}>{UI.SNAP_APPLY}</Button>
        <Button variant="text" onclick={closePreview}>{UI.SNAP_CANCEL}</Button>
      </div>
    </div>
  {/if}
</div>

<style>
  .snaps {
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
  h4,
  h5 {
    margin: 0;
    font-size: 0.78rem;
    font-weight: 700;
  }
  .btns {
    display: flex;
    flex-wrap: wrap;
    gap: 6px;
  }
  .list {
    display: flex;
    flex-direction: column;
    gap: 4px;
    max-height: 200px;
    margin: 0;
    padding: 0;
    overflow-y: auto;
    list-style: none;
  }
  .list li {
    padding: 6px 8px;
    border-radius: 6px;
    background: var(--md-sys-color-surface-container-high);
  }
  .list li.on {
    outline: 2px solid var(--md-sys-color-primary);
  }
  .row {
    display: flex;
    flex-direction: column;
  }
  .name {
    overflow: hidden;
    font-size: 0.82rem;
    font-weight: 600;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .counts,
  .hint {
    font-size: 0.74rem;
    color: var(--md-sys-color-on-surface-variant);
  }
  .hint {
    margin: 0;
    line-height: 1.4;
  }
  .preview {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px;
    border-radius: 8px;
    background: var(--md-sys-color-surface-container);
  }
  .rows {
    margin: 0;
    padding-left: 16px;
    font-size: 0.78rem;
    line-height: 1.5;
  }
  .note {
    margin: 0;
    padding: 6px 8px;
    font-size: 0.76rem;
    line-height: 1.4;
    border-radius: 6px;
    background: var(--md-sys-color-surface-container-highest);
    border-left: 3px solid var(--md-sys-color-tertiary);
  }
  .gonelist ul {
    margin: 4px 0 0;
    padding-left: 16px;
    font-size: 0.76rem;
    color: var(--md-sys-color-on-surface-variant);
  }
  .offers {
    display: flex;
    flex-direction: column;
    gap: 4px;
  }
  .offer {
    display: flex;
    align-items: center;
    gap: 6px;
    font-size: 0.78rem;
    cursor: pointer;
  }
  .ids {
    margin-left: auto;
    font-size: 0.72rem;
    font-variant-numeric: tabular-nums;
    color: var(--md-sys-color-on-surface-variant);
  }
</style>
