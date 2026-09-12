<script setup lang="ts">
import { ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import {
  getResource,
  restartDeployment,
  scaleDeployment,
  type ResourceKind,
} from '@/api/workload';
import type { ResourceItem } from '@/api/types';
import { humanizeError } from '@/utils/errorMessages';

/**
 * Deployment 伸缩 / Rollout Restart 两个操作弹窗的封装。
 * scaleTarget 置位时自动从详情读取当前 replicas;操作成功后 emit done(父级刷新列表)。
 */
const props = defineProps<{
  cluster: string;
  kind: ResourceKind;
  scaleTarget: ResourceItem | null;
  restartTarget: ResourceItem | null;
}>();

const emit = defineEmits<{
  'update:scaleTarget': [value: ResourceItem | null];
  'update:restartTarget': [value: ResourceItem | null];
  done: [];
}>();

const replicas = ref(0);

watch(
  () => props.scaleTarget,
  async (t) => {
    if (t === null) return;
    try {
      const detail = await getResource(props.cluster, props.kind, t.name, t.namespace);
      const spec = (detail['spec'] ?? {}) as { replicas?: number };
      replicas.value = spec.replicas ?? 0;
    } catch (e) {
      ElMessage.error(humanizeError(e));
      emit('update:scaleTarget', null);
    }
  },
);

async function onScale(): Promise<void> {
  if (props.scaleTarget === null) return;
  await scaleDeployment(props.cluster, props.scaleTarget.name, replicas.value, props.scaleTarget.namespace);
  ElMessage.success(`已伸缩至 ${replicas.value} 副本`);
  emit('update:scaleTarget', null);
  emit('done');
}

async function onRestart(): Promise<void> {
  if (props.restartTarget === null) return;
  await restartDeployment(props.cluster, props.restartTarget.name, props.restartTarget.namespace);
  ElMessage.success('已触发 Rollout Restart');
  emit('update:restartTarget', null);
  emit('done');
}
</script>

<template>
  <el-dialog
    :model-value="props.scaleTarget !== null"
    title="伸缩副本"
    width="360px"
    @update:model-value="(v: boolean) => !v && emit('update:scaleTarget', null)"
  >
    <el-input-number v-model="replicas" :min="0" :max="200" />
    <template #footer>
      <el-button @click="emit('update:scaleTarget', null)">取消</el-button>
      <el-button type="primary" @click="onScale">确认</el-button>
    </template>
  </el-dialog>

  <DangerConfirmDialog
    :model-value="props.restartTarget !== null"
    title="Rollout Restart Deployment"
    :confirm-keyword="props.restartTarget?.name ?? ''"
    :impact-list="['所有副本将滚动重建,期间服务可能抖动']"
    confirm-label="重启"
    @update:model-value="(v: boolean) => !v && emit('update:restartTarget', null)"
    @confirm="onRestart"
  />
</template>
