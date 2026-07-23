import { describe, expect, it } from 'vitest'
import { routeTransitionKey } from './routeTransition'

describe('routeTransitionKey', () => {
  it('changes for route navigation but ignores query-only changes', () => {
    expect(routeTransitionKey({ path: '/library' })).toBe('/library')
    expect(routeTransitionKey({ path: '/library' })).toBe(routeTransitionKey({ path: '/library' }))
    expect(routeTransitionKey({ path: '/subscriptions' })).not.toBe(routeTransitionKey({ path: '/library' }))
  })
})
