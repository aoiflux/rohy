<script>
  // The read-only query console (item 33).
  //
  // 🔒 A structured builder and nothing more: a subject, conditions ANDed together, an optional
  // field to count by, a limit. Every control is built from QueryAPI.Describe, so the page cannot
  // offer what the backend would refuse, and it holds no field list of its own.
  //
  // 🔒 Nothing here writes, and nothing runs until Run is pressed.
  import { onMount } from 'svelte';
  import { theme } from '../stores/theme.js';
  import { query } from '../stores/query.js';
  import { maintenance } from '../stores/maintenance.js';
  import { ERROR_CODES, ROUTES, THEMES, UI } from '../lib/consts/index.js';
  import { KIND, filterable, groupable, fieldOf, subjectOf } from '../lib/query/spec.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import Select from '../components/material/Select.svelte';
  import QueryResult from '../components/query/QueryResult.svelte';

  const vocab = $derived($query.vocab);
  const subject = $derived(subjectOf(vocab, $query.subject));
  const fields = $derived(filterable(subject));
  const subjects = $derived((vocab?.subjects ?? []).map((s) => ({ value: s.key, label: s.label })));
  const fieldOptions = $derived(fields.map((f) => ({ value: f.key, label: f.label })));
  const groupOptions = $derived([
    { value: '', label: UI.QUERY_GROUP_NONE },
    ...groupable(subject).map((f) => ({ value: f.key, label: f.label })),
  ]);
  const full = $derived(vocab ? $query.rows.length >= vocab.max_conditions : true);
  const error = $derived($query.error);
  const queryFault = $derived(error?.code === ERROR_CODES.QUERY);

  onMount(() => {
    query.load();
  });

  // A redaction may remove what the last result shows: drop it rather than keep the content
  // on screen (the rule every list follows, api_caselock.go).
  let redactionsSeen = $maintenance.redactions;
  $effect(() => {
    const n = $maintenance.redactions;
    if (n === redactionsSeen) return;
    redactionsSeen = n;
    query.dropResult();
  });

  function opOptions(row) {
    return (fieldOf(subject, row.field)?.ops ?? []).map((op) => ({ value: op, label: UI.QUERY_OPS[op] ?? op }));
  }

  function valueOptions(row) {
    return (fieldOf(subject, row.field)?.values ?? []).map((v) => ({ value: v, label: v }));
  }

  function kindOf(row) {
    return fieldOf(subject, row.field)?.kind ?? KIND.TEXT;
  }

  function helpOf(row) {
    return fieldOf(subject, row.field)?.help ?? '';
  }

  function onKey(e) {
    if (e.key === 'Enter') query.run();
  }
</script>

