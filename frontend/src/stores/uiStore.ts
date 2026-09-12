import { computed, ref } from 'vue';
import { defineStore } from 'pinia';
import { loadTheme, saveTheme } from '@/utils/storage';

export const useUiStore = defineStore('ui', () => {
  const theme = ref<'light' | 'dark'>(loadTheme());
  const sidebarCollapsed = ref<boolean>(false);

  const isDark = computed(() => theme.value === 'dark');

  function setTheme(value: 'light' | 'dark'): void {
    theme.value = value;
    saveTheme(value);
    document.documentElement.classList.toggle('dark', value === 'dark');
  }

  function toggleTheme(): void {
    setTheme(theme.value === 'dark' ? 'light' : 'dark');
  }

  function toggleSidebar(): void {
    sidebarCollapsed.value = !sidebarCollapsed.value;
  }

  return { theme, sidebarCollapsed, isDark, setTheme, toggleTheme, toggleSidebar };
});
