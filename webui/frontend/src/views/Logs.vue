<script setup lang="ts">
// 同步日志页：历史记录（最顶部）+ 实时运行日志（默认展开）
// 历史记录：failed / skipped / partial 可查看同步详情；支持清空（DELETE /api/sync/logs）
import { NDataTable, NTag, NModal, NButton, NSpace, useMessage } from 'naive-ui'
import { ref, onMounted, onUnmounted, h } from 'vue'
import { request } from '../api'
import type { SyncLogEntry } from '../types'

const logs = ref<SyncLogEntry[]>([])
const logLines = ref<string[]>([])
const message = useMessage()

// failed / skipped / partial 共用中性的同步详情弹窗
const showDetailModal = ref(false)
const detailLog = ref<SyncLogEntry | null>(null)

let logEs: EventSource | null = null
let active = false
const streamStatus = ref('连接中')
const streamNotice = ref('')
let cursor: { epoch: string; seq: bigint } | null = null
// 游标用 BigInt 比较；实例标识长度允许标准库未来增长。
function parseCursor(id: string) {
  const match = /^([A-Z2-7]{26,}):(0|[1-9][0-9]{0,19})$/.exec(id)
  if (!match) return null
  const seq = BigInt(match[2])
  return seq <= 18446744073709551615n ? { epoch: match[1], seq } : null
}
// reset 同时重建显示窗口和去重基准，原因提示不混入日志正文。
function resetStream(e: Event) {
  if (!active) return
  const event = e as MessageEvent<string>
  const baseline = parseCursor(event.lastEventId)
  const notices: Record<string, string> = {
    initial: '',
    instance_changed: '服务已重启，显示当前进程最近日志',
    history_expired: '断线期间部分日志已超出缓存，显示最近日志',
    invalid_cursor: '日志流已重新建立，显示最近日志',
  }
  if (!baseline || !Object.prototype.hasOwnProperty.call(notices, event.data)) {
    streamStatus.value = '日志流格式异常'
    return
  }
  logLines.value = []
  cursor = baseline
  streamNotice.value = notices[event.data]
}
function receiveLog(e: MessageEvent<string>) {
  if (!active) return
  const next = parseCursor(e.lastEventId)
  if (!next || next.seq === 0n || !cursor || next.epoch !== cursor.epoch) {
    streamStatus.value = '日志流格式异常'
    return
  }
  if (next.seq <= cursor.seq) return
  if (next.seq !== cursor.seq + 1n) streamNotice.value = '部分运行日志未接收，当前显示可能不连续'
  logLines.value.push(e.data)
  if (logLines.value.length > 1000) logLines.value.shift()
  cursor = next
}

// ─── 历史记录加载（挂载与刷新按钮共用） ───
async function loadLogs() {
  try {
    const result = await request<SyncLogEntry[]>('/api/sync/logs')
    if (active) logs.value = result
  } catch (e: any) {
    if (active) message.error(`刷新失败: ${e.message}`)
  }
}

