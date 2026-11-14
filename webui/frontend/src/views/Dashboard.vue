<script setup lang="ts">
// 仪表盘：2×2 大卡片（同步引擎 / 上次同步 / 统计概览 / 操作中心）+ 首次使用引导
// 改进 3：不再放置「运行测试」入口（左侧菜单栏为唯一入口）
import { NCard, NGrid, NGi, NButton, NAlert, NTooltip, NSpace, useMessage } from 'naive-ui'
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'
import { useSettings } from '../composables/useSettings'
import type { EdgeOneSiteStatus, EdgeOneStatus, SourceStatus, SyncStatus } from '../types'

const status = ref<SyncStatus>({
  running: false,
  last_sync: null,
  enabled: true,
  last_success: null,
  last_round: null,
  // Build7 Step 4：内存态时间戳（无在途轮次时为 null）
  round_started_at: null,
  process_started_at: '',
})
const edgeStatus = ref<EdgeOneStatus | null>(null)
const edgeStatusError = ref('')
const switching = ref(false) // 开关请求 loading
const stats = ref({ targets: 0, rules: 0 })
const showGuide = ref(false) // 首次使用引导条
const router = useRouter()
const message = useMessage()
const { refresh, tcReady, aliReady } = useSettings()
let pageActive = true
// 状态与统计各自只接受最新发起请求的响应。
let statusSequence = 0
let statsSequence = 0
let edgeSequence = 0
let triggerTimer: ReturnType<typeof setTimeout> | null = null
let timer: ReturnType<typeof setInterval> | null = null

// 三态标签：同步引擎状态（唯一展示：28px 大字 + 状态色；Build4 Step 9 去重）
const engineTag = computed(() => {
  if (!status.value.running) return { text: '已停止', color: '#808080' }
  return status.value.enabled
    ? { text: '运行中', color: '#18a058' }
    : { text: '已暂停', color: '#f0a020' }
})

async function fetchStatus() {
  if (!pageActive) return
  const sequence = ++statusSequence
  try {
    const s = await request<SyncStatus>('/api/sync/status')
    if (pageActive && sequence === statusSequence) status.value = s
  } catch { /* 轮询失败忽略 */ }
}

// 预检状态：同一 HTTP 200 下 clear/overlap/data_unavailable/single_source 必须可区分。
const preflightHint = computed(() => {
  const p = status.value.address_preflight
  if (!p) return { type: 'info' as const, text: '全局预检状态未知（后端未返回 address_preflight）' }
  if (p.state === 'single_source' && p.known_conflict) {
    return { type: 'info' as const, text: '保留历史地址冲突记录；当前按已启用来源运行，重新双开后复查' }
  }
  if (p.error && p.state !== 'overlap') {
    return { type: 'error' as const, text: '全局预检未能完成；正式同步已阻断两侧' }
  }
  if (p.known_conflict && p.state !== 'overlap') {
    return { type: 'error' as const, text: '存在已查实地址重叠；正式同步保持保守阻断' }
  }
  switch (p.state) {
    case 'overlap': {
      const c = p.conflict
      const detail = c ? `：${c.dns_name} ${c.dns_address} 落入 ${c.edgeone_prefix}（${c.zone_id}）` : ''
      return { type: 'error' as const, text: `地址重叠，正式同步已阻断两侧${detail}` }
    }
    case 'data_unavailable': {
      // 配置保存/重启后尚未完成首次预检：这是「未观察」，不是「已证明数据不足」。
      if (!p.observed_at && !p.dns_complete && !p.edgeone_complete) {
        return { type: 'info' as const, text: '尚未完成首次全局预检；正式同步将按下一轮预检决定，当前不视为数据不足' }
      }
      if (!p.dns_complete && !p.edgeone_complete) {
        return { type: 'warning' as const, text: '数据不足：DNS 与 EdgeOne 两侧输入均不完整，正式同步阻断两侧' }
      }
      const failed = !p.dns_complete ? 'DNS' : 'EdgeOne'
      return { type: 'warning' as const, text: `数据不足：${failed} 侧输入不完整；完整侧仍按自身安全门继续（单侧继续）` }
    }
    case 'clear':
      return { type: 'success' as const, text: '全局预检完整，未发现地址重叠' }
    default:
      return { type: 'info' as const, text: '仅单项来源启用，当前不做全局重叠判定' }
  }
})

