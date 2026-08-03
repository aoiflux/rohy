<script>
  // The card a collapsed group folds into.
  //
  // It is deliberately NOT a node card wearing a different colour. A node card stands for one
  // piece of evidence; this stands for many, and the two must not be confusable — so it carries
  // its count in the largest text on it, a stacked-paper edge, and an explicit expand control.
  //
  // The count is the cluster's own, not how many members are currently on the canvas. A folded
  // card that under-reported what it holds would be hiding the one thing it exists to disclose.
  import { GRAPH, UI } from '../../lib/consts/index.js';

  let {
    /** @type {{id:string,clusterId:string,label:string,size:number,onCanvas:number,x:number,y:number}} */
    card,
    onexpand = undefined,
  } = $props();

  const hidden = $derived(card.size - card.onCanvas);
</script>

<button
  class="cluster"
  style="left: {card.x}px; top: {card.y}px; width: {GRAPH.NODE_WIDTH}px; height: {GRAPH.NODE_HEIGHT}px"
  onclick={() => onexpand?.(card.clusterId)}
  title="{UI.CLUSTER_EXPAND} — {card.label}"
>
  <span class="count">{card.size}</span>
  <span class="what">{UI.CLUSTER_EVENTS}</span>
  <span class="label">{card.label}</span>
  <!-- If some members are not on this canvas, say so rather than letting the count imply they
       are all here to be unfolded. -->
  {#if hidden > 0}
    <span class="elsewhere">{hidden} {UI.CLUSTER_NOT_ON_CANVAS}</span>
  {/if}
  <span class="expand" aria-hidden="true">⤢</span>
</button>

<style>
  /* The stacked-paper edge is the whole visual argument: this is several things, not one. */
  .cluster {
    position: absolute;
    display: flex;
    flex-direction: column;
    align-items: flex-start;
    justify-content: center;
    gap: 2px;
    padding: 10px 14px;
    text-align: left;
    font-family: var(--font-sans);
    color: var(--color-on-surface);
    background: var(--color-surface);
    border: 2px solid var(--color-outline);
    border-radius: 10px;
    cursor: pointer;
    box-shadow:
      6px 6px 0 -2px var(--color-surface),
      6px 6px 0 0 var(--color-outline),
      12px 12px 0 -2px var(--color-surface),
      12px 12px 0 0 var(--color-outline);
  }
  .cluster:hover {
    border-color: var(--color-primary);
  }
  .count {
    font-size: 1.5rem;
    font-weight: 800;
    line-height: 1;
  }
  .what {
    font-size: 0.7rem;
    letter-spacing: 0.06em;
    text-transform: uppercase;
    color: var(--color-on-surface-muted);
  }
  .label {
    max-width: 100%;
    overflow: hidden;
    font-size: 0.8rem;
    font-weight: 600;
    white-space: nowrap;
    text-overflow: ellipsis;
  }
  .elsewhere {
    font-size: 0.68rem;
    color: var(--color-on-surface-muted);
  }
  .expand {
    position: absolute;
    top: 6px;
    right: 8px;
    font-size: 0.9rem;
    color: var(--color-on-surface-muted);
  }
</style>
