<script setup lang="ts">
import { computed, ref, watchEffect } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { CalendarDays, ExternalLink, Plus, Search } from '@lucide/vue'
import { api } from '../api/client'
import type { CalendarDay } from '../api/types'
import AppDialog from '../components/AppDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PosterCard from '../components/PosterCard.vue'
import StateBlock from '../components/StateBlock.vue'
import { useUIStore } from '../stores/ui'

type CalendarItem = CalendarDay['items'][number]
const ui=useUIStore(),qc=useQueryClient(),selected=ref<number>(new Date().getDay()||7),detail=ref<CalendarItem|null>(null),sources=ref<Array<Record<string,unknown>>>([]),busy=ref(false)
const query=useQuery({queryKey:['calendar'],queryFn:()=>api<{days:CalendarDay[];today:number}>('/calendar')})
watchEffect(()=>{if(query.data.value)selected.value=query.data.value.today})
const day=computed(()=>query.data.value?.days.find(d=>d.weekday.id===selected.value))
async function findSource(){if(!detail.value)return;busy.value=true;try{const result=await api<{items:Array<Record<string,unknown>>}>(`/subscriptions/search?q=${encodeURIComponent(detail.value.name_cn||detail.value.name)}`);sources.value=result.items||[]}catch(e){ui.toast(e instanceof Error?e.message:'搜索订阅源失败','error')}finally{busy.value=false}}
async function subscribe(source:Record<string,unknown>){const id=String(source.MikanID||source.mikan_id||'');const title=String(source.Title||source.title||detail.value?.name_cn||detail.value?.name||'');try{await api('/subscriptions',{method:'POST',body:JSON.stringify({title,rss_url:`https://mikanani.me/RSS/Bangumi?bangumiId=${encodeURIComponent(id)}`,expected_episodes:0,stale_after_hours:168}),headers:{'Content-Type':'application/json'}});ui.toast('订阅已添加');detail.value=null;sources.value=[];qc.invalidateQueries({queryKey:['subscriptions']})}catch(e){ui.toast(e instanceof Error?e.message:'添加订阅失败','error')}}
const sourceTitle=(item:Record<string,unknown>)=>String(item.Title||item.title||'未命名番剧')
</script>

<template><div class="page-grid"><PageHeader eyebrow="WEEKLY" title="追番日历" description="按播出日浏览本周动画，打开详情或直接查找可用订阅源。"><RouterLink to="/subscriptions" class="btn btn-primary"><Plus :size="17"/>管理订阅</RouterLink></PageHeader><StateBlock v-if="query.isLoading.value" state="loading"/><StateBlock v-else-if="query.isError.value" state="error" title="日历暂时不可用" description="Bangumi 连接可能超时，请稍后重试。" @retry="query.refetch()"/><template v-else-if="query.data.value"><div class="panel flex gap-2 overflow-x-auto p-2" role="tablist" aria-label="一周播出日"><button v-for="item in query.data.value.days" :key="item.weekday.id" class="btn min-w-[104px] flex-1" :class="selected===item.weekday.id?'btn-primary':'btn-quiet'" role="tab" :aria-selected="selected===item.weekday.id" @click="selected=item.weekday.id"><CalendarDays :size="15"/>{{ item.weekday.cn }}</button></div><section v-if="day?.items.length" class="poster-grid"><PosterCard v-for="item in day.items" :key="item.id" :title="item.name_cn||item.name" :image="item.images?.large||item.images?.common" :meta="item.air_date||'本周放送'" :badges="selected===query.data.value.today?['今日']:[]"><button class="btn btn-secondary mt-3 w-full text-xs" @click="detail=item;sources=[]">查看详情</button></PosterCard></section><StateBlock v-else state="empty" title="这一天暂无条目"/></template>
  <AppDialog :open="Boolean(detail)" :title="detail?.name_cn||detail?.name||'番剧详情'" description="查看条目信息，并从 Mikan 查找订阅源。" wide @update:open="v=>{if(!v){detail=null;sources=[]}}"><div class="grid gap-6 sm:grid-cols-[180px_1fr]"><img :src="detail?.images?.large||detail?.images?.common||'/static/img/no_poster.svg'" alt="" class="aspect-[2/3] w-full rounded-2xl object-cover"/><div><p class="muted text-sm leading-6">{{ detail?.summary||'暂无简介' }}</p><p class="muted mt-3 text-sm">{{ detail?.air_date||'播出日期待定' }}</p><div class="mt-5 flex flex-wrap gap-2"><a class="btn btn-secondary" :href="`https://bgm.tv/subject/${detail?.id}`" target="_blank" rel="noreferrer"><ExternalLink :size="16"/>Bangumi 条目</a><button class="btn btn-primary" :disabled="busy" @click="findSource"><Search :size="16"/>查找订阅源</button></div><div v-if="sources.length" class="mt-5 space-y-2"><button v-for="item in sources" :key="String(item.MikanID||item.mikan_id)" class="panel-muted flex min-h-14 w-full items-center justify-between gap-3 p-3 text-left" @click="subscribe(item)"><strong>{{ sourceTitle(item) }}</strong><span class="badge badge-success">添加订阅</span></button></div></div></div></AppDialog>
</div></template>
