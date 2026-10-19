<template>
  <q-page class="page-pad">
    <div v-if="!supported" class="text-grey">This device has no <code>profile</code> op.</div>
    <template v-else>
      <div class="row items-center q-gutter-sm q-mb-sm">
        <q-btn dense unelevated no-caps :color="paused ? 'positive' : 'primary'" :icon="paused ? 'play_arrow' : 'pause'" :label="paused ? 'Resume' : 'Pause'" @click="togglePause" />
        <q-btn-toggle v-model="intervalS" dense no-caps unelevated toggle-color="primary" :options="[1, 2, 5, 10].map((v) => ({ label: v + 's', value: v }))" />
        <q-btn dense outline no-caps icon="download" label="Export CSV" :disable="!hist.length" @click="exportCsv" />
        <q-btn dense flat no-caps icon="delete_sweep" label="Clear" @click="clear" />
        <span class="text-caption text-grey" data-test="status">{{ hist.length }} samples<template v-if="last"> | window {{ last.window_s.toFixed(1) }}s</template></span>
      </div>
      <div v-if="error" class="text-negative text-caption q-mb-sm">{{ error }}</div>

      <div v-if="last" class="row q-col-gutter-md q-mb-md">
        <div class="col-12 col-sm-6 col-lg-3">
          <div class="text-caption">Total CPU <b data-test="cpu-total">{{ last.cpu_total.toFixed(0) }}%</b></div>
          <spark-chart :series="[ser((p) => p.cpu_total)]" :max="100" label="total cpu" />
        </div>
        <div class="col-12 col-sm-6 col-lg-3">
          <div class="text-caption">Per core <b>{{ last.core_busy.map((c) => c.toFixed(0) + '%').join(' / ') }}</b></div>
          <spark-chart :series="coreSeries" :max="100" :area="false" label="per-core cpu" />
        </div>
        <div class="col-12 col-sm-6 col-lg-3">
          <div class="text-caption">RAM: free <b>{{ fmtBytes(last.free_ram) }}</b>, processes <b>{{ fmtBytes(hist.sumRam(last)) }}</b></div>
          <spark-chart :series="ramSeries" :area="false" label="ram" />
          <div class="text-caption text-grey">blue = free, green = sum of process RAM</div>
        </div>
        <div class="col-12 col-sm-6 col-lg-3">
          <div class="text-caption">Kernel heap <b>{{ fmtBytes(last.kernel_heap) }}</b></div>
          <spark-chart :series="[ser((p) => p.kernel_heap)]" label="kernel heap" />
        </div>
      </div>
      <div v-else class="text-grey q-mb-md">waiting for the first sample...</div>

      <div class="row q-col-gutter-md">
        <div class="col-12 col-lg-8">
          <div class="text-subtitle1">Processes</div>
          <q-markup-table dense flat bordered separator="horizontal" class="mono">
            <thead>
              <tr>
                <th v-for="c in cols" :key="c.key" :class="c.key === 'name' ? 'text-left' : 'text-right'" style="cursor: pointer" :data-col="c.key" @click="setSort(c.key)">
                  {{ c.label }}<q-icon v-if="sort.key === c.key" :name="sort.dir === -1 ? 'arrow_downward' : 'arrow_upward'" size="14px" />
                </th>
                <th>cpu history</th>
              </tr>
            </thead>
            <tbody>
              <tr v-for="p in procs" :key="p.id + p.name" :class="{ 'bg-blue-1 text-black': p.name === selected }" style="cursor: pointer" data-row="proc" @click="selected = selected === p.name ? '' : p.name">
                <td>{{ p.name }}</td>
                <td class="text-right">{{ p.cpu.toFixed(1) }}</td>
                <td class="text-right">{{ fmtBytes(p.ram) }}</td>
                <td class="text-right">{{ p.threads }}</td>
                <td class="text-right">{{ p.chunks }}</td>
                <td style="width: 90px"><spark-chart :series="[procSpark(p.name)]" width="80px" height="18px" :max="100" label="process cpu" /></td>
              </tr>
              <tr v-if="!procs.length"><td colspan="6" class="text-grey">no data</td></tr>
            </tbody>
          </q-markup-table>
        </div>
        <div class="col-12 col-lg-4">
          <div class="text-subtitle1">Threads <span v-if="selected" class="text-caption">of {{ selected }} <q-btn flat dense size="sm" icon="close" @click="selected = ''" /></span></div>
          <q-markup-table dense flat bordered separator="horizontal" class="mono">
            <thead><tr><th class="text-left">process</th><th class="text-left">thread</th><th class="text-right">cpu %</th></tr></thead>
            <tbody>
              <tr v-for="t in threads" :key="t.id" data-row="thread">
                <td>{{ t.process }}</td><td>{{ t.name }}</td><td class="text-right">{{ t.cpu.toFixed(1) }}</td>
              </tr>
              <tr v-if="!threads.length"><td colspan="3" class="text-grey">no data</td></tr>
            </tbody>
          </q-markup-table>
        </div>
      </div>

    </template>
  </q-page>
