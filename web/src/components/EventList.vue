<template>
  <q-list dense separator class="mono" style="font-size: 12px">
    <q-item v-if="!events.length"><q-item-section class="text-grey">no events</q-item-section></q-item>
    <q-expansion-item v-for="e in events" :key="e.id" dense expand-icon-toggle group="ev">
      <template #header>
        <q-item-section side style="min-width: 70px">{{ fmtTime(e.ts) }}</q-item-section>
        <q-item-section side>
          <q-badge :color="color(e.kind)" :label="e.kind" />
        </q-item-section>
        <q-item-section class="ellipsis">{{ brief(e) }}</q-item-section>
        <q-item-section v-if="showBoot" side class="text-grey">#{{ e.boot }}</q-item-section>
      </template>
      <pre class="json q-pa-sm">{{ pretty(e.data) }}</pre>
    </q-expansion-item>
  </q-list>
</template>

<script setup lang="ts">
import type { DeviceEvent } from '@/api/types';
import { fmtTime, pretty } from '@/utils';

withDefaults(defineProps<{ events: DeviceEvent[]; showBoot?: boolean }>(), { showBoot: false });

function color(k: string): string {
  if (k === 'fault' || k === 'panic') return 'negative';
  if (k.startsWith('boot.')) return 'primary';
  if (k.startsWith('link.')) return 'orange';
  if (k.startsWith('upload.')) return 'teal';
  if (k.startsWith('mon.')) return 'grey';
  return 'blue-grey';
}
function brief(e: DeviceEvent): string {
  const d = e.data;
  if (!d) return '';
  return Object.entries(d).map(([k, v]) => `${k}=${typeof v === 'object' ? JSON.stringify(v) : v}`).join(' ');
}
</script>
