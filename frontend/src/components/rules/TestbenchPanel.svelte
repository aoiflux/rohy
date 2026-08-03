<script>
  // The rule testbench: what this rule would produce against the case in front of you.
  //
  // The counts are laid out as prominently as the matches on purpose. A rule returning three
  // results because the pattern is rare and one returning three because most of the case could
  // not be considered are the same number, and only the counts tell them apart — so
  // "could not be considered" is a headline here rather than a diagnostic buried under it.
  //
  // Nothing on this panel writes. There is deliberately no "add to graph" affordance: a dry run
  // that could touch a graph is one an analyst has to stop and think about before pressing.
  import { UI } from '../../lib/consts/index.js';

  let {
    /** the DryRunResult, or null when the buffer has changed since the last run */
    result = null,
    running = false,
    /** whether the rule currently parses; a run is pointless otherwise */
    runnable = true,
    onrun = undefined,
  } = $props();

  const fmt = (n) => new Intl.NumberFormat().format(n ?? 0);

  /** The counts worth surfacing, and only the ones this run actually has something to say
   *  about — a zero unresolved-parents on a sequence rule is noise. */
  const caveats = $derived(
    !result
      ? []
      : [
          { n: result.skipped_no_keys, label: UI.TESTBENCH_NO_KEYS, hint: UI.TESTBENCH_NO_KEYS_HINT },
          { n: result.stale_correlation_keys, label: UI.TESTBENCH_STALE, hint: UI.TESTBENCH_STALE_HINT },
          { n: result.unresolved_parents, label: UI.TESTBENCH_UNRESOLVED, hint: UI.TESTBENCH_UNRESOLVED_HINT },
          { n: result.skipped_undated, label: UI.TESTBENCH_UNDATED, hint: UI.TESTBENCH_UNDATED_HINT },
        ].filter((c) => c.n > 0),
  );

  const time = (iso) => {
    if (!iso) return '';
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '' : d.toISOString().replace('T', ' ').slice(0, 19);
  };
</script>

<div class="testbench">
  <div class="actions">
    <button class="run" onclick={() => onrun?.()} disabled={running || !runnable}>
      {running ? UI.TESTBENCH_RUNNING : UI.TESTBENCH_RUN}
    </button>
    <span class="blurb">{UI.TESTBENCH_BLURB}</span>
  </div>

  {#if !runnable}
    <p class="msg">{UI.TESTBENCH_FIX_FIRST}</p>
  {:else if !result}
    <p class="msg">{UI.TESTBENCH_IDLE}</p>
  {:else}
    <div class="stats">
      <div class="stat">
        <span class="n">{fmt(result.matches)}</span>
        <span class="k">{UI.TESTBENCH_MATCHES}</span>
      </div>
      <div class="stat">
        <span class="n">{fmt(result.relations)}</span>
        <span class="k">{UI.TESTBENCH_EDGES}</span>
      </div>
      <div class="stat">
        <span class="n">{fmt(result.events)}</span>
        <span class="k">{UI.TESTBENCH_EVENTS}</span>
      </div>
      <div class="stat">
        <span class="n">{fmt(result.elapsed_ms)}<small>ms</small></span>
        <span class="k">{UI.TESTBENCH_ELAPSED}</span>
      </div>
    </div>

    {#if result.matches === 0}
      <p class="verdict none">{UI.TESTBENCH_NO_MATCHES}</p>
    {/if}
    {#if result.truncated}
      <p class="verdict warn">{UI.TESTBENCH_TRUNCATED} {fmt(result.dropped)}</p>
    {/if}

    {#if caveats.length}
      <!-- What the run could NOT see. Placed above the matches because it changes how the
           match count should be read, and a reader who scrolls past it has already drawn a
           conclusion. -->
      <ul class="caveats">
        {#each caveats as c (c.label)}
          <li title={c.hint}><strong>{fmt(c.n)}</strong> {c.label}</li>
        {/each}
      </ul>
    {/if}

    {#if result.samples?.length}
      <h4>{UI.TESTBENCH_SAMPLES}</h4>
      <ol class="samples">
        {#each result.samples as sample (sample.match_id)}
          <li>
            <div class="chain">
              {#each sample.events as e, i (e.id)}
                {#if i > 0}<span class="arrow">→</span>{/if}
                <span class="ev" title={`${e.provider || ''} ${e.channel || ''}`.trim()}>
                  <span class="eid">{e.event_id}</span>
                  <span class="meta">{e.computer}</span>
                  <span class="meta">{time(e.timestamp)}</span>
                </span>
              {/each}
            </div>
            {#if sample.basis?.length}
              <div class="basis">
                {#each sample.basis as b (b)}<code>{b}</code>{/each}
              </div>
            {/if}
          </li>
        {/each}
      </ol>
    {/if}
  {/if}
</div>

<style>
  .testbench {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-3);
    overflow-y: auto;
  }
  .actions {
    display: flex;
    align-items: center;
    gap: var(--space-3);
    flex-wrap: wrap;
  }
  .run {
    border: none;
    background: var(--color-primary);
    color: var(--color-on-primary);
    border-radius: var(--radius-md);
    padding: var(--space-2) var(--space-4);
    font-family: var(--font-sans);
    font-weight: 600;
    font-size: 0.82rem;
    cursor: pointer;
  }
  .run:disabled {
    opacity: 0.45;
    cursor: default;
  }
  .blurb,
  .msg {
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-on-surface-muted);
    line-height: 1.5;
    margin: 0;
  }

  .stats {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(72px, 1fr));
    gap: var(--space-2);
  }
  .stat {
    display: flex;
    flex-direction: column;
    padding: var(--space-2);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    background: var(--color-surface-variant);
  }
  .n {
    font-size: 1.15rem;
    font-weight: 700;
    color: var(--color-on-surface);
    font-variant-numeric: tabular-nums;
  }
  .n small {
    font-size: 0.62rem;
    font-weight: 400;
    color: var(--color-on-surface-muted);
    margin-left: 1px;
  }
  .k {
    font-size: 0.62rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-on-surface-muted);
  }

  .verdict {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.78rem;
    line-height: 1.5;
    padding: var(--space-2) var(--space-3);
    border-radius: var(--radius-sm);
  }
  .verdict.none {
    background: color-mix(in srgb, var(--color-on-surface-muted) 12%, transparent);
    color: var(--color-on-surface);
  }
  .verdict.warn {
    background: color-mix(in srgb, var(--color-warning) 16%, transparent);
    color: var(--color-on-surface);
  }

  .caveats {
    margin: 0;
    padding-left: 1.1em;
    font-family: var(--font-sans);
    font-size: 0.76rem;
    line-height: 1.6;
    color: var(--color-on-surface);
  }
  .caveats li {
    cursor: help;
  }

  h4 {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.68rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  .samples {
    margin: 0;
    padding-left: 1.2em;
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
  }
  .chain {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .ev {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-2);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    padding: 2px var(--space-2);
    background: var(--color-surface);
  }
  .eid {
    font-weight: 700;
    font-size: 0.8rem;
    color: var(--color-on-surface);
  }
  .meta {
    font-size: 0.66rem;
    color: var(--color-on-surface-muted);
    font-variant-numeric: tabular-nums;
  }
  .arrow {
    color: var(--color-primary);
  }
  .basis {
    display: flex;
    gap: var(--space-2);
    flex-wrap: wrap;
    margin-top: var(--space-1);
  }
  .basis code {
    font-size: 0.68rem;
    background: var(--color-surface-variant);
    border-radius: var(--radius-sm);
    padding: 1px 5px;
    color: var(--color-primary);
  }
</style>
