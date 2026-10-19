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
            <q-input v-if="reboot" v-model="waitFor" dense outlined class="q-mt-xs" :disable="busy" label="Then wait for a log line (regular expression; empty = the first line of the new boot)"
                     placeholder="e.g. running slot B" />
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

            <div v-if="startedAt && reboot" class="q-mt-md">
              <div class="text-subtitle2">The new boot</div>
              <div v-if="report">
                <boot-report-card :report="report" />
                <q-btn flat dense no-caps color="primary" icon="restart_alt" label="Open in Boots" class="q-mt-xs" @click="openBoots" />
              </div>
              <div v-else-if="bootEvent"><q-spinner size="16px" /> booted <b>slot {{ bootEvent.data?.slot }}</b> (boot #{{ bootEvent.boot }}), waiting for the line...</div>
              <div v-else-if="busy"><q-spinner size="16px" /> waiting for the boot...</div>
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
import type { Blob as BlobInfo, BootReport, DeviceEvent } from '@/api/types';
import BootReportCard from '@/components/BootReportCard.vue';
import { useIdeStore } from '@/ide/layout';
import { focusBoot } from '@/stores/focus';
import { useDevicesStore } from '@/stores/devices';
import { useLiveStore } from '@/stores/live';
import { useTransfersStore } from '@/stores/transfers';
import { fmtSize, pretty } from '@/utils';

const devices = useDevicesStore();
const live = useLiveStore();
const transfers = useTransfersStore();
const ide = useIdeStore();

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
const waitFor = ref('');
const report = ref<BootReport | null>(null);

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
  report.value = null;
  try {
    // with a reboot the op also waits for the new boot and returns its report (push wait=)
    const args: Record<string, unknown> = { reboot: reboot.value, ...(reboot.value ? { wait: waitFor.value.trim() || '.' } : {}) };
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
      if (e.device === id && e.kind === 'boot.slot' && e.ts >= startedAt.value - 1000) bootEvent.value = e;
    });
    const r = await api.op(id, 'push', args) as { result?: unknown; boot?: BootReport };
    if (r && r.boot) { result.value = r.result; report.value = r.boot; } else result.value = r;
    Notify.create({ type: 'positive', message: 'push finished', timeout: 1500 });
  } catch (e) {
    error.value = (e as Error).message;
    if (e instanceof ApiError && e.result !== undefined) {
      const r = e.result as { result?: unknown; boot?: BootReport } | null;
      if (r && typeof r === 'object' && 'boot' in r) { result.value = r.result; report.value = r.boot ?? null; } else result.value = e.result;
    }
  } finally {
    busy.value = false;
  }
}

function openBoots(): void {
  if (report.value) focusBoot.value = report.value.boot;
  ide.openTab('boots');
}
</script>
