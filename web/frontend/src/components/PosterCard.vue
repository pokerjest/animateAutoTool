<script setup lang="ts">
import { computed } from 'vue'
import { ImageOff } from '@lucide/vue'
import { handlePosterError, posterThumbnailURL } from '../api/client'

const props = defineProps<{
  title: string
  image?: string
  fallbackImage?: string
  meta?: string
  badges?: string[]
  openable?: boolean
}>()

const emit = defineEmits<{ open: [] }>()
const failed = defineModel<boolean>('failed', { default: false })
const src = computed(() => posterThumbnailURL(props.image, 360))

function onImageError(event: Event) {
  if (!handlePosterError(event, props.fallbackImage)) failed.value = true
}

function onCardClick(event: MouseEvent) {
  if (!props.openable) return
  const target = event.target
  const interactive = target instanceof Element
    ? target.closest('a,button,input,select,textarea,[role="button"]')
    : null
  if (interactive && interactive !== event.currentTarget) return
  emit('open')
}
</script>

<template>
  <article
    class="panel group overflow-hidden"
    :class="openable ? 'cursor-pointer focus-visible:outline-none focus-visible:ring-2 focus-visible:ring-[var(--brand)]' : ''"
    :aria-label="openable ? `打开 ${title} 详情` : undefined"
    :data-testid="openable ? 'poster-open' : undefined"
    :role="openable ? 'button' : undefined"
    :tabindex="openable ? 0 : undefined"
    @click="onCardClick"
    @keydown.enter.self="openable && emit('open')"
    @keydown.space.self.prevent="openable && emit('open')"
  >
    <div
      class="relative block aspect-[2/3] w-full overflow-hidden bg-[var(--surface-muted)] text-left"
    >
      <img
        v-if="!failed"
        :src="src"
        :alt="`${title} 海报`"
        loading="lazy"
        decoding="async"
        fetchpriority="low"
        class="h-full w-full object-cover transition duration-500 group-hover:scale-[1.035]"
        @error="onImageError"
      />
      <div v-else class="muted grid h-full place-items-center">
        <ImageOff :size="30" />
      </div>
    </div>

    <div class="p-3">
      <div v-if="badges?.length" class="mb-2 flex flex-wrap gap-1.5">
        <span v-for="badge in badges" :key="badge" class="badge">
          {{ badge }}
        </span>
      </div>
      <h3 class="line-clamp-2 min-h-10 text-sm font-extrabold leading-5">
        {{ title }}
      </h3>
      <p v-if="meta" class="muted mt-1 truncate text-xs">{{ meta }}</p>
      <slot />
    </div>
  </article>
</template>
