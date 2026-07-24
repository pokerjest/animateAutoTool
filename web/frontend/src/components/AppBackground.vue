<script setup lang="ts">
import { onBeforeUnmount, onMounted, ref, watch } from 'vue'
import { api, posterThumbnailURL } from '../api/client'
import type { RandomBackground } from '../api/types'
import { useUIStore } from '../stores/ui'
import { backgroundPosterWidth } from '../utils/appBackground'

const ui = useUIStore()
const backgroundURL = ref('')
let baseURL = ''
let currentWidth = backgroundPosterWidth(window.innerWidth)
let loadRevision = 0

function preloadBackground(url: string) {
  const revision = ++loadRevision
  const image = new Image()
  image.decoding = 'async'
  image.onload = () => {
    if (revision === loadRevision && ui.backgroundMode === 'anime') backgroundURL.value = url
  }
  image.onerror = () => {
    if (revision === loadRevision) backgroundURL.value = ''
  }
  image.src = url
}

function renderForViewport() {
  if (!baseURL || ui.backgroundMode !== 'anime') return
  preloadBackground(posterThumbnailURL(baseURL, currentWidth))
}

async function loadBackground() {
  if (baseURL) {
    renderForViewport()
    return
  }

  const revision = ++loadRevision
  try {
    const result = await api<RandomBackground>('/ui/background/random')
    if (revision !== loadRevision || ui.backgroundMode !== 'anime') return
    baseURL = result.url
    renderForViewport()
  } catch {
    if (revision === loadRevision) backgroundURL.value = ''
  }
}

function handleResize() {
  const nextWidth = backgroundPosterWidth(window.innerWidth)
  if (nextWidth === currentWidth) return
  currentWidth = nextWidth
  renderForViewport()
}

const stopModeWatch = watch(() => ui.backgroundMode, mode => {
  if (mode === 'anime') void loadBackground()
  else {
    loadRevision += 1
    backgroundURL.value = ''
  }
}, { immediate: true })

onMounted(() => window.addEventListener('resize', handleResize, { passive: true }))
onBeforeUnmount(() => {
  loadRevision += 1
  stopModeWatch()
  window.removeEventListener('resize', handleResize)
})
</script>

<template>
  <div
    v-if="backgroundURL"
    class="anime-background"
    :style="{ backgroundImage: `url(${JSON.stringify(backgroundURL)})` }"
    data-testid="anime-background"
    aria-hidden="true"
  ></div>
</template>
