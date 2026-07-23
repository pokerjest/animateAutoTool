<script setup lang="ts">
import { computed, ref } from 'vue'
import { RouterLink } from 'vue-router'
import { ShieldAlert } from '@lucide/vue'
import AppDialog from './AppDialog.vue'
import { useSessionStore } from '../stores/session'

withDefaults(defineProps<{ label?: string; linkClass?: string }>(), {
  label: '本机恢复',
  linkClass: '',
})

const session = useSessionStore()
const dialogOpen = ref(false)
const localPort = computed(() => window.location.port ? `:${window.location.port}` : '')
</script>

<template>
  <span class="contents">
    <RouterLink v-if="session.localRecoveryAvailable" to="/recover" :class="linkClass">{{ label }}</RouterLink>
    <button v-else type="button" :class="linkClass" @click="dialogOpen = true">{{ label }}</button>
    <AppDialog
      v-model:open="dialogOpen"
      title="本机恢复仅限 localhost"
      description="为保护管理员账户，局域网和公网访问不能打开恢复流程。"
    >
      <div class="panel-muted flex items-start gap-3 p-4">
        <ShieldAlert class="mt-0.5 shrink-0 text-[var(--warning)]" :size="22" />
        <div class="min-w-0 text-sm leading-6">
          <strong class="block">请在运行 AnimateTool 的电脑上操作</strong>
          <p class="muted mt-1">在该电脑的浏览器中打开以下任一地址，然后再次点击“本机恢复”：</p>
          <div class="mt-3 grid gap-2 font-mono text-xs">
            <code class="break-all rounded-lg bg-[var(--surface-solid)] px-3 py-2">http://localhost{{ localPort }}</code>
            <code class="break-all rounded-lg bg-[var(--surface-solid)] px-3 py-2">http://127.0.0.1{{ localPort }}</code>
          </div>
        </div>
      </div>
      <button type="button" class="btn btn-primary mt-5 w-full" @click="dialogOpen = false">我知道了</button>
    </AppDialog>
  </span>
</template>
