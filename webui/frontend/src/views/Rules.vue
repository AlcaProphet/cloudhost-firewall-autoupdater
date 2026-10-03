<script setup lang="ts">
import { NDataTable, NButton, NModal, NForm, NFormItem, NInput, NSelect, NSpace, NSwitch, NTag, useMessage } from 'naive-ui'
import { ref, onMounted, onUnmounted, h, watch, computed, nextTick } from 'vue'
import { request, RequestError } from '../api'
import { cloudLabelMap } from '../constants'
import type { DomainRule } from '../types'

const rules = ref<DomainRule[]>([])
const showModal = ref(false)
// 保存包含后续列表刷新，锁定期间不允许重新创建或切换表单。
const saving = ref(false)
const editingId = ref<number | null>(null)
const form = ref({ host: '', protocol: 'TCP', ports: '', action: 'ACCEPT', comment: '', enable_ipv6: false, targets: [] as number[] })
const message = useMessage()
const targetOptions = ref<{ label: string; value: number }[]>([])

const protocolOptions = [
  { label: 'TCP', value: 'TCP' },
  { label: 'UDP', value: 'UDP' },
  { label: 'TCP+UDP', value: 'TCP+UDP' },
  { label: 'ICMP', value: 'ICMP' },
]

const actionOptions = [
  { label: 'ACCEPT', value: 'ACCEPT' },
  { label: 'DROP', value: 'DROP' },
]

// ICMP 协议时自动将端口设为 ALL
watch(() => form.value.protocol, (newProto) => {
  if (newProto === 'ICMP') {
    form.value.ports = 'ALL'
  }
})

// 列表只接受最新请求；删除和卸载使此前的响应失效。
let pageActive = true
let loadSequence = 0
onUnmounted(() => {
  pageActive = false
  ++loadSequence
})

async function load(failureLabel = '加载规则失败') {
  if (!pageActive) return
  const sequence = ++loadSequence
  try {
    const data = await request<DomainRule[]>('/api/rules')
    if (pageActive && sequence === loadSequence) rules.value = data
  } catch (e: any) {
    if (pageActive && sequence === loadSequence) message.error(`${failureLabel}: ${e.message}`)
  }
}

async function loadTargets() {
  try {
    const data = await request<any[]>('/api/targets')
    targetOptions.value = data.map((t: any) => ({
      label: `${cloudLabelMap[t.cloud_type] || t.cloud_type} / ${t.resource_id}`,
      value: t.id,
    }))
  } catch (e: any) {
    message.error(`加载目标失败: ${e.message}`)
  }
}

onMounted(async () => {
  await load()
  await loadTargets()
})

function openAdd() {
  if (!pageActive || saving.value || deletePhase.value !== 'idle') return
  editingId.value = null
  form.value = { host: '', protocol: 'TCP', ports: '', action: 'ACCEPT', comment: '', enable_ipv6: false, targets: [] }
  showModal.value = true
}

function openEdit(row: any) {
  if (!pageActive || saving.value || deletePhase.value !== 'idle') return
  editingId.value = row.id
  form.value = { host: row.host, protocol: row.protocol, ports: row.ports, action: row.action, comment: row.comment || '', enable_ipv6: !!row.enable_ipv6, targets: Array.isArray(row.targets) ? [...row.targets] : [] }
  showModal.value = true
}

async function saveRule() {
  if (!pageActive || saving.value || deletePhase.value !== 'idle') return
  if (!showModal.value) return
  saving.value = true
  ++loadSequence
  const id = editingId.value
  const method = id ? 'PUT' : 'POST'
  const url = id ? `/api/rules/${id}` : '/api/rules'
  try {
    await request(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(form.value),
    })
    if (!pageActive) return
    showModal.value = false
    message.success(id ? '更新成功' : '添加成功')
    await load('配置已保存，但规则列表刷新失败，请刷新页面核对')
  } catch (e: unknown) {
    if (pageActive) message.error(e instanceof RequestError
      ? `保存失败: ${e.message}`
      : '未能确认保存结果，请刷新列表核对；不会自动重试保存')
  } finally {
    saving.value = false
  }
}

