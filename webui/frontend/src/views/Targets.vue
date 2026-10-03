<script setup lang="ts">
import { NDataTable, NButton, NModal, NForm, NFormItem, NInput, NSelect, NSpace, NAlert, useMessage } from 'naive-ui'
import { ref, onMounted, onUnmounted, h, watch, computed, nextTick } from 'vue'
import { useRouter } from 'vue-router'
import { request, RequestError } from '../api'
import { cloudOptions, cloudLabelMap, resourceIdHint } from '../constants'
import { useSettings } from '../composables/useSettings'
import { useZones } from '../composables/useZones'
import { useScannedResources } from '../composables/useScannedResources'
import type { TargetConfig, TestConnectionResult } from '../types'

const targets = ref<TargetConfig[]>([])
const showModal = ref(false)
// 保存包含后续列表刷新，锁定期间不允许重新创建或切换表单。
const saving = ref(false)
const editingId = ref<number | null>(null)
// resource_id 初始为 null：避免 NSelect tag 模式将空字符串视为已选值导致 placeholder 不显示
const form = ref<{ cloud_type: string; region: string; resource_id: string | null }>({ cloud_type: 'tc_lighthouse', region: '', resource_id: null })
const testResult = ref('')
const message = useMessage()

// ─── Keys 缺失提示（改进 12：仅提示，不阻止保存） ───
const router = useRouter()
const { refresh: refreshSettings, tcReady, aliReady } = useSettings()
const credWarning = ref('')

// 依据当前表单云类型刷新凭据提示
function updateCredWarning() {
  const ct = form.value.cloud_type
  if (ct.startsWith('tc_')) {
    credWarning.value = tcReady.value ? '' : '腾讯云凭据未配置，请先在「全局设置」中填写 SecretId/SecretKey，否则同步将失败'
  } else if (ct.startsWith('ali_')) {
    credWarning.value = aliReady.value ? '' : '阿里云凭据未配置，请先在「全局设置」中填写 AccessKeyId/AccessKeySecret，否则同步将失败'
  } else {
    credWarning.value = ''
  }
}

// 云类型变化时刷新提示
watch(() => form.value.cloud_type, updateCredWarning)
watch(() => form.value.cloud_type, (ct) => { loadScanned(ct) })

// ─── 地域自动补全（改进：GET /api/zones 预填建议，允许输入任意值） ───
const { load: loadZones, regionOptions } = useZones()

// ─── 资源 ID 自动补全（扫描结果 + 手动输入） ───
const { load: loadScanned, resourceOptions, resourcesOf } = useScannedResources()

// 资源-地域联动：选择扫描出的资源时自动填入其所在区域（手动输入未匹配不联动）
watch(() => form.value.resource_id, (rid) => {
  if (!rid) return
  const found = resourcesOf(form.value.cloud_type).find((r) => r.resource_id === rid)
  if (found && found.region) {
    form.value.region = found.region
  }
})

// 地域选项：当前云类型预填列表 + 当前已填值（列表外值时保证编辑回显）
const regionOpts = computed(() => {
  const opts = regionOptions(form.value.cloud_type)
  const cur = form.value.region
  if (cur && !opts.some((o) => o.value === cur)) {
    opts.push({ label: cur, value: cur })
  }
  return opts
})

// 资源 ID 选项：扫描结果 + 当前已填值（保证编辑回显）
const resourceOpts = computed(() => {
  const opts = resourceOptions(form.value.cloud_type)
  const cur = form.value.resource_id
  if (cur && !opts.some((o) => o.value === cur)) {
    opts.push({ label: cur, value: cur })
  }
  return opts
})

// 资源 ID 提示由 placeholder 承载（移除 NAlert 说明块，保证表单三项等高对齐）

// 列表只接受最新请求；删除和卸载使此前的响应失效。
let pageActive = true
let loadSequence = 0
onUnmounted(() => {
  pageActive = false
  ++loadSequence
})

async function load(failureLabel = '加载目标失败') {
  if (!pageActive) return
  const sequence = ++loadSequence
  try {
    const data = await request<TargetConfig[]>('/api/targets')
    if (pageActive && sequence === loadSequence) targets.value = data
  } catch (e: any) {
    if (pageActive && sequence === loadSequence) message.error(`${failureLabel}: ${e.message}`)
  }
}

// 挂载：加载目标 + 凭据状态（refreshSettings 强制拉取保证提示新鲜）
onMounted(async () => {
  await Promise.all([load(), refreshSettings(), loadZones(), loadScanned(form.value.cloud_type)])
  updateCredWarning()
})

function openAdd() {
  if (!pageActive || saving.value || deletePhase.value !== 'idle') return
  editingId.value = null
  form.value = { cloud_type: 'tc_lighthouse', region: '', resource_id: null }
  showModal.value = true
  updateCredWarning()
}

function openEdit(row: TargetConfig) {
  if (!pageActive || saving.value || deletePhase.value !== 'idle') return
  editingId.value = row.id
  form.value = { cloud_type: row.cloud_type, region: row.region, resource_id: row.resource_id }
  showModal.value = true
  updateCredWarning()
}

