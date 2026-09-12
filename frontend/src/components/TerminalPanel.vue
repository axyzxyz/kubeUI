<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { ElMessage } from 'element-plus';
import type { Terminal } from '@xterm/xterm';
import { useUiStore } from '@/stores/uiStore';
import { humanizeError } from '@/utils/errorMessages';

const props = defineProps<{
  cluster: string;
  pod: string;
  namespace: string;
  containers: string[];
}>();

const container = ref(props.containers[0] ?? '');
const ready = ref(false);
const errorMsg = ref('');
const boxRef = ref<HTMLDivElement | null>(null);
const uiStore = useUiStore();

let term: Terminal | null = null;
let fit: { fit: () => void } | null = null;
let ws: WebSocket | null = null;
let resizeObserver: ResizeObserver | null = null;

function wsUrl(): string {
  const proto = window.location.protocol === 'https:' ? 'wss' : 'ws';
  const auth = JSON.parse(localStorage.getItem('kubeui.auth') ?? 'null') as {
    accessToken: string;
  } | null;
  const token = auth?.accessToken ?? '';
  const params = new URLSearchParams({ namespace: props.namespace, token });
  // container 为空时省略,由后端取默认容器
  if (container.value !== '') params.set('container', container.value);
  return (
    `${proto}://${window.location.host}/api/v1/clusters/${props.cluster}/pods/${props.pod}/terminal` +
    `?${params.toString()}`
  );
}

function send(obj: Record<string, unknown>): void {
  if (ws !== null && ws.readyState === WebSocket.OPEN) {
    ws.send(JSON.stringify(obj));
  }
}

async function setupTerm(): Promise<void> {
  if (boxRef.value === null || term !== null) return;
  const xterm = await import('@xterm/xterm');
  const fitAddon = await import('@xterm/addon-fit');
  await import('@xterm/xterm/css/xterm.css');
  // 终端背景跟随主题:暗色与 --bg-inset(#12161d)一致,亮色用白色面板
  const dark = uiStore.isDark;
  const t = new xterm.Terminal({
    cursorBlink: true,
    fontSize: 13,
    convertEol: true,
    theme: dark
      ? { background: '#12161d', foreground: '#e6eaf2', cursor: '#e6eaf2' }
      : { background: '#ffffff', foreground: '#1a2230' },
  });
  const f = new fitAddon.FitAddon();
  t.loadAddon(f);
  t.open(boxRef.value);
  f.fit();
  t.onData((data) => send({ type: 'terminal:stdin', data }));
  term = t;
  fit = f;
  resizeObserver = new ResizeObserver(() => {
    fit?.fit();
    send({ type: 'terminal:resize', cols: t.cols, rows: t.rows });
  });
  resizeObserver.observe(boxRef.value);
}

function connect(): void {
  const sock = new WebSocket(wsUrl());
  sock.binaryType = 'arraybuffer';
  sock.onopen = () => {
    ready.value = true;
    errorMsg.value = '';
    send({ type: 'terminal:resize', cols: 100, rows: 30 });
  };
  sock.onmessage = (ev) => {
    if (ev.data instanceof ArrayBuffer) {
      term?.write(new TextDecoder().decode(ev.data));
      return;
    }
    const raw = String(ev.data);
    try {
      const msg = JSON.parse(raw) as {
        type: string;
        payload?: { code?: number; message?: string };
      };
      if (msg.type === 'terminal:exit') {
        // 信封结构:{"type":"terminal:exit","payload":{"code":0}}
        term?.write(`\r\n[会话结束 code=${msg.payload?.code ?? 0}]\r\n`);
        sock.close();
      } else if (msg.type === 'error') {
        errorMsg.value = msg.payload?.message ?? '终端错误';
      }
    } catch {
      term?.write(raw);
    }
  };
  sock.onclose = () => {
    ready.value = false;
  };
  ws = sock;
}

function restart(): void {
  term?.reset();
  ws?.close();
  connect();
}

watch(container, restart);

// 容器列表异步就绪时补默认选中(避免空 container 且用户未选择)
watch(
  () => props.containers,
  (list) => {
    if (container.value === '' && list.length > 0) container.value = list[0] ?? '';
  },
);

onMounted(() => {
  void setupTerm()
    .then(connect)
    .catch((e: unknown) => {
      errorMsg.value = humanizeError(e);
    });
});

onBeforeUnmount(() => {
  ws?.close();
  resizeObserver?.disconnect();
  term?.dispose();
  term = null;
  ElMessage.closeAll();
});
</script>

<template>
  <div class="terminal-panel">
    <div class="toolbar">
      <el-select v-model="container" size="small" style="width: 180px">
        <el-option v-for="c in props.containers" :key="c" :label="c" :value="c" />
      </el-select>
      <el-tag :type="ready ? 'success' : 'danger'" size="small">
        {{ ready ? '已连接' : '未连接' }}
      </el-tag>
      <el-button size="small" @click="restart">重启会话</el-button>
    </div>
    <div v-if="errorMsg" class="error-tip">{{ errorMsg }}</div>
    <div ref="boxRef" class="term-box" />
  </div>
</template>

<style scoped>
.terminal-panel {
  display: flex;
  flex-direction: column;
  height: 100%;
  /* Dialog 中撑满;在 Drawer tab 等无固定高度的宿主里保底可读高度 */
  min-height: 360px;
  gap: 8px;
}
.toolbar {
  display: flex;
  gap: 12px;
  align-items: center;
}
.error-tip {
  color: var(--danger);
}
.term-box {
  flex: 1;
  min-height: 0;
  background: var(--bg-inset);
  border-radius: 8px;
  border: 1px solid var(--border-1);
  padding: 4px;
}
.term-box :deep(.xterm) {
  height: 100%;
}
</style>
