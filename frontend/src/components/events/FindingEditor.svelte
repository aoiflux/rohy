<script>
  // Analyst findings editor (P25) — the flag, tags, and note for one event, shown inside the
  // event detail drawer.
  //
  // This is the only surface in rohy where the user AUTHORS data rather than reading what was
  // derived, so it is deliberately marked as theirs: its own panel, its own accent, and a
  // line stating that what is written here sits beside the evidence and never inside it.
  //
  // Saving is automatic. An analyst writing a note mid-investigation should not have to
  // remember a Save button, and losing a paragraph because they clicked away would be far
  // worse than an extra write — so edits debounce while typing and flush on blur.
  import { UI, FINDINGS } from '../../lib/consts/index.js';
  import { findings } from '../../stores/findings.js';

  let { event = null } = $props();

  // The event's content identity — what the finding is keyed on.
  const key = $derived(event ? event.hash_normalized : '');
  const stored = $derived(key ? $findings.byKey[key] || null : null);

  // A short human summary of the event, recorded with the finding so an annotation whose
  // event later leaves the case still says what was marked.
  const descriptor = $derived(
    event ? [event.event_id, event.provider, event.timestamp || UI.UNDATED_TIMESTAMP].filter(Boolean).join(' · ') : '',
  );

  // Local edit state for the note. It is seeded from the store and re-seeded when the
  // selected event changes, but is NOT bound straight through: typing must not round-trip
  // to disk on every keystroke.
  let note = $state('');
  let noteKey = $state('');
  let tagDraft = $state('');
  let saveTimer = null;

  // Re-seed when the drawer switches events. Keyed on the event hash so an in-flight edit is
  // not clobbered by the store refreshing the same event's finding underneath it.
  $effect(() => {
    if (key !== noteKey) {
      noteKey = key;
      note = (stored && stored.note) || '';
    }
  });

  const tags = $derived(stored && stored.tags ? stored.tags : []);
  const tooLong = $derived(note.length > FINDINGS.NOTE_MAX);

  // Tags already used elsewhere in the case, minus the ones on this event — so the analyst
  // reuses a vocabulary instead of inventing a new spelling each time.
  const suggestions = $derived(
    $findings.tags
      .filter((t) => !tags.includes(t.tag))
      .slice(0, FINDINGS.TAG_SUGGESTIONS),
  );

  function flushNote() {
    if (saveTimer) {
      clearTimeout(saveTimer);
      saveTimer = null;
    }
    if (!key || tooLong) return;
    if (note === ((stored && stored.note) || '')) return; // nothing actually changed
    findings.setNote(key, note, descriptor);
  }

  function onNoteInput() {
    if (saveTimer) clearTimeout(saveTimer);
    saveTimer = setTimeout(flushNote, FINDINGS.SAVE_DEBOUNCE_MS);
  }

  function addTag(raw) {
    const t = String(raw || '').trim();
    if (!key || !t) return;
    tagDraft = '';
    if (tags.some((x) => x.toLowerCase() === t.toLowerCase())) return; // already present
    findings.setTags(key, [...tags, t], descriptor);
  }

  function removeTag(t) {
    findings.setTags(
      key,
      tags.filter((x) => x !== t),
      descriptor,
    );
  }

  function onTagKey(e) {
    if (e.key === 'Enter') {
      e.preventDefault();
      addTag(tagDraft);
    } else if (e.key === 'Backspace' && tagDraft === '' && tags.length) {
      // Backspace on an empty box removes the last tag — the standard chip-input idiom.
      removeTag(tags[tags.length - 1]);
    }
  }
</script>

