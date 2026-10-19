<template>
  <div class="column fit no-wrap" data-test="terminal">
    <!-- no tabs to show (phetty unreachable): the merged header alone -->
    <div v-if="state !== 'running'" class="ide-tabs">
      <term-head-start @refresh="probe()" />
      <div style="flex: 1" />
      <term-head-end />
    </div>
    <div class="col relative-position" style="min-height: 0">
      <div v-if="state === 'running'" class="absolute-full"><term-node :node="tree.root" /></div>
      <div v-else class="ide-empty column items-center q-gutter-sm" data-test="tty-unavailable" style="background: #1e1f22">
        <q-spinner v-if="state === 'checking'" size="20px" />
        <template v-else>
          <q-icon name="terminal" size="32px" color="grey-6" />
          <div class="text-center" style="max-width: 460px">{{ message }}</div>
          <q-btn v-if="state === 'stopped' && message.includes('not running')" outline dense no-caps color="primary" icon="play_arrow" label="Start phetty" @click="probe(true)" />
        </template>
      </div>
    </div>
  </div>
</template>

<script setup lang="ts">
// The phone's phetty ttys. Each tab attaches to one tty (a new one, or one from the list); closing a tab leaves the tty
// running on the phone. Tabs split the panel by dragging (splitTree.ts); a moved tab re-attaches to its tty, whose
// scrollback the phone replays. When phetty cannot be reached the panel says why instead of a dead terminal.
import { computed, onMounted, provide, reactive, ref, watch } from 'vue';
import { api } from '@/api/client';
import { useDevicesStore } from '@/stores/devices';
import TermHeadEnd from './TermHeadEnd.vue';
import TermHeadStart from './TermHeadStart.vue';
import TermNode from './TermNode.vue';
import { addTab, dropTab, groupsOf, newTree, removeTab } from './splitTree';
import { TERM_CTX } from './termCtx';

type State = 'checking' | 'running' | 'stopped' | 'unsupported' | 'offline';
const devices = useDevicesStore();
const state = ref<State>('checking');
const message = ref('');
const ttys = ref<{ id: number; cols: number; rows: number; shell: number; title: string }[]>([]);
const tree = reactive(newTree());
const ttyOf = reactive<Record<string, number>>({});
const dragKey = ref('');
let seq = 0;

async function probe(start = false): Promise<void> {
  if (!devices.currentId) return;
  if (state.value !== 'running') state.value = 'checking';
  try {
    const r = await api.ttys(devices.currentId, start);
    if (!r.implemented) {
      state.value = 'unsupported';
      message.value = 'This device has no terminal service.';
    } else if (!r.running) {
      state.value = 'stopped';
      message.value = r.error || 'phetty is not running on the phone.';
    } else {
      ttys.value = r.ttys;
      state.value = 'running';
      if (!groupsOf(tree.root).some((g) => g.tabs.length)) add(r.ttys[0]?.id ?? 0);
    }
  } catch (e) {
    state.value = 'offline';
    message.value = `Cannot reach the phone: ${(e as Error).message}`;
  }
}

function add(id: number, groupId?: number): void {
  const open = id ? Object.keys(ttyOf).find((k) => ttyOf[k] === id) : undefined;
  if (open) {
    const g = groupsOf(tree.root).find((x) => x.tabs.includes(open));
    if (g) { g.active = open; tree.focus = g.id; }
    return;
  }
  const key = `t${++seq}`;
  ttyOf[key] = id;
  addTab(tree, key, groupId);
}
function close(key: string): void { removeTab(tree, key); delete ttyOf[key]; }
function reset(): void { Object.assign(tree, newTree()); for (const k of Object.keys(ttyOf)) delete ttyOf[k]; }

provide(TERM_CTX, {
  tree, ttyOf, device: computed(() => devices.currentId), dragKey,
  add, close, drop: (k, g, z) => dropTab(tree, k, g, z), gone: () => void probe(), probe: () => void probe(), ttys,
});

onMounted(() => { void probe(); });
watch(() => devices.currentId, () => { reset(); void probe(); });
// a reboot drops the ttys: check again once the new boot is up
watch(() => devices.current?.health.boot, (b, old) => { if (old !== undefined && b !== old) { reset(); setTimeout(() => void probe(), 15000); } });
</script>
