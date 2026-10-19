<template>
  <div class="column fit no-wrap" :class="{ focused: ctx.tree.focus === group.id && many }" @pointerdown.capture="ctx.tree.focus = group.id">
    <div class="ide-tabs" :class="{ 'drop-strip': stripOver }" @dragover="onStripOver" @dragleave="stripOver = false" @drop="onStripDrop">
      <term-head-start v-if="first" @refresh="ctx.probe()" />
      <div v-for="k in group.tabs" :key="k" class="tab" :class="{ active: k === group.active, dragging: ctx.dragKey.value === k }"
           draggable="true" @dragstart="onDragStart($event, k)" @dragend="ctx.dragKey.value = ''" @click="group.active = k">
        <q-icon name="terminal" size="14px" /><span>{{ ctx.ttyOf[k] ? `tty ${ctx.ttyOf[k]}` : 'new tty' }}</span>
        <q-btn flat dense round size="xs" icon="close" class="x" aria-label="close terminal" @click.stop="ctx.close(k)">
          <q-tooltip>Close the tab (the tty keeps running on the phone)</q-tooltip>
        </q-btn>
      </div>
      <q-btn flat dense round size="sm" icon="add" aria-label="new terminal" @click="ctx.add(0, group.id)"><q-tooltip>New tty here</q-tooltip></q-btn>
      <div style="flex: 1" />
      <term-head-end v-if="last" :ttys="ctx.ttys.value" @open="(n: number) => ctx.add(n)" />
    </div>
    <div class="col relative-position" style="background: #1e1f22; min-height: 0">
      <terminal-view v-for="k in group.tabs" v-show="k === group.active" :key="k" :device="ctx.device.value" :tty="ctx.ttyOf[k] ?? 0"
                     :visible="k === group.active" class="absolute-full" @attached="(n: number) => (ctx.ttyOf[k] = n)" @gone="ctx.gone()" />
      <div v-if="!group.tabs.length" class="ide-empty">No terminals. <q-btn flat dense no-caps label="Open one" @click="ctx.add(0, group.id)" /></div>
      <!-- while a terminal tab is dragged: an edge splits that way, the middle moves the tab here -->
      <div v-if="ctx.dragKey.value" class="drop-layer" @dragover="onOver" @dragleave="zone = null" @drop="onDrop">
        <div v-if="zone" class="drop-hint" :class="zone" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// One group of terminal tabs in the Terminal panel; tabs drag between groups and split them, as in the editor area.
import { computed, inject, ref } from 'vue';
import TermHeadEnd from './TermHeadEnd.vue';
import TermHeadStart from './TermHeadStart.vue';
import TerminalView from './TerminalView.vue';
import { cornerGroup, groupsOf, zoneAt, type TGroup, type Zone } from './splitTree';
import { TERM_CTX } from './termCtx';

const props = defineProps<{ group: TGroup }>();
const ctx = inject(TERM_CTX)!;
const zone = ref<Zone | null>(null);
const stripOver = ref(false);
const many = computed(() => groupsOf(ctx.tree.root).length > 1);
// the merged header: title + refresh on the top-left group, the tty list + window actions on the top-right one
const first = computed(() => cornerGroup(ctx.tree.root, 'left').id === props.group.id);
const last = computed(() => cornerGroup(ctx.tree.root, 'right').id === props.group.id);
const MIME = 'application/x-phonelab-tty';

function onDragStart(e: DragEvent, k: string): void {
  e.dataTransfer?.setData(MIME, k);
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  setTimeout(() => { ctx.dragKey.value = k; }, 0);
}
function finish(): string {
  const k = ctx.dragKey.value;
  zone.value = null; stripOver.value = false; ctx.dragKey.value = '';
  return k;
}
function onOver(e: DragEvent): void { e.preventDefault(); zone.value = zoneAt(e); }
function onDrop(e: DragEvent): void {
  e.preventDefault();
  const z = zone.value ?? zoneAt(e);
  const k = finish() || e.dataTransfer?.getData(MIME) || '';
  if (k) ctx.drop(k, props.group.id, z);
}
function onStripOver(e: DragEvent): void { if (ctx.dragKey.value) { e.preventDefault(); stripOver.value = true; } }
function onStripDrop(e: DragEvent): void {
  e.preventDefault();
  const k = finish() || e.dataTransfer?.getData(MIME) || '';
  if (k) ctx.drop(k, props.group.id, 'center');
}
</script>

<style scoped>
.focused > .ide-tabs { box-shadow: inset 0 -2px 0 var(--q-primary); }
</style>
