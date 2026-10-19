<template>
  <q-page class="page-pad">
    <div class="row q-col-gutter-sm items-center q-mb-sm">
      <q-select v-model="boot" :options="bootOptions" emit-value map-options dense outlined label="Boot" class="col-12 col-sm-5" />
      <q-input v-model="grep" dense outlined clearable label="grep (server side)" class="col-12 col-sm-4" @keyup.enter="newest" />
      <div class="col-12 col-sm-3 row q-gutter-x-xs no-wrap">
        <q-btn dense flat icon="first_page" no-caps label="Older" :disable="!lines.length" @click="older" />
        <q-btn dense flat icon="last_page" no-caps label="Newer" :disable="!lines.length" @click="newer" />
        <q-btn dense color="primary" no-caps label="Newest" icon="vertical_align_bottom" @click="newest" />
      </div>
    </div>
    <div v-if="error" class="text-negative q-mb-sm">{{ error }}</div>
    <log-view :lines="lines" height="calc(100vh - 230px)" />
  </q-page>
</template>

<script setup lang="ts">
import { computed, onMounted, ref, watch } from 'vue';
import { useRoute } from 'vue-router';
import { api } from '@/api/client';
import type { Boot, Line } from '@/api/types';
import { useDevicesStore } from '@/stores/devices';
import { fmtTs } from '@/utils';
import LogView from '@/components/LogView.vue';

const PAGE = 500;
const devices = useDevicesStore();
const route = useRoute();

const boots = ref<Boot[]>([]);
const boot = ref(0);
const grep = ref<string | null>('');
const lines = ref<Line[]>([]);
const error = ref('');

const bootOptions = computed(() => [
  { label: 'all boots', value: 0 },
  ...boots.value.map((b) => ({ label: `#${b.id}  slot ${b.slot || '?'}  ${b.outcome || ''}  ${fmtTs(b.started)}  (${b.lines} lines${b.faults ? ', ' + b.faults + ' faults' : ''})`, value: b.id })),
]);

async function run(q: { after?: number | undefined; before?: number | undefined }): Promise<void> {
  const id = devices.currentId;
  if (!id) return;
  error.value = '';
  try {
    const r = await api.lines(id, { boot: boot.value, grep: grep.value ?? '', limit: PAGE, ...q });
    if (r.length) lines.value = r;
    else if (!q.after && !q.before) lines.value = [];
  } catch (e) { error.value = (e as Error).message; }
}
const newest = () => run({});
const older = () => run({ before: lines.value[0]?.id });
const newer = () => run({ after: lines.value[lines.value.length - 1]?.id });

async function init(): Promise<void> {
  const id = devices.currentId;
  if (!id) return;
  try { boots.value = await api.boots(id, 0, 100); } catch (e) { error.value = (e as Error).message; }
  const q = Number(route.query.boot);
  if (q) boot.value = q;
  await newest();
}

onMounted(init);
watch(() => devices.currentId, init);
watch(boot, newest);
</script>
