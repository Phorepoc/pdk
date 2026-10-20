// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

// Pure logic of the Live page: coordinate mapping, the frame poll scheduler, the touch gesture
// recogniser and the serialised input queue. No DOM here so it is unit-testable.

export type Which = 0 | 1 | 2;

export interface Pt { x: number; y: number; panel: boolean }

/** Maps a pixel of the displayed frame to what the touch/swipe ops want.
 *  which 2 = composite (half the panel): panel = image*2; which 1 = panel layer: panel = image;
 *  which 0 = OS screen: OS pixels, no panel flag. */
export function mapPoint(which: Which, ix: number, iy: number): Pt {
  const x = Math.round(ix);
  const y = Math.round(iy);
  if (which === 2) return { x: x * 2, y: y * 2, panel: true };
  if (which === 1) return { x, y, panel: true };
  return { x, y, panel: false };
}

// ---------------------------------------------------------------- frame polling

export interface FrameResult {
  status: number; // HTTP status; 200 ok, 409 busy, 502 error, 0 network
  error?: string;
}

export interface PollerOptions<R extends FrameResult> {
  fetchFrame: () => Promise<R>;
  /** ms between the end of one request and the start of the next */
  interval: () => number;
  onFrame: (r: R) => void;
  onBusy?: () => void;
  onError?: (r: R) => void;
  /** wait after a hard error; default max(interval, 5000) */
  errorDelay?: () => number;
}

/** Sequential poller: the next request starts only after the previous finished + interval, so there is
 *  never more than one in flight. 409 (link busy) is skipped silently; errors back off. */
export class FramePoller<R extends FrameResult> {
  private timer: ReturnType<typeof setTimeout> | null = null;
  private running = false;
  private inflight = false;
  constructor(private o: PollerOptions<R>) {}

  get active(): boolean { return this.running; }
  get busy(): boolean { return this.inflight; }

  start(): void {
    if (this.running) return;
    this.running = true;
    this.schedule(0);
  }

  pause(): void {
    this.running = false;
    if (this.timer) clearTimeout(this.timer);
    this.timer = null;
  }

  /** Fetch now (if idle) -- used right after an input so the result shows up quickly. */
  poke(): void {
    if (!this.running || this.inflight) return;
    this.schedule(0);
  }

  private schedule(ms: number): void {
    if (this.timer) clearTimeout(this.timer);
    this.timer = setTimeout(() => { void this.tick(); }, ms);
  }

  private async tick(): Promise<void> {
    this.timer = null;
    if (!this.running || this.inflight) return;
    this.inflight = true;
    let delay = this.o.interval();
    try {
      const r = await this.o.fetchFrame();
      if (r.status === 200) this.o.onFrame(r);
      else if (r.status === 409) this.o.onBusy?.();
      else { this.o.onError?.(r); delay = this.o.errorDelay ? this.o.errorDelay() : Math.max(delay, 5000); }
    } catch (e) {
      this.o.onError?.({ status: 0, error: (e as Error).message } as R);
      delay = this.o.errorDelay ? this.o.errorDelay() : Math.max(delay, 5000);
    } finally {
      this.inflight = false;
    }
    if (this.running) this.schedule(delay);
  }
}

// ---------------------------------------------------------------- input queue

export interface InputCall { op: string; args: Record<string, unknown> }

/** Sends input ops strictly in order (HTTP requests could otherwise overtake each other). A queued
 *  touch "move" is replaced by a newer move instead of piling up. */
export class InputQueue {
  private q: InputCall[] = [];
  private busy = false;
  constructor(private send: (c: InputCall) => Promise<unknown>, private onError?: (e: unknown, c: InputCall) => void) {}

  get pending(): number { return this.q.length; }

  push(op: string, args: Record<string, unknown>): void {
    const last = this.q[this.q.length - 1];
    const isMove = (c: InputCall | undefined) => c?.op === 'touch' && c.args.action === 'move';
    if (isMove(last) && op === 'touch' && args.action === 'move') this.q[this.q.length - 1] = { op, args };
    else this.q.push({ op, args });
    void this.pump();
  }

  private async pump(): Promise<void> {
    if (this.busy) return;
    this.busy = true;
    try {
      while (this.q.length) {
        const c = this.q.shift() as InputCall;
        try { await this.send(c); } catch (e) { this.onError?.(e, c); }
      }
    } finally { this.busy = false; }
  }
}

// ---------------------------------------------------------------- touch gestures

export interface GestureOptions {
  which: () => Which;
  send: (op: string, args: Record<string, unknown>) => void;
  dragMode: () => boolean;
  now?: () => number;
  /** below this many image pixels of travel a press is a click */
  slop?: number;
  tapMaxMs?: number;
  moveIntervalMs?: number;
  longMs?: number;
}

/** Turns pointer down/move/up (in image pixels) into touch/swipe ops.
 *  - stream mode: a quick click with little movement = one `tap`; otherwise `down`, throttled `move`s, `up`.
 *  - drag mode: the whole drag is ONE swipe (start -> end, duration of the drag).
 *  - long press (right button / modifier): a tap with hold_ms 1000. */
