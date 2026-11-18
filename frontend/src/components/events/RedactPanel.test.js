import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import EventDetail from './EventDetail.svelte';
import RedactPanel from './RedactPanel.svelte';
import GraphNode from '../graph/GraphNode.svelte';
import { UI } from '../../lib/consts/index.js';

// The redaction panel in the drawer.

const live = { id: 7, event_id: '4624', hash_raw: 'a', hash_normalized: 'b', timestamp: '2026-08-09T10:00:00Z' };

describe('components/events / RedactPanel', () => {
  it('offers the redaction and says what it does, asking nothing yet', () => {
    const { body } = render(RedactPanel, { props: { event: live } });
    expect(body).toContain(UI.REDACT_OPEN);
    expect(body).toContain('keeps it from being ingested again');
    // 🔒 Nothing to confirm until the preview has said what would go.
    expect(body).not.toContain(UI.REDACT_CONFIRM);
    expect(body).not.toContain('<textarea');
  });

  it('a redacted event says when and why, and offers nothing to redact', () => {
    const detail = { id: 7, redacted: true, redaction: { at: '2026-11-05T09:00:00Z', reason: 'erasure request' } };
    const { body } = render(RedactPanel, { props: { event: { id: 7, redacted: true }, detail } });
    expect(body).toContain('Redacted 2026-11-05: erasure request');
    expect(body).not.toContain(UI.REDACT_OPEN);
  });

  it('a redacted event with no ledger says so', () => {
    const { body } = render(RedactPanel, { props: { event: { id: 7, redacted: true } } });
    expect(body).toContain(UI.REDACTED_NO_LEDGER);
  });

  it('sits in the drawer', () => {
    const { body } = render(EventDetail, { props: { event: live } });
    expect(body).toContain(UI.REDACT_OPEN);
  });
});


describe('components/graph / GraphNode redacted', () => {
  it('a redacted card draws nothing but that it was redacted', () => {
    const node = { event: { id: 7, redacted: true }, x: 0, y: 0 };
    const { body } = render(GraphNode, { props: { node } });
    expect(body).toContain(UI.REDACTED_BADGE);
    const liveNode = { event: { id: 8, event_id: '4624', channel: 'Security', provider: 'P' }, x: 0, y: 0 };
    expect(render(GraphNode, { props: { node: liveNode } }).body).not.toContain(UI.REDACTED_BADGE);
  });
});
