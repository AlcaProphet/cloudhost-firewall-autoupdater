<script setup lang="ts">
import { NForm, NFormItem, NInput, NSelect, NButton, NSpace, NCard, NGrid, NGi, NModal, useMessage, useThemeVars } from 'naive-ui'
import { ref, reactive, computed, onMounted, onUnmounted } from 'vue'
import { request, RequestError, type MissingFieldsDetails } from '../api'
import { useZones } from '../composables/useZones'
import { useScannedResources } from '../composables/useScannedResources'

const settings = ref<Record<string, string>>({})
const saving = ref(false)
let pageActive = true
let importReloadTimer: ReturnType<typeof setTimeout> | undefined
onUnmounted(() => {
  pageActive = false
  if (importReloadTimer !== undefined) clearTimeout(importReloadTimer)
  pendingImport.value = null
})
const message = useMessage()
// 主题感知变量：明暗模式下文字/分隔线颜色自动切换（修复暗色模式扫描结果不可读）
const themeVars = useThemeVars()

// ─── 扫描资源（卡片内云产品 + 地域选择 → 扫描 → 已扫描资源列表） ───
const { load: loadZones, regionOptions } = useZones()
const { load: loadScanned, scan: scanResources, clear: clearResources, resourcesOf } = useScannedResources()

// product/region 初始为 null：选择框显示占位提示（选择云产品 / 选择地域）
// hasScanned：厂商级状态（任一产品扫描成功即置 true，用于空态区分）
const tcScan = reactive({
  product: null as string | null, region: null as string | null,
  loading: false, error: '', hasScanned: false,
})
const aliScan = reactive({
  product: null as string | null, region: null as string | null,
  loading: false, error: '', hasScanned: false,
})

const tcProductOptions = [
  { label: '腾讯云轻量云', value: 'tc_lighthouse' },
  { label: '腾讯云CVM', value: 'tc_cvm' },
]
const aliProductOptions = [
  { label: '阿里云轻量云', value: 'ali_swas' },
  { label: '阿里云ECS', value: 'ali_ecs' },
]

// 已扫描资源区：按厂商聚合全部产品（轻量云在前、CVM/ECS 在后），不随选项框当前选择变化
const tcAllScanned = computed(() => [
  ...resourcesOf('tc_lighthouse'),
  ...resourcesOf('tc_cvm'),
])
const aliAllScanned = computed(() => [
  ...resourcesOf('ali_swas'),
  ...resourcesOf('ali_ecs'),
])

// 空态三态（厂商级）：未扫描 → 引导；已扫描且 0 条 → 未找到；有记录 → 列表
const tcEmptyHint = computed(() => (tcScan.hasScanned ? '未找到资源，请尝试切换产品或地域' : '暂无扫描结果，选择云产品与地域后点击「扫描资源」'))
const aliEmptyHint = computed(() => (aliScan.hasScanned ? '未找到资源，请尝试切换产品或地域' : '暂无扫描结果，选择云产品与地域后点击「扫描资源」'))

// 产品类型短标签（表格列宽紧凑展示用）：轻量云类统称「轻量云」，CVM/ECS 用英文缩写
function shortProductLabel(ct: string): string {
  const m: Record<string, string> = { tc_lighthouse: '轻量云', tc_cvm: 'CVM', ali_swas: '轻量云', ali_ecs: 'ECS' }
  return m[ct] || ct
}

onMounted(async () => {
  try {
    settings.value = await request<Record<string, string>>('/api/settings')
  } catch (e: any) {
    message.error(`加载设置失败: ${e.message}`)
  }
  loadZones()
  // 预加载四类扫描结果（含重启后 DB 持久化数据）
  await Promise.all(['tc_lighthouse', 'tc_cvm', 'ali_swas', 'ali_ecs'].map((ct) => loadScanned(ct)))
})

async function runScan(s: typeof tcScan) {
  if (!pageActive || clearing.value || s.loading) return
  if (!s.product || !s.region) {
    message.warning('请先选择云产品与地域')
    return
  }
  s.loading = true
  s.error = ''
  const err = await scanResources(s.product, s.region)
  if (err) {
    s.error = err
  } else {
    s.hasScanned = true // 扫描成功（含 0 条）→ 厂商级进入"未找到资源"提示态
  }
  s.loading = false
}

