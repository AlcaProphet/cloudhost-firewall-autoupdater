<script setup lang="ts">
// Dry Run 结果展示：三级分组（目标 → 域名 → 规则）+ 语义化空状态
import { NCard, NDataTable, NAlert, NSpace, NStatistic, NGrid, NGi } from 'naive-ui'
import { computed } from 'vue'
import type { DryRunResult, RuleChange } from '../types'
import { cloudLabelMap } from '../constants'

const props = defineProps<{
  results: DryRunResult[]
  warnings: string[]
  hasRun: boolean
}>()

// ─── 统计条（内部计算） ───
const stats = computed(() => {
  let toAdd = 0
  let toDelete = 0
  let skipped = 0
  let errors = 0
  for (const r of props.results) {
    toAdd += r.to_add?.length || 0
    toDelete += r.to_delete?.length || 0
    skipped += r.skipped?.length || 0
    if (r.error) errors++
  }
  return { targets: props.results.length, toAdd, toDelete, skipped, errors }
})

// ─── 按 provider 分组 ───
const groups = computed(() => {
  const map = new Map<string, DryRunResult[]>()
  for (const r of props.results) {
    const list = map.get(r.provider) || []
    list.push(r)
    map.set(r.provider, list)
  }
  return Array.from(map.entries())
})

// 规则明细列（待添加 / 待删除共用）
const changeColumns = [
  { title: '协议', key: 'protocol' },
  { title: '端口', key: 'port' },
  { title: '动作', key: 'action' },
  { title: 'CIDR', key: 'cidr' },
  { title: '描述', key: 'desc' },
]

// 无法实施（跳过）明细列：在既有五列基础上追加「跳过原因」
const skippedColumns = [
  ...changeColumns,
  { title: '跳过原因', key: 'skip_reason' },
]

function providerLabel(name: string): string {
  const ct = name.split('(')[0]
  return cloudLabelMap[ct] || name
}

function emptyChange(): RuleChange[] {
  return []
}
</script>

<template>
  <div>
    <!-- 空状态（按优先级） -->
    <NAlert v-if="!hasRun" type="info" :show-icon="false">
      尚未执行模拟测试，点击上方「执行模拟测试」开始
    </NAlert>

    <template v-else>
      <NAlert v-if="warnings.length > 0" type="warning" :show-icon="false" style="margin-bottom: 12px">
        <template v-for="(w, i) in warnings" :key="i">
          <div>{{ w }}</div>
        </template>
      </NAlert>

      <!-- 统计条 -->
      <NGrid :cols="5" :x-gap="12" style="margin-bottom: 16px">
        <NGi>
          <NStatistic label="目标数" :value="stats.targets" />
        </NGi>
        <NGi>
          <NStatistic label="待添加" :value="stats.toAdd" />
        </NGi>
        <NGi>
          <NStatistic label="待删除" :value="stats.toDelete" />
        </NGi>
        <NGi>
          <NStatistic label="无法实施" :value="stats.skipped" />
        </NGi>
        <NGi>
          <NStatistic label="错误" :value="stats.errors" />
        </NGi>
      </NGrid>

      <!-- 无目标且无规则时仅展示 warnings；否则展示明细 -->
      <NAlert v-if="results.length === 0 && warnings.length === 0" type="success" :show-icon="false">
        无待变更规则
      </NAlert>

      <!-- 三级分组：目标 → 域名 → 规则 -->
      <NSpace vertical size="large">
        <NCard v-for="[provider, items] in groups" :key="provider" :title="providerLabel(provider)" size="small">
          <div v-for="item in items" :key="item.domain" style="margin-bottom: 16px">
            <h4 style="margin: 0 0 8px">{{ item.domain }}</h4>

            <!-- 错误行：不展开明细 -->
            <NAlert v-if="item.error" type="error" :show-icon="false">
              {{ item.error }}
            </NAlert>

            <template v-else>
              <div style="font-size: 14px; color: #666; margin-bottom: 4px">待添加</div>
              <NDataTable
                :columns="changeColumns"
                :data="item.to_add?.length ? item.to_add : emptyChange()"
                :bordered="true"
                size="small"
                :max-height="200"
              />
              <div style="font-size: 14px; color: #666; margin: 12px 0 4px">待删除</div>
              <NDataTable
                :columns="changeColumns"
                :data="item.to_delete?.length ? item.to_delete : emptyChange()"
                :bordered="true"
                size="small"
                :max-height="200"
              />
              <!-- 无法实施（跳过）的期望规则：该云产品无法表达，同步时会跳过 -->
              <template v-if="item.skipped?.length">
                <div style="font-size: 14px; color: #f0a020; margin: 12px 0 4px">无法实施（同步时跳过）</div>
                <NDataTable
                  :columns="skippedColumns"
                  :data="item.skipped"
                  :bordered="true"
                  size="small"
                  :max-height="200"
                />
              </template>
            </template>
          </div>
        </NCard>
      </NSpace>
    </template>
  </div>
</template>
