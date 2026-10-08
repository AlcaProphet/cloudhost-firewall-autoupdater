<script setup lang="ts">
// 仪表盘：2×2 大卡片（同步引擎 / 上次同步 / 统计概览 / 操作中心）+ 首次使用引导
// 改进 3：不再放置「运行测试」入口（左侧菜单栏为唯一入口）
import { NCard, NGrid, NGi, NButton, NAlert, NTooltip, NSpace, useMessage } from 'naive-ui'
import { ref, computed, onMounted, onUnmounted } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'
import { useSettings } from '../composables/useSettings'
import type { SyncStatus } from '../types'

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
const switching = ref(false) // 开关请求 loading
const stats = ref({ targets: 0, rules: 0 })
const showGuide = ref(false) // 首次使用引导条
const router = useRouter()
const message = useMessage()
const { refresh, tcReady, aliReady } = useSettings()
let timer: ReturnType<typeof setInterval> | null = null

// 三态标签：同步引擎状态（唯一展示：28px 大字 + 状态色；Build4 Step 9 去重）
const engineTag = computed(() => {
  if (!status.value.running) return { text: '已停止', color: '#808080' }
  return status.value.enabled
    ? { text: '运行中', color: '#18a058' }
    : { text: '已暂停', color: '#f0a020' }
})

async function fetchStatus() {
  try {
    const s = await request<SyncStatus>('/api/sync/status')
    status.value = s
  } catch { /* 轮询失败忽略 */ }
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

// 统计概览：目标数 / 规则数（最近一轮增删直接使用同步状态快照）
async function fetchStats() {
  try {
    const [targets, rules] = await Promise.all([
      request<any[]>('/api/targets'),
      request<any[]>('/api/rules'),
    ])
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
  showGuide.value = !(tcReady.value || aliReady.value)
}

function closeGuide() { showGuide.value = false }

async function triggerSync() {
  try {
    await request('/api/sync/trigger', { method: 'POST' })
    message.success('同步已触发')
  } catch (e: any) {
    message.error(`触发失败: ${e.message}`) // 暂停时后端返回 409
  }
  setTimeout(fetchStatus, 1000)
}

async function toggleSync() {
  switching.value = true
  try {
    await request(`/api/sync/${status.value.enabled ? 'pause' : 'resume'}`, { method: 'POST' })
    status.value.enabled = !status.value.enabled
    message.success(status.value.enabled ? '同步已开启' : '同步已暂停')
  } catch (e: any) {
    message.error(`操作失败: ${e.message}`)
  } finally {
    switching.value = false
    fetchStatus()
  }
}

onMounted(() => {
  fetchStatus()
  fetchStats()
  checkGuide()
  timer = setInterval(() => { fetchStatus(); fetchStats() }, 5000)
})

onUnmounted(() => {
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

    <!-- 2×2 大卡片（改进 2：方案 B） -->
    <NGrid :cols="2" :x-gap="16" :y-gap="16">
      <NGi>
        <NCard title="同步引擎" style="min-height: 200px">
          <div :style="{ fontSize: '28px', fontWeight: 600, marginTop: '20px', color: engineTag.color }">{{ engineTag.text }}</div>
          <!-- NTag 已移除（Build4 Step 9：状态显示去重，唯一展示） -->
          <div style="font-size: 14px; color: #888; margin-top: 8px">开启后按同步间隔自动执行；变更预览请使用「模拟测试」页</div>
        </NCard>
      </NGi>
      <NGi>
        <NCard title="上次同步" style="min-height: 200px">
          <div style="font-size: 24px; font-weight: 600; margin-top: 20px">
            {{ status.last_sync ? new Date(status.last_sync).toLocaleString() : '暂无' }}
          </div>
          <div style="font-size: 14px; color: #888; margin-top: 8px">最近一轮同步完成时间</div>
        </NCard>
      </NGi>
      <NGi>
        <NCard title="统计概览" style="min-height: 200px">
          <NSpace vertical size="large" style="margin-top: 20px">
            <div style="font-size: 20px">云资源目标 <b style="font-size: 28px">{{ stats.targets }}</b> 个</div>
            <div style="font-size: 20px">域名规则 <b style="font-size: 28px">{{ stats.rules }}</b> 条</div>
            <div style="font-size: 20px">最近同步 新增 <b style="font-size: 28px">{{ status.last_round?.added ?? 0 }}</b> / 删除 <b style="font-size: 28px">{{ status.last_round?.deleted ?? 0 }}</b></div>
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
  </div>
</template>