// ─── 清空扫描结果（卡片式确认后执行；清空该厂商全部产品，与「已扫描的资源」展示范围一致） ───
const clearConfirm = ref(false)
const clearing = ref(false)
const clearTarget = ref<'tc' | 'ali' | null>(null)

const clearTitle = computed(() => (clearTarget.value === 'tc' ? '清空腾讯云扫描结果' : '清空阿里云扫描结果'))
const clearDesc = computed(() =>
  clearTarget.value === 'tc'
    ? '将清空腾讯云（轻量云 + CVM）的全部扫描结果，添加目标时将不再提供这些资源的自动补全。此操作不可恢复，确认继续？'
    : '将清空阿里云（轻量云 + ECS）的全部扫描结果，添加目标时将不再提供这些资源的自动补全。此操作不可恢复，确认继续？'
)

// 两个产品独立清空；部分结果未确认时保留失败产品缓存，不自动重试。
async function confirmClearScan() {
  if (!pageActive || clearing.value || !clearConfirm.value || !clearTarget.value) return
  const scanState = clearTarget.value === 'tc' ? tcScan : aliScan
  if (scanState.loading) {
    message.warning('请等待该厂商扫描完成后再清空')
    return
  }
  const products = clearTarget.value === 'tc' ? ['tc_lighthouse', 'tc_cvm'] : ['ali_swas', 'ali_ecs']
  clearing.value = true
  try {
    const results = await Promise.allSettled(products.map(ct => clearResources(ct)))
    if (!pageActive) return
    const failures = results.filter(result => result.status === 'rejected').length
    clearConfirm.value = false
    if (failures === 0) {
      scanState.hasScanned = false
      message.success('扫描结果已清空')
    } else {
      message.error(failures === products.length
        ? '未能确认扫描结果已清空，请刷新页面核对；不会自动重试'
        : '仅部分产品确认清空，其他产品未能确认，请刷新页面核对；不会自动重试')
    }
  } finally {
    clearing.value = false
  }
}

// ─── 清空所有数据（重新初始化，卡片式确认） ───
const showResetConfirm = ref(false)

async function resetAll() {
  showResetConfirm.value = false
  try {
    const data = await request<{ message: string }>('/api/config/reset', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({}),
    })
    message.success(data.message || '数据已清空')
    // 刷新页面回到全新初始化状态（避免表单组件残留旧值）
    setTimeout(() => window.location.reload(), 800)
  } catch (e: any) {
    message.error(`清空失败: ${e.message}`)
  }
}

// 设置页只提交十个可见字段；theme 由侧边栏单独更新，后端仍保留十一字段部分更新 DTO
const editableKeys = [
  'tc_access_id', 'tc_access_key', 'ali_access_id', 'ali_access_key',
  'tag', 'interval', 'dns', 'dns_timeout', 'dns_fail_threshold', 'log_level',
] as const

// buildSettingsPayload 只构造当前设置页可见字段的保存 payload
// 不再整体回传 GET 响应对象，避免把数据库中的保留键/未知键提交给后端
function buildSettingsPayload(src: Record<string, string>): Record<string, string> {
  const payload: Record<string, string> = {}
  for (const key of editableKeys) {
    const value = src[key]
    if (value !== undefined) payload[key] = value
  }
  return payload
}

// 时长语法与正数校验交由后端统一处理，保留用户输入供错误后修改。
async function save() {
  if (!pageActive || saving.value) return
  saving.value = true
  try {
    await request('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(buildSettingsPayload(settings.value)),
    })
    if (pageActive) message.success('保存成功')
  } catch (e: any) {
    if (pageActive) message.error(`保存失败: ${e.message}`)
  } finally {
    saving.value = false
  }
}

// ─── 配置导入导出（version 3 完整敏感快照，Build6 Step 5 / Build7 §4.3） ───
//
// 安全边界：配置包是**明文完整敏感快照**（含腾讯云/阿里云密钥、SMTP 密码、
// Webhook URL 与 Uptime Kuma Push URL），安全等级等同于生产 Secret 或 SQLite 数据库备份。
// 因此导出使用 POST + fetch + Blob，不把响应交给通用 JSON 请求封装，
// 不在 console 输出响应体；文件名从 Content-Disposition 解析，不可用时用固定安全名。

