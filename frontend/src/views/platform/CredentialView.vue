<script setup lang="ts">
import { computed, onMounted, onUnmounted, ref } from 'vue';
import {
  issueKubeconfig,
  listMyKubeconfigs,
  revokeKubeconfig,
} from '@/api/credential';
import type { IssuedKubeconfig, IssuedKubeconfigCreated } from '@/api/types';
import DangerConfirmDialog from '@/components/DangerConfirmDialog.vue';
import RelativeTime from '@/components/RelativeTime.vue';
import { useClusterStore } from '@/stores/clusterStore';
import { humanizeError } from '@/utils/errorMessages';

const clusterStore = useClusterStore();

const items = ref<IssuedKubeconfig[]>([]);
const loading = ref(false);
const errorMsg = ref('');

// 前端分页(我的凭证接口不分页,个人凭证数量有限)
const page = ref(1);
const size = ref(10);
const pagedItems = computed(() =>
  items.value.slice((page.value - 1) * size.value, page.value * size.value),
);

// ---- 签发表单 ----
const ttl = ref('24h');
const description = ref('');
const issuing = ref(false);
// 签发结果(token/downloadUrl 只展示一次)
const issued = ref<IssuedKubeconfigCreated | null>(null);
const issuedVisible = ref(false);
const copied = ref('');

// ---- 撤销确认 ----
const revokeTarget = ref<IssuedKubeconfig | null>(null);

// ---- 下载链接倒计时(linkExpiresAt) ----
const nowSec = ref(Math.floor(Date.now() / 1000));
let timer: number | null = null;
const countdown = computed(() => {
  if (issued.value === null) return '';
  const exp = Math.floor(Date.parse(issued.value.linkExpiresAt) / 1000);
  if (Number.isNaN(exp)) return '';
  const remain = exp - nowSec.value;
  if (remain <= 0) return '已过期';
  const mm = String(Math.floor(remain / 60)).padStart(2, '0');
  const ss = String(remain % 60).padStart(2, '0');
  return `${mm}:${ss}`;
});

