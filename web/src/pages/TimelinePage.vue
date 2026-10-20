<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-page class="page-pad">
    <q-tabs v-model="tab" dense align="left" no-caps class="q-mb-sm">
      <q-tab name="boots" label="Boots" />
      <q-tab name="ops" label="Ops audit" />
      <q-tab name="events" label="Events" />
    </q-tabs>
    <div v-if="error" class="text-negative q-mb-sm">{{ error }}</div>

    <q-tab-panels v-model="tab" animated keep-alive class="bg-transparent">
      <q-tab-panel name="boots" class="q-pa-none">
        <q-markup-table dense flat bordered separator="horizontal">
          <thead><tr><th class="text-left">boot</th><th class="text-left">started</th><th class="text-left">slot</th><th class="text-left">outcome</th><th class="text-right">lines</th><th class="text-right">faults</th><th>pin</th></tr></thead>
          <tbody>
            <tr v-for="b in boots" :key="b.id">
              <td><router-link :to="{ path: '/log', query: { boot: String(b.id) } }">#{{ b.id }}</router-link></td>
              <td>{{ fmtTs(b.started) }}</td>
              <td>{{ b.slot }}</td>
              <td><q-badge :color="outcomeColor(b.outcome)" :label="b.outcome || 'unknown'" /></td>
              <td class="text-right">{{ b.lines }}</td>
              <td class="text-right" :class="b.faults ? 'text-negative text-weight-bold' : ''">{{ b.faults }}</td>
              <td class="text-center"><q-btn flat dense round :icon="b.pinned ? 'push_pin' : 'o_push_pin'" :color="b.pinned ? 'primary' : 'grey'" aria-label="pin" @click="togglePin(b)" /></td>
            </tr>
            <tr v-if="!boots.length"><td colspan="7" class="text-grey">no boots recorded</td></tr>
          </tbody>
        </q-markup-table>
        <q-btn v-if="boots.length >= 50" flat no-caps label="Load older" class="q-mt-sm" @click="moreBoots" />
      </q-tab-panel>

      <q-tab-panel name="ops" class="q-pa-none">
        <q-list bordered separator dense>
          <q-expansion-item v-for="o in ops" :key="o.id" dense group="ops">
            <template #header>
              <q-item-section side style="min-width: 150px" class="text-caption">{{ fmtTs(o.ts) }}</q-item-section>
              <q-item-section side><q-badge :color="o.error ? 'negative' : 'positive'" :label="o.op" /></q-item-section>
              <q-item-section class="ellipsis mono text-caption">{{ brief(o.args) }}</q-item-section>
              <q-item-section side class="text-caption">{{ o.caller }} {{ o.ms }}ms</q-item-section>
            </template>
            <div class="q-pa-sm">
              <div class="text-caption text-grey">args</div><pre class="json mono">{{ pretty(o.args) }}</pre>
              <div v-if="o.error" class="text-negative">error: {{ o.error }}</div>
              <div class="text-caption text-grey">result</div><pre class="json mono">{{ pretty(o.result) }}</pre>
            </div>
          </q-expansion-item>
          <q-item v-if="!ops.length"><q-item-section class="text-grey">no ops recorded</q-item-section></q-item>
        </q-list>
        <q-btn v-if="ops.length >= 50" flat no-caps label="Load older" class="q-mt-sm" @click="moreOps" />
      </q-tab-panel>

      <q-tab-panel name="events" class="q-pa-none">
        <div class="row q-col-gutter-sm items-center q-mb-sm">
          <q-input v-model="kind" dense outlined clearable class="col-12 col-sm-5" label="kinds (comma list, prefix with trailing dot)" @keyup.enter="loadEvents" />
          <div class="col row q-gutter-xs">
            <q-chip v-for="k in kindPresets" :key="k" clickable dense outline size="sm" :label="k" @click="kind = k; loadEvents()" />
          </div>
          <q-btn color="primary" dense no-caps label="Apply" @click="loadEvents" />
        </div>
        <event-list :events="events" show-boot />
        <q-btn v-if="events.length >= 100" flat no-caps label="Load older" class="q-mt-sm" @click="moreEvents" />
      </q-tab-panel>
    </q-tab-panels>
  </q-page>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from 'vue';
import { api } from '@/api/client';
import type { Boot, DeviceEvent, OpRecord } from '@/api/types';
import EventList from '@/components/EventList.vue';
import { useDevicesStore } from '@/stores/devices';
import { fmtTs, pretty } from '@/utils';

const devices = useDevicesStore();
const tab = ref('boots');
const error = ref('');
const boots = ref<Boot[]>([]);
const ops = ref<OpRecord[]>([]);
const events = ref<DeviceEvent[]>([]);
const kind = ref<string | null>('boot.,fault,panic,upload.,link.');
const kindPresets = ['boot.', 'fault,panic', 'upload.', 'link.', 'mon.', 'key.'];

const outcomeColor = (o: string) => (/ok|good|healthy/i.test(o) ? 'positive' : /fail|fault|panic|bad/i.test(o) ? 'negative' : 'grey');
const brief = (a: unknown) => (a ? JSON.stringify(a) : '');
const last = <T extends { id: number }>(a: T[]) => a[a.length - 1]?.id ?? 0;

async function guard(f: () => Promise<void>): Promise<void> {
  error.value = '';
  try { await f(); } catch (e) { error.value = (e as Error).message; }
}
const loadBoots = () => guard(async () => { boots.value = await api.boots(devices.currentId, 0, 50); });
const moreBoots = () => guard(async () => { boots.value.push(...await api.boots(devices.currentId, last(boots.value), 50)); });
const loadOps = () => guard(async () => { ops.value = await api.history(devices.currentId, 0, 50); });
const moreOps = () => guard(async () => { ops.value.push(...await api.history(devices.currentId, last(ops.value), 50)); });
const loadEvents = () => guard(async () => { events.value = await api.events(devices.currentId, { kind: kind.value ?? '', limit: 100 }); });
const moreEvents = () => guard(async () => {
  events.value.push(...await api.events(devices.currentId, { kind: kind.value ?? '', limit: 100, before: last(events.value) }));
});

async function togglePin(b: Boot): Promise<void> {
  await guard(async () => { await api.pin('boot', b.id, !b.pinned); b.pinned = !b.pinned; });
}

function loadAll(): void {
  if (!devices.currentId) return;
  void loadBoots(); void loadOps(); void loadEvents();
}
onMounted(loadAll);
watch(() => devices.currentId, loadAll);
</script>
