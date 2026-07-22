import { createRouter, createWebHistory } from 'vue-router'
import { useSessionStore } from './stores/session'

const routes = [
  { path: '/login', component: () => import('./views/LoginView.vue'), meta: { public: true, title: '登录' } },
  { path: '/recover', component: () => import('./views/RecoverView.vue'), meta: { public: true, title: '本机恢复' } },
  { path: '/setup', component: () => import('./views/SetupView.vue'), meta: { title: '完成初始化' } },
  { path: '/', component: () => import('./views/DashboardView.vue'), meta: { title: '概览' } },
  { path: '/subscriptions', component: () => import('./views/SubscriptionsView.vue'), meta: { title: '订阅管理' } },
  { path: '/calendar', component: () => import('./views/CalendarView.vue'), meta: { title: '追番日历' } },
  { path: '/library', component: () => import('./views/LibraryView.vue'), meta: { title: '番剧图鉴' } },
  { path: '/local-anime', component: () => import('./views/LocalAnimeView.vue'), meta: { title: '本地番剧' } },
  { path: '/player', component: () => import('./views/PlayerView.vue'), meta: { title: '播放器' } },
  { path: '/backup', component: () => import('./views/BackupView.vue'), meta: { title: '备份与恢复' } },
  { path: '/health', component: () => import('./views/HealthView.vue'), meta: { title: '系统健康' } },
  { path: '/settings', component: () => import('./views/SettingsView.vue'), meta: { title: '系统设置' } },
  { path: '/assistant', component: () => import('./views/AssistantView.vue'), meta: { title: 'AI 助手' } },
  { path: '/:pathMatch(.*)*', component: () => import('./views/NotFoundView.vue'), meta: { title: '页面不存在' } },
]

const router = createRouter({ history: createWebHistory(), routes, scrollBehavior: () => ({ top: 0 }) })
router.beforeEach(async to => {
  const session = useSessionStore()
  try { await session.load() } catch { if (to.path !== '/login' && to.path !== '/recover') return '/login' }
  document.title = `${String(to.meta.title || 'AnimateTool')} · AnimateTool`
  if (to.meta.public) return session.authenticated && to.path === '/login' ? (session.setupPending ? '/setup' : '/') : true
  if (!session.authenticated) return `/login?redirect=${encodeURIComponent(to.fullPath)}`
  if (session.setupPending && to.path !== '/setup') return '/setup'
  if (!session.setupPending && to.path === '/setup') return '/'
  return true
})
export default router
