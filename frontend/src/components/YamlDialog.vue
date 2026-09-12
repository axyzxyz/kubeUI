<script setup lang="ts">
import { ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { ApiError } from '@/api/http';
import { getResourceYaml, updateResourceYaml } from '@/api/workload';
import type { ResourceKind } from '@/api/workload';
import YamlEditor from '@/components/YamlEditor.vue';
import type { YamlLintError } from '@/composables/useYamlLint';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  modelValue: boolean;
  cluster: string;
  kind: ResourceKind;
  name: string;
  namespace?: string;
}>();

const emit = defineEmits<{
  'update:modelValue': [value: boolean];
  saved: [];
}>();

const yaml = ref('');
const loading = ref(false);
const editing = ref(false);
const saving = ref(false);
const conflict = ref(false);
const errorMsg = ref('');
const lintError = ref<YamlLintError | null>(null);

watch(
  () => props.modelValue,
  (v) => {
    if (v) {
      editing.value = false;
      conflict.value = false;
      errorMsg.value = '';
      lintError.value = null;
      void load();
    }
  },
);

async function load(): Promise<void> {
  loading.value = true;
  errorMsg.value = '';
  try {
    const data = await getResourceYaml(props.cluster, props.kind, props.name, props.namespace);
    yaml.value = data.yaml;
  } catch (e) {
    errorMsg.value = humanizeError(e);
  } finally {
    loading.value = false;
  }
}

function startEdit(): void {
  editing.value = true;
  conflict.value = false;
}

function onLintError(e: YamlLintError | null): void {
  lintError.value = e;
}

async function save(): Promise<void> {
  saving.value = true;
  errorMsg.value = '';
  conflict.value = false;
  try {
    await updateResourceYaml(props.cluster, props.kind, props.name, yaml.value, props.namespace);
    ElMessage.success(`已保存 ${props.name}`);
    editing.value = false;
    emit('saved');
  } catch (e) {
    // YAML 更新冲突:后端统一 code 40406(HTTP 409)
    if (e instanceof ApiError && e.code === 40406) {
      conflict.value = true;
    }
    errorMsg.value = humanizeError(e);
  } finally {
    saving.value = false;
  }
}
</script>

<template>
  <el-drawer
    :model-value="props.modelValue"
    :title="`YAML · ${props.name}`"
    size="60%"
    append-to-body
    @update:model-value="(v: boolean) => emit('update:modelValue', v)"
  >
    <div v-if="errorMsg" class="error-tip">
      {{ errorMsg }}
      <el-button v-if="conflict" size="small" @click="load">查看最新版本</el-button>
    </div>
    <el-alert
      v-if="conflict"
      type="warning"
      :closable="false"
      title="资源已被修改(resourceVersion 冲突),请基于最新版本重新编辑"
      style="margin-bottom: 8px"
    />
    <YamlEditor
      v-model:value="yaml"
      :readonly="!editing"
      height="calc(100vh - 300px)"
      :lint="editing"
      @lint-error="onLintError"
    />
    <div class="toolbar">
      <template v-if="editing">
        <el-button @click="editing = false">取消</el-button>
        <el-button type="primary" :loading="saving" :disabled="lintError !== null" @click="save">
          保存
        </el-button>
      </template>
      <template v-else>
        <el-button @click="load">刷新</el-button>
        <el-button type="primary" :disabled="loading" @click="startEdit">编辑</el-button>
      </template>
    </div>
  </el-drawer>
</template>

<style scoped>
.toolbar {
  margin-top: 12px;
  display: flex;
  justify-content: flex-end;
  gap: 8px;
}
.error-tip {
  color: var(--danger);
  margin-bottom: 8px;
  display: flex;
  gap: 8px;
  align-items: center;
}
</style>
