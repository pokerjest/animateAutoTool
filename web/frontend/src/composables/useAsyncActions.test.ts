import { describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useAsyncActions } from './useAsyncActions'

describe('useAsyncActions', () => {
  it('blocks duplicate keys while allowing independent list actions', async () => {
    setActivePinia(createPinia())
    let release!: () => void
    const pending = new Promise<void>(resolve => { release = resolve })
    const action = vi.fn(() => pending)
    const actions = useAsyncActions()

    const first = actions.run('subscription-1', action)
    const duplicate = actions.run('subscription-1', action)
    const secondItem = actions.run('subscription-2', async () => 'done')

    await vi.waitFor(() => expect(action).toHaveBeenCalledTimes(1))
    expect(actions.isBusy('subscription-1')).toBe(true)
    expect(await secondItem).toBe('done')
    expect(actions.isBusy('subscription-2')).toBe(false)

    release()
    await Promise.all([first, duplicate])
    expect(actions.isBusy('subscription-1')).toBe(false)
  })
})
