import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { SessionState } from '../api/types'

export const useSessionStore = defineStore('session', {
  state: () => ({ state: null as SessionState | null, loading: false }),
  getters: { authenticated: s => Boolean(s.state?.authenticated), setupPending: s => Boolean(s.state?.setup_pending) },
  actions: {
    async load(force = false) {
      if (this.state && !force) return this.state
      this.loading = true
      try { this.state = await api<SessionState>('/session'); return this.state } finally { this.loading = false }
    },
    async login(username: string, password: string, remember_me: boolean) {
      await api('/session/login', { method: 'POST', body: JSON.stringify({ username, password, remember_me }), headers: { 'Content-Type': 'application/json' } })
      return this.load(true)
    },
    async logout() { await api('/session/logout', { method: 'POST' }); this.state = null },
  },
})
