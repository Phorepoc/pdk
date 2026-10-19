<template>
  <div class="ide-tw" :class="{ focused: ide.st.focus === group.id && ide.groupCount > 1 }" @pointerdown.capture="ide.st.focus = group.id">
    <div class="ide-tabs" :class="{ 'drop-strip': stripOver }" @dragover="onStripOver" @dragleave="stripOver = false" @drop="onStripDrop">
      <div v-for="t in group.tabs" :key="t" class="tab" :class="{ active: t === group.active, dragging: ide.dragTab === t }" :title="panelById(t)?.arg"
           draggable="true" @dragstart="onDragStart($event, t)" @dragend="ide.dragTab = ''" @click="group.active = t">
        <q-icon :name="panelById(t)?.icon" size="14px" />
        <span>{{ panelById(t)?.title }}</span>
        <q-btn flat dense round size="xs" icon="close" class="x" aria-label="close tab" @click.stop="ide.closeTab(t)" />
        <move-menu :id="t" context-menu :movable="!isDynamic(t)">
          <q-separator />
          <q-item clickable :disable="group.tabs.length < 2" @click="ide.splitTab(t, 'row')"><q-item-section avatar><q-icon name="vertical_split" size="xs" /></q-item-section><q-item-section>Split right</q-item-section></q-item>
          <q-item clickable :disable="group.tabs.length < 2" @click="ide.splitTab(t, 'column')"><q-item-section avatar><q-icon name="horizontal_split" size="xs" /></q-item-section><q-item-section>Split down</q-item-section></q-item>
        </move-menu>
      </div>
      <div class="spacer" style="flex: 1" />
    </div>
    <div class="relative-position" style="flex: 1; min-height: 0; display: flex; flex-direction: column">
      <panel-stack :ids="group.tabs" :active="group.active" />
      <!-- while a tab is dragged: drop on an edge to split that way, in the middle to move the tab here -->
      <div v-if="ide.dragTab" class="drop-layer" @dragover="onOver" @dragleave="zone = null" @drop="onDrop">
        <div v-if="zone" class="drop-hint" :class="zone" />
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// One editor group (a tab strip). Tabs are dragged JetBrains-style: onto another group's tab strip or middle = move
// there; onto an edge of any group = split that way. The right-click menu keeps Split right / down.
import { ref } from 'vue';
import MoveMenu from './MoveMenu.vue';
import PanelStack from './PanelStack.vue';
import { useIdeStore, type DropZone, type Group } from './layout';
import { isDynamic, panelById } from './panels';

const props = defineProps<{ group: Group }>();
const ide = useIdeStore();
const zone = ref<DropZone | null>(null);
const stripOver = ref(false);
const MIME = 'application/x-phonelab-tab';

function onDragStart(e: DragEvent, t: string): void {
  e.dataTransfer?.setData(MIME, t);
  if (e.dataTransfer) e.dataTransfer.effectAllowed = 'move';
  // set after the browser took its drag image, so the overlay does not swallow the start
  setTimeout(() => { ide.dragTab = t; }, 0);
}

function zoneAt(e: DragEvent): DropZone {
  const r = (e.currentTarget as HTMLElement).getBoundingClientRect();
  const x = (e.clientX - r.left) / r.width;
  const y = (e.clientY - r.top) / r.height;
  const edge = Math.min(x, 1 - x, y, 1 - y);
  if (edge > 0.25) return 'center';
  if (edge === x) return 'left';
  if (edge === 1 - x) return 'right';
  return edge === y ? 'top' : 'bottom';
}
function onOver(e: DragEvent): void {
  if (!ide.dragTab) return;
  e.preventDefault();
  zone.value = zoneAt(e);
}
function finish(): void { zone.value = null; stripOver.value = false; ide.dragTab = ''; }
function onDrop(e: DragEvent): void {
  e.preventDefault();
  const t = ide.dragTab || e.dataTransfer?.getData(MIME) || '';
  const z = zone.value ?? zoneAt(e);
  finish();
  if (t) ide.dropTab(t, props.group.id, z);
}
function onStripOver(e: DragEvent): void {
  if (!ide.dragTab) return;
  e.preventDefault();
  stripOver.value = true;
}
function onStripDrop(e: DragEvent): void {
  e.preventDefault();
  const t = ide.dragTab || e.dataTransfer?.getData(MIME) || '';
  finish();
  if (t) ide.dropTab(t, props.group.id, 'center');
}
</script>

<style>
.ide-tw.focused > .ide-tabs { box-shadow: inset 0 -2px 0 var(--q-primary); }
.ide-tabs.drop-strip { background: rgba(53, 116, 240, 0.18); }
.ide-tabs .tab.dragging { opacity: 0.4; }
.drop-layer { position: absolute; inset: 0; z-index: 10; }
.drop-hint { position: absolute; background: rgba(53, 116, 240, 0.25); border: 2px solid rgba(53, 116, 240, 0.8); pointer-events: none; transition: all 0.08s; }
.drop-hint.center { inset: 0; }
.drop-hint.left { inset: 0 50% 0 0; }
.drop-hint.right { inset: 0 0 0 50%; }
.drop-hint.top { inset: 0 0 50% 0; }
.drop-hint.bottom { inset: 50% 0 0 0; }
</style>
