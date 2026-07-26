<script>
  // The sequence and its connection labels, edited together as one chain.
  //
  // This is the one place the form beats raw JSON outright. `labels[i]` labels the edge from
  // sequence[i] to sequence[i+1], so a sequence of n steps has exactly n−1 labels — the
  // format's most error-prone rule, and one that is invisible when the two arrays sit in
  // separate boxes. Rendering each label BETWEEN the two steps it joins makes the off-by-one
  // impossible to make: there is nowhere to put a fourth label on three steps.
  //
  // Because of that, adding or removing a step edits both arrays at once, and the two go
  // back as a single change — one undo takes back the whole operation rather than half of it.
  import { UI } from '../../lib/consts/index.js';

  let {
    sequence = [],
    labels = [],
    maxSteps = 1000,
    /** Problems on the sequence, keyed by element index for per-step marking. */
    problems = [],
    onchange = undefined,
  } = $props();

  const badIndexes = $derived(new Set(problems.filter((p) => p.index >= 0).map((p) => p.index)));

  /** commit sends both arrays together, trimming the labels to the number of connections
   *  that actually exist so the pair can never be inconsistent. */
  function commit(nextSequence, nextLabels) {
    const connections = Math.max(0, nextSequence.length - 1);
    const trimmed = nextLabels.slice(0, connections);
    // A tail of empty labels says nothing that their absence does not, and dropping it keeps
    // the saved file as small as the rule it describes.
    while (trimmed.length > 0 && trimmed[trimmed.length - 1] === '') trimmed.pop();
    onchange?.({ sequence: nextSequence, labels: trimmed });
  }

  function setStep(i, value) {
    const next = [...sequence];
    next[i] = value;
    commit(next, labels);
  }

  function setLabel(i, value) {
    const next = [...labels];
    while (next.length <= i) next.push('');
    next[i] = value;
    commit(sequence, next);
  }

  function addStep() {
    commit([...sequence, ''], labels);
  }

  function removeStep(i) {
    // Removing a step removes exactly one connection: the one leaving it, or — when it is
    // the last step — the one arriving at it. Dropping that label keeps every remaining one
    // describing the hop it was written for.
    const nextSequence = sequence.filter((_, n) => n !== i);
    const drop = Math.min(i, labels.length - 1);
    commit(nextSequence, labels.filter((_, n) => n !== drop));
  }

  function move(i, delta) {
    const to = i + delta;
    if (to < 0 || to >= sequence.length) return;
    const next = [...sequence];
    [next[i], next[to]] = [next[to], next[i]];
    // The labels are NOT reordered with the steps: a label describes the transition at a
    // position in the chain ("then succeeds"), not the step it happens to follow, and
    // carrying it along would silently reword the wrong hop.
    commit(next, labels);
  }
</script>

<div class="chain">
  {#each sequence as step, i (i)}
    <div class="step" class:bad={badIndexes.has(i)}>
      <span class="pos">{i + 1}</span>
      <input
        class="id"
        value={step}
        inputmode="numeric"
        placeholder={UI.RULE_EDITOR_STEP_PLACEHOLDER}
        aria-label={`${UI.RULE_EDITOR_STEP} ${i + 1}`}
        oninput={(e) => setStep(i, e.currentTarget.value)}
      />
      <div class="stepactions">
        <button type="button" title={UI.RULE_EDITOR_MOVE_UP} disabled={i === 0} onclick={() => move(i, -1)}>↑</button>
        <button
          type="button"
          title={UI.RULE_EDITOR_MOVE_DOWN}
          disabled={i === sequence.length - 1}
          onclick={() => move(i, 1)}>↓</button
        >
        <button type="button" title={UI.RULE_EDITOR_REMOVE_STEP} onclick={() => removeStep(i)}>×</button>
      </div>
    </div>

    {#if i < sequence.length - 1}
      <!-- The connection, drawn between the two steps it joins. -->
      <div class="link">
        <span class="arrow" aria-hidden="true">↓</span>
        <input
          class="label"
          value={labels[i] ?? ''}
          placeholder={UI.RULE_EDITOR_LABEL_PLACEHOLDER}
          aria-label={`${UI.RULE_EDITOR_CONNECTION} ${i + 1}`}
          oninput={(e) => setLabel(i, e.currentTarget.value)}
        />
      </div>
    {/if}
  {/each}

  <button type="button" class="add" disabled={sequence.length >= maxSteps} onclick={addStep}>
    + {UI.RULE_EDITOR_ADD_STEP}
  </button>

  <p class="count">
    {sequence.length}
    {UI.RULE_EDITOR_STEPS} · {Math.max(0, sequence.length - 1)}
    {UI.RULE_EDITOR_CONNECTIONS}
  </p>
</div>

<style>
  .chain {
    display: flex;
    flex-direction: column;
    gap: var(--space-1);
  }
  .step {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding: var(--space-2);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
  }
  .step.bad {
    border-color: var(--color-error);
  }
  .pos {
    flex: 0 0 auto;
    width: 20px;
    text-align: center;
    font-family: var(--font-mono);
    font-size: 0.7rem;
    color: var(--color-on-surface-muted);
  }
  .id {
    flex: 1;
    min-width: 0;
    background: var(--color-surface);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-sm);
    padding: var(--space-2);
    font-family: var(--font-mono);
    font-size: 0.82rem;
    color: var(--color-on-surface);
    outline: none;
  }
  .id:focus {
    border-color: var(--color-primary);
  }
  .stepactions {
    display: flex;
    gap: 2px;
  }
  .stepactions button {
    width: 24px;
    height: 24px;
    background: none;
    border: 1px solid transparent;
    border-radius: var(--radius-sm);
    color: var(--color-on-surface-muted);
    cursor: pointer;
    line-height: 1;
  }
  .stepactions button:hover:not(:disabled) {
    color: var(--color-primary);
    border-color: var(--color-outline);
  }
  .stepactions button:disabled {
    opacity: 0.3;
    cursor: default;
  }

  .link {
    display: flex;
    align-items: center;
    gap: var(--space-2);
    padding-left: 26px;
  }
  .arrow {
    color: var(--color-on-surface-muted);
    font-size: 0.8rem;
  }
  .label {
    flex: 1;
    min-width: 0;
    background: transparent;
    border: none;
    border-bottom: 1px dashed var(--color-outline);
    padding: var(--space-1) var(--space-2);
    font-family: var(--font-sans);
    font-size: 0.75rem;
    font-style: italic;
    color: var(--color-accent);
    outline: none;
  }
  .label:focus {
    border-bottom-style: solid;
    border-bottom-color: var(--color-accent);
  }
  .label::placeholder {
    color: var(--color-on-surface-muted);
    font-style: normal;
  }

  .add {
    align-self: flex-start;
    margin-top: var(--space-2);
    padding: var(--space-2) var(--space-3);
    background: none;
    border: 1px dashed var(--color-outline);
    border-radius: var(--radius-md);
    color: var(--color-primary);
    font-family: var(--font-sans);
    font-size: 0.78rem;
    font-weight: 700;
    cursor: pointer;
  }
  .add:hover:not(:disabled) {
    border-color: var(--color-primary);
    border-style: solid;
  }
  .add:disabled {
    opacity: 0.4;
    cursor: default;
  }
  .count {
    margin: var(--space-2) 0 0;
    font-family: var(--font-sans);
    font-size: 0.7rem;
    color: var(--color-on-surface-muted);
  }
</style>
