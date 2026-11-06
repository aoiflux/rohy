<script>
  // Checkpoints on the Maintenance page (item 42): publish one, record what came back, and check the
  // case against a digest you hold.
  //
  // 🔒 rohy ships no anchor. This card records a checkpoint and hands over its digest; the analyst
  // publishes it. A receipt is the case's own record, never a witness, and no state here is a tick.
  // 🔒 The digest to check is never prefilled from the case: a check against the case's own copy
  // proves nothing.
  import { onMount } from 'svelte';
  import { UI } from '../../lib/consts/index.js';
  import { anchorCheckView, latest, receiptTime } from '../../lib/maintenance/anchor.js';
  import { ranAt } from '../../lib/maintenance/report.js';
  import * as api from '../../lib/api/index.js';
  import { maintenance } from '../../stores/maintenance.js';
  import Button from '../material/Button.svelte';
  import TextField from '../material/TextField.svelte';

  let { busy = false, noWrites = false } = $props();

  const list = $derived($maintenance.checkpoints);
  const published = $derived($maintenance.published);
  const last = $derived(latest(list));

  let confirming = $state(false);
  let copied = $state(false);
  let ref = $state('');
  let at = $state('');
  let receiptError = $state('');
  let receiptNote = $state('');
  let held = $state('');
  let checking = $state(false);
  let checkError = $state('');
  let answer = $state(/** @type {any} */ (null));

  onMount(() => maintenance.loadCheckpoints());

  async function publish() {
    confirming = false;
    await maintenance.publishCheckpoint();
  }

  async function copyDigest() {
    if (!last?.digest) return;
    try {
      await navigator.clipboard.writeText(last.digest);
      copied = true;
      setTimeout(() => (copied = false), 2000);
    } catch {
      // The digest is on screen and selectable; a refused clipboard is not worth an error.
    }
  }

  async function record() {
    receiptError = '';
    receiptNote = '';
    const when = receiptTime(at);
    if (when === null) {
      receiptError = UI.ANCHOR_RECEIPT_BAD_TIME;
      return;
    }
    try {
      const out = await maintenance.recordReceipt(last.seq, ref, when);
      ref = '';
      at = '';
      receiptNote = out?.audit_gap ? `${UI.ANCHOR_RECEIPT_SAVED} ${out.audit_gap}` : UI.ANCHOR_RECEIPT_SAVED;
    } catch (e) {
      receiptError = e?.message || String(e);
    }
  }

  async function check() {
    checking = true;
    checkError = '';
    answer = null;
    try {
      answer = anchorCheckView(await api.checkCheckpoint(held));
    } catch (e) {
      checkError = e?.message || String(e);
    } finally {
      checking = false;
    }
  }
</script>

