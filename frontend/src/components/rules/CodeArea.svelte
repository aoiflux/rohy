<script>
  // A JSON code editor: a transparent-text <textarea> over a syntax-highlighted <pre>.
  //
  // Hand-rolled on purpose. A code-editor library would be the largest dependency in an app
  // that currently has zero runtime dependencies, in a tool built to run offline on evidence
  // machines — and it would need its own bridge to reach the CSS token system that gives
  // every other surface here its light and dark themes. A rule is a JSON object with seven
  // keys; none of the reasons to reach for CodeMirror apply.
  //
  // The pattern's one hard requirement is that both layers lay text out IDENTICALLY: the
  // same font, size, line height, padding, wrapping and tab size, or the highlight drifts
  // away from the characters the caret is actually on. Everything in the style block below
  // that appears twice appears twice for that reason.
  import { segments } from '../../lib/rules/highlight.js';
  import { bracketMarks, keystroke, markSegments, matchAt } from '../../lib/rules/brackets.js';
  import { UI } from '../../lib/consts/index.js';

  let {
    value = '',
    /** 1-based line numbers to mark as containing a problem. */
    errorLines = [],
    readonly = false,
    ariaLabel = UI.RULE_EDITOR_SOURCE_LABEL,
    oninput = undefined,
    onkeydown = undefined,
    /** Called with the caret offset whenever it moves, or -1 when there is a selection. */
    oncaret = undefined,
    element = $bindable(null),
  } = $props();

  let scroller = $state(null);
  let scrollTop = $state(0);
  let scrollLeft = $state(0);
  /** The caret while the field has focus and nothing is selected; -1 otherwise. */
  let caret = $state(-1);

  // The bracket beside the caret and its partner are marked in the underlay. matchAt clamps,
  // because the caret can be one edit behind the value for the frame in between.
  const parts = $derived(markSegments(segments(value), bracketMarks(caret < 0 ? null : matchAt(value, caret))));
  // A trailing newline produces no visible line in the <pre>, so the last row of the gutter
  // would have nothing to sit beside. Counting from the split keeps the two in step.
  const lines = $derived(value.split('\n'));
  const errorSet = $derived(new Set(errorLines.filter((n) => n > 0)));

  function sync(e) {
    scrollTop = e.currentTarget.scrollTop;
    scrollLeft = e.currentTarget.scrollLeft;
  }

  /** track records the caret. Typing does not report it: the owner hears of an edit through
   *  oninput, and oncaret is for the moves that change no text. */
  function track(el, report = true) {
    caret = el.selectionStart === el.selectionEnd ? el.selectionStart : -1;
    if (report) oncaret?.(caret);
  }

  function handleInput(e) {
    track(e.currentTarget, false);
    oninput?.(e.currentTarget.value, e);
  }

  /**
   * The owner sees the key first: an open suggestion list owns Enter, Tab and the arrows, and
   * only a key it leaves alone is an edit. The edits themselves — pairs closed and typed over,
   * an empty pair deleted whole, Enter keeping the indent, Tab as two spaces — are
   * brackets.js's. Tab traps keyboard users, so Escape is left free to leave the field and
   * the hint below says so.
   */
  function handleKeydown(e) {
    onkeydown?.(e);
    if (e.defaultPrevented || readonly || e.isComposing) return;
    // AltGr arrives as Ctrl+Alt on Windows, and it is how much of Europe types { and [.
    const altGr = e.getModifierState?.('AltGraph');
    if (!altGr && (e.ctrlKey || e.metaKey || e.altKey)) return;
    if (e.shiftKey && (e.key === 'Tab' || e.key === 'Enter')) return;
    const el = e.currentTarget;
    const next = keystroke(value, el.selectionStart, el.selectionEnd, e.key);
    if (!next) return;
    e.preventDefault();
    // The element is written before the owner hears of it, so anything reading the caret on
    // the input — the suggestion list does — reads where it now is. Svelte then writes the
    // same text back, which leaves the caret alone. A step over a closer changes no text and
    // is only a caret move: it must not reach oninput, which would record an undo step and
    // drop a testbench result for an edit that did not happen.
    const edited = next.text !== value;
    if (edited) el.value = next.text;
    el.selectionStart = el.selectionEnd = next.caret;
    track(el, !edited);
    if (edited) oninput?.(next.text, e);
  }

  /** focusLine puts the caret at the start of a 1-based line — how clicking a problem in the
   *  error rail gets the author to it. */
  export function focusLine(line) {
    if (!element) return;
    const offset = lines.slice(0, Math.max(0, line - 1)).reduce((n, l) => n + l.length + 1, 0);
    element.focus();
    element.selectionStart = element.selectionEnd = Math.min(offset, value.length);
    track(element);
    // Put the target line near the middle rather than at the very top edge.
    const lineHeight = element.scrollHeight / Math.max(lines.length, 1);
    element.scrollTop = Math.max(0, (line - 1) * lineHeight - element.clientHeight / 2);
  }
</script>

