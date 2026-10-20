<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-scroll-area class="fit" data-test="boots">
    <div class="q-pa-sm">
      <div class="row items-center q-gutter-sm no-wrap q-mb-sm">
        <q-select v-model="boot" :options="bootOptions" emit-value map-options dense outlined options-dense label="Boot" style="min-width: 260px" />
        <q-select v-model="compare" :options="compareOptions" emit-value map-options dense outlined options-dense clearable label="Compare with" style="min-width: 260px" />
        <q-input v-model="symbols" dense outlined label="symbol file (faults)" style="min-width: 240px" />
        <q-btn dense flat icon="refresh" @click="load"><q-tooltip>Reload</q-tooltip></q-btn>
        <q-spinner v-if="busy" size="18px" />
      </div>
      <div v-if="error" class="text-negative q-mb-sm">{{ error }}</div>

      <div class="row q-col-gutter-md">
        <div :class="compare ? 'col-12 col-lg-6' : 'col-12'">
          <boot-report-card v-if="report" :report="report" />
        </div>
        <div v-if="compare && reportB" class="col-12 col-lg-6">
          <boot-report-card :report="reportB" />
        </div>
      </div>

      <template v-if="diff">
        <div class="sect">Differences (numbers and addresses ignored)</div>
        <div v-if="Object.keys(diff.events).length" class="row q-gutter-xs q-mb-sm">
          <q-chip v-for="(v, k) in diff.events" :key="k" dense square :label="`${k}: ${v[0]} -> ${v[1]}`" />
        </div>
        <div class="row q-col-gutter-md">
          <div class="col-12 col-lg-6">
            <div class="text-caption q-mb-xs">Only in #{{ diff.a }} ({{ diff.only_a.length }})</div>
            <div v-for="(l, i) in diff.only_a" :key="i" class="mono line text-negative ellipsis">- {{ l }}</div>
          </div>
          <div class="col-12 col-lg-6">
            <div class="text-caption q-mb-xs">Only in #{{ diff.b }} ({{ diff.only_b.length }})</div>
            <div v-for="(l, i) in diff.only_b" :key="i" class="mono line text-positive ellipsis">+ {{ l }}</div>
          </div>
        </div>
      </template>
    </div>
  </q-scroll-area>
</template>

<script setup lang="ts">
// Boots: the report of any boot (faults with function names, panics, test lines) and the difference between two boots.
import { computed, onMounted, ref, watch } from 'vue';
import { api } from '@/api/client';
import type { Boot, BootDiff, BootReport } from '@/api/types';
import BootReportCard from '@/components/BootReportCard.vue';
import { useDevicesStore } from '@/stores/devices';
import { focusBoot } from '@/stores/focus';
import { hot } from '@/stores/hotspots';
import { fmtTs } from '@/utils';

const props = defineProps<{ arg?: string }>();
const devices = useDevicesStore();
const boots = ref<Boot[]>([]);
const boot = ref(0);
const compare = ref<number | null>(null);
const symbols = hot.symbols; // the symbol file is shared with Hotspots
const report = ref<BootReport | null>(null);
const reportB = ref<BootReport | null>(null);
const diff = ref<BootDiff | null>(null);
const busy = ref(false);
const error = ref('');

const label = (b: Boot) => `#${b.id}  slot ${b.slot || '?'}  ${b.outcome || ''}  ${fmtTs(b.started)}`;
const bootOptions = computed(() => boots.value.map((b) => ({ label: label(b), value: b.id })));
const compareOptions = computed(() => boots.value.filter((b) => b.id !== boot.value).map((b) => ({ label: label(b), value: b.id })));

async function load(): Promise<void> {
  const id = devices.currentId;
  if (!id) return;
  busy.value = true;
  error.value = '';
  try {
    if (!boots.value.length) {
      boots.value = await api.boots(id, 0, 100);
      if (!boot.value) boot.value = focusBoot.value || Number(props.arg) || boots.value[0]?.id || 0;
    }
    const sym = symbols.value ? { symbols: symbols.value } : {};
    report.value = boot.value ? (await api.op(id, 'boot_report', { boot: boot.value, ...sym })) as BootReport : null;
    if (compare.value) {
      reportB.value = (await api.op(id, 'boot_report', { boot: compare.value, ...sym })) as BootReport;
      // older boot first, so "only in the later boot" reads as what changed
      const [a, b] = [compare.value, boot.value].sort((x, y) => x - y);
      diff.value = (await api.op(id, 'diff_boots', { a, b })) as BootDiff;
    } else { reportB.value = null; diff.value = null; }
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}

onMounted(load);
watch([boot, compare], load);
// another view asked for a boot (Push: Open in Boots): it may be newer than the list
watch(focusBoot, (b) => { if (b) { boots.value = []; boot.value = b; void load(); } });
watch(() => devices.currentId, () => { boots.value = []; boot.value = 0; compare.value = null; void load(); });
</script>

<style scoped>
.sect { font-size: 10px; text-transform: uppercase; letter-spacing: 0.8px; opacity: 0.6; margin: 14px 0 6px; }
.line { font-size: 12px; padding: 1px 0; }
</style>
