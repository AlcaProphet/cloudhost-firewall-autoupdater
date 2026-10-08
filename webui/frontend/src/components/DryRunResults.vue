<script setup lang="ts">
// Dry Run 结果展示（Issue7 §7.5）：每个已配置目标一张卡片，target_id 作为稳定 key。
//
// 固定口径：
//   - 清理候选只是预览：标题必须写「满足安全门后可清理」，不得表述为确定删除；
//   - cleanup_deferred / unsupported / conflicts 显示稳定原因文案；
//   - 无适用规则目标只显示未调度提示，不渲染空规划表格；
//   - 所有数组在 DTO 层恒为 []，这里只做空态展示。
import { NCard, NDataTable, NAlert, NSpace, NStatistic, NGrid, NGi, NTag } from 'naive-ui'
import { computed } from 'vue'
import type { DryRunResult, PlannedRule, RuleChange, PlanIssue, PlanMatch } from '../types'
import { cloudLabelMap } from '../constants'

const props = defineProps<{
  results: DryRunResult[]
  warnings: string[]
  status: 'idle' | 'running' | 'completed' | 'failed'
  error: string
}>()

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
            <NTag v-else type="default" size="small" :bordered="false">无适用规则</NTag>
            <NTag size="small" :bordered="false" style="margin-left: 8px">{{ resourceID(item.provider) }}</NTag>
          </template>

          <NAlert v-if="!item.domains?.length" type="info" :show-icon="false">
            当前没有适用于此目标的域名规则；正式同步将跳过此目标，不会读取或修改其云防火墙
          </NAlert>

          <template v-else>
            <NAlert v-if="item.error" type="error" :show-icon="false" style="margin-bottom: 12px">
              {{ item.error }}
            </NAlert>

            <div style="font-size: 14px; color: #888; margin-bottom: 8px">
              来源域名：{{ item.domains.join(', ') }}
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
