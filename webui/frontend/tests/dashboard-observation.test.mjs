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
function evaluate(source) {
  const module = { exports: {} }
  const js = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
  new Script(js).runInNewContext({ module, exports: module.exports, setTimeout, clearTimeout, setInterval, clearInterval, require(name) {
    if (name === 'vue') return { ...vue, onMounted() {}, onUnmounted() {} }
    if (name === 'vue-router') return { useRouter: () => ({ push() {} }) }
    if (name === 'naive-ui') return { ...Object.fromEntries(['NCard','NGrid','NGi','NButton','NAlert','NTooltip','NSpace'].map(name => [name, { name }])), useMessage: () => ({}) }
    if (name.endsWith('/api')) return { request: async () => ({}) }
    if (name.endsWith('useSettings')) return { useSettings: () => ({ refresh: async () => {}, tcReady: vue.ref(true), aliReady: vue.ref(true) }) }
    throw new Error(name)
  } })
  return module.exports
}
const render = evaluate(template.code).render
function mount(round) {
  const s = evaluate(compiled.content).default.setup({}, { expose() {} })
  s.status.value.last_round = round
  const nodes = render({}, [], {}, vue.proxyRefs(s), {}, {})
  const text = []
  function visit(node) {
    if (typeof node === 'string') text.push(node)
    else if (Array.isArray(node)) node.forEach(visit)
    else if (node) { if (typeof node.children === 'object' && !Array.isArray(node.children)) { for (const [name, slot] of Object.entries(node.children)) if (name !== '_' && typeof slot === 'function') visit(slot()) } else visit(node.children) }
  }
  visit(nodes)
  return { s, text: text.join(' ') }
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
