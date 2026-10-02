// 编译真实组件脚本验证日志流，不引入额外组件测试框架。
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { parse, compileScript } from '@vue/compiler-sfc'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const vue = require('vue')
const source = readFileSync(new URL('../src/views/Logs.vue', import.meta.url), 'utf8')
const { descriptor } = parse(source)
const compiled = compileScript(descriptor, { id: 'logs-stream-test' })
const code = ts.transpileModule(compiled.content, {
  compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2020 },
}).outputText
// 标准 base32 字母表中的两个不同实例标识。
const epoch = 'ABCDEFGHIJKLMNOPQRSTUVWXYZ'
const other = 'ZYXWVUTSRQPONMLKJIHGFEDCBA'

function mount(request = async () => []) {
  let mounted, unmounted
  const instances = [], messages = []
  class EventSourceMock {
    static CLOSED = 2
    readyState = 0
    closed = 0
    listeners = new Map()
    constructor() { instances.push(this) }
    addEventListener(name, fn) { this.listeners.set(name, fn) }
    removeEventListener(name, fn) {
      if (this.listeners.get(name) === fn) this.listeners.delete(name)
    }
    close() { this.closed++; this.readyState = 2 }
    reset(reason = 'initial', id = epoch + ':0') {
      this.listeners.get('reset')?.({ data: reason, lastEventId: id })
    }
    line(n, data = 'same', instance = epoch) {
      this.onmessage?.({ data, lastEventId: instance + ':' + n })
    }
  }
  const module = { exports: {} }
  new Script(code).runInNewContext({
    module, exports: module.exports, EventSource: EventSourceMock,
    require(name) {
      if (name === 'vue') return {
        ...vue, onMounted: (fn) => { mounted = fn }, onUnmounted: (fn) => { unmounted = fn },
      }
      if (name === 'naive-ui') return {
        useMessage: () => ({ error: (m) => messages.push(m), success: (m) => messages.push(m) }),
      }
      if (name === '../api') return { request }
      throw new Error(`未预期的依赖: ${name}`)
    },
  })
  const state = module.exports.default.setup({}, { expose() {} })
  return { state, instances, messages, mounted, unmounted }
}

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

test('重复 ID 忽略，相同正文的不同 ID 均保留', async () => {
  const m = mount()
  await m.mounted()
  const es = m.instances[0]
  es.reset()
  es.line(1); es.line(1); es.line(2)
  assert.deepEqual([...m.state.logLines.value], ['same', 'same'])
  m.unmounted()
})

test('仅显示最近 1000 条，旧序号忽略，过期 reset 从基准续传', async () => {
  const m = mount()
  await m.mounted()
  const es = m.instances[0]
  es.reset()
  for (let i = 1; i <= 1005; i++) es.line(i, '' + i)
  assert.equal(m.state.logLines.value.length, 1000)
  assert.equal(m.state.logLines.value[0], '6')
  es.line(4, 'duplicate')
  assert.equal(m.state.logLines.value.length, 1000)
  es.reset('history_expired', epoch + ':1000')
  assert.equal(m.state.logLines.value.length, 0)
  es.line(1001, 'resumed')
  assert.deepEqual([...m.state.logLines.value], ['resumed'])
  assert.ok(m.state.streamNotice.value.includes('缓存'))
  m.unmounted()
})

test('实例重置允许低序号，超安全整数仍精确比较，非法帧不污染窗口', async () => {
  const m = mount()
  await m.mounted()
  const es = m.instances[0]
  es.reset(); es.line(100, 'old')
  es.reset('instance_changed', other + ':0'); es.line(1, 'new', other)
  assert.deepEqual([...m.state.logLines.value], ['new'])
  es.reset('initial', epoch + ':9007199254740992')
  es.line('9007199254740993', 'big'); es.line('9007199254740992', 'older')
  assert.deepEqual([...m.state.logLines.value], ['big'])
  for (const n of ['18446744073709551616', '-1', '01', '0']) es.line(n, 'invalid')
  es.line(2, 'wrong-instance', other)
  es.reset('bad', epoch + ':0')
  assert.deepEqual([...m.state.logLines.value], ['big'])
  assert.equal(m.state.streamStatus.value, '日志流格式异常')
  m.unmounted()
})

test('空缓存 reset 与更长实例标识可用，无新增重连不清空窗口', async () => {
  const m = mount()
  await m.mounted()
  const es = m.instances[0], longer = epoch + 'ABCD'
  es.reset('initial', longer + ':0')
  assert.equal(m.state.logLines.value.length, 0)
  es.line(1, 'first', longer)
  es.onopen(); es.onerror(); es.onopen()
  assert.deepEqual([...m.state.logLines.value], ['first'])
  assert.equal(m.instances.length, 1)
  m.unmounted()
})

test('连接状态不创建新连接，跳号提示不连续，卸载清理所有监听', async () => {
  const m = mount()
  await m.mounted()
  const es = m.instances[0]
  es.reset(); es.line(1); es.line(3)
  assert.ok(m.state.streamNotice.value.includes('不连续'))
  es.onopen()
  assert.equal(m.state.streamStatus.value, '已连接')
  assert.ok(m.state.streamNotice.value.includes('不连续'))
  es.onerror()
  assert.equal(m.state.streamStatus.value, '重连中')
  es.readyState = 2; es.onerror()
  assert.equal(m.state.streamStatus.value, '连接已关闭')
  assert.equal(m.instances.length, 1)
  m.unmounted()
  assert.equal(es.closed, 1)
  assert.equal(es.listeners.size, 0)
  assert.equal(es.onmessage, null)
  assert.equal(es.onopen, null)
  assert.equal(es.onerror, null)
  es.reset(); es.line(4)
  assert.equal(m.state.logLines.value.length, 2)
})

test('历史请求挂起不延迟 SSE，卸载后成功或失败响应均不发布', async () => {
  for (const outcome of ['success', 'failure']) {
    const d = deferred(), m = mount(() => d.promise)
    m.mounted()
    assert.equal(m.instances.length, 1)
    m.unmounted()
    if (outcome === 'success') d.resolve([{ id: 1 }])
    else d.reject(new Error('晚到失败'))
    await d.promise.catch(() => {})
    await Promise.resolve()
    assert.equal(m.instances.length, 1)
    assert.equal(m.instances[0].closed, 1)
    assert.equal(m.state.logs.value.length, 0)
    assert.equal(m.messages.length, 0)
  }
})
