<script setup lang="ts">
import { nextTick, onBeforeUnmount, onMounted, ref } from 'vue'

const props = defineProps<{ remaining: number }>()
const emit = defineEmits<{ load: [] }>()
const sentinel = ref<HTMLElement | null>(null)

let observer: IntersectionObserver | null = null
let fallbackHandler: (() => void) | null = null
let queued = false

function requestLoad() {
  if (queued) return
  queued = true
  emit('load')
  void nextTick(() => { queued = false })
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

onBeforeUnmount(() => {
  observer?.disconnect()
  if (fallbackHandler) window.removeEventListener('scroll', fallbackHandler)
})
</script>

<template>
  <div ref="sentinel" class="h-px w-full" data-testid="auto-load-sentinel" aria-hidden="true"></div>
  <p class="sr-only" aria-live="polite">向下滚动将自动加载其余 {{ props.remaining }} 项</p>
</template>
