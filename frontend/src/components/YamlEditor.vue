<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue';
import type * as cmState from '@codemirror/state';
import type * as cmView from '@codemirror/view';
import type * as cmCommands from '@codemirror/commands';
import type * as cmLanguage from '@codemirror/language';
import type * as cmLangYaml from '@codemirror/lang-yaml';
import type * as cmOneDark from '@codemirror/theme-one-dark';
import { useYamlLint } from '@/composables/useYamlLint';
import type { YamlLintError } from '@/composables/useYamlLint';
import { useUiStore } from '@/stores/uiStore';

/**
 * YAML 代码编辑器(CodeMirror 6,动态 import 按需分包):
 * v-model:value、readonly、height 自适应;js-yaml 实时 lint,
 * 解析错误在编辑器下方红条提示并通过 lint-error 事件通知父级(禁用提交)。
 * 暗色主题跟随 uiStore(html.dark)切换 oneDark。
 * 用法:<YamlEditor v-model:value="yaml" :readonly="!editing" height="58vh" lint @lint-error="..." />
 */
const props = withDefaults(
  defineProps<{
    value: string;
    readonly?: boolean;
    /** 编辑器高度,默认撑满父容器 */
    height?: string;
    /** 是否启用 YAML 实时解析错误提示 */
    lint?: boolean;
  }>(),
  { readonly: false, height: '100%', lint: false },
);

const emit = defineEmits<{
  'update:value': [value: string];
  'lint-error': [error: YamlLintError | null];
}>();

const uiStore = useUiStore();
const { error: lintError, lint: lintYaml } = useYamlLint();

const hostRef = ref<HTMLDivElement | null>(null);
let view: cmView.EditorView | null = null;
let cm: CmModules | null = null;
let themeCompartment: cmState.Compartment | null = null;
let readonlyCompartment: cmState.Compartment | null = null;

interface CmModules {
  state: typeof cmState;
  view: typeof cmView;
  commands: typeof cmCommands;
  language: typeof cmLanguage;
  langYaml: typeof cmLangYaml;
  oneDark: typeof cmOneDark;
}

let cmPromise: Promise<CmModules> | null = null;

function loadCm(): Promise<CmModules> {
  cmPromise ??= Promise.all([
    import('@codemirror/state'),
    import('@codemirror/view'),
    import('@codemirror/commands'),
    import('@codemirror/language'),
    import('@codemirror/lang-yaml'),
    import('@codemirror/theme-one-dark'),
  ]).then(([state, view, commands, language, langYaml, oneDark]) => ({
    state,
    view,
    commands,
    language,
    langYaml,
    oneDark,
  }));
  return cmPromise;
}

/** 亮色用默认高亮样式 + 项目 CSS 变量背景;暗色用 oneDark */
function themeOf(dark: boolean): cmState.Extension {
  if (cm === null || themeCompartment === null) return [];
  if (dark) return cm.oneDark.oneDark;
  return cm.language.syntaxHighlighting(cm.language.defaultHighlightStyle, { fallback: true });
}

function readonlyExtensions(ro: boolean): cmState.Extension[] {
  if (cm === null) return [];
  return [cm.state.EditorState.readOnly.of(ro), cm.view.EditorView.editable.of(!ro)];
}

let lintSeq = 0;

async function runLint(text: string): Promise<void> {
  const seq = ++lintSeq;
  const err = await lintYaml(text);
  if (seq !== lintSeq) return; // 丢弃过期解析结果
  lintError.value = err;
  emit('lint-error', err);
}

onMounted(async () => {
  if (hostRef.value === null) return;
  cm = await loadCm();
  const { EditorView, keymap, lineNumbers, drawSelection } = cm.view;
  const themeComp = new cm.state.Compartment();
  const readonlyComp = new cm.state.Compartment();
  themeCompartment = themeComp;
  readonlyCompartment = readonlyComp;
  const state = cm.state.EditorState.create({
    doc: props.value,
    extensions: [
      lineNumbers(),
      drawSelection(),
      cm.commands.history(),
      keymap.of([...cm.commands.defaultKeymap, ...cm.commands.historyKeymap, cm.commands.indentWithTab]),
      cm.langYaml.yaml(),
      cm.language.indentUnit.of('  '),
      EditorView.lineWrapping,
      EditorView.updateListener.of((u: cmView.ViewUpdate) => {
        if (!u.docChanged) return;
        const text = u.state.doc.toString();
        emit('update:value', text);
        if (props.lint) void runLint(text);
      }),
      themeComp.of(themeOf(uiStore.isDark)),
      readonlyComp.of(readonlyExtensions(props.readonly)),
    ],
  });
  view = new EditorView({ state, parent: hostRef.value });
  if (props.lint) void runLint(props.value);
});

onBeforeUnmount(() => {
  view?.destroy();
  view = null;
  cm = null;
});

// 外部值变化(打开 Dialog 重新载入模板/远端 YAML)时同步进编辑器
watch(
  () => props.value,
  (v) => {
    if (view === null) return;
    const current = view.state.doc.toString();
    if (current === v) return;
    view.dispatch({ changes: { from: 0, to: current.length, insert: v } });
    if (props.lint) void runLint(v);
  },
);

watch(
  () => props.readonly,
  (ro) => {
    if (view === null || readonlyCompartment === null) return;
    view.dispatch({ effects: readonlyCompartment.reconfigure(readonlyExtensions(ro)) });
  },
);

watch(
  () => uiStore.isDark,
  (dark) => {
    if (view === null || themeCompartment === null) return;
    view.dispatch({ effects: themeCompartment.reconfigure(themeOf(dark)) });
  },
);
</script>

<template>
  <div class="yaml-editor">
    <div ref="hostRef" class="cm-host" :style="{ height: props.height }" />
    <div v-if="props.lint && lintError !== null" class="yaml-lint-error">
      第 {{ lintError.line + 1 }} 行:{{ lintError.message }}
    </div>
  </div>
</template>

<style scoped>
.yaml-editor {
  display: flex;
  flex-direction: column;
  height: 100%;
  gap: 4px;
}
.cm-host {
  border: 1px solid var(--border-1);
  border-radius: 6px;
  overflow: hidden;
  background: var(--bg-surface-2);
}
.yaml-editor :deep(.cm-editor) {
  height: 100%;
  font-size: 12.5px;
}
.yaml-editor :deep(.cm-editor.cm-focused) {
  outline: none;
}
.yaml-editor :deep(.cm-scroller) {
  font-family: var(--font-code);
  overflow: auto;
}
.yaml-editor :deep(.cm-gutters) {
  background: var(--bg-surface-2);
  border-right: 1px solid var(--border-1);
  color: var(--text-3);
}
.yaml-editor :deep(.cm-activeLineGutter),
.yaml-editor :deep(.cm-activeLine) {
  background: var(--bg-inset);
}
.yaml-lint-error {
  color: var(--danger);
  background: var(--danger-bg);
  padding: 4px 8px;
  border-radius: 4px;
  font-size: 12px;
  flex-shrink: 0;
}
</style>
