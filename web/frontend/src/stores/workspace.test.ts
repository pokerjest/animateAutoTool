import { beforeEach, describe, expect, it } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useWorkspaceStore } from './workspace'

describe('workspace store', () => {
  beforeEach(() => {
    localStorage.clear()
    setActivePinia(createPinia())
  })

  it('persists the selected mode and remembers each workspace route independently', () => {
    const workspace = useWorkspaceStore()
    expect(workspace.mode).toBe('manage')
    workspace.rememberRoute('/subscriptions')
    workspace.setMode('media')
    workspace.rememberRoute('/media/item/jellyfin/episode-1')

    expect(workspace.routeFor('manage')).toBe('/subscriptions')
    expect(workspace.routeFor('media')).toBe('/media/item/jellyfin/episode-1')
    expect(localStorage.getItem('animate.workspace.mode')).toBe('media')
  })
})
