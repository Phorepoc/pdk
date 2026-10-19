<template>
  <div class="boot-report" data-test="boot-report">
    <div class="row items-center q-gutter-sm">
      <span class="text-subtitle1 text-weight-medium">Boot #{{ report.boot }}</span>
      <q-badge v-if="report.slot" color="grey-8" :label="`slot ${report.slot}`" />
      <q-badge :color="outcomeColor" :label="report.outcome || 'running'" />
      <span class="text-caption text-grey">{{ started }} | {{ report.lines }} lines</span>
    </div>
    <div v-if="report.matched" class="text-caption mono q-mt-xs ellipsis"><q-icon name="check" color="positive" /> {{ report.matched }}</div>

    <div class="row q-gutter-md q-mt-sm">
      <div class="stat"><div class="n" :class="{ 'text-negative': report.faults.length }">{{ report.faults.length }}</div><div class="l">faults</div></div>
      <div class="stat"><div class="n" :class="{ 'text-negative': report.panics.length }">{{ report.panics.length }}</div><div class="l">panics</div></div>
      <div class="stat"><div class="n text-positive">{{ report.tests.pass }}</div><div class="l">tests passed</div></div>
      <div class="stat"><div class="n" :class="{ 'text-negative': report.tests.fail }">{{ report.tests.fail }}</div><div class="l">tests failed</div></div>
    </div>

    <template v-if="report.faults.length">
      <div class="sect">Faults</div>
      <div v-for="(f, i) in report.faults" :key="i" class="mono line">
        <span v-if="f.function" class="text-warning">{{ f.function }}</span><span v-if="f.image" class="text-grey"> in {{ f.image }}</span>
        <span v-if="f.pc" class="text-grey"> ({{ f.pc }})</span>
        <div class="text-grey ellipsis">{{ f.line }}</div>
      </div>
    </template>
    <template v-if="report.panics.length">
      <div class="sect">Panics</div>
      <div class="mono line">{{ report.panics.join(', ') }}</div>
    </template>
    <template v-if="report.tests.lines.length">
      <div class="sect">Test lines</div>
      <div v-for="(l, i) in report.tests.lines" :key="i" class="mono line ellipsis" :class="/FAIL/.test(l) ? 'text-negative' : ''">{{ l }}</div>
    </template>
    <template v-if="eventKinds.length">
      <div class="sect">Events</div>
      <div class="row q-gutter-xs">
        <q-chip v-for="[k, n] in eventKinds" :key="k" dense square :label="`${k} ${n}`" />
      </div>
    </template>
  </div>
</template>

<script setup lang="ts">
// How one boot went (boot_report / push wait=): slot, outcome, faults with function names, panics, test lines.
import { computed } from 'vue';
import type { BootReport } from '@/api/types';

const props = defineProps<{ report: BootReport }>();
const started = computed(() => (props.report.started ? new Date(props.report.started).toLocaleString() : ''));
const outcomeColor = computed(() => (props.report.outcome === 'fault' ? 'negative' : props.report.outcome === 'healthy' ? 'positive' : 'grey'));
const eventKinds = computed(() => Object.entries(props.report.events).sort((a, b) => b[1] - a[1]));
</script>

<style scoped>
.stat { min-width: 70px; }
.stat .n { font-size: 20px; font-weight: 600; line-height: 1.1; }
.stat .l { font-size: 11px; opacity: 0.7; }
.sect { font-size: 10px; text-transform: uppercase; letter-spacing: 0.8px; opacity: 0.6; margin: 10px 0 4px; }
.line { font-size: 12px; padding: 1px 0; }
</style>
