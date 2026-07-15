<script setup>
import { onBeforeUnmount, onMounted, ref } from 'vue';
import AppRoot from './components/AppRoot.vue';
import CheckinWorkbench from './components/CheckinWorkbench.vue';
import ContentViewer from './components/ContentViewer.vue';
import Dashboard from './components/Dashboard.vue';
import { useAppShellStore } from './stores/appShell';
import { disposeApp, initializeApp } from './legacy-app';
const shell = useAppShellStore();
const retrying = ref(false);

async function boot() {
  if (retrying.value) return;
  retrying.value = true;
  shell.setMounting();
  try {
    await initializeApp();
    shell.setReady();
  } catch (error) {
    shell.setError(error);
  } finally {
    retrying.value = false;
  }
}

async function retryBoot() {
  disposeApp();
  await boot();
}

onMounted(boot);

onBeforeUnmount(() => {
  disposeApp();
});
</script>

<template>
  <main class="vue-app-shell" :data-status="shell.status">
    <section v-if="shell.error" class="vue-shell-state vue-shell-failure" role="alert">
      <strong>前端加载失败</strong>
      <span>{{ shell.error }}</span>
      <button type="button" :disabled="retrying" @click="retryBoot">
        {{ retrying ? '正在重试…' : '重新加载' }}
      </button>
    </section>
    <section v-else-if="shell.status !== 'ready'" class="vue-shell-state vue-shell-loading" role="status" aria-live="polite">
      <span class="vue-shell-spinner" aria-hidden="true"></span>
      <strong>正在载入门训打卡</strong>
      <span>正在同步账号和小组数据…</span>
    </section>
    <template v-else>
      <AppRoot />
      <CheckinWorkbench />
      <Dashboard />
      <ContentViewer />
    </template>
  </main>
</template>

<style scoped>
.vue-shell-state {
  min-height: 100dvh;
  display: grid;
  place-content: center;
  justify-items: center;
  gap: 10px;
  padding: 24px;
  text-align: center;
}

.vue-shell-state > span {
  color: var(--muted, #5b6475);
}

.vue-shell-failure {
  background:
    radial-gradient(circle at 50% 30%, rgba(255, 69, 58, 0.09), transparent 30%),
    var(--canvas, #f7f5f0);
  color: #b42318;
}

.vue-shell-failure button {
  min-width: 132px;
  margin-top: 8px;
}

.vue-shell-spinner {
  width: 34px;
  height: 34px;
  border: 3px solid rgba(10, 132, 255, 0.15);
  border-top-color: #0a84ff;
  border-radius: 50%;
  animation: shell-spin 0.8s linear infinite;
}

@keyframes shell-spin {
  to { transform: rotate(360deg); }
}

@media (prefers-reduced-motion: reduce) {
  .vue-shell-spinner { animation: none; }
}
</style>
