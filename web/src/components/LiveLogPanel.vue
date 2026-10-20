<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-splitter v-model="split" class="fit" :limits="[30, 90]" data-test="livelog">
    <template #before>
      <div class="fit q-pa-xs"><log-view :lines="deviceLines" live height="100%" /></div>
    </template>
    <template #after>
      <q-scroll-area class="fit"><event-list :events="deviceEvents" /></q-scroll-area>
    </template>
  </q-splitter>
</template>

<script setup lang="ts">
// The live log of the selected device, with its parsed events beside it.
import { computed, ref } from 'vue';
import EventList from '@/components/EventList.vue';
import LogView from '@/components/LogView.vue';
import { useDevicesStore } from '@/stores/devices';
import { useLiveStore } from '@/stores/live';

const devices = useDevicesStore();
const live = useLiveStore();
const split = ref(70);
const deviceLines = computed(() => live.lines.filter((l) => l.device === devices.currentId));
const deviceEvents = computed(() => live.events.filter((e) => e.device === devices.currentId && !e.kind.startsWith('mon.')).slice(0, 100));
</script>
