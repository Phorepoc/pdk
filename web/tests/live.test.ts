import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { createPinia, setActivePinia } from 'pinia';
import { LiveSocket, streamUrl } from '@/api/ws';
import { setToken } from '@/api/client';
import { LINE_CAP, useLiveStore } from '@/stores/live';
import { useDevicesStore } from '@/stores/devices';
import type { Health, StreamMsg } from '@/api/types';

class FakeWS {
  static instances: FakeWS[] = [];
  readyState = 0;
  onopen: (() => void) | null = null;
  onclose: (() => void) | null = null;
  onmessage: ((e: { data: string }) => void) | null = null;
  onerror: (() => void) | null = null;
  constructor(public url: string) { FakeWS.instances.push(this); }
  open() { this.readyState = 1; this.onopen?.(); }
  close() { this.readyState = 3; this.onclose?.(); }
}
const health = (device: string, extra: Partial<Health> = {}): Health =>
  ({ device, driver: 'p', transport: 'serial', state: 'connected', last_byte_age_s: 0, reconnects: 0, boot: 1, lines: 0, ...extra });
const line = (id: number, text = 'x'): StreamMsg => ({ type: 'line', device: 'd', data: { id, device: 'd', boot: 1, ts: id, text } });

describe('LiveSocket', () => {
  beforeEach(() => { FakeWS.instances = []; vi.useFakeTimers(); vi.stubGlobal('WebSocket', FakeWS); setToken(''); });
  afterEach(() => { vi.useRealTimers(); vi.unstubAllGlobals(); });

  it('reconnects with exponential backoff and resets after a successful open', () => {
    const onClose = vi.fn();
    const s = new LiveSocket({ onMessage: () => undefined, onClose, minDelay: 100, maxDelay: 400 });
    s.start();
    expect(FakeWS.instances).toHaveLength(1);
    FakeWS.instances[0]!.close(); // never opened
    expect(onClose).toHaveBeenLastCalledWith(false);
    vi.advanceTimersByTime(99); expect(FakeWS.instances).toHaveLength(1);
    vi.advanceTimersByTime(1); expect(FakeWS.instances).toHaveLength(2);
    FakeWS.instances[1]!.close();
    vi.advanceTimersByTime(199); expect(FakeWS.instances).toHaveLength(2);
    vi.advanceTimersByTime(1); expect(FakeWS.instances).toHaveLength(3);
    FakeWS.instances[2]!.open();
    FakeWS.instances[2]!.close();
    expect(onClose).toHaveBeenLastCalledWith(true);
    vi.advanceTimersByTime(100); expect(FakeWS.instances).toHaveLength(4); // delay reset to min
    s.stop();
    vi.advanceTimersByTime(10000); expect(FakeWS.instances).toHaveLength(4);
  });

  it('parses messages and ignores garbage', () => {
    const got: StreamMsg[] = [];
    const s = new LiveSocket({ onMessage: (m) => got.push(m) });
    s.start();
    const ws = FakeWS.instances[0]!;
    ws.open();
    ws.onmessage?.({ data: JSON.stringify(line(1)) });
    ws.onmessage?.({ data: 'not json' });
    expect(got).toHaveLength(1);
    s.stop();
  });

  it('puts the token in the URL', () => {
    setToken('tok');
    expect(streamUrl(['line', 'event'])).toMatch(/^ws:\/\/.+\/api\/stream\?types=line%2Cevent&token=tok$/);
  });
});

describe('live store', () => {
  beforeEach(() => { setActivePinia(createPinia()); vi.useFakeTimers(); });
  afterEach(() => vi.useRealTimers());

  it('caps the line ring', () => {
    const live = useLiveStore();
    for (let i = 1; i <= LINE_CAP + 2000; i++) live.dispatch(line(i));
    expect(live.lines.length).toBeLessThanOrEqual(LINE_CAP + 500);
    expect(live.lines.length).toBeGreaterThanOrEqual(LINE_CAP);
    expect(live.lines[live.lines.length - 1]!.id).toBe(LINE_CAP + 2000);
  });

  it('dispatches by message type', () => {
    const live = useLiveStore();
    const devices = useDevicesStore();
    devices.list = [{ id: 'd', driver: 'p', health: health('d'), ops: [] }];
    const seen: string[] = [];
    live.onEvent((e) => seen.push(e.kind));

    live.dispatch({ type: 'hello', device: '', data: { ok: true, version: 'v', uptime_s: 1, bus_dropped: 7, devices: [health('d', { slot: 'A' })] } });
    expect(devices.list[0]!.health.slot).toBe('A');
    expect(live.dropped).toBe(7);

    live.dispatch({ type: 'state', device: 'd', data: health('d', { state: 'disconnected' }) });
    expect(devices.list[0]!.health.state).toBe('disconnected');

    live.dispatch({ type: 'event', device: 'd', data: { id: 1, device: 'd', boot: 1, ts: 1, kind: 'boot.slot', data: { slot: 'B' } } });
    expect(live.events[0]!.kind).toBe('boot.slot');
    expect(seen).toEqual(['boot.slot']);

    live.dispatch({ type: 'progress', device: 'd', data: { op: 'push', stage: 'send', done: 5, total: 10 } });
    expect(live.progress['d']).toMatchObject({ stage: 'send', done: 5 });
    live.dispatch({ type: 'op', device: 'd', data: { id: 1, device: 'd', ts: 1, caller: 'web', op: 'push', args: {}, result: null, error: '', ms: 3 } });
    expect(live.progress['d']).toBeUndefined();
    expect(live.lastOp?.op).toBe('push');
  });

  it('ignores a replayed line already in the ring', () => {
    const live = useLiveStore();
    live.dispatch(line(5)); live.dispatch(line(5)); live.dispatch(line(4));
    expect(live.lines).toHaveLength(1);
  });
});
