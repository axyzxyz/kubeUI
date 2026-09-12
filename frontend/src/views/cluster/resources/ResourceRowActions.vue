<script setup lang="ts">
/**
 * 资源列表行内操作按钮:按权限置灰(disabled + tooltip)。
 * 无 resources:write 时写操作禁用;无 terminal:use 时终端禁用。
 * tooltip 挂在外层 span 上(禁用态按钮不触发鼠标事件)。
 */
import type { ResourceKind } from '@/api/workload';

defineProps<{
  kind: ResourceKind;
  canWrite: boolean;
  canTerminal: boolean;
}>();

const emit = defineEmits<{
  detail: [];
  scale: [];
  restart: [];
  terminal: [];
  yaml: [];
  remove: [];
}>();

const WRITE_TIP = '权限不足:需要 资源写入(resources:write)权限,请联系管理员授权';
const TERMINAL_TIP = '权限不足:需要 终端使用(terminal:use)权限,请联系管理员授权';
/** 可进终端/执行的工作负载类型(与后端 exec 端点一致) */
const EXEC_KINDS: readonly ResourceKind[] = ['pods', 'deployments', 'statefulsets', 'daemonsets'];
</script>

<template>
  <el-button v-if="kind === 'pods'" link type="primary" @click="emit('detail')">详情</el-button>

  <template v-if="kind === 'deployments'">
    <el-tooltip :content="WRITE_TIP" :disabled="canWrite" placement="top">
      <span class="btn-wrap">
        <el-button link type="primary" :disabled="!canWrite" @click="emit('scale')">伸缩</el-button>
      </span>
    </el-tooltip>
    <el-tooltip :content="WRITE_TIP" :disabled="canWrite" placement="top">
      <span class="btn-wrap">
        <el-button link type="warning" :disabled="!canWrite" @click="emit('restart')">重启</el-button>
      </span>
    </el-tooltip>
  </template>

  <el-tooltip v-if="EXEC_KINDS.includes(kind)" :content="TERMINAL_TIP" :disabled="canTerminal" placement="top">
    <span class="btn-wrap">
      <el-button link type="success" :disabled="!canTerminal" @click="emit('terminal')">终端</el-button>
    </span>
  </el-tooltip>

  <el-tooltip :content="WRITE_TIP" :disabled="canWrite" placement="top">
    <span class="btn-wrap">
      <el-button link type="primary" :disabled="!canWrite" @click="emit('yaml')">YAML</el-button>
    </span>
  </el-tooltip>

  <el-tooltip :content="WRITE_TIP" :disabled="canWrite" placement="top">
    <span class="btn-wrap">
      <el-button link type="danger" :disabled="!canWrite" @click="emit('remove')">删除</el-button>
    </span>
  </el-tooltip>
</template>

<style scoped>
.btn-wrap {
  display: inline-block;
}
</style>
