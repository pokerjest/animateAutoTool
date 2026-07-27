(() => {
  const root = document.documentElement
  const storedTheme = localStorage.getItem('animate-docs-theme')
  if (storedTheme === 'dark' || storedTheme === 'light') root.dataset.theme = storedTheme

  const themeButton = document.querySelector('[data-theme-toggle]')
  themeButton?.addEventListener('click', () => {
    const next = root.dataset.theme === 'dark' ? 'light' : 'dark'
    root.dataset.theme = next
    localStorage.setItem('animate-docs-theme', next)
    themeButton.setAttribute('aria-label', next === 'dark' ? '切换亮色模式' : '切换深色模式')
  })

  const sidebar = document.querySelector('[data-sidebar]')
  const menuButton = document.querySelector('[data-menu-toggle]')
  menuButton?.addEventListener('click', () => {
    const open = sidebar?.classList.toggle('is-open') ?? false
    menuButton.setAttribute('aria-expanded', String(open))
  })
  sidebar?.querySelectorAll('a').forEach((link) => {
    link.addEventListener('click', () => {
      sidebar.classList.remove('is-open')
      menuButton?.setAttribute('aria-expanded', 'false')
    })
  })

  const searchable = [...document.querySelectorAll('[data-searchable]')]
  const emptyState = document.querySelector('[data-search-empty]')
  const searches = [...document.querySelectorAll('[data-search]')]
  const filter = (value) => {
    const query = value.trim().toLowerCase()
    let visible = 0
    searchable.forEach((section) => {
      const haystack = `${section.textContent} ${section.dataset.keywords || ''}`.toLowerCase()
      const match = !query || haystack.includes(query)
      section.classList.toggle('is-hidden', !match)
      if (match) visible += 1
    })
    searches.forEach((input) => {
      if (input.value !== value) input.value = value
    })
    if (emptyState) emptyState.hidden = visible > 0
  }
  searches.forEach((input) => input.addEventListener('input', () => filter(input.value)))

  document.querySelectorAll('[data-copy]').forEach((button) => {
    button.addEventListener('click', async () => {
      const value = button.dataset.copy || ''
      try {
        await navigator.clipboard.writeText(value)
        const original = button.textContent
        button.textContent = '已复制'
        window.setTimeout(() => { button.textContent = original }, 1200)
      } catch {
        button.textContent = '请手动复制'
      }
    })
  })

  const navLinks = [...document.querySelectorAll('.sidebar nav a')]
  const headings = navLinks
    .map((link) => document.querySelector(link.getAttribute('href')))
    .filter(Boolean)
  const observer = new IntersectionObserver((entries) => {
    entries.forEach((entry) => {
      if (!entry.isIntersecting) return
      navLinks.forEach((link) => link.classList.toggle('active', link.getAttribute('href') === `#${entry.target.id}`))
    })
  }, { rootMargin: '-20% 0px -68% 0px' })
  headings.forEach((heading) => observer.observe(heading))
})()
