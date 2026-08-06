<script setup lang="ts">
import { computed, ref, watch } from 'vue'
import { useQuery } from '@tanstack/vue-query'
import { ArrowLeft, CalendarDays, Check, Film, Search, Users } from '@lucide/vue'
import { api, handlePosterError, mikanDiscoveryPosterURL, normalizePosterURL } from '../api/client'
import type {
  MikanDashboard,
  MikanDiscoveryItem,
  MikanEpisodePreview,
  MikanSubgroup,
  MikanSubscriptionSelection,
  ResolutionFilter,
  SubtitleLanguage,
} from '../api/types'
import AppDialog from './AppDialog.vue'
import AsyncButton from './AsyncButton.vue'
import StateBlock from './StateBlock.vue'
import { buildMikanSelection, mikanEpisodeMatchesFilters, regexRuleError } from '../utils/mikanSubscription'

const props = withDefaults(defineProps<{
  open: boolean
  initialSearch?: string
  initialSearchAliases?: string[]
  initialBangumiSubjectId?: string
  saving?: boolean
}>(), {
  initialSearch: '',
  initialSearchAliases: () => [],
  initialBangumiSubjectId: '',
  saving: false,
})
const emit = defineEmits<{
  'update:open': [value: boolean]
  select: [selection: MikanSubscriptionSelection]
}>()

type DiscoveryTab = 'season' | 'search'
type DiscoveryStep = 'browse' | 'subgroups'
type SelectedAnime = MikanDiscoveryItem & { season: string }

const now = new Date()
const years = Array.from({ length: 12 }, (_, index) => String(now.getFullYear() - index))
const seasons = ['春', '夏', '秋', '冬'] as const
const dayOrder = ['1', '2', '3', '4', '5', '6', '0', '7', '8']
const dayNames: Record<string, string> = {
  '1': '周一', '2': '周二', '3': '周三', '4': '周四', '5': '周五', '6': '周六', '0': '周日', '7': 'OVA', '8': '剧场版',
}

function seasonForMonth(month: number) {
  if (month >= 3 && month <= 5) return '春'
  if (month >= 6 && month <= 8) return '夏'
  if (month >= 9 && month <= 11) return '秋'
  return '冬'
}

const tab = ref<DiscoveryTab>('season')
const step = ref<DiscoveryStep>('browse')
const selectedYear = ref(String(now.getFullYear()))
const selectedSeason = ref(seasonForMonth(now.getMonth()))
const activeDay = ref(String(now.getDay()))
const searchText = ref('')
const submittedSearch = ref('')
const resolveInitial = ref(false)
const exactItems = ref<MikanDiscoveryItem[]>([])
const selectedAnime = ref<SelectedAnime | null>(null)
const selectedSubgroup = ref<MikanSubgroup | null>(null)
const resolutionFilter = ref<ResolutionFilter>('')
const subtitleLanguage = ref<SubtitleLanguage>('')
const includeRule = ref('')
const excludeRule = ref('')

const dashboard = useQuery({
  queryKey: computed(() => ['mikan-dashboard', selectedYear.value, selectedSeason.value]),
  queryFn: () => api<MikanDashboard>(`/subscriptions/mikan/dashboard?year=${encodeURIComponent(selectedYear.value)}&season=${encodeURIComponent(selectedSeason.value)}`),
  enabled: computed(() => props.open && step.value === 'browse' && tab.value === 'season'),
})

const initialResolveQueries = computed(() => Array.from(new Set(
  [props.initialSearch, ...props.initialSearchAliases].map(value => value.trim()).filter(Boolean),
)))

const exactMatch = useQuery({
  queryKey: computed(() => ['mikan-resolve', props.initialBangumiSubjectId, ...initialResolveQueries.value]),
  queryFn: async () => {
    let lastError: unknown
    let successfulRequest = false
    for (const query of initialResolveQueries.value) {
      const params = new URLSearchParams({ bangumi_subject_id: props.initialBangumiSubjectId, q: query })
      try {
        const result = await api<{ items: MikanDiscoveryItem[] }>(`/subscriptions/mikan/resolve?${params}`)
        successfulRequest = true
        if (result.items.length) return result
      } catch (error) {
        lastError = error
      }
    }
    if (successfulRequest) return { items: [] }
    throw lastError instanceof Error ? lastError : new Error('无法通过 Bangumi ID 关联 Mikan')
  },
  enabled: computed(() => props.open && step.value === 'browse' && tab.value === 'search' && resolveInitial.value && Boolean(props.initialBangumiSubjectId) && initialResolveQueries.value.length > 0),
  retry: false,
})

