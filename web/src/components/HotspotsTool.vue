<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-scroll-area class="fit">
    <div class="q-pa-sm">
      <hotspots-panel v-if="devices.hasOp(id, 'hotspots')" :device="id" />
      <div v-else class="text-grey">This device has no <code>hotspots</code> op.</div>
    </div>
  </q-scroll-area>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import HotspotsPanel from '@/components/HotspotsPanel.vue';
import { useDevicesStore } from '@/stores/devices';

const devices = useDevicesStore();
const id = computed(() => devices.currentId);
</script>
