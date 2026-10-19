import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { FramePoller, Gesture, InputQueue, centreSwipe, mapKeyboardKey, mapPoint, type FrameResult } from '@/api/remote';

describe('coordinate mapping', () => {
  it('which 2 (composite) doubles into panel pixels', () => {
    expect(mapPoint(2, 100, 359.6)).toEqual({ x: 200, y: 720, panel: true });
  });
  it('which 0 is OS pixels without the panel flag', () => {
    expect(mapPoint(0, 10, 20)).toEqual({ x: 10, y: 20, panel: false });
  });
  it('which 1 is already panel pixels', () => {
    expect(mapPoint(1, 10, 20)).toEqual({ x: 10, y: 20, panel: true });
  });
  it('centre swipes map the same way', () => {
    expect(centreSwipe(2, 360, 720, 'up')).toMatchObject({ x1: 360, x2: 360, y1: 1152, y2: 288, panel: true });
    expect(centreSwipe(0, 360, 640, 'left')).toMatchObject({ y1: 320, y2: 320, panel: false });
  });
  it('maps keyboard keys', () => {
    expect(mapKeyboardKey('ArrowUp')).toBe('up');
    expect(mapKeyboardKey('Enter')).toBe('select');
    expect(mapKeyboardKey('F2')).toBe('rsk');
    expect(mapKeyboardKey('Q')).toBe('q');
    expect(mapKeyboardKey('Shift')).toBeNull();
  });
});

describe('FramePoller', () => {
  beforeEach(() => vi.useFakeTimers());
  afterEach(() => vi.useRealTimers());

  function make(statuses: number[], interval = 1000) {
    let inflight = 0, maxInflight = 0, calls = 0;
    const frames: number[] = [], busy: number[] = [], errs: number[] = [];
    const p = new FramePoller<FrameResult>({
      fetchFrame: async () => {
        inflight++; maxInflight = Math.max(maxInflight, inflight);
        calls++;
        const s = statuses[Math.min(calls - 1, statuses.length - 1)]!;
        await new Promise((r) => setTimeout(r, 300)); // capture time
        inflight--;
        return s === 200 ? { status: s } : { status: s, error: 'x' };
      },
      interval: () => interval,
      onFrame: () => frames.push(calls),
      onBusy: () => busy.push(calls),
      onError: () => errs.push(calls),
    });
    return { p, frames, busy, errs, calls: () => calls, maxInflight: () => maxInflight };
  }

  it('never overlaps and waits the interval after each request finishes', async () => {
    const t = make([200]);
    t.p.start();
    await vi.advanceTimersByTimeAsync(0); expect(t.calls()).toBe(1);
    await vi.advanceTimersByTimeAsync(299); expect(t.frames).toHaveLength(0);
    await vi.advanceTimersByTimeAsync(1); expect(t.frames).toHaveLength(1);
    await vi.advanceTimersByTimeAsync(999); expect(t.calls()).toBe(1); // interval counts from the end
    await vi.advanceTimersByTimeAsync(1); expect(t.calls()).toBe(2);
    await vi.advanceTimersByTimeAsync(10000);
    expect(t.maxInflight()).toBe(1);
    t.p.pause();
  });

  it('pause stops further requests, resume restarts', async () => {
    const t = make([200]);
    t.p.start();
    await vi.advanceTimersByTimeAsync(400);
    t.p.pause();
    const n = t.calls();
    await vi.advanceTimersByTimeAsync(10000);
    expect(t.calls()).toBe(n);
    t.p.start();
    await vi.advanceTimersByTimeAsync(1);
    expect(t.calls()).toBe(n + 1);
    t.p.pause();
  });

  it('skips a 409 quietly and carries on at the normal interval; errors back off', async () => {
    const t = make([409, 200, 502, 200]);
    t.p.start();
    await vi.advanceTimersByTimeAsync(300); // 409 done
    expect(t.busy).toEqual([1]); expect(t.errs).toEqual([]);
    await vi.advanceTimersByTimeAsync(1000 + 300); // frame
    expect(t.frames).toEqual([2]);
    await vi.advanceTimersByTimeAsync(1000 + 300); // 502
    expect(t.errs).toEqual([3]);
    await vi.advanceTimersByTimeAsync(4999); expect(t.calls()).toBe(3); // 5 s error backoff
    await vi.advanceTimersByTimeAsync(1); expect(t.calls()).toBe(4);
    t.p.pause();
  });
});

