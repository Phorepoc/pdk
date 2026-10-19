import { defineStore } from 'pinia';
import { ref, shallowRef, triggerRef } from 'vue';
import { api } from '@/api/client';
import { LiveSocket } from '@/api/ws';
import type { DeviceEvent, Line, OpRecord, Progress, StreamMsg } from '@/api/types';
import { useDevicesStore } from './devices';

export const LINE_CAP = 5000;
export const EVENT_CAP = 300;

/** The one WebSocket + the in-memory live log ring. Everything the pages show "live" comes from here. */
export const useLiveStore = defineStore('live', () => {
  const devices = useDevicesStore();

  // The ring is a plain array in a shallowRef: pushing thousands of lines through deep reactivity is
  // expensive, so we mutate and trigger at most once per tick.
  const lines = shallowRef<Line[]>([]);
  const events = shallowRef<DeviceEvent[]>([]); // newest first
  const progress = ref<Record<string, Progress | undefined>>({});
  const lastOp = shallowRef<OpRecord | null>(null);
  const connected = ref(false);
  const dropped = ref(0);
  let pending = false;
  let sock: LiveSocket | null = null;
  const eventListeners = new Set<(e: DeviceEvent) => void>();

  function scheduleTrigger(): void {
    if (pending) return;
    pending = true;
    setTimeout(() => { pending = false; triggerRef(lines); triggerRef(events); }, 50);
  }

  function pushLine(l: Line): void {
    const arr = lines.value;
    const last = arr[arr.length - 1];
    if (last && l.id <= last.id && l.device === last.device) return; // overlap with a seed
    arr.push(l);
    if (arr.length > LINE_CAP + 500) arr.splice(0, arr.length - LINE_CAP);
    scheduleTrigger();
  }

  function pushEvent(e: DeviceEvent): void {
    const arr = events.value;
    arr.unshift(e);
    if (arr.length > EVENT_CAP) arr.length = EVENT_CAP;
    scheduleTrigger();
    eventListeners.forEach((fn) => fn(e));
  }

  function dispatch(m: StreamMsg): void {
    switch (m.type) {
      case 'hello':
        dropped.value = m.data?.bus_dropped ?? 0;
        m.data?.devices?.forEach((h) => devices.setHealth(h));
        break;
      case 'line': pushLine(m.data); break;
      case 'event':
        // the server folds mon.* (cpu, ram, sd io) into health but only sends a state message on link changes:
        // keep the device's monitor values fresh from the events themselves
        if (m.data.kind.startsWith('mon.')) devices.setMonitor(m.data.device, m.data.kind, m.data.data);
        pushEvent(m.data);
        break;
      case 'state': devices.setHealth(m.data); break;
      case 'progress': progress.value = { ...progress.value, [m.device]: m.data }; break;
      case 'op':
        lastOp.value = m.data;
        if (progress.value[m.device]) progress.value = { ...progress.value, [m.device]: undefined };
        break;
    }
  }

  /** Backfill the ring with the newest history so the tail is not empty on page load. */
  async function seed(device: string, n = 300): Promise<void> {
    if (!device) return;
    try {
      const [ls, es] = await Promise.all([api.lines(device, { limit: n }), api.events(device, { limit: 50 })]);
      const have = new Set(lines.value.filter((l) => l.device === device).map((l) => l.id));
      const fresh = ls.filter((l) => !have.has(l.id));
      lines.value = [...fresh, ...lines.value].sort((a, b) => a.ts - b.ts || a.id - b.id).slice(-LINE_CAP);
      if (!events.value.length) events.value = es;
    } catch { /* the page shows its own error state */ }
  }

  function start(): void {
    if (sock) return;
    sock = new LiveSocket({
      onMessage: dispatch,
      onOpen: () => { connected.value = true; },
      onClose: (everOpened) => {
        connected.value = false;
        // a refused handshake looks identical for 401 and "server down": probe so the 401 handler fires
        if (!everOpened) void api.health().catch(() => undefined);
      },
    });
    sock.start();
  }
  function stop(): void { sock?.stop(); sock = null; connected.value = false; }
  function restart(): void { sock?.restart(); }
  function clear(): void { lines.value = []; triggerRef(lines); }
  function onEvent(fn: (e: DeviceEvent) => void): () => void {
    eventListeners.add(fn);
    return () => eventListeners.delete(fn);
  }

  return { lines, events, progress, lastOp, connected, dropped, dispatch, seed, start, stop, restart, clear, onEvent };
});