const searchResults = useQuery({
  queryKey: computed(() => ['mikan-search', submittedSearch.value]),
  queryFn: () => api<{ items: MikanDiscoveryItem[] }>(`/subscriptions/search?q=${encodeURIComponent(submittedSearch.value)}`),
  enabled: computed(() => props.open && step.value === 'browse' && tab.value === 'search' && !resolveInitial.value && exactItems.value.length === 0 && Boolean(submittedSearch.value)),
})

const subgroups = useQuery({
  queryKey: computed(() => ['mikan-subgroups', selectedAnime.value?.mikan_id]),
  queryFn: () => api<{ items: MikanSubgroup[] }>(`/subscriptions/mikan/subgroups?mikan_id=${encodeURIComponent(selectedAnime.value!.mikan_id)}`),
  enabled: computed(() => props.open && step.value === 'subgroups' && Boolean(selectedAnime.value?.mikan_id)),
})

const episodes = useQuery({
  queryKey: computed(() => ['mikan-episodes', selectedAnime.value?.mikan_id, selectedSubgroup.value?.id ?? null]),
  queryFn: () => {
    const params = new URLSearchParams({ mikan_id: selectedAnime.value!.mikan_id })
    if (selectedSubgroup.value?.id) params.set('subgroup_id', selectedSubgroup.value.id)
    return api<MikanEpisodePreview>(`/subscriptions/mikan/episodes?${params}`)
  },
  enabled: computed(() => props.open && step.value === 'subgroups' && Boolean(selectedAnime.value && selectedSubgroup.value)),
})

const dashboardItems = computed(() => dashboard.data.value?.days?.[activeDay.value] || [])
const displayedSearchItems = computed(() => exactItems.value.length ? exactItems.value : (searchResults.data.value?.items || []))
const groupItems = computed(() => subgroups.data.value?.items || [])
const filteredEpisodes = computed(() => (episodes.data.value?.items || []).filter(episode => (
  mikanEpisodeMatchesFilters(episode, resolutionFilter.value, subtitleLanguage.value, includeRule.value, excludeRule.value)
)))
const includeRuleError = computed(() => regexRuleError(includeRule.value))
const excludeRuleError = computed(() => regexRuleError(excludeRule.value))
const hasRegexError = computed(() => Boolean(includeRuleError.value || excludeRuleError.value))
const dialogTitle = computed(() => step.value === 'subgroups' ? '选择字幕组' : '从 Mikan 发现番剧')
const dialogDescription = computed(() => step.value === 'subgroups'
  ? '先检查字幕组最近发布的资源，再确认订阅策略。'
  : '按季度浏览 Mikan 番组，或直接搜索番剧名称。')

watch(() => dashboard.data.value, value => {
  if (!value) return
  const current = String(now.getDay())
  activeDay.value = value.days[current]?.length
    ? current
    : dayOrder.find(day => value.days[day]?.length) || dayOrder[0]
})

watch(() => subgroups.data.value, value => {
  selectedSubgroup.value = value?.items.find(item => item.is_all) || value?.items[0] || null
})

watch(() => exactMatch.data.value, value => {
  if (!resolveInitial.value || !value) return
  resolveInitial.value = false
  exactItems.value = value.items
  if (value.items.length === 1) chooseAnime(value.items[0])
})

watch(() => exactMatch.isError.value, failed => {
  if (!failed || !resolveInitial.value) return
  resolveInitial.value = false
  exactItems.value = []
})

watch(() => props.open, open => {
  if (!open) return
  const initialSearch = props.initialSearch.trim()
  tab.value = initialSearch ? 'search' : 'season'
  step.value = 'browse'
  selectedAnime.value = null
  selectedSubgroup.value = null
  exactItems.value = []
  resolveInitial.value = Boolean(props.initialBangumiSubjectId.trim() && initialResolveQueries.value.length)
  resolutionFilter.value = ''
  subtitleLanguage.value = ''
  includeRule.value = ''
  excludeRule.value = ''
  submittedSearch.value = initialSearch
  searchText.value = initialSearch
}, { immediate: true })

function submitSearch() {
  if (resolveInitial.value || searchResults.isFetching.value) return
  const query = searchText.value.trim()
  if (!query) return
  exactItems.value = []
  if (submittedSearch.value === query) searchResults.refetch()
  else submittedSearch.value = query
}

function chooseAnime(item: MikanDiscoveryItem, season = '') {
  selectedAnime.value = { ...item, season }
  selectedSubgroup.value = null
  resolutionFilter.value = ''
  subtitleLanguage.value = ''
  includeRule.value = ''
  excludeRule.value = ''
  step.value = 'subgroups'
}

