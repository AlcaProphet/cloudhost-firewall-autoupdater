<script setup lang="ts">
import { NCard, NForm, NFormItem, NInput, NRadioGroup, NRadio, NSwitch, NButton, NText, NAlert, useMessage } from 'naive-ui'
import { ref, onMounted } from 'vue'
import { request } from '../api'
import type { AlertEmailConfig, AlertPolicyConfig, AlertUptimeKumaPushConfig, AlertWebhookConfig, TestEmailPayload } from '../types'

// 告警配置页（Build7 §4.7）：单页四张卡片，一次提交四个完整对象。
// 默认值：渠道、三个触发开关与 Push 全部关闭；health_timeout=10m、Push interval=60s。

type AlertsData = {
  policy: AlertPolicyConfig
  email: AlertEmailConfig
  webhook: AlertWebhookConfig
  uptime_kuma_push: AlertUptimeKumaPushConfig
}

const message = useMessage()

const policy = ref<AlertPolicyConfig>({
  dns_failed_enabled: false,
  sync_error_enabled: false,
  operational_error_enabled: false,
  health_timeout: '10m',
})

const email = ref<AlertEmailConfig>({
  enabled: false,
  host: '',
  port: '587',
  security: 'auto_starttls',
  username: '',
  password: '',
  from_addr: '',
  to_addr: '',
  subject: '[FWAlizer] 告警通知',
  body: 'FWAlizer 检测到运行异常，请检查同步日志。',
})

const webhook = ref<AlertWebhookConfig>({
  enabled: false,
  url: '',
  channel: '',
})

const push = ref<AlertUptimeKumaPushConfig>({
  enabled: false,
  url: '',
  interval: '60s',
})

const saving = ref(false)
// 只有完整配置成功加载后才能覆盖保存；默认表单仍可用于独立测试邮件。
const loadState = ref<'loading' | 'ready' | 'error'>('loading')

// 响应泛型不提供运行时校验；先检查全部字段，避免部分真实配置混入默认表单。
function isAlertsData(value: unknown): value is AlertsData {
  function hasFields(value: unknown, booleans: string[], strings: string[]): boolean {
    if (value === null || typeof value !== 'object' || Array.isArray(value)) return false
    const fields = value as Record<string, unknown>
    return booleans.every((key) => typeof fields[key] === 'boolean')
      && strings.every((key) => typeof fields[key] === 'string')
  }
  if (value === null || typeof value !== 'object' || Array.isArray(value)) return false
  const data = value as Record<string, unknown>
  return hasFields(data.policy,
    ['dns_failed_enabled', 'sync_error_enabled', 'operational_error_enabled'], ['health_timeout'])
    && hasFields(data.email, ['enabled'],
      ['host', 'port', 'security', 'username', 'password', 'from_addr', 'to_addr', 'subject', 'body'])
    && ['auto_starttls', 'starttls', 'implicit_tls'].includes((data.email as AlertEmailConfig).security)
    && hasFields(data.webhook, ['enabled'], ['url', 'channel'])
    && hasFields(data.uptime_kuma_push, ['enabled'], ['url', 'interval'])
}

// 测试发送状态（Build7 §5.3）：
// 结果只存在于本组件内存中，刷新页面即消失；不新增查询 API、SSE、轮询或发送历史。
const testing = ref(false)
const testResult = ref<{ ok: boolean; text: string } | null>(null)

// 测试发送：使用当前表单值，不触发保存；请求上限固定 35 秒。
// 成功只表述为「SMTP 服务器已接受测试邮件」，不表示已投递到收件箱。
//
// 请求体必须显式只取 9 个发送字段（Build7 §5.1）：不得直接序列化整个 email 表单对象，
// 它多一个 enabled，而后端 testEmailRequest 使用严格解码，多带字段会被 HTTP 400 拒绝。
async function testSend() {
  testing.value = true
  testResult.value = null
  try {
    const payload: TestEmailPayload = {
      host: email.value.host,
      port: email.value.port,
      security: email.value.security,
      username: email.value.username,
      password: email.value.password,
      from_addr: email.value.from_addr,
      to_addr: email.value.to_addr,
      subject: email.value.subject,
      body: email.value.body,
    }
    const data = await request<{ success: boolean; message?: string; error?: string }>(
      '/api/alerts/test-email',
      {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        body: JSON.stringify(payload),
      },
      35000,
    )
    if (data?.success) {
      testResult.value = { ok: true, text: data.message || 'SMTP 服务器已接受测试邮件' }
    } else {
      testResult.value = { ok: false, text: data?.error || '测试邮件发送失败' }
    }
  } catch (e: any) {
    testResult.value = { ok: false, text: `测试邮件发送失败: ${e.message}` }
  } finally {
    testing.value = false
  }
}

