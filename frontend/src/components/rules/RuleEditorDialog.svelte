<script>
  // The rule editor shell (P26): mode switch, the two panels, the shared error rail, and the
  // save path.
  //
  // Both modes edit ONE document — the JSON text — so switching is a change of view, not a
  // handoff between two copies of the rule that have to be reconciled. Guided → raw always
  // works; raw → guided is refused when the text does not parse, because a form seeded from
  // a partial read of a broken document would silently discard whatever it could not
  // understand.
  import { ruleEditor } from '../../stores/ruleEditor.js';
  import { rules } from '../../stores/rules.js';
  import { snackbar } from '../../stores/snackbar.js';
  import { EDITOR_MODES, RULE_PROBLEMS, UI } from '../../lib/consts/index.js';
  import { projectToForm, isFormEditable, parseText } from '../../lib/rules/document.js';
  import { eventIdsFromRules } from '../../lib/rules/complete.js';
  import { download } from '../../lib/export.js';
  import { formToFilter } from '../../lib/filter.js';
  import { prefs } from '../../stores/prefs.js';

  import Dialog from '../material/Dialog.svelte';
  import Button from '../material/Button.svelte';
  import RawEditorPanel from './RawEditorPanel.svelte';
  import GuidedEditorPanel from './GuidedEditorPanel.svelte';
  import RulePreviewPane from './RulePreviewPane.svelte';
  import RuleDiffView from './RuleDiffView.svelte';
  import RuleFieldDiff from './RuleFieldDiff.svelte';
  import TestbenchPanel from './TestbenchPanel.svelte';

  /** Called with the save result, so the rules view can offer to run the rule it just got. */
  let { onsaved = undefined } = $props();

  let rawPanel = $state(null);
  let fileInput = $state(null);
  let sidePane = $state('preview'); // 'preview' | 'diff' | 'test'
  let confirmingDiscard = $state(false);

  const problems = $derived(ruleEditor.problemsOf($ruleEditor));
  const errorLines = $derived(problems.errors.map((p) => p.line).filter((n) => n > 0));
  const form = $derived(projectToForm($ruleEditor.doc, $ruleEditor.schema));
  const canGuided = $derived(isFormEditable($ruleEditor.doc));
  const rename = $derived($ruleEditor.open ? ruleEditor.renameTarget() : null);

  // Both sides parsed, for the field diff. Either may be null — an unparseable buffer is the
  // normal state mid-edit — and the field view says so rather than showing an empty table.
  const beforeRule = $derived(parseText($ruleEditor.original).value);
  const afterRule = $derived($ruleEditor.doc.value ?? null);

  // Completion candidates. The event IDs the built-in library uses are a decent list and
  // need no query; grounding them in the case's own data is a later refinement.
  const eventIds = $derived(eventIdsFromRules($rules.list));

  const title = $derived(
    $ruleEditor.duplicatingFrom
      ? `${UI.RULE_EDITOR_TITLE_DUPLICATE}: ${$ruleEditor.duplicatingFrom}`
      : $ruleEditor.editingId
        ? `${UI.RULE_EDITOR_TITLE_EDIT}: ${$ruleEditor.editingId}`
        : UI.RULE_EDITOR_TITLE_NEW,
  );

  function switchMode(mode) {
    if (ruleEditor.setMode(mode)) return;
    // The only way this fails is unparseable text, and the parse error is the useful thing
    // to say — followed by putting the caret on it.
    const syntax = problems.errors.find((p) => p.code === RULE_PROBLEMS.SYNTAX);
    snackbar.error(`${UI.RULE_EDITOR_CANNOT_GUIDE} ${syntax ? syntax.message : ''}`.trim());
    if (syntax?.line) rawPanel?.focusLine(syntax.line);
  }

  function goToProblem(problem) {
    if ($ruleEditor.mode === EDITOR_MODES.RAW && problem.line > 0) {
      rawPanel?.focusLine(problem.line);
      return;
    }
    // In guided mode the control is what to reach for, not a line number.
    if (problem.field) {
      document.getElementById(`field-${problem.field}`)?.focus();
    }
  }

  function requestClose() {
    if (ruleEditor.dirty()) {
      confirmingDiscard = true;
      return;
    }
    ruleEditor.close();
  }

  async function doSave(andRun) {
    const result = await ruleEditor.save();
    if (!result) {
      snackbar.error($ruleEditor.error || UI.RULE_EDITOR_SAVE_REFUSED);
      return;
    }
    const note = result.created
      ? UI.RULE_EDITOR_SAVED_NEW
      : result.renamed
        ? `${UI.RULE_EDITOR_SAVED_RENAMED} ${result.previous_id} → ${result.rule.id}`
        : UI.RULE_EDITOR_SAVED;
    snackbar.success(`${note}: ${result.rule.name}`);
    ruleEditor.close();
    onsaved?.(result, andRun);
  }

  function exportRule() {
    const name = ($ruleEditor.doc.value?.name || 'rule').toLowerCase().replace(/[^a-z0-9]+/g, '-');
    download(`${name}.json`, 'application/json', $ruleEditor.doc.text);
  }

  /**
   * Import reads into the BUFFER, which is why it uses a file input rather than the backend's
   * import dialog: that one copies a file into the rules directory, whereas here the point is
   * to open somebody's rule, look at it, and decide.
   */
  function importRule(event) {
    const file = event.currentTarget.files?.[0];
    if (!file) return;
    const reader = new FileReader();
    reader.onload = () => ruleEditor.setText(String(reader.result ?? ''), { coalesce: false });
    reader.onerror = () => snackbar.error(UI.RULE_EDITOR_IMPORT_FAILED);
    reader.readAsText(file);
    event.currentTarget.value = ''; // so the same file can be picked twice
  }

  /**
   * Editor shortcuts. They are all modified keys, so none of them fires while someone is
   * typing an event ID or a description — the reason the canvas's bare-letter bindings are
   * not used here.
   */
  function onkeydown(e) {
    const ctrl = e.ctrlKey || e.metaKey;
    if (ctrl && e.key.toLowerCase() === 's') {
      e.preventDefault();
      if (ruleEditor.canSave($ruleEditor)) doSave(false);
    } else if (ctrl && e.shiftKey && e.key.toLowerCase() === 'z') {
      e.preventDefault();
      ruleEditor.redo();
    } else if (ctrl && e.key.toLowerCase() === 'z') {
      e.preventDefault();
      ruleEditor.undo();
    } else if (ctrl && e.shiftKey && e.key.toLowerCase() === 'f') {
      e.preventDefault();
      ruleEditor.format(false);
    } else if (ctrl && e.key.toLowerCase() === 'e') {
      e.preventDefault();
      switchMode($ruleEditor.mode === EDITOR_MODES.RAW ? EDITOR_MODES.GUIDED : EDITOR_MODES.RAW);
    }
  }
