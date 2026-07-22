<script setup lang="ts">
import { computed } from 'vue'
import { useRoute, useRouter } from 'vue-router'
import { Activity, ArchiveRestore, Bot, CalendarDays, ChevronRight, Clapperboard, Download, HeartPulse, Home, Library, LogOut, Menu, MoonStar, Settings, Sparkles, Sun, Tv, X } from '@lucide/vue'
import { useUIStore } from '../stores/ui'
import { useTaskStore } from '../stores/tasks'
import { useSessionStore } from '../stores/session'
import TaskCenter from './TaskCenter.vue'

const route = useRoute(); const router = useRouter(); const ui = useUIStore(); const tasks = useTaskStore(); const session = useSessionStore()
const groups = [
  { label: '概览', links: [{ to: '/', label: '今日概览', icon: Home }, { to: '/calendar', label: '追番日历', icon: CalendarDays }] },
  { label: '追番', links: [{ to: '/subscriptions', label: '订阅管理', icon: Tv }, { to: '/assistant', label: 'AI 助手', icon: Bot }] },
  { label: '媒体', links: [{ to: '/library', label: '番剧图鉴', icon: Library }, { to: '/local-anime', label: '本地番剧', icon: Clapperboard }, { to: '/player', label: '播放器', icon: Download }] },
  { label: '系统', links: [{ to: '/health', label: '系统健康', icon: HeartPulse }, { to: '/backup', label: '备份恢复', icon: ArchiveRestore }, { to: '/settings', label: '系统设置', icon: Settings }] },
]
const bottom = groups.flatMap(g => g.links).filter(l => ['/', '/calendar', '/subscriptions', '/library'].includes(l.to))
const isActive = (to: string) => to === '/' ? route.path === '/' : route.path.startsWith(to)
const themeIcon = computed(() => ui.theme === 'dark' ? Sun : MoonStar)
const toggleTheme = () => ui.setTheme(document.documentElement.classList.contains('dark') ? 'light' : 'dark')
const logout = async () => { await session.logout(); router.push('/login') }
</script>