<div class="qv">
  <AppBar route={ROUTES.QUERY}>
    <Button variant="tonal" onclick={() => query.run()} disabled={!vocab || $query.running}>
      {$query.running ? UI.QUERY_RUNNING : UI.QUERY_RUN}
    </Button>
    <Button variant="text" onclick={() => query.clear()} disabled={$query.running}>{UI.QUERY_CLEAR}</Button>
    <Button variant="tonal" onclick={() => theme.toggle()}>
      {$theme === THEMES.DARK ? '☀' : '☾'} {UI.ACTION_TOGGLE_THEME}
    </Button>
  </AppBar>

  <div class="body">
    <p class="intro">{UI.QUERY_INTRO}</p>

    {#if error}
      <div class="banner err" role="alert">
        <span>{queryFault ? UI.QUERY_FAULT_QUERY : UI.QUERY_FAULT_CASE} {error.message}</span>
        <Button variant="text" onclick={() => query.clearError()}>{UI.MAINT_DISMISS}</Button>
      </div>
    {/if}

    {#if !vocab}
      {#if !error}<p class="hint">{UI.QUERY_LOADING}</p>{/if}
    {:else}
      <section class="card builder">
        <div class="line">
          <Select compact label={UI.QUERY_SUBJECT} value={$query.subject} options={subjects}
            onchange={(v) => query.setSubject(v)} />
        </div>

        <h3>{UI.QUERY_WHERE}</h3>
        {#if $query.rows.length === 0}
          <p class="hint">{UI.QUERY_NO_CONDITIONS}</p>
        {/if}
        {#each $query.rows as row, i (i)}
          <div class="line cond">
            <Select compact value={row.field} options={fieldOptions} onchange={(v) => query.setField(i, v)} />
            <Select compact value={row.op} options={opOptions(row)} onchange={(v) => query.setOp(i, v)} />
            {#if kindOf(row) === KIND.ENUM}
              <Select compact value={row.value} options={valueOptions(row)} onchange={(v) => query.setValue(i, v)} />
            {:else}
              <input
                class="value"
                aria-label={fieldOf(subject, row.field)?.label ?? ''}
                inputmode={kindOf(row) === KIND.NUMBER ? 'numeric' : 'text'}
                placeholder={kindOf(row) === KIND.TIME ? UI.QUERY_TIME_PLACEHOLDER : ''}
                maxlength={vocab.max_value_length}
                value={row.value}
                oninput={(e) => query.setValue(i, e.currentTarget.value)}
                onkeydown={onKey}
              />
            {/if}
            <Button variant="text" onclick={() => query.removeCondition(i)} title={UI.QUERY_REMOVE_CONDITION}>✕</Button>
          </div>
          {#if helpOf(row)}<p class="hint help">{helpOf(row)}</p>{/if}
        {/each}
        <div class="line">
          <Button variant="text" onclick={() => query.addCondition()} disabled={full}>+ {UI.QUERY_ADD_CONDITION}</Button>
        </div>

        <div class="line">
          <Select compact label={UI.QUERY_GROUP_BY} value={$query.groupBy} options={groupOptions}
            onchange={(v) => query.setGroupBy(v)} />
          <label class="limit">
            <span>{UI.QUERY_LIMIT}</span>
            <input
              type="number"
              min="1"
              max={vocab.max_limit}
              placeholder={String(vocab.default_limit)}
              value={$query.limit || ''}
              oninput={(e) => query.setLimit(e.currentTarget.value === '' ? 0 : Number(e.currentTarget.value))}
              onkeydown={onKey}
            />
          </label>
        </div>

        {#if $query.problem}
          <p class="problem" role="status">{$query.problem}</p>
        {/if}
      </section>

      {#if $query.dropped}
        <div class="banner"><span>{UI.QUERY_DROPPED}</span></div>
      {/if}

      <QueryResult result={$query.result} columns={subject?.columns ?? []} />
    {/if}
  </div>
</div>

<style>
  .qv {
    display: flex;
    flex-direction: column;
    height: 100%;
    overflow: hidden;
  }
  .body {
    display: flex;
    flex-direction: column;
    gap: 12px;
    padding: var(--space-5);
    overflow-y: auto;
    min-height: 0;
  }
  .intro {
    margin: 0;
    max-width: 72ch;
    font-size: 0.9rem;
    line-height: 1.5;
  }
  .card {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 14px 16px;
    border-radius: 10px;
    background: var(--md-sys-color-surface-container);
  }
  h3 {
    margin: 4px 0 0;
    font-size: 0.78rem;
    letter-spacing: 0.08em;
    text-transform: uppercase;
    color: var(--md-sys-color-on-surface-variant);
  }
  .line {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: 8px;
  }
  .value,
  .limit input {
    font-family: var(--font-sans);
    font-size: 0.82rem;
    color: var(--color-on-surface);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    padding: var(--space-2) var(--space-3);
    outline: none;
  }
  .value {
    flex: 1 1 24ch;
    min-width: 16ch;
  }
  .value:focus,
  .limit input:focus {
    border-color: var(--color-primary);
  }
  .limit {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    font-size: 0.72rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-on-surface-variant);
  }
  .limit input {
    width: 8ch;
  }
  .hint {
    margin: 0;
    font-size: 0.8rem;
    line-height: 1.45;
    color: var(--md-sys-color-on-surface-variant);
  }
  .help {
    margin-top: -4px;
  }
  .problem {
    margin: 0;
    font-size: 0.85rem;
    color: var(--md-sys-color-error);
  }
  .banner {
    display: flex;
    flex-direction: column;
    gap: 8px;
    padding: 10px 14px;
    border-radius: 8px;
    background: var(--md-sys-color-surface-container-high);
    font-size: 0.86rem;
  }
  .banner.err {
    border-left: 3px solid var(--md-sys-color-error);
  }
</style>
