<script setup lang="ts">
import { onBeforeUnmount, onMounted, watch } from 'vue'
import { RouterView, useRoute } from 'vue-router'
import AppShell from './components/AppShell.vue'
import ToastStack from './components/ToastStack.vue'
import { useUIStore } from './stores/ui'
import { useTaskStore } from './stores/tasks'
import { useSessionStore } from './stores/session'

const route = useRoute(); const ui = useUIStore(); const tasks = useTaskStore(); const session = useSessionStore()
const media = matchMedia('(prefers-color-scheme: dark)')
const syncTheme = () => ui.applyTheme()
const stopSessionWatch = watch(() => session.authenticated && !session.setupPending, shouldConnect => shouldConnect ? tasks.connect() : tasks.disconnect(), { immediate: true })
onMounted(() => { ui.applyTheme(); media.addEventListener('change', syncTheme) })
onBeforeUnmount(() => { media.removeEventListener('change', syncTheme); stopSessionWatch(); tasks.disconnect() })
</script>

<template>
  <a href="#main-content" class="skip-link">跳到主要内容</a>
  <AppShell v-if="!route.meta.public"><RouterView /></AppShell>
  <RouterView v-else />
  <ToastStack />
</template>
