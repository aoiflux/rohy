<script>
  // Forensic filter bar (P6.3 + P9). Collapsed by default: the header alone tells the user
  // whether the view is filtered, and the full form expands on demand.
  //
  // Local field state is pushed to the events store on Apply (so typing doesn't spam
  // queries) and persisted, so both the panel state and the query survive navigation and a
  // restart. Time bounds use datetime-local inputs and are converted to the RFC3339 the
  // backend expects by lib/filter.js, which the events store shares.
  import { slide } from 'svelte/transition';
  import { events } from '../../stores/events.js';
  import { prefs } from '../../stores/prefs.js';
  import { emptyForm, formToFilter, activeFilterCount, filterSummary } from '../../lib/filter.js';
  import { motion } from '../../lib/motion.js';
  import { findings } from '../../stores/findings.js';
  import {
    UI,
    MOTION,
    SOURCE_TYPES,
    SOURCE_TYPE_LABEL,
    RELATION_FILTERS,
    FINDING_FILTERS,
    UNDATED,
  } from '../../lib/consts/index.js';
  import TextField from '../material/TextField.svelte';
  import Select from '../material/Select.svelte';
  import Checkbox from '../material/Checkbox.svelte';
  import Button from '../material/Button.svelte';

  // Source-type options: an "Any" sentinel (empty = unfiltered) plus each known type.
  const sourceTypeOptions = [
    { value: '', label: UI.SOURCE_TYPE_ANY },
    ...Object.values(SOURCE_TYPES).map((v) => ({ value: v, label: SOURCE_TYPE_LABEL[v] })),
  ];

  // Field labels for the collapsed summary, so it reads like the form rather than like keys.
  const FIELD_LABELS = {
    search: UI.LABEL_SEARCH,
    provider: UI.LABEL_PROVIDER,
    channel: UI.LABEL_CHANNEL,
    event_id: UI.LABEL_EVENT_ID,
    user: UI.LABEL_USER,
    time_from: UI.LABEL_TIME_FROM,
    time_to: UI.LABEL_TIME_TO,
    source_type: UI.LABEL_SOURCE_TYPE,
    source_identifier: UI.LABEL_SOURCE_IDENTIFIER,
    min_occurrences: UI.LABEL_MIN_OCCURRENCES,
    finding_state: UI.FILTER_FINDINGS,
    tag: UI.FILTER_TAG,
  };

  // Seeded from the persisted form: reopening the view shows the query that is actually
  // applied, instead of blank inputs over filtered results.
  let f = $state({ ...emptyForm(), ...prefs.current().filterForm });
  let open = $state(prefs.current().searchOpen);

  const activeCount = $derived(activeFilterCount(f));
  const summary = $derived(filterSummary(f, FIELD_LABELS));

  function toggle() {
    open = !open;
    prefs.setSearchOpen(open);
  }

  // Relation quick filters (P11). They live in the header rather than inside the collapsed
  // body, because their whole value is being one click away — burying them behind the
  // expand toggle would defeat the point.
  const QUICK = [
    { value: RELATION_FILTERS.ANY, label: UI.FILTER_HAS_RELATIONS },
    { value: RELATION_FILTERS.SYSTEM, label: UI.FILTER_RULE_CORRELATED },
    { value: RELATION_FILTERS.USER, label: UI.FILTER_MANUALLY_MAPPED },
  ];

  // Timeline-participation chips (P23). The list shows everything by default; these isolate
  // one group or the other. UNDATED.INCLUDE is "no filter" here, not a third state.
  const TIMELINE_QUICK = [
    { value: UNDATED.EXCLUDE, label: UI.FILTER_ON_TIMELINE },
    { value: UNDATED.ONLY, label: UI.FILTER_NO_TIMELINE },
  ];

  // Analyst-findings chips (P25). Flagged is the one an analyst reaches for constantly
  // mid-investigation, so it earns a place in the header; the rest of the finding states live
  // in the expanded form.
  const FINDING_QUICK = [{ value: FINDING_FILTERS.FLAGGED, label: UI.FILTER_FINDING_FLAGGED }];

  const findingOptions = [
    { value: FINDING_FILTERS.ANY, label: UI.FILTER_FINDING_ANY },
    { value: FINDING_FILTERS.FLAGGED, label: UI.FILTER_FINDING_FLAGGED },
    { value: FINDING_FILTERS.ANNOTATED, label: UI.FILTER_FINDING_ANNOTATED },
    { value: FINDING_FILTERS.NOTED, label: UI.FILTER_FINDING_NOTED },
    { value: FINDING_FILTERS.NONE, label: UI.FILTER_FINDING_NONE },
  ];

  // Tag options come from the vocabulary actually in use, so the filter can only ask for
  // tags that exist — a free-text tag filter would mostly produce empty results from typos.
  const tagOptions = $derived([
    { value: '', label: UI.FILTER_TAG_ANY },
    ...$findings.tags.map((t) => ({ value: t.tag, label: `${t.tag} (${t.count})` })),
  ]);

  // Clicking the active chip clears it, so a quick filter is as quick to undo as to apply.
  function quickFilter(value) {
    f.relation_state = f.relation_state === value ? RELATION_FILTERS.NONE : value;
    apply();
  }

  function findingFilter(value) {
    f.finding_state = f.finding_state === value ? FINDING_FILTERS.ANY : value;
    apply();
  }

  function timelineFilter(value) {
    f.undated = f.undated === value ? UNDATED.INCLUDE : value;
    apply();
  }

  // Submitting the form applies: Enter anywhere in the panel works without a keydown
  // handler, and the Apply button is the form's submit button.
  function apply(e) {
    e?.preventDefault?.();
    prefs.setFilterForm(f);
    events.setFilter(formToFilter(f));
    events.load();
  }

  function clear() {
    f = emptyForm();
    prefs.setFilterForm(f);
    events.setFilter(formToFilter(f));
    events.load();
  }

  // Ctrl+F opens the panel and focuses the search box — the shortcut is what keeps a
  // collapsed-by-default panel fast to reach (R-SC1).
  let searchEl = $state(/** @type {any} */ (null));
  let panelEl = $state(/** @type {any} */ (null));

  function onwindowkeydown(e) {
    if ((e.ctrlKey || e.metaKey) && e.key.toLowerCase() === 'f') {
      e.preventDefault();
      if (!open) toggle();
      queueMicrotask(() => searchEl?.focus?.());
      return;
    }
    // Escape collapses the panel only while the user is actually in it; otherwise it would
    // steal the key from whatever else is open (a dialog, the detail drawer).
    if (e.key === 'Escape' && open && panelEl?.contains?.(e.target)) {
      toggle();
    }
  }
