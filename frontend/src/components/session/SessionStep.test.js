import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import SessionStep from './SessionStep.svelte';
import SigningCard from '../maintenance/SigningCard.svelte';
import StatusBar from '../material/StatusBar.svelte';
import { UI } from '../../lib/consts/index.js';

// The session step and the signing card (item 41). Rendered on the server, before the backend has
// answered: what shows then must ask nothing and claim nothing.

describe('components/session / SessionStep', () => {
  it('asks nothing before the backend says what the case needs', () => {
    const { body } = render(SessionStep);
    expect(body).toContain(UI.SESSION_TITLE);
    expect(body).not.toContain('type="password"');
    expect(body).not.toContain('<textarea');
  });
});

describe('components/maintenance / SigningCard', () => {
  it('says what a signature establishes, and no more', () => {
    const { body } = render(SigningCard);
    expect(body).toContain(UI.SIGNING_TITLE);
    expect(body).toContain('proves nothing by itself');
    expect(body).not.toContain(UI.SIGNING_REQUEST);
  });
});

describe('components/material / StatusBar', () => {
  it('marks nothing read-only until the session says so', () => {
    const { body } = render(StatusBar);
    expect(body).not.toContain(UI.STATUS_READ_ONLY);
  });
});
