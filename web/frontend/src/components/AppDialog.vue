<script setup lang="ts">
import { X } from '@lucide/vue'
import { DialogClose, DialogContent, DialogDescription, DialogOverlay, DialogPortal, DialogRoot, DialogTitle } from 'reka-ui'

defineProps<{ open: boolean; title: string; description?: string; wide?: boolean }>()
defineEmits<{ 'update:open': [value: boolean] }>()
</script>

<template>
  <DialogRoot :open="open" @update:open="$emit('update:open', $event)">
    <DialogPortal>
      <DialogOverlay class="fixed inset-0 z-[80] bg-black/45 backdrop-blur-sm" />
      <DialogContent class="panel fixed left-1/2 top-1/2 z-[81] max-h-[90vh] w-[94vw] -translate-x-1/2 -translate-y-1/2 overflow-y-auto p-5 sm:p-7" :class="wide ? 'max-w-[920px]' : 'max-w-[620px]'">
        <div class="mb-6 flex items-start justify-between gap-4">
          <div>
            <DialogTitle class="text-2xl font-black">{{ title }}</DialogTitle>
            <DialogDescription v-if="description" class="muted mt-2 text-sm leading-6">{{ description }}</DialogDescription>
          </div>
          <DialogClose class="btn btn-quiet h-11 min-h-11 w-11 shrink-0 p-0" aria-label="关闭"><X :size="20" /></DialogClose>
        </div>
        <slot />
      </DialogContent>
    </DialogPortal>
  </DialogRoot>
</template>
