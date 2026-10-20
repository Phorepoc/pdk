<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <q-splitter
    v-if="show" :model-value="size" unit="px" :limits="[120, Infinity]" :horizontal="horizontal" :reverse="!dockFirst"
    class="fit" :class="{ 'ide-dockarea': true }" @update:model-value="(v: number) => $emit('update:size', v)"
  >
    <template #before><slot :name="dockFirst ? 'dock' : 'main'" /></template>
    <template #after><slot :name="dockFirst ? 'main' : 'dock'" /></template>
  </q-splitter>
  <slot v-else name="main" />
</template>

<script setup lang="ts">
defineProps<{ show: boolean; size: number; horizontal?: boolean; dockFirst?: boolean }>();
defineEmits<{ 'update:size': [number] }>();
</script>
