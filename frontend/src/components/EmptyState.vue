<script setup lang="ts">
const props = defineProps<{
  icon?: string;
  title: string;
  description?: string;
}>();
</script>

<template>
  <div class="empty-state">
    <div class="icon">{{ props.icon ?? '◯' }}</div>
    <div class="title">{{ title }}</div>
    <div v-if="description" class="desc">{{ description }}</div>
    <slot name="extra" />
    <slot />
  </div>
</template>

<style scoped>
.empty-state {
  display: flex;
  flex-direction: column;
  align-items: center;
  gap: 8px;
  padding: 48px 0;
  color: var(--text-2);
}
.icon {
  font-size: 32px;
  color: var(--text-3);
}
.title {
  color: var(--text-1);
}
.desc {
  font-size: 13px;
  color: var(--text-3);
}
</style>
