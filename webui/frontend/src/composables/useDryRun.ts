// Dry Run 请求阶段与本次预览；completed 仅表示请求完成，不代表全部目标正常。
import { ref, computed } from 'vue'
import { request } from '../api'
import type { DryRunResponse } from '../types'

export function useDryRun() {
  const status = ref<'idle' | 'running' | 'completed' | 'failed'>('idle')
  const loading = computed(() => status.value === 'running')
  const results = ref<DryRunResponse['results']>([])
  const warnings = ref<string[]>([])
  const error = ref('')
  const lastFinishedAt = ref<Date | null>(null)

  // 每次执行清空旧预览；失败状态持久展示，并抛错供页面通知。
  async function run() {
    status.value = 'running'
    error.value = ''
    results.value = []
    warnings.value = []
    try {
      const data = await request<DryRunResponse>('/api/sync/dryrun', { method: 'POST' })
      results.value = data.results || []
      warnings.value = data.warnings || []
      lastFinishedAt.value = new Date()
      status.value = 'completed'
    } catch (e: unknown) {
      error.value = e instanceof Error && e.message ? e.message : '模拟测试请求失败'
      lastFinishedAt.value = new Date()
      status.value = 'failed'
      throw e
    }
  }

  return { status, loading, results, warnings, error, lastFinishedAt, run }
}
