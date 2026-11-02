// useSettings 共享设置逻辑：主题与凭据状态
// 消费方：App.vue（主题切换）、Dashboard.vue（首次引导）、Targets.vue（凭据缺失提示）
// 模块级单例状态：多组件共享一份 /api/settings 数据，避免重复请求
import { ref } from 'vue'
import { request } from '../api'

const settings = ref<Record<string, string>>({})
const loaded = ref(false)
const loading = ref(false)

// 主题状态（light / dark，默认 light）
const theme = ref<'light' | 'dark'>('light')

// 凭据状态（云厂商 Key 是否已配置）
const tcReady = ref(false)
const aliReady = ref(false)

// 加载设置（幂等：已加载或加载中不重复请求；失败静默，下次调用重试）
async function load() {
  if (loaded.value || loading.value) return
  loading.value = true
  try {
    settings.value = await request<Record<string, string>>('/api/settings')
    loaded.value = true
    refreshCredentialState()
  } catch { /* 失败静默 */ } finally {
    loading.value = false
  }
}

// 强制重新拉取设置（凭据配置变更等需要新鲜数据的场景）
// 与 load 的幂等缓存不同：每次真实请求；失败保留旧数据
async function refresh() {
  loading.value = true
  try {
    settings.value = await request<Record<string, string>>('/api/settings')
    loaded.value = true
    refreshCredentialState()
  } catch { /* 失败静默，保留旧数据 */ } finally {
    loading.value = false
  }
}

// 应用主题：从 DB 读取并同步到 theme ref
async function applyTheme() {
  await load()
  theme.value = (settings.value.theme as 'light' | 'dark') || 'light'
}

// 切换主题：即时生效 + 持久化到 DB（失败不回滚 UI，下次进入页面以 DB 为准）
async function setTheme(v: 'light' | 'dark') {
  theme.value = v
  try {
    await request('/api/settings', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ theme: v }),
    })
    settings.value.theme = v
  } catch { /* 持久化失败仅跳过 */ }
}

// 刷新凭据状态（基于已加载的 settings）
function refreshCredentialState() {
  tcReady.value = !!(settings.value.tc_access_id && settings.value.tc_access_key)
  aliReady.value = !!(settings.value.ali_access_id && settings.value.ali_access_key)
}

export function useSettings() {
  return { theme, tcReady, aliReady, refresh, applyTheme, setTheme }
}
