import { defineStore } from 'pinia'

export type ThemeMode = 'system' | 'light' | 'dark'
export interface Toast { id: number; message: string; tone: 'success' | 'error' | 'info' }

export const useUIStore = defineStore('ui', {
  state: () => ({ theme: (localStorage.getItem('animate-theme') || 'system') as ThemeMode, mobileMore: false, taskOpen: false, toasts: [] as Toast[], nextToast: 1 }),
  actions: {
    applyTheme() {
      const dark = this.theme === 'dark' || (this.theme === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
      document.documentElement.classList.toggle('dark', dark)
      document.documentElement.dataset.theme = dark ? 'dark' : 'light'
    },
    setTheme(theme: ThemeMode) { this.theme = theme; localStorage.setItem('animate-theme', theme); this.applyTheme() },
    toast(message: string, tone: Toast['tone'] = 'success') {
      const id = this.nextToast++; this.toasts.push({ id, message, tone }); setTimeout(() => this.toasts = this.toasts.filter(t => t.id !== id), 4200)
    },
  },
})