<template>
  <div class="app-backdrop min-h-screen">
    <aside class="glass fixed inset-y-4 left-4 z-40 hidden w-[248px] flex-col rounded-[1.7rem] p-4 lg:flex">
      <RouterLink to="/" class="mb-6 flex items-center gap-3 rounded-2xl p-2">
        <span class="grid h-11 w-11 place-items-center rounded-2xl bg-gradient-to-br from-pink-400 to-rose-600 text-white shadow-lg"><Sparkles :size="22" /></span>
        <span><strong class="block text-[1.05rem] tracking-tight">AnimateTool</strong><small class="muted">你的追番中枢</small></span>
      </RouterLink>
      <nav aria-label="主导航" class="min-h-0 flex-1 overflow-y-auto pr-1">
        <section v-for="group in groups" :key="group.label" class="mb-4">
          <h2 class="mb-1 px-3 text-[.66rem] font-extrabold uppercase tracking-[.14em] text-[var(--ink-muted)]">{{ group.label }}</h2>
          <RouterLink v-for="link in group.links" :key="link.to" :to="link.to" class="mb-1 flex min-h-11 items-center gap-3 rounded-xl px-3 text-sm font-bold transition" :class="isActive(link.to) ? 'bg-[var(--brand-soft)] text-[var(--brand-strong)]' : 'muted hover:bg-[var(--surface-muted)] hover:text-[var(--ink)]'">
            <component :is="link.icon" :size="18" /><span>{{ link.label }}</span><ChevronRight v-if="isActive(link.to)" :size="15" class="ml-auto" />
          </RouterLink>
        </section>
      </nav>
      <div class="panel-muted mt-2 flex items-center justify-between p-2">
        <button class="btn btn-quiet h-10 min-h-10 w-10 p-0" type="button" @click="toggleTheme" aria-label="切换明暗主题"><component :is="themeIcon" :size="18" /></button>
        <button class="min-w-0 flex-1 truncate px-2 text-left text-xs font-bold" type="button" @click="logout"><span class="block truncate">{{ session.state?.username || '管理员' }}</span><span class="muted">退出登录</span></button>
        <span class="badge">{{ session.state?.version }}</span>
      </div>
    </aside>

    <header class="glass fixed inset-x-3 top-3 z-40 flex h-16 items-center justify-between rounded-2xl px-3 lg:left-[280px] lg:right-6">
      <div class="flex min-w-0 items-center gap-3">
        <button class="btn btn-quiet h-11 min-h-11 w-11 p-0 lg:hidden" type="button" @click="ui.mobileMore = true" aria-label="打开更多导航"><Menu :size="21" /></button>
        <div class="min-w-0"><p class="eyebrow">Animate Auto Tool</p><h1 class="truncate text-lg font-extrabold">{{ route.meta.title }}</h1></div>
      </div>
      <button class="btn btn-secondary relative" type="button" @click="ui.taskOpen = true" aria-label="打开任务中心">
        <Activity :size="18" /><span class="hidden sm:inline">任务中心</span><span v-if="tasks.runningCount" class="badge badge-warning">{{ tasks.runningCount }}</span>
      </button>
    </header>

    <main id="main-content" class="min-w-0 px-3 pb-28 pt-24 lg:ml-[280px] lg:px-6 lg:pb-8"><div class="mx-auto max-w-[1440px]"><slot /></div></main>

    <nav aria-label="移动端主导航" class="glass fixed inset-x-3 bottom-3 z-40 grid grid-cols-5 rounded-2xl p-1.5 lg:hidden">
      <RouterLink v-for="link in bottom" :key="link.to" :to="link.to" class="flex min-h-14 flex-col items-center justify-center gap-1 rounded-xl text-[.64rem] font-bold" :class="isActive(link.to) ? 'bg-[var(--brand-soft)] text-[var(--brand-strong)]' : 'muted'"><component :is="link.icon" :size="19" /><span>{{ link.label.slice(0, 4) }}</span></RouterLink>
      <button type="button" class="muted flex min-h-14 flex-col items-center justify-center gap-1 rounded-xl text-[.64rem] font-bold" @click="ui.mobileMore = true"><Menu :size="19" /><span>更多</span></button>
    </nav>

    <div v-if="ui.mobileMore" class="fixed inset-0 z-50 lg:hidden" @keydown.escape="ui.mobileMore=false">
      <button class="absolute inset-0 bg-black/45" type="button" aria-label="关闭导航" @click="ui.mobileMore=false"></button>
      <aside class="glass absolute inset-y-0 right-0 w-[min(88vw,360px)] overflow-y-auto rounded-l-[2rem] p-5">
        <div class="mb-5 flex items-center justify-between"><div><p class="eyebrow">导航</p><h2 class="text-xl font-black">所有功能</h2></div><button class="btn btn-quiet h-11 w-11 p-0" @click="ui.mobileMore=false" aria-label="关闭"><X /></button></div>
        <div v-for="group in groups" :key="group.label" class="mb-5"><h3 class="mb-2 px-2 text-xs font-extrabold muted">{{ group.label }}</h3><RouterLink v-for="link in group.links" :key="link.to" :to="link.to" class="mb-1 flex min-h-12 items-center gap-3 rounded-xl px-3 font-bold" :class="isActive(link.to) ? 'bg-[var(--brand-soft)] text-[var(--brand-strong)]' : ''" @click="ui.mobileMore=false"><component :is="link.icon" :size="19" />{{ link.label }}</RouterLink></div>
        <div class="panel-muted mt-6 grid gap-2 p-3">
          <button class="btn btn-secondary w-full justify-start" type="button" @click="toggleTheme" aria-label="切换明暗主题"><component :is="themeIcon" :size="18" />切换明暗主题</button>
          <button class="btn btn-quiet w-full justify-start text-[var(--danger)]" type="button" @click="ui.mobileMore=false; logout()"><LogOut :size="18" />退出登录</button>
        </div>
      </aside>
    </div>
    <TaskCenter />
  </div>
</template>