</template>

<script setup lang="ts">
import { computed, onBeforeUnmount, onMounted, ref, shallowRef, triggerRef, watch } from 'vue';
import { api, ApiError } from '@/api/client';
import { FramePoller, type FrameResult } from '@/api/remote';
import { SampleHistory, fmtBytes, samplesToCsv, sortProcs, type ProcKey, type ProfileResult, type SortSpec } from '@/api/profile';
import SparkChart from '@/components/SparkChart.vue';
import { useDevicesStore } from '@/stores/devices';
import { downloadText } from '@/utils';

interface PollResult extends FrameResult { data?: ProfileResult }

const devices = useDevicesStore();
const id = computed(() => devices.currentId);
const supported = computed(() => devices.hasOp(id.value, 'profile'));

const paused = ref(false);
const intervalS = ref(5);
const error = ref('');
const selected = ref('');
const sort = ref<SortSpec>({ key: 'cpu', dir: -1 });
const hist = new SampleHistory();
const ver = ref(0); // bumped per sample so computed values re-read the (non reactive) history
const lastRef = shallowRef<ProfileResult | null>(null);
const last = computed(() => (ver.value ? lastRef.value : lastRef.value));

const cols: { key: ProcKey; label: string }[] = [
  { key: 'name', label: 'name' }, { key: 'cpu', label: 'cpu %' }, { key: 'ram', label: 'ram' },
  { key: 'threads', label: 'threads' }, { key: 'chunks', label: 'chunks' },
];

const procs = computed(() => sortProcs(last.value?.processes ?? [], sort.value));
const threads = computed(() => (last.value?.threads ?? []).filter((t) => !selected.value || t.process === selected.value));
const coreSeries = computed(() => { void ver.value; return (last.value?.core_busy ?? []).map((_, i) => hist.coreSeries(i)); });
const ramSeries = computed(() => { void ver.value; return [hist.series((p) => p.free_ram), hist.series((p) => hist.sumRam(p))]; });

function ser(f: (p: ProfileResult) => number): number[] { void ver.value; return hist.series(f); }
function procSpark(name: string): number[] { void ver.value; return hist.procSeries(name, (q) => q.cpu); }

function setSort(key: ProcKey): void {
  sort.value = sort.value.key === key ? { key, dir: sort.value.dir === 1 ? -1 : 1 } : { key, dir: key === 'name' ? 1 : -1 };
}

const poller = new FramePoller<PollResult>({
  fetchFrame: async () => {
    try {
      const data = (await api.op(id.value, 'profile', { top: 25, window_ms: 1000 }, 'live')) as ProfileResult;
      return { status: 200, data };
    } catch (e) {
      const msg = (e as Error).message;
      if (/busy/i.test(msg)) return { status: 409 };
      return { status: e instanceof ApiError && e.status ? e.status : 502, error: msg };
    }
  },
  interval: () => intervalS.value * 1000,
  onFrame: (r) => {
    if (!r.data) return;
    error.value = '';
    hist.add(r.data);
    lastRef.value = r.data;
    ver.value++;
    triggerRef(lastRef);
  },
  onError: (r) => { error.value = `profile: ${r.error ?? r.status}`; },
});

function togglePause(): void {
  paused.value = !paused.value;
  if (paused.value) poller.pause(); else poller.start();
}
function clear(): void { hist.clear(); lastRef.value = null; ver.value++; }
function exportCsv(): void { downloadText('phonelab-profile.csv', samplesToCsv(hist.samples)); }

onMounted(() => { if (supported.value && !paused.value) poller.start(); });
watch(supported, (s) => { if (s && !paused.value) poller.start(); else poller.pause(); });
watch(id, clear);
onBeforeUnmount(() => poller.pause());
</script>