</script>

<svelte:window onkeydown={onwindowkeydown} />

<section class="panel" bind:this={panelEl}>
  <div class="head">
    <button
      class="toggle"
      type="button"
      onclick={toggle}
      aria-expanded={open}
      aria-controls="filter-body"
      title={open ? UI.SEARCH_COLLAPSE : UI.SEARCH_EXPAND}
    >
      <span class="chev" class:open aria-hidden="true">▸</span>
      <span class="ptitle">{UI.SEARCH_PANEL_TITLE}</span>
      <span class="kbd">{UI.SEARCH_SHORTCUT_HINT}</span>
    </button>

    <div class="chips" role="group" aria-label={UI.QUICK_FILTERS}>
      {#each QUICK as q (q.value)}
        <button
          class="chip"
          class:on={f.relation_state === q.value}
          type="button"
          aria-pressed={f.relation_state === q.value}
          onclick={() => quickFilter(q.value)}
        >
          {q.label}
        </button>
      {/each}
      <span class="sep" aria-hidden="true"></span>
      {#each TIMELINE_QUICK as q (q.value)}
        <button
          class="chip"
          class:on={f.undated === q.value}
          type="button"
          aria-pressed={f.undated === q.value}
          onclick={() => timelineFilter(q.value)}
        >
          {q.label}
        </button>
      {/each}
      <span class="sep" aria-hidden="true"></span>
      {#each FINDING_QUICK as q (q.value)}
        <button
          class="chip mine"
          class:on={f.finding_state === q.value}
          type="button"
          aria-pressed={f.finding_state === q.value}
          onclick={() => findingFilter(q.value)}
        >
          ★ {q.label}
        </button>
      {/each}
    </div>

    <!-- Collapsing must never hide the fact that the view is filtered. -->
    {#if activeCount > 0}
      <span class="active" title={summary}>
        <b>{activeCount}</b>
        {UI.FILTERS_ACTIVE_SUFFIX}
        <span class="sum">{summary}</span>
      </span>
      <Button variant="text" onclick={clear}>{UI.ACTION_CLEAR_FILTERS}</Button>
    {:else}
      <span class="active muted">{UI.FILTERS_NONE}</span>
    {/if}
  </div>

  {#if open}
    <form id="filter-body" class="bar" transition:slide={motion(MOTION.MEDIUM)} onsubmit={apply}>
      <div class="wide">
        <TextField label={UI.LABEL_SEARCH} bind:value={f.search} bind:element={searchEl} />
      </div>
      <TextField label={UI.LABEL_PROVIDER} bind:value={f.provider} />
      <TextField label={UI.LABEL_CHANNEL} bind:value={f.channel} />
      <TextField label={UI.LABEL_EVENT_ID} bind:value={f.event_id} />
      <TextField label={UI.LABEL_USER} bind:value={f.user} />
      <TextField label={UI.LABEL_TIME_FROM} type="datetime-local" bind:value={f.time_from} />
      <TextField label={UI.LABEL_TIME_TO} type="datetime-local" bind:value={f.time_to} />
      <TextField label={UI.LABEL_MIN_OCCURRENCES} type="number" bind:value={f.min_occurrences} />
      <Select label={UI.LABEL_SOURCE_TYPE} options={sourceTypeOptions} bind:value={f.source_type} />
      <TextField label={UI.LABEL_SOURCE_IDENTIFIER} bind:value={f.source_identifier} />
      <Select label={UI.FILTER_FINDINGS} options={findingOptions} bind:value={f.finding_state} />
      <Select label={UI.FILTER_TAG} options={tagOptions} bind:value={f.tag} />
      <div class="controls">
        <Checkbox label={UI.LABEL_NEWEST_FIRST} bind:checked={f.descending} />
        <div class="btns">
          <Button variant="text" onclick={clear}>{UI.ACTION_CLEAR_FILTERS}</Button>
          <Button type="submit">{UI.ACTION_APPLY_FILTERS}</Button>
        </div>
      </div>
    </form>
  {/if}
</section>

<style>
  .panel {
    background: var(--color-surface);
    border-bottom: 1px solid var(--color-outline);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    padding: var(--space-2) var(--space-4);
    min-height: 40px;
  }
  .toggle {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    background: none;
    border: none;
    padding: var(--space-2);
    margin-left: calc(-1 * var(--space-2));
    border-radius: var(--radius-sm);
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.85rem;
    font-weight: 700;
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--motion-fast) var(--motion-ease);
  }
  .toggle:hover {
    background: var(--color-surface-variant);
  }
  .toggle:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .chev {
    display: inline-block;
    transition: transform var(--motion-medium) var(--motion-ease);
    color: var(--color-on-surface-variant);
  }
  .chev.open {
    transform: rotate(90deg);
  }
  @media (prefers-reduced-motion: reduce) {
    .chev {
      transition: none;
    }
  }
  .kbd {
    font-family: var(--font-mono);
    font-size: 0.68rem;
    font-weight: 400;
    color: var(--color-on-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    padding: 0 4px;
  }
  .chips {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .chip {
    font-family: var(--font-sans);
    font-size: 0.75rem;
    padding: 2px var(--space-3);
    border-radius: 999px;
    border: 1px solid var(--color-outline);
    background: transparent;
    color: var(--color-on-surface-variant);
    cursor: pointer;
    white-space: nowrap;
    transition: background var(--motion-fast) var(--motion-ease),
      color var(--motion-fast) var(--motion-ease), border-color var(--motion-fast) var(--motion-ease);
  }
  .chip:hover {
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
  }
  .chip.on {
    background: var(--color-primary);
    border-color: var(--color-primary);
    color: var(--color-on-primary);
  }
  /* Filters over the analyst's OWN marks carry the accent used for authored data
     everywhere else, so they never read as another machine-derived facet. */
  .chip.mine.on {
    background: var(--color-accent);
    border-color: var(--color-accent);
    color: var(--color-on-accent, var(--color-on-primary));
  }
  /* Separates the two independent chip groups so they do not read as one exclusive set. */
  .sep {
    width: 1px;
    align-self: stretch;
    background: var(--color-outline);
    margin: 0 var(--space-1);
  }
  .chip:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  @media (prefers-reduced-motion: reduce) {
    .chip {
      transition: none;
    }
  }
  .active {
    flex: 1;
    min-width: 0;
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    color: var(--color-primary);
  }
  .active.muted {
    color: var(--color-on-surface-muted);
  }
  .sum {
    color: var(--color-on-surface-variant);
    overflow: hidden;
    text-overflow: ellipsis;
    white-space: nowrap;
  }

  .bar {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(160px, 1fr));
    gap: var(--space-3);
    padding: 0 var(--space-4) var(--space-4);
    align-items: end;
  }
  .wide {
    grid-column: 1 / -1;
  }
  .controls {
    grid-column: 1 / -1;
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .btns {
    display: flex;
    gap: var(--space-3);
  }
</style>
