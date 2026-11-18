<script>
  // Redacting one event.
  //
  // Three steps, each its own state: the preview (what goes, what stays, what rohy cannot reach),
  // the reason, and the outcome step by step. Everything belongs to the event it was asked about;
  // moving to another event drops it all.
  //
  // 🔒 The reason is never prefilled and never carried from one event to the next: it is kept in
  // plain text, permanently, and it has to be what the analyst meant for this event.
  import { UI } from '../../lib/consts/index.js';
  import * as api from '../../lib/api/index.js';
  import { previewView, outcomeView, reasonProblem, redactionFailure, redactedLine } from '../../lib/maintenance/redaction.js';
  import { maintenance } from '../../stores/maintenance.js';
  import Button from '../material/Button.svelte';

  /** @type {{event: any, detail?: any}} */
  let { event, detail = null } = $props();

  const redacted = $derived(!!(event && event.redacted) || !!(detail && detail.redacted));

  let open = $state(false);
  let loading = $state(false);
  let preview = $state(/** @type {import('../../lib/maintenance/redaction.js').PreviewView|null} */ (null));
  let reason = $state('');
  let touched = $state(false);
  let busy = $state(false);
  let outcome = $state(/** @type {import('../../lib/maintenance/redaction.js').OutcomeView|null} */ (null));
  let shownFor = $state(/** @type {number|null} */ (null));
  let gen = 0;

  const reset = () => {
    gen++;
    open = false;
    loading = false;
    preview = null;
    reason = '';
    touched = false;
    busy = false;
    outcome = null;
  };
  $effect(() => {
    const id = event ? event.id : null;
    if (shownFor === id) return;
    shownFor = id;
    reset();
  });

  const problem = $derived(reasonProblem(reason));
  const canConfirm = $derived(!!preview && preview.ok && preview.blocked === '' && problem === '' && !busy);

  async function openPreview() {
    const id = event.id;
    const g = ++gen;
    open = true;
    loading = true;
    outcome = null;
    try {
      const p = await api.previewRedaction(id);
      if (g === gen) preview = previewView(p);
    } catch (e) {
      if (g === gen) outcome = redactionFailure(e);
    } finally {
      if (g === gen) loading = false;
    }
  }

  async function confirm() {
    touched = true;
    if (!canConfirm) return;
    const id = event.id;
    const g = gen;
    busy = true;
    const out = await maintenance.redact(id, reason);
    if (g !== gen) return;
    busy = false;
    outcome = out;
    if (out.ok) {
      preview = null;
      reason = '';
    }
  }
</script>

<section class="redact">
  <h3>{UI.REDACT_TITLE}</h3>
  {#if redacted}
    <p class="badge" role="status">{redactedLine(detail)}</p>
  {:else if !open}
    <p class="muted">{UI.REDACT_HINT}</p>
    <div class="actions">
      <Button variant="outlined" onclick={openPreview}>{UI.REDACT_OPEN}</Button>
    </div>
  {:else}
    {#if loading}
      <p class="muted" role="status">{UI.REDACT_LOADING}</p>
    {:else if preview}
      {#if preview.blocked}
        <p class="msg damage" role="status">{preview.blocked}</p>
      {/if}
      <p class="rlabel">{UI.REDACT_GOES}</p>
      <ul class="list">
        {#each preview.goes as g (g)}<li>{g}</li>{/each}
      </ul>
      <p class="rlabel">{UI.REDACT_STAYS}</p>
      <ul class="list">
        {#each preview.stays as s (s)}<li>{s}</li>{/each}
      </ul>
      {#if preview.findings.length}
        <ul class="list notes">
          {#each preview.findings as f (f.key)}
            <li>{f.note || f.tags.join(', ') || '—'}</li>
          {/each}
        </ul>
      {/if}
      <p class="rlabel">{UI.REDACT_UNREACHED}</p>
      <ul class="list">
        {#each preview.notReached as n (n)}<li>{n}</li>{/each}
      </ul>

      <label class="reason">
        <span class="rlabel">{UI.REDACT_REASON_LABEL}</span>
        <textarea bind:value={reason} oninput={() => (touched = true)} rows="3" disabled={busy}></textarea>
      </label>
      <p class="muted">{UI.REDACT_REASON_HINT}</p>
      {#if touched && problem}
        <p class="msg warn">{problem}</p>
      {/if}
      <div class="actions">
        <Button variant="text" disabled={busy} onclick={reset}>{UI.REDACT_CANCEL}</Button>
        <Button disabled={!canConfirm} onclick={confirm}>{UI.REDACT_CONFIRM}</Button>
      </div>
      {#if busy}
        <p class="muted" role="status">{UI.REDACT_WORKING}</p>
      {/if}
    {/if}

    {#if outcome && !outcome.ok}
      <p class="msg damage" role="status">{UI.REDACT_FAILED} {outcome.error}</p>
    {:else if outcome}
      <p class={outcome.done ? 'msg' : 'msg warn'} role="status">{outcome.message}</p>
      {#if outcome.error}
        <p class="msg damage">{outcome.error}</p>
      {/if}
      <ul class="steps">
        {#each outcome.steps as s (s.name)}
          <li class={'step ' + s.tone}>
            <span class="sname">{s.label}</span>
            <span class="sstate">{s.stateLabel}</span>
            {#if s.error}<p class="smsg damage">{s.error}</p>{/if}
            {#each s.notes as n (n)}<p class="smsg">{n}</p>{/each}
          </li>
        {/each}
      </ul>
    {/if}
  {/if}
</section>

<style>
  h3 {
    margin: 0 0 var(--space-3);
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-on-surface-muted);
  }
  .muted {
    color: var(--color-on-surface-muted);
    font-size: 0.85rem;
    margin: var(--space-2) 0 0;
  }
  .badge {
    margin: 0;
    font-size: 0.85rem;
    font-weight: 700;
    color: var(--color-warning);
  }
  .rlabel {
    display: block;
    margin: var(--space-3) 0 var(--space-2);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
  }
  .list {
    margin: 0;
    padding-left: var(--space-5);
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
  .notes {
    font-style: italic;
  }
  .reason {
    display: block;
  }
  textarea {
    width: 100%;
    box-sizing: border-box;
    resize: vertical;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-3);
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.85rem;
  }
  textarea:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .actions {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-3);
    margin-top: var(--space-3);
  }
  .msg {
    margin: var(--space-3) 0 0;
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
  .msg.warn {
    color: var(--color-warning);
  }
  .msg.damage,
  .smsg.damage {
    color: var(--color-error);
  }
  .steps {
    list-style: none;
    margin: var(--space-3) 0 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .step {
    border-left: 3px solid var(--color-outline);
    padding-left: var(--space-3);
  }
  .step.done {
    border-left-color: var(--color-primary);
  }
  .step.failed {
    border-left-color: var(--color-error);
  }
  .step.not_reached {
    border-left-color: var(--color-warning);
  }
  .sname {
    font-weight: 700;
    font-size: 0.85rem;
  }
  .sstate {
    margin-left: var(--space-2);
    font-size: 0.78rem;
    color: var(--color-on-surface-muted);
  }
  .smsg {
    margin: var(--space-1) 0 0;
    font-size: 0.85rem;
    color: var(--color-on-surface);
  }
</style>
