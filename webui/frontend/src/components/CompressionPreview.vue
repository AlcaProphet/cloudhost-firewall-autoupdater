<script setup lang="ts">
import { NAlert, NDataTable } from 'naive-ui'
import type { DryRunCompression } from '../types'
defineProps<{ compression: DryRunCompression; dataComplete: boolean }>()
const number = (value: number | null | undefined) => value == null ? '未知' : value.toLocaleString()
const columns = [
  { title: '协议', key: 'protocol' }, { title: '端口', key: 'port' }, { title: 'CIDR', key: 'cidr' },
  { title: '使用方式', key: 'reused', render: (r: { reused: boolean }) => r.reused ? '复用' : '拟新增' },
  { title: '表示', key: 'lossy', render: (r: { lossy: boolean }) => r.lossy ? '有损扩张' : '无损' },
]
</script>
<template>
  <div style="margin-top: 12px; line-height: 1.9; overflow-wrap: anywhere">
    <b>实例策略压缩：{{ compression.enabled ? '已开启' : '已关闭' }}</b>
    <NAlert v-if="compression.assumed" type="info">以下为假设启用的只读测算，不是当前实际执行计划；实际计划仍按关闭态运行，保存绑定不会启用压缩。</NAlert>
    <NAlert v-else-if="!dataComplete || compression.peak == null || compression.blockers.length" type="warning">规划证据不足或存在阻断，不能视为可执行计划。</NAlert>
    <p v-else>有效规划：{{ compression.mode }}；仍需正式 S1/S2 回读与站点确认安全门。</p>
    <p>实际占用 {{ dataComplete ? number(compression.s0_rows) : '未知' }}；同轮其他新增 {{ number(compression.other_new_rows) }}；压缩新增 {{ number(compression.add_rows) }}；常态上限 {{ compression.normal_cap }}，固定预留 {{ compression.reserved }}，硬上限 {{ compression.hard_cap }}。</p>
    <p>常态新增预算 {{ number(compression.normal_budget) }}；共存峰值 {{ number(compression.peak) }}；临时借用 {{ number(compression.borrowed) }}；预计清理 {{ number(compression.expected_cleanup) }}；预计清理后规则 {{ number(compression.expected_after) }}；预计清理后空位 {{ compression.expected_after == null ? '未知' : compression.hard_cap - compression.expected_after }}。</p>
    <p>风险额度 {{ number(compression.risk_limit) }}；当前额外 IPv4 {{ number(compression.current_extra_ipv4) }}；峰值额外 IPv4 {{ number(compression.extra_ipv4) }}；最大连续额外地址数 {{ number(compression.max_extra_run) }}。旧残留计入风险，手工规则不抵扣。</p>
    <p>退出压缩：{{ !compression.pending_known ? '待复查状态未知' : compression.pending_review ? '关闭后待复查，旧放行仍可能残留' : '无待复查标记' }}；残留候选 {{ dataComplete ? number(compression.residual_rows) : '未知' }}；预留恢复 {{ compression.reserved_restored == null ? '未知' : compression.reserved_restored ? '当前快照已恢复' : '尚未恢复' }}。关闭开关不等于撤销云端放行。</p>
    <div v-for="(risk, i) in compression.per_port" :key="i">{{ risk.Protocol }} / {{ risk.Port }}：额外 {{ number(risk.ExtraIPv4) }}，最大连续 {{ number(risk.MaxExtraRun) }}；范围 {{ risk.Ranges?.join(', ') || '无' }}</div>
    <NAlert v-for="(b, i) in compression.blockers" :key="`b-${i}`" type="error">{{ b.Code }}：{{ b.Message }}</NAlert>
    <NAlert v-for="(b, i) in compression.warnings" :key="`w-${i}`" type="warning">{{ b.Code }}：{{ b.Message }}</NAlert>
    <NDataTable v-if="compression.rows.length" :columns="columns" :data="compression.rows" :scroll-x="600" />
  </div>
</template>