function sourceLabel(src?: SourceStatus): string {
  if (!src) return '未知'
  if (!src.enabled) return '已关闭'
  return src.effective ? '已启用并生效' : '已启用（总暂停中）'
}

function dueLabel(value: string | null | undefined): string {
  return value ? new Date(value).toLocaleString() : '暂无记录'
}

function outcomeLabel(value: string | undefined): string {
  switch (value) {
    case 'success': return '成功'
    case 'partial': return '部分完成'
    case 'failed': return '失败'
    case 'idle': return '无任务'
    case 'blocked': return '被阻断'
    case 'incomplete': return '数据不足'
    default: return '暂无结果'
  }
}

function confirmationLabel(st: EdgeOneSiteStatus): string {
  if (st.actual_effective) return '已生效'
  if (st.confirmation_state === 'accepted') return '已确认，等待生效'
  if (st.confirmation_state === 'unknown') return '结果未知'
  return '待确认'
}

function coverageLabel(st: EdgeOneSiteStatus): string {
  if (!st.coverage?.length) return '暂无覆盖记录'
  const complete = st.coverage.filter(item => item.complete).length
  const reasons = [...new Set(st.coverage.flatMap(item => item.reasons || []))]
  return `覆盖 ${complete}/${st.coverage.length}${reasons.length ? `；${reasons.join(', ')}` : ''}`
}

function versionLabel(value: string): string {
  return value || '未知'
}

// 清理观察独立于累计操作；历史、估计和未知都不能作为完整当前残留。
const cleanupHint = computed(() => {
  const round = status.value.last_round
  if (!round) return '暂无记录'
  if (round.total === 0) return '无适用目标'
  const o = round.cleanup_observation_summary
  if (!o) return '当前残留未确认（缺少观察信息）'
  if (o.complete) return `最终尝试 S1 候选 ${o.observed_candidates} 条；已观察残留 ${o.observed_deferred} 条`
  const pending = o.estimated_targets + o.historical_targets + o.unknown_targets
  return `已观察部分残留 ${o.observed_deferred} 条；另有 ${pending} 个目标当前残留未确认（估计 ${o.estimated_targets}、历史 ${o.historical_targets}、未知 ${o.unknown_targets}）`
})

// 失败和部分实施仍由 outcome 决定；观察完整性只用于清理提示。
const healthHint = computed(() => {
  if (!status.value.enabled) return null
  const round = status.value.last_round
  if (!round) return null

  if (round.outcome === 'failed') {
    const partial = round.skipped > 0 ? `，部分实施 ${round.skipped}` : ''
    return { type: 'error' as const, text: `最近一轮同步失败：共 ${round.total} 个目标，失败 ${round.failed}${partial}。请查看同步日志排查。` }
  }
  if (round.outcome === 'partial') {
    return { type: 'warning' as const, text: `最近一轮同步部分完成：共 ${round.total} 个目标，其中 ${round.skipped} 个目标存在平台无法实施的规则（已确认的权限不受影响）。` }
  }
  if (round.outcome === 'idle') return null
  const observation = round.cleanup_observation_summary
  if (!observation || !observation.complete) {
    return { type: 'warning' as const, text: `所需权限已确认，清理状态尚未最终确认。${cleanupHint.value}` }
  }
  if (observation.observed_deferred > 0) {
    return { type: 'warning' as const, text: `所需权限已确认，陈旧规则清理延后：已观察残留 ${observation.observed_deferred} 条（本轮累计确认清理 ${round.cleanup_deleted} 条）。` }
  }
  return null
})

async function fetchEdgeStatus() {
  if (!pageActive) return
  const sequence = ++edgeSequence
  try {
    const data = await request<EdgeOneStatus>('/api/edgeone/status')
    if (pageActive && sequence === edgeSequence) {
      edgeStatus.value = data
      edgeStatusError.value = ''
    }
  } catch (e: any) {
    // 保留最近一次成功记录，但必须明确提示本次查询失败；不能显示成“暂无记录”。
    if (pageActive && sequence === edgeSequence) edgeStatusError.value = e?.message || '站点状态读取失败'
  }
}

// 统计概览：目标数 / 规则数（最近一轮增删直接使用同步状态快照）
async function fetchStats() {
  if (!pageActive) return
  const sequence = ++statsSequence
  try {
    const [targets, rules] = await Promise.all([
      request<any[]>('/api/targets'),
      request<any[]>('/api/rules'),
    ])
    if (!pageActive || sequence !== statsSequence) return
    stats.value = {
      targets: targets.length,
      rules: rules.length,
    }
  } catch { /* 统计失败忽略，不阻塞主状态 */ }
}

