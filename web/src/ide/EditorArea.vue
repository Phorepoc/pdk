<!-- Copyright The Phorepoc Authors
     SPDX-License-Identifier: AGPL-3.0-only -->

<template><editor-node :node="ide.st.editor" /></template>

<script setup lang="ts">
import EditorNode from './EditorNode.vue';
import { useIdeStore } from './layout';
const ide = useIdeStore();
</script>
