<script setup lang="ts">
// EdgeOne 回源模块：站点配置与目标绑定（Build8 Step 13）。
// 运行状态、版本与确认信息由 Step 14 接入；本页只消费 Step 2～4 配置合同。
import {
  NAlert, NButton, NCard, NForm, NFormItem, NInput, NModal, NSelect, NSpace, NSwitch, NTag, useMessage,
} from 'naive-ui'
import { computed, onMounted, onUnmounted, reactive, ref } from 'vue'
import { request } from '../api'
import type { EdgeOneSite, EdgeOneSiteStatus, EdgeOneStatus, TargetConfig } from '../types'

const message = useMessage()
let pageActive = true
let loadSequence = 0
let statusSequence = 0
let statusTimer: ReturnType<typeof setInterval> | null = null
onUnmounted(() => {
  pageActive = false
  if (statusTimer !== null) clearInterval(statusTimer)
})

const settings = ref<Record<string, string>>({})
const targets = ref<TargetConfig[]>([])
const sites = ref<EdgeOneSite[]>([])
const loading = ref(false)
const loadError = ref('')
const toggling = ref(false)
const edgeStatus = ref<EdgeOneStatus | null>(null)
const statusError = ref('')

const edgeEnabled = computed(() => settings.value.edgeone_enabled === 'true')
const targetOptions = computed(() => targets.value.map(t => ({
  label: `${t.cloud_type} · ${t.region} · ${t.resource_id}`,
  value: t.id,
})))

const protocolOptions = ['TCP', 'UDP', 'TCP+UDP', 'ICMP'].map(value => ({ label: value, value }))
const familyOptions = [
  { label: 'IPv4', value: 'ipv4' },
  { label: 'IPv6', value: 'ipv6' },
  { label: '双栈', value: 'dual' },
]

function targetLabel(id: number): string {
  const t = targets.value.find(item => item.id === id)
  return t ? `${t.cloud_type} · ${t.region} · ${t.resource_id}` : `#${id}`
}

async function loadAll() {
  const sequence = ++loadSequence
  loading.value = true
  loadError.value = ''
  try {
    const [settingsData, targetData, siteData] = await Promise.all([
      request<Record<string, string>>('/api/settings'),
      request<TargetConfig[]>('/api/targets'),
      request<{ sites: EdgeOneSite[] }>('/api/edgeone/sites'),
    ])
    if (!pageActive || sequence !== loadSequence) return
    settings.value = settingsData
    targets.value = Array.isArray(targetData) ? targetData : []
    sites.value = Array.isArray(siteData?.sites) ? siteData.sites : []
  } catch (e: any) {
    if (pageActive && sequence === loadSequence) loadError.value = e.message
  } finally {
    if (pageActive && sequence === loadSequence) loading.value = false
  }
}

async function loadStatus() {
  const sequence = ++statusSequence
  try {
    const data = await request<EdgeOneStatus>('/api/edgeone/status')
    if (!pageActive || sequence !== statusSequence) return
    edgeStatus.value = data
    statusError.value = ''
  } catch (e: any) {
    if (pageActive && sequence === statusSequence) statusError.value = e.message
  }
}

const statusSites = computed(() => edgeStatus.value?.sites || [])

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
  const suffix = reasons.length ? `；${reasons.join(', ')}` : ''
  return `覆盖 ${complete}/${st.coverage.length}${suffix}`
}

function versionLabel(value: string): string {
  return value || '未知'
}

function timeLabel(value: string | null): string {
  return value ? new Date(value).toLocaleString() : '暂无记录'
}

async function loadSites() {
  const sequence = ++loadSequence
  try {
    const data = await request<{ sites: EdgeOneSite[] }>('/api/edgeone/sites')
    if (!pageActive || sequence !== loadSequence) return
    sites.value = Array.isArray(data?.sites) ? data.sites : []
    await loadStatus()
  } catch (e: any) {
    if (pageActive && sequence === loadSequence) loadError.value = e.message
  }
}

async function toggleEdgeOne() {
  if (!pageActive || toggling.value) return
  const next = !edgeEnabled.value
  toggling.value = true
  try {
    await request('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ edgeone_enabled: next ? 'true' : 'false' }),
    })
    if (!pageActive) return
    settings.value = { ...settings.value, edgeone_enabled: next ? 'true' : 'false' }
    message.success(next ? 'EdgeOne 回源已启用' : 'EdgeOne 回源已关闭')
  } catch (e: any) {
    if (pageActive) message.error(`切换失败: ${e.message}`)
  } finally {
    toggling.value = false
  }
}

