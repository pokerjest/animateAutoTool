<script setup lang="ts">
import { computed, onBeforeUnmount, ref, watch } from 'vue'
import { useInfiniteQuery, useQueryClient } from '@tanstack/vue-query'
import { useRouter } from 'vue-router'
import { CheckSquare2, CircleAlert, FolderCog, FolderPlus, ListChecks, MoreHorizontal, RefreshCw, Search, Sparkles, Trash2, WandSparkles, X } from '@lucide/vue'
import { api, apiEnvelope, handlePosterError, posterURL } from '../api/client'
import type { AIAnalysisAccepted, LocalAnime, LocalOrganizeSelection, MetadataMatchCandidate, MetadataMatchSearchResult, TaskAccepted } from '../api/types'
import AIProposalPanel from '../components/AIProposalPanel.vue'
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
import { localPlayerLocation } from '../utils/playerRoutes'

interface Directory { ID:number; path:string; description:string }
interface Issue { ID:number; Title?:string; title?:string; Message?:string; message?:string; Hint?:string; hint?:string }
interface ScanStatus { IsRunning?:boolean; LastSummary?:string; LastFinishedAt?:string; LastDuration?:string; ParseFailureCount?:number; ParseConflictCount?:number; DiscoveredFiles?:number; CandidateSeries?:number }
interface Payload { directories:Directory[]; items:LocalAnime[]; scan_status:ScanStatus; diagnostics:Issue[] }
const router=useRouter(),ui=useUIStore(),tasks=useTaskStore(),qc=useQueryClient(),actions=useAsyncActions(),search=ref(''),adding=ref(false),dirPath=ref(''),deleteDir=ref<Directory|null>(null),selected=ref<LocalAnime|null>(null),matchQuery=ref(''),matchSource=ref('bangumi'),matchResults=ref<MetadataMatchCandidate[]>([]),matchStatus=ref<MetadataMatchSearchResult['source_status']>({}),metadataAIProposalID=ref(''),healthAIProposalID=ref('')
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
const scanStatus=computed(()=>firstPage.value?.scan_status)
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
function openAnimeCard(item:LocalAnime){if(batchMode.value){toggleItemSelection(item.ID);return}void router.push(localPlayerLocation(item.ID,undefined,true))}
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
function selectedSourceID(){const metadata=selected.value?.metadata;if(!metadata)return 0;return matchSource.value==='bangumi'?metadata.bangumi_id:matchSource.value==='tmdb'?metadata.tmdb_id:metadata.anilist_id}
async function searchMatch(){if(!matchQuery.value.trim()&&!selected.value)return;try{await actions.run('search-match',async()=>{const params=new URLSearchParams({q:matchQuery.value||selected.value?.title||'',source:matchSource.value});const sourceID=selectedSourceID();if(sourceID)params.set('source_id',String(sourceID));const result=await api<MetadataMatchSearchResult>(`/metadata/match-search?${params}`);matchResults.value=result.candidates;matchStatus.value=result.source_status})}catch(e){ui.toast(e instanceof Error?e.message:'搜索失败','error')}}
async function suggestMetadata(){if(!selected.value)return;try{const accepted=await actions.runTask(`ai-metadata-${selected.value.ID}`,()=>api<AIAnalysisAccepted>(`/ai/metadata/local-anime/${selected.value!.ID}/suggest`,{method:'POST',body:JSON.stringify({source:matchSource.value,source_id:selectedSourceID(),query:matchQuery.value})}),'AI 元数据匹配','ai-analysis',`正在比对 ${selected.value.title} 的真实三源候选`);metadataAIProposalID.value=accepted.proposal_id}catch(e){ui.toast(e instanceof Error?e.message:'启动 AI 元数据匹配失败','error')}}
async function analyzeIssue(issue:Issue){if(!issue.ID)return;try{const accepted=await actions.runTask(`ai-issue-${issue.ID}`,()=>api<AIAnalysisAccepted>(`/ai/library-issues/${issue.ID}/analyze`,{method:'POST'}),'AI 媒体库诊断','ai-analysis','正在分析媒体库问题和脱敏日志');healthAIProposalID.value=accepted.proposal_id}catch(e){ui.toast(e instanceof Error?e.message:'启动 AI 媒体库诊断失败','error')}}
async function fixMatch(result:MetadataMatchCandidate){if(!selected.value)return;const matches={bangumi_id:result.bangumi?.id||0,tmdb_id:result.tmdb?.id||0,anilist_id:result.anilist?.id||0};try{await actions.run(`fix-${result.bangumi?.id||result.tmdb?.id||result.anilist?.id}`,async()=>{await api('/library/fix-match',{method:'POST',body:JSON.stringify({id:selected.value!.ID,type:'local',matches}),headers:{'Content-Type':'application/json'}});ui.toast('三源匹配已更新');matchResults.value=[];qc.invalidateQueries({queryKey:['local-anime']})})}catch(e){ui.toast(e instanceof Error?e.message:'匹配失败','error')}}
const resultKey=(item:MetadataMatchCandidate)=>`${item.bangumi?.id||0}-${item.tmdb?.id||0}-${item.anilist?.id||0}`
const sourceLabel=(source:string)=>source==='bangumi'?'Bangumi':source==='tmdb'?'TMDB':'AniList'
</script>

