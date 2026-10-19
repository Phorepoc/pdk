<template>
  <q-page class="q-pa-sm">
    <div v-if="!ops.length" class="text-grey">No device / ops.</div>
    <template v-else>
      <q-expansion-item v-if="memOps.length" dense dense-toggle icon="memory" label="Memory (peek / dump / poke)" class="q-mb-sm" header-class="text-weight-medium">
        <memory-panel :device="devices.currentId" :ops="memOps" />
      </q-expansion-item>
      <q-input v-model="search" dense outlined clearable placeholder="filter ops (name, capability, description)" class="q-mb-xs">
        <template #prepend><q-icon name="filter_alt" size="xs" /></template>
      </q-input>
      <div class="rounded-borders" style="border: 1px solid rgba(128,128,128,.25)">
        <op-form v-for="o in filtered" :key="o.name" :device="devices.currentId" :spec="o" />
      </div>
    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import MemoryPanel from '@/components/MemoryPanel.vue';
import OpForm from '@/components/OpForm.vue';
import { useDevicesStore } from '@/stores/devices';

const devices = useDevicesStore();
const search = ref<string | null>('');
const ops = computed(() => devices.current?.ops ?? []);
const memOps = computed(() => ['peek', 'dump', 'poke'].filter((n) => ops.value.some((o) => o.name === n)));
const filtered = computed(() => {
  const s = (search.value ?? '').toLowerCase();
  return ops.value.filter((o) => !s || o.name.includes(s) || o.capability.includes(s) || o.description.toLowerCase().includes(s));
});
</script>
