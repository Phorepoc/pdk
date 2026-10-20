<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="column" :style="{ height }">
    <div class="row items-center q-gutter-x-sm q-mb-xs no-wrap">
      <q-input v-model="filter" dense outlined clearable placeholder="filter" class="col" debounce="150">
        <template #prepend><q-icon name="filter_alt" /></template>
      </q-input>
      <q-btn v-if="live" dense flat :icon="paused ? 'play_arrow' : 'pause'" :label="paused ? 'Resume' : 'Pause'" no-caps @click="togglePause" />
      <q-btn dense flat icon="content_copy" aria-label="copy" @click="copy"><q-tooltip>Copy visible lines</q-tooltip></q-btn>
      <q-btn dense flat icon="download" aria-label="download" @click="download"><q-tooltip>Download visible lines</q-tooltip></q-btn>
    </div>
    <div class="text-caption text-grey q-mb-xs">
      {{ shown.length }}<template v-if="shown.length !== source.length"> / {{ source.length }}</template> lines
      <span v-if="paused" class="text-warning"> (paused; {{ props.lines.length - source.length }} new)</span>
    </div>
    <q-virtual-scroll
      ref="vs"
      class="logbox col rounded-borders"
      style="border: 1px solid rgba(128,128,128,.35)"
      :items="shown"
      :virtual-scroll-item-size="18"
      @virtual-scroll="onScroll"
    >
      <template #default="{ item }">
        <div :key="(item as Line).id" class="logline" :class="{ bad: BAD_LINE.test((item as Line).text) }">
          <span v-if="showTs" class="ts">{{ fmtTime((item as Line).ts) }}</span>{{ (item as Line).text }}
        </div>
      </template>
    </q-virtual-scroll>
  </div>
</template>

<script setup lang="ts">
import { computed, nextTick, ref, watch } from 'vue';
import { copyToClipboard, Notify } from 'quasar';
import type { Line } from '@/api/types';
import { BAD_LINE, downloadText, fmtTime } from '@/utils';

const props = withDefaults(defineProps<{ lines: Line[]; live?: boolean; height?: string; showTs?: boolean }>(), {
  live: false, height: '420px', showTs: true,
});

const filter = ref<string | null>('');
const paused = ref(false);
const frozen = ref<Line[]>([]);
const vs = ref<{ scrollTo: (i: number, align?: string) => void } | null>(null);
let atEnd = true;

const source = computed(() => (paused.value ? frozen.value : props.lines));
const shown = computed(() => {
  const f = (filter.value ?? '').toLowerCase();
  return f ? source.value.filter((l) => l.text.toLowerCase().includes(f)) : source.value;
});

function togglePause(): void {
  if (!paused.value) frozen.value = props.lines.slice();
  paused.value = !paused.value;
}

function onScroll(d: { index: number; to: number }): void {
  atEnd = d.to >= shown.value.length - 2;
}

watch(
  () => [shown.value.length, shown.value[shown.value.length - 1]?.id],
  async () => {
    if (!props.live || paused.value || !atEnd) return;
    await nextTick();
    vs.value?.scrollTo(shown.value.length - 1, 'end-force');
  },
);

const text = () => shown.value.map((l) => l.text).join('\n');
async function copy(): Promise<void> {
  try { await copyToClipboard(text()); Notify.create({ message: `Copied ${shown.value.length} lines`, timeout: 1200 }); }
  catch { Notify.create({ type: 'negative', message: 'Copy failed' }); }
}
function download(): void { downloadText('phonelab-log.txt', text() + '\n'); }
</script>
