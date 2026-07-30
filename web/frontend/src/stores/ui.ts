import { defineStore } from 'pinia'

export type ThemeMode = 'system' | 'light' | 'dark'
export type BackgroundMode = 'default' | 'anime'
export interface Toast { id: number; message: string; tone: 'success' | 'error' | 'info' }

function storedBackgroundMode(): BackgroundMode {
  return localStorage.getItem('animate-background-mode') === 'anime' ? 'anime' : 'default'
}

function storedDesktopSidebarCollapsed(): boolean {
  return localStorage.getItem('animate-desktop-sidebar-collapsed') === 'true'
}

export const useUIStore = defineStore('ui', {
  state: () => ({
    theme: (localStorage.getItem('animate-theme') || 'system') as ThemeMode,
    backgroundMode: storedBackgroundMode(),
    desktopSidebarCollapsed: storedDesktopSidebarCollapsed(),
    mobileMore: false,
    taskOpen: false,
    toasts: [] as Toast[],
    nextToast: 1,
  }),
  actions: {
    applyTheme() {
      const dark = this.theme === 'dark' || (this.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
      document.documentElement.classList.toggle('dark', dark)
      document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    },
    setTheme(theme: ThemeMode) { this.theme = theme; localStorage.setItem('animate-theme', theme); this.applyTheme() },
    setBackgroundMode(mode: BackgroundMode) { this.backgroundMode = mode; localStorage.setItem('animate-background-mode', mode) },
    setDesktopSidebarCollapsed(collapsed: boolean) {
      this.desktopSidebarCollapsed = collapsed
      localStorage.setItem('animate-desktop-sidebar-collapsed', String(collapsed))
    },
    toast(message: string, tone: Toast['tone'] = 'success') {
      const id = this.nextToast++; this.toasts.push({ id, message, tone }); setTimeout(() => this.toasts = this.toasts.filter(t => t.id !== id), 4200)
    },
  },
})
