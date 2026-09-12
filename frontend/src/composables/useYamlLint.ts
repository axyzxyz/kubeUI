import { ref } from 'vue';
import type { Ref } from 'vue';

/** YAML 实时解析结果:line 为 0 基行号,null 表示解析通过 */
export interface YamlLintError {
  line: number;
  message: string;
}

interface YamlMark {
  line: number;
}

function isYamlMark(v: unknown): v is YamlMark {
  return typeof v === 'object' && v !== null && typeof (v as YamlMark).line === 'number';
}

export interface YamlLintResult {
  error: Ref<YamlLintError | null>;
  lint: (text: string) => Promise<YamlLintError | null>;
}

/**
 * YAML 实时 lint:用 js-yaml load 捕获解析异常,提取 e.mark.line。
 * 返回 { error, lint }:lint(text) 同步解析并更新 error。
 */
export function useYamlLint(): YamlLintResult {
  const error = ref<YamlLintError | null>(null);

  async function lint(text: string): Promise<YamlLintError | null> {
    if (text.trim() === '') {
      error.value = null;
      return null;
    }
    try {
      const { load } = await import('js-yaml');
      load(text);
      error.value = null;
      return null;
    } catch (e) {
      const mark = (e as { mark?: unknown }).mark;
      const line = isYamlMark(mark) ? mark.line : 0;
      const message = e instanceof Error ? e.message : String(e);
      // js-yaml message 自带 "3:4" 行列前缀与原文摘录,截掉首行列信息只留原因
      const clean = message.split('\n')[0]?.replace(/^\d+:\d+\s*/, '') ?? message;
      error.value = { line, message: clean };
      return error.value;
    }
  }

  return { error, lint };
}
