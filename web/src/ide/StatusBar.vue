<template>
  <div class="ide-status">
    <span class="item" data-test="conn">
      <q-icon :name="live.connected ? 'wifi' : 'wifi_off'" :color="live.connected ? 'positive' : 'negative'" size="14px" />
      {{ live.connected ? 'live' : 'offline' }}
    </span>
    <span class="item" v-if="devices.currentId"><q-icon name="smartphone" size="14px" />{{ devices.currentId }}</span>
    <span class="item" v-if="h">{{ h.state }}<template v-if="h.slot"> | slot {{ h.slot }}</template><template v-if="h.boot"> | boot #{{ h.boot }}</template></span>
    <span class="item transfer" v-if="h?.busy_op" data-test="busy">
      <q-spinner v-if="!frac" size="12px" color="warning" /><q-icon v-else name="system_update_alt" size="14px" color="warning" />
      <span class="text-warning">{{ h.busy_op }}<template v-if="stage"> | {{ stage }}</template></span>
      <q-linear-progress v-if="frac" :value="frac" size="8px" rounded color="warning" track-color="grey-8" style="width: 160px" />
      <span v-if="frac">{{ Math.round(frac * 100) }}%</span>
    </span>
    <span v-for="t in transfers.list" :key="t.id" class="item transfer" data-test="transfer">
      <q-icon :name="t.error ? 'error' : 'upload'" :color="t.error ? 'negative' : 'primary'" size="14px" />
      <span class="ellipsis" style="max-width: 220px">{{ t.label }}</span>
      <q-linear-progress :value="t.total ? t.done / t.total : 0" :indeterminate="!t.total && !t.error" size="6px" style="width: 90px" :color="t.error ? 'negative' : 'primary'" />
      <span v-if="t.total">{{ Math.round((100 * t.done) / t.total) }}%</span>
      <q-tooltip v-if="t.error">{{ t.error }}</q-tooltip>
    </span>
    <span class="grow" />
    <span class="item click" @click="ide.reset()"><q-icon name="restart_alt" size="14px" />Reset layout</span>
    <span class="item click" @click="settings.dialog = true"><q-icon name="settings" size="14px" />Settings</span>
    <span class="item">PhoneLab {{ devices.version }}</span>
  </div>
</template>

<script setup lang="ts">
import { computed } from 'vue';
import { useDevicesStore } from '@/stores/devices';
import { useLiveStore } from '@/stores/live';
import { useSettingsStore } from '@/stores/settings';
import { useTransfersStore } from '@/stores/transfers';
import { useIdeStore } from './layout';

const devices = useDevicesStore();
const live = useLiveStore();
const settings = useSettingsStore();
const transfers = useTransfersStore();
const ide = useIdeStore();
const h = computed(() => devices.current?.health);
// a push reports its progress over the live socket (stage, done / total)
const prog = computed(() => live.progress[devices.currentId]);
const frac = computed(() => (prog.value && prog.value.total ? Math.min(1, prog.value.done / prog.value.total) : 0));
const stage = computed(() => prog.value?.stage ?? '');
</script>

<style scoped>
.transfer { gap: 6px; }
</style>
