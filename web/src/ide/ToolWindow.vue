<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <div class="ide-tw">
    <div v-if="!def?.ownHeader" class="ide-twhead">
      <q-icon :name="def?.icon" size="16px" />
      <span class="title">{{ def?.title }}</span>
      <slot name="actions" />
      <q-btn flat dense round size="sm" icon="more_vert" aria-label="options"><move-menu :id="id" /></q-btn>
      <q-btn v-if="dock === 'bottom'" flat dense round size="sm" :icon="ide.bottomMax ? 'close_fullscreen' : 'open_in_full'" @click="ide.bottomMax = !ide.bottomMax" />
      <q-btn flat dense round size="sm" icon="remove" aria-label="hide" @click="ide.hide(dock)"><q-tooltip>Hide</q-tooltip></q-btn>
    </div>
    <panel-stack :ids="ide.panelsIn(dock)" :active="id" />
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import MoveMenu from './MoveMenu.vue';
import PanelStack from './PanelStack.vue';
import { useIdeStore } from './layout';
import { panelById } from './panels';

const props = defineProps<{ dock: 'left' | 'bottom' | 'right'; id: string }>();
const ide = useIdeStore();
const def = computed(() => panelById(props.id));
</script>
