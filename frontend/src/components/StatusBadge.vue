<script setup lang="ts">
import { computed } from 'vue';

const props = defineProps<{ status: string }>();

type Tone = 'success' | 'warning' | 'danger' | 'info' | 'neutral';

const TONE_MAP: Record<string, Tone> = {
  running: 'success',
  ready: 'success',
  connected: 'success',
  bound: 'success',
  available: 'success',
  succeeded: 'info',
  completed: 'info',
  active: 'success',
  pending: 'warning',
  containercreating: 'warning',
  degraded: 'warning',
  reconnecting: 'warning',
  warning: 'warning',
  failed: 'danger',
  crashloopbackoff: 'danger',
  imagepullbackoff: 'danger',
  evicted: 'danger',
  offline: 'danger',
  deny: 'danger',
  terminating: 'neutral',
  unknown: 'neutral',
  disabled: 'neutral',
};

const tone = computed<Tone>(() => {
  const s = props.status.toLowerCase();
  // 副本摘要 "d/w"(Deployment/StatefulSet/DaemonSet):全就绪绿、部分黄、零就绪红
  const m = /^(\d+)\/(\d+)$/.exec(s);
  if (m !== null) {
    const ready = Number(m[1]);
    const want = Number(m[2]);
    if (want === 0) return 'neutral';
    if (ready >= want) return 'success';
    return ready > 0 ? 'warning' : 'danger';
  }
  // Node Ready condition 的布尔结果
  if (s === 'true') return 'success';
  if (s === 'false') return 'warning';
  return TONE_MAP[s] ?? 'neutral';
});

const COLOR_VAR: Record<Tone, string> = {
  success: 'var(--success)',
  warning: 'var(--warning)',
  danger: 'var(--danger)',
  info: 'var(--info)',
  neutral: 'var(--neutral)',
};

const dotColor = computed(() => COLOR_VAR[tone.value]);
</script>

<template>
  <span class="status-badge">
    <span class="dot" :style="{ background: dotColor }" />
    <span class="text">{{ props.status }}</span>
  </span>
</template>

<style scoped>
.status-badge {
  display: inline-flex;
  align-items: center;
  gap: 6px;
}
.dot {
  width: 8px;
  height: 8px;
  border-radius: 50%;
  flex: none;
}
.text {
  color: var(--text-1);
}
</style>
