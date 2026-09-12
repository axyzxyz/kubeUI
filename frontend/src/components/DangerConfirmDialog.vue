<script setup lang="ts">
import { computed } from 'vue';
import { ElMessage } from 'element-plus';

const props = defineProps<{
  modelValue: boolean;
  title: string;
  /** 操作对象名(展示为红色加粗,不再要求输入比对) */
  confirmKeyword: string;
  impactList?: string[];
  confirmLabel?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  confirm: [];
}>();

const visible = computed({
  get: () => props.modelValue,
  set: (v: boolean) => emit('update:modelValue', v),
});

/** 删除类操作显示「将删除 X」;其他操作(禁用/重置/注销等)显示执行对象 */
const isDeleteLike = computed(
  () => props.confirmLabel === undefined || props.confirmLabel.includes('删除'),
);

function onConfirm(): void {
  emit('confirm');
  visible.value = false;
  ElMessage.success(`已${props.confirmLabel ?? '删除'} ${props.confirmKeyword}`);
}
</script>

<template>
  <el-dialog v-model="visible" :title="props.title" width="480px" append-to-body>
    <div class="danger-body">
      <div class="confirm-row">
        <template v-if="isDeleteLike">
          <span>将删除</span>
          <code class="mono keyword">{{ props.confirmKeyword }}</code>
        </template>
        <template v-else>
          <span>将对</span>
          <code class="mono keyword">{{ props.confirmKeyword }}</code>
          <span>执行「{{ props.confirmLabel }}」</span>
        </template>
      </div>
      <div v-if="props.impactList && props.impactList.length > 0" class="impact">
        <div class="impact-title">关联影响:</div>
        <div v-for="line in props.impactList" :key="line" class="impact-line">· {{ line }}</div>
      </div>
    </div>
    <template #footer>
      <el-button @click="visible = false">取消</el-button>
      <el-button type="danger" @click="onConfirm">
        {{ props.confirmLabel ?? '确认删除' }}
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.danger-body {
  display: flex;
  flex-direction: column;
  gap: 12px;
}
.impact {
  background: var(--danger-bg);
  border-radius: 4px;
  padding: 8px 12px;
  font-size: 13px;
}
.impact-title {
  color: var(--danger);
  font-weight: 600;
}
.impact-line {
  color: var(--text-2);
}
.confirm-row {
  display: flex;
  gap: 6px;
  align-items: center;
  font-size: 14px;
}
.keyword {
  color: var(--danger);
  font-weight: 700;
  background: var(--bg-inset);
  padding: 0 6px;
  border-radius: 4px;
  word-break: break-all;
}
</style>
