<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="column fit no-wrap" data-test="file-view">
    <div class="row items-center no-wrap q-px-sm q-gutter-x-sm file-head">
      <q-icon :name="kind === 'image' ? 'image' : kind === 'text' ? 'article' : 'insert_drive_file'" size="18px" />
      <span class="mono ellipsis col">{{ arg }}</span>
      <span v-if="entry" class="text-caption text-grey">{{ fmtSize(entry.size) }}<template v-if="entry.mtime"> | {{ new Date(entry.mtime * 1000).toLocaleString() }}</template></span>
      <q-btn flat dense size="sm" icon="refresh" @click="load"><q-tooltip>Reload</q-tooltip></q-btn>
      <q-btn flat dense size="sm" icon="download" @click="download"><q-tooltip>Download</q-tooltip></q-btn>
      <q-btn v-if="settings.allowDelete" flat dense size="sm" icon="delete" color="negative" data-test="file-delete" @click="remove"><q-tooltip>Delete on the phone</q-tooltip></q-btn>
    </div>
    <q-linear-progress v-if="busy" indeterminate size="2px" />
    <div v-if="error" class="text-negative q-pa-sm">{{ error }}</div>
    <div class="col relative-position" style="min-height: 0">
      <div v-if="kind === 'image' && imgUrl" class="absolute-full flex flex-center scroll q-pa-sm">
        <img :src="imgUrl" alt="" style="max-width: 100%; max-height: 100%; image-rendering: pixelated" @error="error = 'cannot show this image'" />
      </div>
      <pre v-else-if="text !== null" class="absolute-full mono file-text q-ma-none q-pa-sm">{{ text }}</pre>
      <div v-else-if="!busy" class="q-pa-md text-grey">
        <div>{{ kind === 'binary' ? 'Binary file' : 'Not loaded' }}{{ entry ? `, ${fmtSize(entry.size)}` : '' }}.</div>
        <q-btn class="q-mt-sm" outline dense no-caps icon="data_object" label="Show the first 4 KB as hex" @click="loadHex" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// A file on the phone opened from the Project panel: its details, and its contents when it is an image or text.
import { computed, onMounted, ref } from 'vue';
import { Dialog, Notify } from 'quasar';
import { api, getToken } from '@/api/client';
import type { FsEntry } from '@/api/types';
import { useIdeStore } from '@/ide/layout';
import { fileTab } from '@/ide/panels';
import { useDevicesStore } from '@/stores/devices';
import { useSettingsStore } from '@/stores/settings';

const props = defineProps<{ arg: string }>();
const devices = useDevicesStore();
const settings = useSettingsStore();
const ide = useIdeStore();

const IMAGE = /\.(png|jpe?g|gif|bmp|webp|svg|ico)$/i;
const TEXT = /\.(txt|log|ini|cfg|conf|xml|html?|json|md|csv|py|sh|js|css|c|cpp|h|hrh|rss|rls|mmp|inf|iby|oby|pkg|loc|url|m3u|pls|vcf|srt)$/i;
const TEXT_MAX = 512 * 1024;

const entry = ref<FsEntry | null>(null);
const text = ref<string | null>(null);
const imgUrl = ref('');
const busy = ref(false);
const error = ref('');
const kind = computed(() => (IMAGE.test(props.arg) ? 'image' : TEXT.test(props.arg) ? 'text' : 'binary'));

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1048576).toFixed(1)} MB`;
}

async function fetchBytes(limit: number): Promise<Uint8Array> {
  const t = getToken();
  const res = await fetch(api.fsUrl(devices.currentId, props.arg), { headers: t ? { Authorization: `Bearer ${t}` } : {} });
  if (!res.ok) throw new Error((await res.text()) || `HTTP ${res.status}`);
  const reader = res.body!.getReader();
  const parts: Uint8Array[] = [];
  let n = 0;
  while (n < limit) {
    const { done, value } = await reader.read();
    if (done) break;
    parts.push(value);
    n += value.length;
  }
  void reader.cancel();
  const out = new Uint8Array(Math.min(n, limit));
  let o = 0;
  for (const p of parts) { out.set(p.subarray(0, out.length - o), o); o += p.length; if (o >= out.length) break; }
  return out;
}

async function load(): Promise<void> {
  busy.value = true;
  error.value = '';
  text.value = null;
  try {
    const dir = props.arg.replace(/\/[^/]*$/, '') || '/';
    const name = props.arg.split('/').pop();
    entry.value = (await api.fsList(devices.currentId, dir)).entries.find((e) => e.name === name) ?? null;
    if (!entry.value) throw new Error('not found on the phone');
    if (kind.value === 'image') imgUrl.value = api.fsUrl(devices.currentId, props.arg) + `&t=${Date.now()}`;
    else if (kind.value === 'text') {
      const b = await fetchBytes(TEXT_MAX);
      text.value = new TextDecoder().decode(b) + (entry.value.size > TEXT_MAX ? `\n... (first ${TEXT_MAX / 1024} KB of ${fmtSize(entry.value.size)})` : '');
    }
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}

async function loadHex(): Promise<void> {
  busy.value = true;
  try {
    const b = await fetchBytes(4096);
    const rows: string[] = [];
    for (let i = 0; i < b.length; i += 16) {
      const s = b.subarray(i, i + 16);
      const hex = [...s].map((x) => x.toString(16).padStart(2, '0')).join(' ');
      const asc = [...s].map((x) => (x >= 32 && x < 127 ? String.fromCharCode(x) : '.')).join('');
      rows.push(`${i.toString(16).padStart(8, '0')}  ${hex.padEnd(47)}  ${asc}`);
    }
    text.value = rows.join('\n');
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}

function download(): void { window.open(api.fsUrl(devices.currentId, props.arg), '_blank'); }

function remove(): void {
  Dialog.create({ title: 'Delete on the phone?', message: props.arg, cancel: true, ok: { label: 'Delete', color: 'negative' } }).onOk(async () => {
    try {
      await api.fsDelete(devices.currentId, props.arg);
      Notify.create({ type: 'positive', message: `Deleted ${props.arg}`, timeout: 1500 });
      ide.closeTab(fileTab(props.arg));
    } catch (e) { Notify.create({ type: 'negative', message: (e as Error).message }); }
  });
}

onMounted(load);
</script>

<style scoped>
.file-head { height: 30px; border-bottom: 1px solid var(--ide-border); }
.file-text { overflow: auto; font-size: 12px; white-space: pre; }
</style>
