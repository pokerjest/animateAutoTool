import DOMPurify from 'dompurify'
import { marked } from 'marked'

function isSafeLink(value: string) {
  try {
    const url = new URL(value, window.location.origin)
    return url.protocol === 'http:' || url.protocol === 'https:'
  } catch {
    return false
  }
}

export function renderAssistantMarkdown(content: string) {
  const renderer = new marked.Renderer()
  renderer.html = () => ''
  renderer.image = () => ''

  const parsed = marked.parse(content, {
    async: false,
    breaks: true,
    gfm: true,
    renderer,
  }) as string

  const sanitized = DOMPurify.sanitize(parsed, {
    ALLOWED_TAGS: ['p', 'ul', 'ol', 'li', 'strong', 'em', 'blockquote', 'pre', 'code', 'a', 'br', 'hr'],
    ALLOWED_ATTR: ['href', 'title'],
    FORBID_TAGS: ['img', 'svg', 'style', 'script', 'iframe', 'object', 'embed'],
  })

  const documentFragment = document.createElement('template')
  documentFragment.innerHTML = sanitized
  for (const link of documentFragment.content.querySelectorAll<HTMLAnchorElement>('a')) {
    const href = link.getAttribute('href')?.trim() || ''
    if (!isSafeLink(href)) {
      link.removeAttribute('href')
      continue
    }
    const url = new URL(href, window.location.origin)
    if (url.origin === window.location.origin) {
      link.setAttribute('href', `${url.pathname}${url.search}${url.hash}`)
      link.dataset.internalRoute = 'true'
    } else {
      link.target = '_blank'
      link.rel = 'noopener noreferrer'
    }
  }
  return documentFragment.innerHTML
}
