<template>
  <div class="row items-center no-wrap" style="flex: none">
    <q-btn v-if="ttys" flat dense round size="sm" icon="menu" aria-label="ttys on the phone">
      <q-tooltip>{{ ttys.length }} ttys on the phone</q-tooltip>
      <q-menu auto-close dense>
        <q-list dense style="min-width: 180px">
          <q-item-label header class="q-py-xs">On the phone</q-item-label>
          <q-item v-for="t in ttys" :key="t.id" clickable @click="$emit('open', t.id)">
            <q-item-section>tty {{ t.id }} <span class="text-caption text-grey">{{ t.cols }}x{{ t.rows }} {{ t.title }}</span></q-item-section>
            <q-item-section side><q-badge :color="t.shell ? 'positive' : 'grey'" rounded /></q-item-section>
          </q-item>
          <q-item v-if="!ttys.length"><q-item-section class="text-grey">none</q-item-section></q-item>
        </q-list>
      </q-menu>
    </q-btn>
    <q-btn flat dense round size="sm" icon="more_vert" aria-label="options"><move-menu id="terminal" /></q-btn>
    <q-btn v-if="dock === 'bottom'" flat dense round size="sm" :icon="ide.bottomMax ? 'close_fullscreen' : 'open_in_full'" @click="ide.bottomMax = !ide.bottomMax" />
    <q-btn v-if="dock !== 'editor'" flat dense round size="sm" icon="remove" aria-label="hide" @click="ide.hide(dock)"><q-tooltip>Hide</q-tooltip></q-btn>
  </div>
</template>

<script setup lang="ts">
// The end of the Terminal's merged header: the phone's tty list and the tool window's actions (top-right group only).
import { computed } from 'vue';
import MoveMenu from './MoveMenu.vue';
import { useIdeStore } from './layout';

defineProps<{ ttys?: { id: number; cols: number; rows: number; shell: number; title: string }[] }>();
defineEmits<{ open: [number] }>();
const ide = useIdeStore();
const dock = computed(() => ide.st.where['terminal'] as 'left' | 'bottom' | 'right' | 'editor');
</script>
