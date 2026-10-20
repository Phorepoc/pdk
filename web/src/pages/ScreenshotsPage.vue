<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-page class="page-pad">
    <q-card v-if="remote" flat bordered class="q-mb-md">
      <q-card-section>
        <div class="text-h6">Remote</div>
        <div class="row q-gutter-sm q-mt-xs items-center">
          <q-btn v-if="hasOp('screenshot')" color="primary" no-caps icon="photo_camera" label="Take screenshot" :loading="shooting" @click="shoot" />
          <template v-if="hasOp('key')">
            <q-input v-model="key" dense outlined label="key" style="width: 110px" />
            <q-select v-model="keyAction" :options="['press', 'down', 'up']" dense outlined style="width: 110px" />
            <q-btn color="primary" outline no-caps label="Send key" :disable="!key" @click="sendKey" />
          </template>
          <q-select v-if="hasOp('touch')" v-model="touchAction" :options="['tap', 'down', 'up', 'move']" dense outlined label="touch action" style="width: 130px" />
        </div>
        <div v-if="remoteMsg" class="text-caption q-mt-xs" :class="remoteErr ? 'text-negative' : 'text-grey'">{{ remoteMsg }}</div>
        <div v-if="latest" class="q-mt-md">
          <img ref="img" :src="api.blobUrl(latest.sha)" alt="latest screenshot" style="max-width: 100%; cursor: crosshair; border: 1px solid rgba(128,128,128,.4)"
               @click="touch" />
          <div class="text-caption text-grey">{{ hasOp('touch') ? 'Click the image to touch that point.' : '' }}</div>
        </div>
      </q-card-section>
    </q-card>

    <div class="text-subtitle1 q-mb-sm">Screenshots</div>
    <div v-if="error" class="text-negative">{{ error }}</div>
    <div v-if="!shots.length" class="text-grey">
      No screenshots yet. Screenshots arrive with the phone-side capture support.
    </div>
    <div class="row q-col-gutter-sm">
      <div v-for="b in shots" :key="b.sha" class="col-6 col-sm-4 col-md-3 col-lg-2">
        <q-card flat bordered>
          <a :href="api.blobUrl(b.sha)" target="_blank" rel="noopener">
            <q-img :src="api.blobUrl(b.sha)" :ratio="3 / 4" fit="contain" loading="lazy" />
          </a>
          <q-card-section class="q-pa-xs text-caption">
            {{ fmtTs(b.ts) }}
            <q-btn flat dense round size="sm" icon="download" :href="api.blobUrl(b.sha, true)" aria-label="download" />
          </q-card-section>
        </q-card>
      </div>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { api } from '@/api/client';
import type { Blob } from '@/api/types';
import { useDevicesStore } from '@/stores/devices';
import { fmtTs } from '@/utils';

const devices = useDevicesStore();
const shots = ref<Blob[]>([]);
const error = ref('');
const img = ref<HTMLImageElement | null>(null);
const shooting = ref(false);
const key = ref('');
const keyAction = ref('press');
const touchAction = ref('tap');
const remoteMsg = ref('');
const remoteErr = ref(false);

const hasOp = (n: string) => devices.hasOp(devices.currentId, n);
const remote = computed(() => hasOp('screenshot') || hasOp('key') || hasOp('touch'));
const latest = computed(() => shots.value[0]);

async function load(): Promise<void> {
  try { shots.value = (await api.blobs('screenshot', 100)).sort((a, b) => b.ts - a.ts); error.value = ''; }
  catch (e) { error.value = (e as Error).message; }
}

async function remoteCall(op: string, args: Record<string, unknown>): Promise<unknown> {
  remoteErr.value = false;
  remoteMsg.value = '';
  try { return await api.op(devices.currentId, op, args); }
  catch (e) { remoteErr.value = true; remoteMsg.value = `${op}: ${(e as Error).message}`; return undefined; }
}

async function shoot(): Promise<void> {
  shooting.value = true;
  try { await remoteCall('screenshot', {}); await load(); } finally { shooting.value = false; }
}
async function sendKey(): Promise<void> {
  const r = await remoteCall('key', { key: key.value, action: keyAction.value });
  if (r !== undefined) remoteMsg.value = `key ${key.value} ${keyAction.value}: ok`;
}
async function touch(ev: MouseEvent): Promise<void> {
  const el = img.value;
  if (!el || !hasOp('touch') || !el.clientWidth) return;
  const r = el.getBoundingClientRect();
  const x = Math.round(((ev.clientX - r.left) / r.width) * (el.naturalWidth || r.width));
  const y = Math.round(((ev.clientY - r.top) / r.height) * (el.naturalHeight || r.height));
  const res = await remoteCall('touch', { x, y, action: touchAction.value });
  if (res !== undefined) remoteMsg.value = `touch ${x},${y} ${touchAction.value}: ok`;
}

onMounted(load);
watch(() => devices.currentId, load);
</script>
