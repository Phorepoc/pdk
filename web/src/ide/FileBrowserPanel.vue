<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="column fit no-wrap" data-test="files" @dragover.prevent @drop.prevent="onDrop">
    <div class="row items-center no-wrap q-px-xs q-gutter-x-xs" style="border-bottom: 1px solid var(--ide-border)">
      <q-btn flat dense size="sm" icon="refresh" aria-label="refresh" @click="reload"><q-tooltip>Refresh</q-tooltip></q-btn>
      <q-btn flat dense size="sm" icon="upload_file" aria-label="upload" :disable="!!unsupported" @click="pick?.click()"><q-tooltip>Upload to the selected folder</q-tooltip></q-btn>
      <q-btn flat dense size="sm" icon="download" aria-label="download" :disable="!selectedFile" @click="download"><q-tooltip>Download the selected file</q-tooltip></q-btn>
      <q-btn v-if="settings.allowDelete" flat dense size="sm" icon="delete" aria-label="delete" data-test="fs-delete" :disable="!canDelete" @click="remove"><q-tooltip>Delete the selected file or folder</q-tooltip></q-btn>
      <span class="ellipsis text-caption text-grey col mono q-pl-xs">{{ targetDir }}</span>
      <input ref="pick" type="file" multiple hidden @change="onPick" />
    </div>
    <q-banner v-if="unsupported" dense class="bg-warning text-black" data-test="fs-unsupported">
      {{ unsupported }}
      <div class="text-caption">Expected device ops: {{ missing.join(', ') }} (see docs/phonelab.md, "IDE view").</div>
    </q-banner>
    <q-banner v-else-if="error" dense class="bg-negative text-white">{{ error }}</q-banner>
    <q-scroll-area class="col">
      <q-tree
        ref="tree" v-model:selected="selected" v-model:expanded="expanded" :nodes="nodes" node-key="path" label-key="label"
        no-nodes-label="Nothing to show" dense no-selection-unset @lazy-load="onLazy" @update:selected="onSelect"
      >
        <template #default-header="{ node }">
          <q-icon :name="node.type === 'd' ? 'folder' : 'insert_drive_file'" size="16px" :color="node.type === 'd' ? 'amber-6' : 'grey-5'" class="q-mr-xs" />
          <span class="ellipsis">{{ node.label }}</span>
          <span v-if="node.type === 'f'" class="text-caption text-grey q-ml-sm">{{ fmtSize(node.size) }}</span>
        </template>
      </q-tree>
    </q-scroll-area>
    <q-linear-progress v-if="busy" indeterminate size="2px" />
    <div class="text-caption text-grey q-pa-xs">Drop files here to upload into {{ targetDir }}</div>
  </div>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useQuasar } from 'quasar';
import { ApiError, api } from '@/api/client';
import { useIdeStore } from '@/ide/layout';
import { fileTab } from '@/ide/panels';
import { useSettingsStore } from '@/stores/settings';
import { useTransfersStore } from '@/stores/transfers';
import type { FsEntry } from '@/api/types';
import { useDevicesStore } from '@/stores/devices';

interface FsNode { label: string; path: string; type: 'd' | 'f'; size: number; lazy?: boolean; children?: FsNode[] }

const $q = useQuasar();
const devices = useDevicesStore();
const settings = useSettingsStore();
const transfers = useTransfersStore();
const ide = useIdeStore();
const root = (): FsNode[] => [{ label: '/', path: '/', type: 'd', size: 0, lazy: true }];
const nodes = ref<FsNode[]>(root());
const expanded = ref<string[]>([]);
const selected = ref<string | null>('/');
const tree = ref<{ getNodeByKey: (k: string) => FsNode | undefined } | null>(null);
const pick = ref<HTMLInputElement | null>(null);
const unsupported = ref('');
const missing = ref<string[]>([]);
const error = ref('');
const busy = ref(false);

const sel = computed(() => (selected.value ? tree.value?.getNodeByKey(selected.value) : undefined));
const selectedFile = computed(() => (sel.value?.type === 'f' ? sel.value : undefined));
const targetDir = computed(() => {
  const s = selected.value ?? '/';
  if (!sel.value || sel.value.type === 'd') return s;
  return s.replace(/\/[^/]*$/, '') || '/';
});

