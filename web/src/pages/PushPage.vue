<template>
  <q-page class="page-pad">
    <div v-if="!canPush" class="text-grey">This device has no <code>push</code> op.</div>
    <div v-else class="row q-col-gutter-md">
      <div class="col-12 col-md-6">
        <q-card flat bordered>
          <q-card-section>
            <div class="text-h6">Push image to {{ devices.currentId }}</div>
            <div class="text-caption text-grey">{{ pushSpec?.description }}</div>
          </q-card-section>
          <q-card-section>
            <q-btn-toggle v-model="source" no-caps unelevated toggle-color="primary" dense
                          :options="[{ label: 'Upload a file', value: 'upload' }, { label: 'Server path', value: 'path' }]" />
            <div v-if="source === 'upload'" class="q-mt-md">
              <q-file v-model="file" label="Image file" outlined dense clearable :disable="busy" @update:model-value="uploaded = null" />
              <q-linear-progress v-if="upProgress !== null" :value="upProgress" class="q-mt-sm" size="10px" />
              <div v-if="uploaded" class="text-caption q-mt-xs">blob <span class="mono">{{ uploaded.sha.slice(0, 16) }}...</span> ({{ fmtSize(uploaded.size) }})</div>
            </div>
            <div v-else class="q-mt-md">
              <q-input v-model="path" label="Path on the server (relative to repo root)" outlined dense :disable="busy"
                       placeholder=".pinephone-build/symbian.img" />
            </div>
            <q-checkbox v-model="reboot" class="q-mt-sm" label="Reboot into the new image after a good upload" :disable="busy" />
            <div>
              <q-btn color="primary" no-caps icon="upload" label="Push" :loading="busy" :disable="!ready" @click="push" />
            </div>
          </q-card-section>
        </q-card>
      </div>

      <div class="col-12 col-md-6">
        <q-card flat bordered>
          <q-card-section>
            <div class="text-subtitle1">Progress</div>
            <div v-if="prog">
              <div class="text-body2">{{ prog.op }}: {{ prog.stage }} {{ prog.done }} / {{ prog.total }}</div>
              <q-linear-progress :value="prog.total ? prog.done / prog.total : 0" size="14px" class="q-mt-xs" :color="prog.stage === 'verify' ? 'teal' : 'primary'" />
            </div>
            <div v-else-if="!busy && !result && !error" class="text-grey">Idle.</div>
            <div v-if="busy && !prog" class="text-grey">Working...</div>

            <div v-if="error" class="text-negative q-mt-sm">{{ error }}</div>
            <pre v-if="result !== undefined" class="json mono q-mt-sm">{{ pretty(result) }}</pre>

            <div v-if="startedAt" class="q-mt-md">
              <div class="text-subtitle2">Boot after push</div>
              <div v-if="bootEvent">
                <q-icon name="check_circle" color="positive" /> booted <b>slot {{ bootEvent.data?.slot }}</b>
                <span class="text-grey"> {{ bootEvent.data?.detail }}</span> (boot #{{ bootEvent.boot }})
                <q-btn flat dense no-caps color="primary" label="view log" :to="{ path: '/log', query: { boot: String(bootEvent.boot) } }" />
              </div>
              <div v-else-if="busy || awaitingBoot"><q-spinner size="16px" /> waiting for the boot.slot event...</div>
            </div>
          </q-card-section>
        </q-card>
      </div>
    </div>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, ref } from 'vue';
import { Notify } from 'quasar';
import { ApiError, api } from '@/api/client';
import type { Blob as BlobInfo, DeviceEvent } from '@/api/types';
import { useDevicesStore } from '@/stores/devices';
import { useLiveStore } from '@/stores/live';
import { useTransfersStore } from '@/stores/transfers';
import { fmtSize, pretty } from '@/utils';

const devices = useDevicesStore();
const live = useLiveStore();
const transfers = useTransfersStore();

const pushSpec = computed(() => devices.current?.ops.find((o) => o.name === 'push'));
const canPush = computed(() => !!pushSpec.value);

const source = ref<'upload' | 'path'>('upload');
const file = ref<File | null>(null);
const path = ref('');
const reboot = ref(true);
const busy = ref(false);
const upProgress = ref<number | null>(null);
const uploaded = ref<BlobInfo | null>(null);
const result = ref<unknown>(undefined);
const error = ref('');
const startedAt = ref(0);
const bootEvent = ref<DeviceEvent | null>(null);
const awaitingBoot = ref(false);

const ready = computed(() => (source.value === 'upload' ? !!file.value : !!path.value.trim()));
const prog = computed(() => live.progress[devices.currentId]);

let off: (() => void) | null = null;
onBeforeUnmount(() => off?.());

async function push(): Promise<void> {
  const id = devices.currentId;
  busy.value = true;
  error.value = '';
  result.value = undefined;
  bootEvent.value = null;
  awaitingBoot.value = false;
  try {
    const args: Record<string, unknown> = { reboot: reboot.value };
    if (source.value === 'upload') {
      upProgress.value = 0;
      const f = file.value as File;
      uploaded.value ??= await transfers.track(`upload ${f.name}`, (p) => api.uploadBlob(f, 'image', id, (d, t) => { upProgress.value = d / t; p(d, t); }));
      upProgress.value = 1;
      args.blob = uploaded.value.sha;
    } else {
      args.file = path.value.trim();
    }
    startedAt.value = Date.now();
    off?.();
    off = live.onEvent((e) => {
      if (e.device === id && e.kind === 'boot.slot' && e.ts >= startedAt.value - 1000) { bootEvent.value = e; awaitingBoot.value = false; }
    });
    result.value = await api.op(id, 'push', args);
    awaitingBoot.value = reboot.value;
    Notify.create({ type: 'positive', message: 'push finished', timeout: 1500 });
  } catch (e) {
    error.value = (e as Error).message;
    if (e instanceof ApiError && e.result !== undefined) result.value = e.result;
  } finally {
    busy.value = false;
  }
}
</script>