async function saveTarget() {
  if (!pageActive || saving.value || deletePhase.value !== 'idle') return
  if (!showModal.value) return
  saving.value = true
  ++loadSequence
  const id = editingId.value
  const method = id ? 'PUT' : 'POST'
  const url = id ? `/api/targets/${id}` : '/api/targets'
  try {
    await request(url, {
      method,
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ cloud_type: form.value.cloud_type, region: form.value.region, resource_id: form.value.resource_id }),
    })
    if (!pageActive) return
    showModal.value = false
    message.success(id ? '更新成功' : '添加成功')
    await load('配置已保存，但目标列表刷新失败，请刷新页面核对')
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
const pendingDelete = ref<Readonly<TargetConfig> | null>(null)
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

function openDeleteConfirm(row: TargetConfig, origin: HTMLElement | null = null) {
  if (!pageActive || saving.value || deletePhase.value !== 'idle' || showModal.value) return
  pendingDelete.value = { ...row }
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
    await request(`/api/targets/${id}`, { method: 'DELETE' })
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
    targets.value = targets.value.filter(row => row.id !== id)
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

// 弹窗内表单级「测试连接」：用未保存的表单值验证（保留），15s 超时
async function testConnection() {
  if (!pageActive || saving.value) return
  testResult.value = '测试中...'
  try {
    const data = await request<TestConnectionResult>(
      '/api/test-connection',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify({ cloud_type: form.value.cloud_type, region: form.value.region, resource_id: form.value.resource_id }),
      },
      15000
    )
    testResult.value = data.success ? (data.message || '连接成功') : `失败: ${data.error || '未知错误'}`
  } catch (e: any) {
    testResult.value = e?.message === '请求超时'
      ? '连接超时（15 秒），请检查网络或云 API 状态'
      : `请求失败: ${e.message}`
  }
}

const columns = [
  { title: '#', key: 'index', render: (_: any, i: number) => i + 1 },
  { title: '云产品', key: 'cloud_type', render: (row: any) => cloudLabelMap[row.cloud_type] || row.cloud_type },
  { title: '资源ID', key: 'resource_id' },
  { title: '地域', key: 'region' },
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
    <h2>云资源管理</h2>
    <NButton ref="pageAddButton" type="primary" size="large" :disabled="deleteBusy || saving" style="margin: 8px 0 12px" @click="openAdd">添加目标</NButton>
    <NDataTable :columns="columns" :data="targets" :bordered="true" />

    <NModal v-model:show="showModal" :title="editingId ? '编辑目标' : '添加目标'" preset="card" :closable="!saving" :mask-closable="!saving" :close-on-esc="!saving" style="width: 500px">
      <NForm :disabled="saving" :model="form" label-placement="left" label-width="80">
        <!-- Keys 缺失提示（改进 12） -->
        <NAlert v-if="credWarning" type="warning" style="margin-bottom: 12px">
          {{ credWarning }}
          <NButton text type="primary" size="small" style="margin-left: 8px" @click="router.push('/settings')">去设置</NButton>
        </NAlert>
        <NFormItem label="云产品">
          <NSelect v-model:value="form.cloud_type" :options="cloudOptions" />
        </NFormItem>
        <NFormItem label="资源ID">
          <NSelect
            v-model:value="form.resource_id"
            :options="resourceOpts"
            :placeholder="resourceIdHint(form.cloud_type)"
            filterable
            tag
            clearable
          />
        </NFormItem>
        <NFormItem label="地域">
          <NSelect
            v-model:value="form.region"
            :options="regionOpts"
            filterable
            tag
            clearable
            placeholder="选择或输入地域 ID（如 ap-guangzhou）"
          />
        </NFormItem>
        <NSpace>
          <NButton type="primary" :loading="saving" :disabled="saving" @click="saveTarget">保存</NButton>
          <NButton :disabled="saving" @click="testConnection">测试连接</NButton>
        </NSpace>
        <p v-if="testResult" style="margin-top: 8px; color: #666">{{ testResult }}</p>
      </NForm>
    </NModal>

    <!-- 删除只在确认处理函数发起；所有关闭入口均为取消。 -->
    <NModal
      :show="showDeleteConfirm" @update:show="updateDeleteConfirm"
      preset="card" title="删除目标配置" aria-label="删除目标配置"
      style="width: min(520px, calc(100vw - 32px))"
      :closable="!deleteBusy" :mask-closable="!deleteBusy" :close-on-esc="!deleteBusy"
      :auto-focus="false" @after-enter="focusDeleteCancel" @after-leave="afterDeleteLeave"
    >
      <div v-if="pendingDelete" style="overflow-wrap: anywhere; line-height: 1.7">
        <p>配置 ID：#{{ pendingDelete.id }}</p>
        <p>云产品：{{ cloudLabelMap[pendingDelete.cloud_type] || pendingDelete.cloud_type }}</p>
        <p>资源 ID：{{ pendingDelete.resource_id }}</p>
        <p>地域：{{ pendingDelete.region }}</p>
      </div>
      <p style="line-height: 1.7">删除后，后续同步将不再管理该目标。此操作不会删除云资源或立即清理已有云规则。正在执行的同步轮次可能继续完成。</p>
      <NSpace justify="end">
        <NButton ref="cancelDeleteButton" size="large" :disabled="deleteBusy" @click="cancelDelete">取消</NButton>
        <NButton type="error" size="large" :disabled="deleteBusy || !pendingDelete" :loading="deletePhase === 'deleting'" @click="confirmDelete">
          {{ deletePhase === 'deleting' ? '删除中…' : '确认删除' }}
        </NButton>
      </NSpace>
    </NModal>
  </div>
</template>
