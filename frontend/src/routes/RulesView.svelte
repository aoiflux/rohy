<script>
  // Rules management view (P5). Lists the merged rule library — embedded defaults plus
  // imported user rules — with per-rule enable/disable, a source badge, the event-ID
  // sequence rendered with its optional connection labels, delete for imported rules
  // (built-ins are protected), import controls, and the per-file load errors the backend
  // reports so a broken file is visible rather than silently missing.
  import { onMount } from 'svelte';
  import { theme } from '../stores/theme.js';
  import { route } from '../stores/router.js';
  import { rules } from '../stores/rules.js';
  import { ruleEditor } from '../stores/ruleEditor.js';
  import { events, emptyFilter } from '../stores/events.js';
  import { graph } from '../stores/graph.js';
  import { snackbar } from '../stores/snackbar.js';
  import { UI, ROUTES, THEMES, RULE_SOURCES } from '../lib/consts/index.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import Dialog from '../components/material/Dialog.svelte';
  import ProgressBar from '../components/material/ProgressBar.svelte';
  import Menu from '../components/material/Menu.svelte';
  import RuleEditorDialog from '../components/rules/RuleEditorDialog.svelte';

  onMount(() => rules.load());

  // Renders a rule as its chain: 4625 → 4625 —then succeeds→ 4624. A connection without a
  // custom label shows the bare arrow; a labeled one shows the user's own text.
  function chain(rule) {
    const seq = rule.sequence || [];
    const labels = rule.labels || [];
    const out = [];
    seq.forEach((id, i) => {
      out.push({ kind: 'step', text: id });
      if (i < seq.length - 1) {
        out.push({ kind: 'link', text: (labels[i] || '').trim() });
      }
    });
    return out;
  }

  function sourceLabel(rule) {
    return rule.source === RULE_SOURCES.BUILTIN ? UI.RULE_SOURCE_BUILTIN : UI.RULE_SOURCE_USER;
  }

  async function toggle(rule) {
    const next = !rule.enabled;
    if (await rules.setEnabled(rule.id, next)) {
      snackbar.success(next ? UI.RULE_ENABLED : UI.RULE_DISABLED);
    }
  }

  async function remove(rule) {
    if (await rules.remove(rule.id)) snackbar.success(UI.RULE_DELETED);
  }

  // Both import paths report the same way: what landed, and how many files were refused
  // (with the first reason surfaced, since the full list stays in the errors panel).
  async function runImport(fn) {
    const res = await fn();
    if (!res) return;
    const ok = res.imported || [];
    const bad = res.errors || [];
    if (ok.length === 0 && bad.length === 0) return; // cancelled dialog
    if (ok.length === 0) {
      snackbar.error(`${UI.RULES_IMPORT_NONE} ${bad.length} ${UI.RULES_IMPORT_REJECTED} — ${bad[0].message}`);
      return;
    }
    const suffix = bad.length ? ` · ${bad.length} ${UI.RULES_IMPORT_REJECTED}` : '';
    snackbar.success(`${UI.RULES_IMPORTED}: ${ok.join(', ')}${suffix}`);
  }

  const enabled = $derived($rules.list.filter((r) => r.enabled).length);

  // --- Overflow menus ---
  //
  // Two levels, same idea: the actions you reach for constantly stay visible, and the ones
  // you reach for occasionally go behind a ⋯. In the bar that is library maintenance; on a
  // row it is everything except running that one rule.

  let libraryMenu = $state(false);
  /** id → the row whose menu is open, so only one is ever up. */
  let rowMenu = $state('');

  const LIBRARY_ACTIONS = [
    { id: 'import-files', label: UI.ACTION_IMPORT_RULES },
    { id: 'import-folder', label: UI.ACTION_IMPORT_RULE_FOLDER },
    { id: 'reload', label: UI.ACTION_RELOAD_RULES },
  ];

  function onLibraryAction(id) {
    if (id === 'import-files') runImport(rules.importFiles);
    else if (id === 'import-folder') runImport(rules.importFolder);
    else if (id === 'reload') rules.reload();
  }

  // A built-in cannot be written to or deleted — it lives in the binary — so its menu offers
  // the copy instead. Building the list per rule keeps that distinction in one place rather
  // than spread across conditionals in the markup.
  function rowActions(rule) {
    if (rules.isDeletable(rule)) {
      return [
        { id: 'edit', label: UI.ACTION_EDIT_RULE },
        { id: 'duplicate', label: UI.ACTION_DUPLICATE_RULE },
        { id: 'delete', label: UI.ACTION_DELETE_RULE, danger: true },
      ];
    }
    return [
      { id: 'duplicate', label: UI.ACTION_DUPLICATE_RULE },
      { id: 'inspect', label: UI.ACTION_INSPECT_RULE },
    ];
  }

  function onRowAction(rule, id) {
    if (id === 'edit') openEditor(rule);
    else if (id === 'duplicate') ruleEditor.duplicate(rule);
    else if (id === 'delete') remove(rule);
    else if (id === 'inspect') inspect(rule);
  }

  // --- Rule inspector (P19) ---

  let inspected = $state(/** @type {any} */ (null));
  let inspectedSource = $state(/** @type {any} */ (null));
  let inspectLoading = $state(false);

  async function inspect(rule) {
    inspected = rule;
    inspectedSource = null;
    inspectLoading = true;
    // Load the graph list too, so the inspector can offer a jump to the graph this rule
    // has already produced (P6 binds graphs to rules by id).
    graph.loadGraphs();
    inspectedSource = await rules.source(rule.id);
    inspectLoading = false;
  }

  function closeInspector() {
    inspected = null;
    inspectedSource = null;
  }

  // The graph this rule built, if it has run. Bound by rule id, so a renamed graph still
  // resolves.
  const inspectedGraph = $derived(
    inspected ? ($graph.graphs || []).find((g) => g.rule_id === inspected.id) || null : null,
  );

  async function openRuleGraph() {
    const target = inspectedGraph;
    closeInspector();
    await graph.setActive(target.id);
    route.go(ROUTES.GRAPH);
  }

  async function copySource() {
    const text = inspectedSource && inspectedSource.source;
    if (!text) return;
    try {
      await navigator.clipboard.writeText(text);
      snackbar.success(UI.RULE_COPIED);
    } catch (err) {
      snackbar.error(String(err && err.message ? err.message : err));
    }
  }

  function chainText(rule) {
    return chain(rule)
      .map((p) => (p.kind === 'step' ? p.text : p.text ? `—${p.text}→` : UI.RULE_UNTAGGED_LABEL))
      .join(' ');
  }

  // A run can be scoped to whatever the events view is filtered to. "Active" means the
  // filter differs from the empty one in any field that actually narrows the dataset —
  // paging and sort direction don't count, because a build always evaluates the whole set.
  const PAGING_FIELDS = ['offset', 'limit', 'descending'];
  const filterActive = $derived.by(() => {
    const empty = emptyFilter();
    return Object.keys(empty).some(
      (k) => !PAGING_FIELDS.includes(k) && String($events.filter[k] ?? '') !== String(empty[k] ?? ''),
    );
  });
  let scoped = $state(false);

  // --- Rule editor (P26) ---

  // Editing is only ever offered on a rule that can actually be written to. A built-in lives
  // in the binary, so the equivalent action there is a duplicate: the copy takes a new name
  // and saves as a new rule, which is the documented way to vary one.
  async function openEditor(rule) {
    closeInspector();
    if (rules.isDeletable(rule)) await ruleEditor.edit(rule);
    else await ruleEditor.duplicate(rule);
  }

  // Repairing a file that failed to load. Until now this needed leaving the application: the
  // errors panel could name the problem but not do anything about it.
  async function fixBrokenFile(loadError) {
    const src = await rules.sourceOfFile(loadError.path);
    if (src === null) {
      snackbar.error($rules.error || UI.RULE_INSPECT_UNAVAILABLE);
      return;
    }
    ruleEditor.fix(src, loadError.path);
  }

  // After a save the library has already refreshed. `andRun` comes from Save-and-run, and
  // reuses the ordinary run path so a rule saved that way reports exactly like one run from
  // the list.
  function onRuleSaved(result, andRun) {
    if (andRun) runRules([result.rule.id]);
  }

  // Runs rules and reports what was built. On success the first produced graph becomes
  // active and the user is offered a jump to the canvas — the run itself never steals the
  // view, so a build can't yank you out of what you were doing.
  async function runRules(ruleIds) {
    const filter = scoped && filterActive ? $events.filter : emptyFilter();
    const res = await rules.run(ruleIds, filter);
    if (!res) {
      snackbar.error($rules.error || UI.RULES_RUN_NONE);
      return;
    }
    const outcomes = res.outcomes || [];
    if (outcomes.length === 0) {
      snackbar.warn(UI.RULES_RUN_NONE);
      return;
    }
    const relations = outcomes.reduce((n, o) => n + (o.relations || 0), 0);
    if (relations === 0) {
      snackbar.info(UI.RULES_RUN_EMPTY);
      return;
    }
    const truncated = outcomes.some((o) => o.truncated);
    // Undated events cannot take part in time-ordered correlation. Saying so keeps a run
    // that appears to have "missed" events explicable rather than arbitrary (P23.5).
    const skipped = res.skipped_undated
      ? ` · ${res.skipped_undated} ${UI.RULES_RUN_SKIPPED_UNDATED}`
      : '';
    const summary =
      `${outcomes.length} ${UI.RULES_RUN_GRAPHS} · ${relations} ${UI.RULES_RUN_RELATIONS} ` +
      `${res.events} ${UI.RULES_RUN_EVENTS}${truncated ? ` · ${UI.RULES_RUN_TRUNCATED}` : ''}${skipped}`;

    const first = outcomes.find((o) => o.relations > 0) || outcomes[0];
    snackbar.success(summary, {
      action: {
        label: UI.ACTION_OPEN_GRAPH,
        run: async () => {
          await graph.setActive(first.graph_id);
          route.go(ROUTES.GRAPH);
        },
      },
    });
  }
