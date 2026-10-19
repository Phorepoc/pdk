<template>
  <q-menu auto-close dense>
    <q-list dense style="min-width: 150px">
      <template v-if="movable">
        <q-item-label header class="q-py-xs">Move to</q-item-label>
        <q-item v-for="d in targets" :key="d.id" clickable :disable="ide.st.where[id] === d.id" @click="ide.move(id, d.id)">
          <q-item-section avatar><q-icon :name="d.icon" size="xs" /></q-item-section>
          <q-item-section>{{ d.label }}</q-item-section>
        </q-item>
      </template>
      <slot />
    </q-list>
  </q-menu>
</template>

<script setup lang="ts">
import { useIdeStore } from './layout';
import type { DockId } from './panels';
withDefaults(defineProps<{ id: string; movable?: boolean }>(), { movable: true });
const ide = useIdeStore();
const targets: { id: DockId; label: string; icon: string }[] = [
  { id: 'left', label: 'Left dock', icon: 'align_horizontal_left' },
  { id: 'bottom', label: 'Bottom dock', icon: 'vertical_align_bottom' },
  { id: 'right', label: 'Right dock', icon: 'align_horizontal_right' },
  { id: 'editor', label: 'Editor area', icon: 'tab' },
];
</script>