{#if event}
  <section class="finding">
    <div class="head">
      <h3>{UI.FINDING_SECTION}</h3>
      <span class="state">
        {#if $findings.saving}{UI.FINDING_SAVING}{:else if stored}{UI.FINDING_SAVED}{/if}
      </span>
    </div>
    <p class="hint">{UI.FINDING_HINT}</p>

    <button
      class="flag"
      class:on={stored && stored.flagged}
      type="button"
      aria-pressed={!!(stored && stored.flagged)}
      onclick={() => findings.toggleFlag(key, descriptor)}
    >
      <span class="mark">{stored && stored.flagged ? '★' : '☆'}</span>
      {stored && stored.flagged ? UI.FINDING_FLAGGED : UI.FINDING_FLAG}
    </button>

    <div class="block">
      <span class="blabel">{UI.FINDING_TAGS}</span>
      <div class="tags">
        {#each tags as t (t)}
          <span class="tag">
            {t}
            <button type="button" aria-label={UI.FINDING_TAG_REMOVE} onclick={() => removeTag(t)}>×</button>
          </span>
        {/each}
        <input
          class="taginput"
          bind:value={tagDraft}
          onkeydown={onTagKey}
          onblur={() => addTag(tagDraft)}
          placeholder={UI.FINDING_TAG_PLACEHOLDER}
          maxlength={FINDINGS.TAG_MAX}
          disabled={tags.length >= FINDINGS.TAGS_MAX}
        />
      </div>
      {#if suggestions.length}
        <div class="suggest" aria-label={UI.FINDING_TAG_EXISTING}>
          {#each suggestions as s (s.tag)}
            <button type="button" class="chip" onclick={() => addTag(s.tag)}>{s.tag} · {s.count}</button>
          {/each}
        </div>
      {/if}
    </div>

    <div class="block">
      <span class="blabel">{UI.FINDING_NOTE}</span>
      <textarea
        bind:value={note}
        oninput={onNoteInput}
        onblur={flushNote}
        placeholder={UI.FINDING_NOTE_PLACEHOLDER}
        rows="4"
      ></textarea>
      {#if tooLong}
        <p class="warn">{UI.FINDING_NOTE_TOO_LONG}</p>
      {/if}
    </div>

    {#if $findings.error}
      <p class="warn">{UI.FINDING_SAVE_FAILED}: {$findings.error}</p>
    {/if}
    {#if stored && stored.updated_at}
      <p class="stamp">{UI.FINDING_UPDATED}: {new Date(stored.updated_at).toLocaleString()}</p>
    {/if}
  </section>
{/if}

<style>
  /* Analyst-authored content is visually separated from the derived metadata above it: its
     own surface and a left accent, so a reader can never mistake a note for evidence. */
  .finding {
    border: 1px solid var(--color-accent);
    border-left-width: 3px;
    border-radius: var(--radius-md);
    background: color-mix(in srgb, var(--color-accent) 6%, transparent);
    padding: var(--space-4);
  }
  .head {
    display: flex;
    align-items: baseline;
    justify-content: space-between;
    gap: var(--space-3);
  }
  h3 {
    margin: 0;
    font-size: 0.8rem;
    text-transform: uppercase;
    letter-spacing: 0.05em;
    color: var(--color-accent);
  }
  .state {
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
  }
  .hint {
    margin: var(--space-2) 0 var(--space-3);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    color: var(--color-on-surface-muted);
  }
  .flag {
    display: inline-flex;
    align-items: center;
    gap: var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.82rem;
    color: var(--color-on-surface);
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: 999px;
    padding: var(--space-2) var(--space-4);
    cursor: pointer;
  }
  .flag:hover {
    border-color: var(--color-accent);
  }
  .flag.on {
    color: var(--color-accent);
    border-color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, transparent);
    font-weight: 700;
  }
  .mark {
    font-size: 1rem;
    line-height: 1;
  }
  .block {
    margin-top: var(--space-4);
  }
  .blabel {
    display: block;
    font-family: var(--font-sans);
    font-size: 0.75rem;
    color: var(--color-on-surface-muted);
    margin-bottom: var(--space-2);
  }
  .tags {
    display: flex;
    flex-wrap: wrap;
    align-items: center;
    gap: var(--space-2);
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    padding: var(--space-2);
  }
  .tag {
    display: inline-flex;
    align-items: center;
    gap: 4px;
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-accent);
    background: color-mix(in srgb, var(--color-accent) 14%, transparent);
    border: 1px solid var(--color-accent);
    border-radius: 999px;
    padding: 1px var(--space-2) 1px var(--space-3);
  }
  .tag button {
    background: transparent;
    border: none;
    color: inherit;
    cursor: pointer;
    font-size: 0.9rem;
    line-height: 1;
    padding: 0 2px;
  }
  .taginput {
    flex: 1;
    min-width: 140px;
    background: transparent;
    border: none;
    outline: none;
    color: var(--color-on-surface);
    font-family: var(--font-sans);
    font-size: 0.8rem;
    padding: var(--space-1, 2px);
  }
  .suggest {
    display: flex;
    flex-wrap: wrap;
    gap: var(--space-2);
    margin-top: var(--space-2);
  }
  .chip {
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: 999px;
    padding: 1px var(--space-3);
    cursor: pointer;
  }
  .chip:hover {
    color: var(--color-accent);
    border-color: var(--color-accent);
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
    font-size: 0.82rem;
    line-height: 1.5;
  }
  textarea:focus {
    outline: none;
    border-color: var(--color-accent);
  }
  .warn {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.75rem;
    color: var(--color-error, var(--color-accent));
  }
  .stamp {
    margin: var(--space-3) 0 0;
    font-family: var(--font-sans);
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
  }
</style>
