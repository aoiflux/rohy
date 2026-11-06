<script>
  // One query's answer (item 33): its status lines, then a row table or a count table.
  //
  // The status lines come first and are never optional. A list that stops at the limit, a count
  // that left the undated events out, a case that moved during the read: each changes what the
  // table can be taken to mean, so each is said before the table is read.
  import { UI } from '../../lib/consts/index.js';
  import { cell, groupLabel, statusLines } from '../../lib/query/spec.js';

  let { result = null, columns = [] } = $props();

  const lines = $derived(statusLines(result));
  const rows = $derived(result ? (result.subject === 'relations' ? result.relations : result.events) ?? [] : []);
  const groups = $derived(result?.groups ?? []);
  const counting = $derived(Boolean(result?.group_by));
  const groupColumn = $derived(columns.find((c) => c.key === result?.group_by)?.label ?? result?.group_by ?? '');
</script>

{#if result}
  <div class="result">
    <ul class="status">
      {#each lines as line, i (i)}
        <li class:warn={line === UI.QUERY_STATUS_CHANGED || line === UI.QUERY_STATUS_TRUNCATED}>{line}</li>
      {/each}
    </ul>

    {#if counting}
      {#if groups.length === 0}
        <p class="empty">{UI.QUERY_EMPTY}</p>
      {:else}
        <div class="scroll">
          <table>
            <thead><tr><th>{groupColumn}</th><th class="num">{UI.QUERY_COUNT}</th></tr></thead>
            <tbody>
              {#each groups as g, i (i)}
                <tr class:quiet={g.redacted || g.value === ''}>
                  <td>{groupLabel(g)}</td>
                  <td class="num">{g.count}</td>
                </tr>
              {/each}
            </tbody>
          </table>
        </div>
      {/if}
    {:else if rows.length === 0}
      <p class="empty">{UI.QUERY_EMPTY}</p>
    {:else}
      <div class="scroll">
        <table>
          <thead>
            <tr>
              {#each columns as c (c.key)}<th>{c.label}</th>{/each}
            </tr>
          </thead>
          <tbody>
            {#each rows as row (row.id)}
              <tr class:quiet={row.redacted}>
                {#each columns as c (c.key)}<td>{cell(row, c.key)}</td>{/each}
              </tr>
            {/each}
          </tbody>
        </table>
      </div>
    {/if}
  </div>
{/if}

<style>
  .result {
    display: flex;
    flex-direction: column;
    gap: 8px;
    min-height: 0;
  }
  .status {
    display: flex;
    flex-direction: column;
    gap: 2px;
    margin: 0;
    padding: 0;
    list-style: none;
    font-size: 0.82rem;
    color: var(--md-sys-color-on-surface-variant);
  }
  .status li.warn {
    color: var(--md-sys-color-tertiary);
  }
  .empty {
    margin: 0;
    font-size: 0.86rem;
  }
  .scroll {
    overflow: auto;
    max-height: 60vh;
    border-radius: 8px;
    background: var(--md-sys-color-surface-container);
  }
  table {
    border-collapse: collapse;
    font-size: 0.8rem;
    min-width: 100%;
  }
  th,
  td {
    padding: 4px 10px;
    text-align: left;
    white-space: nowrap;
    border-bottom: 1px solid var(--md-sys-color-outline-variant);
  }
  th {
    position: sticky;
    top: 0;
    background: var(--md-sys-color-surface-container-high);
    font-weight: 700;
  }
  .num {
    text-align: right;
    font-variant-numeric: tabular-nums;
  }
  /* A redacted row, the blank group and the redacted group read quieter: absence, not data. */
  tr.quiet td {
    color: var(--md-sys-color-on-surface-variant);
    font-style: italic;
  }
</style>
