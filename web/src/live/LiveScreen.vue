<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="column fit no-wrap" data-test="live">
    <div v-if="!supported" class="text-grey q-pa-md">
      This device does not offer the <code>screenshot</code>, <code>touch</code> and <code>key</code> ops needed for remote control.
    </div>
    <template v-else>
      <div class="col row no-wrap" style="min-height: 0">
        <!-- the screen -->
        <div ref="area" class="col screen-area" :class="{ scroll: r.zoom !== 'fit' }">
          <div ref="focusEl" tabindex="0" class="preview-focus" @keydown="onKey">
            <div class="preview-wrap">
              <video v-show="videoOn && frameInfo" ref="vid" class="preview-img" :style="imgStyle" muted playsinline disablepictureinpicture
                     @pointerdown="pDown" @pointermove="pMove" @pointerup="pUp" @pointercancel="pCancel" @contextmenu.prevent />
              <img v-if="url && !videoOn" ref="img" :src="url" alt="phone screen" draggable="false" class="preview-img"
                   :style="imgStyle" @load="onImgLoad"
                   @pointerdown="pDown" @pointermove="pMove" @pointerup="pUp" @pointercancel="pCancel" @contextmenu.prevent />
              <div v-if="marker && (videoOn ? frameInfo : url)" class="touch-marker" :style="{ left: marker.px + '%', top: marker.py + '%' }" />
              <div v-if="videoOn ? !frameInfo : !url" class="text-grey q-pa-lg">waiting for the first {{ videoOn ? 'video picture' : 'frame' }}...</div>
            </div>
          </div>
        </div>
        <!-- collapsible side drawers -->
        <div v-if="r.keypadOpen || r.settingsOpen" class="drawer column no-wrap q-gutter-y-md q-pa-sm">
          <phone-keypad v-if="r.keypadOpen" />
          <live-settings v-if="r.settingsOpen" />
        </div>
      </div>

      <!-- bottom bar -->
      <div class="live-bar row items-center no-wrap q-gutter-x-xs">
        <q-btn flat dense size="sm" :icon="r.paused ? 'play_arrow' : 'pause'" :color="r.paused ? 'positive' : undefined" @click="togglePause">
          <q-tooltip>{{ r.paused ? 'Resume' : 'Pause' }}</q-tooltip>
        </q-btn>
        <q-btn flat dense size="sm" icon="photo_camera" @click="r.snapshot()"><q-tooltip>Snapshot (saved to Screenshots)</q-tooltip></q-btn>
        <q-btn v-if="canRecord" flat dense size="sm" :icon="recording ? 'stop' : 'fiber_manual_record'" color="negative" :loading="recBusy"
               data-test="record" @click="toggleRecord">
          <q-tooltip>{{ recording ? 'Stop recording (saved to Screenshots, Videos)' : 'Record the screen as video (H.264)' }}</q-tooltip>
        </q-btn>
        <span v-if="recording" class="text-caption text-negative">REC {{ recSeconds }}s</span>
        <span class="text-caption ellipsis col" data-test="stats">
          <template v-if="frameInfo && videoOn">{{ frameInfo.w }}x{{ frameInfo.h }} | video | {{ fps.toFixed(1) }} updates/s<span v-if="lastWhite > 500" class="text-warning"> | white {{ (lastWhite / 10).toFixed(0) }}%</span></template>
          <template v-else-if="frameInfo">{{ frameInfo.w }}x{{ frameInfo.h }} | {{ frameInfo.seconds.toFixed(2) }}s | {{ (frameInfo.wire / 1024).toFixed(1) }} KB ({{ frameInfo.tiles }} tiles) | {{ fps.toFixed(1) }} fps</template>
          <span v-if="busyCount" class="text-warning"> | link busy ({{ busyCount }})</span>
          <span v-if="note" class="text-warning"> | {{ note }}</span>
          <span v-if="errMsg" class="text-negative"> | {{ errMsg }}</span>
        </span>
        <q-btn flat dense size="sm" icon="dialpad" :color="r.keypadOpen ? 'primary' : undefined" data-test="toggle-keypad" @click="r.keypadOpen = !r.keypadOpen">
          <q-tooltip>Keypad</q-tooltip>
        </q-btn>
        <q-btn flat dense size="sm" icon="tune" :color="r.settingsOpen ? 'primary' : undefined" data-test="toggle-settings" @click="r.settingsOpen = !r.settingsOpen">
          <q-tooltip>View settings, swipes, text</q-tooltip>
        </q-btn>
        <q-btn flat dense size="sm" icon="open_in_new" @click="openControls"><q-tooltip>Phone controls in their own tab</q-tooltip></q-btn>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
