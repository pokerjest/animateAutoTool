import { afterEach, describe, expect, it } from 'vitest'
import { renderAssistantMarkdown } from './assistantMarkdown'

afterEach(() => {
  document.body.innerHTML = ''
})

describe('assistant markdown sanitization', () => {
  it('renders supported markdown and strips raw HTML, images and executable URLs', () => {
    const html = renderAssistantMarkdown([
      '**加粗**',
      '',
      '- 第一项',
      '- 第二项',
      '',
      '<img src=x onerror="alert(1)">',
      '<script>alert(1)</script>',
      '![远程图片](https://example.test/cover.jpg)',
      '[危险链接](javascript:alert(1))',
    ].join('\n'))

    expect(html).toContain('<strong>加粗</strong>')
    expect(html).toContain('<li>第一项</li>')
    expect(html).not.toContain('<img')
    expect(html).not.toContain('<script')
    expect(html).not.toContain('onerror')
    expect(html).not.toContain('javascript:')
  })

  it('marks same-origin routes for router navigation and hardens external links', () => {
    const html = renderAssistantMarkdown('[健康页](/health?from=assistant) [官网](https://example.com/docs)')
    const template = document.createElement('template')
    template.innerHTML = html
    const links = [...template.content.querySelectorAll('a')]

    expect(links[0].getAttribute('href')).toBe('/health?from=assistant')
    expect(links[0].dataset.internalRoute).toBe('true')
    expect(links[0].getAttribute('target')).toBeNull()
    expect(links[1].getAttribute('target')).toBe('_blank')
    expect(links[1].getAttribute('rel')).toBe('noopener noreferrer')
  })
})
