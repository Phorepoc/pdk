<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: Apache-2.0 -->

<template>
  <!-- pages built on q-page need a q-layout: give each its own container layout -->
  <q-layout v-if="def.page" container view="hhh lpr fff" class="ide-host">
    <q-page-container><component :is="def.component" v-bind="def.arg !== undefined ? { arg: def.arg } : {}" /></q-page-container>
  </q-layout>
  <component :is="def.component" v-else v-bind="def.arg !== undefined ? { arg: def.arg } : {}" />
</template>

<script setup lang="ts">
import type { PanelDef } from './panels';
defineProps<{ def: PanelDef }>();
</script>

<style>
.ide-host { height: 100%; width: 100%; background: transparent; }
</style>
