<template>
  <svg :viewBox="`0 0 ${w} ${h}`" :width="width" :height="height" preserveAspectRatio="none" class="spark" role="img" :aria-label="label">
    <g v-for="(s, i) in series" :key="i">
      <polygon v-if="area && i === 0 && s.length > 1" :points="areaPts(s)" :fill="colors[i % colors.length]" fill-opacity="0.18" />
      <polyline v-if="s.length > 1" :points="pts(s)" fill="none" :stroke="colors[i % colors.length]" stroke-width="1.5" vector-effect="non-scaling-stroke" />
    </g>
  </svg>
</template>

<script setup lang="ts">
import { computed } from 'vue';

const props = withDefaults(defineProps<{
  series: number[][];
  max?: number;       // fixed y max (e.g. 100); otherwise auto
  width?: string;
  height?: string;
  area?: boolean;
  label?: string;
  colors?: string[];
}>(), { width: '100%', height: '60px', area: true, label: 'chart', colors: () => ['#1976d2', '#43a047', '#fb8c00', '#8e24aa', '#e53935'] });

const w = 200;
const h = 50;
const top = computed(() => props.max ?? Math.max(1, ...props.series.flat()));

function pts(s: number[]): string {
  const n = s.length;
  return s.map((v, i) => `${((i / (n - 1)) * w).toFixed(1)},${(h - Math.min(1, v / top.value) * (h - 2) - 1).toFixed(1)}`).join(' ');
}
function areaPts(s: number[]): string { return `0,${h} ${pts(s)} ${w},${h}`; }
</script>

<style scoped>
.spark { background: rgba(128, 128, 128, 0.1); border-radius: 3px; display: block; }
</style>
