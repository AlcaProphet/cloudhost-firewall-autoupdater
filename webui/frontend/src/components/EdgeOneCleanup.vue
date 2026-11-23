<script setup lang="ts">
import { NAlert, NButton, NCard, NCheckbox, NDataTable, NModal, NSelect, NSpace } from 'naive-ui'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { request, RequestError } from '../api'
import type { CleanupOperation, CleanupPreview, TargetConfig } from '../types'
const props = defineProps<{ targets: TargetConfig[]; edgeEnabled: boolean }>()
const emit = defineEmits<{ changed: [] }>()
const all = ref(false), selected = ref<number[]>([]), disable = ref(false)
const preview = ref<CleanupPreview | null>(null), operation = ref<CleanupOperation | null>(null)
const show = ref(false), previewing = ref(false), submitting = ref(false), reading = ref(false)
const error = ref('')
const pending = ref<{ preview_id: string; disable_edgeone: boolean } | null>(null)
const busy = computed(() => previewing.value || submitting.value || reading.value)
const running = computed(() => operation.value && ['queued', 'running'].includes(operation.value.status))
const options = computed(() => {
  const physical = new Map<string, { label: string; value: number }>()
  for (const t of props.targets) {
    const key = JSON.stringify([t.cloud_type, t.region, t.resource_id])
    const existing = physical.get(key)
    if (existing) existing.label += ` / #${t.id}`
    else physical.set(key, { value: t.id, label: `${t.cloud_type} · ${t.region} · ${t.resource_id}（别名 #${t.id}）` })
  }
  return [...physical.values()]
})
const storageKey = 'fwalizer-edgeone-cleanup'
let active = true
let timer: ReturnType<typeof setTimeout> | null = null
onUnmounted(() => { active = false; if (timer !== null) clearTimeout(timer) })
function remember() {
  try {
    window.sessionStorage.setItem(storageKey, JSON.stringify({ pending: pending.value, operation_id: operation.value?.operation_id || '' }))
  } catch { /* 存储不可用时仍可在当前页面回读，不因此重复提交。 */ }
}
function schedule() {
  if (timer !== null) clearTimeout(timer)
  if (active && running.value) timer = setTimeout(readResult, 2000)
}
async function readResult() {
  const id = operation.value?.operation_id
  if (!active || !id || reading.value) return
  reading.value = true
  try {
    const data = await request<CleanupOperation>(`/api/edgeone/firewall-cleanup/${encodeURIComponent(id)}`)
    if (!active) return
    operation.value = data
    error.value = ''
    remember()
    if (!['queued', 'running'].includes(data.status)) emit('changed')
  } catch (e: any) {
    if (active && e instanceof RequestError && e.status === 404) {
      operation.value = { ...operation.value!, status: 'unavailable', cleared: false }
      remember()
    }
    if (active) error.value = `进度读取失败：${e.message}。可重新查询；任务不会因 HTTP 断开而取消。若服务已重启或任务已过期，请重新预览核对云端现状。`
  } finally {
    if (active) { reading.value = false; schedule() }
  }
}
async function buildPreview() {
  if (!active || busy.value || running.value || pending.value) return
  if (!all.value && !selected.value.length) { error.value = '请选择目标，或明确勾选全部已配置目标'; return }
  previewing.value = true
  preview.value = null
  operation.value = null
  error.value = ''
  disable.value = false
  remember()
  try {
    const data = await request<CleanupPreview>('/api/edgeone/firewall-cleanup/preview', {
      method: 'POST', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(all.value ? { all: true } : { target_ids: [...selected.value] }),
    })
    if (active) { preview.value = data; show.value = true }
  } catch (e: any) { if (active) error.value = `清理预览失败：${e.message}` }
  finally { if (active) previewing.value = false }
}
function cancel() {
  if (submitting.value || pending.value) return
  show.value = false
  preview.value = null
}
async function confirm() {
  if (!active || submitting.value || operation.value) return
  if (!pending.value) {
    if (!show.value || !preview.value?.preview_id) return
    if (!preview.value.expires_at || Date.now() >= Date.parse(preview.value.expires_at)) {
      error.value = '预览已过期，请取消并重新预览'
      return
    }
    pending.value = { preview_id: preview.value.preview_id, disable_edgeone: disable.value }
    remember()
  }
  submitting.value = true
  error.value = ''
  try {
    // 重复确认仅用原 preview_id 和原选项核对同一操作，绝不自动创建新授权。
    const data = await request<CleanupOperation>('/api/edgeone/firewall-cleanup', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(pending.value),
    })
    if (!active) return
    operation.value = data
    pending.value = null
    show.value = false
    remember()
    emit('changed')
    schedule()
  } catch (e: any) {
    if (!active) return
    if (e instanceof RequestError && e.operationId) {
      operation.value = { operation_id: e.operationId, status: 'queued', targets: [] } as unknown as CleanupOperation
      pending.value = null
      show.value = false
      remember()
      await readResult()
    } else if (e instanceof RequestError && e.status === 503) {
      pending.value = null
      preview.value = null
      show.value = false
      remember()
      error.value = `清理引擎未受理：${e.message}。请稍后重新预览。`
    } else if (e instanceof RequestError && e.status >= 400 && e.status < 500) {
      pending.value = null
      preview.value = null
      show.value = false
      remember()
      error.value = `清理未获受理：${e.message}。请重新预览。`
    } else {
      error.value = `受理结果尚未确认：${e.message}。请点击“核对受理结果”，使用原预览标识查询同一操作。`
    }
  } finally { if (active) submitting.value = false }
}
function resetResult() {
  if (busy.value || running.value || pending.value) return
  operation.value = null
  preview.value = null
  remember()
}
const count = (value: number | null) => value === null ? '未知' : value
const columns = [
  { title: '分类', key: 'status', render: (r: { status: string }) => r.status === 'deletable' ? '可删' : '跳过' },
  { title: '协议', key: 'protocol' }, { title: '端口', key: 'port' }, { title: '来源', key: 'source' },
  { title: '动作', key: 'action' }, { title: '描述', key: 'description' },
  { title: '定位 ID', key: 'rule_id' }, { title: '入站索引', key: 'policy_index' }, { title: '跳过原因', key: 'reason' },
]
const labels: Record<string, string> = { queued: '已排队', running: '执行中', completed: '已结束', rejected: '已拒绝', unchanged: '保持原开关', disabled: '已关闭', already_disabled: '原已关闭', failed: '关闭失败', confirmed: '已确认依赖', potential: '潜在依赖', unverified: '未验证', none: '无适用 DNS 依赖' }
const label = (value: string) => labels[value] || value
onMounted(async () => {
  try {
    const saved = JSON.parse(window.sessionStorage.getItem(storageKey) || 'null')
    if (saved?.operation_id && typeof saved.operation_id === 'string') {
      operation.value = { operation_id: saved.operation_id, status: 'queued', targets: [] } as unknown as CleanupOperation
      await readResult()
    } else if (saved?.pending && typeof saved.pending.preview_id === 'string' && typeof saved.pending.disable_edgeone === 'boolean') {
      pending.value = saved.pending
      error.value = '上次受理结果尚未确认，请核对原操作。'
    }
  } catch { /* 不可用或损坏的本地记录不产生云操作。 */ }
})
</script>
<template>
  <NCard title="删除 EdgeOne TAG 规则" size="small" style="margin-top: 16px">
    <p>选择一个或多个物理目标，包含已解绑但仍配置的目标；同实例别名合并。安全组只处理受管入站规则。</p>
    <NAlert type="warning">此操作会撤除当前 TAG 下已确认且可定位的回源规则，包括仍在使用的规则，可能中断回源及依赖它的 DNS 访问。共享安全组的全部关联实例也会受影响。若 EdgeOne 保持开启且后续同步具备准入条件，规则可能自动重建。总暂停时仍可执行手动清理。</NAlert>
    <NSpace vertical style="margin-top: 12px">
      <NCheckbox v-model:checked="all" :disabled="busy || !!running || !!pending">全部已配置目标</NCheckbox>
      <NSelect v-model:value="selected" multiple :options="options" :disabled="all || busy || !!running || !!pending" placeholder="明确选择目标（空选择不会清理全部）" />
      <NSpace><NButton size="large" type="error" :loading="previewing" :disabled="busy || !!running || !!pending || (!all && !selected.length)" @click="buildPreview">只读预览清理范围</NButton></NSpace>
      <NAlert v-if="error" type="error">{{ error }}</NAlert>
      <p v-if="pending">待核对预览 {{ pending.preview_id }}；原选项：{{ pending.disable_edgeone ? '先关闭全部站点的 EdgeOne 同步' : '保持 EdgeOne 开关' }}。</p>
      <NButton v-if="pending" size="large" :loading="submitting" :disabled="submitting" @click="confirm">核对受理结果</NButton>
      <template v-if="operation">
        <p>操作 {{ operation.operation_id }}：{{ label(operation.status) }}；TAG [{{ operation.tag || '等待查询' }}]；开关动作 {{ label(operation.switch_action || '等待查询') }}。{{ operation.reason_code || '' }}</p>
        <NAlert :type="operation.cleared ? 'success' : 'warning'">{{ operation.cleared ? '已清空：所有所选目标均可信回读且当前 TAG 零残留' : '尚未证明已清空；任务结束不等于规则已清空' }}</NAlert>
        <div v-for="t in operation.targets" :key="t.target_ids.join(',')" style="overflow-wrap: anywhere; line-height: 1.9">
          <b>{{ t.cloud_type }} · {{ t.region }} · {{ t.resource_id }}（{{ t.target_ids.join(', ') }}）</b>
          <p>云确认删除 {{ t.confirmed_deleted }}；回读已不存在 {{ count(t.absent_rows) }}；当前 TAG 残留 {{ count(t.remaining_rows) }}；回读{{ t.readback_verified ? '可信' : '未验证' }}；{{ t.outcome_unknown ? '提交结果未知' : '无提交未知标记' }}。{{ t.status }} {{ t.reason_code || '' }}</p>
        </div>
        <NSpace><NButton size="large" :loading="reading" :disabled="reading" @click="readResult">重新查询进度</NButton><NButton size="large" :disabled="busy || !!running" @click="resetResult">结束查看，重新预览</NButton></NSpace>
      </template>
    </NSpace>
    <NModal :show="show" @update:show="(value) => { if (!value) cancel() }" preset="card" title="确认删除 EdgeOne TAG 规则" :closable="!submitting && !pending" :mask-closable="!submitting && !pending" :close-on-esc="!submitting && !pending" style="width: min(1000px, calc(100vw - 32px))">
      <template v-if="preview">
        <p>范围：{{ preview.scope === 'all' ? '全部已配置目标' : '所选目标' }}；TAG [{{ preview.tag }}]；{{ preview.summary.targets }} 个物理目标，可删 {{ preview.summary.deletable_rows }} 行，跳过 {{ preview.summary.skipped_rows }} 行，未读 {{ preview.summary.targets_unread }} 个目标。有效至 {{ preview.expires_at || '无可执行标识' }}。</p>
        <NAlert type="error">确认将撤除上述可删物理行，可能中断回源。未读与跳过项会保留，执行前云端或配置变化可能导致拒绝。开启同步后可能自动重建。</NAlert>
        <p v-for="(w, i) in preview.warnings" :key="i">{{ w }}</p>
        <NCard v-for="t in preview.targets" :key="t.target_ids.join(',')" size="small" style="margin-top: 12px" :title="`${t.cloud_type} · ${t.region} · ${t.resource_id}`">
          <p>别名 {{ t.target_ids.join(', ') }}；站点 {{ t.sites.map(s => s.zone_id).join(', ') || '已解绑／无绑定' }}；快照版本 {{ t.revision || '未知' }}；{{ t.status === 'unread' ? '未读' : '已读' }} {{ t.reason || '' }}。</p>
          <p>可删 {{ t.deletable_rows }}，跳过 {{ t.skipped_rows }}，其他保留 {{ t.retained_rows }}；DNS：{{ label(t.dns_dependency.status) }} {{ t.dns_dependency.reason || '' }}</p>
          <NDataTable v-if="t.rows.length" :columns="columns" :data="t.rows" :scroll-x="900" />
        </NCard>
        <NCheckbox v-model:checked="disable" :disabled="submitting || !!pending" style="margin-top: 16px">清理前关闭 EdgeOne 回源同步（影响全部站点）</NCheckbox>
        <p>默认保持当前开关（当前{{ edgeEnabled ? '开启' : '关闭' }}）。勾选后先关闭全部站点的 EdgeOne 同步，关闭失败则零删除；不改变总暂停或 DNS 开关。</p>
        <NAlert v-if="error" type="error">{{ error }}</NAlert>
        <NSpace justify="end" style="margin-top: 12px"><NButton size="large" :disabled="submitting || !!pending" @click="cancel">取消</NButton><NButton size="large" type="error" :loading="submitting" :disabled="submitting || !preview.preview_id" @click="confirm">{{ pending ? '核对受理结果' : '确认删除预览规则' }}</NButton></NSpace>
      </template>
    </NModal>
  </NCard>
</template>