describe('Gesture', () => {
  let t = 0;
  let sent: Array<[string, Record<string, unknown>]>;
  let drag = false;
  let which: 0 | 1 | 2 = 2;
  const mk = () => new Gesture({ which: () => which, dragMode: () => drag, now: () => t, send: (o, a) => { sent.push([o, a]); } });
  beforeEach(() => { vi.useFakeTimers(); t = 0; sent = []; drag = false; which = 2; });
  afterEach(() => vi.useRealTimers());

  it('a quick click is a single tap in panel pixels', () => {
    const g = mk();
    g.down(100, 200); t = 100; g.up(101, 200);
    expect(sent).toEqual([['touch', { x: 200, y: 400, panel: true, action: 'tap' }]]);
  });

  it('a drag streams down / throttled moves / up', () => {
    const g = mk();
    g.down(10, 10);
    g.move(10, 40); // beyond slop: down + first move
    expect(sent.map((s) => s[1].action)).toEqual(['down', 'move']);
    t = 10; g.move(10, 50); // inside the 50 ms gap: dropped
    t = 80; g.move(10, 60);
    t = 90; g.up(10, 60);
    expect(sent.map((s) => s[1].action)).toEqual(['down', 'move', 'move', 'up']);
  });

  it('a press held past 250 ms becomes touch down, then up', () => {
    const g = mk();
    g.down(5, 5);
    t = 300; vi.advanceTimersByTime(300);
    expect(sent.map((s) => s[1].action)).toEqual(['down']);
    t = 900; g.up(5, 5);
    expect(sent.map((s) => s[1].action)).toEqual(['down', 'up']);
  });

  it('long press (right click) is one tap with hold_ms 1000', () => {
    const g = mk();
    g.down(5, 6, true); g.move(50, 60); t = 50; g.up(50, 60);
    expect(sent).toEqual([['touch', { x: 10, y: 12, panel: true, action: 'tap', hold_ms: 1000 }]]);
  });

  it('drag mode sends ONE swipe with the drag duration and no streamed moves', () => {
    drag = true;
    const g = mk();
    g.down(100, 300); g.move(100, 200); g.move(100, 100); t = 420; g.up(100, 50);
    expect(sent).toEqual([['swipe', { x1: 200, y1: 600, x2: 200, y2: 100, ms: 420, panel: true }]]);
  });

  it('drag mode: OS-only view swipes without panel flag; tiny drag is a tap', () => {
    drag = true; which = 0;
    const g = mk();
    g.down(10, 300); t = 200; g.up(10, 100);
    expect(sent[0]).toEqual(['swipe', { x1: 10, y1: 300, x2: 10, y2: 100, ms: 200, panel: false }]);
    sent.length = 0;
    g.down(10, 10); t = 300; g.up(11, 10);
    expect(sent).toEqual([['touch', { x: 10, y: 10, panel: false, action: 'tap' }]]);
  });

  it('cancel while streaming releases the touch', () => {
    const g = mk();
    g.down(0, 0); g.move(0, 50); g.cancel();
    expect(sent[sent.length - 1]![1].action).toBe('up');
  });
});

describe('InputQueue', () => {
  it('keeps order and collapses queued moves', async () => {
    const done: string[] = [];
    let release: (() => void) | null = null;
    const q = new InputQueue((c) => new Promise<void>((res) => { done.push(`${c.op}:${String(c.args.action ?? c.args.key)}`); release = res; }));
    q.push('touch', { action: 'down' });
    q.push('touch', { action: 'move', y: 1 });
    q.push('touch', { action: 'move', y: 2 });
    q.push('touch', { action: 'up' });
    expect(q.pending).toBe(2); // 'down' already in flight; the two moves collapsed
    for (let i = 0; i < 5; i++) { await Promise.resolve(); (release as (() => void) | null)?.(); await new Promise((r) => setTimeout(r, 0)); }
    expect(done).toEqual(['touch:down', 'touch:move', 'touch:up']);
  });
});
