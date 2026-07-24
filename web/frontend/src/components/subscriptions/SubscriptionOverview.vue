<script setup lang="ts">
import { computed } from 'vue'
import { Filter, Search } from '@lucide/vue'

type SubscriptionFilter = 'all' | 'active' | 'paused' | 'issues'

interface SubscriptionTrend {
  checked_count: number
  success_count: number
  warning_count: number
  error_count: number
  active_issue_count: number
}

const props = defineProps<{
  trend?: SubscriptionTrend
  search: string
  filter: SubscriptionFilter
}>()

const emit = defineEmits<{
  'update:search': [value: string]
  'update:filter': [value: SubscriptionFilter]
}>()

const stats = computed(() => props.trend ? [
  { label: '最近检查', value: props.trend.checked_count },
  { label: '成功', value: props.trend.success_count },
  { label: '关注', value: props.trend.warning_count },
  { label: '失败', value: props.trend.error_count },
  { label: '待处理', value: props.trend.active_issue_count },
] : [])

const filters: Array<{ id: SubscriptionFilter; label: string }> = [
  { id: 'all', label: '全部' },
  { id: 'active', label: '运行中' },
  { id: 'paused', label: '已暂停' },
  { id: 'issues', label: '有异常' },
]

function updateSearch(event: Event) {
  emit('update:search', (event.target as HTMLInputElement).value)
}
</script>

<template>
  <section v-if="stats.length" class="grid gap-3 sm:grid-cols-2 xl:grid-cols-5">
    <article v-for="item in stats" :key="item.label" class="panel p-4">
      <p class="muted text-xs font-bold">{{ item.label }}</p>
      <strong class="mt-1 block text-2xl font-black">{{ item.value }}</strong>
    </article>
  </section>

  <section class="panel p-4">
    <div class="grid gap-3 md:grid-cols-[1fr_auto]">
      <label class="search-field">
        <Search :size="18" aria-hidden="true" />
        <input
          :value="search"
          class="field field-leading-icon"
          placeholder="搜索番剧或字幕组"
          aria-label="搜索订阅"
          @input="updateSearch"
        />
      </label>
      <div class="flex gap-2 overflow-x-auto" aria-label="筛选订阅">
        <button
          v-for="item in filters"
          :key="item.id"
          class="btn whitespace-nowrap"
          :class="filter === item.id ? 'btn-primary' : 'btn-secondary'"
          @click="emit('update:filter', item.id)"
        >
          <Filter v-if="item.id === 'all'" :size="15" />
          {{ item.label }}
        </button>
      </div>
    </div>
  </section>
</template>
