import { onMounted } from 'vue';
import { useUiStore } from '@/stores/uiStore';

/** 挂载时同步 <html> 的 dark class 与持久化主题 */
export function useTheme(): void {
  const ui = useUiStore();
  onMounted(() => {
    document.documentElement.classList.toggle('dark', ui.isDark);
  });
}