// The Live tab: the phone's screen (click / drag = touch, keyboard when focused), a bottom bar, and the keypad and
// settings as collapsible drawers. The same controls also open as their own tab (Phone controls).
import { computed, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { api, type FrameResponse } from '@/api/client';
import { FramePoller, Gesture, mapKeyboardKey } from '@/api/remote';
import { LiveVideo, liveVideoSupported, type LiveVideoReply } from '@/api/livevideo';
import { useIdeStore } from '@/ide/layout';
import { useDevicesStore } from '@/stores/devices';
import { useRemoteStore } from '@/stores/remote';
import LiveSettings from './LiveSettings.vue';
import PhoneKeypad from './PhoneKeypad.vue';

const devices = useDevicesStore();
const r = useRemoteStore();
const ide = useIdeStore();
const id = computed(() => devices.currentId);
const supported = computed(() => ['screenshot', 'touch', 'key'].every((o) => devices.hasOp(id.value, o)));

const url = ref('');
const frameInfo = ref<{ w: number; h: number; seconds: number; wire: number; tiles: number } | null>(null);
const fps = ref(0);
const busyCount = ref(0);
const errMsg = ref('');
const note = ref('');
const marker = ref<{ px: number; py: number } | null>(null);
const img = ref<HTMLImageElement | null>(null);
const vid = ref<HTMLVideoElement | null>(null);
const focusEl = ref<HTMLElement | null>(null);

const imgStyle = computed<Record<string, string>>(() => {
  const w = frameInfo.value?.w ?? 0;
  if (r.zoom === 'fit') return { maxWidth: '100%', maxHeight: '100%', width: 'auto', height: 'auto', imageRendering: 'auto' };
  return { width: w * Number(r.zoom) + 'px', maxWidth: 'none', imageRendering: 'pixelated' };
});

// ------------------------------------------------------------ frames
let snapNext = false;
let lastFrameAt = 0;
let toRevoke: string[] = [];

const poller = new FramePoller<FrameResponse>({
  fetchFrame: () => {
    const keep = snapNext || r.store;
    snapNext = false;
    return api.frame(id.value, r.which, keep);
  },
  interval: () => r.intervalS * 1000,
  onFrame: (f) => {
    errMsg.value = '';
    busyCount.value = 0;
    if (!f.blob) return;
    const nu = URL.createObjectURL(f.blob);
    if (url.value) toRevoke.push(url.value);
    url.value = nu;
    frameInfo.value = { w: f.width || 0, h: f.height || 0, seconds: f.seconds || 0, wire: f.wire || 0, tiles: f.tiles || 0 };
    r.frameSize = { w: f.width || 0, h: f.height || 0 };
    const t = performance.now();
    if (lastFrameAt) { const x = 1000 / (t - lastFrameAt); fps.value = fps.value ? fps.value * 0.7 + x * 0.3 : x; }
    lastFrameAt = t;
  },
  onBusy: () => { busyCount.value++; },
  onError: (f) => {
    errMsg.value = `frame: ${f.error ?? f.status}`;
    if (r.which === 2 && f.status === 502) {
      r.which = 0; // an old phone image without the composite view
      note.value = 'composite view unsupported, showing the OS screen';
    }
  },
});

function onImgLoad(): void {
  // the new picture is decoded: the old object URLs can go (no leak at 1 fps for hours)
  for (const u of toRevoke) URL.revokeObjectURL(u);
  toRevoke = [];
}

function togglePause(): void {
  r.paused = !r.paused;
  applySource();
}

// ------------------------------------------------------------ the video transport (Settings > Source > Video)
// The phone's encoder as fragmented MP4 in a <video>: plain http, records nothing (live_video only keeps the encoder
// running while this view asks). Its picture is the screen as the user sees it (360x720) = the composite view: touch
// maps as view 2.
const canVideo = computed(() => devices.hasOp(id.value, 'live_video') && liveVideoSupported());
const videoOn = computed(() => r.source === 'video' && canVideo.value);
const lastWhite = ref(0);
const viewer = Math.random().toString(36).slice(2); // this view's own paced timeline on the server
const video = new LiveVideo({
  call: async (from) => (await api.op(id.value, 'live_video', from === undefined ? { viewer } : { from, viewer }, 'live')) as LiveVideoReply | undefined,
  video: () => vid.value,
  onFrame: (w, h, white) => {
    errMsg.value = '';
    frameInfo.value = { w, h, seconds: 0, wire: 0, tiles: 0 };
    r.frameSize = { w, h };
    lastWhite.value = white;
    const t = performance.now();
    if (lastFrameAt) { const x = 1000 / (t - lastFrameAt); fps.value = fps.value ? fps.value * 0.9 + x * 0.1 : x; }
    lastFrameAt = t;
  },
  onError: (m) => { errMsg.value = `video: ${m}`; },
});

/** runs exactly one transport (or none while paused) */
function applySource(): void {
  const on = supported.value && !r.paused;
  if (on && videoOn.value) { poller.pause(); if (!video.active) { frameInfo.value = null; lastFrameAt = 0; fps.value = 0; video.start(); } }
  else { if (video.active) { video.stop(); frameInfo.value = null; lastFrameAt = 0; fps.value = 0; } if (on) poller.start(); else poller.pause(); }
}
watch(videoOn, applySource);

function openControls(): void {
  r.keypadOpen = false;
  r.settingsOpen = false;
  ide.openTab('controls');
}

watch(() => r.which, () => { lastFrameAt = 0; if (!videoOn.value) poller.poke(); });

// ------------------------------------------------------------ video recording (record_start / record_stop)
const canRecord = computed(() => devices.hasOp(id.value, 'record_start'));
const recording = ref(false);
const recBusy = ref(false);
const recSeconds = ref(0);
let recTimer: ReturnType<typeof setInterval> | undefined;

async function toggleRecord(): Promise<void> {
  recBusy.value = true;
  try {
    if (!recording.value) {
      await api.op(id.value, 'record_start', {});
      recording.value = true;
      recSeconds.value = 0;
      recTimer = setInterval(() => { recSeconds.value++; }, 1000);
      note.value = '';
    } else {
      clearInterval(recTimer);
      recording.value = false;
      const res = (await api.op(id.value, 'record_stop', {})) as { summary?: { frames?: number; duration_ms?: number } } | undefined;
      const s = res?.summary;
      note.value = s ? `recorded ${s.frames ?? 0} frames, ${((s.duration_ms ?? 0) / 1000).toFixed(1)}s (Screenshots, Videos)` : 'recorded';
    }
  } catch (e) {
    errMsg.value = `record: ${(e as Error).message}`;
    if (recording.value) { clearInterval(recTimer); recording.value = false; }
  } finally { recBusy.value = false; }
}

// ------------------------------------------------------------ input
const gesture = new Gesture({ which: () => (videoOn.value ? 2 : r.which), send: r.send, dragMode: () => r.dragMode });

function imgPoint(e: PointerEvent): { x: number; y: number } | null {
  const el: HTMLImageElement | HTMLVideoElement | null = videoOn.value ? vid.value : img.value;
  if (!el) return null;
  const b = el.getBoundingClientRect();
  if (!b.width || !b.height) return null;
  const w = (el instanceof HTMLVideoElement ? el.videoWidth : el.naturalWidth) || frameInfo.value?.w || b.width;
  const h = (el instanceof HTMLVideoElement ? el.videoHeight : el.naturalHeight) || frameInfo.value?.h || b.height;
  return { x: Math.min(w - 1, Math.max(0, ((e.clientX - b.left) / b.width) * w)), y: Math.min(h - 1, Math.max(0, ((e.clientY - b.top) / b.height) * h)) };
}
function syncMarker(): void {
  const m = gesture.marker;
  const w = frameInfo.value?.w || img.value?.naturalWidth || 1;
  const h = frameInfo.value?.h || img.value?.naturalHeight || 1;
  marker.value = m ? { px: (m.x / w) * 100, py: (m.y / h) * 100 } : null;
}
function pDown(e: PointerEvent): void {
  const p = imgPoint(e);
  if (!p) return;
  (e.target as Element).setPointerCapture?.(e.pointerId);
  focusEl.value?.focus({ preventScroll: true });
  gesture.down(p.x, p.y, e.button === 2 || e.shiftKey || e.altKey);
  syncMarker();
}
let lastMoveEv = 0;
function pMove(e: PointerEvent): void {
  if (!gesture.pressed) return;
  const t = performance.now();
  if (t - lastMoveEv < 45) return; // ~20/s
  lastMoveEv = t;
  const p = imgPoint(e);
  if (p) { gesture.move(p.x, p.y); syncMarker(); }
}
function pUp(e: PointerEvent): void {
  const p = imgPoint(e);
  if (p) gesture.up(p.x, p.y); else gesture.cancel();
  syncMarker();
  setTimeout(() => { if (!gesture.pressed) marker.value = null; }, 600);
}
function pCancel(): void { gesture.cancel(); marker.value = null; }

function onKey(e: KeyboardEvent): void {
  if (e.ctrlKey || e.metaKey) return;
  const k = mapKeyboardKey(e.key);
  if (!k) return;
  e.preventDefault();
  if (e.repeat) return;
  r.send('key', { key: k, action: 'press' });
}

onMounted(() => {
  r.onPoke(() => { if (!videoOn.value) poller.poke(); }, () => { if (poller.active) { snapNext = true; poller.poke(); } });
  applySource();
});
watch(supported, applySource);
watch(id, () => {
  if (video.active) { video.stop(); applySource(); } if (url.value) { toRevoke.push(url.value); url.value = ''; } frameInfo.value = null; poller.poke(); });
onBeforeUnmount(() => {
  r.onPoke(null);
  clearInterval(recTimer);
  video.stop();
  poller.pause();
  if (url.value) URL.revokeObjectURL(url.value);
  for (const u of toRevoke) URL.revokeObjectURL(u);
});
</script>

<style scoped>
.screen-area { display: flex; align-items: center; justify-content: center; min-width: 0; min-height: 0; padding: 8px; overflow: hidden; }
.screen-area.scroll { overflow: auto; align-items: flex-start; justify-content: flex-start; }
.preview-focus { outline: none; max-width: 100%; max-height: 100%; display: flex; }
.screen-area.scroll .preview-focus { max-width: none; max-height: none; }
.preview-focus:focus-within .preview-img { box-shadow: 0 0 0 2px var(--q-primary); }
.preview-wrap { position: relative; line-height: 0; max-height: 100%; display: flex; }
.preview-img { display: block; touch-action: none; user-select: none; cursor: crosshair; border: 1px solid rgba(128, 128, 128, 0.4); object-fit: contain; }
.touch-marker { position: absolute; width: 22px; height: 22px; margin: -11px 0 0 -11px; border: 2px solid #ff1744; border-radius: 50%; pointer-events: none; background: rgba(255, 23, 68, 0.25); }
.drawer { overflow-y: auto; border-left: 1px solid var(--ide-border); flex: none; }
.live-bar { flex: none; height: 30px; padding: 0 6px; border-top: 1px solid var(--ide-border); }
</style>
