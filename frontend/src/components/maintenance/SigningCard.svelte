<script>
  // The case's signing on the Maintenance page: how this session opened the case, who has
  // worked on it, the keys it lists, and the two actions — sign this case from its next start, and
  // unlock the key of a case opened read-only.
  //
  // 🔒 A passphrase lives here only until its request is sent, and is cleared whatever the answer.
  import { onMount } from 'svelte';
  import { CASE_MODE, SIGNING_STATE, UI } from '../../lib/consts/index.js';
  import * as api from '../../lib/api/index.js';
  import { groupFingerprint, newPassphraseProblem } from '../../lib/session/session.js';
  import Button from '../material/Button.svelte';
  import TextField from '../material/TextField.svelte';
  import { session } from '../../stores/session.js';

  let view = $state(/** @type {any} */ (null));
  let error = $state('');
  let note = $state('');
  let busy = $state(false);
  let setup = $state(/** @type {''|'create'|'use'} */ (''));
  let path = $state('');
  let pass = $state('');
  let repeat = $state('');
  let label = $state('');
  let unlockPass = $state('');
  let copied = $state(false);

  async function load() {
    try {
      view = await api.signingStatus();
      session.refresh();
    } catch (e) {
      error = e?.message || String(e);
    }
  }
  onMount(load);

  const canRequest = $derived(
    !!view &&
      !view.read_only &&
      (view.state === SIGNING_STATE.UNSIGNED || view.state === SIGNING_STATE.UNSIGNED_AFTER_SIGNED),
  );
  const locked = $derived(!!view && view.mode === CASE_MODE.SIGNED && !!view.read_only);
  const passProblem = $derived(setup === 'create' ? newPassphraseProblem(pass, repeat) : pass ? '' : UI.SESSION_NEED_PASSPHRASE);

  async function pick(create) {
    error = '';
    note = '';
    try {
      const p = await api.pickKeyFile(create);
      if (!p) {
        note = UI.SIGNING_PICK_FAILED;
        return;
      }
      path = p;
      setup = create ? 'create' : 'use';
    } catch (e) {
      error = e?.message || String(e);
    }
  }

  async function request() {
    if (passProblem || busy) return;
    busy = true;
    error = '';
    const req = { path, create: setup === 'create', passphrase: pass, label: label.trim() };
    pass = '';
    repeat = '';
    try {
      const info = await api.requestSigning(req);
      note = UI.SIGNING_REQUESTED.replace('%s', groupFingerprint(info.fingerprint));
      setup = '';
      await load();
    } catch (e) {
      error = e?.message || String(e);
    } finally {
      busy = false;
    }
  }

  async function unlock() {
    if (!unlockPass || busy) return;
    busy = true;
    error = '';
    const p = unlockPass;
    unlockPass = '';
    try {
      await api.unlockKey(p);
      note = UI.SIGNING_UNLOCKED;
      await load();
    } catch (e) {
      error = e?.message || String(e);
    } finally {
      busy = false;
    }
  }

  async function copyFingerprint() {
    try {
      await navigator.clipboard.writeText(view.key.fingerprint);
      copied = true;
      setTimeout(() => (copied = false), 1500);
    } catch (_) {
      // The fingerprint stays on screen to copy by hand.
    }
  }
</script>

<section class="card signing">
  <h3>{UI.SIGNING_TITLE}</h3>
  <p class="hint">{UI.SIGNING_HINT}</p>
  {#if view}
    <p>
      <b>{UI.SIGNING_MODE_LABEL[view.mode] ?? view.mode}</b>
      {#if view.name}· {UI.SIGNING_ANALYST.replace('%s', view.name)}{/if}
    </p>
    {#if view.read_only}<p class="warn">{UI.SIGNING_READ_ONLY} {view.read_only}</p>{/if}

    {#if view.key}
      <p class="rlabel">{UI.SIGNING_KEY_FINGERPRINT}</p>
      <div class="fp">
        <code>{groupFingerprint(view.key.fingerprint)}</code>
        <Button variant="text" onclick={copyFingerprint}>{copied ? UI.SIGNING_COPIED : UI.SIGNING_COPY}</Button>
      </div>
    {/if}

    {#if locked}
      <TextField label={UI.SESSION_PASSPHRASE} type="password" bind:value={unlockPass} disabled={busy} />
      <div class="actions"><Button disabled={busy || !unlockPass} onclick={unlock}>{UI.SIGNING_UNLOCK}</Button></div>
    {/if}

    {#if canRequest}
      {#if !setup}
        <div class="actions">
          <Button variant="tonal" disabled={busy} onclick={() => pick(true)}>{UI.SIGNING_CREATE_KEY}</Button>
          <Button variant="outlined" disabled={busy} onclick={() => pick(false)}>{UI.SIGNING_USE_KEY}</Button>
        </div>
      {:else}
        <p class="mono">{path}</p>
        <TextField label={UI.SESSION_PASSPHRASE} type="password" bind:value={pass} disabled={busy} />
        {#if setup === 'create'}
          <TextField label={UI.SIGNING_PASS_REPEAT} type="password" bind:value={repeat} disabled={busy} />
          <TextField label={UI.SIGNING_KEY_LABEL} bind:value={label} disabled={busy} />
        {/if}
        {#if pass && passProblem}<p class="warn">{passProblem}</p>{/if}
        <div class="actions">
          <Button variant="text" disabled={busy} onclick={() => (setup = '')}>{UI.REDACT_CANCEL}</Button>
          <Button disabled={busy || !!passProblem} onclick={request}>{UI.SIGNING_REQUEST}</Button>
        </div>
      {/if}
    {/if}

    {#if view.analysts && view.analysts.length}
      <p class="rlabel">{UI.SIGNING_ANALYSTS}</p>
      <ul class="list">
        {#each view.analysts as a (a.actor_id)}<li>{a.name}</li>{/each}
      </ul>
    {/if}
    {#if view.keys && view.keys.length}
      <p class="rlabel">{UI.SIGNING_KEYS}</p>
      <ul class="list">
        {#each view.keys as k (k.key_id)}
          <li>
            <code>{groupFingerprint(k.fingerprint)}</code> — {k.analyst}
            {#if !k.bound}<span class="warn">({UI.SIGNING_UNBOUND})</span>{/if}
          </li>
        {/each}
      </ul>
    {/if}
    {#if view.audit_gaps && view.audit_gaps.length}
      <p class="warn">{UI.SIGNING_AUDIT_GAPS}</p>
      <ul class="list">
        {#each view.audit_gaps as g (g)}<li>{g}</li>{/each}
      </ul>
    {/if}
  {/if}
  {#if note}<p class="ok" role="status">{note}</p>{/if}
  {#if error}<p class="err" role="alert">{UI.SIGNING_FAILED} {error}</p>{/if}
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
  p {
    margin: 0;
  }
  .hint {
    color: var(--md-sys-color-on-surface-variant);
    font-size: 0.85rem;
  }
  .rlabel {
    font-size: 0.78rem;
    font-weight: 700;
  }
  .warn {
    color: var(--md-sys-color-tertiary);
  }
  .err {
    color: var(--md-sys-color-error);
  }
  .ok {
    color: var(--md-sys-color-primary);
  }
  .fp {
    display: flex;
    align-items: center;
    gap: 8px;
    flex-wrap: wrap;
  }
  code,
  .mono {
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
