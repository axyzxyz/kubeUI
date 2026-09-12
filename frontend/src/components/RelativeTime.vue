<script setup lang="ts">
import { computed } from 'vue';
import { absoluteTime, relativeTime } from '@/utils/format';

const props = defineProps<{ value: string }>();

const relative = computed(() => relativeTime(props.value));
const absolute = computed(() => absoluteTime(props.value));
</script>

<template>
  <span class="relative-time num mono" :title="absolute">{{ relative }}</span>
</template>

<style scoped>
.relative-time {
  font-size: 12px;
  color: var(--text-2);
}
</style>
