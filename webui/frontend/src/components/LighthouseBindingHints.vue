<script setup lang="ts">
import { NAlert, NButton, NSpace } from 'naive-ui'
import { computed, onUnmounted, ref, watch } from 'vue'
import { useRouter } from 'vue-router'
import { request } from '../api'
import type { LighthouseCompression, TargetConfig } from '../types'
const props = defineProps<{ targets: TargetConfig[]; targetIds: number[] }>()
const router = useRouter()
const instances = computed(() => {
  const seen = new Set<string>()
  return props.targets.filter(t => {
    if (t.cloud_type !== 'tc_lighthouse' || !props.targetIds.includes(t.id)) return false
    const key = JSON.stringify([t.cloud_type, t.region, t.resource_id])
    if (seen.has(key)) return false
    seen.add(key)
    return true
  })
})
const states = ref<Record<number, LighthouseCompression>>({})
const errors = ref<Record<number, string>>({})
let active = true, sequence = 0
onUnmounted(() => { active = false; ++sequence })
async function refresh() {
  if (!active) return
  const current = ++sequence
  states.value = {}
  errors.value = {}
  await Promise.all(instances.value.map(async t => {
    try {
      const data = await request<LighthouseCompression>(`/api/targets/${t.id}/lighthouse-compression`)
      if (active && current === sequence) states.value = { ...states.value, [t.id]: data }
    } catch (e: any) {
      if (active && current === sequence) errors.value = { ...errors.value, [t.id]: e.message }
    }
  }))
}
watch(instances, refresh, { immediate: true })
</script>
<template>
  <NAlert v-for="t in instances" :key="t.id" type="warning" :show-icon="false" style="margin-bottom: 12px; overflow-wrap: anywhere">
    <b>{{ t.region }} · {{ t.resource_id }}</b>：腾讯云轻量应用服务器每台最多 100 条防火墙规则。
    当前实例的策略压缩{{ states[t.id] ? (states[t.id].enabled ? '已开启' : '已关闭') : '状态未知（正在读取或读取失败）' }}。
    <span v-if="states[t.id]">额外放行 IPv4 上限 {{ states[t.id].extra_ipv4_allow }}；{{ states[t.id].pending_review ? '关闭后待复查，云端放行可能仍有残留。' : '' }}</span>
    启用后，规则不足时可能额外放行 EdgeOne 回源列表之外的 IPv4 地址，这些地址可能绕过 EdgeOne 直接访问源站配置的协议和端口。
    常态预留 4 条（最多 96 条）；换版期间，新旧规则同时占用名额，仍可能无法完成同步。请先查看模拟结果。保存绑定不会开启压缩，同实例别名与全部绑定站点共用设置。
    <div v-if="errors[t.id]">读取失败：{{ errors[t.id] }} <NButton text @click="refresh">重新读取</NButton></div>
    <NSpace style="margin-top: 8px">
      <NButton size="large" @click="router.push('/dry-run')">查看模拟</NButton>
      <NButton size="large" @click="router.push({ path: '/targets', query: { compression: String(t.id) } })">前往实例设置</NButton>
    </NSpace>
  </NAlert>
</template>
