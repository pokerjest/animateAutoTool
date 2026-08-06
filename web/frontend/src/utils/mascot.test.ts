import { describe, expect, it } from 'vitest'
import { mascotFor, mascotScenes } from './mascot'

describe('mascot scenes', () => {
  it('keeps every runtime asset on the stable current path', () => {
    for (const scene of Object.values(mascotScenes)) {
      expect(scene.src).toMatch(/^\/mascot\/current\//)
      expect(scene.src).not.toMatch(/\/v[12]\//)
    }
  })

  it('uses different v2-derived poses for product contexts', () => {
    expect(mascotFor('brand').src).toContain('/expressions/wink.png')
    expect(mascotFor('login').src).toContain('/actions/present.png')
    expect(mascotFor('organizing').src).toContain('/actions/organizing.png')
    expect(mascotFor('diagnosing').src).toContain('/actions/troubleshooting.png')
    expect(mascotFor('empty-search').src).toContain('/expressions/confused.png')
  })
})
