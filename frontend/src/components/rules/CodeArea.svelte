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
  import { UI } from '../../lib/consts/index.js';

  let {
    value = '',
    /** 1-based line numbers to mark as containing a problem. */
    errorLines = [],
    readonly = false,
    ariaLabel = UI.RULE_EDITOR_SOURCE_LABEL,
    oninput = undefined,
    onkeydown = undefined,
    element = $bindable(null),
  } = $props();

  let scroller = $state(null);
  let scrollTop = $state(0);
  let scrollLeft = $state(0);

  const parts = $derived(segments(value));
  // A trailing newline produces no visible line in the <pre>, so the last row of the gutter
  // would have nothing to sit beside. Counting from the split keeps the two in step.
  const lines = $derived(value.split('\n'));
  const errorSet = $derived(new Set(errorLines.filter((n) => n > 0)));

  function sync(e) {
    scrollTop = e.currentTarget.scrollTop;
    scrollLeft = e.currentTarget.scrollLeft;
  }

  function handleInput(e) {
    oninput?.(e.currentTarget.value, e);
  }

  /**
   * Tab inserts two spaces rather than moving focus. In a code field that is what the key
   * means — but it also traps keyboard users, so Escape is left free to leave the field and
   * the hint below says so.
   */
  function handleKeydown(e) {
    if (e.key === 'Tab' && !e.shiftKey && !readonly) {
      e.preventDefault();
      const el = e.currentTarget;
      const { selectionStart: start, selectionEnd: end } = el;
      const next = `${value.slice(0, start)}  ${value.slice(end)}`;
      oninput?.(next, e);
      // Restore the caret after Svelte writes the new value back into the element.
      queueMicrotask(() => {
        el.selectionStart = el.selectionEnd = start + 2;
      });
      return;
    }
    onkeydown?.(e);
  }

  /** focusLine puts the caret at the start of a 1-based line — how clicking a problem in the
   *  error rail gets the author to it. */
  export function focusLine(line) {
    if (!element) return;
    const offset = lines.slice(0, Math.max(0, line - 1)).reduce((n, l) => n + l.length + 1, 0);
    element.focus();
    element.selectionStart = element.selectionEnd = Math.min(offset, value.length);
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
      style:transform="translate({-scrollLeft}px, {-scrollTop}px)">{#each parts as part}{#if part.type}<span
            class="t-{part.type}">{part.text}</span>{:else}{part.text}{/if}{/each}<span class="tail"> </span></pre>

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
</style>
