import { afterEach, describe, expect, it } from 'vitest'
import { mount } from '@vue/test-utils'
import { createPinia, setActivePinia } from 'pinia'
import LocalRecoveryLink from './LocalRecoveryLink.vue'
import { useSessionStore } from '../stores/session'
import type { SessionState } from '../api/types'

const baseState: SessionState = {
  authenticated: false,
  setup_pending: false,
  local_setup_available: false,
  local_recovery_available: true,
  version: 'test',
  recovery_local_only: true,
}

const AppDialogStub = {
  props: ['open', 'title', 'description'],
  emits: ['update:open'],
  template: '<div v-if="open" role="dialog"><h2>{{ title }}</h2><p>{{ description }}</p><slot /></div>',
}

function render(available: boolean) {
  const pinia = createPinia()
  setActivePinia(pinia)
  useSessionStore().state = { ...baseState, local_recovery_available: available }
  return mount(LocalRecoveryLink, {
    props: { linkClass: 'recovery-entry' },
    global: {
      plugins: [pinia],
      stubs: {
        AppDialog: AppDialogStub,
        RouterLink: { props: ['to'], template: '<a :href="to"><slot /></a>' },
      },
    },
  })
}

afterEach(() => {
  document.body.innerHTML = ''
})

describe('LocalRecoveryLink', () => {
  it('opens the recovery route immediately for a direct localhost session', () => {
    const wrapper = render(true)
    expect(wrapper.get('a').attributes('href')).toBe('/recover')
    expect(wrapper.find('[role="dialog"]').exists()).toBe(false)
  })

  it('shows the local-only message instead of navigating for a remote session', async () => {
    const wrapper = render(false)
    expect(wrapper.find('a').exists()).toBe(false)
    await wrapper.get('button.recovery-entry').trigger('click')
    expect(wrapper.get('[role="dialog"]').text()).toContain('本机恢复仅限 localhost')
    expect(wrapper.get('[role="dialog"]').text()).toContain('http://localhost')
    expect(wrapper.get('[role="dialog"]').text()).toContain('http://127.0.0.1')
  })
})