// ─── 时间格式化（本地时区，自动检测） ───
function formatTime(ts: string): string {
  if (!ts) return '-'
  const d = new Date(ts)
  if (isNaN(d.getTime())) return ts
  const pad = (n: number) => String(n).padStart(2, '0')
  const offset = -d.getTimezoneOffset()
  const sign = offset >= 0 ? '+' : '-'
  const tzStr = `UTC${sign}${pad(Math.floor(Math.abs(offset) / 60))}:${pad(Math.abs(offset) % 60)}`
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}:${pad(d.getSeconds())} ${tzStr}`
}

// ─── 生命周期 ───
// 日志连接不等待历史请求，卸载后晚到响应不再发布。
onMounted(() => {
  active = true
  logEs = new EventSource('/api/logs/stream')
  logEs.addEventListener('reset', resetStream)
  logEs.onmessage = receiveLog
  logEs.onopen = () => {
    if (active) streamStatus.value = '已连接'
  }
  logEs.onerror = () => {
    if (active && logEs) {
      streamStatus.value = logEs.readyState === EventSource.CLOSED ? '连接已关闭' : '重连中'
    }
  }
  void loadLogs()
})
onUnmounted(() => {
  active = false
  if (logEs) {
    logEs.removeEventListener('reset', resetStream)
    logEs.onmessage = null
    logEs.onopen = null
    logEs.onerror = null
    logEs.close()
    logEs = null
  }
})

// ─── 历史记录 ───
function openDetail(row: SyncLogEntry) {
  detailLog.value = row
  showDetailModal.value = true
}

// 历史记录清空确认（卡片式弹窗）
const showClearConfirm = ref(false)

async function clearLogs() {
  showClearConfirm.value = false
  try {
    await request('/api/sync/logs', { method: 'DELETE' })
    logs.value = []
    message.success('历史记录已清空')
  } catch (e: any) {
    message.error(`清空失败: ${e.message}`)
  }
}

const columns = [
  { title: '时间', key: 'timestamp', render: (row: any) => formatTime(row.timestamp) },
  { title: '目标', key: 'target' },
  { title: '域名', key: 'domain' },
  {
    title: '结果', key: 'result',
    render(row: any) {
      const failed = row.result === 'failed'
      const type = failed ? 'error' : row.result === 'success' ? 'success' : 'warning'
      return h(NTag, {
        type, size: 'small',
        style: failed ? 'cursor: pointer;' : '',
        onClick: failed ? () => openDetail(row) : undefined,
      }, { default: () => row.result })
    }
  },
  { title: '新增', key: 'added' },
  { title: '删除', key: 'deleted' },
  {
    title: '详情', key: 'detail',
    render(row: SyncLogEntry) {
      if (row.result === 'success') return '-'
      return h(NButton, { text: true, onClick: () => openDetail(row) }, { default: () => '同步详情' })
    },
  },
]
</script>

<template>
  <div>
    <h2>同步日志</h2>

    <!-- 刷新 / 清空按钮：置于页面标题下方（改进：从历史记录标题行移出） -->
    <NSpace style="margin: 8px 0 12px">
      <NButton size="large" @click="loadLogs">刷新</NButton>
      <NButton size="large" type="error" tertiary @click="showClearConfirm = true">清空记录</NButton>
    </NSpace>

    <!-- 历史记录（最顶部，Build4 Step 4：改进 5） -->
    <h3 style="margin: 0">历史记录</h3>
    <NDataTable :columns="columns" :data="logs" :bordered="true" :max-height="400" style="margin-top: 12px" />

    <!-- 实时运行日志（常驻展开，Build4 Step 11：移除折叠控件） -->
    <h3 style="margin-top: 16px">运行日志（实时）</h3>
    <p aria-live="polite" style="margin: 8px 0">{{ streamStatus }}<span v-if="streamNotice"> · {{ streamNotice }}</span></p>
    <pre style="max-height: 300px; overflow-y: auto; background: #1e1e1e; color: #d4d4d4; padding: 12px; border-radius: 6px; font-size: 12px; line-height: 1.6; white-space: pre-wrap; word-break: break-all;">{{ logLines.join('\n') || '等待日志输出...' }}</pre>

    <!-- 清空历史记录确认弹窗（卡片式） -->
    <NModal v-model:show="showClearConfirm" preset="card" title="清空历史记录" style="width: 420px">
      <p style="margin: 0 0 16px; line-height: 1.7">将清空全部同步历史记录，此操作不可恢复。确认继续？</p>
      <NSpace justify="end">
        <NButton size="large" @click="showClearConfirm = false">取消</NButton>
        <NButton type="error" size="large" @click="clearLogs">确认清空</NButton>
      </NSpace>
    </NModal>

    <!-- failed / skipped / partial 统一查看同步详情 -->
    <NModal v-model:show="showDetailModal" preset="card" title="同步详情" style="width: 600px">
      <p v-if="detailLog" style="line-height: 1.9">
        <b>时间：</b>{{ formatTime(detailLog.timestamp) }}<br />
        <b>目标：</b>{{ detailLog.target || '-' }}<br />
        <b>域名：</b>{{ detailLog.domain || '-' }}<br />
        <b>结果：</b>{{ detailLog.result }}<br />
        <b>新增 / 删除：</b>{{ detailLog.added }} / {{ detailLog.deleted }}
      </p>
      <p style="margin-bottom: 8px"><b>{{ detailLog?.result === 'failed' ? '错误原因' : '同步说明' }}：</b></p>
      <pre v-if="detailLog?.error" :style="{ background: '#1e1e1e', color: detailLog.result === 'failed' ? '#f44336' : '#d4d4d4', padding: '12px', borderRadius: '6px', fontSize: '12px', lineHeight: '1.6', whiteSpace: 'pre-wrap', wordBreak: 'break-all' }">{{ detailLog.error }}</pre>
      <p v-else style="color: #999">该记录未保存详细说明</p>
    </NModal>
  </div>
</template>
