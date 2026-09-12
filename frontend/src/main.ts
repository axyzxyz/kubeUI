import { createPinia } from 'pinia';
import ElementPlus, { ElNotification } from 'element-plus';
import 'element-plus/dist/index.css';
import { createApp } from 'vue';
import App from './App.vue';
import { router } from './router';
import './styles/index.scss';

const app = createApp(App);

// 捕获组件内未处理异常:除 console 外以通知浮层可见化,
// 供无 DevTools 的自动化环境定位"整页交互失效"类问题(BUG-03/BUG-17)。
function reportError(kind: string, err: unknown): void {
  // eslint-disable-next-line no-console
  console.error(`[v911] ${kind}:`, err);
  ElNotification({
    title: `前端运行时错误(${kind})`,
    message: err instanceof Error ? `${err.message}\n${err.stack ?? ''}`.slice(0, 600) : String(err),
    type: 'error',
    duration: 0,
  });
}

app.config.errorHandler = (err, _instance, info) => {
  reportError(`unhandled@${info}`, err);
};
window.addEventListener('error', (e) => {
  reportError('window.error', e.error ?? e.message);
});
window.addEventListener('unhandledrejection', (e) => {
  reportError('unhandledrejection', e.reason);
});
app.use(createPinia());
app.use(router);
app.use(ElementPlus);
app.mount('#app');
