<template>
  <q-btn flat dense round :icon="panel.icon" class="ide-stripbtn" :class="{ active, shown }" :aria-label="panel.title" :data-test="'strip-' + panel.id" @click="ide.activate(panel.id)">
    <q-tooltip anchor="center right" self="center left" :offset="[8, 0]">{{ panel.title }}</q-tooltip>
  </q-btn>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useIdeStore } from './layout';
import type { PanelDef } from './panels';

const props = defineProps<{ panel: PanelDef }>();
const ide = useIdeStore();
const shown = computed(() => ide.shown.has(props.panel.id));
const active = computed(() => shown.value && ide.st.where[props.panel.id] !== 'editor');
</script>
