import { reactive } from 'vue'
import type { TaskAccepted } from '../api/types'
import { useTaskStore } from '../stores/tasks'

export function useAsyncActions() {
  const pending = reactive(new Set<string>())
  const taskIDs = reactive(new Map<string, string>())
  const running = new Map<string, Promise<unknown>>()
  const tasks = useTaskStore()

  function isBusy(key: string, knownTaskID = '') {
    const taskID = knownTaskID || taskIDs.get(key) || ''
    return pending.has(key) || Boolean(taskID && tasks.isRunning(taskID))
  }

  function run<T>(key: string, action: () => Promise<T>): Promise<T> {
    const existing = running.get(key) as Promise<T> | undefined
    if (existing) return existing
    pending.add(key)
    const promise = Promise.resolve().then(action).finally(() => {
      pending.delete(key)
      running.delete(key)
    })
    running.set(key, promise)
    return promise
  }

  async function runTask(key: string, action: () => Promise<TaskAccepted>, title: string, kind = 'task', detail = '任务已经启动') {
    return run(key, async () => {
      const accepted = await action()
      taskIDs.set(key, accepted.task_id)
      tasks.track(accepted, title, kind, detail)
      return accepted
    })
  }

  return { isBusy, run, runTask }
}
