<script>
  // The correlation algorithms explainer.
  //
  // RULES.md section 8 already states what each algorithm establishes, and it is the authority.
  // But it is a document, read before or after the work — and the moment an author actually
  // needs it is while they are choosing an algorithm in the editor, which is exactly when they
  // will not go and read a file. This page is that explanation placed where the decision is
  // made, and reachable from the editor's algorithm selector.
  //
  // What it deliberately does NOT do is restate the format. Field names, bounds and defaults
  // are served by the backend descriptor and rendered by the guided editor; a second copy here
  // would drift. This page explains BEHAVIOUR — what a matcher does, and what a match therefore
  // proves — which is the part a form cannot show.
  import { route } from '../stores/router.js';
  import { ruleEditor } from '../stores/ruleEditor.js';
  import { ROUTES, LEARN } from '../lib/consts/index.js';
  import { SCENARIOS } from '../lib/learn/algorithms.js';

  import AppBar from '../components/material/AppBar.svelte';
  import Button from '../components/material/Button.svelte';
  import ScenarioPlayer from '../components/learn/ScenarioPlayer.svelte';

  let selected = $state(SCENARIOS[0].id);
  const scenario = $derived(SCENARIOS.find((s) => s.id === selected) ?? SCENARIOS[0]);

  function openEditor() {
    route.go(ROUTES.RULES);
    ruleEditor.createNew();
  }
</script>

