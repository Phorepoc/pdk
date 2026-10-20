<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <editor-group v-if="node.kind === 'group'" :group="node" />
  <div v-else ref="box" class="ide-split fit" :class="node.dir">
    <template v-for="(c, i) in node.children" :key="c.id">
      <div class="ide-split-cell" :style="{ flexGrow: node.sizes[i] }"><editor-node :node="c" /></div>
      <div v-if="i < node.children.length - 1" class="ide-split-handle" @pointerdown="drag($event, i)" />
    </template>
  </div>
</template>

<script setup lang="ts">
// One node of the editor split tree: a tab group, or a row / column of nodes with drag handles between them.
import { ref } from 'vue';
import EditorGroup from './EditorGroup.vue';
import type { EditorNode as Node } from './layout';

defineOptions({ name: 'EditorNode' });
const props = defineProps<{ node: Node }>();
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

<style>
.ide-split { display: flex; min-width: 0; min-height: 0; }
.ide-split.row { flex-direction: row; }
.ide-split.column { flex-direction: column; }
/* sizes are flex-grow weights over a zero basis, so the handles always fit; the content fills its cell absolutely
   (a percentage height would not resolve inside a column) */
.ide-split-cell { position: relative; flex: 1 1 0; min-width: 0; min-height: 0; overflow: hidden; }
.ide-split-cell > * { position: absolute; inset: 0; }
.ide-split-handle { flex: 0 0 4px; background: var(--ide-border); }
.ide-split.row > .ide-split-handle { cursor: col-resize; }
.ide-split.column > .ide-split-handle { cursor: row-resize; }
.ide-split-handle:hover { background: var(--q-primary); }
</style>
