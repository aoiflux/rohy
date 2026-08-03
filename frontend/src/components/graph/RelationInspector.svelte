<script>
  // What one edge is, and why it exists.
  //
  // v0.1.0 could only say "the tool inferred this" or "a human asserted this" — a colour on the
  // canvas. Relations now carry which rule produced them, which occurrence they belong to,
  // which step of the sequence they are, and the BASIS: the values that made the match. This
  // panel is where that becomes a claim an analyst can check rather than a claim they have to
  // take.
  //
  // The distinction it must never blur is inferred versus asserted. A rule-created edge and a
  // hand-drawn one are different kinds of evidence and are shown as different things, not as
  // one thing with some fields missing.
  import { UI, RELATION_LABEL } from '../../lib/consts/index.js';

  let {
    /** RelationDetail from InspectRelation, or null when nothing is selected. */
    detail = null,
    onclose = undefined,
    /** (ruleId, stepIndex) => void — open the rule at the step that produced this edge. */
    onopenrule = undefined,
    /** (eventId) => void — focus an endpoint on the canvas. */
    onfocusevent = undefined,
  } = $props();

  const rel = $derived(detail?.relation ?? null);
  const inferred = $derived(rel?.created_by === 'system');
  const time = (iso) => {
    if (!iso) return UI.EVENT_NO_TIMESTAMP ?? '—';
    const d = new Date(iso);
    return Number.isNaN(d.getTime()) ? '—' : d.toISOString().replace('T', ' ').slice(0, 19);
  };
</script>