<div class="page">
  <AppBar route={ROUTES.ALGORITHMS}>
    <Button variant="text" onclick={() => route.go(ROUTES.RULES)}>{LEARN.TO_RULES}</Button>
    <Button onclick={openEditor}>{LEARN.WRITE_A_RULE}</Button>
  </AppBar>

  <div class="scroll">
    <section class="intro">
      <h2>{LEARN.TITLE}</h2>
      <p>
        A rule's algorithm decides how its events are matched — and, far more importantly, what a
        match lets you <em>conclude</em>. Two rules can find the same pair of events and support
        very different claims about them.
      </p>
      <p class="muted">
        Every diagram below is a worked example taken from the engine's own test cases, so what
        you see here is what the matcher actually does.
      </p>
    </section>

    <!-- Chooser. Each card states the claim its algorithm supports, because that is the axis a
         rule author is really choosing on. -->
    <div class="chooser" role="tablist" aria-label={LEARN.TITLE}>
      {#each SCENARIOS as s}
        <button
          class="card"
          class:on={s.id === selected}
          role="tab"
          aria-selected={s.id === selected}
          onclick={() => (selected = s.id)}
        >
          <span class="card-name">{s.title}</span>
          <span class="card-tagline">{s.tagline}</span>
        </button>
      {/each}
    </div>

    <section class="detail">
      <header class="detail-head">
        <div>
          <h3>{scenario.title}</h3>
          <p class="establishes">
            <span class="badge">{LEARN.ESTABLISHES}</span>
            {scenario.establishes}
          </p>
        </div>
      </header>

      <div class="split">
        <div class="viz">
          <ScenarioPlayer {scenario} />
        </div>

        <aside class="side">
          <h4>{LEARN.THE_RULE}</h4>
          <pre>{scenario.rule}</pre>

          <h4>{LEARN.STEPS}</h4>
          <ol class="outline">
            {#each scenario.steps as s}
              <li>{s.title}</li>
            {/each}
          </ol>

          <p class="mirrors" title={scenario.mirrors}>
            {LEARN.MIRRORS}
            <code>{scenario.mirrors}</code>
          </p>
        </aside>
      </div>
    </section>

    <section class="closing">
      <h3>{LEARN.CHOOSING}</h3>
      <div class="guide">
        <p>
          <strong>Start with <code>sequence</code>.</strong> It is the cheapest to write and the
          easiest to read, and for many patterns "these happened in this order on this host" is
          genuinely the finding.
        </p>
        <p>
          <strong>Reach for <code>field</code> the moment the pairing matters.</strong> If your
          description contains the phrase "the same account" or "that session", the rule needs
          <code>match_fields</code> or it does not support the sentence you are writing.
        </p>
        <p>
          <strong>Add <code>temporal</code> when the gap is the point.</strong> A burst of failures
          followed by a success within a minute is a different finding from the same pair a week
          apart. It composes with <code>match_fields</code>.
        </p>
        <p>
          <strong>Use <code>lineage</code> for process ancestry.</strong> It reconstructs
          parent-child links from creation records instead of matching a sequence at all.
        </p>
      </div>

      <p class="pointer">
        {LEARN.FULL_REFERENCE}
      </p>
    </section>
  </div>
</div>

<style>
  .page {
    display: flex;
    flex-direction: column;
    height: 100%;
    min-height: 0;
  }
  .scroll {
    flex: 1;
    min-height: 0;
    overflow-y: auto;
    padding: var(--space-5);
    display: flex;
    flex-direction: column;
    gap: var(--space-5);
  }

  .intro {
    max-width: 68ch;
  }
  .intro h2 {
    margin: 0 0 var(--space-3);
    font-size: 1.5rem;
    color: var(--color-on-background);
  }
  .intro p {
    margin: 0 0 var(--space-2);
    line-height: 1.6;
    color: var(--color-on-surface);
  }
  .muted {
    color: var(--color-on-surface-muted) !important;
    font-size: 0.9rem;
  }

  /* --- chooser --- */
  .chooser {
    display: grid;
    grid-template-columns: repeat(auto-fit, minmax(210px, 1fr));
    gap: var(--space-3);
  }
  .card {
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    gap: var(--space-1);
    text-align: left;
    padding: var(--space-4);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    color: var(--color-on-surface);
    cursor: pointer;
    font-family: var(--font-sans);
    transition:
      border-color var(--motion-fast) var(--motion-ease),
      background var(--motion-fast) var(--motion-ease),
      transform var(--motion-fast) var(--motion-ease);
  }
  .card:hover {
    background: var(--color-surface-variant);
  }
  .card.on {
    border-color: var(--color-primary);
    background: color-mix(in srgb, var(--color-primary) 10%, var(--color-surface));
    transform: translateY(-2px);
  }
  .card-name {
    font-weight: 700;
    font-size: 1rem;
  }
  .card-tagline {
    font-size: 0.85rem;
    color: var(--color-on-surface-muted);
    line-height: 1.4;
  }

  /* --- detail --- */
  .detail {
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-lg);
    background: var(--color-surface);
    padding: var(--space-5);
  }
  .detail-head h3 {
    margin: 0 0 var(--space-2);
    font-size: 1.25rem;
  }
  .establishes {
    margin: 0 0 var(--space-4);
    color: var(--color-on-surface);
    line-height: 1.55;
    max-width: 72ch;
  }
  .badge {
    display: inline-block;
    margin-right: var(--space-2);
    padding: 2px var(--space-2);
    border-radius: var(--radius-sm);
    background: var(--color-primary);
    color: var(--color-on-primary);
    font-size: 0.7rem;
    font-weight: 700;
    letter-spacing: 0.04em;
    text-transform: uppercase;
    vertical-align: 1px;
  }

  .split {
    display: grid;
    grid-template-columns: minmax(0, 1fr) 280px;
    gap: var(--space-5);
    align-items: start;
  }
  .viz {
    min-width: 0;
  }
  .side h4 {
    margin: 0 0 var(--space-2);
    font-size: 0.78rem;
    text-transform: uppercase;
    letter-spacing: 0.06em;
    color: var(--color-on-surface-muted);
  }
  .side pre {
    margin: 0 0 var(--space-5);
    padding: var(--space-3);
    background: var(--color-surface-variant);
    border: 1px solid var(--color-outline);
    border-radius: var(--radius-md);
    font-size: 0.76rem;
    line-height: 1.5;
    overflow-x: auto;
    color: var(--color-on-surface);
  }
  .outline {
    margin: 0 0 var(--space-4);
    padding-left: 1.2em;
    color: var(--color-on-surface-muted);
    font-size: 0.85rem;
    line-height: 1.6;
  }
  .mirrors {
    margin: 0;
    font-size: 0.72rem;
    color: var(--color-on-surface-muted);
    line-height: 1.5;
  }
  .mirrors code {
    display: block;
    margin-top: 2px;
    word-break: break-word;
  }

  /* --- closing --- */
  .closing {
    max-width: 78ch;
  }
  .closing h3 {
    margin: 0 0 var(--space-3);
    font-size: 1.1rem;
  }
  .guide p {
    margin: 0 0 var(--space-3);
    line-height: 1.6;
    color: var(--color-on-surface);
  }
  .guide code {
    background: var(--color-surface-variant);
    border-radius: var(--radius-sm);
    padding: 1px 5px;
    font-size: 0.88em;
  }
  .pointer {
    margin: var(--space-4) 0 0;
    color: var(--color-on-surface-muted);
    font-size: 0.9rem;
    line-height: 1.6;
  }

  /* The sidebar drops below the diagram before the diagram gets too narrow to read. */
  @media (max-width: 900px) {
    .split {
      grid-template-columns: minmax(0, 1fr);
    }
  }
</style>
