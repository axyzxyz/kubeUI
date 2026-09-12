<script setup lang="ts">
/**
 * 资源列表批量操作工具栏:选中计数 + 批量删除/批量重启。
 * 无 resources:write 时按钮置灰并提示(tooltip 挂外层 span,禁用态按钮不触发鼠标事件)。
 */
defineProps<{
  selectedCount: number;
  canWrite: boolean;
  /** 当前资源类型是否支持批量重启(仅 pods/deployments) */
  canRestart: boolean;
  running: boolean;
}>();

const emit = defineEmits<{
  remove: [];
  restart: [];
}>();

const WRITE_TIP = '权限不足:需要 资源写入(resources:write)权限,请联系管理员授权';
</script>

<template>
  <div class="batch-bar">
    <span class="batch-count">已选 {{ selectedCount }} 项</span>
    <el-tooltip :content="WRITE_TIP" :disabled="canWrite" placement="top">
      <span>
        <el-button
          size="small"
          type="danger"
          :disabled="!canWrite"
          :loading="running"
          @click="emit('remove')"
        >
          批量删除
        </el-button>
      </span>
    </el-tooltip>
    <el-tooltip v-if="canRestart" :content="WRITE_TIP" :disabled="canWrite" placement="top">
      <span>
        <el-button
          size="small"
          type="warning"
          :disabled="!canWrite"
          :loading="running"
          @click="emit('restart')"
        >
          批量重启
        </el-button>
      </span>
    </el-tooltip>
  </div>
</template>

<style scoped>
.batch-bar {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}
.batch-count {
  font-size: 13px;
  color: var(--text-2);
}
</style>
