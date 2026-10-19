<template>
  <q-card flat bordered>
    <q-card-section>
      <div class="text-subtitle1 text-weight-medium">Memory</div>
      <div class="text-caption text-grey">peek / poke / dump as a table; the previous read is kept for a diff.</div>
    </q-card-section>
    <q-card-section class="q-pt-none">
      <q-tabs v-model="mode" dense align="left" no-caps class="q-mb-sm">
        <q-tab v-if="has('peek')" name="peek" label="peek" />
        <q-tab v-if="has('dump')" name="dump" label="dump" />
        <q-tab v-if="has('poke')" name="poke" label="poke" />
      </q-tabs>
      <div class="row q-col-gutter-sm items-start">
        <template v-if="mode === 'peek'">
          <q-input v-model="addr" label="addr" dense outlined class="col-12 col-sm-5" input-class="mono" placeholder="0x01c20000" :error="!!err.addr" :error-message="err.addr" />
          <q-input v-model.number="count" label="words" type="number" dense outlined class="col-6 col-sm-3" />
        </template>
        <template v-else-if="mode === 'dump'">
          <q-input v-model="ranges" label="ranges (addr:count,...)" dense outlined class="col-12 col-sm-8" input-class="mono" placeholder="0x01c20000:4,0x01c20100:8" :error="!!err.ranges" :error-message="err.ranges" />
        </template>
        <template v-else>
          <q-input v-model="addr" label="addr" dense outlined class="col-12 col-sm-4" input-class="mono" :error="!!err.addr" :error-message="err.addr" />
          <q-input v-model="value" label="value" dense outlined class="col-12 col-sm-4" input-class="mono" :error="!!err.value" :error-message="err.value" />
        </template>
        <div class="col-auto">
          <q-btn color="primary" no-caps :label="mode" :loading="busy" @click="go" />
        </div>
      </div>
      <div v-if="error" class="text-negative q-mt-sm">{{ error }}</div>
      <div v-else-if="note" class="text-positive q-mt-sm">{{ note }}</div>
    </q-card-section>
    <q-card-section v-if="rows.length" class="q-pt-none">
      <div class="row items-center q-mb-xs">
        <div class="text-caption col">{{ rows.length }} words<span v-if="prev.length"> vs previous read ({{ changed }} changed)</span></div>
        <q-toggle v-model="diffOnly" dense label="changed only" :disable="!prev.length" />
      </div>
      <q-markup-table dense flat bordered separator="horizontal" class="mono">
        <thead><tr><th class="text-left">addr</th><th class="text-left">value</th><th class="text-right">decimal</th><th v-if="prev.length" class="text-left">previous</th></tr></thead>
        <tbody>
          <tr v-for="r in visible" :key="r.addr" :class="r.changed ? 'bg-amber-2 text-black' : ''">
            <td>{{ r.addr }}</td><td>{{ r.value }}</td><td class="text-right">{{ r.dec }}</td>
            <td v-if="prev.length">{{ r.prev ?? '-' }}</td>
          </tr>
        </tbody>
      </q-markup-table>
    </q-card-section>
  </q-card>
</template>

<script setup lang="ts">
import { computed, ref } from 'vue';
import { api } from '@/api/client';
import { hex32, toNum } from '@/utils';

interface Word { addr: string; value: string }

const props = defineProps<{ device: string; ops: string[] }>();
const has = (n: string) => props.ops.includes(n);
const mode = ref(has('peek') ? 'peek' : has('dump') ? 'dump' : 'poke');
const addr = ref('');
const value = ref('');
const count = ref(4);
const ranges = ref('');
const busy = ref(false);
const error = ref('');
const note = ref('');
const err = ref<Record<string, string>>({});
const cur = ref<Word[]>([]);
const prev = ref<Word[]>([]);
const diffOnly = ref(false);

const HEX = /^(0x[0-9a-f]+|[0-9]+)$/i;

function words(r: unknown): Word[] {
  const arr = Array.isArray(r) ? r : r && typeof r === 'object' ? [r] : [];
  return arr.filter((w): w is Word => !!w && typeof w === 'object' && 'addr' in w && 'value' in w);
}

const prevMap = computed(() => new Map(prev.value.map((w) => [toNum(w.addr), w.value])));
const rows = computed(() => cur.value.map((w) => {
  const p = prevMap.value.get(toNum(w.addr));
  return { addr: hex32(w.addr), value: hex32(w.value), dec: toNum(w.value) >>> 0, prev: p === undefined ? undefined : hex32(p),
    changed: p !== undefined && toNum(p) !== toNum(w.value) };
}));
const visible = computed(() => (diffOnly.value ? rows.value.filter((r) => r.changed) : rows.value));
const changed = computed(() => rows.value.filter((r) => r.changed).length);

async function go(): Promise<void> {
  err.value = {};
  error.value = '';
  let args: Record<string, unknown> = {};
  if (mode.value === 'dump') {
    if (!ranges.value.trim()) err.value.ranges = 'required';
    args = { ranges: ranges.value.trim() };
  } else {
    if (!HEX.test(addr.value.trim())) err.value.addr = 'hex (0x...) or decimal';
    args = { addr: addr.value.trim() };
    if (mode.value === 'peek') args.count = count.value || 1;
    else {
      if (!HEX.test(value.value.trim())) err.value.value = 'hex (0x...) or decimal';
      args.value = value.value.trim();
    }
  }
  if (Object.keys(err.value).length) return;
  busy.value = true;
  try {
    const r = words(await api.op(props.device, mode.value, args));
    if (mode.value === 'poke') {
      note.value = r[0] ? `wrote ${hex32(r[0].value)} to ${hex32(r[0].addr)}` : 'ok';
    } else {
      note.value = '';
      if (cur.value.length) prev.value = cur.value;
      cur.value = r;
    }
  } catch (e) { error.value = (e as Error).message; }
  finally { busy.value = false; }
}
</script>
