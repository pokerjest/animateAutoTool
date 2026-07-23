import { defineStore } from 'pinia'
import { api } from '../api/client'
import type { TaskAccepted, TaskUpdate } from '../api/types'

export interface LiveTask {
  id: string
  kind?: string
  title: string
  detail: string
  current?: number
  total?: number
  tone: 'running' | 'success' | 'error'
  updatedAt?: string
}

let refreshTimer: ReturnType<typeof setInterval> | null = null

function toLiveTask(task: TaskUpdate): LiveTask {
  return {
    id: task.task_id,
    kind: task.kind,
    title: task.title,
    detail: task.message,
    current: task.current,
    total: task.total,
    tone: task.status === 'error' ? 'error' : task.status === 'completed' ? 'success' : 'running',
    updatedAt: task.updated_at,
  }
}

export const useTaskStore = defineStore('tasks', {
  state: () => ({
    connected: false,
    tasks: [] as LiveTask[],
    source: null as EventSource | null,
    revision: 0,
    lastTransition: null as { task: LiveTask; previousTone?: LiveTask['tone'] } | null,
    transitions: [] as Array<{ task: LiveTask; previousTone?: LiveTask['tone'] }>,
  }),
  getters: {
    runningCount: state => state.tasks.filter(task => task.tone === 'running').length,
    isRunning: state => (taskID: string) => state.tasks.some(task => task.id === taskID && task.tone === 'running'),
    taskByID: state => (taskID: string) => state.tasks.find(task => task.id === taskID),
  },
  actions: {
    async hydrate() {
      try {
        const result = await api<{ items: TaskUpdate[] }>('/tasks')
        for (const task of result.items || []) this.upsert(toLiveTask(task))
      } catch {
        // Session changes and brief reconnect gaps are handled by the next poll.
      }
    },
    connect() {
      if (this.source) return
      void this.hydrate()
      const source = new EventSource('/api/v1/events')
      this.source = source
      source.onopen = () => { this.connected = true; void this.hydrate() }
      source.onerror = () => { this.connected = false }
      source.addEventListener('task_update', event => {
        const task = JSON.parse((event as MessageEvent).data || '{}') as TaskUpdate
        if (task.task_id) this.upsert(toLiveTask(task))
      })
      const bindLegacy = (name: string, title: string) => source.addEventListener(name, event => {
        const data = JSON.parse((event as MessageEvent).data || '{}')
        const status = data.status || data.type
        const tone = status === 'error' ? 'error' : ['complete', 'completed', 'success', 'resolved'].includes(status) ? 'success' : 'running'
        this.upsert({ id: `legacy:${name}`, title, detail: data.summary || data.message || data.title || data.dir || '任务状态已更新', current: data.current, total: data.total, tone })
      })
      bindLegacy('scan_progress', '本地扫描'); bindLegacy('scan_run', '本地扫描'); bindLegacy('scan_complete', '本地扫描')
      bindLegacy('metadata_updated', '元数据刷新'); bindLegacy('subscription_run', '订阅检查'); bindLegacy('scheduler_run', '自动调度')
      bindLegacy('download_progress', '下载任务'); bindLegacy('download_ready', '下载完成'); bindLegacy('library_issue', '媒体库诊断')
      if (!refreshTimer) refreshTimer = setInterval(() => { if (this.runningCount || !this.connected) void this.hydrate() }, 3000)
    },
    disconnect() {
      this.source?.close()
      this.source = null
      this.connected = false
      if (refreshTimer) clearInterval(refreshTimer)
      refreshTimer = null
    },
    track(task: TaskAccepted, title: string, kind = 'task', detail = '任务已经启动') {
      const existing = this.taskByID(task.task_id)
      if (existing && existing.tone !== 'running') return
      this.upsert({ id: task.task_id, kind, title, detail, tone: 'running', updatedAt: new Date().toISOString() })
    },
    consumeTransitions() {
      return this.transitions.splice(0)
    },
    upsert(task: LiveTask) {
      const index = this.tasks.findIndex(item => item.id === task.id)
      const previousTone = index >= 0 ? this.tasks[index].tone : undefined
      if (index >= 0) this.tasks.splice(index, 1)
      this.tasks.unshift(task)
      this.tasks.sort((left, right) => {
        const leftTime = left.updatedAt ? Date.parse(left.updatedAt) : 0
        const rightTime = right.updatedAt ? Date.parse(right.updatedAt) : 0
        return rightTime - leftTime
      })
      this.tasks = this.tasks.slice(0, 50)
      this.lastTransition = { task, previousTone }
      this.transitions.push({ task, previousTone })
      this.revision += 1
    },
  },
})
