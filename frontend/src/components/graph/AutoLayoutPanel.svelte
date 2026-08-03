<script>
  // Arranging the canvas by a profile computed in Go.
  //
  // The one thing this panel is careful about: node positions are the only part of a graph an
  // analyst places by hand, and there is no undo for them. So a profile is PREVIEWED — the nodes
  // move, nothing is written — and the positions they moved from are held until the arrangement
  // is either kept or put back.
  //
  // The snapshot is taken once, at the first preview, and deliberately NOT refreshed when the
  // profile changes. Otherwise trying two profiles in a row would make "put back" restore the
  // first preview rather than the hand placement, which is the thing worth being able to return
  // to.
  import { onMount } from 'svelte';
  import { graph } from '../../stores/graph.js';
  import { snackbar } from '../../stores/snackbar.js';
  import * as api from '../../lib/api/index.js';
  import { UI } from '../../lib/consts/index.js';
  import { snapshot, toSaved, placed, describe, needsField } from '../../lib/graph/autolayout.js';
  import Button from '../material/Button.svelte';
  import Select from '../material/Select.svelte';

  let profiles = $state([]);
  let fields = $state([]);
  let profile = $state('');
  let field = $state('');
  let result = $state(null);
  let busy = $state(false);
  /** Where the nodes were before the first preview; null means nothing is being previewed. */
  let original = $state(null);

  const nodeCount = $derived(Object.keys($graph.nodes || {}).length);
  const showsField = $derived(needsField(profiles, profile));
  const summary = $derived(describe(result));
  const landed = $derived(placed(result, $graph.nodes));
  const shortfall = $derived(landed.total - landed.applied);

  const profileOptions = $derived(profiles.map((p) => ({ value: p.name, label: p.label })));
  const fieldOptions = $derived(fields.map((f) => ({ value: f, label: f })));
  const activeSummary = $derived(profiles.find((p) => p.name === profile)?.summary ?? '');

  // onMount rather than $effect: this loads once and it ASSIGNS the very state it reads to pick
  // a default. In an effect that pairing is an update loop waiting for someone to move the await
  // — a bug this codebase has already shipped once (LLD.md §23).
  onMount(async () => {
    try {
      const [ps, fs] = await Promise.all([api.layoutProfiles(), api.layoutFields()]);
      profiles = ps || [];
      fields = fs || [];
      if (profiles.length) profile = profiles[0].name;
      if (fields.length) field = fields[0];
    } catch (_) {
      // A failure here leaves the panel with no options rather than a broken control; the canvas
      // is unaffected, which is the part that matters.
    }
  });

  async function preview() {
    if (nodeCount === 0) {
      snackbar.info(UI.ARRANGE_EMPTY);
      return;
    }
    busy = true;
    try {
      const res = await api.computeLayout({
        graph_id: $graph.activeGraphId,
        profile,
        slot: showsField ? field : '',
      });
      if (original === null) original = snapshot($graph.nodes);
      result = res;
      graph.applyLayout(toSaved(res, $graph.nodes));
    } catch (e) {
      snackbar.error(`${UI.ARRANGE_FAILED} ${String(e && e.message ? e.message : e)}`);
    } finally {
      busy = false;
    }
  }

  async function keep() {
    try {
      await graph.saveLayout();
      original = null;
      result = null;
      snackbar.info(UI.ARRANGE_KEPT);
    } catch (e) {
      snackbar.error(`${UI.ARRANGE_FAILED} ${String(e && e.message ? e.message : e)}`);
    }
  }

  function revert() {
    if (original === null) return;
    graph.applyLayout({ nodes: original });
    original = null;
    result = null;
    snackbar.info(UI.ARRANGE_REVERTED);
  }
</script>

<div class="arrange">
  <h3>{UI.ARRANGE_TITLE}</h3>

  <Select label={UI.ARRANGE_PROFILE} options={profileOptions} bind:value={profile} />
  {#if activeSummary}
    <p class="hint">{activeSummary}</p>
  {/if}

  <!-- The field picker appears only for the profile that groups by one. A dead control on the
       other three would read as "this does nothing here", which is a question rather than an
       answer. -->
  {#if showsField}
    <Select label={UI.ARRANGE_FIELD} options={fieldOptions} bind:value={field} />
  {/if}

  <div class="btns">
    <Button variant="tonal" onclick={preview} disabled={busy || !profile}>{UI.ARRANGE_PREVIEW}</Button>
    {#if original !== null}
      <Button variant="text" onclick={keep}>{UI.ARRANGE_KEEP}</Button>
      <Button variant="text" onclick={revert}>{UI.ARRANGE_REVERT}</Button>
    {/if}
  </div>

  {#if original !== null}
    <p class="previewing">{UI.ARRANGE_PREVIEWING}</p>
  {/if}

  {#if summary.summary}
    <p class="hint">{summary.summary}</p>
  {/if}

  <!-- The backend's caveat, verbatim. A broken cycle, an empty correlation projection, or events
       with no timestamp all change what the picture can be read to mean, so this is shown as a
       warning rather than folded into the summary. -->
  {#if summary.note}
    <p class="note">{summary.note}</p>
  {/if}

  {#if shortfall > 0}
    <p class="note">{shortfall} {UI.ARRANGE_PARTIAL}</p>
  {/if}
</div>

<style>
  .arrange {
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
  .previewing {
    margin: 0;
    font-size: 0.78rem;
    font-weight: 600;
    color: var(--md-sys-color-primary);
  }
  .note {
    margin: 0;
    padding: 6px 8px;
    font-size: 0.78rem;
    line-height: 1.4;
    border-radius: 6px;
    background: var(--md-sys-color-surface-container-high);
    color: var(--md-sys-color-on-surface);
    border-left: 3px solid var(--md-sys-color-tertiary);
  }
</style>