// 首次使用引导：四类凭据均未配置时展示（改进 11）；凭据配置后自动消失
// 用 refresh() 强制拉取：用户在设置页配置凭据后返回本页，引导条应立即消失（避免模块级缓存导致旧状态）
async function checkGuide() {
  await refresh()
  if (!pageActive) return
  showGuide.value = !(tcReady.value || aliReady.value)
}

function closeGuide() { showGuide.value = false }

async function triggerSync() {
  if (!pageActive) return
  try {
    await request('/api/sync/trigger', { method: 'POST' })
    if (!pageActive) return
    message.success('同步已触发')
  } catch (e: any) {
    if (!pageActive) return
    message.error(`触发失败: ${e.message}`) // 暂停时后端返回 409
  }
  if (!pageActive) return
  if (triggerTimer) clearTimeout(triggerTimer)
  triggerTimer = setTimeout(() => {
    triggerTimer = null
    fetchStatus()
  }, 1000)
}

async function toggleSync() {
  if (!pageActive) return
  switching.value = true
  try {
    await request(`/api/sync/${status.value.enabled ? 'pause' : 'resume'}`, { method: 'POST' })
    if (!pageActive) return
    status.value.enabled = !status.value.enabled
    message.success(status.value.enabled ? '同步已开启' : '同步已暂停')
  } catch (e: any) {
    if (!pageActive) return
    message.error(`操作失败: ${e.message}`)
  } finally {
    switching.value = false
    fetchStatus()
  }
}

onMounted(() => {
  fetchStatus()
  fetchEdgeStatus()
  fetchStats()
  checkGuide()
  timer = setInterval(() => { fetchStatus(); fetchEdgeStatus(); fetchStats() }, 5000)
})

onUnmounted(() => {
  pageActive = false
  if (triggerTimer) clearTimeout(triggerTimer)
  if (timer) clearInterval(timer)
})
</script>