{#if detail && rel}
  <aside class="inspector" aria-label={UI.INSPECTOR_TITLE}>
    <header>
      <div class="head">
        <!-- Provenance first, and as a badge rather than a field, because it is the single
             most consequential thing about an edge: what kind of claim it is. -->
        <span class="badge" class:inferred class:asserted={!inferred}>
          {inferred ? UI.INSPECTOR_INFERRED : UI.INSPECTOR_ASSERTED}
        </span>
        <span class="type">{RELATION_LABEL[rel.relation_type] ?? rel.relation_type}</span>
      </div>
      <button class="close" onclick={() => onclose?.()} aria-label={UI.ACTION_CLOSE}>×</button>
    </header>

    {#if rel.relation_label}
      <p class="label">“{rel.relation_label}”</p>
    {/if}

    <!-- The two events, in order, because an edge is a directed claim about them. -->
    <div class="ends">
      {#each [detail.from, detail.to] as e, i (i)}
        {#if e}
          <button class="end" onclick={() => onfocusevent?.(e.id)}>
            <span class="eid">{e.event_id}</span>
            <span class="meta">{e.computer}</span>
            <span class="meta">{time(e.timestamp)}</span>
          </button>
          {#if i === 0}<span class="arrow">→</span>{/if}
        {/if}
      {/each}
    </div>

    {#if inferred}
      {#if detail.recorded}
        <dl>
          {#if rel.rule_id}
            <dt>{UI.INSPECTOR_RULE}</dt>
            <dd>
              <button class="link" onclick={() => onopenrule?.(rel.rule_id, rel.step_index)}>
                {detail.graph_name || rel.rule_id}
              </button>
            </dd>
          {/if}
          {#if rel.algorithm}
            <dt>{UI.INSPECTOR_ALGORITHM}</dt>
            <dd><code>{rel.algorithm}</code></dd>
          {/if}
          {#if rel.step_index !== undefined && rel.step_index !== null}
            <dt>{UI.INSPECTOR_STEP}</dt>
            <dd>{UI.INSPECTOR_STEP_N} {rel.step_index + 1}</dd>
          {/if}
          <dt>{UI.INSPECTOR_CONFIDENCE}</dt>
          <dd>
            {rel.confidence_score}
            <span class="hint">{UI.INSPECTOR_CONFIDENCE_HINT}</span>
          </dd>
        </dl>

        {#if rel.basis?.length}
          <!-- The reason the two events were joined. This is what turns "inferred" from a
               colour into something checkable. -->
          <h4>{UI.INSPECTOR_BASIS}</h4>
          <ul class="basis">
            {#each rel.basis as b (b)}<li><code>{b}</code></li>{/each}
          </ul>
        {/if}

        {#if detail.sibling_ids?.length}
          <p class="siblings">
            {UI.INSPECTOR_PART_OF} {detail.sibling_ids.length + 1} {UI.INSPECTOR_EDGES_HIGHLIGHTED}
          </p>
        {/if}
      {:else}
        <!-- Not blank fields. "We did not track this yet" and "the rule recorded nothing" are
             different claims, and only one of them is true here. -->
        <p class="unrecorded">{UI.INSPECTOR_UNRECORDED}</p>
      {/if}
    {:else}
      <p class="asserted-note">{UI.INSPECTOR_ASSERTED_NOTE}</p>
    {/if}
  </aside>
{/if}

<style>
  .inspector {
    display: flex;
    flex-direction: column;
    gap: var(--space-3);
    padding: var(--space-4);
    background: var(--color-surface);
    border-left: 1px solid var(--color-outline);
    overflow-y: auto;
    min-width: 0;
  }
  header {
    display: flex;
    align-items: flex-start;
    justify-content: space-between;
    gap: var(--space-2);
  }
  .head {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .badge {
    font-family: var(--font-sans);
    font-size: 0.62rem;
    font-weight: 700;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    padding: 2px var(--space-2);
    border-radius: var(--radius-sm);
  }
  .badge.inferred {
    background: var(--color-primary);
    color: var(--color-on-primary);
  }
  .badge.asserted {
    background: var(--color-accent);
    color: var(--color-on-accent);
  }
  .type {
    font-family: var(--font-sans);
    font-size: 0.8rem;
    color: var(--color-on-surface-muted);
  }
  .close {
    border: none;
    background: transparent;
    color: var(--color-on-surface-muted);
    font-size: 1.2rem;
    line-height: 1;
    cursor: pointer;
  }
  .label {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface);
    font-style: italic;
  }

  .ends {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    flex-wrap: wrap;
  }
  .end {
    display: inline-flex;
    align-items: baseline;
    gap: var(--space-2);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    background: var(--color-surface-variant);
    padding: var(--space-1) var(--space-2);
    cursor: pointer;
    font-family: var(--font-sans);
  }
  .end:hover {
    border-color: var(--color-primary);
  }
  .eid {
    font-weight: 700;
    font-size: 0.82rem;
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

  dl {
    display: grid;
    grid-template-columns: auto 1fr;
    gap: var(--space-1) var(--space-3);
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.78rem;
  }
  dt {
    color: var(--color-on-surface-muted);
    text-transform: uppercase;
    font-size: 0.62rem;
    letter-spacing: 0.05em;
    align-self: center;
  }
  dd {
    margin: 0;
    color: var(--color-on-surface);
  }
  .hint {
    display: block;
    font-size: 0.66rem;
    color: var(--color-on-surface-muted);
    line-height: 1.4;
  }
  .link {
    border: none;
    background: transparent;
    padding: 0;
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: 0.78rem;
    text-decoration: underline;
    cursor: pointer;
    text-align: left;
  }

  h4 {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.62rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  .basis {
    margin: 0;
    padding-left: 1.1em;
    display: flex;
    flex-direction: column;
    gap: 2px;
  }
  .basis code {
    font-size: 0.72rem;
    color: var(--color-primary);
  }
  .siblings,
  .unrecorded,
  .asserted-note {
    margin: 0;
    font-family: var(--font-sans);
    font-size: 0.74rem;
    line-height: 1.5;
    color: var(--color-on-surface-muted);
  }
  .unrecorded {
    padding: var(--space-2) var(--space-3);
    border-left: 2px solid var(--color-outline);
    background: var(--color-surface-variant);
    border-radius: 0 var(--radius-sm) var(--radius-sm) 0;
  }
</style>
