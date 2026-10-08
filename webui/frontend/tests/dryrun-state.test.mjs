// 编译真实页面与结果模板，执行真实请求封装；隔离源码用于行为负向控制。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import assert from 'node:assert/strict'
import nativeTest from 'node:test'
const test = (name, fn) => nativeTest(name, { timeout: 3000 }, fn)
const root = process.env.FWALIZER_I812_SOURCE_ROOT || fileURLToPath(new URL('../', import.meta.url))
const require = createRequire(`${root}/package.json`)
const vue = require('vue'), ts = require('typescript')
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc')
const read = path => readFileSync(`${root}/src/${path}`, 'utf8')
function deferred() {
  let resolve, reject
  const promise = new Promise((r, j) => { resolve = r; reject = j })
  return { promise, resolve, reject }
}
function mount() {
  let now = 1000, active, calls = 0, state
  const messages = []
  class ClockDate extends Date { constructor(...args) { super(...(args.length ? args : [now])) } }
  function evaluate(source, dependencies) {
    const module = { exports: {} }
    const js = ts.transpileModule(source, { reportDiagnostics: true, compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } })
    assert.equal((js.diagnostics || []).filter(d => d.category === ts.DiagnosticCategory.Error).length, 0)
    new Script(js.outputText).runInNewContext({ module, exports: module.exports, require: dependencies, Error, Date: ClockDate, AbortController, setTimeout, clearTimeout,
      fetch: (url, options) => {
        assert.equal(url, '/api/sync/dryrun'); assert.equal(options.method, 'POST')
        calls++; return active.promise
      } })
    return module.exports
  }
  const api = evaluate(read('api.ts'), name => { throw new Error(name) })
  const hook = evaluate(read('composables/useDryRun.ts'), name => {
    if (name === 'vue') return vue
    if (name === '../api') return api
    throw new Error(name)
  }).useDryRun
  const components = Object.fromEntries(['NButton','NSpace','NCard','NDataTable','NAlert','NStatistic','NGrid','NGi','NTag'].map(name => [name, { name }]))
  function dependencies(name) {
    if (name === 'vue') return vue
    if (name === 'naive-ui') return { ...components, useMessage: () => Object.fromEntries(['info','success','error'].map(kind => [kind, text => messages.push({ kind, text })])) }
    if (name.endsWith('/useDryRun')) return { useDryRun: () => state = hook() }
    if (name.endsWith('/constants')) return { cloudLabelMap: { tc_cvm: '腾讯云 CVM' } }
    if (name.endsWith('/DryRunResults.vue')) return { __esModule: true, default: { name: 'DryRunResults' } }
    throw new Error(name)
  }
  function compile(path) {
    const { descriptor } = parse(read(path))
    const script = compileScript(descriptor, { id: 'i812' })
    const template = compileTemplate({ source: descriptor.template.content, filename: path, id: 'i812', compilerOptions: { bindingMetadata: script.bindings } })
    assert.deepEqual(template.errors, [])
    return { setup: evaluate(script.content, dependencies).default.setup, render: evaluate(template.code, dependencies).render }
  }
  const component = compile('components/DryRunResults.vue'), page = compile('views/RunTest.vue')
  const setup = page.setup({}, { expose() {} })
  function inspect() {
    const texts = [], alerts = [], kinds = [], statistics = [], tables = []
    function visit(node) {
      if (typeof node === 'string') texts.push(node)
      else if (Array.isArray(node)) node.forEach(visit)
      else if (node) {
        const kind = node.type?.name
        if (kind === 'DryRunResults') {
          // 复现 Vue 在组件边界进行的 kebab-case 属性归一。
          const props = Object.fromEntries(Object.entries(node.props).map(([key, value]) => [key.replace(/-([a-z])/g, (_, c) => c.toUpperCase()), value]))
          const child = component.setup(props, { expose() {} })
          visit(component.render({}, [], props, vue.proxyRefs(child), {}, {})); return
        }
        if (kind) kinds.push(kind)
        if (kind === 'NAlert') alerts.push(node.props?.type)
        if (kind === 'NStatistic') statistics.push(node.props)
        if (kind === 'NDataTable') tables.push(node.props?.data)
        if (node.children && typeof node.children === 'object' && !Array.isArray(node.children)) {
          for (const [key, slot] of Object.entries(node.children)) if (key !== '_' && typeof slot === 'function') visit(slot())
        } else visit(node.children)
      }
    }
    visit(page.render({}, [], {}, vue.proxyRefs(setup), {}, {}))
    return { text: texts.join(' '), alerts, kinds, statistics, tables }
  }
  function start() { active = deferred(); return setup.runDryRun() }
  function respond(body, status = 200) { now += 1000; active.resolve({ ok: status >= 200 && status < 300, status, json: async () => body }) }
  function reject(error) { now += 1000; active.reject(error) }
  return { state, messages, inspect, start, click: () => setup.runDryRun(), respond, reject, calls: () => calls, now: () => now }
}
const empty = { results: [], warnings: [] }
const target = extra => ({ target_id: 7, provider: 'tc_cvm(sg-fixture)', domains: ['probe.invalid'], desired: [], satisfied_by_owned: [], satisfied_by_external: [], to_add: [], cleanup_candidates: [], cleanup_deferred: [], dns_errors: [], unsupported: [], conflicts: [], coverage_ready: false, error: '', ...extra })
function noPreview(output) {
  assert.equal(output.kinds.includes('NGrid'), false, '未取得本次结果不能显示统计')
  assert.equal(output.kinds.includes('NCard'), false, '未取得本次结果不能显示目标卡片')
  assert.equal(output.alerts.includes('success'), false)
  assert.doesNotMatch(output.text, /无待变更规则/)
}
test('首次未执行；首次等待隐藏预览；重复点击仅一个 POST', async () => {
  const e = mount()
  assert.equal(e.state.status.value, 'idle'); assert.equal(e.state.lastFinishedAt.value, null)
  assert.match(e.inspect().text, /尚未执行/)
  const first = e.start()
  assert.equal(e.state.status.value, 'running'); assert.equal(e.state.loading.value, true)
  assert.match(e.inspect().text, /正在执行/); noPreview(e.inspect())
  const duplicate = e.click()
  assert.equal(e.calls(), 1); assert.equal(e.messages.length, 0)
  await duplicate
  e.respond(empty); await first
  assert.equal(e.state.loading.value, false); assert.equal(e.messages.length, 1)
})
for (const [name, status, reason] of [['409',409,'已有模拟测试正在执行'], ['500',500,'模拟测试失败'], ['503',503,'同步引擎未启动'], ['网络错误',0,'网络连接断开']]) {
  for (const hadSuccess of [false, true]) {
    test(`${hadSuccess ? '曾成功' : '首次'}→${name}：持久失败、无成功/历史预览；重试恢复`, async () => {
      const e = mount()
      if (hadSuccess) { const p = e.start(); e.respond({ results: [target({ to_add: [{ protocol: 'TCP', port: '443', cidr: '192.0.2.1/32' }] })], warnings: ['旧提示'] }); await p }
      const previous = e.state.lastFinishedAt.value, p = e.start()
      assert.equal(e.state.results.value.length, 0); assert.equal(e.state.warnings.value.length, 0)
      assert.equal(e.state.error.value, '')
      assert.match(e.inspect().text, /正在执行/); noPreview(e.inspect())
      if (status) e.respond({ error: reason }, status); else e.reject(new Error(reason))
      await p
      assert.equal(e.state.status.value, 'failed'); assert.equal(e.state.loading.value, false)
      assert.equal(e.state.lastFinishedAt.value.getTime(), e.now())
      if (previous) assert(e.state.lastFinishedAt.value.getTime() > previous.getTime())
      assert.match(e.inspect().text, /本次模拟测试请求失败/); assert(e.inspect().text.includes(reason))
      assert.doesNotMatch(e.inspect().text, /尚未执行|旧提示/); noPreview(e.inspect())
      assert.equal(e.messages.at(-1).kind, 'error')
      e.messages.length = 0
      assert(e.inspect().text.includes(reason), '通知消失后仍保留原因')
      const retry = e.start(); assert.equal(e.state.error.value, '')
      e.respond({ results: [target({ coverage_ready: true })], warnings: [] }); await retry
      assert.equal(e.state.status.value, 'completed'); assert.equal(e.state.loading.value, false)
      assert.equal(e.inspect().kinds.includes('NCard'), true)
      assert.equal(e.inspect().alerts.includes('error'), false); assert.equal(e.messages.at(-1).kind, 'info')
    })
  }
}
for (const [name, warnings] of [['零目标',['暂无云资源目标，请先在云资源管理页配置']], ['运行时未就绪',['运行时状态尚未就绪']], ['零结果无提示',[]]]) {
  test(`${name}：中性空结果保留原始 warning，不推导无变更`, async () => {
    const e = mount(), p = e.start(); e.respond({ results: [], warnings }); await p
    const out = e.inspect()
    assert.equal(e.state.status.value, 'completed'); assert.match(out.text, /本次未返回目标预览/)
    for (const warning of warnings) assert(out.text.includes(warning))
    assert.equal(out.alerts.includes('success'), false); assert.doesNotMatch(out.text, /无待变更规则/)
    assert.equal(e.messages.at(-1).kind, 'info'); assert.match(out.text, /最近请求结束/)
  })
}
test('无适用规则保留跳过卡片，不显示空规划表格', async () => {
  const e = mount(), p = e.start(); e.respond({ results: [target({ domains: [] })], warnings: ['暂无域名规则'] }); await p
  const out = e.inspect()
  assert.match(out.text, /无适用规则/); assert.match(out.text, /正式同步将跳过此目标/)
  assert.equal(out.kinds.filter(k => k === 'NCard').length, 1); assert.equal(out.tables.length, 0)
  assert.doesNotMatch(out.text, /本次未返回目标预览/)
})
test('HTTP 200 的目标错误/DNS/限制/冲突独立展示；通知不冒充所有目标正常', async () => {
  const e = mount(), p = e.start()
  e.respond({ results: [target({ error: '云快照读取失败', dns_errors: [{ message: 'DNS 解析失败详情' }], unsupported: [{ message: '平台无法实施详情' }], conflicts: [{ message: '规则冲突详情' }], cleanup_deferred: [{ message: '清理延后详情' }] })], warnings: ['响应提示'] }); await p
  const out = e.inspect(); assert.equal(e.state.status.value, 'completed')
  for (const text of ['云快照读取失败','DNS 解析失败详情','平台无法实施详情','规则冲突详情','清理延后详情','响应提示']) assert(out.text.includes(text))
  assert(out.alerts.includes('error')); assert.equal(e.messages.at(-1).kind, 'info')
  assert.equal(e.messages.some(m => m.kind === 'success'), false)
})
test('正常目标保留待新增明细/统计；真实零变更仍显示目标卡片', async () => {
  const e = mount(), p = e.start()
  e.respond({ results: [target({ to_add: [{ protocol: 'TCP', port: '443', cidr: '192.0.2.1/32' }] })], warnings: [] }); await p
  assert.equal(e.inspect().statistics.find(s => s.label === '待新增').value, 1)
  assert.equal(e.inspect().tables.some(rows => rows.some(row => row.cidr === '192.0.2.1/32')), true)
  const retry = e.start(); e.respond({ results: [target({ coverage_ready: true })], warnings: [] }); await retry
  assert.equal(e.inspect().statistics.find(s => s.label === '待新增').value, 0)
  assert.equal(e.inspect().kinds.includes('NCard'), true); assert.equal(e.inspect().alerts.includes('success'), false)
})
for (const reason of [null, new Error('')]) {
  test(`非标准失败 ${reason === null ? 'null' : '空 message'} 使用稳定原因并释放 loading`, async () => {
    const e = mount(), p = e.start(); e.reject(reason); await p
    assert.equal(e.state.loading.value, false); assert.equal(e.state.status.value, 'failed')
    assert.match(e.inspect().text, /模拟测试请求失败/); assert.match(e.messages.at(-1).text, /模拟测试请求失败/)
    noPreview(e.inspect())
  })
}