<template>
  <div class="page-grid">
    <PageHeader eyebrow="ON DEVICE" title="本地番剧" description="扫描本地媒体，检查元数据，并从同一工作区进入播放。"><button class="btn btn-secondary" :class="batchMode?'border-[var(--brand)] text-[var(--brand)]':''" @click="toggleBatchMode"><ListChecks :size="17"/>{{ batchMode?'退出批量':'批量整理' }}</button><button class="btn btn-secondary" @click="adding=true"><FolderPlus :size="17"/>添加目录</button><AsyncButton class="btn btn-primary" :loading="actions.isBusy('scan','local-scan')" loading-label="扫描中…" @click="scan"><RefreshCw :size="17"/>重新扫描</AsyncButton></PageHeader>
    <section v-if="scanTask?.tone==='running'" class="panel p-4" role="status" aria-live="polite" aria-busy="true">
      <div class="flex flex-wrap items-center justify-between gap-3"><div><p class="eyebrow">LOCAL SCAN</p><strong class="mt-1 block">{{ scanTask.detail }}</strong></div><span class="badge">{{ scanTask.total ? `${scanPercent}%` : '正在统计' }}</span></div>
      <div class="mt-3 h-2 overflow-hidden rounded-full bg-[var(--surface-muted)]"><div v-if="scanTask.total" class="h-full rounded-full bg-[var(--brand)] transition-[width]" :style="{width:`${scanPercent}%`}"></div><div v-else class="h-full w-1/3 animate-pulse rounded-full bg-[var(--brand)]"></div></div>
      <p v-if="scanTask.total" class="muted mt-2 text-xs">{{ scanTask.current||0 }} / {{ scanTask.total }} 个扫描步骤</p>
    </section>
    <section v-else-if="scanStatus?.LastSummary" class="panel-muted flex flex-wrap items-center gap-3 p-4 text-sm">
      <div class="min-w-0 flex-1"><strong>最近扫描</strong><p class="muted mt-1">{{ scanStatus.LastSummary }}</p></div>
      <span v-if="scanStatus.LastDuration" class="badge">{{ scanStatus.LastDuration }}</span>
      <span v-if="scanStatus.ParseFailureCount" class="badge border-amber-300 text-amber-700 dark:text-amber-300">无法识别 {{ scanStatus.ParseFailureCount }}</span>
      <span v-if="scanStatus.ParseConflictCount" class="badge border-amber-300 text-amber-700 dark:text-amber-300">编号冲突 {{ scanStatus.ParseConflictCount }}</span>
    </section>
    <section v-if="firstPage?.diagnostics.length" class="rounded-2xl border border-amber-200 bg-amber-50 p-4 text-amber-900 dark:border-amber-900 dark:bg-amber-950/40 dark:text-amber-200"><div class="flex flex-wrap items-start gap-3"><CircleAlert class="shrink-0"/><div class="min-w-0 flex-1"><strong>媒体库有 {{ firstPage.diagnostics.length }} 项需要关注</strong><p class="mt-1 text-sm">{{ firstPage.diagnostics[0].Title||firstPage.diagnostics[0].title }}：{{ firstPage.diagnostics[0].Message||firstPage.diagnostics[0].message }}</p></div><AsyncButton v-if="firstPage.diagnostics[0].ID" class="btn btn-secondary shrink-0" :loading="actions.isBusy(`ai-issue-${firstPage.diagnostics[0].ID}`)" loading-label="分析中…" @click="analyzeIssue(firstPage.diagnostics[0])"><Sparkles :size="15"/>AI 分析</AsyncButton></div></section>
    <AIProposalPanel v-if="healthAIProposalID" :proposal-id="healthAIProposalID" @applied="healthAIProposalID=''" @dismissed="healthAIProposalID=''"/>
    <section class="panel p-4"><div class="grid gap-3 lg:grid-cols-[1fr_auto]"><label class="search-field"><Search :size="18" aria-hidden="true"/><input v-model="search" class="field field-leading-icon" placeholder="搜索本地番剧" aria-label="搜索本地番剧"/></label><span class="badge self-center">{{ totalItems }} 部本地番剧</span></div><div v-if="firstPage?.directories.length" class="mt-4 grid gap-2 lg:grid-cols-2"><article v-for="dir in firstPage.directories" :key="dir.ID" class="panel-muted flex min-w-0 items-center gap-3 p-3"><FolderCog class="shrink-0 text-[var(--sky)]" :size="19"/><div class="min-w-0 flex-1"><p class="truncate text-sm font-bold">{{ dir.path }}</p><p class="muted text-xs">扫描根目录</p></div><button class="btn btn-danger h-11 w-11 p-0" aria-label="移除目录" @click="deleteDir=dir"><Trash2 :size="16"/></button></article></div></section>
    <section v-if="batchMode" class="sticky top-3 z-20 rounded-2xl border border-[var(--brand)] bg-[var(--surface-solid)] p-3 shadow-lg"><div class="flex flex-wrap items-center gap-2"><span class="badge badge-success">已选择 {{ selectedCount }} 部</span><button v-if="!allMatching" class="btn btn-secondary" @click="selectAllResults"><CheckSquare2 :size="16"/>全选当前搜索结果（{{ totalItems }}）</button><button v-else class="btn btn-secondary" @click="clearBatchSelection(false)"><X :size="16"/>取消全选</button><span v-if="allMatching" class="muted text-xs">已包含尚未滚动加载的结果，可逐项排除。</span><span class="flex-1"></span><AsyncButton class="btn btn-primary" :disabled="!selectedCount" @click="openBatchOrganize"><WandSparkles :size="16"/>预览并整理</AsyncButton></div></section>
    <StateBlock v-if="query.isLoading.value" state="loading"/><StateBlock v-else-if="query.isError.value" state="error" title="本地媒体加载失败" :retrying="query.isFetching.value" @retry="query.refetch()"/><StateBlock v-else-if="!items.length" state="empty" title="还没有扫描到本地番剧" description="先添加包含番剧文件夹的根目录，然后运行一次扫描。"/>
    <section v-else class="poster-grid"><PosterCard v-for="item in items" :key="item.ID" openable :open-label="batchMode?(isItemSelected(item.ID)?'点击取消选择':'点击选择'):'点击卡片播放'" :title="item.metadata?.title_cn||item.metadata?.title||item.title" :image="posterURL(item.metadata||{image:item.image},{width:360})" :meta="`${item.file_count} 集 · 第 ${item.season||1} 季`" :badges="[...(item.has_repair_actions?['待完善']:[]),...(batchMode&&isItemSelected(item.ID)?['已选择']:[])]" @open="openAnimeCard(item)"><template v-if="!batchMode" #actions><details class="poster-card-menu"><summary aria-label="更多操作" title="更多操作"><MoreHorizontal :size="17"/></summary><div class="poster-card-menu-content"><button aria-label="整理文件" @click="openSingleOrganize(item)"><WandSparkles :size="14"/>整理文件</button><button aria-label="元数据与匹配" @click="selected=item;matchQuery=item.title"><Sparkles :size="14"/>元数据与匹配</button></div></details></template></PosterCard></section>
    <AutoLoadSentinel v-if="query.hasNextPage.value" :remaining="remainingItems" :loading="query.isFetchingNextPage.value" :paused="query.isError.value" @load="loadMore"/>
    <p v-if="query.isFetchingNextPage.value" class="muted py-3 text-center text-sm" role="status" aria-live="polite">正在加载更多本地番剧…</p>

    <AppDialog :open="adding" title="添加媒体目录" description="目录会在后台扫描，不会移动已有文件。" @update:open="adding=$event"><form @submit.prevent="add"><label class="label">绝对路径<div class="flex gap-2"><input v-model="dirPath" class="field" placeholder="/Volumes/Anime" required/><AsyncButton class="btn btn-secondary shrink-0" :loading="actions.isBusy('choose-dir')" loading-label="选择中…" @click="chooseDir">选择</AsyncButton></div></label><div class="mt-6 flex justify-end"><AsyncButton type="submit" class="btn btn-primary" :loading="actions.isBusy('add-dir','local-scan')" loading-label="扫描中…">添加并扫描</AsyncButton></div></form></AppDialog>
    <LocalOrganizeDialog :open="organizeOpen" :selection="organizeSelection" @update:open="organizeOpen=$event" @applied="onOrganizeApplied" />
    <AppDialog :open="Boolean(selected)" :title="selected?.metadata?.title_cn||selected?.title||'元数据'" description="从一个来源出发联查 Bangumi、TMDB、AniList；确认后会一次性同步三源关系。" wide @update:open="v=>{if(!v){selected=null;matchResults=[];matchStatus={};metadataAIProposalID=''}}"><div class="grid gap-5 lg:grid-cols-[.8fr_1.2fr]"><section class="panel-muted p-4"><img :src="posterURL(selected?.metadata||{image:selected?.image},{width:720})" alt="" decoding="async" class="mx-auto aspect-[2/3] w-full max-w-48 rounded-xl object-cover" @error="handlePosterError($event,selected?.image)"/><p class="muted mt-4 text-sm leading-6">{{ selected?.metadata?.summary||selected?.summary||'暂无简介' }}</p><AsyncButton class="btn btn-secondary mt-4 w-full" :loading="Boolean(selected&&actions.isBusy(`refresh-${selected.ID}`,`local-metadata-${selected.ID}`))" loading-label="刷新中…" @click="selected&&refreshMetadata(selected)"><RefreshCw :size="16"/>刷新元数据</AsyncButton><div class="mt-3 grid grid-cols-3 gap-2"><AsyncButton v-for="source in ['bangumi','tmdb','anilist']" :key="source" class="btn btn-quiet px-2 text-xs" :loading="actions.isBusy(`source-${source}`)" loading-label="切换中…" @click="switchSource(source)">{{ source }}</AsyncButton></div></section><section><div class="grid gap-3 sm:grid-cols-[140px_1fr_auto_auto]"><select v-model="matchSource" class="field"><option value="bangumi">Bangumi</option><option value="tmdb">TMDB</option><option value="anilist">AniList</option></select><input v-model="matchQuery" class="field" placeholder="搜索标题" @keydown.enter.prevent="searchMatch"/><AsyncButton class="btn btn-secondary" :loading="Boolean(selected&&actions.isBusy(`ai-metadata-${selected.ID}`))" loading-label="AI 比对中…" @click="suggestMetadata"><Sparkles :size="16"/>AI 联查匹配</AsyncButton><AsyncButton class="btn btn-primary" :loading="actions.isBusy('search-match')" loading-label="搜索中…" @click="searchMatch"><Search :size="16"/>三源搜索</AsyncButton></div><div v-if="Object.keys(matchStatus).length" class="mt-3 flex flex-wrap gap-2"><span v-for="(status,source) in matchStatus" :key="source" class="badge" :class="status.error?'badge-warning':status.searched?'badge-success':''">{{ sourceLabel(source) }}：{{ status.error||`${status.count} 个候选` }}</span></div><AIProposalPanel v-if="metadataAIProposalID" class="mt-4" :proposal-id="metadataAIProposalID" compact @applied="metadataAIProposalID='';matchResults=[];qc.invalidateQueries({queryKey:['local-anime']})" @dismissed="metadataAIProposalID=''"/><div class="mt-4 space-y-2"><AsyncButton v-for="item in matchResults" :key="resultKey(item)" class="panel-muted block w-full p-3 text-left" :loading="actions.isBusy(`fix-${item.bangumi?.id||item.tmdb?.id||item.anilist?.id}`)" loading-label="正在应用匹配…" @click="fixMatch(item)"><div class="flex items-center justify-between gap-3"><strong>{{ item.title||'未命名条目' }}</strong><span class="badge badge-success">确认三源匹配</span></div><div class="mt-2 grid gap-2 text-xs sm:grid-cols-3"><span :class="item.bangumi?'':'muted'">Bangumi：{{ item.bangumi?`${item.bangumi.name_cn||item.bangumi.name} (#${item.bangumi.id})`:'未找到' }}</span><span :class="item.tmdb?'':'muted'">TMDB：{{ item.tmdb?`${item.tmdb.name_cn||item.tmdb.name} (#${item.tmdb.id})`:'未找到/未配置' }}</span><span :class="item.anilist?'':'muted'">AniList：{{ item.anilist?`${item.anilist.name_cn||item.anilist.name} (#${item.anilist.id})`:'未找到/未配置' }}</span></div><p v-if="item.evidence?.length" class="muted mt-2 text-xs">{{ item.evidence.join(' · ') }}</p></AsyncButton></div><StateBlock v-if="!matchResults.length&&!metadataAIProposalID" state="empty" title="搜索并选择正确条目" description="手动匹配会同步更新订阅、本地库和 NFO 数据。AI 也只能从这里显示的真实候选中选择。"/></section></div></AppDialog>
    <ConfirmDialog :open="Boolean(deleteDir)" danger :loading="Boolean(deleteDir&&actions.isBusy(`remove-${deleteDir.ID}`))" loading-label="移除中…" title="移除扫描目录？" :description="`只会移除 ${deleteDir?.path||''} 的索引，不会删除磁盘上的媒体文件。`" confirm-label="移除目录" @update:open="v=>{if(!v)deleteDir=null}" @confirm="removeDir"/>
  </div>
</template>
