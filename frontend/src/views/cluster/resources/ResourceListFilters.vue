<script setup lang="ts">
/**
 * 资源列表筛选栏:关键字 / labelSelector / namespace + 刷新 + 新建。
 * 全部 v-model 透传,search 事件触发父级重新拉取;create 事件打开新建 Dialog。
 * 刷新按钮右上角挂「?」角标,悬停才显示 WS 增量刷新提示(el-tooltip 默认隐藏)。
 */
defineProps<{
  keyword: string;
  labelSelector: string;
  ns: string;
  namespaceScoped: boolean;
  namespaces: { name: string }[];
  /** 无 resources:write 时「新建」置灰并提示 */
  canWrite: boolean;
}>();

const emit = defineEmits<{
  'update:keyword': [value: string];
  'update:labelSelector': [value: string];
  'update:ns': [value: string];
  search: [];
  create: [];
}>();
</script>

<template>
  <div class="filters">
    <el-input
      :model-value="keyword"
      placeholder="搜索名称"
      size="small"
      style="width: 200px"
      clearable
      @update:model-value="(v: string) => emit('update:keyword', v)"
      @change="emit('search')"
    />
    <el-input
      :model-value="labelSelector"
      placeholder="labelSelector 如 env=prod"
      size="small"
      style="width: 220px"
      clearable
      @update:model-value="(v: string) => emit('update:labelSelector', v)"
      @change="emit('search')"
    />
    <el-select
      v-if="namespaceScoped"
      :model-value="ns"
      placeholder="全部命名空间"
      size="small"
      style="width: 180px"
      clearable
      filterable
      @update:model-value="(v: string) => emit('update:ns', v)"
    >
      <el-option
        v-for="o in namespaces"
        :key="o.name"
        :label="o.name"
        :value="o.name"
      />
    </el-select>
    <span class="refresh-wrap">
      <el-button size="small" type="primary" @click="emit('search')">刷新</el-button>
      <el-tooltip
        content="已订阅 WebSocket 增量刷新:资源变更自动推送到列表,通常无需手动刷新"
        placement="top"
      >
        <span class="refresh-hint">?</span>
      </el-tooltip>
    </span>
    <el-tooltip
      content="权限不足:需要 资源写入(resources:write)权限,请联系管理员授权"
      :disabled="canWrite"
      placement="top"
    >
      <span>
        <el-button size="small" type="primary" plain :disabled="!canWrite" @click="emit('create')">
          新建
        </el-button>
      </span>
    </el-tooltip>
  </div>
</template>

<style scoped>
.filters {
  display: flex;
  gap: 8px;
  align-items: center;
  margin-bottom: 12px;
}
.refresh-wrap {
  position: relative;
  display: inline-flex;
}
.refresh-hint {
  position: absolute;
  top: -7px;
  right: -7px;
  width: 14px;
  height: 14px;
  border-radius: 50%;
  background: var(--neutral);
  color: #fff;
  font-size: 10px;
  line-height: 14px;
  text-align: center;
  cursor: help;
  z-index: 1;
  user-select: none;
}
</style>