// 删除状态属于当前页面，不在关闭动画中清理，避免旧动画覆盖新确认。
const deletePhase = ref<'idle' | 'confirming' | 'deleting' | 'refreshing'>('idle')
const pendingDelete = ref<Readonly<DomainRule> | null>(null)
const deleteBusy = computed(() => deletePhase.value === 'deleting' || deletePhase.value === 'refreshing')
const showDeleteConfirm = computed(() => deletePhase.value === 'confirming' || deletePhase.value === 'deleting')
const cancelDeleteButton = ref<{ $el: HTMLElement } | null>(null)
const pageAddButton = ref<{ $el: HTMLElement } | null>(null)
let deleteOrigin: HTMLElement | null = null
let deleteDialogLeft = true
let deleteFocusPending = false
let deleteSucceeded = false

// 列表刷新与退出动画都完成后再恢复焦点，避免 FocusTrap 后到的恢复覆盖。
function finishDeleteFocus() {
  if (!pageActive || deletePhase.value !== 'idle' || showModal.value || !deleteDialogLeft || !deleteFocusPending) return
  deleteFocusPending = false
  const focusTarget = !deleteSucceeded && deleteOrigin?.isConnected ? deleteOrigin : pageAddButton.value?.$el
  focusTarget?.focus()
}

async function afterDeleteLeave() {
  await nextTick()
  if (showDeleteConfirm.value) return
  deleteDialogLeft = true
  finishDeleteFocus()
}

function focusDeleteCancel() {
  if (pageActive && deletePhase.value === 'confirming') cancelDeleteButton.value?.$el.focus()
}

function openDeleteConfirm(row: DomainRule, origin: HTMLElement | null = null) {
  if (!pageActive || saving.value || deletePhase.value !== 'idle' || showModal.value) return
  pendingDelete.value = { ...row, targets: [...row.targets] }
  deleteOrigin = origin
  deleteDialogLeft = false
  deleteFocusPending = false
  deletePhase.value = 'confirming'
}

function cancelDelete() {
  if (deletePhase.value !== 'confirming') return
  pendingDelete.value = null
  deletePhase.value = 'idle'
}

function updateDeleteConfirm(show: boolean) {
  if (!show) cancelDelete()
}

async function confirmDelete() {
  if (!pageActive || deletePhase.value !== 'confirming' || !pendingDelete.value) return
  const id = pendingDelete.value.id
  deletePhase.value = 'deleting'
  ++loadSequence
  let deleted = false
  try {
    await request(`/api/rules/${id}`, { method: 'DELETE' })
    deleted = true
  } catch (e: unknown) {
    if (pageActive) {
      message.error(e instanceof RequestError
        ? `删除失败: ${e.message}`
        : '未能确认删除结果，正在刷新列表核对；不会自动重试删除')
    }
  }

  // 离开页面不代表服务端取消；旧页面不再刷新、提示或抢占焦点。
  if (!pageActive) return
  if (deleted) {
    rules.value = rules.value.filter(row => row.id !== id)
    message.success('删除成功')
  }
  deleteSucceeded = deleted
  deleteFocusPending = true
  pendingDelete.value = null
  deletePhase.value = 'refreshing'
  try {
    await load(deleted
      ? '配置已删除，但列表刷新失败，请刷新页面核对'
      : '核对删除结果时列表加载失败，请刷新页面')
  } finally {
    deletePhase.value = 'idle'
  }
  await nextTick()
  finishDeleteFocus()
}

const columns = [
  { title: '#', key: 'index', render: (_: any, i: number) => i + 1 },
  { title: '域名', key: 'host' },
  { title: '协议', key: 'protocol' },
  { title: '端口', key: 'ports' },
  { title: '动作', key: 'action' },
  {
    title: 'IP版本', key: 'enable_ipv6',
    render(row: any) {
      return h(NTag, { size: 'small', type: row.enable_ipv6 ? 'info' : 'default' }, { default: () => row.enable_ipv6 ? 'IPv4+6' : '仅IPv4' })
    }
  },
  { title: '备注', key: 'comment' },
  {
    title: '适用目标', key: 'targets',
    render(row: any) {
      if (!row.targets || row.targets.length === 0) return '全部'
      return row.targets.map((id: number) => {
        const opt = targetOptions.value.find(o => o.value === id)
        return opt ? opt.label : `#${id}`
      }).join(', ')
    }
  },
  {
    title: '操作', key: 'actions',
    render(row: any) {
      return h(NSpace, { size: 'small' }, {
        default: () => [
          h(NButton, { size: 'tiny', disabled: deleteBusy.value || saving.value, onClick: () => openEdit(row) }, { default: () => '编辑' }),
          h(NButton, { size: 'tiny', type: 'error', disabled: deleteBusy.value || saving.value, onClick: (event: MouseEvent) => openDeleteConfirm(row, event.currentTarget as HTMLElement) }, { default: () => '删除' }),
        ]
      })
    }
  },
]
</script>

