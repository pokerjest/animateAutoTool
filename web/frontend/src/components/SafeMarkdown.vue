<script setup lang="ts">
import { computed } from 'vue'
import { useRouter } from 'vue-router'
import { renderAssistantMarkdown } from '../utils/assistantMarkdown'

const props = defineProps<{ content: string }>()
const router = useRouter()
const html = computed(() => renderAssistantMarkdown(props.content))

function handleClick(event: MouseEvent) {
  const target = event.target
  if (!(target instanceof Element)) return
  const link = target.closest<HTMLAnchorElement>('a[data-internal-route="true"]')
  if (!link) return
  event.preventDefault()
  const href = link.getAttribute('href')
  if (href) void router.push(href)
}
</script>

<template>
  <div class="assistant-markdown" @click="handleClick" v-html="html"></div>
</template>

<style scoped>
.assistant-markdown :deep(p + p),
.assistant-markdown :deep(p + ul),
.assistant-markdown :deep(p + ol),
.assistant-markdown :deep(ul + p),
.assistant-markdown :deep(ol + p),
.assistant-markdown :deep(blockquote + p),
.assistant-markdown :deep(pre + p) {
  margin-top: .7rem;
}
.assistant-markdown :deep(ul),
.assistant-markdown :deep(ol) {
  margin: .5rem 0;
  padding-left: 1.25rem;
}
.assistant-markdown :deep(ul) { list-style: disc; }
.assistant-markdown :deep(ol) { list-style: decimal; }
.assistant-markdown :deep(li + li) { margin-top: .25rem; }
.assistant-markdown :deep(blockquote) {
  margin: .65rem 0;
  border-left: 3px solid var(--brand);
  padding-left: .8rem;
  color: var(--ink-muted);
}
.assistant-markdown :deep(pre) {
  max-width: 100%;
  overflow-x: auto;
  border-radius: .75rem;
  background: color-mix(in srgb, var(--ink) 92%, transparent);
  padding: .75rem;
  color: var(--canvas);
}
.assistant-markdown :deep(code) {
  border-radius: .35rem;
  background: color-mix(in srgb, var(--ink) 8%, transparent);
  padding: .08rem .3rem;
  font-size: .9em;
}
.assistant-markdown :deep(pre code) {
  background: transparent;
  padding: 0;
}
.assistant-markdown :deep(a) {
  color: var(--brand-strong);
  font-weight: 750;
  text-decoration: underline;
  text-underline-offset: 2px;
}
</style>
