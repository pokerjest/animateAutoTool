import { describe, expect, it } from 'vitest'
import { backgroundPosterWidth, backgroundViewport } from './appBackground'

describe('responsive anime background sizing', () => {
  it.each([
    [390, 'mobile', 640],
    [767, 'mobile', 640],
    [768, 'tablet', 960],
    [1024, 'tablet', 960],
    [1199, 'tablet', 960],
    [1200, 'desktop', 1280],
    [1440, 'desktop', 1280],
  ] as const)('maps %ipx to %s and a %ipx poster', (viewportWidth, viewport, posterWidth) => {
    expect(backgroundViewport(viewportWidth)).toBe(viewport)
    expect(backgroundPosterWidth(viewportWidth)).toBe(posterWidth)
  })
})
