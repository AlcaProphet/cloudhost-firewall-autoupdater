<script setup lang="ts">
// Dry Run 结果展示（Issue7 §7.5）：每个已配置目标一张卡片，target_id 作为稳定 key。
//
// 固定口径：
//   - 清理候选只是预览：标题必须写「满足安全门后可清理」，不得表述为确定删除；
//   - cleanup_deferred / unsupported / conflicts 显示稳定原因文案；
//   - 无适用规则目标只显示未调度提示，不渲染空规划表格；
//   - 所有数组在 DTO 层恒为 []，这里只做空态展示。
import { NCard, NDataTable, NAlert, NSpace, NStatistic, NGrid, NGi, NTag, NButton } from 'naive-ui'
import { computed } from 'vue'
import type { DryRunExecutionUnit, DryRunPreflight, DryRunResult, PlannedRule, RuleChange, PlanIssue, PlanMatch } from '../types'
import { cloudLabelMap } from '../constants'

const props = defineProps<{
  results: DryRunResult[]
  warnings: string[]
  preflight?: DryRunPreflight | null
  executionUnits?: DryRunExecutionUnit[]
  status: 'idle' | 'running' | 'completed' | 'failed'
  error: string
}>()

const units = computed(() => props.executionUnits || [])

// 目标是否有任何适用来源：DNS 规则或 EdgeOne 站点。DNS 关闭或仅 EdgeOne
// 站点时 domains 为空，不能把它误判为「无适用规则」而隐藏真实计划。
function hasTargetPlan(item: DryRunResult): boolean {
  return Boolean(item.domains?.length || item.edgeone_sites?.length)
}

const plannedTargetKeys = computed(() =>
  new Set(props.results.filter(hasTargetPlan).map(item => item.execution_target_key)),
)

function sharedTargetKey(item: DryRunResult): string | undefined {
  const key = item.execution_target_key
  if (key && !hasTargetPlan(item) && plannedTargetKeys.value.has(key)) return key
}

function scrollToExecutionUnit(key: string | undefined) {
  const id = units.value.find(unit => unit.target_key === key)?.target_ids[0]
  document.getElementById(`dryrun-unit-${id}`)?.scrollIntoView({ block: 'start' })
}

const preflightHint = computed(() => {
  const p = props.preflight
  if (!p) return null
  if (p.error && p.state !== 'overlap') {
    return { type: 'error' as const, text: '全局预检未能完成；正式同步会阻断两侧' }
  }
  switch (p.state) {
    case 'overlap': {
      const c = p.conflict
      const detail = c ? `：${c.dns_name} ${c.dns_address} 落入 ${c.edgeone_prefix}（${c.zone_id}）` : ''
      return { type: 'error' as const, text: `全局地址重叠，正式同步会阻断两侧${detail}` }
    }
    case 'data_unavailable': {
      if (!p.dns_complete && !p.edgeone_complete) {
        return { type: 'warning' as const, text: '数据不足：DNS 与 EdgeOne 两侧输入均不完整，正式同步会阻断两侧' }
      }
      const failed = !p.dns_complete ? 'DNS' : 'EdgeOne'
      return { type: 'warning' as const, text: `数据不足：${failed} 侧输入不完整；完整侧按自身安全门继续（单侧继续）` }
    }
    case 'clear':
      return { type: 'success' as const, text: '全局预检完整，未发现地址重叠；以下仍只是预览，不会写入云端' }
    default:
      return {
        type: 'info' as const,
        text: p.known_conflict
          ? '保留历史地址冲突记录；当前按已启用来源运行，重新双开后复查'
          : '仅单项来源启用，当前不做全局重叠判定',
      }
  }
})

function versionLabel(value: string): string {
  return value || '未知'
}

function limitLabel(unit: DryRunExecutionUnit): string {
  if (unit.limit === null || unit.limit === undefined) return '未知'
  return `${unit.limit}（${unit.limit_source}）`
}