export class Gesture {
  private start: { x: number; y: number; t: number } | null = null;
  private last: { x: number; y: number } = { x: 0, y: 0 };
  private streaming = false;
  private long = false;
  private lastMove = 0;
  private timer: ReturnType<typeof setTimeout> | null = null;
  private slop: number;
  private tapMax: number;
  private moveGap: number;
  private longMs: number;
  /** last touched point in image pixels (for the marker) */
  marker: { x: number; y: number } | null = null;
  constructor(private o: GestureOptions) {
    this.slop = o.slop ?? 6;
    this.tapMax = o.tapMaxMs ?? 250;
    this.moveGap = o.moveIntervalMs ?? 50;
    this.longMs = o.longMs ?? 1000;
  }

  private now(): number { return (this.o.now ?? Date.now)(); }
  private pt(x: number, y: number) { return mapPoint(this.o.which(), x, y); }
  private touch(action: string, x: number, y: number, extra: Record<string, unknown> = {}): void {
    this.o.send('touch', { ...this.pt(x, y), action, ...extra });
  }

  get pressed(): boolean { return this.start !== null; }

  down(x: number, y: number, long = false): void {
    this.cancelTimer();
    this.start = { x, y, t: this.now() };
    this.last = { x, y };
    this.streaming = false;
    this.long = long;
    this.marker = { x, y };
    if (!long && !this.o.dragMode()) {
      // a press held past the tap window becomes a real touch-down (so long holds work)
      this.timer = setTimeout(() => this.beginStream(), this.tapMax);
    }
  }

  private beginStream(): void {
    if (!this.start || this.streaming) return;
    this.streaming = true;
    this.touch('down', this.start.x, this.start.y);
    this.lastMove = -Infinity;
  }

  move(x: number, y: number): void {
    if (!this.start) return;
    this.last = { x, y };
    this.marker = { x, y };
    if (this.long || this.o.dragMode()) return;
    if (!this.streaming) {
      if (Math.hypot(x - this.start.x, y - this.start.y) <= this.slop) return;
      this.cancelTimer();
      this.beginStream();
    }
    const t = this.now();
    if (t - this.lastMove >= this.moveGap) { this.lastMove = t; this.touch('move', x, y); }
  }

  up(x: number, y: number): void {
    const s = this.start;
    if (!s) return;
    this.cancelTimer();
    this.start = null;
    const dur = this.now() - s.t;
    const travel = Math.hypot(x - s.x, y - s.y);
    this.marker = { x, y };
    if (this.long) { this.touch('tap', s.x, s.y, { hold_ms: this.longMs }); return; }
    if (this.o.dragMode()) {
      if (travel <= this.slop) { this.touch('tap', s.x, s.y); return; }
      const a = this.pt(s.x, s.y);
      const b = this.pt(x, y);
      this.o.send('swipe', { x1: a.x, y1: a.y, x2: b.x, y2: b.y, ms: Math.max(100, Math.min(3000, Math.round(dur))), panel: a.panel });
      return;
    }
    if (!this.streaming) {
      if (travel <= this.slop && dur <= this.tapMax) { this.touch('tap', s.x, s.y); return; }
      this.beginStream();
    }
    this.touch('up', x, y);
  }

  cancel(): void {
    const wasStreaming = this.streaming;
    this.cancelTimer();
    if (this.start && wasStreaming) this.touch('up', this.last.x, this.last.y);
    this.start = null;
  }

  private cancelTimer(): void { if (this.timer) clearTimeout(this.timer); this.timer = null; }
}

/** Swipe across the centre of the shown frame (image pixels w x h). */
export function centreSwipe(which: Which, w: number, h: number, dir: 'up' | 'down' | 'left' | 'right', ms = 300): Record<string, unknown> {
  const cx = w / 2, cy = h / 2, dx = w * 0.3, dy = h * 0.3;
  const [x1, y1, x2, y2] = dir === 'up' ? [cx, cy + dy, cx, cy - dy]
    : dir === 'down' ? [cx, cy - dy, cx, cy + dy]
      : dir === 'left' ? [cx + dx, cy, cx - dx, cy] : [cx - dx, cy, cx + dx, cy];
  const a = mapPoint(which, x1, y1);
  const b = mapPoint(which, x2, y2);
  return { x1: a.x, y1: a.y, x2: b.x, y2: b.y, ms, panel: a.panel };
}

/** Browser KeyboardEvent.key -> phone key name, or null if not mapped. */
export function mapKeyboardKey(k: string): string | null {
  const m: Record<string, string> = {
    ArrowUp: 'up', ArrowDown: 'down', ArrowLeft: 'left', ArrowRight: 'right', Enter: 'select', F1: 'lsk', F2: 'rsk',
    Escape: 'red', Backspace: 'backspace',
  };
  if (m[k]) return m[k];
  if (/^[a-z0-9]$/i.test(k)) return k.toLowerCase();
  if (k === '*') return 'star';
  if (k === '#') return 'hash';
  return null;
}
