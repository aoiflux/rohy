/**
 * Placing annotations on the canvas (P29).
 *
 * The document is stored in Go. What lives here is the arithmetic that turns an anchor into a
 * position, and the rules about what may be drawn at all.
 *
 * One decision runs through the whole module: **an annotation is drawn only where it can honestly
 * be placed.** A note anchored to an event that is not on this canvas has no position — and a
 * pin floating at the origin, or clamped to the nearest card, would be a mark pointing at the
 * wrong evidence. So it is not drawn, and the count of what could not be placed is surfaced
 * instead. That is the same rule the temporal layout applies to undated events.
 */

/** Kinds and anchor kinds, mirroring backend consts. */
export const KIND = Object.freeze({ NOTE: 'note', REGION: 'region', ARROW: 'arrow' });
export const ANCHOR = Object.freeze({ EVENT: 'event', WORLD: 'world' });

/**
 * resolveAnchor turns one anchor into a world-space point, or null when it cannot be placed.
 *
 * An event anchor resolves through `nodeOf` (hash → live node id) and then through the canvas
 * nodes. Both steps can fail — the event may have left the case, or may simply not be on this
 * canvas — and both mean "no position", not "position zero".
 *
 * @param {{kind:string,hash?:string,x?:number,y?:number,w?:number,h?:number}} anchor
 * @param {Record<string,number>} nodeOf
 * @param {Record<string|number,{x:number,y:number}>} nodes
 * @param {{width:number,height:number}} geom
 */
export function resolveAnchor(anchor, nodeOf, nodes, geom) {
  if (!anchor) return null;
  if (anchor.kind === ANCHOR.WORLD) {
    if (!Number.isFinite(anchor.x) || !Number.isFinite(anchor.y)) return null;
    return { x: anchor.x, y: anchor.y, w: anchor.w || 0, h: anchor.h || 0 };
  }
  if (anchor.kind !== ANCHOR.EVENT || !anchor.hash) return null;

  const id = nodeOf?.[anchor.hash];
  if (id === undefined || id === null) return null;
  const n = nodes?.[String(id)];
  if (!n || !Number.isFinite(n.x) || !Number.isFinite(n.y)) return null;
  // Anchored to the card's top-right corner, so a pin sits beside the evidence rather than over
  // the fields it is a comment on.
  return { x: n.x + (geom?.width ?? 0), y: n.y, w: 0, h: 0 };
}

/**
 * placed returns the annotations that can be drawn, each with its resolved geometry, plus a count
 * of those that could not be placed and why.
 *
 * Hidden layers are excluded from `items` but NOT counted as unplaceable: hiding a layer is a
 * choice the analyst made, and reporting it as a problem would train them to ignore the report.
 *
 * @param {{layers?:{id:string,visible:boolean,z:number,colour?:string,name:string}[], items?:any[]}|null} doc
 * @param {Record<string,number>} nodeOf
 * @param {Record<string|number,{x:number,y:number}>} nodes
 * @param {{width:number,height:number}} geom
 */
export function placed(doc, nodeOf, nodes, geom) {
  const layers = new Map((doc?.layers ?? []).map((l) => [l.id, l]));
  const out = [];
  let unplaceable = 0;

  for (const it of doc?.items ?? []) {
    if (!it) continue;
    const layer = layers.get(it.layer);
    const from = resolveAnchor(it.anchor, nodeOf, nodes, geom);
    const to = it.kind === KIND.ARROW ? resolveAnchor(it.to, nodeOf, nodes, geom) : null;

    if (!from || (it.kind === KIND.ARROW && !to)) {
      // 🔒 No position, so it is not drawn. A pin at the origin or clamped to the nearest card
      // would be a mark pointing at the wrong evidence.
      unplaceable += 1;
      continue;
    }
    if (layer && layer.visible === false) continue;
    out.push({ item: it, layer: layer ?? null, from, to });
  }

  // Draw in layer order, then in creation order within a layer, so the picture is the same every
  // time and a layer moved to the front actually comes forward.
  out.sort((a, b) => {
    const za = a.layer?.z ?? 0;
    const zb = b.layer?.z ?? 0;
    if (za !== zb) return za - zb;
    return String(a.item.id).localeCompare(String(b.item.id));
  });
  return { items: out, unplaceable };
}

/**
 * arrowPath renders an SVG path between two resolved anchors, from the centre of each. Regions
 * have width and height; a pin does not, so `centre` handles both.
 */
export function arrowPath(from, to) {
  if (!from || !to) return '';
  const a = centre(from);
  const b = centre(to);
  return `M ${round(a.x)} ${round(a.y)} L ${round(b.x)} ${round(b.y)}`;
}

export function centre(box) {
  return { x: box.x + (box.w || 0) / 2, y: box.y + (box.h || 0) / 2 };
}

const round = (v) => Math.round(v * 100) / 100;

/**
 * normaliseRegion turns a drag (two corners, in any direction) into a positive-extent rectangle.
 * A region stored with a negative width renders as nothing at all, which reads as a drag that
 * did not take.
 */
export function normaliseRegion(a, b) {
  const x = Math.min(a.x, b.x);
  const y = Math.min(a.y, b.y);
  return { x, y, w: Math.abs(b.x - a.x), h: Math.abs(b.y - a.y) };
}

/**
 * nextLayerColour picks a colour for a new layer from a fixed rotation, so two layers made in a
 * row are visibly different without asking the analyst to choose.
 */
export const LAYER_COLOURS = Object.freeze([
  '#c2410c',
  '#0369a1',
  '#4d7c0f',
  '#7c3aed',
  '#b91c1c',
  '#0f766e',
]);

export function nextLayerColour(existing) {
  const used = new Set((existing ?? []).map((l) => l && l.colour));
  return LAYER_COLOURS.find((c) => !used.has(c)) ?? LAYER_COLOURS[(existing?.length ?? 0) % LAYER_COLOURS.length];
}

/**
 * describe reports what the canvas is not showing, for the note under the layer list.
 *
 * `orphaned` comes from the backend — anchors whose event has left the case entirely — and is
 * kept separate from `unplaceable`, which is the wider "not on this canvas". They are different
 * problems with different fixes: one is gone, the other is merely elsewhere.
 */
export function describe(unplaceable, orphaned) {
  const notes = [];
  const gone = (orphaned ?? []).length;
  if (gone > 0) notes.push({ kind: 'orphaned', count: gone });
  const elsewhere = Math.max((unplaceable ?? 0) - gone, 0);
  if (elsewhere > 0) notes.push({ kind: 'offcanvas', count: elsewhere });
  return notes;
}