async function refresh(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const res = await listMyKubeconfigs();
    items.value = res.items;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

async function onIssue(): Promise<void> {
  const cluster = clusterStore.currentName;
  if (cluster === '') {
    errorMsg.value = '请先在顶部选择集群';
    return;
  }
  issuing.value = true;
  errorMsg.value = '';
  try {
    issued.value = await issueKubeconfig(cluster, {
      ttl: ttl.value === '' ? undefined : ttl.value,
      description: description.value === '' ? undefined : description.value,
    });
    issuedVisible.value = true;
    await refresh();
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    issuing.value = false;
  }
}

async function copyText(text: string, key: string): Promise<void> {
  await navigator.clipboard.writeText(text);
  copied.value = key;
  window.setTimeout(() => {
    if (copied.value === key) copied.value = '';
  }, 2000);
}

async function onRevoke(): Promise<void> {
  if (revokeTarget.value === null) return;
  await revokeKubeconfig(revokeTarget.value.id);
  revokeTarget.value = null;
  await refresh();
}

onMounted(() => {
  void refresh();
  timer = window.setInterval(() => {
    nowSec.value = Math.floor(Date.now() / 1000);
  }, 1000);
});
onUnmounted(() => {
  if (timer !== null) window.clearInterval(timer);
});
</script>

<template>
  <div class="page">
    <div class="head">
      <h1 class="page-title">个人凭证 · 客户端 kubeconfig 签发</h1>
    </div>

    <el-alert
      v-if="errorMsg"
      type="error"
      :closable="false"
      :title="errorMsg"
      show-icon
      style="margin-bottom: 12px"
    />

    <el-card shadow="never" class="issue-card">
      <div class="issue-form">
        <span class="label">目标集群</span>
        <el-tag size="small">{{ clusterStore.currentName || '—(顶部未选择集群)' }}</el-tag>
        <span class="label">有效期</span>
        <el-select v-model="ttl" style="width: 120px">
          <el-option label="1h" value="1h" />
          <el-option label="8h" value="8h" />
          <el-option label="24h" value="24h" />
          <el-option label="72h" value="72h" />
          <el-option label="7d" value="168h" />
        </el-select>
        <el-input
          v-model="description"
          placeholder="用途描述(可选)"
          style="width: 220px"
          clearable
        />
        <el-button type="primary" :loading="issuing" @click="onIssue">签发凭证</el-button>
      </div>
      <p class="hint">签发成功后 token 与下载链接只展示一次,请立即保存。</p>
    </el-card>

    <h2 class="section-title">我的凭证</h2>
    <el-table v-loading="loading" :data="pagedItems" class="table-compact" row-key="id">
      <el-table-column prop="cluster" label="集群" width="160" />
      <el-table-column prop="description" label="描述" min-width="160" />
      <el-table-column label="状态" width="110">
        <template #default="{ row }">
          <el-tag v-if="row.revoked" type="danger" size="small">已撤销</el-tag>
          <el-tag v-else-if="Date.parse(row.expiresAt) < Date.now()" type="warning" size="small">
            已过期
          </el-tag>
          <el-tag v-else type="success" size="small">有效</el-tag>
        </template>
      </el-table-column>
      <el-table-column label="过期时间" width="180">
        <template #default="{ row }">
          <RelativeTime :value="row.expiresAt" />
        </template>
      </el-table-column>
      <el-table-column label="签发时间" width="180">
        <template #default="{ row }">
          <RelativeTime :value="row.createdAt" />
        </template>
      </el-table-column>
      <el-table-column label="操作" width="110" fixed="right">
        <template #default="{ row }">
          <el-button v-if="!row.revoked" link type="danger" @click="revokeTarget = row">
            撤销
          </el-button>
        </template>
      </el-table-column>
    </el-table>
    <el-pagination
      v-model:current-page="page"
      layout="total, prev, pager, next"
      :total="items.length"
      :page-size="size"
      style="margin-top: 12px"
    />

    <el-dialog v-model="issuedVisible" title="凭证签发成功(仅展示一次)" width="560px">
      <template v-if="issued">
        <el-alert
          type="warning"
          :closable="false"
          title="token 与下载链接只显示这一次,关闭弹窗后无法再次查看"
          style="margin-bottom: 12px"
        />
        <div class="field">
          <span class="label">Bearer Token</span>
          <div class="row">
            <code class="mono ellipsis">{{ issued.token }}</code>
            <el-button size="small" @click="copyText(issued.token, 'token')">
              {{ copied === 'token' ? '已复制' : '复制' }}
            </el-button>
          </div>
        </div>
        <div class="field">
          <span class="label">
            一次性下载链接(剩余 <b class="mono">{{ countdown }}</b> 失效)
          </span>
          <div class="row">
            <code class="mono ellipsis">{{ issued.downloadUrl }}</code>
            <el-button size="small" @click="copyText(issued.downloadUrl, 'url')">
              {{ copied === 'url' ? '已复制' : '复制' }}
            </el-button>
          </div>
        </div>
      </template>
      <template #footer>
        <el-button type="primary" @click="issuedVisible = false">我已保存</el-button>
      </template>
    </el-dialog>

    <DangerConfirmDialog
      :model-value="revokeTarget !== null"
      title="撤销凭证"
      :confirm-keyword="revokeTarget?.cluster ?? ''"
      :impact-list="['使用该凭证的客户端将立即被拒绝访问']"
      confirm-label="撤销"
      @update:model-value="
        (v: boolean) => {
          if (!v) revokeTarget = null;
        }
      "
      @confirm="onRevoke"
    />
  </div>
</template>

<style scoped>
.head {
  display: flex;
  justify-content: space-between;
  align-items: center;
}
.issue-card {
  margin-bottom: 16px;
}
.issue-form {
  display: flex;
  gap: 12px;
  align-items: center;
  flex-wrap: wrap;
}
.label {
  font-size: 13px;
  color: var(--text-2);
}
.hint {
  margin: 8px 0 0;
  font-size: 12px;
  color: var(--text-3);
}
.section-title {
  font-size: 15px;
  margin: 0 0 8px;
}
.field {
  margin-bottom: 12px;
}
.row {
  display: flex;
  gap: 8px;
  align-items: center;
}
.mono {
  font-family: monospace;
  font-size: 12px;
}
.ellipsis {
  overflow: hidden;
  text-overflow: ellipsis;
  white-space: nowrap;
  flex: 1;
}
</style>