// ─── 顶部统计条（目标级口径） ───
const stats = computed(() => {
  let desired = 0
  let owned = 0
  let external = 0
  let toAdd = 0
  let candidates = 0
  let unsupported = 0
  let errors = 0
  for (const r of props.results) {
    desired += r.desired?.length || 0
    owned += r.satisfied_by_owned?.length || 0
    external += r.satisfied_by_external?.length || 0
    toAdd += r.to_add?.length || 0
    candidates += r.cleanup_candidates?.length || 0
    unsupported += r.unsupported?.length || 0
    if (r.error) errors++
  }
  if (units.value.length) {
    toAdd = units.value.reduce((total, unit) => total + unit.planned_add, 0)
    candidates = units.value.reduce((total, unit) => total + unit.cleanup_candidates.length, 0)
  }
  return { targets: props.results.length, desired, owned, external, toAdd, candidates, unsupported, errors }
})

// 规则明细列（待新增 / 清理候选共用）
const changeColumns = [
  { title: '协议', key: 'protocol' },
  { title: '端口', key: 'port' },
  { title: '动作', key: 'action' },
  { title: 'CIDR', key: 'cidr' },
  { title: '描述', key: 'desc' },
]

// 期望功能列
const desiredColumns = [
  { title: '地址族', key: 'family' },
  { title: 'CIDR', key: 'cidr' },
  { title: '协议', key: 'protocol' },
  { title: '端口', key: 'port' },
  { title: '动作', key: 'action' },
  { title: '来源域名', key: 'domains' },
]

function providerLabel(name: string): string {
  const ct = name.split('(')[0]
  return cloudLabelMap[ct] || name
}

function resourceID(name: string): string {
  const start = name.indexOf('(')
  const end = name.indexOf(')')
  return start >= 0 && end > start ? name.slice(start + 1, end) : name
}

function emptyChange(): RuleChange[] {
  return []
}

function desiredRows(rules: PlannedRule[]) {
  return (rules || []).map((r) => ({
    family: r.key.family,
    cidr: r.key.cidr,
    protocol: r.key.protocol,
    port: r.key.port,
    action: r.key.action,
    domains: (r.domains || []).join(', ') + (r.implementable ? '' : '（平台无法实施）'),
  }))
}

function matchRows(matches: PlanMatch[]) {
  return (matches || []).map((m) => ({
    family: m.key.family,
    cidr: m.key.cidr,
    protocol: m.key.protocol,
    port: m.key.port,
    action: m.key.action,
    desc: m.description,
  }))
}

function issueText(issue: PlanIssue): string {
  const where = issue.key ? `${issue.key.family} ${issue.key.cidr} ${issue.key.protocol}/${issue.key.port}` : ''
  return where ? `${issue.message}（${where}）` : issue.message
}
</script>