<div class="code" bind:this={scroller}>
  <div class="gutter" style:transform="translateY({-scrollTop}px)" aria-hidden="true">
    {#each lines as _, i}
      <div class="lineno" class:bad={errorSet.has(i + 1)}>{i + 1}</div>
    {/each}
  </div>

  <div class="field">
    <!-- The underlay is decoration: the textarea above it carries the accessible text, so a
         screen reader must not encounter the same content twice. -->
    <pre
      class="underlay"
      aria-hidden="true"
      style:transform="translate({-scrollLeft}px, {-scrollTop}px)">{#each parts as part}{#if part.type || part.mark}<span
            class={part.type ? `t-${part.type}` : ''}
            class:m-match={part.mark === 'match'}
            class:m-unmatched={part.mark === 'unmatched'}>{part.text}</span>{:else}{part.text}{/if}{/each}<span class="tail"> </span></pre>

    <textarea
      bind:this={element}
      class="input"
      class:readonly
      {value}
      {readonly}
      aria-label={ariaLabel}
      spellcheck="false"
      autocomplete="off"
      autocapitalize="off"
      autocorrect="off"
      wrap="off"
      oninput={handleInput}
      onkeydown={handleKeydown}
      onkeyup={(e) => track(e.currentTarget)}
      onmouseup={(e) => track(e.currentTarget)}
      onfocus={(e) => track(e.currentTarget)}
      onselect={(e) => track(e.currentTarget)}
      onblur={() => {
        caret = -1;
        oncaret?.(-1);
      }}
      onscroll={sync}
    ></textarea>
  </div>
</div>

<style>
  .code {
    display: flex;
    flex: 1;
    min-height: 0;
    overflow: hidden;
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
  }

  .gutter {
    flex: 0 0 auto;
    padding: var(--space-3) var(--space-2);
    text-align: right;
    background: var(--color-surface);
    border-right: 1px solid var(--color-outline);
    user-select: none;
    /* The gutter is translated rather than scrolled so it cannot lag behind the text on a
       fast scroll — the two would visibly separate for a frame. */
    will-change: transform;
  }
  .lineno {
    font-family: var(--font-mono);
    font-size: 0.78rem;
    line-height: 1.55;
    color: var(--color-on-surface-muted);
    min-width: 2ch;
  }
  .lineno.bad {
    color: var(--color-error);
    font-weight: 700;
  }

  .field {
    position: relative;
    flex: 1;
    min-width: 0;
    overflow: hidden;
  }

  /* Everything below that is duplicated between .underlay and .input is duplicated because
     the two layers must lay text out identically — any difference shows up as highlighting
     that drifts away from the characters under the caret. */
  .underlay,
  .input {
    margin: 0;
    padding: var(--space-3);
    border: none;
    font-family: var(--font-mono);
    font-size: 0.78rem;
    line-height: 1.55;
    tab-size: 2;
    white-space: pre;
    word-break: normal;
    overflow-wrap: normal;
  }

  .underlay {
    position: absolute;
    inset: 0;
    pointer-events: none;
    color: var(--color-on-surface);
    will-change: transform;
  }
  /* A trailing newline renders no line box, which would clip the last line's background.
     A zero-width-ish tail keeps the box open. */
  .tail {
    opacity: 0;
  }

  .input {
    position: relative;
    width: 100%;
    height: 100%;
    box-sizing: border-box;
    resize: none;
    background: transparent;
    /* The text itself is invisible; only the caret and the selection show through, so what
       the reader sees is the highlighted copy underneath. */
    color: transparent;
    caret-color: var(--color-on-surface);
    outline: none;
    overflow: auto;
  }
  .input::selection {
    background: color-mix(in srgb, var(--color-primary) 35%, transparent);
  }
  .input.readonly {
    caret-color: transparent;
  }
  .code:focus-within {
    border-color: var(--color-primary);
  }

  /* Token colours are theme tokens, so light and dark come from the same place as the rest
     of the app rather than from a second palette that has to be kept in step. */
  .t-key {
    color: var(--color-primary);
    font-weight: 700;
  }
  .t-string {
    color: var(--color-success);
  }
  .t-number {
    color: var(--color-accent);
  }
  .t-boolean,
  .t-null {
    color: var(--color-warning);
  }
  .t-punct {
    color: var(--color-on-surface-muted);
  }
  .t-error {
    color: var(--color-error);
    text-decoration: underline wavy var(--color-error);
    text-underline-offset: 3px;
  }

  /* The bracket pair beside the caret. Background and an inset shadow only: anything that
     takes space (a border, padding, a heavier weight) would push the underlay out of line
     with the textarea's characters. */
  .m-match {
    background: color-mix(in srgb, var(--color-primary) 22%, transparent);
    box-shadow: inset 0 0 0 1px color-mix(in srgb, var(--color-primary) 60%, transparent);
    border-radius: 2px;
  }
  .m-unmatched {
    background: color-mix(in srgb, var(--color-error) 25%, transparent);
    border-radius: 2px;
  }
</style>
