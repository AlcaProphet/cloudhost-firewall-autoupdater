// 执行真实 Dashboard setup 和模板渲染；不以字符串源码搜索代替展示行为。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
const root = process.env.FWALIZER_I802_SOURCE_ROOT || fileURLToPath(new URL('../', import.meta.url))
const require = createRequire(`${root}/package.json`)
const vue = require('vue'), ts = require('typescript')
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc')
const { descriptor } = parse(readFileSync(`${root}/src/views/Dashboard.vue`, 'utf8'))
const compiled = compileScript(descriptor, { id: 'i802' })
const template = compileTemplate({ source: descriptor.template.content, filename: 'Dashboard.vue', id: 'i802', compilerOptions: { bindingMetadata: compiled.bindings } })
assert.deepEqual(template.errors, [])
function evaluate(source, request = async () => ({})) {
  const module = { exports: {} }
  const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Script(js).runInNewContext({ module, exports: module.exports, setTimeout, clearTimeout, setInterval, clearInterval, require(name) {
    if (name === 'vue') return { ...vue, onMounted() {}, onUnmounted() {} }
    if (name === 'vue-router') return { useRouter: () => ({ push() {} }) }
    if (name === 'naive-ui') return { ...Object.fromEntries(['NCard','NGrid','NGi','NButton','NAlert','NTooltip','NSpace'].map(name => [name, { name }])), useMessage: () => ({}) }
    if (name.endsWith('/api')) return { request }
    if (name.endsWith('useSettings')) return { useSettings: () => ({ refresh: async () => {}, tcReady: vue.ref(true), aliReady: vue.ref(true) }) }
    throw new Error(name)
  } })
  return module.exports
}
const render = evaluate(template.code).render
function view(s) {
  const nodes = render({}, [], {}, vue.proxyRefs(s), {}, {})
  function textOf(node) {
    if (typeof node === 'string' || typeof node === 'number') return String(node)
    if (Array.isArray(node)) return node.map(textOf).join(' ')
    if (!node || node.type === vue.Comment) return ''
    if (node.children && typeof node.children === 'object' && !Array.isArray(node.children)) {
      return Object.entries(node.children).filter(([name, slot]) => name !== '_' && typeof slot === 'function').map(([, slot]) => textOf(slot())).join(' ')
    }
    return textOf(node.children)
  }
  const text = textOf(nodes).replace(/\s+/g, ' ').trim()
  return { s, text }
}
function mount(round, overrides = {}, request) {
  const s = evaluate(compiled.content, request).default.setup({}, { expose() {} })
  s.status.value = { ...s.status.value, last_round: round, ...overrides }
  return view(s)
}
const summary = { observed_targets: 1, estimated_targets: 0, historical_targets: 0, unknown_targets: 0, observed_candidates: 1, observed_deferred: 0, complete: true }
const round = extra => ({ total: 1, failed: 0, skipped: 0, added: 0, deleted: 2, cleanup_candidates: 99, cleanup_deleted: 2, cleanup_deferred: 99, outcome: 'success', cleanup_observation_summary: { ...summary }, ...extra })
test('无记录与零目标不冒充已观察零', () => {
  assert.match(mount(null).text, /暂无记录/)
  assert.match(mount(round({ total: 0, outcome: 'idle' })).text, /无适用目标/)
  assert.equal(mount(round({ total: 0, outcome: 'idle' })).s.healthHint.value, null)
})
test('累计确认清理与最终S1候选分开，可信零无告警', () => {
  const { s, text } = mount(round())
  assert.match(text, /本轮累计确认清理 2 条/)
  assert.match(text, /最终尝试 S1 候选 1 条；已观察残留 0 条/)
  assert.doesNotMatch(text, /残留 99/)
  assert.equal(s.healthHint.value, null)
})
for (const category of ['estimated_targets', 'historical_targets', 'unknown_targets']) {
  test(`${category}: 零估计或历史不能显示完整无残留`, () => {
    const o = { ...summary, observed_targets: 0, observed_candidates: 0, [category]: 1, complete: false }
    const { s, text } = mount(round({ cleanup_deferred: 0, cleanup_observation_summary: o }))
    assert.match(text, /1 个目标当前残留未确认/)
    assert.ok(s.healthHint.value, "未确认清理必须保留提示")
    assert.match(s.healthHint.value.text, /清理状态尚未最终确认/)
    assert.equal(s.healthHint.value.type, 'warning')
  })
}
test('混合目标显示观察部分与未确认分类，不使用旧数值合计', () => {
  const o = { ...summary, estimated_targets: 1, historical_targets: 1, unknown_targets: 1, observed_deferred: 3, complete: false }
  const { text } = mount(round({ total: 4, cleanup_observation_summary: o }))
  assert.match(text, /已观察部分残留 3 条/)
  assert.match(text, /3 个目标当前残留未确认（估计 1、历史 1、未知 1）/)
})
test('失败和partial优先；暂停抑制提示；success残留仍提示延后', () => {
  assert.equal(mount(round({ outcome: 'failed', failed: 1 })).s.healthHint.value.type, 'error')
  assert.match(mount(round({ outcome: 'partial', skipped: 1 })).s.healthHint.value.text, /平台无法实施/)
  const { s } = mount(round({ cleanup_observation_summary: { ...summary, observed_deferred: 1 } }))
  assert.match(s.healthHint.value.text, /已观察残留 1 条/)
  s.status.value.enabled = false
  assert.equal(s.healthHint.value, null)
})
test('旧响应缺失观察字段时显示未确认', () => {
  const { s, text } = mount(round({ cleanup_observation_summary: undefined, cleanup_deferred: 0 }))
  assert.match(text, /缺少观察信息/)
  assert.equal(s.healthHint.value.type, 'warning')
})

