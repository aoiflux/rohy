import { describe, it, expect } from 'vitest';
import { render } from 'svelte/server';
import MaintenanceView from '../../routes/MaintenanceView.svelte';
import { ACTION, runnable, actionsIn } from './report.js';
import { UI } from '../consts/index.js';

// Forensic posture in the UI (plan §14.8 / §14.2).
//
// The assertions here are almost all about COPY and about which of four states is shown, because
// that is where this feature can actually do harm. A wrong number in a chart is a wrong number; a
// case that reads as verified when nothing was verified is a false statement about evidence, and
// it is believed precisely when it matters.

describe('lib/maintenance / forensic actions', () => {
  it('compact and verify are buttons; retaining a root is not', () => {
    expect(runnable(ACTION.COMPACT)).toBe(true);
    expect(runnable(ACTION.VERIFY_IMAGE)).toBe(true);
    // 🔒 Not a shortcoming. A value rohy stored for you would sit in the same folder as the
    // evidence, where anything able to change one could change the other. Keeping it elsewhere is
    // the entire mechanism, so there is nothing here for a button to do.
    expect(runnable(ACTION.RETAIN_ROOT)).toBe(false);
  });

  it('offers each posture fix once, however many findings ask for it', () => {
    const report = {
      findings: [
        { code: 'never_compacted', severity: 'warning', action: ACTION.COMPACT },
        { code: 'image_uncovered', severity: 'warning', action: ACTION.COMPACT },
        { code: 'image_unverified', severity: 'warning', action: ACTION.VERIFY_IMAGE },
        { code: 'no_root_retained', severity: 'warning', action: ACTION.RETAIN_ROOT },
      ],
    };
    const actions = actionsIn(report);
    expect(actions).toContain(ACTION.COMPACT);
    expect(actions).toContain(ACTION.VERIFY_IMAGE);
    expect(actions).not.toContain(ACTION.RETAIN_ROOT);
    expect(actions.filter((a) => a === ACTION.COMPACT)).toHaveLength(1);
  });

  it('every runnable posture action has a button label', () => {
    // A finding whose action has no label renders an empty button, which is worse than advice.
    for (const action of [ACTION.COMPACT, ACTION.VERIFY_IMAGE]) {
      expect(UI.MAINT_ACTION[action], `no label for ${action}`).toBeTruthy();
    }
    expect(UI.MAINT_ADVICE[ACTION.RETAIN_ROOT], 'no advice text for retain_root').toBeTruthy();
  });

  it('has copy for all four image outcomes', () => {
    // 🔒 Four, not two. Missing copy for `no_image` or `absent` would render the raw enum value,
    // which reads as a diagnostic code rather than as "nothing was checked".
    for (const state of ['match', 'mismatch', 'absent', 'no_image']) {
      expect(UI.MAINT_IMAGE[state], `no copy for image state ${state}`).toBeTruthy();
    }
  });

  it('never claims the case is unaltered — only that it is unchanged since it was sealed', () => {
    // The engine can make alteration DETECTABLE to someone holding a value kept elsewhere. It
    // cannot prevent it. Copy that promised more would be believed exactly when it matters.
    const match = UI.MAINT_IMAGE.match.toLowerCase();
    expect(match).toContain('sealed');
    expect(match).toMatch(/kept elsewhere|elsewhere/);
    // And it must not present as a bare guarantee.
    expect(match).not.toMatch(/\bcannot be (altered|changed|tampered)\b/);
    expect(match).not.toMatch(/\bproves? that the (case|evidence) is\b/);
  });

  it('states that an unsigned case cannot be attributed retrospectively', () => {
    // This is the one thing about the unsigned posture that changes what an analyst should do
    // now rather than later, so it must be in the copy and not only in the plan.
    const text = UI.MAINT_FORENSIC_UNSIGNED.toLowerCase();
    expect(text).toMatch(/cannot be added afterwards|retrospectiv/);
  });

  it('says an unsealed case is not a case with anything missing', () => {
    // "Nothing can be proven yet" and "events are missing" are completely different situations
    // and a reader who conflates them goes looking for data loss that never happened.
    expect(UI.MAINT_FORENSIC_UNSEALED.toLowerCase()).toContain('not the same as');
  });
});

describe('routes / MaintenanceView — forensic posture', () => {
  // onMount does not run on the server, so the posture is null here: the state the page is in
  // before the read returns, and also when the backend is unavailable.
  it('shows the posture as unread rather than as clean before it is read', () => {
    const out = render(MaintenanceView, {});
    expect(out.body).toContain(UI.MAINT_FORENSIC_TITLE);
    expect(out.body).toContain(UI.MAINT_FORENSIC_UNREAD);
    // 🔒 It must not assert anything about sealing or signing on the strength of no data.
    expect(out.body).not.toContain(UI.MAINT_FORENSIC_SEALED);
    expect(out.body).not.toContain(UI.MAINT_FORENSIC_SIGNED);
    expect(out.body).not.toContain(UI.MAINT_FORENSIC_UNSIGNED);
  });

  it('does not offer the image check before there is an image to check', () => {
    // The card is gated on a compacted case: a button that could only report "nothing to check"
    // is a button that teaches people the check does nothing.
    const out = render(MaintenanceView, {});
    expect(out.body).not.toContain(UI.MAINT_IMAGE_TITLE);
  });

  it('still leads with the honest no-check-run state', () => {
    // Guards against the new cards displacing the assertion the original screen was built on.
    const out = render(MaintenanceView, {});
    expect(out.body).toContain('No check has been run');
    expect(out.body).not.toContain('Nothing wrong found');
  });
});
