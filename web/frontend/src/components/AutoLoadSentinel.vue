<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue'

const props = withDefaults(defineProps<{ remaining: number; loading?: boolean; paused?: boolean }>(), {
  loading: false,
  paused: false,
})
const emit = defineEmits<{ load: [] }>()
const sentinel = ref<HTMLElement | null>(null)

let observer: IntersectionObserver | null = null
let fallbackHandler: (() => void) | null = null
let queued = false

function isNearViewport() {
  if (!sentinel.value) return false
  const bounds = sentinel.value.getBoundingClientRect()
  return bounds.top <= window.innerHeight + 320 && bounds.bottom >= -320
}

function requestLoad() {
  if (queued || props.loading || props.paused) return
  queued = true
  emit('load')
}

function requestLoadIfVisible() {
  if (!props.loading && !props.paused && isNearViewport()) requestLoad()
}

onMounted(() => {
  if (typeof IntersectionObserver !== 'undefined') {
    observer = new IntersectionObserver(entries => {
      if (entries.some(entry => entry.isIntersecting)) requestLoad()
    }, { rootMargin: '320px 0px' })
    if (sentinel.value) observer.observe(sentinel.value)
    return
  }

  // Fallback for older embedded browsers without IntersectionObserver.
  fallbackHandler = () => {
    if (!sentinel.value) return
    const bounds = sentinel.value.getBoundingClientRect()
    if (bounds.top <= window.innerHeight + 320 && bounds.bottom >= -320) requestLoad()
  }
  window.addEventListener('scroll', fallbackHandler, { passive: true })
})

// Keep the sentinel armed while a slow page request is in flight. Intersection
// Observer does not necessarily emit a second event when the sentinel remains
// visible after the response arrives, so re-check visibility when loading ends.
watch(() => props.loading, (loading, wasLoading) => {
  if (wasLoading && !loading) {
    queued = false
    void nextTick(requestLoadIfVisible)
  }
})

watch(() => props.paused, (paused, wasPaused) => {
  if (wasPaused && !paused) {
    queued = false
    void nextTick(requestLoadIfVisible)
  }
})

onBeforeUnmount(() => {
  observer?.disconnect()
  if (fallbackHandler) window.removeEventListener('scroll', fallbackHandler)
})
</script>

<template>
  <div ref="sentinel" class="h-px w-full" data-testid="auto-load-sentinel" aria-hidden="true"></div>
  <p class="sr-only" aria-live="polite">向下滚动将自动加载其余 {{ props.remaining }} 项</p>
</template>
