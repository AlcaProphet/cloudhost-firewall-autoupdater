<script setup lang="ts">
import { NForm, NFormItem, NInput, NSelect, NButton, NSpace, NCard, NGrid, NGi, NModal, useMessage, useThemeVars } from 'naive-ui'
import { ref, reactive, computed, onMounted } from 'vue'
import { request } from '../api'
import { useZones } from '../composables/useZones'
import { useScannedResources } from '../composables/useScannedResources'

const settings = ref<Record<string, string>>({})
const message = useMessage()
// 主题感知变量：明暗模式下文字/分隔线颜色自动切换（修复暗色模式扫描结果不可读）
const themeVars = useThemeVars()

// 间隔格式校验（如 30s、5m、1h、500ms），保存前拦截非法值
const intervalPattern = /^\d+(ms|s|m|h)$/

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
const clearTarget = ref<'tc' | 'ali' | null>(null)

const clearTitle = computed(() => (clearTarget.value === 'tc' ? '清空腾讯云扫描结果' : '清空阿里云扫描结果'))
const clearDesc = computed(() =>
  clearTarget.value === 'tc'
    ? '将清空腾讯云（轻量云 + CVM）的全部扫描结果，添加目标时将不再提供这些资源的自动补全。此操作不可恢复，确认继续？'
    : '将清空阿里云（轻量云 + ECS）的全部扫描结果，添加目标时将不再提供这些资源的自动补全。此操作不可恢复，确认继续？'
)

async function confirmClearScan() {
  const products = clearTarget.value === 'tc' ? ['tc_lighthouse', 'tc_cvm'] : ['ali_swas', 'ali_ecs']
  clearConfirm.value = false
  for (const ct of products) {
    await clearResources(ct)
  }
  message.success('扫描结果已清空')
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

async function save() {
  if (!intervalPattern.test(String(settings.value.interval || ''))) {
    message.error('同步间隔格式无效，示例：30s / 5m / 1h')
    return
  }
  try {
    await request('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(settings.value),
    })
    message.success('保存成功')
  } catch (e: any) {
    message.error(`保存失败: ${e.message}`) // 修复：非 2xx 不再误报成功
  }
}

// ─── 导出配置（卡片式确认后下载；配置文件不含凭据，也不是 SQLite 在线备份） ───
const showExportConfirm = ref(false)

function doExport() {
  showExportConfirm.value = false
  window.open('/api/config/export', '_blank')
}

// ─── 导入配置（选中文件解析后弹卡片式确认，确认后执行；导入会清空现有凭据） ───
const showImportConfirm = ref(false)
const pendingImport = ref<any>(null)

async function importConfig(e: Event) {
  const input = e.target as HTMLInputElement
  const file = input.files?.[0]
  if (!file) return
  // 先解析 JSON（独立 try/catch：格式错误与请求错误提示分离）
  let data: any
  try {
    data = JSON.parse(await file.text())
  } catch {
    message.error('JSON 格式错误')
    input.value = ''
    return
  }
  pendingImport.value = data
  showImportConfirm.value = true
  input.value = '' // 重置文件选择，允许再次选择同一文件
}

async function confirmImport() {
  showImportConfirm.value = false
  const data = pendingImport.value
  if (!data) return
  try {
    await request('/api/config/import', {
      method: 'POST',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify(data),
    })
    message.success('导入成功')
  } catch (err: any) {
    message.error(`导入失败: ${err.message}`) // RequestError 携带后端 error 信息
    return
  }
  // 导入成功后刷新设置表单（失败不影响导入结果）
  try {
    settings.value = await request<Record<string, string>>('/api/settings')
  } catch (err: any) {
    message.error(`导入成功，但刷新设置失败: ${err.message}`)
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
            <NButton type="primary" size="large" :loading="tcScan.loading" @click="runScan(tcScan)">扫描资源</NButton>
            <NButton type="error" tertiary size="large" @click="clearTarget = 'tc'; clearConfirm = true">清空</NButton>
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
            <NButton type="primary" size="large" :loading="aliScan.loading" @click="runScan(aliScan)">扫描资源</NButton>
            <NButton type="error" tertiary size="large" @click="clearTarget = 'ali'; clearConfirm = true">清空</NButton>
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
            <NInput v-model:value="settings.interval" placeholder="5m（30s / 5m / 1h）" />
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
          <NButton type="primary" size="large" @click="save">保存</NButton>
          <!-- 导出确认（卡片式弹窗） -->
          <NButton size="large" @click="showExportConfirm = true">导出配置</NButton>
          <label>
            <NButton tag="span" size="large">导入配置</NButton>
            <input type="file" accept=".json" style="display: none" @change="importConfig" />
          </label>
          <!-- 清空所有数据（卡片式确认弹窗） -->
          <NButton type="error" tertiary size="large" @click="showResetConfirm = true">清空所有数据</NButton>
        </NSpace>
      </NFormItem>
    </NForm>

    <!-- 导出确认弹窗 -->
    <NModal v-model:show="showExportConfirm" preset="card" title="确认导出配置" style="width: 420px">
      <p style="margin: 0 0 16px; line-height: 1.7">
        配置文件不包含云厂商凭据（安全设计），后续导入恢复后需重新填写凭据；它是目标、规则与设置的配置迁移方式，不是 SQLite 数据库备份。是否继续导出？
      </p>
      <NSpace justify="end">
        <NButton size="large" @click="showExportConfirm = false">取消</NButton>
        <NButton type="primary" size="large" @click="doExport">确认导出</NButton>
      </NSpace>
    </NModal>

    <!-- 导入确认弹窗 -->
    <NModal v-model:show="showImportConfirm" preset="card" title="确认导入配置" style="width: 420px">
      <p style="margin: 0 0 16px; line-height: 1.7">
        导入将替换当前全部目标、规则与设置，并清空现有云厂商凭据（配置文件中不含凭据），导入完成后需重新填写凭据。确认继续？
      </p>
      <NSpace justify="end">
        <NButton size="large" @click="showImportConfirm = false">取消</NButton>
        <NButton type="primary" size="large" @click="confirmImport">确认导入</NButton>
      </NSpace>
    </NModal>

    <!-- 清空扫描结果确认弹窗（厂商级，红色警告按钮） -->
    <NModal v-model:show="clearConfirm" preset="card" :title="clearTitle" style="width: 420px">
      <p style="margin: 0 0 16px; line-height: 1.7">
        {{ clearDesc }}
      </p>
      <NSpace justify="end">
        <NButton size="large" @click="clearConfirm = false">取消</NButton>
        <NButton type="error" size="large" @click="confirmClearScan">确认清空</NButton>
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
