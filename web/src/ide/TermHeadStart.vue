<template>
  <div class="row items-center no-wrap q-pl-sm" style="flex: none">
    <q-icon name="terminal" size="16px" />
    <span class="term-title">Terminal</span>
    <q-btn flat dense round size="sm" icon="refresh" aria-label="check again" @click="$emit('refresh')"><q-tooltip>Check the phone again</q-tooltip></q-btn>
  </div>
</template>

<script setup lang="ts">
// The start of the Terminal's merged header: its title and refresh (on the top-left group only).
defineEmits<{ refresh: [] }>();
</script>

<style scoped>
.term-title { font-weight: 600; margin: 0 6px; font-size: 12px; }
</style>
