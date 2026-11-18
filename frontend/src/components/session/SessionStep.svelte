<script>
  // The session step: what the case needs from the analyst before it opens.
  //
  // It asks only what the backend says the case needs — a name on a first run; for a signed case,
  // how to open it. 🔒 The passphrase lives in this component only until the request is sent, and
  // is cleared whatever the answer.
  import { onMount } from 'svelte';
  import { SESSION_CHOICE, SESSION_NEED, UI } from '../../lib/consts/index.js';
  import * as api from '../../lib/api/index.js';
  import { defaultChoice, evidenceLine, formProblem, groupFingerprint, needs, sessionRequest } from '../../lib/session/session.js';
  import Button from '../material/Button.svelte';
  import TextField from '../material/TextField.svelte';
  import Checkbox from '../material/Checkbox.svelte';

  let view = $state(/** @type {any} */ (null));
  let error = $state('');
  let busy = $state(false);
  let touched = $state(false);
  let name = $state('');
  let choice = $state('');
  let passphrase = $state('');
  let reason = $state('');
  let acceptLog = $state(false);
  let keepProfileAside = $state(false);

  async function load() {
    try {
      view = await api.sessionStatus();
      if (!choice || !(view.choices || []).includes(choice)) choice = defaultChoice(view);
      if (!name) name = view.name || view.default_name || '';
    } catch (e) {
      error = e?.message || String(e);
    }
  }
  onMount(load);

  const form = $derived({ name, choice, passphrase, reason, acceptLog, keepProfileAside });
  const need = $derived(view ? needs(view, choice) : { name: false, passphrase: false, reason: false, acceptLog: false });
  const problem = $derived(view ? formProblem(view, form) : '');

  async function submit() {
    touched = true;
    if (!view || problem || busy) return;
    busy = true;
    error = '';
    const req = sessionRequest(view, form);
    passphrase = '';
    try {
      await api.beginSession(req);
      // The backend goes on initializing; the splash follows its stages from here.
    } catch (e) {
      error = e?.message || String(e);
      busy = false;
      await load();
    }
  }
</script>

<section class="session" aria-labelledby="session-title">
  <h2 id="session-title">{UI.SESSION_TITLE}</h2>
  {#if !view}
    {#if error}<p class="msg damage" role="alert">{error}</p>{/if}
  {:else}
    {#if view.profile_error}
      <p class="msg damage">{UI.SESSION_PROFILE_DAMAGED} {view.profile_error}</p>
      <Checkbox label={UI.SESSION_PROFILE_ASIDE} bind:checked={keepProfileAside} />
    {/if}

    {#if view.need === SESSION_NEED.IDENTITY || view.name}
      <TextField label={UI.SESSION_NAME_LABEL} bind:value={name} disabled={busy} />
      <p class="muted">{UI.SESSION_NAME_HINT}</p>
    {/if}

    <p class="state">
      {UI.SESSION_STATE} <strong>{UI.SESSION_STATE_LABEL[view.state ?? ''] ?? view.state}</strong>
    </p>
    {#if evidenceLine(view)}<p class="muted">{evidenceLine(view)}</p>{/if}
    {#if view.problem}<p class="msg warn" role="status">{view.problem}</p>{/if}
    {#if view.open_error}<p class="msg damage" role="status">{view.open_error}</p>{/if}
    {#if view.pending_redactions}
      <p class="msg warn">{UI.SESSION_PENDING.replace('%d', view.pending_redactions)}</p>
    {/if}
    {#if view.key}
      <p class="muted">{UI.SESSION_KEY.replace('%s', groupFingerprint(view.key.fingerprint))}</p>
    {:else if view.key_error}
      <p class="msg warn">{view.key_error}</p>
    {/if}

    {#if (view.choices || []).length > 1}
      <fieldset class="choices" disabled={busy}>
        {#each view.choices as c (c)}
          <label class="choice">
            <input type="radio" name="session-choice" value={c} bind:group={choice} />
            <span>
              <span class="clabel">{UI.SESSION_CHOICE_LABEL[c] ?? c}</span>
              {#if UI.SESSION_CHOICE_HINT[c]}<span class="muted">{UI.SESSION_CHOICE_HINT[c]}</span>{/if}
            </span>
          </label>
        {/each}
      </fieldset>
    {/if}

    {#if need.passphrase}
      <TextField label={UI.SESSION_PASSPHRASE} type="password" bind:value={passphrase} disabled={busy} />
    {/if}
    {#if need.acceptLog}
      <Checkbox label={UI.SESSION_ACCEPT_LOG} bind:checked={acceptLog} disabled={busy} />
    {/if}
    {#if need.reason || (need.acceptLog && acceptLog)}
      <label class="reason">
        <span class="rlabel">{need.reason ? UI.SESSION_REASON : UI.SESSION_ACCEPT_REASON}</span>
        <textarea bind:value={reason} rows="3" disabled={busy}></textarea>
      </label>
      <p class="muted">{UI.SESSION_REASON_WARNING}</p>
    {/if}

    {#if touched && problem}<p class="msg warn">{problem}</p>{/if}
    {#if error}<p class="msg damage" role="alert">{UI.SESSION_FAILED} {error}</p>{/if}
    <div class="actions">
      <Button disabled={busy || (touched && !!problem)} onclick={submit}>
        {choice && choice !== SESSION_CHOICE.START ? (UI.SESSION_CHOICE_LABEL[choice] ?? UI.SESSION_SUBMIT) : UI.SESSION_SUBMIT}
      </Button>
    </div>
    {#if busy}<p class="muted" role="status">{UI.SESSION_WORKING}</p>{/if}
  {/if}
</section>

<style>
  .session {
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
    max-width: 34rem;
    width: 100%;
    text-align: left;
  }
  h2 {
    margin: 0;
    font-size: 1.05rem;
  }
  .muted {
    color: var(--color-on-surface-muted);
    font-size: 0.82rem;
    margin: 0;
  }
  .state {
    margin: 0;
  }
  .msg {
    margin: 0;
    font-size: 0.85rem;
  }
  .warn {
    color: var(--color-warning);
  }
  .damage {
    color: var(--color-error);
  }
  .choices {
    border: none;
    margin: 0;
    padding: 0;
    display: flex;
    flex-direction: column;
    gap: var(--space-2);
  }
  .choice {
    display: flex;
    gap: var(--space-2);
    align-items: flex-start;
    cursor: pointer;
  }
  .choice > span {
    display: flex;
    flex-direction: column;
  }
  .clabel {
    font-weight: 700;
  }
  .reason {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .rlabel {
    font-size: 0.78rem;
    font-weight: 700;
  }
  textarea {
    font-family: var(--font-sans);
    resize: vertical;
  }
  .actions {
    display: flex;
    justify-content: flex-end;
  }
</style>
