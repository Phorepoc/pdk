<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div>
    <div class="row items-center q-gutter-sm q-mb-sm">
      <div class="text-subtitle1">Hotspots</div>
      <q-btn dense unelevated no-caps color="primary" icon="fiber_manual_record" label="Start" :disable="busy || running" data-test="hot-start" @click="start" />
      <q-btn dense unelevated no-caps color="negative" icon="stop" label="Stop" :disable="busy || !running" data-test="hot-stop" @click="stop" />
      <q-btn dense outline no-caps icon="timer" :label="`Run ${secs}s`" :disable="busy || running" @click="run" />
      <q-input v-model.number="secs" dense outlined type="number" style="width: 70px" label="s" />
      <q-input v-model="symbols" dense outlined style="min-width: 260px" label="symbol file" />
      <span v-if="running" class="text-caption text-negative">sampling since {{ since }}... do the slow thing on the phone, then Stop</span>
      <q-spinner v-if="busy" size="18px" />
    </div>
    <div class="text-caption text-grey q-mb-sm">Every busy core's user pc every 2 ms. % = share of busy samples.</div>
    <div v-if="error" class="text-negative text-caption q-mb-sm">{{ error }}</div>
    <template v-if="res">
      <div class="text-caption q-mb-sm" data-test="hot-summary">
        {{ res.samples }} busy samples, {{ res.idle_samples }} idle<span v-if="res.lost">, {{ res.lost }} lost</span>
        over {{ res.seconds.toFixed(1) }}s | symbols: {{ res.symbols }}
      </div>
      <div class="row q-col-gutter-md">
        <div class="col-12 col-lg-8">
          <q-markup-table dense flat bordered separator="horizontal" class="mono">
            <thead><tr><th class="text-right">%</th><th class="text-left">function</th><th class="text-left">image</th><th class="text-left">thread</th></tr></thead>
            <tbody>
              <tr v-for="f in res.functions" :key="f.image + f.function" data-row="hot-fn">
                <td class="text-right">{{ f.pct.toFixed(1) }}</td><td style="word-break: break-all">{{ f.function }}<div v-for="c in f.callers ?? []" :key="c.thread" class="text-caption text-grey">&larr; {{ c.thread }} ({{ c.pct.toFixed(1) }})</div></td><td>{{ f.image }}</td><td>{{ f.thread }}</td>
              </tr>
            </tbody>
          </q-markup-table>
        </div>
        <div class="col-12 col-lg-4">
          <q-markup-table dense flat bordered separator="horizontal" class="mono q-mb-md">
            <thead><tr><th class="text-left">thread</th><th class="text-right">%</th></tr></thead>
            <tbody><tr v-for="t in res.threads.slice(0, 15)" :key="t.thread"><td>{{ t.thread }}</td><td class="text-right">{{ t.pct.toFixed(1) }}</td></tr></tbody>
          </q-markup-table>
          <q-markup-table dense flat bordered separator="horizontal" class="mono">
            <thead><tr><th class="text-left">image</th><th class="text-right">%</th></tr></thead>
            <tbody><tr v-for="t in res.images.slice(0, 15)" :key="t.thread"><td>{{ t.thread }}</td><td class="text-right">{{ t.pct.toFixed(1) }}</td></tr></tbody>
          </q-markup-table>
        </div>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
import { api } from '@/api/client';
import { hot, type HotResult } from '@/stores/hotspots';

const props = defineProps<{ device: string }>();

const { secs, symbols, running, busy, since, error, res } = hot;

async function call(action: string, extra: Record<string, unknown> = {}): Promise<unknown> {
  busy.value = true;
  error.value = '';
  try {
    return await api.op(props.device, 'hotspots', { action, symbols: symbols.value, top: 60, ...extra });
  } catch (e) {
    error.value = `hotspots: ${(e as Error).message}`;
    return null;
  } finally {
    busy.value = false;
  }
}

async function start(): Promise<void> {
  if ((await call('start')) !== null) {
    running.value = true;
    since.value = new Date().toLocaleTimeString();
  }
}
async function stop(): Promise<void> {
  const r = await call('stop');
  running.value = false;
  if (r) res.value = r as HotResult;
}
async function run(): Promise<void> {
  const r = await call('run', { seconds: secs.value });
  if (r) res.value = r as HotResult;
}
</script>
