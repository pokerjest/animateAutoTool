<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { CalendarDays, ExternalLink, Plus, Sparkles } from '@lucide/vue'
import { api, handlePosterError, normalizePosterURL } from '../api/client'
import type { CalendarDay, MikanSubscriptionSelection } from '../api/types'
import AppDialog from '../components/AppDialog.vue'
import MikanDiscoveryDialog from '../components/MikanDiscoveryDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PosterCard from '../components/PosterCard.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore } from '../stores/ui'

type CalendarItem = CalendarDay['items'][number]
const ui=useUIStore(),qc=useQueryClient(),actions=useAsyncActions(),selected=ref<number>(new Date().getDay()||7),detail=ref<CalendarItem|null>(null),subscriptionTarget=ref<CalendarItem|null>(null),mikanOpen=ref(false)
const query=useQuery({queryKey:['calendar'],queryFn:()=>api<{days:CalendarDay[];today:number}>('/calendar')})
watchEffect(()=>{if(query.data.value)selected.value=query.data.value.today})
const day=computed(()=>query.data.value?.days.find(d=>d.weekday.id===selected.value))
function openDetail(item:CalendarItem){detail.value=item}
function openMikanSubscription(){if(!detail.value)return;subscriptionTarget.value=detail.value;detail.value=null;mikanOpen.value=true}
function setMikanOpen(value:boolean){mikanOpen.value=value;if(!value)subscriptionTarget.value=null}
async function subscribe(selection:MikanSubscriptionSelection){try{await actions.run('subscribe',async()=>{await api('/subscriptions',{method:'POST',body:JSON.stringify({...selection,expected_episodes:0,stale_after_hours:168}),headers:{'Content-Type':'application/json'}});ui.toast(`已添加 Mikan 订阅 · ${selection.subtitle_group||'全部字幕组'}`);mikanOpen.value=false;subscriptionTarget.value=null;qc.invalidateQueries({queryKey:['subscriptions']})})}catch(e){ui.toast(e instanceof Error?e.message:'添加 Mikan 订阅失败','error')}}
</script>

<template><div class="page-grid"><PageHeader eyebrow="WEEKLY" title="追番日历" description="按播出日浏览本周动画；点击海报查看条目并选择 Mikan 订阅源。"><RouterLink to="/subscriptions" class="btn btn-primary"><Plus :size="17"/>管理订阅</RouterLink></PageHeader><StateBlock v-if="query.isLoading.value" state="loading"/><StateBlock v-else-if="query.isError.value" state="error" title="日历暂时不可用" description="Bangumi 连接可能超时，请稍后重试。" :retrying="query.isFetching.value" @retry="query.refetch()"/><template v-else-if="query.data.value"><div class="panel flex gap-2 overflow-x-auto p-2" role="tablist" aria-label="一周播出日"><button v-for="item in query.data.value.days" :key="item.weekday.id" class="btn min-w-[104px] flex-1" :class="selected===item.weekday.id?'btn-primary':'btn-quiet'" role="tab" :aria-selected="selected===item.weekday.id" @click="selected=item.weekday.id"><CalendarDays :size="15"/>{{ item.weekday.cn }}</button></div><section v-if="day?.items.length" class="poster-grid"><PosterCard v-for="item in day.items" :key="item.id" openable :title="item.name_cn||item.name" :image="item.images?.large||item.images?.common" :meta="item.air_date||'本周放送'" :badges="selected===query.data.value.today?['今日']:[]" @open="openDetail(item)"/></section><StateBlock v-else state="empty" title="这一天暂无条目"/></template>
  <AppDialog :open="Boolean(detail)" :title="detail?.name_cn||detail?.name||'番剧详情'" description="确认条目信息后，从 Mikan 选择番剧、字幕组和 RSS 源。" wide @update:open="v=>{if(!v)detail=null}"><div class="grid gap-6 sm:grid-cols-[180px_1fr]"><img :src="normalizePosterURL(detail?.images?.large||detail?.images?.common)" :alt="`${detail?.name_cn||detail?.name||'番剧'} 海报`" decoding="async" class="aspect-[2/3] w-full rounded-2xl object-cover" @error="handlePosterError($event)"/><div><p class="muted text-sm leading-6">{{ detail?.summary||'暂无简介' }}</p><p class="muted mt-3 text-sm">{{ detail?.air_date||'播出日期待定' }}</p><div class="panel-muted mt-5 p-4"><p class="eyebrow">MIKAN SUBSCRIPTION</p><strong class="mt-1 block">添加前会选择具体字幕组并预览最近资源</strong><p class="muted mt-1 text-xs">确认后保存 Mikan 番剧 ID、主 RSS、备用 RSS 和字幕组过滤规则。</p></div><div class="mt-5 flex flex-wrap gap-2"><a class="btn btn-secondary" :href="`https://bgm.tv/subject/${detail?.id}`" target="_blank" rel="noreferrer"><ExternalLink :size="16"/>Bangumi 条目</a><button class="btn btn-primary" @click="openMikanSubscription"><Sparkles :size="16"/>从 Mikan 添加订阅</button></div></div></div></AppDialog>
  <MikanDiscoveryDialog :open="mikanOpen" :saving="actions.isBusy('subscribe')" :initial-search="subscriptionTarget?.name_cn||subscriptionTarget?.name||''" @update:open="setMikanOpen" @select="subscribe"/>
</div></template>
