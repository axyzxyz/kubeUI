<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import { getPodLogs } from '@/api/workload';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  cluster: string;
  pod: string;
  namespace: string;
  containers: string[];
}>();

type LogLine = { text: string };

const MAX_LINES = 10_000;

const container = ref(props.containers[0] ?? '');
const previous = ref(false);
const keyword = ref('');
const lines = ref<LogLine[]>([]);
const connected = ref(false);
const errorMsg = ref('');
let ws: WebSocket | null = null;
let timer: number | null = null;

const filtered = computed(() => {
  const kw = keyword.value.trim().toLowerCase();
  if (kw === '') return lines.value;
  return lines.value.filter((l) => l.text.toLowerCase().includes(kw));
});

const url = computed(() => {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  const auth = JSON.parse(localStorage.getItem('kubeui.auth') ?? 'null') as {
    accessToken: string;
  } | null;
  const token = auth?.accessToken ?? '';
  const q = new URLSearchParams({
    namespace: props.namespace,
    container: container.value,
    token,
  });
  return (
    `${proto}://${window.location.host}` +
    `/api/v1/clusters/${props.cluster}/pods/${props.pod}/logs/stream?${q.toString()}`
  );
});

async function loadHistory(): Promise<void> {
  errorMsg.value = '';
  try {
    const data = await getPodLogs(props.cluster, props.pod, {
      namespace: props.namespace,
      container: container.value,
      tailLines: 1000,
      previous: previous.value,
    });
    lines.value = data.logs.split('\n').map((text) => ({ text }));
  } catch (e) {
    errorMsg.value = humanizeError(e);
  }
}

function pushLines(text: string): void {
  for (const line of text.split('\n')) {
    if (line !== '') {
      lines.value.push({ text: line });
    }
  }
  if (lines.value.length > MAX_LINES) {
    lines.value = lines.value.slice(-MAX_LINES);
  }
}

function connectStream(): void {
  ws?.close();
  const sock = new WebSocket(url.value);
  sock.binaryType = 'arraybuffer';
  sock.onopen = () => {
    connected.value = true;
    errorMsg.value = '';
  };
  sock.onmessage = (ev) => {
    // 二进制帧 = 日志字节;文本帧 = 控制帧(JSON)
    if (ev.data instanceof ArrayBuffer) {
      pushLines(new TextDecoder().decode(ev.data));
      return;
    }
    const raw = String(ev.data);
    try {
      const frame = JSON.parse(raw) as { type?: string; payload?: { message?: string } };
      if (frame.type === 'logstream:close') {
        connected.value = false;
        sock.close();
      } else if (frame.type === 'error') {
        errorMsg.value = frame.payload?.message ?? '日志流错误';
      } else if (frame.type === 'pong') {
        // 心跳回执,忽略
      }
    } catch {
      pushLines(raw);
    }
  };
  sock.onclose = () => {
    connected.value = false;
  };
  sock.onerror = () => {
    sock.close();
  };
  ws = sock;
}

function download(): void {
  const blob = new Blob([lines.value.map((l) => l.text).join('\n')], { type: 'text/plain' });
  const a = document.createElement('a');
  a.href = URL.createObjectURL(blob);
  a.download = `${props.pod}-${container.value}.log`;
  a.click();
  URL.revokeObjectURL(a.href);
}

function restart(): void {
  lines.value = [];
  void loadHistory();
  connectStream();
}

watch([container, previous], restart);

watch(
  () => props.pod,
  () => {
    container.value = props.containers[0] ?? '';
    restart();
  },
);

onBeforeUnmount(() => {
  ws?.close();
  if (timer !== null) window.clearInterval(timer);
  if (lines.value.length > 0) ElMessage.closeAll();
});

// 初始加载
restart();
</script>

<template>
  <div class="log-viewer">
    <div class="toolbar">
      <el-select v-model="container" size="small" style="width: 180px">
        <el-option v-for="c in props.containers" :key="c" :label="c" :value="c" />
      </el-select>
      <el-checkbox v-model="previous" size="small">previous(上次崩溃容器)</el-checkbox>
      <el-input
        v-model="keyword"
        size="small"
        placeholder="过滤关键字"
        style="width: 200px"
        clearable
      />
      <el-tag :type="connected ? 'success' : 'danger'" size="small">
        {{ connected ? '实时流已连接' : '未连接' }}
      </el-tag>
      <el-button size="small" @click="restart">重连</el-button>
      <el-button size="small" @click="download">下载</el-button>
    </div>
    <div v-if="errorMsg" class="error-tip">
      {{ errorMsg }}<el-button size="small" @click="restart">重试</el-button>
    </div>
    <div class="log-body mono">
      <div v-for="(line, i) in filtered" :key="`${i}-${line.text.slice(0, 12)}`" class="log-line">
        {{ line.text }}
      </div>
      <div v-if="filtered.length === 0" class="log-empty">暂无日志</div>
    </div>
  </div>
</template>

<style scoped>
.log-viewer {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 8px;
}
.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
}
.error-tip {
  color: var(--danger);
  display: flex;
  gap: 8px;
  align-items: center;
}
.log-body {
  flex: 1;
  overflow: auto;
  background: var(--bg-inset);
  border: 1px solid var(--border-1);
  border-radius: 8px;
  padding: 8px;
  font-size: 12.5px;
  line-height: 20px;
}
.log-line {
  white-space: pre-wrap;
  word-break: break-all;
}
.log-empty {
  color: var(--text-3);
}
</style>
