import { defineStore } from 'pinia'

export interface LiveTask { id: string; title: string; detail: string; current?: number; total?: number; tone: 'running' | 'success' | 'error' }

export const useTaskStore = defineStore('tasks', {
  state: () => ({ connected: false, tasks: [] as LiveTask[], source: null as EventSource | null }),
  getters: { runningCount: s => s.tasks.filter(t => t.tone === 'running').length },
  actions: {
    connect() {
      if (this.source) return
      const source = new EventSource('/api/v1/events')
      this.source = source
      source.onopen = () => this.connected = true
      source.onerror = () => this.connected = false
      const bind = (name: string, title: string) => source.addEventListener(name, event => {
        const data = JSON.parse((event as MessageEvent).data || '{}')
        const status = data.status || data.type
        const tone = status === 'error' ? 'error' : ['complete', 'completed', 'success', 'resolved'].includes(status) ? 'success' : 'running'
        this.upsert({ id: name, title, detail: data.summary || data.message || data.title || data.dir || '任务状态已更新', current: data.current, total: data.total, tone })
      })
      bind('scan_progress', '本地扫描'); bind('scan_run', '本地扫描'); bind('scan_complete', '本地扫描')
      bind('metadata_updated', '元数据刷新'); bind('subscription_run', '订阅检查'); bind('scheduler_run', '自动调度')
      bind('download_progress', '下载任务'); bind('download_ready', '下载完成'); bind('library_issue', '媒体库诊断')
    },
    disconnect() {
      this.source?.close()
      this.source = null
      this.connected = false
    },
    upsert(task: LiveTask) {
      const i = this.tasks.findIndex(t => t.id === task.id)
      if (i >= 0) this.tasks[i] = task; else this.tasks.unshift(task)
      this.tasks = this.tasks.slice(0, 12)
    },
  },
})
