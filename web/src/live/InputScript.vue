<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="input-script" data-test="input-script">
    <div class="sect">Input script</div>
    <textarea v-model="script" class="ls-text mono" rows="7" spellcheck="false"
              placeholder="key menu&#10;wait 500&#10;tap 180 320&#10;swipe up&#10;text hello&#10;shot" />
    <div class="row items-center q-gutter-xs q-mt-xs no-wrap">
      <button class="ls run" :disabled="busy || !script.trim()" @click="run"><q-icon name="play_arrow" size="16px" /> Run</button>
      <q-spinner v-if="busy" size="16px" />
      <span class="text-caption ellipsis" :class="error ? 'text-negative' : 'hint'">{{ error || 'key, text, tap, long, swipe, wait, shot' }}</span>
    </div>
    <div v-for="(s, i) in steps" :key="i" class="step mono" :class="{ bad: s.error }">
      <q-icon :name="s.error ? 'close' : 'check'" size="12px" /> {{ s.step }}<span v-if="s.error"> -- {{ s.error }}</span>
      <img v-if="blob(s)" :src="api.blobUrl(blob(s)!)" class="shot" alt="screenshot" />
    </div>
  </div>
</template>

<script setup lang="ts">
// A sequence of input steps sent as one input op (keys, text, taps, swipes, waits and screenshots).
import { ref } from 'vue';
import { api } from '@/api/client';
import type { InputStep } from '@/api/types';
import { useRemoteStore } from '@/stores/remote';

const r = useRemoteStore();
const script = ref('');
const busy = ref(false);
const error = ref('');
const steps = ref<InputStep[]>([]);

const blob = (s: InputStep): string | undefined => (s.result as { blob?: string } | undefined)?.blob;

async function run(): Promise<void> {
  busy.value = true;
  error.value = '';
  steps.value = [];
  try {
    steps.value = (await api.op(r.id, 'input', { script: script.value })) as InputStep[];
  } catch (e) {
    error.value = (e as Error).message;
    const res = (e as { result?: unknown }).result;
    if (Array.isArray(res)) steps.value = res as InputStep[];
  } finally { busy.value = false; }
}
</script>

<style scoped>
.input-script { width: 232px; padding: 10px 10px 12px; border-radius: 20px; background: #26282c; color: #e8eaed; box-shadow: 0 1px 4px rgba(0, 0, 0, 0.4); }
.sect { font-size: 10px; text-transform: uppercase; letter-spacing: 0.8px; color: #9aa0a6; margin: 0 2px 4px; }
.ls-text { width: 100%; border: none; border-radius: 10px; padding: 6px 8px; background: #1b1c1f; color: #e8eaed; outline: none; font-size: 12px; resize: vertical; box-sizing: border-box; }
.ls-text:focus { box-shadow: 0 0 0 1px #1976d2; }
.ls { border: none; color: #e8eaed; background: #1976d2; cursor: pointer; display: flex; align-items: center; gap: 4px; font-size: 12px; height: 28px; border-radius: 14px; padding: 0 12px; }
.ls:disabled { opacity: 0.4; cursor: default; }
.hint { color: #9aa0a6; }
.step { font-size: 11px; margin-top: 4px; color: #9aa0a6; }
.step.bad { color: #f44336; }
.shot { display: block; width: 100%; margin-top: 4px; border-radius: 6px; }
</style>
