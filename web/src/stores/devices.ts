// Copyright The Phorepoc Authors
// SPDX-License-Identifier: Apache-2.0

import { defineStore } from 'pinia';
import { computed, ref } from 'vue';
import { api } from '@/api/client';
import type { DeviceInfo, Health, OpSpec } from '@/api/types';
import { useSettingsStore } from './settings';

/** Device list, op specs and the latest health of each device (kept fresh by the live store). */
export const useDevicesStore = defineStore('devices', () => {
  const list = ref<DeviceInfo[]>([]);
  const loaded = ref(false);
  const error = ref('');
  const version = ref('');
  const settings = useSettingsStore();

  const current = computed<DeviceInfo | undefined>(
    () => list.value.find((d) => d.id === settings.device) ?? list.value[0],
  );
  const currentId = computed(() => current.value?.id ?? '');

  async function load(): Promise<void> {
    try {
      list.value = await api.devices();
      error.value = '';
      loaded.value = true;
      if (!list.value.some((d) => d.id === settings.device)) settings.device = list.value[0]?.id ?? '';
    } catch (e) {
      error.value = (e as Error).message;
    }
  }

  function setHealth(h: Health): void {
    const d = list.value.find((x) => x.id === h.device);
    if (d) d.health = h;
  }

  /** when each device's monitor last reported (ms) */
  const monAt = ref<Record<string, number>>({});
  function setMonitor(id: string, kind: string, data: unknown): void {
    monAt.value = { ...monAt.value, [id]: Date.now() };
    const d = list.value.find((x) => x.id === id);
    if (d) d.health = { ...d.health, monitor: { ...(d.health.monitor ?? {}), [kind]: data } } as Health;
  }

  function opsOf(id: string): OpSpec[] {
    return list.value.find((d) => d.id === id)?.ops ?? [];
  }
  function hasOp(id: string, name: string): boolean {
    return opsOf(id).some((o) => o.name === name);
  }

  return { list, loaded, error, version, current, currentId, monAt, load, setHealth, setMonitor, opsOf, hasOp };
});