// I8-R13：只检查页面可观察行为；null 与已确认的零有不同含义。
const completedAt = '2026-10-17T01:02:03Z'
const oldSuccessAt = '2026-10-16T01:02:03Z'
const fullStatus = (overrides = {}) => ({ running: true, enabled: true, last_sync: completedAt, last_success: oldSuccessAt, last_round: round({ added: 3, deleted: 2 }), round_started_at: null, process_started_at: '2026-10-17T00:00:00Z', ...overrides })
function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}

test('I8R13 时间和轮次统计说明限定本次服务启动范围', () => {
  const { text } = mount(null)
  assert.match(text, /本次启动后的最近一轮完成时间，重启后重新记录/)
  assert.match(text, /以下为本次启动后的最近一轮同步统计/)
  assert.doesNotMatch(text, /从未成功|从未同步|尚未执行|无变更/)
})

test('I8R13 无轮次用暂无记录，目标与规则数量仍显示当前配置', () => {
  const { s } = mount(null)
  s.stats.value = { targets: 3, rules: 7 }
  const { text } = view(s)
  assert.match(text, /最近同步增删 暂无记录/)
  assert.doesNotMatch(text, /最近同步 新增 0 \/ 删除 0/)
  assert.match(text, /云资源目标 3 个/)
  assert.match(text, /域名规则 7 条/)
  assert.equal(s.healthHint.value, null)
})

for (const outcome of ['success', 'failed', 'partial', 'idle']) {
  test(`I8R13 ${outcome} 的真实零增删仍显示0`, () => {
    const r = round({ added: 0, deleted: 0, cleanup_deleted: 0, outcome, total: outcome === 'idle' ? 0 : 1, failed: outcome === 'failed' ? 1 : 0, skipped: outcome === 'partial' ? 1 : 0 })
    const { text } = mount(r, { last_sync: completedAt })
    assert.match(text, /最近同步 新增 0 \/ 删除 0/)
    assert.doesNotMatch(text, /最近同步增删 暂无记录/)
  })
}

