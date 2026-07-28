<script setup lang="ts">
import { computed, reactive, ref, watchEffect } from 'vue'
import { useQuery, useQueryClient } from '@tanstack/vue-query'
import { Bot, Cloud, Database, Download, Film, KeyRound, Network, Palette, RefreshCw, Save, Settings2, ShieldCheck, Wrench } from '@lucide/vue'
import { useRoute } from 'vue-router'
import { api } from '../api/client'
import type { AIToolRun, MediaLibrary, TaskAccepted } from '../api/types'
import AsyncButton from '../components/AsyncButton.vue'
import AISettingsPanel from '../components/AISettingsPanel.vue'
import LocalRecoveryLink from '../components/LocalRecoveryLink.vue'
import PageHeader from '../components/PageHeader.vue'
import StateBlock from '../components/StateBlock.vue'
import { useAsyncActions } from '../composables/useAsyncActions'
import { useUIStore, type BackgroundMode, type ThemeMode } from '../stores/ui'
import { usePlaybackStore } from '../stores/playback'
import { useWorkspaceStore } from '../stores/workspace'

interface SettingsData{values:Record<string,string>;configured:Record<string,boolean>;stats:Record<string,unknown>;request_ip?:string}
interface Field { key:string; label:string; type?:'text'|'password'|'select'|'boolean'; options?:Array<{value:string;label:string}>; placeholder?:string; description?:string }
interface Group { id:string;label:string;icon:unknown;fields:Field[];providers?:string[] }
interface MediaApp { id:string;title:string;eyebrow:string;description:string;icon:unknown;fields:Field[];provider?:string }
interface AuditEntry { id:number;created_at:string;username:string;action:string;outcome:string;ip:string;target_type:string;target_id:string }
const jellyfinFields:Field[]=[
  {key:'jellyfin_url',label:'AnimateTool 连接地址',placeholder:'例如 http://127.0.0.1:8096',description:'由 AnimateTool 后端访问 Jellyfin，建议填写本机地址、局域网地址或服务器可达的 Tailscale 地址。'},
  {key:'jellyfin_direct_url',label:'浏览器直连地址（可选）',placeholder:'例如 https://jellyfin.example-tailnet.ts.net',description:'由观看设备直接访问 Jellyfin，适合已连接 Tailscale 或处于同一局域网的设备；留空时隐藏 Jellyfin 直连线路。'},
  {key:'jellyfin_username',label:'用户名'},
  {key:'jellyfin_password',label:'密码',type:'password'},
  {key:'jellyfin_api_key',label:'API Key',type:'password'},
]
const alistFields:Field[]=[{key:'alist_url',label:'服务地址'},{key:'alist_token',label:'Token',type:'password'}]
const mediaApps:MediaApp[]=[
  {id:'jellyfin',title:'Jellyfin',eyebrow:'媒体服务器',description:'在这里完成服务器连接、媒体库范围和播放器线路测试。',icon:Film,fields:jellyfinFields,provider:'jellyfin'},
  {id:'alist',title:'AList',eyebrow:'文件聚合',description:'连接独立的 AList 服务，用于访问聚合后的文件资源。',icon:Cloud,fields:alistFields},
]
const groups:Group[]=[
  {id:'downloader',label:'下载器',icon:Download,providers:['qb'],fields:[{key:'qb_mode',label:'运行模式',type:'select',options:[{value:'managed',label:'内置托管'},{value:'external',label:'外部 Web UI'}]},{key:'qb_url',label:'Web UI 地址'},{key:'qb_username',label:'用户名'},{key:'qb_password',label:'密码',type:'password'},{key:'base_download_dir',label:'媒体根目录'},{key:'auto_rename_enabled',label:'下载完成后自动整理',type:'boolean'},{key:'auto_rename_series_template',label:'系列文件夹模板'},{key:'auto_rename_episode_template',label:'剧集文件模板'}]},
  {id:'metadata',label:'元数据',icon:Database,providers:['bangumi','tmdb','anilist'],fields:[{key:'tmdb_token',label:'TMDB Token',type:'password'},{key:'anilist_token',label:'AniList Token',type:'password'},{key:'bangumi_app_id',label:'Bangumi App ID'},{key:'bangumi_app_secret',label:'Bangumi App Secret',type:'password'},{key:'bangumi_access_token',label:'Bangumi Access Token',type:'password'}]},
  {id:'network',label:'网络代理',icon:Network,providers:['mikan'],fields:[{key:'proxy_url',label:'代理地址'},{key:'proxy_bangumi_enabled',label:'Bangumi 使用代理',type:'boolean'},{key:'proxy_mikan_enabled',label:'Mikan 使用代理',type:'boolean'},{key:'proxy_tmdb_enabled',label:'TMDB 使用代理',type:'boolean'},{key:'proxy_anilist_enabled',label:'AniList 使用代理',type:'boolean'},{key:'proxy_jellyfin_enabled',label:'Jellyfin 使用代理',type:'boolean'},{key:'proxy_ai_enabled',label:'AI 服务使用代理',type:'boolean'},{key:'proxy_updater_enabled',label:'应用更新使用代理',type:'boolean'}]},
  {id:'media',label:'媒体服务',icon:Film,providers:['jellyfin'],fields:[...jellyfinFields,...alistFields]},
  {id:'ai',label:'AI 助手',icon:Bot,fields:[
    {key:'ai_provider',label:'当前服务商'},
    {key:'ai_openai_base_url',label:'OpenAI Base URL'},{key:'ai_openai_model',label:'OpenAI 模型'},{key:'ai_openai_api_key',label:'OpenAI API Key',type:'password'},
    {key:'ai_gemini_api_format',label:'Gemini API 格式'},{key:'ai_gemini_base_url',label:'Gemini Base URL'},{key:'ai_gemini_model',label:'Gemini 模型'},{key:'ai_gemini_api_key',label:'Gemini API Key',type:'password'},
    {key:'ai_claude_api_format',label:'Claude API 格式'},{key:'ai_claude_base_url',label:'Claude Base URL'},{key:'ai_claude_model',label:'Claude 模型'},{key:'ai_claude_api_key',label:'Claude API Key',type:'password'},
  ]},
  {id:'cloud',label:'云备份',icon:Cloud,fields:[{key:'r2_endpoint',label:'R2 Endpoint'},{key:'r2_bucket',label:'Bucket'},{key:'r2_access_key',label:'Access Key',type:'password'},{key:'r2_secret_key',label:'Secret Key',type:'password'}]},
  {id:'appearance',label:'外观',icon:Palette,fields:[]},
  {id:'security',label:'安全',icon:ShieldCheck,fields:[]},
  {id:'maintenance',label:'应用维护',icon:Wrench,fields:[]},
]
const route=useRoute(),ui=useUIStore(),playback=usePlaybackStore(),workspace=useWorkspaceStore(),qc=useQueryClient(),actions=useAsyncActions(),active=ref(String(route.query.focus||'downloader')),form=reactive<Record<string,string>>({}),connection=reactive<Record<string,{connected:boolean;detail:string;account?:string;source?:string;source_label?:string}|null>>({}),oldPassword=ref(''),newPassword=ref(''),confirmPassword=ref('')
const query=useQuery({queryKey:['settings'],queryFn:()=>api<SettingsData>('/settings')})
const mediaLibraries=useQuery({queryKey:['settings-media-libraries'],queryFn:()=>api<{items:MediaLibrary[]}>('/media/providers/jellyfin/libraries'),enabled:computed(()=>Boolean(query.data.value?.values.jellyfin_url&&query.data.value?.values.jellyfin_api_key)),retry:false})
const audits=useQuery({queryKey:['audit-logs'],queryFn:()=>api<{items:AuditEntry[]}>('/audit-logs?page_size=25')})
const aiToolRuns=useQuery({queryKey:['ai-tool-runs'],queryFn:()=>api<{items:AIToolRun[]}>('/ai/tool-runs?limit=30'),enabled:computed(()=>active.value==='ai')})
const maintenance=useQuery({queryKey:['maintenance'],queryFn:()=>api<Record<string,unknown>>('/settings/maintenance'),refetchInterval:15000})
watchEffect(()=>{if(query.data.value)Object.assign(form,query.data.value.values)})
watchEffect(()=>{const focus=String(route.query.focus||'');if(focus==='media'&&groups.some(item=>item.id==='media'))active.value='media'})
const group=computed(()=>groups.find(g=>g.id===active.value)||groups[0])
const isSecret=(field:Field)=>field.type==='password'
const fieldPlaceholder=(field:Field)=>field.placeholder||(field.key==='proxy_url'?'例如 http://127.0.0.1:7890 或 socks5://127.0.0.1:7890':isSecret(field)&&query.data.value?.configured[field.key]?'已配置；留空表示保持不变':'')
const playbackSourceLabel = computed(() => playback.preferredSource === 'direct' ? 'Jellyfin 直连' : 'AnimateTool 代理')
async function save(){try{await actions.run('save',async()=>{await api('/settings',{method:'PUT',body:JSON.stringify({values:form}),headers:{'Content-Type':'application/json'}});ui.toast('设置已保存并同步到本地 config.yaml');qc.invalidateQueries({queryKey:['settings']});workspace.invalidateMediaAvailability();void workspace.refreshMediaAvailability()})}catch(e){ui.toast(e instanceof Error?e.message:'保存失败','error')}}
async function testProvider(provider:string){connection[provider]=null;try{await actions.run(`provider-${provider}`,async()=>{const query=provider==='jellyfin'?`?source=${encodeURIComponent(playback.preferredSource)}`:'';connection[provider]=await api(`/settings/connections/${provider}${query}`)})}catch(e){connection[provider]={connected:false,detail:e instanceof Error?e.message:'连接失败'}}}
async function testProxy(){connection.proxy=null;try{await actions.run('test-proxy',async()=>{connection.proxy=await api('/settings/proxy/test',{method:'POST',body:JSON.stringify({proxy_url:form.proxy_url||''}),headers:{'Content-Type':'application/json'}})})}catch(e){connection.proxy={connected:false,detail:e instanceof Error?e.message:'代理连接失败'}}}
async function testR2(){connection.r2=null;try{await actions.run('test-r2',async()=>{const result=await api<{message?:string}>('/backup/r2/test',{method:'POST',body:JSON.stringify({endpoint:form.r2_endpoint||'',bucket:form.r2_bucket||'',access_key:form.r2_access_key||'',secret_key:form.r2_secret_key||''}),headers:{'Content-Type':'application/json'}});connection.r2={connected:true,detail:result.message||'读写校验通过'}})}catch(e){connection.r2={connected:false,detail:e instanceof Error?e.message:'连接失败'}}}
async function changePassword(){if(newPassword.value.length<8||newPassword.value!==confirmPassword.value){ui.toast('新密码至少 8 位，且两次输入必须一致','error');return}try{await actions.run('change-password',async()=>{await api('/session/change-password',{method:'POST',body:JSON.stringify({old_password:oldPassword.value,new_password:newPassword.value}),headers:{'Content-Type':'application/json'}});oldPassword.value='';newPassword.value='';confirmPassword.value='';ui.toast('密码修改成功');qc.invalidateQueries({queryKey:['audit-logs']})})}catch(e){ui.toast(e instanceof Error?e.message:'密码修改失败','error')}}
function addCurrentIPToAllowlist(){const ip=query.data.value?.request_ip?.trim();if(!ip)return;const entries=(form.auth_ip_allowlist||'').split(/[\s,;]+/).filter(Boolean);if(!entries.includes(ip))entries.push(ip);form.auth_ip_allowlist=entries.join('\n')}
async function updateAction(action:'check'|'apply'){try{await actions.runTask(`update-${action}`,()=>api<TaskAccepted>(`/settings/updater/${action}`,{method:'POST'}),action==='check'?'检查应用更新':'下载并应用更新','updater');ui.toast(action==='check'?'更新检查已经启动':'更新下载与应用已经启动')}catch(e){ui.toast(e instanceof Error?e.message:'更新操作失败','error')}}
const deploymentItems=computed(()=>{const d=maintenance.data.value?.deployment as Record<string,unknown>|undefined;return (d?.Items||d?.items||[]) as Array<Record<string,unknown>>})
const updater=computed(()=>(maintenance.data.value?.updater||{}) as Record<string,unknown>)
function selectedLibraryIDs(){
  try{const parsed=JSON.parse(form.jellyfin_library_ids||'[]');return Array.isArray(parsed)?parsed.map(String):[]}catch{return []}
}
function librarySelected(id:string){
  const selected=selectedLibraryIDs()
  return selected.length===0||selected.includes(id)
}
function toggleLibrary(id:string,checked:boolean){
  const all=(mediaLibraries.data.value?.items||[]).map(item=>item.id)
  const current=selectedLibraryIDs()
  const next=new Set(current.length?current:all)
  if(checked)next.add(id);else next.delete(id)
  form.jellyfin_library_ids=next.size===all.length?'[]':JSON.stringify([...next])
}
</script>

