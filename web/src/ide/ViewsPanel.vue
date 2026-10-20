<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-list dense data-test="views">
    <q-item v-for="p in views" :key="p.id" clickable :active="ide.shown.has(p.id)" @click="ide.openTab(p.id)">
      <q-item-section avatar><q-icon :name="p.icon" size="xs" /></q-item-section>
      <q-item-section>{{ p.title }}</q-item-section>
    </q-item>
    <q-item-label header class="q-py-xs">Opens as a tab in the focused editor group</q-item-label>
  </q-list>
</template>

<script setup lang="ts">
// The views that open as editor tabs (panels whose dock is the editor area).
import { computed } from 'vue';
import { useIdeStore } from './layout';
import { panelById } from './panels';

const ide = useIdeStore();
const views = computed(() => ide.panelsIn('editor').map((id) => panelById(id)!).filter(Boolean));
</script>