<template>
  <div>
    <h2>域名规则</h2>
    <NButton ref="pageAddButton" type="primary" size="large" :disabled="deleteBusy || saving" style="margin: 8px 0 12px" @click="openAdd">添加规则</NButton>
    <NDataTable :columns="columns" :data="rules" :bordered="true" />

    <NModal v-model:show="showModal" :title="editingId ? '编辑规则' : '添加规则'" preset="card" :closable="!saving" :mask-closable="!saving" :close-on-esc="!saving" style="width: 540px">
      <NForm :disabled="saving" :model="form" label-placement="left" label-width="80">
        <NFormItem label="域名">
          <NInput v-model:value="form.host" placeholder="api.example.com" />
        </NFormItem>
        <NFormItem label="协议">
          <NSelect v-model:value="form.protocol" :options="protocolOptions" />
        </NFormItem>
        <NFormItem label="端口">
          <NInput v-model:value="form.ports" :placeholder="form.protocol === 'ICMP' ? 'ICMP 协议固定为 ALL' : '443,80 / 8000-8010 / ALL'" :disabled="saving || form.protocol === 'ICMP'" />
        </NFormItem>
        <NFormItem label="动作">
          <NSelect v-model:value="form.action" :options="actionOptions" />
        </NFormItem>
        <NFormItem label="适用目标">
          <NSelect v-model:value="form.targets" :options="targetOptions" multiple placeholder="留空 = 全部目标" clearable />
        </NFormItem>
        <NFormItem label="备注">
          <NInput v-model:value="form.comment" placeholder="可选" />
        </NFormItem>
        <NFormItem label="解析IPv6">
          <NSwitch v-model:value="form.enable_ipv6" />
          <span style="margin-left: 8px; font-size: 14px; color: #999">{{ form.enable_ipv6 ? '同时使用 A + AAAA 记录' : '仅使用 A 记录（IPv4）' }}</span>
        </NFormItem>
        <NButton type="primary" :loading="saving" :disabled="saving" @click="saveRule">保存</NButton>
      </NForm>
    </NModal>

    <!-- 删除只在确认处理函数发起；所有关闭入口均为取消。 -->
    <NModal
      :show="showDeleteConfirm" @update:show="updateDeleteConfirm"
      preset="card" title="删除规则配置" aria-label="删除规则配置"
      style="width: min(520px, calc(100vw - 32px))"
      :closable="!deleteBusy" :mask-closable="!deleteBusy" :close-on-esc="!deleteBusy"
      :auto-focus="false" @after-enter="focusDeleteCancel" @after-leave="afterDeleteLeave"
    >
      <div v-if="pendingDelete" style="overflow-wrap: anywhere; line-height: 1.7">
        <p>配置 ID：#{{ pendingDelete.id }}</p>
        <p>域名：{{ pendingDelete.host }}</p>
        <p>协议 / 端口 / 动作：{{ pendingDelete.protocol }} / {{ pendingDelete.ports }} / {{ pendingDelete.action }}</p>
        <p>IP 版本：{{ pendingDelete.enable_ipv6 ? 'IPv4+6' : '仅IPv4' }}</p>
        <p>适用目标：{{ pendingDelete.targets.length === 0 ? '全部目标' : pendingDelete.targets.map(id => targetOptions.find(option => option.value === id)?.label || `#${id}`).join('；') }}</p>
      </div>
      <p style="line-height: 1.7">删除后，后续同步将不再包含这条规则。不再需要的当前 TAG 云规则可能在满足安全条件后被清理；若目标已无适用规则，则会跳过该目标并保留已有云规则。</p>
      <NSpace justify="end">
        <NButton ref="cancelDeleteButton" size="large" :disabled="deleteBusy" @click="cancelDelete">取消</NButton>
        <NButton type="error" size="large" :disabled="deleteBusy || !pendingDelete" :loading="deletePhase === 'deleting'" @click="confirmDelete">
          {{ deletePhase === 'deleting' ? '删除中…' : '确认删除' }}
        </NButton>
      </NSpace>
    </NModal>
  </div>
</template>
