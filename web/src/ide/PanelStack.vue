<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="ide-twbody">
    <div v-for="p in rendered" v-show="p.id === active" :key="p.id" class="absolute-full">
      <panel-host :def="p" />
    </div>
    <div v-if="!active" class="ide-empty">Nothing open</div>
  </div>
</template>

<script setup lang="ts">
// Mounts a panel the first time it is shown and keeps it alive (v-show) so terminals / live streams survive tab switches.
import { computed, reactive, watch } from 'vue';
import PanelHost from './PanelHost.vue';
import { panelById, type PanelDef } from './panels';

const props = defineProps<{ ids: string[]; active: string }>();
const seen = reactive(new Set<string>());
watch(() => props.active, (a) => { if (a) seen.add(a); }, { immediate: true });
const rendered = computed<PanelDef[]>(() =>
  props.ids.filter((i) => seen.has(i)).map((i) => panelById(i)).filter((p): p is PanelDef => !!p));
</script>