// 导出响应不可用时的固定安全文件名
const EXPORT_FALLBACK_FILENAME = 'fwalizer-config-v3.json'

// parseAttachmentFilename 从 Content-Disposition 中提取安全文件名。
//
// 只接受形如 attachment; filename="..." 的值，并剥离任何路径分隔符与
// 控制字符，避免服务端/中间层注入不安全的文件名。解析失败返回 fallback。
function parseAttachmentFilename(header: string | null): string {
  if (!header) return EXPORT_FALLBACK_FILENAME
  const match = /filename="([^"]+)"/i.exec(header)
  if (!match) return EXPORT_FALLBACK_FILENAME
  const raw = match[1].split(/[\\/]/).pop() || ''
  const safe = raw.replace(/[\x00-\x1f\x7f]/g, '').trim()
  if (!safe || !/^[\w.-]+$/.test(safe)) return EXPORT_FALLBACK_FILENAME
  return safe
}

const showExportConfirm = ref(false)
const exporting = ref(false)

async function doExport() {
  showExportConfirm.value = false
  exporting.value = true
  try {
    const res = await fetch('/api/config/export', { method: 'POST' })
    if (!res.ok) {
      let detail = ''
      try {
        const data = await res.json()
        detail = data?.error || ''
      } catch {
        /* 非 JSON 错误响应：只用状态码 */
      }
      throw new Error(detail || `请求失败 (${res.status})`)
    }
    const blob = await res.blob()
    const filename = parseAttachmentFilename(res.headers.get('Content-Disposition'))
    const url = URL.createObjectURL(blob)
    try {
      const a = document.createElement('a')
      a.href = url
      a.download = filename
      document.body.appendChild(a)
      a.click()
      a.remove()
    } finally {
      URL.revokeObjectURL(url)
    }
    message.success('配置已导出，请妥善保管（含全部密钥）')
  } catch (e: any) {
    message.error(`导出失败: ${e.message}`)
  } finally {
    exporting.value = false
  }
}

const showImportConfirm = ref(false)
// 预检查不改写上传内容，保留原始重复字段与编码供后端严格解码。
const pendingImport = ref<File | null>(null)
const importFilename = ref('')
const readingImport = ref(false)
const importing = ref(false)
const showImportFailure = ref(false)
const importFailure = ref<MissingFieldsDetails | null>(null)
function cancelImport() {
  showImportConfirm.value = false
  pendingImport.value = null
}
async function importConfig(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  input.value = ''
  if (!file || !pageActive || readingImport.value || importing.value) return
  readingImport.value = true
  pendingImport.value = null
  showImportConfirm.value = false
  showImportFailure.value = false
  importFailure.value = null
  try {
    // 仅作本地语法预检查；实际 POST 直接发送 File 原始字节。
    JSON.parse(await file.text())
    if (!pageActive) return
    pendingImport.value = file
    importFilename.value = file.name
    showImportConfirm.value = true
  } catch {
    if (pageActive) message.error('JSON 格式错误')
  } finally {
    readingImport.value = false
  }
}
async function confirmImport() {
  if (!pageActive || readingImport.value || importing.value || !showImportConfirm.value || !pendingImport.value) return
  const file = pendingImport.value
  pendingImport.value = null
  showImportConfirm.value = false
  showImportFailure.value = false
  importFailure.value = null
  importing.value = true
  let accepted = false
  try {
    await request('/api/config/import', {
      method: 'POST', headers: { 'Content-Type': 'application/json' }, body: file,
    })
    if (!pageActive) return
    accepted = true
    message.success('导入成功，正在刷新页面…')
    importReloadTimer = setTimeout(() => { if (pageActive) window.location.reload() }, 600)
  } catch (err: any) {
    if (!pageActive) return
    if (err instanceof RequestError && err.missingFields) {
      importFailure.value = err.missingFields
      showImportFailure.value = true
    } else {
      message.error(`导入失败: ${err.message}`)
    }
  } finally {
    if (!accepted) importing.value = false
  }
}

</script>

