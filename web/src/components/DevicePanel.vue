<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-scroll-area class="fit" data-test="device-panel">
    <div class="q-pa-sm">
      <div v-if="!devices.list.length" class="text-grey">{{ devices.loaded ? 'No devices configured.' : devices.error || 'Loading...' }}</div>
      <q-list v-if="devices.list.length > 1" dense bordered class="rounded-borders q-mb-sm">
        <q-item v-for="d in devices.list" :key="d.id" clickable :active="d.id === devices.currentId" @click="settings.device = d.id">
          <q-item-section avatar><q-icon name="smartphone" size="xs" /></q-item-section>
          <q-item-section>{{ d.id }}</q-item-section>
          <q-item-section side><q-badge :color="d.health.state === 'connected' ? 'positive' : 'negative'" rounded /></q-item-section>
        </q-item>
      </q-list>
      <device-card v-if="devices.current" :info="devices.current" />
    </div>
  </q-scroll-area>
</template>

<script setup lang="ts">
// The connected / selected device: health, cpu and ram monitors, quick power and slot actions.
import DeviceCard from '@/components/DeviceCard.vue';
import { useDevicesStore } from '@/stores/devices';
import { useSettingsStore } from '@/stores/settings';

const devices = useDevicesStore();
const settings = useSettingsStore();
</script>