<template>
  <div>
    <h2>仪表盘</h2>

    <!-- 首次使用引导（改进 11） -->
    <NAlert v-if="showGuide" type="warning" closable @close="closeGuide" style="margin-bottom: 16px">
      首次使用：请先在「全局设置」中填写云厂商 API 密钥（SecretId/SecretKey），再添加云资源目标与域名规则。
      <NButton size="small" type="primary" ghost style="margin-left: 12px" @click="router.push('/settings')">去配置</NButton>
    </NAlert>

    <!-- 同步健康提示（Issue6 A3）：走既有 5s 轮询，不依赖 /api/sync/events -->
    <NAlert v-if="healthHint" :type="healthHint.type" style="margin-bottom: 16px" data-testid="sync-health-hint">
      {{ healthHint.text }}
    </NAlert>

    <NAlert v-if="preflightHint" :type="preflightHint.type" style="margin-bottom: 16px" data-testid="preflight-hint">
      {{ preflightHint.text }}
    </NAlert>

    <!-- 2×2 大卡片（改进 2：方案 B） -->
    <NGrid :cols="2" :x-gap="16" :y-gap="16">
      <NGi>
        <NCard title="同步引擎" style="min-height: 200px">
          <div :style="{ fontSize: '28px', fontWeight: 600, marginTop: '20px', color: engineTag.color }">{{ engineTag.text }}</div>
          <!-- NTag 已移除（Build4 Step 9：状态显示去重，唯一展示） -->
          <div style="font-size: 14px; color: #888; margin-top: 12px">
            DNS：{{ sourceLabel(status.dns) }}；下次到期：{{ dueLabel(status.dns?.next_due_at) }}；
            最近结果：{{ outcomeLabel(status.dns?.last_outcome) }}
          </div>
          <div style="font-size: 14px; color: #888; margin-top: 4px">
            EdgeOne：{{ sourceLabel(status.edgeone) }}；下次到期：{{ dueLabel(status.edgeone?.next_due_at) }}；
            最近结果：{{ outcomeLabel(status.edgeone?.last_outcome) }}
          </div>
          <div style="font-size: 14px; color: #888; margin-top: 8px">开启后按同步间隔自动执行；变更预览请使用「模拟测试」页</div>
        </NCard>
      </NGi>
      <NGi>
        <NCard title="上次同步" style="min-height: 200px">
          <div style="font-size: 24px; font-weight: 600; margin-top: 20px">
            {{ status.last_sync ? new Date(status.last_sync).toLocaleString() : '暂无记录' }}
          </div>
          <div style="font-size: 14px; color: #888; margin-top: 8px">本次启动后的最近一轮完成时间，重启后重新记录</div>
        </NCard>
      </NGi>
      <NGi>
        <NCard title="统计概览" style="min-height: 200px">
          <NSpace vertical size="large" style="margin-top: 20px">
            <div style="font-size: 20px">云资源目标 <b style="font-size: 28px">{{ stats.targets }}</b> 个</div>
            <div style="font-size: 20px">域名规则 <b style="font-size: 28px">{{ stats.rules }}</b> 条</div>
            <div style="font-size: 14px; color: #888">以下为本次启动后的最近一轮同步统计</div>
            <div style="font-size: 20px">
              <template v-if="status.last_round">最近同步 新增 <b style="font-size: 28px">{{ status.last_round.added }}</b> / 删除 <b style="font-size: 28px">{{ status.last_round.deleted }}</b></template>
              <template v-else>最近同步增删 暂无记录</template>
            </div>
            <div style="font-size: 20px" data-testid="cleanup-operations">本轮累计确认清理 {{ status.last_round ? `${status.last_round.cleanup_deleted} 条` : '暂无记录' }}</div>
            <div style="font-size: 16px" data-testid="cleanup-observation">{{ cleanupHint }}</div>
          </NSpace>
        </NCard>
      </NGi>
      <NGi>
        <NCard title="操作中心" style="min-height: 200px">
          <NSpace size="large" style="margin-top: 20px">
            <!-- 暂停时「立即同步」置灰 + hover 提示 -->
            <NTooltip v-if="!status.enabled">
              <template #trigger>
                <span>
                  <NButton type="primary" size="large" disabled>立即同步</NButton>
                </span>
              </template>
              请先开启同步引擎
            </NTooltip>
            <NButton v-else type="primary" size="large" @click="triggerSync">立即同步</NButton>
            <NButton :type="status.enabled ? 'warning' : 'success'" size="large" :loading="switching" @click="toggleSync">
              {{ status.enabled ? '暂停同步' : '开启同步' }}
            </NButton>
          </NSpace>
          <div style="font-size: 14px; color: #888; margin-top: 16px">同步全局开关状态与「全局设置」中持久化配置一致</div>
        </NCard>
      </NGi>
    </NGrid>

    <NCard title="EdgeOne 站点状态" style="margin-top: 16px" size="small">
      <NAlert v-if="edgeStatusError" type="error" :show-icon="false" style="margin-bottom: 12px" data-testid="edge-status-error">
        读取站点状态失败：{{ edgeStatusError }}<span v-if="edgeStatus">（以下为最近一次成功记录）</span>
      </NAlert>
      <div v-if="!status.edgeone?.enabled" style="color: #888">EdgeOne 回源未启用，暂无站点运行状态。</div>
      <div v-else-if="!edgeStatus && !edgeStatusError" style="color: #888">暂无站点运行状态记录。</div>
      <div v-else-if="edgeStatus">
        <div v-for="st in edgeStatus.sites" :key="st.zone_id" style="border-bottom: 1px solid #eee; padding: 8px 0">
          <div style="font-family: monospace; font-weight: 600">{{ st.zone_id }}</div>
          <div style="font-size: 14px; line-height: 1.8">
            当前版本 <b>{{ versionLabel(st.current_version) }}</b>，
            待更新版本 <b>{{ versionLabel(st.pending_version) }}</b>；
            <span v-if="!st.pending_version && st.transition_pending_version">
              正在追踪版本 <b>{{ st.transition_pending_version }}</b>；
            </span>
            最近查询：{{ st.last_query_at ? new Date(st.last_query_at).toLocaleString() : '暂无记录' }}
          </div>
          <div style="font-size: 14px; line-height: 1.8">
            覆盖：{{ coverageLabel(st) }}；确认：<b>{{ confirmationLabel(st) }}</b>
            <span v-if="st.last_query_error">；查询错误：{{ st.last_query_error }}</span>
          </div>
        </div>
        <div v-if="edgeStatus.sites.length === 0" style="color: #888">暂无站点运行状态记录。</div>
      </div>
    </NCard>
  </div>
</template>