</script>

{#if $ruleEditor.open}
  <Dialog open={true} full {title} onclose={requestClose}>
    <!-- svelte-ignore a11y_no_noninteractive_element_interactions -->
    <div class="editor" role="group" {onkeydown}>
      <div class="bar">
        <div class="modes" role="tablist" aria-label={UI.RULE_EDITOR_MODE}>
          <button
            type="button"
            role="tab"
            aria-selected={$ruleEditor.mode === EDITOR_MODES.GUIDED}
            class:on={$ruleEditor.mode === EDITOR_MODES.GUIDED}
            disabled={!canGuided && $ruleEditor.mode !== EDITOR_MODES.GUIDED}
            title={canGuided ? UI.RULE_EDITOR_MODE_GUIDED_HINT : UI.RULE_EDITOR_CANNOT_GUIDE}
            onclick={() => switchMode(EDITOR_MODES.GUIDED)}
          >
            {UI.RULE_EDITOR_MODE_GUIDED}
          </button>
          <button
            type="button"
            role="tab"
            aria-selected={$ruleEditor.mode === EDITOR_MODES.RAW}
            class:on={$ruleEditor.mode === EDITOR_MODES.RAW}
            title={UI.RULE_EDITOR_MODE_RAW_HINT}
            onclick={() => switchMode(EDITOR_MODES.RAW)}
          >
            {UI.RULE_EDITOR_MODE_RAW}
          </button>
        </div>

        <div class="tools">
          {#if $ruleEditor.mode === EDITOR_MODES.RAW}
            <Button variant="text" onclick={() => ruleEditor.format(false)}>{UI.RULE_EDITOR_PRETTY}</Button>
            <Button variant="text" onclick={() => ruleEditor.format(true)}>{UI.RULE_EDITOR_MINIFY}</Button>
          {/if}
          <Button variant="text" onclick={() => fileInput?.click()}>{UI.RULE_EDITOR_IMPORT}</Button>
          <Button variant="text" onclick={exportRule}>{UI.RULE_EDITOR_EXPORT}</Button>
          <input
            bind:this={fileInput}
            class="hidden"
            type="file"
            accept="application/json,.json"
            onchange={importRule}
            tabindex="-1"
            aria-hidden="true"
          />
        </div>
      </div>

      {#if $ruleEditor.loading}
        <p class="msg">{UI.SPLASH_LOADING}</p>
      {:else}
        <div class="split">
          <div class="pane">
            {#if $ruleEditor.mode === EDITOR_MODES.RAW}
              <RawEditorPanel
                bind:this={rawPanel}
                value={$ruleEditor.doc.text}
                {errorLines}
                schema={$ruleEditor.schema}
                {eventIds}
                onchange={(text, opts) => ruleEditor.setText(text, opts)}
              />
            {:else}
              <GuidedEditorPanel
                schema={$ruleEditor.schema}
                value={form.known}
                unknown={form.unknown}
                {problems}
                onfield={(key, value) => ruleEditor.setField(key, value)}
                onfields={(changes) => ruleEditor.setFields(changes)}
              />
            {/if}
          </div>

          <div class="pane side">
            <div class="sidetabs" role="tablist" aria-label={UI.RULE_EDITOR_SIDE}>
              <button
                type="button"
                role="tab"
                aria-selected={sidePane === 'preview'}
                class:on={sidePane === 'preview'}
                onclick={() => (sidePane = 'preview')}>{UI.RULE_EDITOR_PREVIEW}</button
              >
              <button
                type="button"
                role="tab"
                aria-selected={sidePane === 'diff'}
                class:on={sidePane === 'diff'}
                onclick={() => (sidePane = 'diff')}>{UI.RULE_EDITOR_DIFF}</button
              >
              <button
                type="button"
                role="tab"
                aria-selected={sidePane === 'test'}
                class:on={sidePane === 'test'}
                onclick={() => (sidePane = 'test')}>{UI.RULE_EDITOR_TESTBENCH}</button
              >
            </div>
            {#if sidePane === 'preview'}
              <RulePreviewPane text={$ruleEditor.doc.text} />
            {:else if sidePane === 'diff'}
              <!-- Both views, in reading order. "What changed about the rule" is the question an
                   author usually has; "what changed in the text" is the one they need when the
                   answer is surprising. Reformatting shows nothing in the first and a great deal
                   in the second, which is exactly the distinction worth being able to see. -->
              <div class="diffstack">
                <RuleFieldDiff before={beforeRule} after={afterRule} schema={$ruleEditor.schema} />
                <RuleDiffView before={$ruleEditor.original} after={$ruleEditor.doc.text} />
              </div>
            {:else}
              <TestbenchPanel
                result={$ruleEditor.test}
                running={$ruleEditor.testing}
                runnable={problems.errors.length === 0}
                onrun={() => ruleEditor.testbench(formToFilter(prefs.current().filters || {}))}
              />
            {/if}
          </div>
        </div>

        <!-- One rail for both modes. Clicking a problem goes to it: a line in raw mode, a
             control in guided mode — which is what the {code, field, index, line} shape the
             backend reports is for. -->
        <div class="rail" class:bad={problems.errors.length > 0}>
          {#if problems.errors.length === 0 && problems.warnings.length === 0}
            <span class="ok">{UI.RULE_EDITOR_VALID}</span>
          {/if}
          {#each problems.errors as problem, i (i)}
            <button type="button" class="problem error" onclick={() => goToProblem(problem)}>
              {#if problem.line}<span class="at">{UI.RULE_EDITOR_LINE} {problem.line}</span>{/if}
              {problem.message}
            </button>
          {/each}
          {#each problems.warnings as problem, i (i)}
            <button type="button" class="problem warn" onclick={() => goToProblem(problem)}>
              {problem.message}
            </button>
          {/each}
        </div>

        {#if rename}
          <!-- A rename is not a detail: the id is a slug of the name, so this retires one
               rule and creates another, and the graph the old id built stops resolving to
               it. Said before saving, not after. -->
          <p class="rename">
            {UI.RULE_EDITOR_RENAME_WARNING}
            <code>{rename.from}</code> → <code>{rename.to}</code>
          </p>
        {/if}
      {/if}
    </div>

    {#snippet actions()}
      <Button variant="text" onclick={requestClose}>{UI.ACTION_CANCEL}</Button>
      <Button variant="tonal" disabled={!ruleEditor.canSave($ruleEditor)} onclick={() => doSave(true)}>
        {UI.RULE_EDITOR_SAVE_AND_RUN}
      </Button>
      <Button variant="filled" disabled={!ruleEditor.canSave($ruleEditor)} onclick={() => doSave(false)}>
        {$ruleEditor.saving ? UI.RULE_EDITOR_SAVING : UI.RULE_EDITOR_SAVE}
      </Button>
    {/snippet}
  </Dialog>
{/if}

{#if confirmingDiscard}
  <Dialog open={true} title={UI.RULE_EDITOR_DISCARD_TITLE} onclose={() => (confirmingDiscard = false)}>
    <p>{UI.RULE_EDITOR_DISCARD_BODY}</p>
    {#snippet actions()}
      <Button variant="text" onclick={() => (confirmingDiscard = false)}>{UI.RULE_EDITOR_KEEP_EDITING}</Button>
      <Button
        variant="filled"
        onclick={() => {
          confirmingDiscard = false;
          ruleEditor.close();
        }}>{UI.RULE_EDITOR_DISCARD}</Button
      >
    {/snippet}
  </Dialog>
{/if}

<style>
  /* The two diff views share the side pane, separated so neither reads as part of the other. */
  .diffstack {
    display: flex;
    flex-direction: column;
    min-height: 0;
    overflow-y: auto;
  }
  .diffstack > :global(* + *) {
    border-top: 1px solid var(--color-outline);
  }

  .editor {
    display: flex;
    flex-direction: column;
    flex: 1;
    min-height: 0;
    gap: var(--space-3);
  }
  .bar {
    display: flex;
    align-items: center;
    justify-content: space-between;
    gap: var(--space-4);
    flex-wrap: wrap;
  }
  .modes {
    display: inline-flex;
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .modes button,
  .sidetabs button {
    padding: var(--space-2) var(--space-4);
    background: var(--color-surface);
    border: none;
    color: var(--color-on-surface-variant, var(--color-on-surface-muted));
    font-family: var(--font-sans);
    font-size: 0.8rem;
    font-weight: 700;
    cursor: pointer;
  }
  .modes button.on,
  .sidetabs button.on {
    background: var(--color-primary);
    color: var(--color-on-primary);
  }
  .modes button:disabled {
    opacity: 0.4;
    cursor: not-allowed;
  }
  .tools {
    display: flex;
    align-items: center;
    gap: var(--space-1);
  }
  .hidden {
    display: none;
  }

  .split {
    display: grid;
    grid-template-columns: minmax(0, 1.1fr) minmax(0, 1fr);
    gap: var(--space-4);
    flex: 1;
    min-height: 0;
  }
  /* Below this the two panes would each be too narrow to be usable, so the preview drops
     away rather than squeezing the surface that is actually being edited. */
  @media (max-width: 900px) {
    .split {
      grid-template-columns: 1fr;
    }
    .side {
      display: none;
    }
  }
  .pane {
    display: flex;
    flex-direction: column;
    min-height: 0;
    min-width: 0;
  }
  .sidetabs {
    display: inline-flex;
    align-self: flex-start;
    margin-bottom: var(--space-2);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    overflow: hidden;
  }
  .sidetabs button {
    padding: var(--space-1) var(--space-3);
    font-size: 0.72rem;
  }

  .rail {
    flex: 0 0 auto;
    max-height: 96px;
    overflow-y: auto;
    display: flex;
    flex-direction: column;
    gap: 2px;
    padding: var(--space-2);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
  }
  .rail.bad {
    border-color: var(--color-error);
  }
  .ok {
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-success);
  }
  .problem {
    display: flex;
    gap: var(--space-2);
    text-align: left;
    background: none;
    border: none;
    padding: 2px var(--space-1);
    border-radius: var(--radius-sm);
    font-family: var(--font-sans);
    font-size: 0.76rem;
    cursor: pointer;
  }
  .problem:hover {
    background: var(--color-surface);
  }
  .problem.error {
    color: var(--color-error);
  }
  .problem.warn {
    color: var(--color-warning);
  }
  .at {
    flex: 0 0 auto;
    font-family: var(--font-mono);
    font-size: 0.72rem;
    opacity: 0.8;
  }

  .rename {
    flex: 0 0 auto;
    margin: 0;
    padding: var(--space-2) var(--space-3);
    background: color-mix(in srgb, var(--color-warning) 14%, transparent);
    border: 1px solid var(--color-warning);
    border-radius: var(--radius-md);
    font-family: var(--font-sans);
    font-size: 0.76rem;
    color: var(--color-on-surface);
  }
  .rename code {
    font-family: var(--font-mono);
    font-size: 0.74rem;
  }
  .msg {
    padding: var(--space-6);
    text-align: center;
    font-family: var(--font-sans);
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
  }
</style>