<template><div class="page-grid"><PageHeader eyebrow="PREFERENCES" title="系统设置" description="按任务分组配置下载、元数据、媒体服务、AI、外观、安全和应用维护。"><AsyncButton v-if="group.fields.length" class="btn btn-primary" :loading="actions.isBusy('save')" loading-label="正在保存…" @click="save"><Save :size="17"/>保存更改</AsyncButton></PageHeader><StateBlock v-if="query.isLoading.value" state="loading"/><StateBlock v-else-if="query.isError.value" state="error" title="设置加载失败" :retrying="query.isFetching.value" @retry="query.refetch()"/><section v-else class="grid min-w-0 gap-5 lg:grid-cols-[240px_minmax(0,1fr)] xl:grid-cols-[260px_minmax(0,1fr)]"><nav class="panel flex h-fit gap-2 overflow-x-auto p-2 lg:sticky lg:top-4 lg:block lg:overflow-visible lg:p-3" aria-label="设置分组"><button v-for="item in groups" :key="item.id" class="flex min-h-11 shrink-0 items-center gap-2.5 rounded-xl px-3 text-left text-sm font-bold lg:mb-1 lg:min-h-12 lg:w-full lg:gap-3" :class="active===item.id?'bg-[var(--brand-soft)] text-[var(--brand-strong)]':'muted hover:bg-[var(--surface-muted)]'" @click="active=item.id"><component :is="item.icon" class="shrink-0" :size="18"/>{{ item.label }}</button></nav><article class="panel min-w-0 overflow-hidden p-4 sm:p-6 xl:p-7"><div class="mb-6 flex items-center gap-3 border-b border-[var(--line)] pb-5"><span class="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-[var(--brand-soft)] text-[var(--brand)]"><component :is="group.icon" :size="21"/></span><div class="min-w-0"><p class="eyebrow">CONFIGURATION</p><h3 class="text-2xl font-black">{{ group.label }}</h3></div></div>
  <template v-if="group.id==='ai'">
    <AISettingsPanel :form="form" :configured="query.data.value?.configured||{}"/>
    <div class="panel-muted mt-6 flex items-start gap-3 p-4 text-sm leading-6 muted"><Settings2 class="mt-1 shrink-0 text-[var(--sky)]" :size="18"/>三家的 API Key 分别保存且不会回传浏览器。密码框留空会保留原值；切换当前服务商后，请点击页面顶部“保存更改”才会影响 AI 助手。</div>
    <section class="mt-8" data-testid="ai-tool-runs">
      <div class="flex flex-wrap items-start justify-between gap-3">
        <div>
          <h4 class="font-black">最近 AI 工具调用</h4>
          <p class="muted mt-1 text-sm leading-6">这里记录 AI 读取、生成提案和确认执行的内部工具。参数与结果已经脱敏，不展示 API Key、密码、Cookie 或认证头。</p>
        </div>
        <AsyncButton class="btn btn-quiet" :loading="aiToolRuns.isFetching.value" loading-label="刷新中…" @click="aiToolRuns.refetch()"><RefreshCw :size="15"/>刷新</AsyncButton>
      </div>
      <StateBlock v-if="aiToolRuns.isLoading.value" class="mt-4" state="loading" title="正在读取 AI 工具日志"/>
      <StateBlock v-else-if="aiToolRuns.isError.value" class="mt-4" state="error" title="AI 工具日志加载失败" :retrying="aiToolRuns.isFetching.value" @retry="aiToolRuns.refetch()"/>
      <div v-else-if="aiToolRuns.data.value?.items.length" class="mt-4 grid gap-3">
        <details v-for="run in aiToolRuns.data.value.items" :key="run.id" class="panel-muted overflow-hidden p-4">
          <summary class="flex cursor-pointer list-none flex-wrap items-center gap-2">
            <strong class="mr-auto break-all">{{ run.tool_name }}</strong>
            <span class="badge" :class="run.risk==='write'?'badge-danger':run.risk==='propose'?'badge-warning':''">{{ run.risk }}</span>
            <span class="badge" :class="run.outcome==='success'?'badge-success':'badge-danger'">{{ run.outcome }}</span>
            <span class="muted text-xs">{{ run.duration_ms }} ms · {{ new Date(run.created_at).toLocaleString() }}</span>
          </summary>
          <div class="mt-4 grid gap-3 border-t border-[var(--line)] pt-4 text-xs leading-5">
            <p><strong>模型：</strong><span class="muted">{{ [run.provider,run.model].filter(Boolean).join(' / ') || '内部工具' }}</span></p>
            <p v-if="run.proposal_id"><strong>提案：</strong><code>{{ run.proposal_id }}</code></p>
            <p v-if="run.confirmation_required"><strong>确认：</strong><span :class="run.confirmation_validated?'text-[var(--success)]':'text-[var(--warning)]'">{{ run.confirmation_validated ? '已通过一次性令牌' : '未通过确认' }}</span></p>
            <div><strong>脱敏参数摘要</strong><pre class="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-[var(--surface-solid)] p-3">{{ run.arguments_summary || '{}' }}</pre></div>
            <div v-if="run.result_summary"><strong>脱敏结果摘要</strong><pre class="mt-1 max-h-40 overflow-auto whitespace-pre-wrap break-all rounded-xl bg-[var(--surface-solid)] p-3">{{ run.result_summary }}</pre></div>
          </div>
        </details>
      </div>
      <p v-else class="panel-muted mt-4 p-4 text-sm muted">还没有 AI 工具调用记录。只有用户主动发起 AI 分析或助手调用工具时才会产生记录。</p>
    </section>
  </template>
  <template v-else-if="group.fields.length">
    <div v-if="group.id==='media'" class="grid gap-5">
      <section v-for="app in mediaApps" :key="app.id" class="panel-muted overflow-hidden p-4 sm:p-5" :data-testid="`media-app-${app.id}`">
        <header class="flex items-start gap-3 border-b border-[var(--line)] pb-4">
          <span class="grid h-11 w-11 shrink-0 place-items-center rounded-xl bg-[var(--surface-solid)] text-[var(--sky)] shadow-sm"><component :is="app.icon" :size="20"/></span>
          <div class="min-w-0"><p class="eyebrow">{{ app.eyebrow }}</p><h4 class="mt-0.5 text-lg font-black">{{ app.title }}</h4><p class="muted mt-1 text-sm leading-5">{{ app.description }}</p></div>
        </header>
        <div class="mt-5 grid gap-5 md:grid-cols-2"><label v-for="field in app.fields" :key="field.key" class="label">{{ field.label }}<input v-model="form[field.key]" class="field" :type="isSecret(field)?'password':'text'" :autocomplete="isSecret(field)?'new-password':'off'" :data-1p-ignore="isSecret(field)?'true':undefined" :placeholder="fieldPlaceholder(field)"/><span v-if="field.description" class="text-xs font-normal leading-5 muted">{{ field.description }}</span><span v-if="isSecret(field)&&query.data.value?.configured[field.key]" class="flex items-center gap-1 text-xs font-normal text-[var(--success)]"><KeyRound :size="12"/>凭据已安全保存</span></label></div>
        <div v-if="app.id==='jellyfin'" class="mt-5 rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-4 text-sm leading-6" data-testid="jellyfin-playback-help">
          <div class="flex items-start gap-3"><Network class="mt-1 shrink-0 text-[var(--sky)]" :size="18"/><div><strong>播放线路</strong><p class="muted mt-1">播放页面的视频下方可以选择 AnimateTool 代理或 Jellyfin 直连。选择会持续作用于当前浏览器，线路不可用时不会自动改用另一条线路。</p></div></div>
        </div>
        <section v-if="app.id==='jellyfin'" class="mt-5 rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-4" data-testid="jellyfin-library-selection">
          <div class="flex flex-wrap items-start justify-between gap-3"><div><strong>媒体模式显示的 Jellyfin 媒体库</strong><p class="muted mt-1 text-xs leading-5">没有单独选择时默认展示全部影视库。取消勾选可以从 AnimateTool 媒体模式中隐藏某个库，不会修改 Jellyfin 本身。</p></div><AsyncButton class="btn btn-secondary" :loading="mediaLibraries.isFetching.value" loading-label="刷新中…" @click="mediaLibraries.refetch()"><RefreshCw :size="15"/>刷新媒体库</AsyncButton></div>
          <StateBlock v-if="mediaLibraries.isLoading.value" class="mt-3" state="loading" title="正在读取 Jellyfin 媒体库"/>
          <div v-else-if="mediaLibraries.data.value?.items.length" class="mt-3 grid gap-2 sm:grid-cols-2">
            <label v-for="library in mediaLibraries.data.value.items" :key="library.id" class="panel-muted flex min-h-14 items-center gap-3 px-3 text-sm font-bold"><input type="checkbox" class="h-5 w-5 accent-[var(--brand)]" :checked="librarySelected(library.id)" @change="toggleLibrary(library.id,($event.target as HTMLInputElement).checked)"/><span class="min-w-0"><span class="block truncate">{{ library.name }}</span><small class="muted">{{ library.collection_type||'媒体库' }}</small></span></label>
          </div>
          <p v-else class="muted mt-3 text-xs">保存 Jellyfin 配置并确认连接成功后，即可刷新媒体库列表。</p>
        </section>
        <AsyncButton v-if="app.provider" class="mt-4 min-h-16 w-full rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-3 text-left" :loading="actions.isBusy(`provider-${app.provider}`)" loading-label="连接测试中…" @click="testProvider(app.provider)"><div class="flex min-w-0 items-start justify-between gap-3"><strong class="min-w-0 break-words">{{ app.provider==='jellyfin' ? `测试当前线路：${playbackSourceLabel}` : `测试 ${app.title} 连接` }}</strong><RefreshCw :size="15" class="mt-1 shrink-0 text-[var(--sky)]"/></div><p class="mt-1 break-words text-xs" :class="connection[app.provider]?.connected?'text-[var(--success)]':'muted'">{{ connection[app.provider]?(connection[app.provider]?.connected?`已连接${connection[app.provider]?.source_label ? ` · ${connection[app.provider]?.source_label}` : ''}${connection[app.provider]?.account ? ` · ${connection[app.provider]?.account}` : ''}`:connection[app.provider]?.detail):app.provider==='jellyfin'?`当前线路：${playbackSourceLabel}`:'点击测试当前已保存配置' }}</p></AsyncButton>
      </section>
      <section class="panel-muted border border-dashed border-[var(--line)] p-5">
        <div class="flex items-center gap-3"><span class="grid h-11 w-11 place-items-center rounded-xl bg-[var(--surface-solid)] text-[var(--ink-muted)]"><Film :size="20"/></span><div><p class="eyebrow">MEDIA PROVIDERS</p><h4 class="font-black">添加其他媒体提供商</h4><p class="muted mt-1 text-sm">提供商接口已经预留；Plex、Emby 等适配器将在后续版本加入。</p></div></div>
      </section>
    </div>
    <div v-else class="grid gap-5 md:grid-cols-2"><label v-for="field in group.fields" :key="field.key" class="label" :class="field.type==='boolean'?'panel-muted flex min-h-14 grid-cols-[1fr_auto] items-center px-4':''">{{ field.label }}<select v-if="field.type==='select'" v-model="form[field.key]" class="field"><option v-for="option in field.options" :key="option.value" :value="option.value">{{ option.label }}</option></select><input v-else-if="field.type==='boolean'" :checked="form[field.key]==='true'" type="checkbox" class="h-5 w-5 accent-[var(--brand)]" @change="form[field.key]=($event.target as HTMLInputElement).checked?'true':'false'"/><input v-else v-model="form[field.key]" class="field" :type="isSecret(field)?'password':'text'" :autocomplete="isSecret(field)?'new-password':'off'" :data-1p-ignore="isSecret(field)?'true':undefined" :placeholder="fieldPlaceholder(field)"/><span v-if="isSecret(field)&&query.data.value?.configured[field.key]" class="flex items-center gap-1 text-xs font-normal text-[var(--success)]"><KeyRound :size="12"/>凭据已安全保存</span></label></div>
    <div v-if="group.id==='network'" class="panel-muted mt-6 flex items-start gap-3 p-4 text-sm leading-6 muted"><Network class="mt-1 shrink-0 text-[var(--sky)]" :size="18"/>只填主机和端口时会自动按 HTTP 代理保存。代理运行在另一台设备时，请填写其局域网地址并在代理软件中允许局域网连接；每项开关只影响对应服务。</div>
    <div v-if="group.id==='downloader'" class="panel-muted mt-6 p-4 text-sm leading-6 muted" data-testid="auto-rename-help"><strong class="block text-[var(--text)]">Jellyfin / Plex 兼容的默认整理方式</strong><p class="mt-1">系统会通过 qBittorrent 移动并改名，做种不会中断。默认生成 <code>媒体根目录/系列名/Season 01/系列名 - S01E01.mkv</code>，不同季度归入同一系列目录。系列名优先采用已匹配的规范元数据，不使用字幕组发布名。</p><p class="mt-2">可用变量：<code>{title}</code>、<code>{season}</code>、<code>{episode}</code>、<code>{year}</code>、<code>{original}</code>、<code>{ext}</code>。如需区分同名重制版，可自行在系列模板加入 <code>({year})</code>。多文件合集不会自动猜测剧集，以免误改。</p></div>
    <div class="panel-muted mt-6 flex items-start gap-3 p-4 text-sm leading-6 muted"><Settings2 class="mt-1 shrink-0 text-[var(--sky)]" :size="18"/>敏感字段不会从服务器回传。密码框留空时保留原值，填写新值时才会覆盖。保存后，系统配置也会同步到本地 config.yaml；该文件可能包含服务密钥，请勿分享。</div>
    <AsyncButton v-if="group.id==='network'" class="panel-muted mt-6 min-h-20 w-full p-3 text-left" :loading="actions.isBusy('test-proxy')" loading-label="代理测试中…" @click="testProxy"><div class="flex items-center justify-between"><strong>测试当前代理地址</strong><RefreshCw :size="15" class="text-[var(--sky)]"/></div><p class="mt-2 text-xs" :class="connection.proxy?.connected?'text-[var(--success)]':'muted'">{{ connection.proxy?(connection.proxy.connected?'代理连接成功':connection.proxy.detail):'使用当前输入值访问 Bangumi 测试目标，无需先保存' }}</p></AsyncButton>
    <div v-if="group.providers?.length&&group.id!=='media'" class="mt-6"><h4 class="font-black">连接状态</h4><div class="mt-3 grid gap-3 sm:grid-cols-2"><AsyncButton v-for="provider in group.providers" :key="provider" class="panel-muted min-h-20 p-3 text-left" :loading="actions.isBusy(`provider-${provider}`)" loading-label="连接测试中…" @click="testProvider(provider)"><div class="flex items-center justify-between"><strong class="uppercase">{{ provider }}</strong><RefreshCw :size="15" class="text-[var(--sky)]"/></div><p class="mt-2 text-xs" :class="connection[provider]?.connected?'text-[var(--success)]':'muted'">{{ connection[provider]?(connection[provider]?.connected?`已连接 ${connection[provider]?.account||''}`:connection[provider]?.detail):'点击测试当前已保存配置' }}</p></AsyncButton></div></div>
    <AsyncButton v-if="group.id==='cloud'" class="panel-muted mt-6 min-h-20 w-full p-3 text-left" :loading="actions.isBusy('test-r2')" loading-label="R2 测试中…" @click="testR2"><div class="flex items-center justify-between"><strong>R2 读写连通性</strong><RefreshCw :size="15" class="text-[var(--sky)]"/></div><p class="mt-2 text-xs" :class="connection.r2?.connected?'text-[var(--success)]':'muted'">{{ connection.r2?.detail||'点击测试表单中的配置；空白凭据沿用已保存值' }}</p></AsyncButton>
  </template>
  <template v-else-if="group.id==='appearance'"><section class="max-w-2xl"><h4 class="font-black">外观与辅助功能</h4><p class="muted mt-1 text-sm leading-6">调整当前浏览器的主题与页面背景，不影响其他设备。</p><div class="mt-5 grid gap-4"><label class="label">主题模式<select class="field" :value="ui.theme" @change="ui.setTheme(($event.target as HTMLSelectElement).value as ThemeMode)"><option value="system">跟随系统</option><option value="light">亮色</option><option value="dark">深色</option></select></label><label class="label">页面背景<select class="field" data-testid="background-mode" :value="ui.backgroundMode" @change="ui.setBackgroundMode(($event.target as HTMLSelectElement).value as BackgroundMode)"><option value="default">默认渐变背景</option><option value="anime">随机动漫海报</option></select></label><div v-if="ui.backgroundMode==='anime'" class="rounded-xl border border-[var(--line)] bg-[var(--brand-soft)] p-4 text-sm leading-6 text-[var(--brand-strong)]"><strong class="block">已启用动漫海报背景</strong><span>从番剧图鉴随机选择，并自动为手机、平板和电脑加载 640、960、1280px 的合适尺寸；页面切换不会重复下载。</span></div></div></section></template>
  <template v-else-if="group.id==='security'">
    <div class="grid gap-6 xl:grid-cols-2">
      <section class="panel-muted p-4">
        <ShieldCheck class="text-[var(--success)]"/>
        <h4 class="mt-3 font-extrabold">浏览器安全策略已启用</h4>
        <p class="muted mt-1 text-sm leading-6">会话 Cookie、同源写保护、本机恢复限制、IP 白名单与审计记录由服务器统一执行。</p>
      </section>
      <section>
        <h4 class="font-black">修改管理员密码</h4>
        <div class="mt-4 grid gap-3">
          <input v-model="oldPassword" class="field" type="password" placeholder="当前密码"/>
          <input v-model="newPassword" class="field" type="password" placeholder="新密码（至少 8 位）"/>
          <input v-model="confirmPassword" class="field" type="password" placeholder="再次输入新密码"/>
          <AsyncButton class="btn btn-primary" :loading="actions.isBusy('change-password')" loading-label="修改中…" @click="changePassword"><KeyRound :size="16"/>修改密码</AsyncButton>
          <LocalRecoveryLink label="打开本机恢复" link-class="btn btn-secondary"/>
        </div>
      </section>
    </div>
    <section class="panel-muted mt-8 p-4 sm:p-5" data-testid="auth-ip-allowlist">
      <div class="flex flex-wrap items-start justify-between gap-4">
        <div class="max-w-2xl">
          <h4 class="font-black">IP 白名单免密访问</h4>
          <p class="muted mt-1 text-sm leading-6">命中的设备无需输入管理员密码，但写操作仍受同源保护。支持单个 IPv4、IPv6 与 CIDR 网段，每行一项。</p>
        </div>
        <label class="flex min-h-11 items-center gap-3 rounded-xl bg-[var(--surface-solid)] px-4 font-bold">
          <span>启用免密</span>
          <input
            :checked="form.auth_ip_allowlist_enabled==='true'"
            type="checkbox"
            class="h-5 w-5 accent-[var(--brand)]"
            @change="form.auth_ip_allowlist_enabled=($event.target as HTMLInputElement).checked?'true':'false'"
          />
        </label>
      </div>
      <label class="label mt-5">
        允许的 IP 或网段
        <textarea
          v-model="form.auth_ip_allowlist"
          class="field min-h-36 font-mono text-sm"
          placeholder="192.168.1.20&#10;192.168.1.0/24&#10;100.64.0.0/10"
          spellcheck="false"
        />
      </label>
      <div v-if="query.data.value?.request_ip" class="mt-3 flex flex-wrap items-center justify-between gap-3 text-sm">
        <span class="muted">服务器识别当前来源为 <code>{{ query.data.value.request_ip }}</code></span>
        <button type="button" class="btn btn-secondary" @click="addCurrentIPToAllowlist">填入当前 IP</button>
      </div>
      <div class="mt-4 flex items-start gap-3 rounded-xl border border-[var(--line)] bg-[var(--surface-solid)] p-4 text-sm leading-6 muted">
        <Network class="mt-1 shrink-0 text-[var(--warning)]" :size="18"/>
        <span>反向代理部署只会采信 <code>X-Forwarded-For</code> 中由 <code>server.trusted_proxies</code> 转发的来源地址。请勿填写 <code>0.0.0.0/0</code> 或 <code>::/0</code>；系统也会拒绝保存这类全网规则。</span>
      </div>
      <div class="mt-5 flex justify-end">
        <AsyncButton class="btn btn-primary" :loading="actions.isBusy('save')" loading-label="正在保存…" @click="save"><Save :size="17"/>保存免密设置</AsyncButton>
      </div>
    </section>
    <section class="mt-8">
      <div class="flex items-center justify-between">
        <h4 class="font-black">最近安全审计</h4>
        <AsyncButton class="btn btn-quiet" :loading="audits.isFetching.value" loading-label="刷新中…" @click="audits.refetch()"><RefreshCw :size="15"/>刷新</AsyncButton>
      </div>
      <div class="mt-3 overflow-x-auto">
        <table class="w-full min-w-[680px] text-left text-sm">
          <thead class="muted"><tr><th class="p-3">时间</th><th class="p-3">账户</th><th class="p-3">操作</th><th class="p-3">结果</th><th class="p-3">来源</th></tr></thead>
          <tbody><tr v-for="entry in audits.data.value?.items||[]" :key="entry.id" class="border-t border-[var(--line)]"><td class="p-3">{{ new Date(entry.created_at).toLocaleString() }}</td><td class="p-3">{{ entry.username||'系统' }}</td><td class="p-3 font-bold">{{ entry.action }}</td><td class="p-3"><span class="badge" :class="entry.outcome==='success'?'badge-success':'badge-danger'">{{ entry.outcome }}</span></td><td class="p-3 muted">{{ entry.ip }}</td></tr></tbody>
        </table>
      </div>
    </section>
  </template>
  <template v-else><section class="panel-muted p-4"><h4 class="font-black">版本更新</h4><p class="muted mt-2 text-sm">{{ updater.LastMessage||updater.last_message||'更新服务尚未运行' }}</p><div class="mt-3 flex flex-wrap gap-2"><span class="badge">当前 {{ updater.CurrentVersion||updater.current_version||'未知' }}</span><span v-if="updater.LatestVersion||updater.latest_version" class="badge badge-success">最新 {{ updater.LatestVersion||updater.latest_version }}</span></div><div class="mt-4 flex flex-wrap gap-2"><AsyncButton class="btn btn-secondary" :loading="actions.isBusy('update-check','repo-update-check')" loading-label="检查中…" @click="updateAction('check')"><RefreshCw :size="16"/>立即检查</AsyncButton><AsyncButton class="btn btn-primary" :disabled="!(updater.HasUpdate||updater.has_update)" :loading="actions.isBusy('update-apply','repo-update-apply')" loading-label="更新中…" @click="updateAction('apply')">下载并应用更新</AsyncButton></div></section><section class="mt-6"><h4 class="font-black">部署检查</h4><div class="mt-3 grid gap-3"><article v-for="(item,i) in deploymentItems" :key="i" class="panel-muted p-4"><div class="flex items-center justify-between gap-3"><strong>{{ item.Name||item.name }}</strong><span class="badge" :class="(item.Status||item.status)==='pass'?'badge-success':(item.Status||item.status)==='fail'?'badge-danger':'badge-warning'">{{ item.Status||item.status }}</span></div><p class="muted mt-2 text-sm">{{ item.Summary||item.summary }}</p><p v-if="item.Action||item.action" class="mt-2 text-sm text-[var(--warning)]">{{ item.Action||item.action }}</p></article></div></section></template>
  </article></section></div></template>