const showForm = ref(false)
const editingZone = ref('')
const saving = ref(false)
const formError = ref('')
const form = reactive({
  zone_id: '',
  target_ids: [] as number[],
  protocol: 'TCP',
  ports: '',
  address_family: 'ipv4' as 'ipv4' | 'ipv6' | 'dual',
})

function openCreate() {
  editingZone.value = ''
  form.zone_id = ''
  form.target_ids = []
  form.protocol = 'TCP'
  form.ports = ''
  form.address_family = 'ipv4'
  formError.value = ''
  showForm.value = true
}

function openEdit(site: EdgeOneSite) {
  editingZone.value = site.zone_id
  form.zone_id = site.zone_id
  form.target_ids = [...site.target_ids]
  form.protocol = site.protocol
  form.ports = site.ports
  form.address_family = site.address_family
  formError.value = ''
  showForm.value = true
}

async function saveSite() {
  if (!pageActive || saving.value) return
  const zoneID = form.zone_id.trim()
  if (!zoneID) {
    formError.value = '请填写 ZoneId'
    return
  }
  if (!form.target_ids.length) {
    formError.value = '至少选择一个绑定目标'
    return
  }
  saving.value = true
  formError.value = ''
  const body = {
    zone_id: zoneID,
    target_ids: form.target_ids.map(Number),
    protocol: form.protocol,
    ports: form.ports.trim(),
    address_family: form.address_family,
  }
  try {
    if (editingZone.value) {
      await request(`/api/edgeone/sites/${encodeURIComponent(editingZone.value)}`, {
        method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
      })
    } else {
      await request('/api/edgeone/sites', {
        method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body),
      })
    }
    if (!pageActive) return
    showForm.value = false
    message.success(editingZone.value ? '站点已保存' : '站点已创建')
    await loadSites()
  } catch (e: any) {
    if (pageActive) formError.value = e.message
  } finally {
    saving.value = false
  }
}

const showDelete = ref(false)
const deleting = ref(false)
const deleteZone = ref('')

function askDelete(site: EdgeOneSite) {
  deleteZone.value = site.zone_id
  showDelete.value = true
}

async function confirmDelete() {
  if (!pageActive || deleting.value || !deleteZone.value) return
  deleting.value = true
  try {
    await request(`/api/edgeone/sites/${encodeURIComponent(deleteZone.value)}`, { method: 'DELETE' })
    if (!pageActive) return
    showDelete.value = false
    message.success('站点已删除')
    await loadSites()
  } catch (e: any) {
    if (pageActive) message.error(`删除失败: ${e.message}`)
  } finally {
    deleting.value = false
  }
}

onMounted(async () => {
  await loadAll()
  if (!pageActive) return
  await loadStatus()
  if (!pageActive) return
  statusTimer = setInterval(loadStatus, 5000)
})
</script>