<section class="card">
  <h3>{UI.ANCHOR_TITLE}</h3>
  <p class="hint">{UI.ANCHOR_INTRO}</p>

  {#if list?.unavailable}
    <p class="hint">{UI.ANCHOR_UNAVAILABLE}</p>
  {:else if list}
    {#if list.chain_error}
      <p class="err">{UI.ANCHOR_CHAIN_BROKEN} <code>{list.chain_error}</code></p>
    {/if}
    {#if list.receipts_error}
      <p class="warn">{UI.ANCHOR_RECEIPTS_UNREAD} <code>{list.receipts_error}</code></p>
    {/if}
    {#if noWrites}<p class="warn">{UI.ANCHOR_READ_ONLY}</p>{/if}

    {#if published}
      <p class="hint" role="status">
        {UI.ANCHOR_PUBLISHED(published.statement?.checkpoint?.seq)}
        {#if published.sealed}{UI.ANCHOR_SEALED_FIRST}{/if}
      </p>
      {#if published.kept_aside}<p class="warn">{UI.ANCHOR_KEPT_ASIDE} <code>{published.kept_aside}</code></p>{/if}
      {#if published.gaps?.length}
        <p class="warn">{UI.ANCHOR_GAPS}</p>
        <ul class="list">{#each published.gaps as g (g)}<li>{g}</li>{/each}</ul>
      {/if}
    {/if}

    {#if !last}
      <p class="warn">{UI.ANCHOR_NONE}</p>
    {:else}
      <p class="rlabel">{UI.ANCHOR_LAST(last.seq, ranAt(last.captured_at))}</p>
      <div class="fp">
        <span class="hint">{UI.ANCHOR_DIGEST_LABEL}</span>
        <!-- Selectable text: the copy button can be refused, the value must still be reachable. -->
        <code>{last.digest}</code>
        <Button variant="text" onclick={copyDigest}>{copied ? UI.ANCHOR_COPIED : UI.ANCHOR_COPY}</Button>
        <Button variant="text" onclick={() => maintenance.saveStatement(last.seq)}>{UI.ANCHOR_SAVE_AGAIN}</Button>
      </div>
      {#if last.receipts?.length}
        <p class="hint">{UI.ANCHOR_RECEIPTS}</p>
        <ul class="list">
          {#each last.receipts as r (r.ref)}
            <li>{r.ref}{#if r.published_at}{UI.ANCHOR_RECEIPT_AT(ranAt(r.published_at))}{/if}</li>
          {/each}
        </ul>
        <p class="hint">{UI.ANCHOR_RECEIPT_CAVEAT}</p>
      {:else}
        <p class="warn">{UI.ANCHOR_NO_RECEIPT}</p>
      {/if}
    {/if}

    {#if confirming}
      <p class="hint">{UI.ANCHOR_PUBLISH_CONFIRM}</p>
      <div class="actions">
        <Button variant="tonal" onclick={publish} disabled={busy || noWrites}>{UI.ANCHOR_PUBLISH_GO}</Button>
        <Button variant="text" onclick={() => (confirming = false)}>{UI.ANCHOR_CANCEL}</Button>
      </div>
    {:else}
      <div class="actions">
        <Button variant="tonal" onclick={() => (confirming = true)} disabled={busy || noWrites}>
          {UI.ANCHOR_PUBLISH}
        </Button>
      </div>
    {/if}

    {#if last && !noWrites}
      <h4>{UI.ANCHOR_RECEIPT_TITLE}</h4>
      <TextField label={UI.ANCHOR_RECEIPT_REF} placeholder={UI.ANCHOR_RECEIPT_REF_HINT} bind:value={ref} disabled={busy} />
      <TextField label={UI.ANCHOR_RECEIPT_AT_LABEL} type="datetime-local" bind:value={at} disabled={busy} />
      <div class="actions">
        <Button onclick={record} disabled={busy || !ref.trim()}>{UI.ANCHOR_RECEIPT_SAVE}</Button>
      </div>
      {#if receiptNote}<p class="hint" role="status">{receiptNote}</p>{/if}
      {#if receiptError}<p class="err" role="alert">{receiptError}</p>{/if}
    {/if}
  {/if}

  <h4>{UI.ANCHOR_CHECK_TITLE}</h4>
  <p class="hint">{UI.ANCHOR_CHECK_HINT}</p>
  <TextField placeholder={UI.ANCHOR_CHECK_PLACEHOLDER} bind:value={held} disabled={checking} />
  <div class="actions">
    <Button variant="tonal" onclick={check} disabled={checking || !held.trim()}>{UI.ANCHOR_CHECK_GO}</Button>
  </div>
  {#if checkError}<p class="err" role="alert">{checkError}</p>{/if}
  {#if answer}
    <p class={answer.tone}>{answer.message}</p>
    {#if answer.layers.length}
      <ul class="list">
        {#each answer.layers as l (l.key)}
          <li>
            <span class="rlabel">{l.label}</span> — <span class={l.tone}>{l.stateLabel}</span>: {l.message}
            {#each l.notes as n (n)}<br /><span class="hint">{n}</span>{/each}
          </li>
        {/each}
      </ul>
    {/if}
    {#each answer.notes as n (n)}<p class="hint">{n}</p>{/each}
    {#if answer.establishes.length}
      <p class="rlabel">{UI.ANCHOR_CHECK_ESTABLISHES}</p>
      <ul class="list">{#each answer.establishes as e (e)}<li>{e}</li>{/each}</ul>
    {/if}
    <p class="rlabel">{UI.ANCHOR_CHECK_NOT}</p>
    <ul class="list">{#each answer.doesNot as d (d)}<li>{d}</li>{/each}</ul>
  {/if}
</section>

<style>
  .card {
    display: flex;
    flex-direction: column;
    gap: 10px;
    max-width: 80ch;
    padding: 14px 16px;
    border-radius: 10px;
    background: var(--md-sys-color-surface-container);
  }
  h3,
  h4,
  p {
    margin: 0;
  }
  h4 {
    font-size: 0.9rem;
    margin-top: 6px;
  }
  .hint,
  .info {
    color: var(--md-sys-color-on-surface-variant);
    font-size: 0.85rem;
  }
  .rlabel {
    font-size: 0.78rem;
    font-weight: 700;
  }
  .warn,
  .not_established {
    color: var(--md-sys-color-tertiary);
  }
  .err,
  .broken {
    color: var(--md-sys-color-error);
  }
  .established {
    color: var(--md-sys-color-on-surface);
  }
  .fp {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  code {
    font-family: var(--font-mono);
    font-size: 0.82rem;
    word-break: break-all;
  }
  .list {
    margin: 0;
    padding-left: 1.2em;
  }
  .actions {
    display: flex;
    gap: 8px;
    flex-wrap: wrap;
  }
</style>
