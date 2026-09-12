<script setup lang="ts">
/**
 * ResourceList 的全部弹窗/抽屉聚合容器:详情抽屉、YAML、新建、终端、伸缩/重启、删除确认。
 * 仅做 props 透传与 update 事件回传,不持有业务状态。
 */
import type { ResourceItem } from '@/api/types';
import type { ResourceKind } from '@/api/workload';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import YamlDialog from '@/components/YamlDialog.vue';
import CreateResourceDialog from './CreateResourceDialog.vue';
import PodDetailDrawer from './PodDetailDrawer.vue';
import ScaleRestartDialogs from './ScaleRestartDialogs.vue';
import TerminalDialog from './TerminalDialog.vue';

const props = defineProps<{
  cluster: string;
  kind: ResourceKind;
  ns: string;
  detailName: string;
  detailNs: string;
  yamlName: string;
  yamlNs: string;
  deleteTarget: ResourceItem | null;
  createOpen: boolean;
  terminalTarget: ResourceItem | null;
  scaleTarget: ResourceItem | null;
  restartTarget: ResourceItem | null;
}>();

const emit = defineEmits<{
  'update:detailName': [value: string];
  'update:yamlName': [value: string];
  'update:deleteTarget': [value: ResourceItem | null];
  'update:createOpen': [value: boolean];
  'update:terminalTarget': [value: ResourceItem | null];
  'update:scaleTarget': [value: ResourceItem | null];
  'update:restartTarget': [value: ResourceItem | null];
  confirmDelete: [];
  refresh: [];
}>();
</script>

<template>
  <PodDetailDrawer
    :model-value="props.detailName !== ''"
    :cluster="props.cluster"
    :kind="props.kind"
    :name="props.detailName"
    :namespace="props.detailNs"
    @update:model-value="(v: boolean) => !v && emit('update:detailName', '')"
  />

  <YamlDialog
    :model-value="props.yamlName !== ''"
    :cluster="props.cluster"
    :kind="props.kind"
    :name="props.yamlName"
    :namespace="props.yamlNs === '' ? undefined : props.yamlNs"
    @update:model-value="(v: boolean) => !v && emit('update:yamlName', '')"
    @saved="emit('refresh')"
  />

  <CreateResourceDialog
    :model-value="props.createOpen"
    :cluster="props.cluster"
    :kind="props.kind"
    :namespace="props.ns === '' ? undefined : props.ns"
    @update:model-value="(v: boolean) => emit('update:createOpen', v)"
    @created="emit('refresh')"
  />

  <TerminalDialog
    :model-value="props.terminalTarget !== null"
    :cluster="props.cluster"
    :kind="props.kind"
    :name="props.terminalTarget?.name ?? ''"
    :namespace="props.terminalTarget?.namespace ?? undefined"
    @update:model-value="(v: boolean) => !v && emit('update:terminalTarget', null)"
  />

  <ScaleRestartDialogs
    :cluster="props.cluster"
    :kind="props.kind"
    :scale-target="props.scaleTarget"
    :restart-target="props.restartTarget"
    @update:scale-target="emit('update:scaleTarget', $event)"
    @update:restart-target="emit('update:restartTarget', $event)"
    @done="emit('refresh')"
  />

  <DangerConfirmDialog
    :model-value="props.deleteTarget !== null"
    :title="`删除 ${props.kind}`"
    :confirm-keyword="props.deleteTarget?.name ?? ''"
    :impact-list="['该资源及其关联对象将被永久删除', '操作将写入审计日志']"
    @update:model-value="(v: boolean) => !v && emit('update:deleteTarget', null)"
    @confirm="emit('confirmDelete')"
  />
</template>
