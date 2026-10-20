<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="phone-keypad" data-test="keypad">
    <!-- softkeys, call / end, menu / clear around the navigation ring -->
    <div class="nav">
      <button class="pk soft" style="grid-area: lsk" title="Left softkey" v-bind="r.hold('lsk')"><span class="bar" /></button>
      <button class="pk soft" style="grid-area: rsk" title="Right softkey" v-bind="r.hold('rsk')"><span class="bar" /></button>
      <button class="pk call" style="grid-area: green" title="Call" v-bind="r.hold('green')"><q-icon name="call" size="18px" /></button>
      <button class="pk end" style="grid-area: red" title="End" v-bind="r.hold('red')"><q-icon name="call_end" size="18px" /></button>
      <button class="pk small" style="grid-area: menu" title="Menu" v-bind="r.hold('menu')"><q-icon name="apps" size="16px" /></button>
      <button class="pk small" style="grid-area: clear" title="Clear" v-bind="r.hold('backspace')"><q-icon name="backspace" size="16px" /></button>
      <div class="ring" style="grid-area: pad">
        <button class="pk arrow" style="grid-area: u" title="Up" v-bind="r.hold('up')"><q-icon name="keyboard_arrow_up" /></button>
        <button class="pk arrow" style="grid-area: l" title="Left" v-bind="r.hold('left')"><q-icon name="keyboard_arrow_left" /></button>
        <button class="pk ok" style="grid-area: c" title="Select" v-bind="r.hold('select')" />
        <button class="pk arrow" style="grid-area: r" title="Right" v-bind="r.hold('right')"><q-icon name="keyboard_arrow_right" /></button>
        <button class="pk arrow" style="grid-area: d" title="Down" v-bind="r.hold('down')"><q-icon name="keyboard_arrow_down" /></button>
      </div>
    </div>
    <!-- the 12 keys -->
    <div class="digits">
      <button v-for="d in digits" :key="d.k" class="pk digit" v-bind="r.hold(d.key)">
        <span class="n">{{ d.k }}</span><span class="l">{{ d.l }}</span>
      </button>
    </div>
    <!-- the side keys -->
    <div class="side">
      <button class="pk side-key" title="Volume down" v-bind="r.hold('voldown')"><q-icon name="volume_down" size="16px" /></button>
      <button class="pk side-key" title="Volume up" v-bind="r.hold('volup')"><q-icon name="volume_up" size="16px" /></button>
      <button class="pk side-key" title="Camera" v-bind="r.hold('camera')"><q-icon name="photo_camera" size="16px" /></button>
      <button class="pk side-key" title="Edit (pencil)" v-bind="r.hold('pencil')"><q-icon name="edit" size="16px" /></button>
      <button class="pk side-key" title="Lock / unlock" @click="r.send('key', { key: 'lock', action: 'press' })"><q-icon name="lock" size="16px" /></button>
      <button class="pk side-key power" title="Power (hold for a long press)" v-bind="r.hold('power')"><q-icon name="power_settings_new" size="16px" /></button>
    </div>
  </div>
</template>

<script setup lang="ts">
// A phone-shaped keypad: softkeys, call / end, the navigation ring, the 12 keys with their letters, the side keys.
// Every key is press-and-hold (pointer down = key down), so long presses work as on the phone.
import { useRemoteStore } from '@/stores/remote';

const r = useRemoteStore();
const digits = [
  { k: '1', l: '.,?', key: '1' }, { k: '2', l: 'abc', key: '2' }, { k: '3', l: 'def', key: '3' },
  { k: '4', l: 'ghi', key: '4' }, { k: '5', l: 'jkl', key: '5' }, { k: '6', l: 'mno', key: '6' },
  { k: '7', l: 'pqrs', key: '7' }, { k: '8', l: 'tuv', key: '8' }, { k: '9', l: 'wxyz', key: '9' },
  { k: '*', l: '+', key: 'star' }, { k: '0', l: 'space', key: '0' }, { k: '#', l: 'shift', key: 'hash' },
];
</script>

<style scoped>
.phone-keypad { width: 232px; padding: 12px 10px; border-radius: 20px; background: #26282c; user-select: none; box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4); }
.pk { border: none; color: #e8eaed; background: #3a3d42; cursor: pointer; display: flex; align-items: center; justify-content: center; touch-action: none; padding: 0; }
.pk:hover { background: #474b51; }
.pk:active { background: #5a5e66; }
.nav { display: grid; grid-template-columns: 52px 1fr 52px; grid-template-rows: 28px 34px 34px; gap: 5px;
  grid-template-areas: 'lsk pad rsk' 'green pad red' 'menu pad clear'; margin-bottom: 12px; }
.soft { border-radius: 12px 12px 4px 4px; }
.soft .bar { width: 22px; height: 3px; border-radius: 2px; background: #9aa0a6; }
.call { border-radius: 10px; color: #4caf50; }
.end { border-radius: 10px; color: #f44336; }
.small { border-radius: 10px; }
.ring { display: grid; grid-template-areas: '. u .' 'l c r' '. d .'; grid-template-columns: 30px 40px 30px; grid-template-rows: 30px 40px 30px;
  border-radius: 50%; background: #3a3d42; width: 108px; height: 108px; place-content: center; margin: auto; }
.ring .arrow { background: transparent; border-radius: 50%; }
.ring .arrow:hover { background: #474b51; }
.ok { border-radius: 50%; background: #1976d2; }
.ok:hover { background: #2186eb; }
.digits { display: grid; grid-template-columns: repeat(3, 1fr); gap: 6px; margin-bottom: 12px; }
.digit { height: 42px; border-radius: 12px; flex-direction: column; line-height: 1; }
.digit .n { font-size: 18px; }
.digit .l { font-size: 9px; color: #9aa0a6; margin-top: 3px; text-transform: uppercase; letter-spacing: 0.5px; }
.side { display: flex; gap: 4px; justify-content: space-between; }
.side-key { width: 32px; height: 26px; border-radius: 6px; }
.power { color: #f44336; }
</style>