test('I8R13 最近完成时间不替换成旧成功时间，失败确认计数继续展示', () => {
  const { s, text } = mount(round({ outcome: 'failed', failed: 1, added: 3, deleted: 2 }), { last_sync: completedAt, last_success: oldSuccessAt })
  assert.ok(text.includes(new Date(completedAt).toLocaleString()))
  assert.ok(!text.includes(new Date(oldSuccessAt).toLocaleString()))
  assert.match(text, /最近同步 新增 3 \/ 删除 2/)
  assert.equal(s.healthHint.value.type, 'error')
})

test('I8R13 暂停保留时间和已知轮次，只抑制原有健康提示', () => {
  const { s, text } = mount(round({ outcome: 'failed', failed: 1, added: 3, deleted: 2 }), { running: true, enabled: false, last_sync: completedAt })
  assert.match(text, /已暂停/)
  assert.match(text, /最近同步 新增 3 \/ 删除 2/)
  assert.ok(text.includes(new Date(completedAt).toLocaleString()))
  assert.equal(s.healthHint.value, null)
})

test('I8R13 在途轮次保留最近完成轮次，首轮在途仍为暂无记录', () => {
  const during = { round_started_at: completedAt }
  assert.match(mount(round({ added: 3, deleted: 2 }), during).text, /最近同步 新增 3 \/ 删除 2/)
  const first = mount(null, during)
  assert.match(first.text, /最近同步增删 暂无记录/)
  assert.equal(first.s.healthHint.value, null)
})

test('I8R13 初次读取挂起或失败维持中性空记录，收到真实零后展示0', async t => {
  const pending = deferred()
  const { s } = mount(null, {}, () => pending.promise)
  const task = s.fetchStatus()
  t.after(async () => { pending.resolve(fullStatus({ last_round: round({ added: 0, deleted: 0 }) })); await task })
  assert.match(view(s).text, /最近同步增删 暂无记录/)
  pending.resolve(fullStatus({ last_round: round({ added: 0, deleted: 0 }) }))
  await task
  assert.match(view(s).text, /最近同步 新增 0 \/ 删除 0/)
  const failed = mount(null, {}, async () => { throw new Error('local read failure') })
  await failed.s.fetchStatus()
  assert.match(view(failed.s).text, /最近同步增删 暂无记录/)
  assert.doesNotMatch(view(failed.s).text, /从未成功|尚未执行/)
})

test('I8R13 轮询等待或失败保留已有轮次，后续新进程空响应清除旧统计', async t => {
  const pending = deferred()
  let calls = 0
  const { s } = mount(round({ added: 3, deleted: 2 }), fullStatus(), async () => {
    calls++
    if (calls === 1) return pending.promise
    if (calls === 2) throw new Error('local poll failure')
    return fullStatus({ last_sync: null, last_success: null, last_round: null, process_started_at: '2026-10-17T02:00:00Z' })
  })
  const task = s.fetchStatus()
  t.after(async () => { pending.resolve(fullStatus()); await task })
  assert.match(view(s).text, /最近同步 新增 3 \/ 删除 2/)
  pending.resolve(fullStatus())
  await task
  await s.fetchStatus()
  assert.match(view(s).text, /最近同步 新增 3 \/ 删除 2/)
  assert.ok(view(s).text.includes(new Date(completedAt).toLocaleString()))
  await s.fetchStatus()
  assert.match(view(s).text, /最近同步增删 暂无记录/)
  assert.doesNotMatch(view(s).text, /最近同步 新增 3 \/ 删除 2/)
  assert.ok(!view(s).text.includes(new Date(completedAt).toLocaleString()))
  assert.equal(s.healthHint.value, null)
})

test('I8R13 页面重新挂载后读取同一引擎状态，已完成时间和计数一致', async () => {
  for (let i = 0; i < 2; i++) {
    const { s } = mount(null, {}, async () => fullStatus())
    await s.fetchStatus()
    assert.match(view(s).text, /最近同步 新增 3 \/ 删除 2/)
    assert.ok(view(s).text.includes(new Date(completedAt).toLocaleString()))
  }
})
