import { describe, it, expect } from 'vitest';
import { createHistory } from './history.js';

/** A controllable clock, so the coalescing window can be tested without waiting. */
function clock(start = 0) {
  let t = start;
  return { now: () => t, advance: (ms) => (t += ms) };
}

describe('createHistory', () => {
  it('starts with the initial state and nothing to undo', () => {
    const h = createHistory('a');
    expect(h.current()).toBe('a');
    expect(h.canUndo()).toBe(false);
    expect(h.canRedo()).toBe(false);
  });

  it('walks backwards and forwards', () => {
    const h = createHistory('a');
    h.push('b');
    h.push('c');
    expect(h.undo()).toBe('b');
    expect(h.undo()).toBe('a');
    expect(h.undo()).toBeNull();
    expect(h.redo()).toBe('b');
    expect(h.current()).toBe('b');
  });

  // Without coalescing, undo would step back one character at a time through a
  // paragraph-long description, which is not what anyone means by undo.
  it('folds consecutive typing into one entry', () => {
    const c = clock();
    const h = createHistory('', { now: c.now, coalesceMs: 500 });
    h.push('a', { coalesce: true });
    c.advance(50);
    h.push('ab', { coalesce: true });
    c.advance(50);
    h.push('abc', { coalesce: true });

    expect(h.size()).toBe(2);
    expect(h.undo()).toBe('');
  });

  it('starts a new entry once the typing pause is long enough', () => {
    const c = clock();
    const h = createHistory('', { now: c.now, coalesceMs: 500 });
    h.push('a', { coalesce: true });
    c.advance(900);
    h.push('ab', { coalesce: true });
    expect(h.undo()).toBe('a');
  });

  // Every structural action pushes without coalescing, so undo lands on the state before the
  // action rather than somewhere in the middle of it.
  it('never folds a structural change into the typing before it', () => {
    const c = clock();
    const h = createHistory('', { now: c.now, coalesceMs: 500 });
    h.push('{"a":1}', { coalesce: true });
    c.advance(10);
    h.push('{\n  "a": 1\n}'); // a pretty-print, immediately after typing
    expect(h.undo()).toBe('{"a":1}');
  });

  it('does not fold across an undo', () => {
    const c = clock();
    const h = createHistory('a', { now: c.now, coalesceMs: 500 });
    h.push('ab', { coalesce: true });
    h.undo();
    c.advance(10);
    h.push('ac', { coalesce: true });
    // The typing after the undo is its own entry, so undoing again returns to 'a' rather
    // than replacing the state that was just restored.
    expect(h.undo()).toBe('a');
  });

  it('drops the redo tail when a new edit is made', () => {
    const h = createHistory('a');
    h.push('b');
    h.push('c');
    h.undo();
    h.push('d');
    expect(h.canRedo()).toBe(false);
    expect(h.undo()).toBe('b');
  });

  it('bounds how much it keeps', () => {
    const h = createHistory('0', { limit: 5 });
    for (let i = 1; i <= 20; i++) h.push(String(i));
    expect(h.size()).toBe(5);
    expect(h.current()).toBe('20');
    // The oldest states are gone, but the stack is still coherent.
    for (let i = 0; i < 4; i++) h.undo();
    expect(h.canUndo()).toBe(false);
    expect(h.current()).toBe('16');
  });

  it('carries whatever the editor puts in it, not just strings', () => {
    const h = createHistory({ text: 'a', mode: 'raw' });
    h.push({ text: 'a', mode: 'guided' });
    expect(h.undo()).toEqual({ text: 'a', mode: 'raw' });
  });
});
