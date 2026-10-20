// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia';
import { computed, ref, watch } from 'vue';
import { Notify } from 'quasar';
import { api } from '@/api/client';
import { InputQueue, centreSwipe, type Which } from '@/api/remote';
import { useDevicesStore } from './devices';

const KEY = 'phonelab.remote.v1';

/** Remote control state shared by the Live screen and the Phone controls view (either can be open alone or both). */
export const useRemoteStore = defineStore('remote', () => {
  const devices = useDevicesStore();
  const id = computed(() => devices.currentId);

  let saved: Record<string, unknown> = {};
  try { saved = JSON.parse(localStorage.getItem(KEY) ?? '{}') as Record<string, unknown>; } catch { /* defaults */ }
  const paused = ref(false);
  const intervalS = ref((saved.intervalS as number) ?? 1);
  const store = ref(false);
  const zoom = ref<'fit' | '1' | '2'>((saved.zoom as 'fit' | '1' | '2') ?? 'fit');
  const which = ref<Which>((saved.which as Which) ?? 2);
  const dragMode = ref(!!saved.dragMode);
  /** the Live screen's collapsible drawers */
  const keypadOpen = ref(saved.keypadOpen !== false);
  const settingsOpen = ref(!!saved.settingsOpen);
  const frameSize = ref<{ w: number; h: number } | null>(null);

  watch([intervalS, zoom, which, dragMode, keypadOpen, settingsOpen], () => {
    try {
      localStorage.setItem(KEY, JSON.stringify({ intervalS: intervalS.value, zoom: zoom.value, which: which.value,
        dragMode: dragMode.value, keypadOpen: keypadOpen.value, settingsOpen: settingsOpen.value }));
    } catch { /* ignore */ }
  });

  // the Live screen registers its poller here so input from anywhere shows its effect soon
  let pokeFn: (() => void) | null = null;
  let snapFn: (() => void) | null = null;
  function onPoke(fn: (() => void) | null, snap: (() => void) | null = null): void { pokeFn = fn; snapFn = snap; }

  const queue = new InputQueue(
    (c) => api.op(id.value, c.op, c.args),
    (e, c) => { Notify.create({ type: 'negative', message: `${c.op}: ${(e as Error).message}`, timeout: 2500 }); },
  );
  function send(op: string, args: Record<string, unknown>): void {
    queue.push(op, args);
    if (op !== 'key' || args.action !== 'down') setTimeout(() => pokeFn?.(), 400);
  }
  function swipe(dir: 'up' | 'down' | 'left' | 'right'): void {
    const w = frameSize.value?.w || 360;
    const h = frameSize.value?.h || (which.value === 2 ? 720 : 640);
    send('swipe', centreSwipe(which.value, w, h, dir));
  }
  async function snapshot(): Promise<void> {
    if (snapFn) { snapFn(); Notify.create({ message: 'Snapshot requested', timeout: 1000 }); return; }
    const r = await api.frame(id.value, which.value, true);
    Notify.create({ type: r.status === 200 ? 'positive' : 'negative', message: r.status === 200 ? 'Snapshot saved' : `snapshot: ${r.error}`, timeout: 1500 });
  }
  /** press-and-hold buttons: pointer down = key down, up = key up */
  function hold(key: string): Record<string, unknown> {
    return {
      onPointerdown: (e: PointerEvent) => { (e.currentTarget as Element).setPointerCapture?.(e.pointerId); send('key', { key, action: 'down' }); },
      onPointerup: () => send('key', { key, action: 'up' }),
      onPointercancel: () => send('key', { key, action: 'up' }),
      onClick: (e: MouseEvent) => e.preventDefault(),
      style: 'touch-action: none',
    };
  }

  return { id, paused, intervalS, store, zoom, which, dragMode, keypadOpen, settingsOpen, frameSize, onPoke, send, swipe, snapshot, hold };
});
