import { ref, type Ref } from 'vue';
import { ElMessage, ElMessageBox } from 'element-plus';
import { deleteResource, restartDeployment, restartPod, type ResourceKind } from '@/api/workload';
import type { ResourceItem } from '@/api/types';

/** 表格实例需要的最小接口(便于测试与解耦) */
export interface TableLike {
  clearSelection: () => void;
}

export interface BatchActionsState {
  selected: Ref<ResourceItem[]>;
  batchRunning: Ref<boolean>;
  onSelectionChange: (rows: ResourceItem[]) => void;
  /** 批量删除:逐个 DELETE,allSettled 汇总成功/失败 */
  batchDelete: () => Promise<void>;
  /** 批量重启:仅 pods(deleted-recreate)与 deployments(rollout restart) */
  batchRestart: () => Promise<void>;
}

/**
 * 资源列表批量操作:选择、批量删除、批量重启。
 * 调用方负责权限置灰(需 resources:write)与刷新列表(onDone)。
 * onRemoved:删除类操作成功后回调(携带删除成功的行),供调用方做乐观移除,
 * 不依赖 WS deleted 事件兜底 UI。
 */
export function useBatchActions(
  cluster: () => string,
  kind: () => ResourceKind,
  table: Ref<TableLike | null>,
  onDone: () => Promise<void>,
  onRemoved?: (rows: ResourceItem[]) => void,
): BatchActionsState {
  const selected = ref<ResourceItem[]>([]);
  const batchRunning = ref(false);

  function onSelectionChange(rows: ResourceItem[]): void {
    selected.value = rows;
  }

  function rowLabel(r: ResourceItem): string {
    return r.namespace !== undefined && r.namespace !== ''
      ? `${r.namespace}/${r.name}`
      : r.name;
  }

  async function confirmOrThrow(
    title: string,
    message: string,
    confirmLabel: string,
  ): Promise<void> {
    await ElMessageBox.confirm(message, title, {
      type: 'warning',
      confirmButtonText: confirmLabel,
      confirmButtonClass: 'el-button--danger',
      cancelButtonText: '取消',
    });
  }

  async function runSettled(
    title: string,
    confirmLabel: string,
    message: string,
    action: (row: ResourceItem) => Promise<void>,
  ): Promise<void> {
    const rows = selected.value;
    if (rows.length === 0 || batchRunning.value) return;
    try {
      await confirmOrThrow(title, message, confirmLabel);
    } catch {
      return; // 用户取消
    }
    batchRunning.value = true;
    try {
      const results = await Promise.allSettled(rows.map((row) => action(row)));
      const failed = rows.filter((_, i) => results[i]?.status === 'rejected');
      if (failed.length === 0) {
        ElMessage.success(`${confirmLabel}完成:共 ${rows.length} 个`);
      } else {
        ElMessage.warning(
          `${confirmLabel}完成:成功 ${rows.length - failed.length} 个,失败 ${failed.length} 个:${failed
            .map(rowLabel)
            .join('、')}`,
        );
      }
      table.value?.clearSelection();
      // 乐观移除删除成功的行(仅删除类操作传入 onRemoved)
      if (onRemoved !== undefined && confirmLabel.includes('删除')) {
        onRemoved(rows.filter((_, i) => results[i]?.status === 'fulfilled'));
      }
      await onDone();
    } finally {
      batchRunning.value = false;
    }
  }

  async function batchDelete(): Promise<void> {
    const rows = selected.value;
    if (rows.length === 0) return;
    await runSettled(
      '批量删除',
      '确认删除',
      `将删除 ${rows.length} 个资源:${rows.map(rowLabel).join('、')},操作不可恢复`,
      (row) => deleteResource(cluster(), kind(), row.name, row.namespace),
    );
  }

  async function batchRestart(): Promise<void> {
    const rows = selected.value;
    if (rows.length === 0) return;
    const k = kind();
    await runSettled(
      '批量重启',
      '确认重启',
      `将重启 ${rows.length} 个 ${k}:${rows.map(rowLabel).join('、')}`,
      (row) =>
        k === 'pods'
          ? restartPod(cluster(), row.name, row.namespace).then(() => undefined)
          : restartDeployment(cluster(), row.name, row.namespace).then(() => undefined),
    );
  }

  return { selected, batchRunning, onSelectionChange, batchDelete, batchRestart };
}

/** 批量重启支持的资源类型(后端仅有这两个 restart 端点) */
export function supportsBatchRestart(kind: ResourceKind): boolean {
  return kind === 'pods' || kind === 'deployments';
}