function fmtSize(n: number): string {
  if (n < 1024) return `${n} B`;
  if (n < 1048576) return `${(n / 1024).toFixed(1)} KB`;
  return `${(n / 1048576).toFixed(1)} MB`;
}

function toNodes(es: FsEntry[], dir: string): FsNode[] {
  return [...es]
    .sort((a, b) => (a.type === b.type ? a.name.localeCompare(b.name) : a.type === 'd' ? -1 : 1))
    .map((e) => ({ label: e.name, path: (dir === '/' ? '' : dir) + '/' + e.name, type: e.type, size: e.size, ...(e.type === 'd' ? { lazy: true } : {}) }));
}

function note(e: unknown): void {
  if (e instanceof ApiError && e.status === 501) {
    unsupported.value = e.message;
    missing.value = (e.body as { missing_ops?: string[] } | null)?.missing_ops ?? [];
  } else error.value = (e as Error).message;
}

async function list(dir: string): Promise<FsNode[] | null> {
  const id = devices.currentId;
  if (!id) return null;
  try {
    const r = await api.fsList(id, dir);
    unsupported.value = ''; error.value = '';
    return toNodes(r.entries ?? [], dir);
  } catch (e) { note(e); return null; }
}

async function onLazy({ node, done, fail }: { node: FsNode; done: (c: FsNode[]) => void; fail: () => void }): Promise<void> {
  busy.value = true;
  const c = await list(node.path);
  busy.value = false;
  if (c) done(c); else fail();
}

function reload(): void {
  expanded.value = [];
  nodes.value = root();
  selected.value = '/';
  unsupported.value = ''; error.value = '';
  // probing the root surfaces the 501 notice without waiting for a click
  void list('/').then((c) => {
    if (c) { nodes.value = [{ label: '/', path: '/', type: 'd', size: 0, children: c }]; expanded.value = ['/']; }
  });
}

async function upload(files: File[]): Promise<void> {
  const id = devices.currentId;
  if (!id || !files.length) return;
  busy.value = true;
  for (const f of files) {
    const dest = (targetDir.value === '/' ? '' : targetDir.value) + '/' + f.name;
    try {
      await transfers.track(`${f.name} to the phone`, (p) => api.fsPut(id, dest, f, p));
      $q.notify({ type: 'positive', message: `Uploaded ${dest}`, timeout: 1500 });
    } catch (e) { note(e); $q.notify({ type: 'negative', message: `${f.name}: ${(e as Error).message}` }); break; }
  }
  busy.value = false;
  reload();
}
function onPick(ev: Event): void {
  const el = ev.target as HTMLInputElement;
  void upload([...(el.files ?? [])]);
  el.value = '';
}
function onDrop(ev: DragEvent): void { void upload([...(ev.dataTransfer?.files ?? [])]); }
// a click on a file opens it in a tab (details; contents for images and text)
function onSelect(key: string | null): void {
  const n = key ? tree.value?.getNodeByKey(key) : undefined;
  if (n?.type === 'f') ide.openTab(fileTab(n.path));
}
// a drive ("/C:") is never offered for deletion
const canDelete = computed(() => !!sel.value && (sel.value.path.match(/\//g) ?? []).length >= 2);
function remove(): void {
  const n = sel.value;
  if (!n) return;
  $q.dialog({
    title: 'Delete on the phone?', cancel: true, ok: { label: 'Delete', color: 'negative' },
    message: n.type === 'd' ? `${n.path} and everything in it` : n.path,
  }).onOk(async () => {
    busy.value = true;
    try {
      await api.fsDelete(devices.currentId, n.path);
      $q.notify({ type: 'positive', message: `Deleted ${n.path}`, timeout: 1500 });
      ide.closeTab(fileTab(n.path));
      reload();
    } catch (e) { $q.notify({ type: 'negative', message: (e as Error).message }); }
    finally { busy.value = false; }
  });
}
function download(): void {
  if (selectedFile.value) window.open(api.fsUrl(devices.currentId, selectedFile.value.path), '_blank');
}

onMounted(reload);
watch(() => devices.currentId, reload);
</script>
