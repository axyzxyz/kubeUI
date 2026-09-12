<script setup lang="ts">
import { computed, ref } from 'vue';
import { useRouter } from 'vue-router';
import { registerCluster } from '@/api/cluster';
import { createEnrollToken, getAgentManifest } from '@/api/agent';
import { humanizeError } from '@/utils/errorMessages';

const router = useRouter();

type AccessMode = 'direct' | 'agent';

const step = ref(0);
const accessMode = ref<AccessMode>('direct');
type TtlUnit = 'h' | 'd' | 'mo' | 'y' | 'permanent';
const ttlValue = ref(24);
const ttlUnit = ref<TtlUnit>('h');
/** 组装后端 TTL 语义:"24h"/"30d"/"6mo"/"1y" 或 "permanent" */
const ttl = computed(() =>
  ttlUnit.value === 'permanent' ? 'permanent' : `${ttlValue.value}${ttlUnit.value}`,
);
const agentYaml = ref('');
const agentToken = ref('');
const agentLoading = ref(false);
const agentCopied = ref(false);
const kubeconfigText = ref('');
const name = ref('');
const description = ref('');
const context = ref('');
const submitting = ref(false);
const errorMsg = ref('');

const isAgent = computed(() => accessMode.value === 'agent');
const canNext = computed(() => kubeconfigText.value.trim() !== '');
/** 最后一步标题随接入方式变化 */
const lastStepTitle = computed(() => (isAgent.value ? 'Agent 接入' : '完成'));

/** el-upload change 回调参数为 UploadFile(内容在 raw),且不真正上传 */
function onUpload(file: { raw?: File }): boolean {
  const raw = file.raw;
  if (raw !== undefined) {
    void raw.text().then((text) => {
      kubeconfigText.value = text;
    });
  }
  return false;
}

async function submit(): Promise<void> {
  submitting.value = true;
  errorMsg.value = '';
  try {
    await registerCluster({
      name: name.value,
      kubeconfig: kubeconfigText.value,
      contextName: context.value === '' ? undefined : context.value,
      description: description.value,
      accessMode: accessMode.value,
    });
    if (isAgent.value) {
      step.value = 3;
      // 注册成功后自动生成 Agent 部署配置,用户只需复制到目标集群执行
      void genAgentManifest();
    } else {
      step.value = 3;
    }
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    submitting.value = false;
  }
}

/** 生成一次性 Enrollment Token 并渲染 Agent 部署 YAML(token 只显示一次) */
async function genAgentManifest(): Promise<void> {
  agentLoading.value = true;
  errorMsg.value = '';
  try {
    const created = await createEnrollToken({ cluster: name.value, ttl: ttl.value });
    agentToken.value = created.token;
    const manifest = await getAgentManifest(created.token);
    agentYaml.value = manifest.yaml;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    agentLoading.value = false;
  }
}

async function copyAgentYaml(): Promise<void> {
  await navigator.clipboard.writeText(agentYaml.value);
  agentCopied.value = true;
  window.setTimeout(() => {
    agentCopied.value = false;
  }, 2000);
}
</script>

