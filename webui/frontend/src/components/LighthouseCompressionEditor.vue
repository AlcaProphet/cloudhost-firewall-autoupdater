<script setup lang="ts">
import { NAlert, NButton, NCard, NFormItem, NInputNumber, NModal, NSpace, NSwitch } from 'naive-ui'
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'
import type { EdgeOneSite, LighthouseCompression, TargetConfig } from '../types'
const props = defineProps<{ target: TargetConfig; disabled?: boolean }>()
const router = useRouter()
const emit = defineEmits<{ busy: [value: boolean] }>()
const config = ref<LighthouseCompression | null>(null)
const sites = ref<EdgeOneSite[]>([])
const enabled = ref(false)
const allowance = ref<number | null>(null)
const loading = ref(false), saving = ref(false), confirming = ref(false)
const error = ref(''), notice = ref('')
let active = true
onUnmounted(() => { active = false })
const boundSites = computed(() => sites.value.filter(s => s.target_ids.some(id => config.value?.target_ids?.includes(id))))
async function load() {
  if (loading.value || saving.value) return
  loading.value = true
  error.value = ''
  config.value = null
  try {
    const [data, bindings] = await Promise.all([
      request<LighthouseCompression>(`/api/targets/${props.target.id}/lighthouse-compression`),
      request<{ sites: EdgeOneSite[] }>('/api/edgeone/sites'),
    ])
    if (!active) return
    config.value = data
    sites.value = bindings.sites
    enabled.value = data.enabled
    allowance.value = data.extra_ipv4_allow
  } catch (e: any) { if (active) error.value = `读取实例设置失败：${e.message}` }
  finally { if (active) loading.value = false }
}
function requestSave() {
  if (!active || props.disabled || !config.value || loading.value || saving.value || confirming.value) return
  if (allowance.value === null || !Number.isSafeInteger(allowance.value) || allowance.value < 0 || allowance.value > 4294967296) {
    error.value = '请明确输入 0～4294967296 的整数；0 表示只允许无损压缩'
    return
  }
  error.value = ''
  if (enabled.value) confirming.value = true
  else save()
}
async function save() {
  if (!active || props.disabled || !config.value || saving.value) return
  saving.value = true
  emit('busy', true)
  confirming.value = false
  notice.value = ''
  try {
    const data = await request<LighthouseCompression>(`/api/targets/${props.target.id}/lighthouse-compression`, {
      method: 'PUT', headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({ enabled: enabled.value, extra_ipv4_allow: allowance.value }),
    })
    if (!active) return
    config.value = { ...data, target_ids: config.value.target_ids }
    notice.value = '实例压缩设置已保存，全部别名及绑定站点共用。关闭开关不会立即撤销云端放行；请查看模拟与后续正式回读。'
  } catch (e: any) { if (active) error.value = `未能确认设置保存结果，请重新读取核对：${e.message}` }
  finally { emit('busy', false); if (active) saving.value = false }
}
onMounted(load)
</script>
<template>
  <NCard title="EdgeOne 防火墙策略压缩" size="small" style="margin-top: 16px">
    <p>物理实例：{{ target.region }} · {{ target.resource_id }}；别名 {{ config?.target_ids?.join(', ') || '尚未读取' }}。实例设置单独保存。</p>
    <NAlert v-if="error" type="error" style="margin-bottom: 12px">{{ error }}</NAlert>
    <NAlert v-if="notice" type="info" style="margin-bottom: 12px">{{ notice }}</NAlert>
    <NAlert v-if="config?.pending_review" type="warning">关闭后待复查（{{ config.pending_since }}）。云端旧放行可能仍有残留，须由正式回读证明已收窄。</NAlert>
    <NFormItem label="策略压缩"><NSwitch v-model:value="enabled" :disabled="disabled || loading || saving || !config" /></NFormItem>
    <NFormItem label="额外放行 IPv4 上限（0 表示只允许无损）">
      <NInputNumber v-model:value="allowance" :min="0" :max="4294967296" :precision="0" :disabled="disabled || loading || saving || !config" style="width: 100%" />
    </NFormItem>
    <p>常态最多 96 条，固定预留 4 条；仅有安全回收证据的换版增量可临时到 100 条。额外地址可能绕过 EdgeOne 直接访问源站配置的协议和端口；关闭或收窄额度后仍可能保留旧放行。</p>
    <NSpace>
      <NButton size="large" type="primary" :disabled="disabled || loading || saving || !config" :loading="saving" @click="requestSave">保存实例压缩设置</NButton>
      <NButton size="large" :disabled="loading || saving" @click="load">重新读取</NButton>
      <NButton size="large" @click="router.push('/dry-run')">查看模拟</NButton>
    </NSpace>
    <NModal v-model:show="confirming" preset="card" title="确认启用实例策略压缩" style="width: min(620px, calc(100vw - 32px))">
      <p>实例：{{ target.region }} · {{ target.resource_id }}；额外放行 IPv4 上限：{{ allowance }}。</p>
      <p v-for="site in boundSites" :key="site.zone_id">{{ site.zone_id }}：{{ site.protocol }} / {{ site.ports }}（{{ site.address_family }}）</p>
      <p v-if="!boundSites.length">当前没有绑定站点。</p>
      <p>尚未测得容量与放行范围，请先查看模拟结果。可能额外放行 EdgeOne 回源列表之外的 IPv4 地址，绕过 EdgeOne 直接访问上述协议和端口；换版新旧规则共存仍可能超额。</p>
      <NSpace justify="end"><NButton size="large" @click="confirming = false">取消</NButton><NButton size="large" type="error" :disabled="disabled || saving" @click="save">确认保存实例设置</NButton></NSpace>
    </NModal>
  </NCard>
</template>