<template>
  <div>
    <h2>全局设置</h2>

    <!-- 云厂商凭据卡片：并排 2 列网格（参照仪表盘布局）；卡片内 label 置顶避免长文本换行挤占 -->
    <NGrid :cols="2" :x-gap="16" :y-gap="16">
      <NGi>
        <NCard title="腾讯云凭据" size="small">
          <NForm label-placement="top">
            <NFormItem label="SecretId">
              <NInput v-model:value="settings.tc_access_id" type="password" show-password-on="click" placeholder="AKIDxxx" />
            </NFormItem>
            <NFormItem label="SecretKey">
              <NInput v-model:value="settings.tc_access_key" type="password" show-password-on="click" placeholder="SecretKey" />
            </NFormItem>
          </NForm>
          <!-- 扫描操作区 -->
          <NSpace align="center" style="margin-bottom: 8px">
            <NSelect size="large" v-model:value="tcScan.product" :options="tcProductOptions" placeholder="选择云产品" style="width: 150px" />
            <NSelect size="large" v-model:value="tcScan.region" :options="regionOptions(tcScan.product || '')" filterable tag clearable placeholder="选择地域" style="width: 190px" />
            <NButton type="primary" size="large" :loading="tcScan.loading" :disabled="clearing" @click="runScan(tcScan)">扫描资源</NButton>
            <NButton type="error" tertiary size="large" :disabled="clearing || tcScan.loading" @click="clearTarget = 'tc'; clearConfirm = true">清空</NButton>
          </NSpace>
          <p v-if="tcScan.error" style="color: #d03050; font-size: 14px; margin: 0 0 8px">{{ tcScan.error }}</p>
          <!-- 已扫描资源区（厂商聚合：轻量云 + CVM） -->
          <div :style="{ borderTop: `1px solid ${themeVars.dividerColor}`, paddingTop: '8px' }">
            <div :style="{ fontSize: '14px', fontWeight: 600, marginBottom: '6px', color: themeVars.textColor1 }">已扫描的资源</div>
            <div v-if="tcAllScanned.length">
              <div :style="{ display: 'flex', gap: '8px', fontSize: '12px', color: themeVars.textColor3, padding: '2px 0' }">
                <span style="flex: 0 0 64px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">产品类型</span>
                <span style="flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">资源名称</span>
                <span style="flex: 1.6; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">资源ID</span>
                <span style="flex: 0 0 84px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">地域</span>
              </div>
              <div v-for="r in tcAllScanned" :key="r.id" :style="{ display: 'flex', gap: '8px', fontSize: '12px', padding: '3px 0', color: themeVars.textColor1 }">
                <span style="flex: 0 0 64px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ shortProductLabel(r.cloud_type) }}</span>
                <span style="flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.resource_name || '-' }}</span>
                <span style="flex: 1.6; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.resource_id }}</span>
                <span style="flex: 0 0 84px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.region }}</span>
              </div>
            </div>
            <div v-else :style="{ color: themeVars.textColor3, fontSize: '14px' }">{{ tcEmptyHint }}</div>
          </div>
        </NCard>
      </NGi>

      <NGi>
        <NCard title="阿里云凭据" size="small">
          <NForm label-placement="top">
            <NFormItem label="AccessKeyId">
              <NInput v-model:value="settings.ali_access_id" type="password" show-password-on="click" placeholder="LTAIxxx" />
            </NFormItem>
            <NFormItem label="AccessKeySecret">
              <NInput v-model:value="settings.ali_access_key" type="password" show-password-on="click" placeholder="AccessKeySecret" />
            </NFormItem>
          </NForm>
          <!-- 扫描操作区 -->
          <NSpace align="center" style="margin-bottom: 8px">
            <NSelect size="large" v-model:value="aliScan.product" :options="aliProductOptions" placeholder="选择云产品" style="width: 150px" />
            <NSelect size="large" v-model:value="aliScan.region" :options="regionOptions(aliScan.product || '')" filterable tag clearable placeholder="选择地域" style="width: 190px" />
            <NButton type="primary" size="large" :loading="aliScan.loading" :disabled="clearing" @click="runScan(aliScan)">扫描资源</NButton>
            <NButton type="error" tertiary size="large" :disabled="clearing || aliScan.loading" @click="clearTarget = 'ali'; clearConfirm = true">清空</NButton>
          </NSpace>
          <p v-if="aliScan.error" style="color: #d03050; font-size: 14px; margin: 0 0 8px">{{ aliScan.error }}</p>
          <!-- 已扫描资源区（厂商聚合：轻量云 + ECS） -->
          <div :style="{ borderTop: `1px solid ${themeVars.dividerColor}`, paddingTop: '8px' }">
            <div :style="{ fontSize: '14px', fontWeight: 600, marginBottom: '6px', color: themeVars.textColor1 }">已扫描的资源</div>
            <div v-if="aliAllScanned.length">
              <div :style="{ display: 'flex', gap: '8px', fontSize: '12px', color: themeVars.textColor3, padding: '2px 0' }">
                <span style="flex: 0 0 64px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">产品类型</span>
                <span style="flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">资源名称</span>
                <span style="flex: 1.6; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">资源ID</span>
                <span style="flex: 0 0 84px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">地域</span>
              </div>
              <div v-for="r in aliAllScanned" :key="r.id" :style="{ display: 'flex', gap: '8px', fontSize: '12px', padding: '3px 0', color: themeVars.textColor1 }">
                <span style="flex: 0 0 64px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ shortProductLabel(r.cloud_type) }}</span>
                <span style="flex: 1; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.resource_name || '-' }}</span>
                <span style="flex: 1.6; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.resource_id }}</span>
                <span style="flex: 0 0 84px; min-width: 0; overflow: hidden; text-overflow: ellipsis; white-space: nowrap">{{ r.region }}</span>
              </div>
            </div>
            <div v-else :style="{ color: themeVars.textColor3, fontSize: '14px' }">{{ aliEmptyHint }}</div>
          </div>
        </NCard>
      </NGi>
    </NGrid>

    <h3 style="margin: 16px 0 12px">全局设置</h3>
    <NForm :model="settings" label-placement="left" label-width="120">
      <NGrid :cols="2" :x-gap="16">
        <NGi>
          <NFormItem label="TAG">
            <NInput v-model:value="settings.tag" />
          </NFormItem>
        </NGi>
        <NGi>
          <NFormItem label="同步间隔">
            <NInput v-model:value="settings.interval" placeholder="5m（30s / 1h30m / 1.5h）" />
          </NFormItem>
        </NGi>
        <NGi>
          <NFormItem label="DNS 服务器">
            <NInput v-model:value="settings.dns" />
          </NFormItem>
        </NGi>
        <NGi>
          <NFormItem label="DNS 超时">
            <NInput v-model:value="settings.dns_timeout" placeholder="10s" />
          </NFormItem>
        </NGi>
        <NGi>
          <NFormItem label="DNS 失败阈值">
            <NInput v-model:value="settings.dns_fail_threshold" placeholder="5" />
          </NFormItem>
        </NGi>
        <NGi>
          <NFormItem label="日志级别">
            <NSelect v-model:value="settings.log_level" :options="[
              { label: 'Debug', value: 'debug' },
              { label: 'Info', value: 'info' },
              { label: 'Warn', value: 'warn' },
              { label: 'Error', value: 'error' },
            ]" />
          </NFormItem>
        </NGi>
      </NGrid>
      <NFormItem>
        <NSpace>
          <NButton type="primary" size="large" :disabled="saving" :loading="saving" @click="save">保存</NButton>
          <!-- 导出确认（卡片式弹窗） -->
          <NButton size="large" @click="showExportConfirm = true">导出配置</NButton>
          <label>
            <NButton tag="span" size="large" :disabled="readingImport || importing" :loading="readingImport || importing">导入配置</NButton>
            <input type="file" accept=".json" :disabled="readingImport || importing" style="display: none" @change="importConfig" />
          </label>
          <!-- 清空所有数据（卡片式确认弹窗） -->
          <NButton type="error" tertiary size="large" @click="showResetConfirm = true">清空所有数据</NButton>
        </NSpace>
      </NFormItem>
    </NForm>

    <!-- 导出确认弹窗（危险操作：配置包是明文完整敏感快照） -->
    <NModal v-model:show="showExportConfirm" preset="card" title="确认导出完整配置" style="width: 460px">
      <p style="margin: 0 0 8px; line-height: 1.7">
        导出的配置文件是<b>明文完整敏感快照</b>，包含：
      </p>
      <ul style="margin: 0 0 12px; padding-left: 20px; line-height: 1.8">
        <li>腾讯云密钥（SecretId / SecretKey）</li>
        <li>阿里云密钥（AccessKeyId / AccessKeySecret）</li>
        <li>SMTP 密码</li>
        <li>Webhook URL</li>
        <li>Uptime Kuma Push URL（含 token）</li>
      </ul>
      <p style="margin: 0 0 16px; line-height: 1.7">
        请勿提交到 Git、上传公共网盘或通过不可信渠道传输。该文件安全等级等同于生产密钥或数据库备份，
        并且不是 SQLite 在线备份。确认继续导出？
      </p>
      <NSpace justify="end">
        <NButton size="large" @click="showExportConfirm = false">取消</NButton>
        <NButton type="error" size="large" :loading="exporting" @click="doExport">确认导出</NButton>
      </NSpace>
    </NModal>

    <!-- 导入确认弹窗（危险操作：覆盖式替换全部业务配置） -->
    <NModal v-model:show="showImportConfirm" preset="card" title="确认导入完整配置" @update:show="(show) => { if (!show) cancelImport() }" style="width: 460px">
      <p style="margin: 0 0 8px; line-height: 1.7">
        导入将<b>整体覆盖</b>当前全部业务配置：目标、规则、设置、云凭据与告警。
      </p>
      <p style="margin: 0 0 12px; line-height: 1.7">
        配置包是明文完整敏感快照，包含腾讯云密钥、阿里云密钥、SMTP 密码、Webhook URL
        与 Uptime Kuma Push URL（含 token），因此也会一并覆盖现有密钥。
        导入成功后页面会自动刷新；失败时保持当前配置不变。
      </p>
      <p v-if="importFilename" style="margin: 0 0 16px; color: #d03050; line-height: 1.7">
        待导入文件：{{ importFilename }}
      </p>
      <NSpace justify="end">
        <NButton size="large" @click="cancelImport">取消</NButton>
        <NButton type="error" size="large" :disabled="importing" :loading="importing" @click="confirmImport">确认导入</NButton>
      </NSpace>
    </NModal>

    <NModal v-model:show="showImportFailure" preset="card" title="导入失败" style="width: min(640px, calc(100vw - 32px))">
      <p v-if="importFailure">配置包有 {{ importFailure.total }} 项必填内容缺失或为 null，请修正后重新导入。</p>
      <p v-if="importFailure?.truncated">当前列出前 100 项，共 {{ importFailure.total }} 项；修正后请重新导入检查。</p>
      <ol v-if="importFailure" style="max-height: 50vh; overflow-y: auto; overflow-wrap: anywhere">
        <li v-for="path in importFailure.fields" :key="path">{{ path }}</li>
      </ol>
      <NSpace justify="end"><NButton size="large" @click="showImportFailure = false">关闭</NButton></NSpace>
    </NModal>

    <!-- 清空扫描结果确认弹窗（厂商级，红色警告按钮） -->
    <NModal v-model:show="clearConfirm" preset="card" :title="clearTitle" :closable="!clearing" :mask-closable="!clearing" :close-on-esc="!clearing" style="width: 420px">
      <p style="margin: 0 0 16px; line-height: 1.7">
        {{ clearDesc }}
      </p>
      <NSpace justify="end">
        <NButton size="large" :disabled="clearing" @click="clearConfirm = false">取消</NButton>
        <NButton type="error" size="large" :disabled="clearing" :loading="clearing" @click="confirmClearScan">确认清空</NButton>
      </NSpace>
    </NModal>

    <!-- 清空所有数据确认弹窗 -->
    <NModal v-model:show="showResetConfirm" preset="card" title="清空所有数据" style="width: 420px">
      <p style="margin: 0 0 16px; line-height: 1.7">
        将清空全部目标、规则、凭据、日志与扫描结果，此操作不可恢复。确认继续？
      </p>
      <NSpace justify="end">
        <NButton size="large" @click="showResetConfirm = false">取消</NButton>
        <NButton type="error" size="large" @click="resetAll">确认清空</NButton>
      </NSpace>
    </NModal>
  </div>
</template>
