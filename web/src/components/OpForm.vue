<template>
  <div class="op-row" :class="{ open: shown !== undefined }">
    <form class="row items-center no-wrap q-gutter-x-sm" @submit.prevent="run">
      <span class="mono text-weight-medium op-name">{{ spec.name }}</span>
      <q-badge color="grey-8" :label="spec.capability" />
      <q-badge v-if="spec.reboots" color="warning" text-color="black" label="reboots" />
      <q-icon v-if="spec.description" name="info_outline" size="14px" class="text-grey"><q-tooltip max-width="420px">{{ spec.description }}</q-tooltip></q-icon>
      <div class="col row items-center q-gutter-x-xs no-wrap scroll-x">
        <template v-for="f in fields" :key="f.name">
          <q-checkbox v-if="f.kind === 'bool'" v-model="values[f.name]" toggle-indeterminate dense size="xs" :label="f.label" :data-field="f.name">
            <q-tooltip>{{ f.description || f.label }}{{ values[f.name] == null ? ' (default)' : '' }}</q-tooltip>
          </q-checkbox>
          <q-input v-else :model-value="values[f.name] as string | number | null" @update:model-value="(v) => (values[f.name] = v as string | number | null)"
                   dense outlined hide-bottom-space :data-field="f.name" class="op-field" :style="{ width: f.type === 'file' ? '220px' : '120px' }"
                   :type="f.kind === 'int' ? 'number' : 'text'" :placeholder="f.label"
                   :error="!!errors[f.name]" :input-class="f.kind === 'hex' ? 'mono' : ''">
            <q-tooltip>{{ errors[f.name] || f.description || f.label }}</q-tooltip>
          </q-input>
        </template>
      </div>
      <q-btn type="submit" dense unelevated size="sm" color="primary" icon="play_arrow" :loading="running" :aria-label="`Run ${spec.name}`" />
      <q-btn v-if="shown !== undefined" dense flat size="sm" icon="close" aria-label="clear result" @click="shown = undefined" />
    </form>
    <div v-if="shown !== undefined" class="q-mt-xs">
      <div :class="failed ? 'text-negative' : 'text-positive'" class="text-caption">
        {{ failed ? 'error: ' + errMsg : 'result' }} <span v-if="ms">({{ ms }} ms)</span>
      </div>
      <pre class="json mono q-pa-xs q-ma-none rounded-borders" style="background: rgba(128,128,128,.15); max-height: 260px; overflow: auto; font-size: 11px">{{ pretty(shown) }}</pre>
    </div>
  </div>
</template>

<style scoped>
.op-row { padding: 3px 8px; border-bottom: 1px solid rgba(128, 128, 128, 0.2); }
.op-row.open { background: rgba(128, 128, 128, 0.06); }
.op-name { min-width: 110px; }
.op-field :deep(.q-field__control) { height: 28px; min-height: 28px; }
.op-field :deep(.q-field__native) { padding: 0; font-size: 12px; }
.scroll-x { overflow-x: auto; }
</style>

<script setup lang="ts">
import { computed, reactive, ref } from 'vue';
import { Dialog } from 'quasar';
import { ApiError, api } from '@/api/client';
import { buildArgs, fieldsFor, validate, type FormValues } from '@/api/opform';
import type { OpSpec } from '@/api/types';
import { pretty } from '@/utils';

const props = defineProps<{ device: string; spec: OpSpec }>();
const emit = defineEmits<{ (e: 'result', r: unknown): void }>();

const fields = computed(() => fieldsFor(props.spec));
const values = reactive<FormValues>({});
const errors = ref<Record<string, string>>({});
const running = ref(false);
const shown = ref<unknown>(undefined);
const failed = ref(false);
const errMsg = ref('');
const ms = ref(0);

async function exec(): Promise<void> {
  running.value = true;
  failed.value = false;
  const t0 = performance.now();
  try {
    const r = await api.op(props.device, props.spec.name, buildArgs(props.spec, values));
    shown.value = r ?? null;
    emit('result', r);
  } catch (e) {
    failed.value = true;
    errMsg.value = (e as Error).message;
    shown.value = e instanceof ApiError && e.result !== undefined ? e.result : null;
  } finally {
    ms.value = Math.round(performance.now() - t0);
    running.value = false;
  }
}

function run(): void {
  errors.value = validate(props.spec, values);
  if (Object.keys(errors.value).length) return;
  if (props.spec.reboots) {
    Dialog.create({ title: `Run ${props.spec.name}?`, message: 'This operation reboots the device.', cancel: true })
      .onOk(() => { void exec(); });
  } else void exec();
}

defineExpose({ run, values, errors });
</script>