<template>
  <div>
    <h2>EdgeOne 回源</h2>

    <NAlert v-if="loadError" type="error" :show-icon="false" style="margin-bottom: 16px">
      加载 EdgeOne 配置失败：{{ loadError }}
    </NAlert>

    <NCard title="EdgeOne 回源同步" size="small" style="margin-bottom: 16px">
      <NSpace align="center">
        <NSwitch :value="edgeEnabled" :loading="toggling" :disabled="toggling || loading" @update:value="toggleEdgeOne" />
        <span>{{ edgeEnabled ? '已启用' : '已关闭' }}</span>
        <span style="font-size: 14px; color: #888">
          启用后，已绑定目标的站点按 EdgeOne 间隔查询源站保护网段；总暂停仍由仪表盘独立控制。
        </span>
      </NSpace>
    </NCard>

    <NCard title="站点与目标绑定" size="small">
      <template #header-extra>
        <NButton type="primary" size="large" :disabled="saving" @click="openCreate">添加站点</NButton>
      </template>

      <NAlert type="info" :show-icon="false" style="margin-bottom: 12px">
        一个 ZoneId 对应一条站点配置，可绑定多个现有目标。绑定安全组时，规则会作用于整个安全组及其关联实例；
        请确认该安全组只包含预期源站。绑定清单应覆盖该站点的全部实际源站。
      </NAlert>

      <div v-if="loading" style="color: #888">正在加载站点…</div>
      <div v-else-if="sites.length === 0" style="color: #888">暂无 EdgeOne 站点，点击「添加站点」开始配置。</div>
      <NSpace v-else vertical size="large">
        <NCard v-for="site in sites" :key="site.zone_id" size="small">
          <template #header>
            <span style="font-family: monospace">{{ site.zone_id }}</span>
          </template>
          <template #header-extra>
            <NSpace>
              <NButton size="small" :disabled="saving" @click="openEdit(site)">编辑</NButton>
              <NButton size="small" type="error" tertiary :disabled="saving || deleting" @click="askDelete(site)">删除</NButton>
            </NSpace>
          </template>
          <NSpace vertical size="small">
            <div>
              <span style="color: #888; margin-right: 8px">绑定目标</span>
              <NTag v-for="id in site.target_ids" :key="id" size="small" style="margin-right: 6px">
                {{ targetLabel(id) }}
              </NTag>
            </div>
            <div style="font-size: 14px">
              协议 <b>{{ site.protocol }}</b>，端口 <b>{{ site.ports }}</b>，地址族 <b>{{ site.address_family }}</b>
            </div>
          </NSpace>
        </NCard>
      </NSpace>
    </NCard>

    <NCard title="站点运行状态" size="small" style="margin-top: 16px">
      <NAlert v-if="statusError" type="error" :show-icon="false" style="margin-bottom: 12px">
        读取站点状态失败：{{ statusError }}
      </NAlert>
      <div v-if="statusSites.length === 0" style="color: #888">暂无站点运行状态记录。</div>
      <NSpace v-else vertical size="large">
        <div v-for="st in statusSites" :key="st.zone_id" style="border-bottom: 1px solid #eee; padding-bottom: 10px">
          <div style="font-family: monospace; font-weight: 600">{{ st.zone_id }}</div>
          <div style="font-size: 14px; line-height: 1.9">
            当前版本 <b>{{ versionLabel(st.current_version) }}</b>，
            待更新版本 <b>{{ versionLabel(st.pending_version) }}</b>；
            <span v-if="!st.pending_version && st.transition_pending_version">
              正在追踪版本 <b>{{ st.transition_pending_version }}</b>；
            </span>
            最近查询 {{ timeLabel(st.last_query_at) }}。
          </div>
          <div style="font-size: 14px; line-height: 1.9">
            覆盖：{{ coverageLabel(st) }}；确认：<b>{{ confirmationLabel(st) }}</b>；
            查询错误：{{ st.last_query_error || '无' }}。
          </div>
          <div v-if="st.protected_cidrs?.length" style="font-size: 14px">
            过渡保护：{{ st.protected_cidrs.join(', ') }}
          </div>
          <div v-if="st.transition_error" style="font-size: 14px; color: #d03050">
            过渡记录：{{ st.transition_error }}
          </div>
        </div>
      </NSpace>
    </NCard>

    <NModal v-model:show="showForm" preset="card" :title="editingZone ? '编辑 EdgeOne 站点' : '添加 EdgeOne 站点'" style="width: min(680px, calc(100vw - 32px))">
      <NForm label-placement="top">
        <NFormItem label="ZoneId">
          <NInput v-model:value="form.zone_id" :disabled="!!editingZone" placeholder="zone-example" />
        </NFormItem>
        <NFormItem label="绑定目标">
          <NSelect
            multiple
            :value="form.target_ids"
            :options="targetOptions"
            placeholder="至少选择一个目标"
            @update:value="(value: number[] | null) => form.target_ids = (value || []).map(Number)"
          />
        </NFormItem>
        <NFormItem label="协议">
          <NSelect v-model:value="form.protocol" :options="protocolOptions" />
        </NFormItem>
        <NFormItem label="端口">
          <NInput v-model:value="form.ports" placeholder="80,443 或 8000-8010；ICMP 固定 ALL" />
        </NFormItem>
        <NFormItem label="地址族">
          <NSelect v-model:value="form.address_family" :options="familyOptions" />
        </NFormItem>
        <NAlert v-if="formError" type="error" :show-icon="false" style="margin-bottom: 12px">{{ formError }}</NAlert>
        <NSpace justify="end">
          <NButton size="large" :disabled="saving" @click="showForm = false">取消</NButton>
          <NButton type="primary" size="large" :loading="saving" :disabled="saving" @click="saveSite">保存</NButton>
        </NSpace>
      </NForm>
    </NModal>

    <NModal v-model:show="showDelete" preset="card" title="删除 EdgeOne 站点" style="width: 420px">
      <p style="line-height: 1.7">
        将删除站点 <b style="font-family: monospace">{{ deleteZone }}</b> 及其目标绑定。该操作不直接修改云防火墙规则。
      </p>
      <NSpace justify="end">
        <NButton size="large" :disabled="deleting" @click="showDelete = false">取消</NButton>
        <NButton type="error" size="large" :loading="deleting" :disabled="deleting" @click="confirmDelete">确认删除</NButton>
      </NSpace>
    </NModal>
  </div>
</template>
