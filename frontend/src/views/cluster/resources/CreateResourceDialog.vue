<script setup lang="ts">
import { ref, watch } from 'vue';
import { load as yamlLoad } from 'js-yaml';
import { ElMessage } from 'element-plus';
import { ApiError } from '@/api/http';
import { createResource, isClusterScoped, type ResourceKind } from '@/api/workload';
import type { K8sObject } from '@/api/types';
import YamlEditor from '@/components/YamlEditor.vue';
import type { YamlLintError } from '@/composables/useYamlLint';
import { humanizeError } from '@/utils/errorMessages';

/**
 * 新建资源 Dialog:按 kind 预填最小 YAML 模板,提交时前端 YAML→JSON 后
 * POST /clusters/:cluster/:resource?namespace=
 * 用法:<CreateResourceDialog v-model="open" cluster="c1" kind="deployments" namespace="default" @created="refresh" />
 */
const props = defineProps<{
  modelValue: boolean;
  cluster: string;
  kind: ResourceKind;
  namespace?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  created: [name: string];
}>();

const yamlText = ref('');
const saving = ref(false);
const errorMsg = ref('');
const exists = ref(false);
const lintError = ref<YamlLintError | null>(null);

interface KindTemplate {
  apiVersion: string;
  kind: string;
  spec: string;
}

const KIND_TEMPLATES: Record<string, KindTemplate> = {
  deployments: {
    apiVersion: 'apps/v1',
    kind: 'Deployment',
    spec: '  replicas: 1\n  selector:\n    matchLabels:\n      app: example\n  template:\n    metadata:\n      labels:\n        app: example\n    spec:\n      containers:\n        - name: main\n          image: nginx:alpine',
  },
  statefulsets: {
    apiVersion: 'apps/v1',
    kind: 'StatefulSet',
    spec: '  replicas: 1\n  serviceName: example\n  selector:\n    matchLabels:\n      app: example\n  template:\n    metadata:\n      labels:\n        app: example\n    spec:\n      containers:\n        - name: main\n          image: nginx:alpine',
  },
  daemonsets: {
    apiVersion: 'apps/v1',
    kind: 'DaemonSet',
    spec: '  selector:\n    matchLabels:\n      app: example\n  template:\n    metadata:\n      labels:\n        app: example\n    spec:\n      containers:\n        - name: main\n          image: nginx:alpine',
  },
  pods: {
    apiVersion: 'v1',
    kind: 'Pod',
    spec: '  containers:\n    - name: main\n      image: nginx:alpine',
  },
  services: {
    apiVersion: 'v1',
    kind: 'Service',
    spec: '  selector:\n    app: example\n  ports:\n    - port: 80\n      targetPort: 80',
  },
  ingresses: {
    apiVersion: 'networking.k8s.io/v1',
    kind: 'Ingress',
    spec: '  rules:\n    - host: example.local\n      http:\n        paths:\n          - path: /\n            pathType: Prefix\n            backend:\n              service:\n                name: example\n                port:\n                  number: 80',
  },
  configmaps: {
    apiVersion: 'v1',
    kind: 'ConfigMap',
    spec: 'data:\n  key: value',
  },
  secrets: {
    apiVersion: 'v1',
    kind: 'Secret',
    spec: 'type: Opaque\ndata: {}\nstringData:\n  key: value',
  },
  pvcs: {
    apiVersion: 'v1',
    kind: 'PersistentVolumeClaim',
    spec: '  accessModes:\n    - ReadWriteOnce\n  resources:\n    requests:\n      storage: 1Gi',
  },
  namespaces: { apiVersion: 'v1', kind: 'Namespace', spec: '' },
  nodes: { apiVersion: 'v1', kind: 'Node', spec: '' },
};

/** 模板头部:namespaced 资源预填当前 namespace */
function templateOf(kind: string, namespace: string): string {
  const t = KIND_TEMPLATES[kind];
  const kindName = t?.kind ?? kind;
  const apiVersion = t?.apiVersion ?? 'v1';
  const meta =
    namespace === '' || kind === 'namespaces' || kind === 'nodes' || isClusterScoped(kind as ResourceKind)
      ? 'metadata:\n  name: example\n'
      : `metadata:\n  name: example\n  namespace: ${namespace}\n`;
  const spec = t?.spec ?? '';
  return `apiVersion: ${apiVersion}\nkind: ${kindName}\n${meta}${spec}`;
}

watch(
  () => props.modelValue,
  (v) => {
    if (v) {
      errorMsg.value = '';
      exists.value = false;
      lintError.value = null;
      yamlText.value = templateOf(props.kind, props.namespace ?? '');
    }
  },
);

async function submit(): Promise<void> {
  errorMsg.value = '';
  exists.value = false;
  if (lintError.value !== null) {
    errorMsg.value = `YAML 解析失败(第 ${lintError.value.line + 1} 行):${lintError.value.message}`;
    return;
  }
  let obj: unknown;
  try {
    obj = yamlLoad(yamlText.value);
  } catch (e) {
    errorMsg.value = `YAML 解析失败:${humanizeError(e)}`;
    return;
  }
  if (obj === null || typeof obj !== 'object') {
    errorMsg.value = 'YAML 内容必须是一个对象';
    return;
  }
  const meta = ((obj as K8sObject)['metadata'] ?? {}) as { name?: string; namespace?: string };
  if (!meta.name) {
    errorMsg.value = 'metadata.name 不能为空';
    return;
  }
  saving.value = true;
  try {
    await createResource(
      props.cluster,
      props.kind,
      obj as K8sObject,
      meta.namespace ?? props.namespace,
    );
    ElMessage.success(`已创建 ${meta.name}`);
    emit('update:modelValue', false);
    emit('created', meta.name);
  } catch (e) {
    if (e instanceof ApiError && e.code === 40406) exists.value = true;
    errorMsg.value = humanizeError(e);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <el-dialog
    :model-value="props.modelValue"
    :title="`新建 ${props.kind}`"
    width="640px"
    append-to-body
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <el-alert
      type="info"
      :closable="false"
      title="修改 metadata.name 后即可创建"
      style="margin-bottom: 8px"
    />
    <el-alert
      v-if="exists"
      type="warning"
      :closable="false"
      title="同名资源已存在(40406),请修改 metadata.name"
      style="margin-bottom: 8px"
    />
    <div v-if="errorMsg" class="error-tip">{{ errorMsg }}</div>
    <YamlEditor
      v-model:value="yamlText"
      height="50vh"
      lint
      @lint-error="(e: YamlLintError | null) => (lintError = e)"
    />
    <template #footer>
      <el-button @click="emit('update:modelValue', false)">取消</el-button>
      <el-button type="primary" :loading="saving" :disabled="lintError !== null" @click="submit">
        创建
      </el-button>
    </template>
  </el-dialog>
</template>

<style scoped>
.error-tip {
  color: var(--danger);
  margin-bottom: 8px;
  white-space: pre-wrap;
}
</style>