function backToBrowse() {
  step.value = 'browse'
  selectedAnime.value = null
  selectedSubgroup.value = null
}

function confirmSelection() {
  if (!selectedAnime.value || !selectedSubgroup.value) return
  emit('select', buildMikanSelection(selectedAnime.value, selectedSubgroup.value, {
    filter_rule: includeRule.value,
    exclude_rule: excludeRule.value,
    resolution_filter: resolutionFilter.value,
    subtitle_language: subtitleLanguage.value,
  }))
}
</script>

<template>
  <AppDialog :open="open" :title="dialogTitle" :description="dialogDescription" wide @update:open="emit('update:open', $event)">
    <template v-if="step === 'browse'">
      <div class="mb-5 flex gap-2" role="tablist" aria-label="Mikan 发现方式">
        <button class="btn flex-1" :class="tab === 'season' ? 'btn-primary' : 'btn-secondary'" role="tab" :aria-selected="tab === 'season'" @click="tab = 'season'">
          <CalendarDays :size="17" />季度番组
        </button>
        <button class="btn flex-1" :class="tab === 'search' ? 'btn-primary' : 'btn-secondary'" role="tab" :aria-selected="tab === 'search'" @click="tab = 'search'">
          <Search :size="17" />搜索
        </button>
      </div>

      <section v-if="tab === 'season'" role="tabpanel">
        <div class="grid gap-3 sm:grid-cols-2">
          <label class="label">年份<select v-model="selectedYear" class="field"><option v-for="year in years" :key="year" :value="year">{{ year }}</option></select></label>
          <label class="label">季度<select v-model="selectedSeason" class="field"><option v-for="season in seasons" :key="season" :value="season">{{ season }}季</option></select></label>
        </div>
        <div class="mt-4 flex gap-2 overflow-x-auto pb-1" aria-label="播出日期">
          <button v-for="day in dayOrder" :key="day" class="btn min-w-16 whitespace-nowrap px-3" :class="activeDay === day ? 'btn-primary' : 'btn-secondary'" @click="activeDay = day">{{ dayNames[day] }}</button>
        </div>
        <StateBlock v-if="dashboard.isLoading.value" class="mt-5" state="loading" title="正在读取 Mikan 季度番组" />
        <StateBlock v-else-if="dashboard.isError.value" class="mt-5" state="error" title="季度番组加载失败" description="可以重试，或切换到搜索后继续添加订阅。" :retrying="dashboard.isFetching.value" @retry="dashboard.refetch()" />
        <StateBlock v-else-if="!dashboardItems.length" class="mt-5" state="empty" title="这一天暂时没有番组" description="可切换其他日期、OVA 或剧场版。" />
        <div v-else class="mt-5 grid grid-cols-2 gap-3 sm:grid-cols-3 lg:grid-cols-4" data-testid="mikan-dashboard-results">
          <button v-for="item in dashboardItems" :key="item.mikan_id" class="panel-muted group overflow-hidden text-left" @click="chooseAnime(item, dashboard.data.value?.season || `${selectedYear} ${selectedSeason}季`)">
            <img :src="mikanDiscoveryPosterURL(item.image, 360)" :alt="`${item.title} 海报`" loading="lazy" decoding="async" fetchpriority="low" class="aspect-[3/4] w-full object-cover transition group-hover:scale-[1.02]" @error="handlePosterError($event, normalizePosterURL(item.image))" />
            <span class="block p-3">
              <span class="block text-sm font-extrabold leading-5">{{ item.title }}</span>
              <span v-if="item.is_subscribed || item.is_local" class="mt-2 flex flex-wrap gap-1.5" data-testid="mikan-discovery-status">
                <span v-if="item.is_subscribed" class="badge badge-success">已订阅</span>
                <span v-if="item.is_local" class="badge badge-success">本地已有</span>
              </span>
            </span>
          </button>
        </div>
      </section>

      <section v-else role="tabpanel">
        <form class="flex gap-2" @submit.prevent="submitSearch">
          <label class="sr-only" for="mikan-search">搜索番剧名称</label>
          <input id="mikan-search" v-model="searchText" class="field" placeholder="输入番剧名称" autocomplete="off" />
          <AsyncButton type="submit" class="btn btn-primary shrink-0" :disabled="!searchText.trim()" :loading="searchResults.isFetching.value" loading-label="搜索中…"><Search :size="17" />搜索</AsyncButton>
        </form>
        <StateBlock v-if="resolveInitial && exactMatch.isFetching.value" class="mt-5" state="loading" title="正在按 Bangumi ID 精确关联 Mikan" description="会核对 Mikan 详情页中的 bgm.tv 条目链接。" />
        <StateBlock v-else-if="!exactItems.length && searchResults.isLoading.value" class="mt-5" state="loading" title="正在搜索 Mikan" />
        <StateBlock v-else-if="!exactItems.length && searchResults.isError.value" class="mt-5" state="error" title="搜索失败" description="请检查网络或代理设置后重试。" :retrying="searchResults.isFetching.value" @retry="searchResults.refetch()" />
        <StateBlock v-else-if="!submittedSearch" class="mt-5" state="empty" title="搜索 Mikan 番剧" description="输入中文、日文或英文番名均可。" />
        <StateBlock v-else-if="!displayedSearchItems.length" class="mt-5" state="empty" title="没有找到匹配番剧" description="尝试缩短关键词或使用原名搜索。" />
        <div v-else class="mt-5 grid gap-3 sm:grid-cols-2" data-testid="mikan-search-results">
          <p v-if="exactItems.length" class="badge col-span-full w-fit">已通过 Bangumi ID 精确匹配</p>
          <button v-for="item in displayedSearchItems" :key="item.mikan_id" class="panel-muted flex min-h-28 items-center gap-4 p-3 text-left" @click="chooseAnime(item)">
            <img :src="mikanDiscoveryPosterURL(item.image, 160)" :alt="`${item.title} 海报`" loading="lazy" decoding="async" fetchpriority="low" class="h-24 w-16 rounded-xl object-cover" @error="handlePosterError($event, normalizePosterURL(item.image))" />
            <span class="min-w-0">
              <strong class="line-clamp-2">{{ item.title }}</strong>
              <small class="muted mt-2 block">Mikan #{{ item.mikan_id }}</small>
              <span v-if="item.is_subscribed || item.is_local" class="mt-2 flex flex-wrap gap-1.5" data-testid="mikan-discovery-status">
                <span v-if="item.is_subscribed" class="badge badge-success">已订阅</span>
                <span v-if="item.is_local" class="badge badge-success">本地已有</span>
              </span>
            </span>
          </button>
        </div>
      </section>
    </template>

    <template v-else>
      <button class="btn btn-quiet mb-4 px-2" type="button" @click="backToBrowse"><ArrowLeft :size="17" />返回番剧列表</button>
      <div v-if="selectedAnime" class="panel-muted mb-5 flex items-center gap-4 p-4">
        <img :src="mikanDiscoveryPosterURL(selectedAnime.image, 160)" :alt="`${selectedAnime.title} 海报`" decoding="async" class="h-24 w-16 rounded-xl object-cover" @error="handlePosterError($event, normalizePosterURL(selectedAnime.image))" />
        <div class="min-w-0">
          <p class="eyebrow">MIKAN #{{ selectedAnime.mikan_id }}</p>
          <h3 class="mt-1 text-lg font-black">{{ selectedAnime.title }}</h3>
          <p class="muted mt-1 text-sm">{{ selectedAnime.season || '搜索结果' }}</p>
          <div v-if="selectedAnime.is_subscribed || selectedAnime.is_local" class="mt-2 flex flex-wrap gap-1.5" data-testid="mikan-selected-status">
            <span v-if="selectedAnime.is_subscribed" class="badge badge-success">已订阅</span>
            <span v-if="selectedAnime.is_local" class="badge badge-success">本地已有</span>
          </div>
        </div>
      </div>

      <StateBlock v-if="subgroups.isLoading.value" state="loading" title="正在读取字幕组" />
      <StateBlock v-else-if="subgroups.isError.value" state="error" title="字幕组加载失败" description="Mikan 暂时不可用时仍可返回手动填写 RSS。" :retrying="subgroups.isFetching.value" @retry="subgroups.refetch()" />
      <StateBlock v-else-if="!groupItems.length" state="empty" title="没有可用字幕组" description="该番剧目前还没有公开字幕组 RSS。" />
      <div v-else class="mikan-subgroup-layout grid gap-5 lg:grid-cols-[minmax(240px,0.8fr)_minmax(0,1.4fr)]">
        <section>
          <h3 class="flex items-center gap-2 font-black"><Users :size="18" />字幕组</h3>
          <div class="mt-3 grid gap-2" role="radiogroup" aria-label="选择字幕组">
            <button v-for="group in groupItems" :key="group.id || 'all'" class="panel-muted flex min-h-16 items-center justify-between gap-3 p-3 text-left" :class="selectedSubgroup?.id === group.id ? 'ring-2 ring-[var(--brand)]' : ''" role="radio" :aria-checked="selectedSubgroup?.id === group.id" @click="selectedSubgroup = group">
              <span><strong>{{ group.name }}</strong><small class="muted mt-1 block">{{ group.is_all ? '聚合所有字幕组结果' : '字幕组专属 RSS' }}</small></span>
              <Check v-if="selectedSubgroup?.id === group.id" class="shrink-0 text-[var(--brand)]" :size="19" />
            </button>
          </div>

          <div class="panel-muted mt-4 grid gap-3 p-4">
            <h3 class="font-black">资源筛选</h3>
            <label class="label" for="mikan-include-rule">
              必须包含（正则）
              <input id="mikan-include-rule" v-model="includeRule" class="field font-mono" placeholder="例如：1080[Pp].*(CHS|简中)" spellcheck="false" />
              <span v-if="includeRuleError" class="text-xs text-[var(--danger)]" role="alert">正则错误：{{ includeRuleError }}</span>
            </label>
            <label class="label" for="mikan-exclude-rule">
              必须不含（正则）
              <input id="mikan-exclude-rule" v-model="excludeRule" class="field font-mono" placeholder="例如：(720[Pp]|合集|NCOP)" spellcheck="false" />
              <span v-if="excludeRuleError" class="text-xs text-[var(--danger)]" role="alert">正则错误：{{ excludeRuleError }}</span>
            </label>
            <label class="label" for="mikan-resolution-filter">
              清晰度
              <select id="mikan-resolution-filter" v-model="resolutionFilter" class="field">
                <option value="">不限清晰度</option>
                <option value="2160p">2160P / 4K</option>
                <option value="1080p">1080P</option>
                <option value="720p">720P</option>
              </select>
            </label>
            <label class="label" for="mikan-subtitle-language">
              字幕语言
              <select id="mikan-subtitle-language" v-model="subtitleLanguage" class="field">
                <option value="">不限字幕</option>
                <option value="chs">简体中文（含简繁）</option>
                <option value="cht">繁体中文（含简繁）</option>
                <option value="chs_cht">简繁双语</option>
              </select>
            </label>
            <p class="muted text-xs">正则匹配资源标题；字幕组专属 RSS 已经限定字幕组，不会再自动添加重复规则。</p>
          </div>
        </section>

        <section class="mikan-preview-column" aria-live="polite">
          <div class="flex items-center justify-between gap-3"><h3 class="flex items-center gap-2 font-black"><Film :size="18" />最近资源</h3><span v-if="episodes.data.value" class="badge">预览命中 {{ filteredEpisodes.length }} / {{ episodes.data.value.items.length }}</span></div>
          <StateBlock v-if="episodes.isLoading.value" class="mt-3" state="loading" title="正在预览字幕组资源" />
          <StateBlock v-else-if="episodes.isError.value" class="mt-3" state="error" title="资源预览失败" description="可以重试；预览失败不会修改现有订阅。" :retrying="episodes.isFetching.value" @retry="episodes.refetch()" />
          <StateBlock v-else-if="!episodes.data.value?.items.length" class="mt-3" state="empty" title="该字幕组暂时没有资源" description="可以选择其他字幕组或稍后再试。" />
          <StateBlock v-else-if="!filteredEpisodes.length" class="mt-3" state="empty" title="最近资源没有命中筛选" description="可以放宽清晰度或字幕语言；保存后，新发布资源仍会继续按当前条件检查。" />
          <div v-else class="mikan-preview-list mt-3 space-y-2 overflow-y-scroll pr-1" data-testid="mikan-episode-preview">
            <article v-for="episode in filteredEpisodes" :key="`${episode.title}-${episode.pub_date}`" class="panel-muted p-3 text-sm">
              <strong class="line-clamp-2">{{ episode.title }}</strong>
              <div class="muted mt-2 flex flex-wrap gap-2 text-xs"><span v-if="episode.sub_group" class="badge">{{ episode.sub_group }}</span><span v-if="episode.episode_num">第 {{ episode.episode_num }} 集</span><span v-if="episode.resolution">{{ episode.resolution }}</span><span v-if="episode.size">{{ episode.size }}</span></div>
            </article>
          </div>
        </section>
      </div>

      <div class="mt-6 flex flex-wrap justify-end gap-2">
        <button class="btn btn-secondary" type="button" @click="backToBrowse">重新选择番剧</button>
        <AsyncButton class="btn btn-primary" :disabled="!selectedSubgroup || subgroups.isError.value || hasRegexError" :loading="saving || episodes.isFetching.value" :loading-label="saving ? '正在添加…' : '读取资源中…'" data-testid="confirm-mikan-selection" @click="confirmSelection">使用此订阅源</AsyncButton>
      </div>
    </template>
  </AppDialog>
</template>