</script>

<div class="view">
  <!-- Two primary actions stay visible; the library-maintenance ones sit behind the overflow.
       Importing and reloading are things you do occasionally, and each one competing with
       "Run enabled rules" for attention was what made this bar unreadable. -->
  <AppBar route={ROUTES.RULES}>
    <Button variant="filled" onclick={() => runRules([])} disabled={$rules.running || enabled === 0}>
      {$rules.running ? UI.RULES_RUNNING : UI.ACTION_RUN_RULES}
    </Button>
    <Button variant="tonal" onclick={() => ruleEditor.createNew()}>{UI.ACTION_NEW_RULE}</Button>
    <div class="overflow">
      <button
        type="button"
        class="more"
        aria-haspopup="menu"
        aria-expanded={libraryMenu}
        title={UI.NAV_MORE}
        onclick={() => (libraryMenu = !libraryMenu)}>⋯</button
      >
      <Menu bind:open={libraryMenu} align="right" label={UI.NAV_MORE} items={LIBRARY_ACTIONS} onselect={onLibraryAction} />
    </div>
    <Button variant="tonal" onclick={() => theme.toggle()}>
      {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
    </Button>
  </AppBar>

  {#if $rules.running}
    <!-- A build over many rules reports per-rule movement, so a long run is visibly
         working rather than an indefinite "Running rules…". -->
    <div class="runbar">
      <div class="runtext">
        {#if $rules.progress}
          <b>{$rules.progress.rule_index} / {$rules.progress.rule_total}</b>
          <span class="runrule">{$rules.progress.rule}</span>
          <span class="runrel">{$rules.progress.relations} {UI.RULES_RUN_RELATIONS_SHORT}</span>
        {:else}
          <b>{UI.RULES_RUNNING}</b>
        {/if}
      </div>
      <div class="runprogress">
        <ProgressBar
          value={$rules.progress && $rules.progress.rule_total
            ? $rules.progress.rule_index / $rules.progress.rule_total
            : null}
        />
      </div>
      <Button variant="outlined" onclick={() => rules.cancelRun()}>{UI.ACTION_CANCEL}</Button>
    </div>
  {/if}

  <div class="body">
    <!-- The tally lives here rather than in the app bar: it describes the list directly
         below it, and it was taking bar space from the actions. -->
    <div class="scoperow">
      <p class="count">
        <b>{$rules.list.length}</b>
        {UI.RULES_COUNT_SUFFIX} · <b>{enabled}</b>
        {UI.RULES_ENABLED_SUFFIX}
      </p>
      <label class="scope" class:disabled={!filterActive} title={filterActive ? '' : UI.RULES_SCOPE_NONE}>
        <input type="checkbox" bind:checked={scoped} disabled={!filterActive} />
        <span>{UI.RULES_SCOPE_FILTER}</span>
      </label>
    </div>
    <p class="subtitle">{UI.RULES_SUBTITLE}</p>

    {#if $rules.list.length === 0}
      <p class="msg">{$rules.loading ? UI.SPLASH_LOADING : UI.RULES_EMPTY}</p>
    {:else}
      <ul class="rules">
        {#each $rules.list as rule (rule.id)}
          <li class="rule" class:off={!rule.enabled}>
            <label class="toggle">
              <input type="checkbox" checked={rule.enabled} onchange={() => toggle(rule)} />
              <span class="track"><span class="thumb"></span></span>
            </label>

            <!-- Only the metadata block opens the inspector; the toggle and the action
                 cluster sit outside it, so clicking those never triggers a click-through. -->
            <button class="meta" type="button" onclick={() => inspect(rule)} title={UI.RULE_INSPECT_HINT}>
              <div class="titleline">
                <span class="name">{rule.name}</span>
                <span class="badge" class:user={rule.source === RULE_SOURCES.USER}>{sourceLabel(rule)}</span>
              </div>
              {#if rule.description}
                <p class="desc">{rule.description}</p>
              {/if}
              <div class="chain">
                {#each chain(rule) as part}
                  {#if part.kind === 'step'}
                    <span class="step">{part.text}</span>
                  {:else if part.text}
                    <span class="link labeled">{part.text} {UI.RULE_UNTAGGED_LABEL}</span>
                  {:else}
                    <span class="link">{UI.RULE_UNTAGGED_LABEL}</span>
                  {/if}
                {/each}
              </div>
            </button>

            <!-- Running this one rule is the action worth a click; the rest are occasional,
                 and thirty rows of three buttons was more chrome than library. -->
            <div class="actions">
              {#if !rules.isDeletable(rule)}
                <span class="protected" title={UI.RULE_BUILTIN_HINT}>🔒</span>
              {/if}
              <Button variant="text" onclick={() => runRules([rule.id])} disabled={$rules.running}>
                {UI.ACTION_RUN_RULE}
              </Button>
              <div class="overflow">
                <button
                  type="button"
                  class="more"
                  aria-haspopup="menu"
                  aria-expanded={rowMenu === rule.id}
                  aria-label={`${UI.NAV_MORE}: ${rule.name}`}
                  onclick={() => (rowMenu = rowMenu === rule.id ? '' : rule.id)}>⋮</button
                >
                <!-- One shared `rowMenu` id rather than a flag per row, so opening one menu
                     closes any other. That rules out bind:open, hence onclose. -->
                <Menu
                  open={rowMenu === rule.id}
                  align="right"
                  label={rule.name}
                  items={rowActions(rule)}
                  onselect={(id) => onRowAction(rule, id)}
                  onclose={() => (rowMenu = '')}
                />
              </div>
            </div>
          </li>
        {/each}
      </ul>
    {/if}

    {#if $rules.errors.length > 0}
      <section class="errors">
        <h3>{UI.RULES_LOAD_ERRORS}</h3>
        <ul>
          {#each $rules.errors as e}
            <li>
              <div class="errhead">
                <code>{e.path}</code>
                <!-- Repairing a broken file used to mean leaving the application. A built-in
                     that fails to load has no path and nothing to repair, so the action only
                     appears for a file on disk. -->
                {#if e.path}
                  <Button variant="text" title={UI.RULES_FIX_HINT} onclick={() => fixBrokenFile(e)}>
                    {UI.ACTION_FIX_RULE}
                  </Button>
                {/if}
              </div>
              <span>{e.message}</span>
            </li>
          {/each}
        </ul>
      </section>
    {/if}

    {#if $rules.dir}
      <p class="dir">{UI.RULES_DIR_PREFIX} <code>{$rules.dir}</code></p>
    {/if}
  </div>
</div>

{#if inspected}
  <Dialog open={true} wide title={`${UI.RULE_INSPECT_TITLE}: ${inspected.name}`} onclose={closeInspector}>
    <div class="inspect">
      <h3>{UI.RULE_INSPECT_METADATA}</h3>
      <dl class="facts">
        <dt>{UI.LABEL_RULE_ID}</dt>
        <dd><code>{inspected.id}</code></dd>
        <dt>{UI.LABEL_RULE_SOURCE}</dt>
        <dd>{sourceLabel(inspected)}</dd>
        <dt>{UI.LABEL_RULE_ENABLED}</dt>
        <dd>{inspected.enabled ? UI.VALUE_YES : UI.VALUE_NO}</dd>
        <dt>{UI.LABEL_RULE_ALGORITHM}</dt>
        <dd>{inspected.algorithm || UI.VALUE_NONE}</dd>
        <dt>{UI.LABEL_RULE_RELATION}</dt>
        <dd>{inspected.relation_type || UI.VALUE_NONE}</dd>
        <dt>{UI.LABEL_RULE_FORMAT}</dt>
        <dd>{inspected.format_version}</dd>
        <dt>{UI.LABEL_RULE_STEPS}</dt>
        <dd>{(inspected.sequence || []).length}</dd>
        <dt>{UI.LABEL_RULE_CHAIN}</dt>
        <dd class="mono">{chainText(inspected)}</dd>
        {#if inspectedSource && inspectedSource.file}
          <dt>{UI.LABEL_RULE_FILE}</dt>
          <dd><code>{inspectedSource.file}</code></dd>
        {/if}
        {#if inspected.path}
          <dt>{UI.LABEL_RULE_PATH}</dt>
          <dd><code class="wrap">{inspected.path}</code></dd>
        {/if}
        {#if inspectedGraph}
          <dt>{UI.LABEL_RULE_GRAPH}</dt>
          <dd>
            <button class="linkbtn" type="button" onclick={openRuleGraph}>{inspectedGraph.name}</button>
          </dd>
        {/if}
      </dl>

      <div class="srchead">
        <h3>{UI.RULE_INSPECT_SOURCE}</h3>
        {#if inspectedSource && inspectedSource.source}
          <span class="srcactions">
            <Button variant="text" onclick={() => openEditor(inspected)}>
              {rules.isDeletable(inspected) ? UI.ACTION_EDIT_RULE : UI.ACTION_DUPLICATE_RULE}
            </Button>
            <Button variant="text" onclick={copySource}>{UI.ACTION_COPY_SOURCE}</Button>
          </span>
        {/if}
      </div>
      {#if inspectLoading}
        <p class="msg">{UI.RULE_INSPECT_LOADING}</p>
      {:else if inspectedSource && inspectedSource.source}
        <pre class="code">{inspectedSource.source}</pre>
      {:else}
        <p class="msg">{UI.RULE_INSPECT_UNAVAILABLE}</p>
      {/if}
    </div>

    {#snippet actions()}
      <Button variant="tonal" onclick={closeInspector}>{UI.ACTION_CLOSE}</Button>
    {/snippet}
  </Dialog>
{/if}

<RuleEditorDialog onsaved={onRuleSaved} />

<style>
  .view {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .count {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
  }
  .count b {
    color: var(--color-on-surface);
  }

  /* Overflow trigger, used both in the app bar and on each row. It is a plain glyph until
     pointed at, so thirty of them down a list read as texture rather than as thirty
     buttons competing with the rule names. */
  .overflow {
    position: relative;
    display: inline-flex;
  }
  .more {
    width: 28px;
    height: 28px;
    display: inline-flex;
    align-items: center;
    justify-content: center;
    background: transparent;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--color-on-surface-muted);
    font-size: 1rem;
    line-height: 1;
    cursor: pointer;
    transition: background var(--motion-fast) ease, color var(--motion-fast) ease;
  }
  .more:hover,
  .more[aria-expanded='true'] {
    background: var(--color-surface-variant);
    color: var(--color-primary);
    border-color: var(--color-outline);
  }
  .more:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .runbar {
    display: flex;
    align-items: center;
    gap: var(--space-4);
    padding: var(--space-2) var(--space-5);
    background: var(--color-surface-variant);
    border-bottom: 1px solid var(--color-outline);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    color: var(--color-on-surface);
  }
  .runtext {
    display: flex;
    align-items: baseline;
    gap: var(--space-2);
    min-width: 0;
    white-space: nowrap;
  }
  .runrule {
    overflow: hidden;
    text-overflow: ellipsis;
    max-width: 28ch;
  }
  .runrel {
    color: var(--color-on-surface-variant);
  }
  .runprogress {
    flex: 1;
    min-width: 80px;
  }
  .body {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5);
  }
  .subtitle,
  .msg {
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-variant);
    margin: 0 0 var(--space-4);
  }
  .scoperow {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  .scope {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    color: var(--color-on-surface-variant);
    white-space: nowrap;
    cursor: pointer;
  }
  .scope.disabled {
    opacity: 0.5;
    cursor: help;
  }
  .msg {
    padding: var(--space-6);
    text-align: center;
  }

  .rules {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .rule {
    display: grid;
    grid-template-columns: auto 1fr auto;
    gap: var(--space-4);
    align-items: start;
    padding: var(--space-4);
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    transition: opacity var(--motion-fast) ease, border-color var(--motion-fast) ease;
  }
  .rule.off {
    opacity: 0.55;
  }

  /* Switch: a native checkbox for semantics/keyboard, visually a track + thumb. */
  .toggle {
    position: relative;
    display: inline-flex;
    align-items: center;
    margin-top: 2px;
    cursor: pointer;
  }
  .toggle input {
    position: absolute;
    opacity: 0;
    width: 100%;
    height: 100%;
    margin: 0;
    cursor: pointer;
  }
  .track {
    width: 36px;
    height: 20px;
    border-radius: 10px;
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    display: inline-flex;
    align-items: center;
    padding: 2px;
    transition: background var(--motion-fast) ease, border-color var(--motion-fast) ease;
  }
  .thumb {
    width: 14px;
    height: 14px;
    border-radius: 50%;
    background: var(--color-on-surface-variant);
    transition: transform var(--motion-fast) ease, background var(--motion-fast) ease;
  }
  .toggle input:checked + .track {
    background: var(--color-primary);
    border-color: var(--color-primary);
  }
  .toggle input:checked + .track .thumb {
    transform: translateX(16px);
    background: var(--color-on-primary);
  }
  .toggle input:focus-visible + .track {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }

  /* The metadata block is a button (it opens the inspector) but must look like plain
     content — only a subtle hover hints that it is clickable. */
  .meta {
    min-width: 0;
    display: block;
    width: 100%;
    text-align: left;
    background: none;
    border: none;
    padding: var(--space-1);
    margin: calc(-1 * var(--space-1));
    border-radius: var(--radius-sm);
    color: inherit;
    font: inherit;
    cursor: pointer;
    transition: background var(--motion-fast) ease;
  }
  .meta:hover {
    background: var(--color-surface-variant);
  }
  .meta:focus-visible {
    outline: 2px solid var(--color-accent);
    outline-offset: 2px;
  }
  .titleline {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .name {
    font-family: var(--font-sans);
    font-weight: 600;
    color: var(--color-on-surface);
  }
  .badge {
    font-family: var(--font-sans);
    font-size: 0.72rem;
    padding: 1px var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--color-surface-variant);
    color: var(--color-on-surface-variant);
    border: 1px solid var(--color-outline);
  }
  .badge.user {
    background: color-mix(in srgb, var(--color-primary) 16%, transparent);
    color: var(--color-primary);
    border-color: color-mix(in srgb, var(--color-primary) 40%, transparent);
  }
  .desc {
    margin: var(--space-1) 0 0;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-variant);
  }

  .chain {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    margin-top: var(--space-3);
  }
  .step {
    font-family: var(--font-mono);
    font-size: 0.85rem;
    padding: 1px var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--color-surface-variant);
    color: var(--color-on-surface);
  }
  .link {
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-variant);
  }
  .link.labeled {
    color: var(--color-accent);
  }

  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-2);
  }
  .protected {
    font-size: 0.85rem;
    color: var(--color-on-surface-variant);
    cursor: help;
    padding: 0 var(--space-2);
  }

  .errors {
    margin-top: var(--space-6);
    border: 1px solid var(--color-error);
    border-radius: var(--radius-md);
    padding: var(--space-4);
  }
  .errors h3 {
    margin: 0 0 var(--space-3);
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-error);
  }
  .errors ul {
    list-style: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .errors li {
    display: flex;
    flex-direction: column;
    gap: 2px;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-variant);
  }
  .errors code {
    font-family: var(--font-mono);
    font-size: 0.72rem;
    color: var(--color-on-surface);
    word-break: break-all;
  }
  .errhead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  .srcactions {
    display: flex;
    gap: var(--space-1);
  }

  .dir {
    margin-top: var(--space-5);
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-variant);
  }
  .dir code {
    font-family: var(--font-mono);
    word-break: break-all;
  }

  /* --- Rule inspector (P19) --- */
  .inspect h3 {
    font-family: var(--font-sans);
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-variant);
    margin: 0 0 var(--space-3);
  }
  .facts {
    display: grid;
    grid-template-columns: minmax(110px, auto) 1fr;
    gap: var(--space-2) var(--space-4);
    margin: 0 0 var(--space-5);
    font-family: var(--font-sans);
    font-size: 0.85rem;
  }
  .facts dt {
    color: var(--color-on-surface-variant);
  }
  .facts dd {
    margin: 0;
    color: var(--color-on-surface);
    min-width: 0;
  }
  .facts code {
    font-family: var(--font-mono);
    font-size: 0.8rem;
  }
  .facts .wrap {
    word-break: break-all;
  }
  .facts .mono {
    font-family: var(--font-mono);
    font-size: 0.8rem;
  }
  .linkbtn {
    background: none;
    border: none;
    padding: 0;
    font: inherit;
    color: var(--color-primary);
    text-decoration: underline;
    cursor: pointer;
  }
  .srchead {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-3);
  }
  /* The definition is shown verbatim: preserved whitespace, horizontal scroll rather than
     wrapping (wrapping would misrepresent how the file is actually written), and a bounded
     height so a large rule cannot push the dialog off-screen. */
  .code {
    margin: 0;
    padding: var(--space-3);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    font-family: var(--font-mono);
    font-size: 0.78rem;
    line-height: 1.5;
    color: var(--color-on-surface);
    max-height: 320px;
    overflow: auto;
    white-space: pre;
    tab-size: 2;
  }
</style>
