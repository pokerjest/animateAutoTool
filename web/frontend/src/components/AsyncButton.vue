<script setup lang="ts">
import { LoaderCircle } from '@lucide/vue'

withDefaults(defineProps<{ loading?: boolean; loadingLabel?: string; disabled?: boolean; type?: 'button' | 'submit' | 'reset' }>(), {
  loading: false,
  loadingLabel: '处理中…',
  disabled: false,
  type: 'button',
})
</script>

<template>
  <button :type="type" :disabled="disabled || loading" :aria-busy="loading || undefined">
    <template v-if="loading"><LoaderCircle class="animate-spin" :size="17" aria-hidden="true" />{{ loadingLabel }}</template>
    <slot v-else />
  </button>
</template>
