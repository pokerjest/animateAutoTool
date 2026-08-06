import { defineStore } from 'pinia'

export type ThemeMode = 'system' | 'light' | 'dark'
export type BackgroundMode = 'default' | 'anime'
export type SkinMode = 'classic' | 'mascot'
export interface Toast { id: number; message: string; tone: 'success' | 'error' | 'info' }

function storedBackgroundMode(): BackgroundMode {
  return localStorage.getItem('animate-background-mode') === 'anime' ? 'anime' : 'default'
}

function storedDesktopSidebarCollapsed(): boolean {
  return localStorage.getItem('animate-desktop-sidebar-collapsed') === 'true'
}

function storedSkin(): SkinMode {
  return localStorage.getItem('animate-ui-skin') === 'mascot' ? 'mascot' : 'classic'
}

export const useUIStore = defineStore('ui', {
  state: () => ({
    theme: (localStorage.getItem('animate-theme') || 'system') as ThemeMode,
    backgroundMode: storedBackgroundMode(),
    skin: storedSkin(),
    desktopSidebarCollapsed: storedDesktopSidebarCollapsed(),
    mobileMore: false,
    taskOpen: false,
    toasts: [] as Toast[],
    nextToast: 1,
  }),
  actions: {
    applyAppearance() {
      const dark = this.theme === 'dark' || (this.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
      document.documentElement.classList.toggle('dark', dark)
      document.documentElement.dataset.theme = dark ? 'dark' : 'light'
      document.documentElement.dataset.skin = this.skin
      const themeColor = this.skin === 'mascot'
        ? (dark ? '#111419' : '#f5f7fa')
        : (dark ? '#171519' : '#fff9f8')
      document.querySelector('meta[name="theme-color"]')?.setAttribute('content', themeColor)
    },
    applyTheme() { this.applyAppearance() },
    setTheme(theme: ThemeMode) { this.theme = theme; localStorage.setItem('animate-theme', theme); this.applyAppearance() },
    setSkin(skin: SkinMode) { this.skin = skin; localStorage.setItem('animate-ui-skin', skin); this.applyAppearance() },
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
