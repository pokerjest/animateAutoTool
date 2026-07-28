<script setup lang="ts">
import { onMounted } from 'vue'
import { useRouter } from 'vue-router'
import { useAssistantStore } from '../stores/assistant'
import { useWorkspaceStore } from '../stores/workspace'

const router = useRouter()
const assistant = useAssistantStore()
const workspace = useWorkspaceStore()

function safeReturnPath() {
  const fallback = workspace.isMedia ? '/media' : '/'
  const target = workspace.routeFor(workspace.mode)
  if (!target || target === '/assistant' || target.startsWith('/login') || target.startsWith('/recover') || target.startsWith('/setup')) return fallback
  return target
}

onMounted(async () => {
  assistant.expand()
  await router.replace(safeReturnPath())
})
</script>

<template>
  <div class="sr-only" role="status">正在打开 AI 助手…</div>
</template>