<template>
  <div>
    <!-- 空状态（按优先级） -->
    <NAlert v-if="status === 'idle'" type="info" :show-icon="false">
      尚未执行模拟测试，点击上方「执行模拟测试」开始
    </NAlert>

    <NAlert v-else-if="status === 'running'" type="info" :show-icon="false">
      正在执行模拟测试，请等待本次结果
    </NAlert>
    <NAlert v-else-if="status === 'failed'" type="error" :show-icon="false">
      本次模拟测试请求失败：{{ error }}
    </NAlert>
    <template v-else-if="status === 'completed'">
      <NAlert v-if="warnings.length > 0" type="warning" :show-icon="false" style="margin-bottom: 12px">
        <template v-for="(w, i) in warnings" :key="i">
          <div>{{ w }}</div>
        </template>
      </NAlert>

      <NAlert v-if="preflightHint" :type="preflightHint.type" :show-icon="false" style="margin-bottom: 12px" data-testid="dryrun-preflight">
        {{ preflightHint.text }}
        <div v-if="props.preflight?.error" style="font-size: 13px; margin-top: 4px">{{ props.preflight.error }}</div>
        <div v-if="props.preflight?.protected_cidrs?.length" style="font-size: 13px; margin-top: 4px">
          过渡保护：{{ props.preflight.protected_cidrs.join(', ') }}
        </div>
      </NAlert>

      <!-- 统计条（目标级口径） -->
      <NGrid :cols="8" :x-gap="12" style="margin-bottom: 16px">
        <NGi><NStatistic label="已配置目标" :value="stats.targets" /></NGi>
        <NGi><NStatistic label="所需功能" :value="stats.desired" /></NGi>
        <NGi><NStatistic label="已由 TAG 满足" :value="stats.owned" /></NGi>
        <NGi><NStatistic label="已由外部满足" :value="stats.external" /></NGi>
        <NGi><NStatistic label="待新增" :value="stats.toAdd" /></NGi>
        <NGi><NStatistic label="清理候选" :value="stats.candidates" /></NGi>
        <NGi><NStatistic label="无法实施" :value="stats.unsupported" /></NGi>
        <NGi><NStatistic label="错误" :value="stats.errors" /></NGi>
      </NGrid>

      <NSpace v-if="units.length" vertical size="large" style="margin-bottom: 16px">
        <NCard v-for="unit in units" :key="unit.target_key" :id="`dryrun-unit-${unit.target_ids[0]}`" size="small" :title="`执行单元 ${unit.provider}`">
          <div style="font-size: 14px; color: #888; margin-bottom: 6px">
            物理目标 {{ unit.target_key }}；关联本地目标 {{ unit.target_ids.join(', ') || '-' }}
          </div>
          <div style="font-size: 14px; line-height: 1.9">
            已读规则 <b>{{ unit.rules_read }}</b>；当前规则 <b>{{ unit.current_rule_count }}</b>；
            预计新增 <b>{{ unit.planned_add }}</b>；过渡期峰值 <b>{{ unit.transition_peak }}</b>；
            已知限额 <b>{{ limitLabel(unit) }}</b>
          </div>
          <NAlert v-if="unit.limit_exceeded" type="warning" :show-icon="false" style="margin-top: 6px">
            过渡期峰值 {{ unit.transition_peak }} 超过已知限额 {{ unit.limit }}，正式同步可能被平台拒绝。
          </NAlert>
          <div style="font-size: 14px; line-height: 1.9">
            覆盖：{{ unit.coverage_ready ? '完整' : '未完整' }}；数据：{{ unit.data_complete ? '完整' : '不完整' }}；
            清理候选 {{ unit.cleanup_candidates.length }}，可删除 {{ unit.cleanup_deletable.length }}，
            受过渡保护 {{ unit.cleanup_protected.length }}
          </div>
          <div v-if="unit.error" style="color: #d03050; font-size: 14px">快照错误：{{ unit.error }}</div>
          <div v-if="unit.sites.length" style="margin-top: 6px">
            <div v-for="site in unit.sites" :key="site.zone_id" style="font-size: 14px; line-height: 1.8">
              站点 <b>{{ site.zone_id }}</b>：当前 {{ versionLabel(site.current_version) }}，
              待更新 {{ versionLabel(site.pending_version) }}，
              数据{{ site.data_complete ? '完整' : `不完整（${site.reasons.join(', ') || '原因未知'}）` }}
              <span v-if="site.protected_cidrs.length">；保护 {{ site.protected_cidrs.join(', ') }}</span>
            </div>
          </div>
          <template v-if="unit.conflicts.length">
            <NAlert v-for="(issue, i) in unit.conflicts" :key="`uc-${i}`" type="error" :show-icon="false" style="margin-top: 6px">
              {{ issueText(issue) }}
            </NAlert>
          </template>
        </NCard>
      </NSpace>

      <NAlert v-if="results.length === 0" type="info" :show-icon="false">
        本次未返回目标预览，请检查目标配置及页面提示
      </NAlert>

      <!-- 目标卡片：target_id 是稳定 key（同域名多规则不会再产生重复 key） -->
      <NSpace vertical size="large">
        <NCard
          v-for="item in results"
          :key="item.target_id"
          :title="providerLabel(item.provider)"
          size="small"
        >
          <template #header-extra>
            <NTag v-if="item.domains?.length" size="small" :bordered="false">{{ item.domains.length }} 个来源域名</NTag>
            <NTag v-else-if="item.edgeone_sites?.length" size="small" :bordered="false">{{ item.edgeone_sites.length }} 个 EdgeOne 站点</NTag>
            <NTag v-else-if="sharedTargetKey(item)" size="small" :bordered="false">共用物理目标</NTag>
            <NTag v-else type="default" size="small" :bordered="false">无适用规则</NTag>
            <NTag size="small" :bordered="false" style="margin-left: 8px">{{ resourceID(item.provider) }}</NTag>
          </template>

          <NAlert v-if="sharedTargetKey(item)" type="info" :show-icon="false">
            此本地目标没有直接绑定，与其他本地目标共用物理目标 {{ sharedTargetKey(item) }}。
            <NButton text type="primary" @click="scrollToExecutionUnit(sharedTargetKey(item))">查看共同执行单元</NButton>
          </NAlert>
          <NAlert v-else-if="!hasTargetPlan(item)" type="info" :show-icon="false">
            当前没有适用于此目标的域名规则或 EdgeOne 站点；正式同步将跳过此目标，不会读取或修改其云防火墙
          </NAlert>

          <template v-else>
            <NAlert v-if="item.error" type="error" :show-icon="false" style="margin-bottom: 12px">
              {{ item.error }}
            </NAlert>

            <div v-if="item.domains?.length" style="font-size: 14px; color: #888; margin-bottom: 8px">
              来源域名：{{ item.domains.join(', ') }}
            </div>
            <div v-if="item.edgeone_sites?.length" style="font-size: 14px; color: #888; margin-bottom: 8px">
              EdgeOne 站点：{{ item.edgeone_sites.join(', ') }}
            </div>

            <div style="font-size: 14px; color: #666; margin-bottom: 4px">所需功能（完整期望集）</div>
            <NDataTable
              :columns="desiredColumns"
              :data="desiredRows(item.desired)"
              :bordered="true"
              size="small"
              :max-height="200"
            />

            <div style="font-size: 14px; color: #666; margin: 12px 0 4px">已由 TAG 规则满足</div>
            <NDataTable
              :columns="changeColumns"
              :data="matchRows(item.satisfied_by_owned)"
              :bordered="true"
              size="small"
              :max-height="200"
            />

            <div style="font-size: 14px; color: #666; margin: 12px 0 4px">已由外部规则满足（只读，永不改动）</div>
            <NDataTable
              :columns="changeColumns"
              :data="matchRows(item.satisfied_by_external)"
              :bordered="true"
              size="small"
              :max-height="200"
            />

            <div style="font-size: 14px; color: #2080f0; margin: 12px 0 4px">待新增</div>
            <NDataTable
              :columns="changeColumns"
              :data="item.to_add?.length ? item.to_add : emptyChange()"
              :bordered="true"
              size="small"
              :max-height="200"
            />

            <!-- 清理候选只是预览：必须表达「满足安全门后可清理」 -->
            <div style="font-size: 14px; color: #f0a020; margin: 12px 0 4px">
              清理候选（满足安全门后可清理，非确定删除）
            </div>
            <NDataTable
              :columns="changeColumns"
              :data="item.cleanup_candidates?.length ? item.cleanup_candidates : emptyChange()"
              :bordered="true"
              size="small"
              :max-height="200"
            />

            <template v-if="item.cleanup_deferred?.length">
              <div style="font-size: 14px; color: #f0a020; margin: 12px 0 4px">清理延后原因</div>
              <NAlert v-for="(issue, i) in item.cleanup_deferred" :key="`cd-${i}`" type="warning" :show-icon="false" style="margin-bottom: 4px">
                {{ issueText(issue) }}
              </NAlert>
            </template>

            <template v-if="item.unsupported?.length">
              <div style="font-size: 14px; color: #f0a020; margin: 12px 0 4px">平台无法实施（目标记为部分实施）</div>
              <NAlert v-for="(issue, i) in item.unsupported" :key="`un-${i}`" type="warning" :show-icon="false" style="margin-bottom: 4px">
                {{ issueText(issue) }}
              </NAlert>
            </template>

            <template v-if="item.conflicts?.length">
              <div style="font-size: 14px; color: #d03050; margin: 12px 0 4px">冲突（冻结清理，需人工确认）</div>
              <NAlert v-for="(issue, i) in item.conflicts" :key="`cf-${i}`" type="error" :show-icon="false" style="margin-bottom: 4px">
                {{ issueText(issue) }}
              </NAlert>
            </template>

            <template v-if="item.dns_errors?.length">
              <div style="font-size: 14px; color: #d03050; margin: 12px 0 4px">DNS 解析失败（目标记为失败，保留现有规则）</div>
              <NAlert v-for="(issue, i) in item.dns_errors" :key="`dns-${i}`" type="error" :show-icon="false" style="margin-bottom: 4px">
                {{ issueText(issue) }}
              </NAlert>
            </template>
          </template>
        </NCard>
      </NSpace>
    </template>
  </div>
</template>
