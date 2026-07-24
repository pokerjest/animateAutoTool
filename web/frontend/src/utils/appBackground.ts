export type BackgroundViewport = 'mobile' | 'tablet' | 'desktop'

export function backgroundViewport(width: number): BackgroundViewport {
  if (width < 768) return 'mobile'
  if (width < 1200) return 'tablet'
  return 'desktop'
}

export function backgroundPosterWidth(width: number) {
  const viewport = backgroundViewport(width)
  if (viewport === 'mobile') return 640
  if (viewport === 'tablet') return 960
  return 1280
}
