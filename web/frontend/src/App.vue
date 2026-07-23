<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { RouterView } from 'vue-router'
import { useQueryClient } from '@tanstack/vue-query'
import AppShell from './components/AppShell.vue'
import ToastStack from './components/ToastStack.vue'
import { useUIStore } from './stores/ui'
import { useTaskStore } from './stores/tasks'
import { useSessionStore } from './stores/session'
import { routeTransitionKey } from './utils/routeTransition'

const ui = useUIStore(); const tasks = useTaskStore(); const session = useSessionStore(); const queryClient = useQueryClient()
const media = matchMedia('(prefers-color-scheme: dark)')
const syncTheme = () => ui.applyTheme()
const stopSessionWatch = watch(() => session.authenticated && !session.setupPending, shouldConnect => shouldConnect ? tasks.connect() : tasks.disconnect(), { immediate: true })
const stopTaskWatch = watch(() => tasks.revision, () => {
  for (const transition of tasks.consumeTransitions()) {
    const task = transition.task
    if (task.tone === 'running') continue
    if (transition.previousTone === 'running' && task.kind !== 'backup') {
      ui.toast(task.tone === 'error' ? `${task.title}失败：${task.detail}` : `${task.title}已完成`, task.tone === 'error' ? 'error' : 'success')
    }
    if (task.kind === 'sync') void queryClient.invalidateQueries()
    if (task.kind === 'scan') { void queryClient.invalidateQueries({ queryKey: ['local-anime'] }); void queryClient.invalidateQueries({ queryKey: ['dashboard'] }) }
    if (task.kind === 'metadata') { void queryClient.invalidateQueries({ queryKey: ['library'] }); void queryClient.invalidateQueries({ queryKey: ['local-anime'] }); void queryClient.invalidateQueries({ queryKey: ['subscriptions'] }) }
    if (task.kind?.startsWith('subscription')) { void queryClient.invalidateQueries({ queryKey: ['subscriptions'] }); void queryClient.invalidateQueries({ queryKey: ['dashboard'] }) }
    if (task.kind === 'backup') void queryClient.invalidateQueries({ queryKey: ['backup'] })
    if (task.kind === 'updater') void queryClient.invalidateQueries({ queryKey: ['maintenance'] })
  }
})
onMounted(() => { ui.applyTheme(); media.addEventListener('change', syncTheme) })
onBeforeUnmount(() => { media.removeEventListener('change', syncTheme); stopSessionWatch(); stopTaskWatch(); tasks.disconnect() })
</script>

<template>
  <a href="#main-content" class="skip-link">跳到主要内容</a>
  <RouterView v-slot="{ Component, route: activeRoute }">
    <AppShell v-if="!activeRoute.meta.public">
      <Transition name="page" mode="out-in"><component :is="Component" :key="routeTransitionKey(activeRoute)" /></Transition>
    </AppShell>
    <Transition v-else name="page" mode="out-in"><component :is="Component" :key="routeTransitionKey(activeRoute)" /></Transition>
  </RouterView>
  <ToastStack />
</template>
