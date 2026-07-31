import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest'
import { createPinia, setActivePinia } from 'pinia'
import { useTaskStore } from './tasks'

class FakeEventSource {
  static instances: FakeEventSource[] = []
  onopen: (() => void) | null = null
  onerror: (() => void) | null = null
  closed = false
  listeners = new Map<string, Array<(event: MessageEvent) => void>>()
  constructor(public url: string) { FakeEventSource.instances.push(this) }
  addEventListener(name: string, listener: EventListener) {
    const listeners = this.listeners.get(name) || []
    listeners.push(listener as (event: MessageEvent) => void)
    this.listeners.set(name, listeners)
  }
  emit(name: string, data: unknown) {
    for (const listener of this.listeners.get(name) || []) listener({ data: JSON.stringify(data) } as MessageEvent)
  }
  close() { this.closed = true }
}

const response = (data: unknown) => Promise.resolve(new Response(JSON.stringify({ data }), { status: 200, headers: { 'Content-Type': 'application/json' } }))

describe('task stream store', () => {
  beforeEach(() => {
    setActivePinia(createPinia())
    FakeEventSource.instances = []
    vi.stubGlobal('EventSource', FakeEventSource)
    vi.stubGlobal('fetch', vi.fn(() => response({ items: [] })))
  })

  afterEach(() => vi.unstubAllGlobals())

  it('keeps one authenticated stream and closes it on logout', () => {
    const store = useTaskStore()
    store.connect()
    store.connect()
    expect(FakeEventSource.instances).toHaveLength(1)
    expect(FakeEventSource.instances[0].url).toBe('/api/v1/events')
    store.disconnect()
    expect(FakeEventSource.instances[0].closed).toBe(true)
    expect(store.source).toBeNull()
  })

  it('restores task snapshots and applies typed completion events', async () => {
    vi.stubGlobal('fetch', vi.fn(() => response({ items: [{
      task_id: 'local-scan', kind: 'scan', title: '本地扫描', status: 'running', message: '正在扫描', current: 1, total: 3, updated_at: '2026-07-23T00:00:00Z',
    }] })))
    const store = useTaskStore()
    store.connect()

    await vi.waitFor(() => expect(store.isRunning('local-scan')).toBe(true))
    FakeEventSource.instances[0].emit('task_update', {
      task_id: 'local-scan', kind: 'scan', title: '本地扫描', status: 'completed', message: '扫描完成', current: 3, total: 3, updated_at: '2026-07-23T00:00:03Z',
    })

    expect(store.isRunning('local-scan')).toBe(false)
    expect(store.taskByID('local-scan')?.tone).toBe('success')
    expect(store.taskByID('local-scan')?.detail).toBe('扫描完成')
    expect(store.lastTransition?.previousTone).toBe('running')
    store.disconnect()
  })

  it('does not overwrite an already completed task with a late accepted response', () => {
    const store = useTaskStore()
    store.upsert({ id: 'fast-task', kind: 'sync', title: '同步', detail: '已完成', tone: 'success', updatedAt: '2026-07-23T00:00:01Z' })
    store.track({ task_id: 'fast-task', status: 'running' }, '同步', 'sync')
    expect(store.taskByID('fast-task')?.tone).toBe('success')
  })

  it('lets the first server update replace an accepted-response placeholder', () => {
    const store = useTaskStore()
    store.track({ task_id: 'local-scan', status: 'running' }, '本地扫描', 'scan', '正在启动扫描')
    store.upsert({ id: 'local-scan', kind: 'scan', phase: 'metadata', title: '本地扫描', detail: '第 2/2 阶段 · 正在整理元数据', current: 1, total: 8, tone: 'running', updatedAt: '2026-07-31T06:00:03Z' })

    expect(store.taskByID('local-scan')?.phase).toBe('metadata')
    expect(store.taskByID('local-scan')?.detail).toContain('第 2/2 阶段')
  })

  it('ignores stale task updates that arrive after a newer phase', () => {
    const store = useTaskStore()
    store.upsert({ id: 'local-scan', kind: 'scan', phase: 'metadata', title: '本地扫描', detail: '第 2/2 阶段 · 正在整理元数据', current: 1, total: 10, tone: 'running', updatedAt: '2026-07-31T06:00:02Z' })
    store.upsert({ id: 'local-scan', kind: 'scan', phase: 'scan', title: '本地扫描', detail: '第 1/2 阶段 · 正在扫描文件夹', current: 465, total: 465, tone: 'running', updatedAt: '2026-07-31T06:00:01Z' })

    expect(store.taskByID('local-scan')?.phase).toBe('metadata')
    expect(store.taskByID('local-scan')?.detail).toContain('第 2/2 阶段')
    expect(store.taskByID('local-scan')?.current).toBe(1)
  })

  it('coalesces legacy scan events and removes them when the typed task arrives', () => {
    const store = useTaskStore()
    store.connect()
    const source = FakeEventSource.instances[0]

    source.emit('scan_progress', { type: 'progress', dir: '/media/anime', current: 2, total: 4 })
    source.emit('scan_run', { IsRunning: true, LastSummary: '正在扫描 1 个目录' })
    expect(store.tasks.filter(task => task.id === 'legacy:scan')).toHaveLength(1)

    source.emit('scan_complete', { scope: 'run', summary: '扫描完成' })
    expect(store.taskByID('legacy:scan')?.tone).toBe('success')

    source.emit('task_update', {
      task_id: 'local-scan', kind: 'scan', phase: 'metadata', title: '本地扫描', status: 'running', message: '第 2/2 阶段 · 正在整理元数据', current: 1, total: 8, updated_at: '2026-07-31T06:00:03Z',
    })
    expect(store.taskByID('legacy:scan')).toBeUndefined()
    expect(store.tasks.filter(task => task.id === 'local-scan')).toHaveLength(1)
    store.disconnect()
  })
})