<template>
  <div class="page">
    <h1 class="page-title">注册集群</h1>
    <el-steps :active="step" align-center style="max-width: 720px">
      <el-step title="粘贴凭据" />
      <el-step title="连接校验" />
      <el-step title="基本信息" />
      <el-step :title="lastStepTitle" />
    </el-steps>

    <div class="step-body">
      <template v-if="step === 0">
        <el-form label-width="100px" style="max-width: 520px; margin-bottom: 12px">
          <el-form-item label="接入方式" required>
            <el-radio-group v-model="accessMode">
              <el-radio value="direct">直连</el-radio>
              <el-radio value="agent">Agent 反连</el-radio>
            </el-radio-group>
          </el-form-item>
        </el-form>
        <el-alert
          v-if="isAgent"
          type="info"
          :closable="false"
          title="目标集群在平台网络不可达时选择 Agent 反连:注册完成后将生成 Agent 部署 YAML,需在目标集群执行"
          style="margin-bottom: 12px"
        />
        <el-input
          v-model="kubeconfigText"
          type="textarea"
          :rows="14"
          placeholder="apiVersion: v1&#10;kind: Config&#10;..."
          class="mono"
        />
        <div class="row">
          <el-upload
            :auto-upload="false"
            :show-file-list="false"
            accept=".yaml,.yml"
            @change="onUpload"
          >
            <el-button size="small">上传 kubeconfig 文件</el-button>
          </el-upload>
          <span class="hint">凭据仅存储于服务端加密存储,不会明文展示</span>
        </div>
        <div class="actions">
          <el-button @click="router.back()">取消</el-button>
          <el-button type="primary" :disabled="!canNext" @click="step = 1"
            >下一步: 校验连接</el-button
          >
        </div>
      </template>

      <template v-else-if="step === 1">
        <el-alert
          v-if="isAgent"
          type="info"
          :closable="false"
          title="服务端将校验 kubeconfig 格式;平台不直连目标集群,连通性由部署 Agent 后的反连隧道建立"
        />
        <el-alert
          v-else
          type="info"
          :closable="false"
          title="点击完成后,服务端将同步校验 kubeconfig 并拨测目标集群 /version(10s 超时)"
        />
        <div class="check-list">
          <div>✅ 凭据格式合法(服务端校验)</div>
          <div v-if="isAgent">⏳ API Server 可达(部署 Agent 后经隧道自动检测)</div>
          <div v-else>✅ API Server 可达</div>
          <div v-if="isAgent">⏳ 认证与版本检测(Agent 接入后自动完成)</div>
          <div v-else>✅ 认证与权限检查</div>
          <div v-if="!isAgent">✅ 版本检测</div>
        </div>
        <div class="actions">
          <el-button @click="step = 0">上一步</el-button>
          <el-button type="primary" @click="step = 2">下一步</el-button>
        </div>
      </template>

      <template v-else-if="step === 2">
        <el-form label-width="100px" style="max-width: 520px">
          <el-form-item label="集群名称" required>
            <el-input v-model="name" placeholder="小写字母/数字/-,注册后不可改" />
          </el-form-item>
          <el-form-item label="显示名称">
            <el-input v-model="description" placeholder="如: 生产集群 · 华东1" />
          </el-form-item>
          <el-form-item label="context">
            <el-input v-model="context" placeholder="多 context kubeconfig 时显式指定" />
          </el-form-item>
        </el-form>
        <div v-if="errorMsg" class="error">{{ errorMsg }}</div>
        <div class="actions">
          <el-button @click="step = 1">上一步</el-button>
          <el-button type="primary" :loading="submitting" :disabled="name === ''" @click="submit">
            完成注册
          </el-button>
        </div>
      </template>

      <template v-else-if="step === 3 && isAgent">
        <el-alert
          type="warning"
          :closable="false"
          title="集群已注册(offline),请在目标集群部署下方 Agent;Agent 连上后状态将自动变为 ready"
          style="margin-bottom: 12px"
        />
        <el-alert
          type="error"
          :closable="false"
          title="Enrollment Token 只显示一次:离开本页后无法再次获取,请立即复制 YAML"
          style="margin-bottom: 12px"
        />
        <div class="row" style="margin-bottom: 12px">
          <span class="hint">目标集群:</span>
          <el-tag size="small">{{ name }}</el-tag>
          <span class="hint">Token 有效期:</span>
          <el-input-number
            v-if="ttlUnit !== 'permanent'"
            v-model="ttlValue"
            :min="1"
            size="small"
          />
          <el-select v-model="ttlUnit" size="small" style="width: 110px">
            <el-option label="小时" value="h" />
            <el-option label="天" value="d" />
            <el-option label="月(30天)" value="mo" />
            <el-option label="年(365天)" value="y" />
            <el-option label="长期(不过期)" value="permanent" />
          </el-select>
          <el-button type="primary" size="small" :loading="agentLoading" @click="genAgentManifest">
            重新生成
          </el-button>
        </div>
        <el-input
          v-if="agentYaml !== ''"
          v-model="agentYaml"
          type="textarea"
          :rows="16"
          readonly
          class="mono"
        />
        <div v-if="errorMsg" class="error">{{ errorMsg }}</div>
        <div v-if="agentYaml !== ''" class="actions" style="justify-content: flex-start">
          <el-button type="primary" size="small" @click="copyAgentYaml">
            {{ agentCopied ? '已复制' : '一键复制 YAML' }}
          </el-button>
          <span class="hint">在目标集群执行: kubectl apply -f agent.yaml</span>
        </div>
        <p v-if="agentYaml !== ''" class="hint" style="margin-top: 8px">
          有效期内 Agent 断线/重启可自动重连;过期或吊销后重连将被拒绝,需重新签发并更新 Agent。
        </p>
        <div class="actions">
          <el-button @click="router.push('/clusters')">完成,返回集群列表</el-button>
        </div>
      </template>

      <template v-else-if="step === 3">
        <el-result icon="success" title="集群注册成功" :sub-title="`集群 ${name} 已就绪`">
          <template #extra>
            <el-button type="primary" @click="router.push('/clusters')">返回集群列表</el-button>
          </template>
        </el-result>
      </template>
    </div>
  </div>
</template>

<style scoped>
.step-body {
  margin-top: 24px;
  max-width: 720px;
}
.row {
  display: flex;
  align-items: center;
  gap: 12px;
  margin-top: 8px;
}
.hint {
  font-size: 12px;
  color: var(--text-3);
}
.actions {
  margin-top: 20px;
  display: flex;
  gap: 8px;
  justify-content: flex-end;
}
.check-list {
  margin-top: 16px;
  display: flex;
  flex-direction: column;
  gap: 8px;
}
.error {
  color: var(--danger);
  margin-top: 8px;
}
</style>
