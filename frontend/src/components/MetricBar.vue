<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{ percent: number; label?: string }>();

const clamped = computed(() => Math.min(100, Math.max(0, props.percent)));

const color = computed(() => {
  if (clamped.value > 90) return 'var(--danger)';
  if (clamped.value > 60) return 'var(--warning)';
  return 'var(--success)';
});
</script>

<template>
  <div class="metric-bar">
    <div class="track">
      <div class="fill" :style="{ width: clamped + '%', background: color }" />
    </div>
    <span class="num value" :style="{ color }">{{ clamped.toFixed(0) }}%</span>
    <span v-if="props.label" class="label">{{ props.label }}</span>
  </div>
</template>

<style scoped>
.metric-bar {
  display: flex;
  align-items: center;
  gap: 8px;
  min-width: 120px;
}
.track {
  flex: 1;
  height: 6px;
  border-radius: 3px;
  background: var(--bg-inset);
  overflow: hidden;
}
.fill {
  height: 100%;
  border-radius: 3px;
}
.value {
  font-size: 12px;
  width: 36px;
  text-align: right;
}
.label {
  font-size: 12px;
  color: var(--text-3);
}
</style>
