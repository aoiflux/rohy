import { describe, it, expect, beforeEach, vi } from 'vitest';
import { get } from 'svelte/store';
import { render } from 'svelte/server';
import { CHANNELS, SEAL_STATE, UI } from '../lib/consts/index.js';

// The seal after an ingest.
//
// The ingest's last file completes, and then the case is compacted — over a second on a large case.
// Before the bar listened for it, that second was silence after "complete", which reads as a hang.
// The API is stubbed so the backend's events can be delivered by hand.

const handlers = {};
vi.mock('../lib/api/index.js', () => ({
  on: vi.fn((channel, handler) => {
    handlers[channel] = handler;
    return () => {};
  }),
}));

const { ingestion } = await import('./ingestion.js');
const IngestionBar = (await import('../components/material/IngestionBar.svelte')).default;

beforeEach(() => {
  ingestion.reset();
  ingestion.wire();
});

describe('ingestion / sealing', () => {
  it('is sealing from case:sealing until case:sealed, and keeps what the seal did', () => {
    expect(get(ingestion).sealing).toBe(false);

    handlers[CHANNELS.CASE_SEALING]();
    expect(get(ingestion).sealing).toBe(true);

    handlers[CHANNELS.CASE_SEALED]({ state: SEAL_STATE.SEALED, snapshot: 'ab' });
    const s = get(ingestion);
    expect(s.sealing).toBe(false);
    expect(s.lastSeal).toEqual({ state: SEAL_STATE.SEALED, snapshot: 'ab' });
  });

  // 🔒 The wait always ends. case:sealed fires for a skipped or failed seal too, and a payload the
  // backend did not send must not leave the bar up forever.
  it('ends the wait whatever the seal did', () => {
    for (const outcome of [{ state: SEAL_STATE.UNCHANGED }, { state: SEAL_STATE.FAILED }, null]) {
      handlers[CHANNELS.CASE_SEALING]();
      handlers[CHANNELS.CASE_SEALED](outcome);
      expect(get(ingestion).sealing).toBe(false);
    }
  });

  it('the bar stays up through the seal and says what it is doing', () => {
    expect(render(IngestionBar).body).not.toContain(UI.STATUS_SEALING);

    handlers[CHANNELS.CASE_SEALING]();
    const { body } = render(IngestionBar);
    expect(body).toContain(UI.STATUS_SEALING);
    expect(body).toContain(UI.SEAL_BAR_HINT);

    handlers[CHANNELS.CASE_SEALED]({ state: SEAL_STATE.SEALED });
    expect(render(IngestionBar).body).not.toContain(UI.STATUS_SEALING);
  });
});
