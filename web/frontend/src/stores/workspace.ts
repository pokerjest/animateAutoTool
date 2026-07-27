import { defineStore } from 'pinia'

export type WorkspaceMode = 'manage' | 'media'

const modeKey = 'animate.workspace.mode'
const manageRouteKey = 'animate.workspace.last.manage'
const mediaRouteKey = 'animate.workspace.last.media'

function readMode(): WorkspaceMode {
  return localStorage.getItem(modeKey) === 'media' ? 'media' : 'manage'
}

function readRoute(key: string, fallback: string) {
  const value = localStorage.getItem(key)?.trim()
  return value || fallback
}

export const useWorkspaceStore = defineStore('workspace', {
  state: () => ({
    mode: readMode() as WorkspaceMode,
    lastManageRoute: readRoute(manageRouteKey, '/'),
    lastMediaRoute: readRoute(mediaRouteKey, '/media'),
  }),
  getters: {
    isManage: state => state.mode === 'manage',
    isMedia: state => state.mode === 'media',
    lastRoute: state => state.mode === 'media' ? state.lastMediaRoute : state.lastManageRoute,
  },
  actions: {
    setMode(mode: WorkspaceMode) {
      this.mode = mode
      localStorage.setItem(modeKey, mode)
    },
    rememberRoute(path: string, mode?: WorkspaceMode) {
      if (!path || path === '/login' || path === '/recover' || path === '/setup') return
      const targetMode = mode || this.mode
      if (targetMode === 'media') {
        this.lastMediaRoute = path
        localStorage.setItem(mediaRouteKey, path)
      } else {
        this.lastManageRoute = path
        localStorage.setItem(manageRouteKey, path)
      }
    },
    routeFor(mode: WorkspaceMode) {
      return mode === 'media' ? this.lastMediaRoute || '/media' : this.lastManageRoute || '/'
    },
    syncRouteWorkspace(workspace: unknown) {
      if (workspace === 'media' || workspace === 'manage') this.setMode(workspace)
    },
  },
})
