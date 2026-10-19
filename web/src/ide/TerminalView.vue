<template><div ref="el" class="ide-term" /></template>

<script setup lang="ts">
// One xterm.js instance bound to GET /api/devices/{id}/tty/{n} (WebSocket; n = 0 opens a new tty on the phone).
// Binary frames = raw tty bytes both ways; text frames = JSON control: we send {"type":"resize","cols","rows"},
// the server sends {"type":"tty","id"} once attached and {"type":"error","error"} when the tty cannot be reached.
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { Terminal } from '@xterm/xterm';
import { FitAddon } from '@xterm/addon-fit';
import '@xterm/xterm/css/xterm.css';
import { getToken } from '@/api/client';

const props = defineProps<{ device: string; tty: number; visible: boolean }>();
const emit = defineEmits<{ attached: [number]; gone: [] }>();
const el = ref<HTMLDivElement | null>(null);
let term: Terminal | null = null;
let fit: FitAddon | null = null;
let ws: WebSocket | null = null;
let ro: ResizeObserver | null = null;
let closed = false;
let attachedId = props.tty;

function url(): string {
  const proto = location.protocol === 'https:' ? 'wss:' : 'ws:';
  const t = getToken();
  return `${proto}//${location.host}/api/devices/${encodeURIComponent(props.device)}/tty/${attachedId}${t ? `?token=${encodeURIComponent(t)}` : ''}`;
}

function sendSize(): void {
  if (term && ws?.readyState === 1) ws.send(JSON.stringify({ type: 'resize', cols: term.cols, rows: term.rows }));
}
function refit(): void {
  if (!props.visible || !el.value?.offsetParent) return;
  try { fit?.fit(); sendSize(); } catch { /* not laid out yet */ }
}

function red(s: string): void { term?.write(`\r\n\u001b[31m${s}\u001b[0m\r\n`); }

function connect(): void {
  if (!props.device || closed) return;
  ws = new WebSocket(url());
  ws.binaryType = 'arraybuffer';
  ws.onmessage = (e) => {
    if (typeof e.data !== 'string') { term?.write(new Uint8Array(e.data as ArrayBuffer)); return; }
    let m: { type?: string; id?: number; error?: string } = {};
    try { m = JSON.parse(e.data) as typeof m; } catch { return; }
    if (m.type === 'tty' && m.id) { attachedId = m.id; emit('attached', m.id); sendSize(); }
    else if (m.type === 'error') { red(m.error ?? 'error'); emit('gone'); }
  };
  ws.onclose = () => { if (!closed) red('[disconnected]'); };
}

onMounted(() => {
  term = new Terminal({
    fontSize: 12, fontFamily: 'ui-monospace, Consolas, monospace', cursorBlink: true, scrollback: 5000,
    theme: { background: '#1e1f22', foreground: '#dfe1e5', cursor: '#dfe1e5', selectionBackground: '#2e436e' },
  });
  fit = new FitAddon();
  term.loadAddon(fit);
  term.open(el.value!);
  const enc = new TextEncoder();
  term.onData((d) => { if (ws?.readyState === 1) ws.send(enc.encode(d)); });
  ro = new ResizeObserver(refit);
  ro.observe(el.value!);
  connect();
  refit();
});
watch(() => props.visible, (v) => { if (v) { setTimeout(refit, 0); term?.focus(); } });
watch(() => props.device, () => { ws?.close(); term?.reset(); attachedId = 0; connect(); });
onBeforeUnmount(() => { closed = true; ro?.disconnect(); ws?.close(); term?.dispose(); });
</script>

<style>
.ide-term { height: 100%; width: 100%; padding: 4px 0 0 6px; box-sizing: border-box; }
</style>
