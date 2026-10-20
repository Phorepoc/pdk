<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <term-group v-if="node.kind === 'group'" :group="node" />
  <div v-else ref="box" class="ide-split fit" :class="node.dir">
    <template v-for="(c, i) in node.children" :key="c.id">
      <div class="ide-split-cell" :style="{ flexGrow: node.sizes[i] }"><term-node :node="c" /></div>
      <div v-if="i < node.children.length - 1" class="ide-split-handle" @pointerdown="drag($event, i)" />
    </template>
  </div>
</template>

<script setup lang="ts">
// One node of the Terminal panel's split tree (styles shared with the editor's EditorNode).
import { ref } from 'vue';
import TermGroup from './TermGroup.vue';
import type { TNode } from './splitTree';

defineOptions({ name: 'TermNode' });
const props = defineProps<{ node: TNode }>();
const box = ref<HTMLElement | null>(null);

function drag(e: PointerEvent, i: number): void {
  const n = props.node;
  if (n.kind !== 'split' || !box.value) return;
  const rect = box.value.getBoundingClientRect();
  const sum = n.sizes.reduce((x, y) => x + y, 0);
  const total = (n.dir === 'row' ? rect.width : rect.height) - 4 * (n.children.length - 1);
  const start = n.dir === 'row' ? e.clientX : e.clientY;
  const a0 = n.sizes[i]!, b0 = n.sizes[i + 1]!;
  const el = e.target as HTMLElement;
  el.setPointerCapture(e.pointerId);
  const move = (ev: PointerEvent) => {
    const d = (((n.dir === 'row' ? ev.clientX : ev.clientY) - start) / total) * sum;
    const a = Math.min(a0 + b0 - sum * 0.05, Math.max(sum * 0.05, a0 + d));
    n.sizes.splice(i, 2, a, a0 + b0 - a);
  };
  const up = () => { el.removeEventListener('pointermove', move); el.removeEventListener('pointerup', up); };
  el.addEventListener('pointermove', move);
  el.addEventListener('pointerup', up);
}
</script>