async function load() {
  loadState.value = 'loading'
  try {
    const data = await request<unknown>('/api/alerts')
    if (!isAlertsData(data)) throw new Error('告警配置响应不完整或字段类型错误')
    policy.value = data.policy
    email.value = data.email
    webhook.value = data.webhook
    push.value = data.uptime_kuma_push
    loadState.value = 'ready'
  } catch (e: any) {
    loadState.value = 'error'
    message.error(`加载告警配置失败: ${e.message}`)
  }
}

onMounted(load)

async function save() {
  if (loadState.value !== 'ready') {
    message.error('告警配置尚未成功加载，无法保存')
    return
  }
  if (saving.value) return
  if (webhook.value.enabled && webhook.value.channel === '') {
    message.error('请选择 Webhook 通知渠道')
    return
  }
  saving.value = true
  try {
    await request('/api/alerts', {
      method: 'PUT',
      headers: { 'Content-Type': 'application/json' },
      body: JSON.stringify({
        policy: policy.value,
        email: email.value,
        webhook: webhook.value,
        uptime_kuma_push: push.value,
      }),
    })
    message.success('保存成功')
  } catch (e: any) {
    message.error(`保存失败: ${e.message}`)
  } finally {
    saving.value = false
  }
}
</script>

<template>
  <div>
    <h2>告警配置</h2>

    <NAlert v-if="loadState === 'loading'" type="info" style="margin-bottom: 16px">
      正在加载告警配置，加载完成前无法保存。
    </NAlert>
    <NAlert v-else-if="loadState === 'error'" type="error" style="margin-bottom: 16px">
      告警配置未成功加载，保存已禁用，请刷新页面重试。
    </NAlert>

    <NCard title="触发条件" size="small" style="margin-bottom: 16px">
      <NForm :model="policy" label-placement="left" label-width="180">
        <NFormItem label="DNS 解析失败">
          <NSwitch v-model:value="policy.dns_failed_enabled" />
        </NFormItem>
        <NFormItem label="Provider × 域名最终失败">
          <NSwitch v-model:value="policy.sync_error_enabled" />
        </NFormItem>
        <NFormItem label="运行健康异常">
          <NSwitch v-model:value="policy.operational_error_enabled" />
        </NFormItem>
        <NFormItem label="健康超时">
          <NInput v-model:value="policy.health_timeout" placeholder="10m" style="max-width: 220px" />
        </NFormItem>
      </NForm>
      <NText depth="3" style="font-size: 14px">
        三个触发条件是邮件与 Webhook 共用的全局策略；只有渠道开关与对应触发条件同时开启才会发送通知。
      </NText>
    </NCard>

    <NCard title="邮件告警" size="small" style="margin-bottom: 16px">
      <template #header-extra>
        <NSwitch v-model:value="email.enabled" />
      </template>
      <NForm :model="email" label-placement="left" label-width="100">
        <NFormItem label="SMTP 主机">
          <NInput v-model:value="email.host" placeholder="smtp.example.com" />
        </NFormItem>
        <NFormItem label="端口">
          <NInput v-model:value="email.port" placeholder="587" />
        </NFormItem>
        <NFormItem label="安全模式">
          <NRadioGroup v-model:value="email.security">
            <NRadio value="auto_starttls">兼容模式：按需 STARTTLS</NRadio>
            <NRadio value="starttls">强制 STARTTLS</NRadio>
            <NRadio value="implicit_tls">SSL/TLS</NRadio>
          </NRadioGroup>
        </NFormItem>
        <NText depth="3">腾讯云 SMTP 常用配置：smtp.qcloudmail.com，端口 465，SSL/TLS。切换模式后请核对服务商要求的端口。</NText>
        <NFormItem label="用户名">
          <NInput v-model:value="email.username" placeholder="user@example.com" />
        </NFormItem>
        <NFormItem label="密码">
          <NInput v-model:value="email.password" type="password" show-password-on="click" placeholder="SMTP 密码" />
        </NFormItem>
        <NFormItem label="发件人">
          <NInput v-model:value="email.from_addr" placeholder="noreply@example.com" />
        </NFormItem>
        <NFormItem label="收件人">
          <NInput v-model:value="email.to_addr" placeholder="admin@example.com, ops@example.com（多人逗号分隔）" />
        </NFormItem>
        <NFormItem label="主题">
          <NInput v-model:value="email.subject" placeholder="[FWAlizer] 告警通知" />
        </NFormItem>
        <NFormItem label="正文">
          <NInput
            v-model:value="email.body"
            type="textarea"
            :autosize="{ minRows: 3, maxRows: 8 }"
            placeholder="FWAlizer 检测到运行异常，请检查同步日志。"
          />
        </NFormItem>
      </NForm>
      <NText depth="3" style="font-size: 14px">
        邮件为纯文本格式，系统会在正文后追加固定事件详情（事件类型/时间/Provider/域名/错误）；
        上方的邮件开关只控制自动通知，关闭时仍可编辑并测试发送。
      </NText>
      <div style="margin-top: 12px">
        <NButton size="large" :loading="testing" :disabled="testing" @click="testSend">测试发送邮件</NButton>
      </div>
      <div v-if="testing" style="margin-top: 12px">
        <NText depth="3">正在测试发送…</NText>
      </div>
      <div v-else-if="testResult" style="margin-top: 12px">
        <NAlert :type="testResult.ok ? 'success' : 'error'" :title="testResult.ok ? '测试结果' : '测试失败'">
          {{ testResult.text }}
        </NAlert>
      </div>
    </NCard>

    <NCard title="Webhook 告警" size="small" style="margin-bottom: 16px">
      <template #header-extra>
        <NSwitch v-model:value="webhook.enabled" />
      </template>
      <NForm :model="webhook" label-placement="left" label-width="100">
        <NFormItem label="通知渠道">
          <NRadioGroup v-model:value="webhook.channel" :disabled="!webhook.enabled">
            <NRadio value="dingtalk">钉钉</NRadio>
            <NRadio value="feishu">飞书</NRadio>
            <NRadio value="slack">Slack</NRadio>
          </NRadioGroup>
        </NFormItem>
        <NFormItem label="Webhook URL">
          <NInput v-model:value="webhook.url" type="password" show-password-on="click"
            :placeholder="webhook.channel ? '请输入所选渠道的 Webhook URL' : '请先选择通知渠道'"
            :disabled="!webhook.enabled" />
        </NFormItem>
      </NForm>
    </NCard>

    <NCard title="外部运行监控（Uptime Kuma Push）" size="small" style="margin-bottom: 16px">
      <template #header-extra>
        <NSwitch v-model:value="push.enabled" />
      </template>
      <NForm :model="push" label-placement="left" label-width="100">
        <NFormItem label="Push URL">
          <NInput
            v-model:value="push.url"
            type="password"
            show-password-on="click"
            placeholder="https://kuma.example.com/api/push/xxxxxxxx"
            :disabled="!push.enabled"
          />
        </NFormItem>
        <NFormItem label="发送间隔">
          <NInput v-model:value="push.interval" placeholder="60s" style="max-width: 220px" :disabled="!push.enabled" />
        </NFormItem>
      </NForm>
      <NText depth="3" style="font-size: 14px">
        FWAlizer 会按该间隔主动上报当前运行状态；Uptime Kuma 侧的 Heartbeat Interval 应大于本值，
        默认发送 60 秒时建议设置为 120 秒。最小间隔为 20s。
      </NText>
    </NCard>

    <NButton type="primary" size="large" :loading="saving" :disabled="saving || loadState !== 'ready'" @click="save">保存配置</NButton>
  </div>
</template>
