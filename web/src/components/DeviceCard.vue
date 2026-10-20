<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-card flat bordered class="device-card">
    <q-card-section class="row items-center no-wrap q-pb-none">
      <div class="col">
        <div class="text-h6">{{ info.id }} <span class="text-caption text-grey">{{ info.driver }}</span></div>
      </div>
      <q-badge :color="h.state === 'connected' ? 'positive' : 'negative'" :label="h.state" data-test="state" />
    </q-card-section>
    <q-card-section class="q-pt-sm">
      <div class="row q-col-gutter-sm text-body2">
        <div class="col-6">port: <b>{{ h.port || '-' }}</b></div>
        <div class="col-6">slot: <b>{{ h.slot || '-' }}</b></div>
        <div class="col-6">boot: <b>#{{ h.boot }}</b></div>
        <div class="col-6">up: <b>{{ uptime }}</b></div>
        <div class="col-6">last byte: <b>{{ lastByte }}</b></div>
        <div class="col-6">reconnects: <b>{{ h.reconnects }}</b></div>
        <div v-if="h.busy_op" class="col-12">busy: <q-badge color="orange" :label="h.busy_op" /></div>
        <div v-if="h.last_error" class="col-12 text-negative">{{ h.last_error }}</div>
      </div>
      <div v-if="cpu.length" class="q-mt-sm">
        <div class="text-caption text-grey" data-test="mon-age">monitor: {{ monAge }}</div>
        <div v-for="(b, i) in cpu" :key="i" class="row items-center no-wrap q-mb-xs">
          <div style="width: 48px" class="text-caption">cpu{{ i }}</div>
          <div class="bar col"><div :style="{ width: Math.min(100, b) + '%' }" /></div>
          <div style="width: 40px" class="text-right text-caption">{{ b }}%</div>
        </div>
      </div>
      <div v-if="ram !== undefined" class="text-body2 q-mt-xs">free RAM: <b>{{ (ram / 1024).toFixed(1) }} MiB</b></div>
      <div v-if="sdio" class="text-body2">SD io: <b>{{ sdio.calls }}</b> calls, <b>{{ sdio.sectors }}</b> sectors, <b>{{ sdio.ms }}</b> ms</div>
    </q-card-section>
    <q-card-actions v-if="quick.length">
      <q-btn v-for="o in quick" :key="o.name" no-caps outline dense padding="xs md" :label="o.name" :disable="busy"
             :color="o.reboots ? 'warning' : 'primary'" @click="run(o)" />
    </q-card-actions>
  </q-card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { Dialog, Notify } from 'quasar';
import { api } from '@/api/client';
import type { DeviceInfo, OpSpec } from '@/api/types';
import { fmtDur } from '@/utils';
import { useNow } from '@/composables/useNow';
import { useDevicesStore } from '@/stores/devices';

const props = defineProps<{ info: DeviceInfo }>();
const devices = useDevicesStore();
const now = useNow();
const busy = ref(false);

const h = computed(() => props.info.health);
const cpu = computed(() => h.value.monitor?.['mon.cpu']?.busy ?? []);
const monAge = computed(() => {
  const t = devices.monAt[props.info.id];
  return t ? `updated ${fmtDur(Math.max(0, (now.value - t) / 1000))} ago` : 'from the last status';
});
const ram = computed(() => h.value.monitor?.['mon.ram']?.free_kb);
const sdio = computed(() => h.value.monitor?.['mon.sdio']);
const uptime = computed(() => {
  const t = h.value.boot_started ? Date.parse(h.value.boot_started) : NaN;
  return isNaN(t) ? '-' : fmtDur((now.value - t) / 1000);
});
const lastByte = computed(() => {
  const t = h.value.last_byte ? Date.parse(h.value.last_byte) : NaN;
  return isNaN(t) ? fmtDur(h.value.last_byte_age_s) + ' ago' : fmtDur((now.value - t) / 1000) + ' ago';
});
const quick = computed<OpSpec[]>(() => props.info.ops
  .filter((o) => o.capability === 'power' || o.capability === 'slots')
  .filter((o) => !(o.params ?? []).some((p) => p.required)));

function run(o: OpSpec): void {
  const go = async () => {
    busy.value = true;
    try {
      await api.op(props.info.id, o.name, {});
      Notify.create({ type: 'positive', message: `${o.name}: ok`, timeout: 1500 });
    } catch (e) {
      Notify.create({ type: 'negative', message: `${o.name}: ${(e as Error).message}` });
    } finally { busy.value = false; }
  };
  if (o.reboots) {
    Dialog.create({ title: `Run ${o.name}?`, message: `${o.description}. This reboots the device.`, cancel: true })
      .onOk(() => { void go(); });
  } else void go();
}
</script>
