<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useInfiniteQuery, useQueryClient } from '@tanstack/vue-query'
import { CheckSquare2, CircleAlert, FolderCog, FolderPlus, ListChecks, Play, RefreshCw, Search, Sparkles, Trash2, WandSparkles, X } from '@lucide/vue'
import { api, apiEnvelope, handlePosterError, posterURL } from '../api/client'
import type { LocalAnime, LocalOrganizeSelection, MetadataSearchResult, TaskAccepted } from '../api/types'
import AppDialog from '../components/AppDialog.vue'
import AutoLoadSentinel from '../components/AutoLoadSentinel.vue'
import AsyncButton from '../components/AsyncButton.vue'
import ConfirmDialog from '../components/ConfirmDialog.vue'
import LocalOrganizeDialog from '../components/local/LocalOrganizeDialog.vue'
import PageHeader from '../components/PageHeader.vue'
import PosterCard from '../components/PosterCard.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useTaskStore } from '../stores/tasks'
import { useUIStore } from '../stores/ui'

interface Directory { ID:number; path:string; description:string }
interface Issue { ID:number; Title?:string; title?:string; Message?:string; message?:string; Hint?:string; hint?:string }
interface Payload { directories:Directory[]; items:LocalAnime[]; scan_status:Record<string,unknown>; diagnostics:Issue[] }
const ui=useUIStore(),tasks=useTaskStore(),qc=useQueryClient(),actions=useAsyncActions(),search=ref(''),adding=ref(false),dirPath=ref(''),deleteDir=ref<Directory|null>(null),selected=ref<LocalAnime|null>(null),matchQuery=ref(''),matchSource=ref('bangumi'),matchResults=ref<MetadataSearchResult[]>([])
const batchMode=ref(false),selectedIDs=ref(new Set<number>()),allMatching=ref(false),excludedIDs=ref(new Set<number>()),organizeOpen=ref(false),organizeSelection=ref<LocalOrganizeSelection|null>(null)
const debouncedSearch=ref('')
let searchTimer:ReturnType<typeof setTimeout>|undefined
watch(search,value=>{
  if(searchTimer)clearTimeout(searchTimer)
  searchTimer=setTimeout(()=>{debouncedSearch.value=value.trim();clearBatchSelection(false)},250)
})
onBeforeUnmount(()=>{if(searchTimer)clearTimeout(searchTimer)})
const query=useInfiniteQuery({
  queryKey:computed(()=>['local-anime',debouncedSearch.value]),
  initialPageParam:1,
  queryFn:({pageParam})=>apiEnvelope<Payload>(`/local-anime?page=${pageParam}&page_size=48&q=${encodeURIComponent(debouncedSearch.value)}`),
  getNextPageParam:lastPage=>{
    const page=lastPage.meta?.page??1
    const pageSize=lastPage.meta?.page_size??lastPage.data.items.length
    const total=lastPage.meta?.total??lastPage.data.items.length
    return page*pageSize<total?page+1:undefined
  },
})
const pages=computed(()=>query.data.value?.pages||[])
const firstPage=computed(()=>pages.value[0]?.data)
const items=computed(()=>pages.value.flatMap(page=>page.data.items))
const totalItems=computed(()=>pages.value[0]?.meta?.total??items.value.length)
const remainingItems=computed(()=>Math.max(0,totalItems.value-items.value.length))
const selectedCount=computed(()=>allMatching.value?Math.max(0,totalItems.value-excludedIDs.value.size):selectedIDs.value.size)
const scanTask=computed(()=>tasks.taskByID('local-scan'))
const scanPercent=computed(()=>scanTask.value?.total?Math.min(100,Math.round((scanTask.value.current||0)/scanTask.value.total*100)):0)
async function loadMore(){if(query.hasNextPage.value&&!query.isFetchingNextPage.value)await query.fetchNextPage()}
async function scan(){try{await actions.runTask('scan',()=>api<TaskAccepted>('/local-anime/scan',{method:'POST'}),'本地扫描','scan','正在扫描本地媒体目录');ui.toast('本地扫描已经启动')}catch(e){ui.toast(e instanceof Error?e.message:'扫描失败','error')}}
async function chooseDir(){try{await actions.run('choose-dir',async()=>{const result=await api<{path:string}>('/system/pick-directory',{method:'POST',body:JSON.stringify({title:'选择媒体目录',default_path:dirPath.value}),headers:{'Content-Type':'application/json'}});if(result.path)dirPath.value=result.path})}catch(e){ui.toast(e instanceof Error?e.message:'目录选择不可用','error')}}
async function add(){try{await actions.runTask('add-dir',async()=>{const task=await api<TaskAccepted>('/local-directories',{method:'POST',body:JSON.stringify({path:dirPath.value}),headers:{'Content-Type':'application/json'}});dirPath.value='';adding.value=false;ui.toast('目录已添加，扫描已经启动');qc.invalidateQueries({queryKey:['local-anime']});return task},'本地扫描','scan','目录已添加，正在扫描本地媒体')}catch(e){ui.toast(e instanceof Error?e.message:'添加失败','error')}}
async function removeDir(){if(!deleteDir.value)return;const id=deleteDir.value.ID;try{await actions.run(`remove-${id}`,async()=>{await api(`/local-directories/${id}`,{method:'DELETE'});ui.toast('目录已移除；磁盘文件未被删除');deleteDir.value=null;qc.invalidateQueries({queryKey:['local-anime']})})}catch(e){ui.toast(e instanceof Error?e.message:'移除失败','error')}}
function isItemSelected(id:number){return allMatching.value?!excludedIDs.value.has(id):selectedIDs.value.has(id)}
function toggleItemSelection(id:number){
  if(allMatching.value){const next=new Set(excludedIDs.value);if(next.has(id))next.delete(id);else next.add(id);excludedIDs.value=next;return}
  const next=new Set(selectedIDs.value);if(next.has(id))next.delete(id);else next.add(id);selectedIDs.value=next
}
function selectAllResults(){allMatching.value=true;selectedIDs.value=new Set();excludedIDs.value=new Set()}
function clearBatchSelection(close=true){allMatching.value=false;selectedIDs.value=new Set();excludedIDs.value=new Set();if(close)batchMode.value=false}
function toggleBatchMode(){if(batchMode.value)clearBatchSelection();else batchMode.value=true}
function openSingleOrganize(item:LocalAnime){organizeSelection.value={mode:'ids',anime_ids:[item.ID]};organizeOpen.value=true}
function openBatchOrganize(){
  if(!selectedCount.value)return
  organizeSelection.value=allMatching.value?{mode:'query',query:debouncedSearch.value,exclude_ids:[...excludedIDs.value]}:{mode:'ids',anime_ids:[...selectedIDs.value]}
  organizeOpen.value=true
}
function onOrganizeApplied(){clearBatchSelection();void qc.invalidateQueries({queryKey:['local-anime']})}
watch(()=>tasks.lastTransition,transition=>{if(transition?.task.kind==='organize'&&transition.task.tone!=='running')void qc.invalidateQueries({queryKey:['local-anime']})})
async function refreshMetadata(item:LocalAnime){try{await actions.runTask(`refresh-${item.ID}`,()=>api<TaskAccepted>(`/local-anime/${item.ID}/refresh-metadata`,{method:'POST'}),'刷新本地番剧元数据','metadata',`正在刷新 ${item.title}`);ui.toast('元数据刷新已经启动')}catch(e){ui.toast(e instanceof Error?e.message:'刷新失败','error')}}
async function switchSource(source:string){if(!selected.value)return;try{await actions.run(`source-${source}`,async()=>{await api(`/local-anime/${selected.value!.ID}/source?source=${source}`,{method:'POST'});ui.toast('显示数据源已切换');qc.invalidateQueries({queryKey:['local-anime']})})}catch(e){ui.toast(e instanceof Error?e.message:'切换失败','error')}}
async function searchMatch(){if(!matchQuery.value.trim())return;try{await actions.run('search-match',async()=>{matchResults.value=await api<MetadataSearchResult[]>(`/metadata/search?q=${encodeURIComponent(matchQuery.value)}&source=${matchSource.value}`)})}catch(e){ui.toast(e instanceof Error?e.message:'搜索失败','error')}}
async function fixMatch(result:MetadataSearchResult){if(!selected.value)return;const sourceID=result.id;try{await actions.run(`fix-${sourceID}`,async()=>{await api('/library/fix-match',{method:'POST',body:JSON.stringify({id:selected.value!.ID,type:'local',source:matchSource.value,source_id:sourceID}),headers:{'Content-Type':'application/json'}});ui.toast('匹配关系已更新');matchResults.value=[];qc.invalidateQueries({queryKey:['local-anime']})})}catch(e){ui.toast(e instanceof Error?e.message:'匹配失败','error')}}
const resultName=(item:MetadataSearchResult)=>item.name_cn||item.name||'未命名条目'
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="ON DEVICE" title="本地番剧" description="扫描本地媒体，检查元数据，并从同一工作区进入播放。"><button class="btn btn-secondary" :class="batchMode?'border-[var(--brand)] text-[var(--brand)]':''" @click="toggleBatchMode"><ListChecks :size="17"/>{{ batchMode?'退出批量':'批量整理' }}</button><button class="btn btn-secondary" @click="adding=true"><FolderPlus :size="17"/>添加目录</button><AsyncButton class="btn btn-primary" :loading="actions.isBusy('scan','local-scan')" loading-label="扫描中…" @click="scan"><RefreshCw :size="17"/>重新扫描</AsyncButton></PageHeader>
    <section v-if="scanTask?.tone==='running'" class="panel p-4" role="status" aria-live="polite" aria-busy="true">
      <div class="flex flex-wrap items-center justify-between gap-3"><div><p class="eyebrow">LOCAL SCAN</p><strong class="mt-1 block">{{ scanTask.detail }}</strong></div><span class="badge">{{ scanTask.total ? `${scanPercent}%` : '正在统计' }}</span></div>
      <div class="mt-3 h-2 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div v-if="scanTask.total" class="h-full rounded-full bg-[var(--brand)] transition-[width]" :style="{width:`${scanPercent}%`}"></div><div v-else class="h-full w-1/3 animate-pulse rounded-full bg-[var(--brand)]"></div></div>
      <p v-if="scanTask.total" class="muted mt-2 text-xs">{{ scanTask.current||0 }} / {{ scanTask.total }} 个扫描步骤</p>
    </section>
    <section v-if="firstPage?.diagnostics.length" class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200"><div class="flex items-start gap-3"><CircleAlert class="shrink-0"/><div><strong>媒体库有 {{ firstPage.diagnostics.length }} 项需要关注</strong><p class="mt-1 text-sm">{{ firstPage.diagnostics[0].Title||firstPage.diagnostics[0].title }}：{{ firstPage.diagnostics[0].Message||firstPage.diagnostics[0].message }}</p></div></div></section>
    <section class="panel p-4"><div class="grid gap-3 lg:grid-cols-[1fr_auto]"><label class="search-field"><Search :size="18" aria-hidden="true"/><input v-model="search" class="field field-leading-icon" placeholder="搜索本地番剧" aria-label="搜索本地番剧"/></label><span class="badge self-center">{{ totalItems }} 部本地番剧</span></div><div v-if="firstPage?.directories.length" class="mt-4 grid gap-2 lg:grid-cols-2"><article v-for="dir in firstPage.directories" :key="dir.ID" class="panel-muted flex min-w-0 items-center gap-3 p-3"><FolderCog class="shrink-0 text-[var(--sky)]" :size="19"/><div class="min-w-0 flex-1"><p class="truncate text-sm font-bold">{{ dir.path }}</p><p class="muted text-xs">扫描根目录</p></div><button class="btn btn-danger h-11 w-11 p-0" aria-label="移除目录" @click="deleteDir=dir"><Trash2 :size="16"/></button></article></div></section>
    <section v-if="batchMode" class="sticky top-3 z-20 rounded-2xl border border-[var(--brand)] bg-[var(--surface-solid)] p-3 shadow-lg"><div class="flex flex-wrap items-center gap-2"><span class="badge badge-success">已选择 {{ selectedCount }} 部</span><button v-if="!allMatching" class="btn btn-secondary" @click="selectAllResults"><CheckSquare2 :size="16"/>全选当前搜索结果（{{ totalItems }}）</button><button v-else class="btn btn-secondary" @click="clearBatchSelection(false)"><X :size="16"/>取消全选</button><span v-if="allMatching" class="muted text-xs">已包含尚未滚动加载的结果，可逐项排除。</span><span class="flex-1"></span><AsyncButton class="btn btn-primary" :disabled="!selectedCount" @click="openBatchOrganize"><WandSparkles :size="16"/>预览并整理</AsyncButton></div></section>
    <StateBlock v-if="query.isLoading.value" state="loading"/><StateBlock v-else-if="query.isError.value" state="error" title="本地媒体加载失败" :retrying="query.isFetching.value" @retry="query.refetch()"/><StateBlock v-else-if="!items.length" state="empty" title="还没有扫描到本地番剧" description="先添加包含番剧文件夹的根目录，然后运行一次扫描。"/>
    <section v-else class="poster-grid"><PosterCard v-for="item in items" :key="item.ID" :title="item.metadata?.title_cn||item.metadata?.title||item.title" :image="posterURL(item.metadata||{image:item.image},{width:360})" :meta="`${item.file_count} 集 · 第 ${item.season||1} 季`" :badges="[...(item.has_repair_actions?['待完善']:[]),...(batchMode&&isItemSelected(item.ID)?['已选择']:[])]"><div class="mt-3 grid gap-2"><button v-if="batchMode" class="btn w-full text-xs" :class="isItemSelected(item.ID)?'btn-primary':'btn-secondary'" @click="toggleItemSelection(item.ID)"><CheckSquare2 :size="14"/>{{ isItemSelected(item.ID)?'取消选择':'选择此番剧' }}</button><template v-else><RouterLink class="btn btn-primary w-full text-xs" :to="`/player?anime=${item.ID}`"><Play :size="14"/>查看与播放</RouterLink><button class="btn btn-secondary w-full text-xs" @click="openSingleOrganize(item)"><WandSparkles :size="14"/>整理文件</button><button class="btn btn-secondary w-full text-xs" @click="selected=item;matchQuery=item.title"><Sparkles :size="14"/>元数据与匹配</button></template></div></PosterCard></section>
    <AutoLoadSentinel v-if="query.hasNextPage.value" :remaining="remainingItems" @load="loadMore"/>
    <p v-if="query.isFetchingNextPage.value" class="muted py-3 text-center text-sm" role="status" aria-live="polite">正在加载更多本地番剧…</p>

    <AppDialog :open="adding" title="添加媒体目录" description="目录会在后台扫描，不会移动已有文件。" @update:open="adding=$event"><form @submit.prevent="add"><label class="label">绝对路径<div class="flex gap-2"><input v-model="dirPath" class="field" placeholder="/Volumes/Anime" required/><AsyncButton class="btn btn-secondary shrink-0" :loading="actions.isBusy('choose-dir')" loading-label="选择中…" @click="chooseDir">选择</AsyncButton></div></label><div class="mt-6 flex justify-end"><AsyncButton type="submit" class="btn btn-primary" :loading="actions.isBusy('add-dir','local-scan')" loading-label="扫描中…">添加并扫描</AsyncButton></div></form></AppDialog>
    <LocalOrganizeDialog :open="organizeOpen" :selection="organizeSelection" @update:open="organizeOpen=$event" @applied="onOrganizeApplied" />
    <AppDialog :open="Boolean(selected)" :title="selected?.metadata?.title_cn||selected?.title||'元数据'" description="刷新当前条目、切换显示源，或手动修正匹配。" wide @update:open="v=>{if(!v){selected=null;matchResults=[]}}"><div class="grid gap-5 lg:grid-cols-[.8fr_1.2fr]"><section class="panel-muted p-4"><img :src="posterURL(selected?.metadata||{image:selected?.image},{width:720})" alt="" decoding="async" class="mx-auto aspect-[2/3] w-full max-w-48 rounded-xl object-cover" @error="handlePosterError($event,selected?.image)"/><p class="muted mt-4 text-sm leading-6">{{ selected?.metadata?.summary||selected?.summary||'暂无简介' }}</p><AsyncButton class="btn btn-secondary mt-4 w-full" :loading="Boolean(selected&&actions.isBusy(`refresh-${selected.ID}`,`local-metadata-${selected.ID}`))" loading-label="刷新中…" @click="selected&&refreshMetadata(selected)"><RefreshCw :size="16"/>刷新元数据</AsyncButton><div class="mt-3 grid grid-cols-3 gap-2"><AsyncButton v-for="source in ['bangumi','tmdb','anilist']" :key="source" class="btn btn-quiet px-2 text-xs" :loading="actions.isBusy(`source-${source}`)" loading-label="切换中…" @click="switchSource(source)">{{ source }}</AsyncButton></div></section><section><div class="grid gap-3 sm:grid-cols-[140px_1fr_auto]"><select v-model="matchSource" class="field"><option value="bangumi">Bangumi</option><option value="tmdb">TMDB</option><option value="anilist">AniList</option></select><input v-model="matchQuery" class="field" placeholder="搜索标题" @keydown.enter.prevent="searchMatch"/><AsyncButton class="btn btn-primary" :loading="actions.isBusy('search-match')" loading-label="搜索中…" @click="searchMatch"><Search :size="16"/>搜索</AsyncButton></div><div class="mt-4 space-y-2"><AsyncButton v-for="item in matchResults" :key="item.id" class="panel-muted flex min-h-16 w-full items-center justify-between gap-3 p-3 text-left" :loading="actions.isBusy(`fix-${item.id}`)" loading-label="正在应用匹配…" @click="fixMatch(item)"><span><strong>{{ resultName(item) }}</strong><small class="muted mt-1 block">#{{ item.id }}</small></span><span class="badge badge-success">使用此匹配</span></AsyncButton></div><StateBlock v-if="!matchResults.length" state="empty" title="搜索并选择正确条目" description="手动匹配会同步更新订阅、本地库和 NFO 数据。"/></section></div></AppDialog>
    <ConfirmDialog :open="Boolean(deleteDir)" danger :loading="Boolean(deleteDir&&actions.isBusy(`remove-${deleteDir.ID}`))" loading-label="移除中…" title="移除扫描目录？" :description="`只会移除 ${deleteDir?.path||''} 的索引，不会删除磁盘上的媒体文件。`" confirm-label="移除目录" @update:open="v=>{if(!v)deleteDir=null}" @confirm="removeDir"/>
  </div>
</template>
