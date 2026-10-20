<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="live-settings" data-test="live-settings">
    <div class="sect">Refresh</div>
    <div class="seg">
      <button v-for="v in [0.2, 0.5, 1, 2, 5]" :key="v" class="ls" :class="{ on: r.intervalS === v }" @click="r.intervalS = v">{{ v }}s</button>
    </div>

    <div class="sect">Zoom</div>
    <div class="seg">
      <button v-for="z in zooms" :key="z.v" class="ls" :class="{ on: r.zoom === z.v }" @click="r.zoom = z.v">{{ z.l }}</button>
    </div>

    <div class="sect">Show</div>
    <div class="seg">
      <button v-for="w in views" :key="w.v" class="ls" :class="{ on: r.which === w.v }" :title="w.t" @click="r.which = w.v">{{ w.l }}</button>
    </div>

    <div class="sect">Swipe</div>
    <div class="swipe-row">
      <div class="cross">
        <button class="ls arrow" style="grid-area: u" title="Swipe up" @click="r.swipe('up')"><q-icon name="keyboard_double_arrow_up" size="16px" /></button>
        <button class="ls arrow" style="grid-area: l" title="Swipe left" @click="r.swipe('left')"><q-icon name="keyboard_double_arrow_left" size="16px" /></button>
        <button class="ls arrow" style="grid-area: r" title="Swipe right" @click="r.swipe('right')"><q-icon name="keyboard_double_arrow_right" size="16px" /></button>
        <button class="ls arrow" style="grid-area: d" title="Swipe down" @click="r.swipe('down')"><q-icon name="keyboard_double_arrow_down" size="16px" /></button>
      </div>
      <div class="column q-gutter-y-xs" style="flex: 1">
        <button class="ls pill" :class="{ on: r.dragMode }" title="A mouse drag on the screen is sent as one swipe" @click="r.dragMode = !r.dragMode">
          <q-icon name="swipe" size="14px" /> drag = swipe
        </button>
        <button class="ls pill" :class="{ on: r.store }" title="Keep every live frame in the Screenshots history" @click="r.store = !r.store">
          <q-icon name="save" size="14px" /> save frames
        </button>
      </div>
    </div>

    <template v-if="hasText">
      <div class="sect">Type text</div>
      <div class="text-row">
        <input v-model="text" class="ls-input" placeholder="text to type" @keyup.enter="typeText" />
        <button class="ls send" :disabled="!text" title="Type it on the phone" @click="typeText"><q-icon name="keyboard_return" size="16px" /></button>
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
// The live view's settings in the keypad's style: segmented refresh / zoom / layer, a swipe cross, toggles, text entry.
import { computed, ref } from 'vue';
import type { Which } from '@/api/remote';
import { useDevicesStore } from '@/stores/devices';
import { useRemoteStore } from '@/stores/remote';

const r = useRemoteStore();
const devices = useDevicesStore();
const hasText = computed(() => devices.hasOp(r.id, 'text'));
const text = ref('');
const zooms: { v: 'fit' | '1' | '2'; l: string }[] = [{ v: 'fit', l: 'Fit' }, { v: '1', l: '1x' }, { v: '2', l: '2x' }];
const views: { v: Which; l: string; t: string }[] = [
  { v: 2, l: 'All', t: 'The whole panel: status bar, OS screen, soft keys' },
  { v: 0, l: 'OS', t: 'The OS screen only' },
  { v: 1, l: 'Panel', t: 'The panel layer only' },
];
function typeText(): void { if (text.value) { r.send('text', { text: text.value }); text.value = ''; } }
</script>

<style scoped>
.live-settings { width: 232px; padding: 10px 10px 12px; border-radius: 20px; background: #26282c; color: #e8eaed; user-select: none; box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4); }
.sect { font-size: 10px; text-transform: uppercase; letter-spacing: 0.8px; color: #9aa0a6; margin: 8px 2px 4px; }
.sect:first-child { margin-top: 0; }
.ls { border: none; color: #e8eaed; background: #3a3d42; cursor: pointer; display: flex; align-items: center; justify-content: center; gap: 4px; padding: 0; font-size: 12px; }
.ls:hover { background: #474b51; }
.ls:active { background: #5a5e66; }
.ls.on { background: #1976d2; }
.ls.on:hover { background: #2186eb; }
.ls:disabled { opacity: 0.4; cursor: default; }
.seg { display: flex; gap: 3px; }
.seg .ls { flex: 1; height: 28px; }
.seg .ls:first-child { border-radius: 10px 4px 4px 10px; }
.seg .ls:last-child { border-radius: 4px 10px 10px 4px; }
.seg .ls:not(:first-child):not(:last-child) { border-radius: 4px; }
.swipe-row { display: flex; gap: 10px; align-items: center; }
.cross { display: grid; grid-template-areas: '. u .' 'l . r' '. d .'; grid-template-columns: repeat(3, 26px); grid-template-rows: repeat(3, 26px); gap: 2px;
  padding: 4px; border-radius: 50%; background: #3a3d42; }
.cross .arrow { background: transparent; border-radius: 50%; }
.cross .arrow:hover { background: #474b51; }
.pill { height: 28px; border-radius: 14px; padding: 0 10px; justify-content: flex-start; }
.text-row { display: flex; gap: 4px; }
.ls-input { flex: 1; min-width: 0; height: 30px; border: none; border-radius: 10px; padding: 0 10px; background: #1b1c1f; color: #e8eaed; outline: none; font-size: 12px; }
.ls-input:focus { box-shadow: 0 0 0 1px #1976d2; }
.send { width: 34px; height: 30px; border-radius: 10px; }
</style>
