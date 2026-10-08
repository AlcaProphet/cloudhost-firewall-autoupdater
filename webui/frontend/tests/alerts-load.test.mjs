// 直接编译真实组件脚本验证状态机，不引入浏览器或组件测试框架。
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { parse, compileScript } from '@vue/compiler-sfc'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const vue = require('vue')
const source = readFileSync(new URL('../src/views/Alerts.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const compiled = compileScript(descriptor, { id: 'alerts-load-test' })
const code = ts.transpileModule(compiled.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
}).outputText

function mount(request) {
  let mounted
  const messages = []
  const module = { exports: {} }
  new Script(code).runInNewContext({
    module, exports: module.exports,
    require(name) {
      if (name === 'vue') return { ...vue, onMounted: (fn) => { mounted = fn } }
      if (name === 'naive-ui') return {
        useMessage: () => ({
          error: (text) => messages.push({ type: 'error', text }),
          success: (text) => messages.push({ type: 'success', text }),
        }),
      }
      if (name === '../api') return { request }
      throw new Error(`未预期的依赖: ${name}`)
    },
  })
  const state = module.exports.default.setup({}, { expose() {} })
  return { state, messages, mounted }
}

function forms(state) {
  return JSON.parse(JSON.stringify({
    policy: state.policy.value, email: state.email.value,
    webhook: state.webhook.value, uptime_kuma_push: state.push.value,
  }))
}

function fixture() {
  const data = forms(mount(() => {}).state)
  data.email.password = 'fixture-password'
  data.webhook.url = 'https://example.invalid/hook/fixture'
  data.uptime_kuma_push.url = 'https://example.invalid/push/fixture'
  data.policy.sync_error_enabled = true
  return data
}

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

test('加载中与请求失败均拒绝直接保存，表单不变且无成功提示', async (t) => {
  const pending = deferred()
  const calls = []
  const { state, mounted, messages } = mount((...args) => {
    calls.push(args)
    return pending.promise
  })
  const before = forms(state)
  const loading = mounted()
  const blocked = state.save()
  // 断言失败也结算挂起夹具，等待已启动的任务结束。
  t.after(async () => {
    pending.reject(new Error('夹具清理'))
    await Promise.all([loading, blocked])
  })
  // 先检查请求次数，避免错误 PUT 等待同一个未解 Promise 而阻塞断言。
  assert.equal(calls.length, 1, '加载中不得发起PUT')
  await blocked
  assert.equal(state.loadState.value, 'loading')
  assert.equal(calls.length, 1)
  pending.reject(new Error('模拟读取失败'))
  await loading
  await state.save()
  assert.equal(state.loadState.value, 'error')
  assert.equal(calls.length, 1)
  assert.deepEqual(forms(state), before)
  assert.equal(messages.some((m) => m.type === 'success'), false)
})

test('全部对象与子字段的缺失、null 和错误类型均整体拒绝', async () => {
  const valid = fixture()
  const invalid = [null, [], {}, 'invalid']
  for (const [group, fields] of Object.entries(valid)) {
    for (const replacement of [null, [], false]) {
      const data = structuredClone(valid)
      data[group] = replacement
      invalid.push(data)
    }
    const missing = structuredClone(valid)
    delete missing[group]
    invalid.push(missing)
    for (const [key, value] of Object.entries(fields)) {
      for (const replacement of [undefined, null, typeof value === 'boolean' ? 'false' : false]) {
        const data = structuredClone(valid)
        if (replacement === undefined) delete data[group][key]
        else data[group][key] = replacement
        invalid.push(data)
      }
    }
  }
  for (const data of invalid) {
    const calls = []
    const { state, mounted, messages } = mount((...args) => { calls.push(args); return data })
    const before = forms(state)
    await mounted()
    await state.save()
    assert.equal(calls.length, 1, '不完整载荷之后不得发起PUT')
    assert.equal(state.loadState.value, 'error')
    assert.deepEqual(forms(state), before)
    assert.equal(messages.some((m) => m.type === 'success'), false)
  }
})

test('合法空字符串与false通过；成功保存完整对象并保留敏感值', async () => {
  const defaults = mount(() => {}).state
  assert.equal(defaults.isAlertsData(forms(defaults)), true)
  const data = fixture()
  const calls = []
  const { state, mounted, messages } = mount((url, opts) => {
    calls.push({ url, opts })
    return opts ? { message: '保存成功' } : data
  })
  await mounted()
  assert.equal(state.loadState.value, 'ready')
  state.email.value.subject = '修改后的主题'
  await state.save()
  const payload = JSON.parse(calls[1].opts.body)
  assert.deepEqual(payload, forms(state))
  assert.equal(payload.email.password, data.email.password)
  assert.equal(payload.webhook.url, data.webhook.url)
  assert.equal(payload.uptime_kuma_push.url, data.uptime_kuma_push.url)
  assert.equal(calls[1].opts.method, 'PUT')
  assert.equal(messages.filter((m) => m.type === 'success').length, 1)
})

test('重复保存只有一个在途PUT；失败保留编辑并允许重试', async (t) => {
  const pending = deferred()
  let puts = 0
  const { state, mounted } = mount((url, opts) => {
    if (!opts) return fixture()
    puts++
    return puts === 1 ? pending.promise : {}
  })
  await mounted()
  state.email.value.subject = '保留编辑'
  const saving = state.save()
  const duplicate = state.save()
  t.after(async () => {
    pending.reject(new Error('夹具清理'))
    await Promise.all([saving, duplicate])
  })
  assert.equal(puts, 1, '重复保存不得发起第二个PUT')
  await duplicate
  assert.equal(puts, 1)
  pending.reject(new Error('模拟保存失败'))
  await saving
  assert.equal(state.saving.value, false)
  assert.equal(state.loadState.value, 'ready')
  assert.equal(state.email.value.subject, '保留编辑')
  await state.save()
  assert.equal(puts, 2)
})

test('成功后重载失败重新锁定，测试邮件仍独立使用八字段POST', async () => {
  const calls = []
  let reads = 0
  const { state, mounted } = mount((url, opts) => {
    calls.push({ url, opts })
    if (opts) return { success: true }
    if (++reads === 1) return fixture()
    throw new Error('模拟重载失败')
  })
  await mounted()
  await state.load()
  await state.save()
  assert.equal(state.loadState.value, 'error')
  await state.testSend()
  const send = calls.at(-1)
  assert.equal(send.url, '/api/alerts/test-email')
  assert.equal(send.opts.method, 'POST')
  assert.deepEqual(Object.keys(JSON.parse(send.opts.body)).sort(),
    ['host', 'port', 'username', 'password', 'from_addr', 'to_addr', 'subject', 'body'].sort())
  assert.equal(calls.some((call) => call.opts?.method === 'PUT'), false)
})

test('空渠道可随关闭配置保存；开启但未选择时零PUT，选择后可保存', async () => {
  const data = fixture(); data.webhook.channel = ''; data.webhook.url = ''
  const calls = []
  const { state, mounted, messages } = mount((url, opts) => { calls.push({ url, opts }); return opts ? {} : structuredClone(data) })
  await mounted(); assert.equal(state.webhook.value.channel, '')
  await state.save(); assert.equal(calls.length, 2)
  assert.equal(JSON.parse(calls[1].opts.body).webhook.channel, '')
  state.webhook.value.enabled = true
  await state.save(); assert.equal(calls.length, 2)
  assert.equal(messages.at(-1).text, '请选择 Webhook 通知渠道')
  state.webhook.value.channel = 'slack'; state.webhook.value.url = 'https://example.invalid/slack'
  await state.save(); assert.equal(calls.length, 3)
  assert.equal(JSON.parse(calls[2].opts.body).webhook.channel, 'slack')
})

test('加载已有三种渠道后，关闭再开启不清空渠道或URL', async () => {
  for (const channel of ['dingtalk', 'feishu', 'slack']) {
    const data = fixture(); data.webhook.channel = channel; data.webhook.enabled = true
    const calls = []
    const { state, mounted } = mount((url, opts) => { calls.push({ url, opts }); return opts ? {} : structuredClone(data) })
    await mounted(); assert.equal(state.webhook.value.channel, channel)
    state.webhook.value.enabled = false; await state.save()
    state.webhook.value.enabled = true; await state.save()
    for (const c of calls.slice(1)) { const hook = JSON.parse(c.opts.body).webhook; assert.equal(hook.channel, channel); assert.equal(hook.url, data.webhook.url) }
  }
})

test('真实单选组件渲染：空值未选中，已有渠道恰选中一项', async () => {
  const { compileTemplate } = await import('@vue/compiler-sfc')
  const { renderToString } = await import('@vue/server-renderer')
  for (const channel of ['', 'dingtalk', 'feishu', 'slack']) {
    const { descriptor } = parse(source.replace("channel: ''", `channel: '${channel}'`))
    const compiledScript = compileScript(descriptor, { id: 'channel-render-test' })
    const template = compileTemplate({ source: descriptor.template.content, filename: 'Alerts.vue', id: 'channel-render-test', ssr: true, ssrCssVars: [], compilerOptions: { bindingMetadata: compiledScript.bindings } })
    assert.deepEqual(template.errors, [])
    const modules = {}
    for (const [name, code] of [['script', compiledScript.content], ['template', template.code]]) {
      const module = { exports: {} }
      new Script(ts.transpileModule(code, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText).runInNewContext({ module, exports: module.exports, require(name) {
        if (name === '../api') return { request: () => { throw new Error('unexpected SSR request') } }
        if (name === 'naive-ui') return { ...require('naive-ui'), useMessage: () => ({ error() {}, success() {} }) }
        return require(name)
      } })
      modules[name] = module.exports
    }
    const component = modules.script.default; component.ssrRender = modules.template.ssrRender
    const html = await renderToString(vue.createSSRApp(component))
    const inputs = html.match(/<input\b[^>]*type="radio"[^>]*>/g) || []
    const checked = inputs.filter((s) => /\bchecked(?:\s|=|>)/.test(s))
    assert.equal(inputs.length, 3); assert.equal(checked.length, channel === '' ? 0 : 1)
    if (channel) assert.match(checked[0], new RegExp(`value="${channel}"`))
  }
})
