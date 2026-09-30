// 编译真实页面脚本并检查真实模板；浏览器交互另做联合验收。
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import assert from 'node:assert/strict'
import test from 'node:test'
import { parse, compileScript } from '@vue/compiler-sfc'
import { baseParse } from '@vue/compiler-dom'
import ts from 'typescript'

const require = createRequire(import.meta.url)
const vue = require('vue')
function javascript(source) {
  return ts.transpileModule(source, {
    compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 },
  }).outputText
}
const apiModule = { exports: {} }
new Script(javascript(readFileSync(new URL('../src/api.ts', import.meta.url), 'utf8')))
  .runInNewContext({ module: apiModule, exports: apiModule.exports })
const { RequestError } = apiModule.exports

function deferred() {
  let resolve, reject
  const promise = new Promise((yes, no) => { resolve = yes; reject = no })
  return { promise, resolve, reject }
}
function mount(page, request, transform = source => source) {
  const source = readFileSync(new URL(`../src/views/${page}.vue`, import.meta.url), 'utf8')
  const { descriptor } = parse(source)
  const compiled = compileScript(descriptor, { id: `delete-${page}` })
  let unmounted
  const messages = []
  const module = { exports: {} }
  new Script(javascript(transform(compiled.content))).runInNewContext({
    module, exports: module.exports,
    require(name) {
      if (name === 'vue') return {
        ...vue, onMounted() {}, onUnmounted(fn) { unmounted = fn },
      }
      if (name === 'vue-router') return { useRouter: () => ({ push() {} }) }
      if (name === 'naive-ui') return {
        NButton: { name: 'NButton' }, NSpace: { name: 'NSpace' },
        useMessage: () => ({
          success: text => messages.push({ type: 'success', text }),
          error: text => messages.push({ type: 'error', text }),
        }),
      }
      if (name === '../api') return { request, RequestError }
      if (name === '../constants') return { cloudLabelMap: {}, cloudOptions: [], resourceIdHint: () => '' }
      if (name.endsWith('useSettings')) return { useSettings: () => ({ refresh: async () => {}, tcReady: vue.ref(false), aliReady: vue.ref(false) }) }
      if (name.endsWith('useZones')) return { useZones: () => ({ load: async () => {}, regionOptions: () => [] }) }
      if (name.endsWith('useScannedResources')) return { useScannedResources: () => ({ load: async () => {}, resourceOptions: () => [], resourcesOf: () => [] }) }
      throw new Error(`未预期的依赖: ${name}`)
    },
  })
  const state = module.exports.default.setup({}, { expose() {} })
  return { state, messages, unmount: () => unmounted(), template: descriptor.template.content }
}
const target = id => ({ id, cloud_type: 'tc_lighthouse', resource_id: `instance-${id}`, region: 'ap-guangzhou' })
const rule = id => ({ id, host: `host-${id}.example`, protocol: 'TCP', ports: '443', action: 'ACCEPT', targets: [1], comment: '', enable_ipv6: false })
const turn = () => new Promise(resolve => setImmediate(resolve))
const ids = value => Array.from(value, row => row.id)

for (const [page, key, row] of [['Targets', 'targets', target], ['Rules', 'rules', rule]]) {
  test(`${page}：真实行按钮只打开确认；未确认、取消和关闭均零 DELETE`, async () => {
    const calls = []
    const { state: s } = mount(page, (...args) => { calls.push(args); return [] })
    await s.confirmDelete()
    const buttons = s.columns.find(column => column.key === 'actions').render(row(1)).children.default()
    buttons[1].props.onClick({ currentTarget: null })
    assert.equal(s.deletePhase.value, 'confirming')
    assert.equal(calls.length, 0)
    s.cancelDelete()
    await s.confirmDelete()
    s.openDeleteConfirm(row(1))
    s.updateDeleteConfirm(false)
    await s.confirmDelete()
    assert.equal(s.pendingDelete.value, null)
    assert.equal(calls.length, 0)
  })
  test(`${page}：单在途 DELETE，确认期间及刷新期间不切换对象`, async () => {
    const deletion = deferred(), refresh = deferred(), calls = []
    const { state: s, messages } = mount(page, (url, opts) => { calls.push({ url, opts }); return opts ? deletion.promise : refresh.promise })
    s.openDeleteConfirm(row(1))
    const task = s.confirmDelete()
    await s.confirmDelete()
    s.cancelDelete(); s.updateDeleteConfirm(false); s.openDeleteConfirm(row(2))
    assert.equal(s.pendingDelete.value.id, 1)
    assert.equal(s.showDeleteConfirm.value, true)
    assert.equal(s.deleteBusy.value, true)
    assert.equal(calls.length, 1)
    deletion.resolve({})
    await turn()
    assert.equal(s.deletePhase.value, 'refreshing')
    assert.equal(s.showDeleteConfirm.value, false)
    s.openDeleteConfirm(row(2))
    assert.equal(s.pendingDelete.value, null)
    refresh.resolve([row(2)])
    await task
    assert.equal(calls.filter(call => call.opts?.method === 'DELETE').length, 1)
    assert.equal(calls.filter(call => !call.opts).length, 1)
    assert.equal(messages.filter(message => message.type === 'success').length, 1)
    assert.equal(s.deletePhase.value, 'idle')
  })
  test(`${page}：摘要副本绑定 ID；取消 A 后只删除 B`, async () => {
    const calls = []
    const { state: s } = mount(page, (url, opts) => { if (opts) calls.push(url); return [] })
    const original = row(1)
    s.openDeleteConfirm(original)
    original.id = 99
    if (page === 'Rules') original.targets.push(2)
    assert.equal(s.pendingDelete.value.id, 1)
    if (page === 'Rules') assert.deepEqual(Array.from(s.pendingDelete.value.targets), [1])
    s.cancelDelete()
    s.openDeleteConfirm(row(2))
    await s.confirmDelete()
    assert.deepEqual(calls, [`/api/${key}/2`])
  })
  test(`${page}：409/404/500 与网络异常均无成功提示、无自动删除重试并可再次操作`, async () => {
    for (const error of [new RequestError(409, '引用冲突'), new RequestError(404, '不存在'), new RequestError(500, '内部失败'), new Error('连接断开')]) {
      const calls = []
      const { state: s, messages } = mount(page, (url, opts) => {
        calls.push({ url, opts })
        return opts ? Promise.reject(error) : [row(1), row(2)]
      })
      s.openDeleteConfirm(row(1))
      await s.confirmDelete()
      assert.equal(s.deletePhase.value, 'idle')
      assert.equal(s.pendingDelete.value, null)
      assert.equal(calls.filter(call => call.opts?.method === 'DELETE').length, 1)
      assert.equal(calls.filter(call => !call.opts).length, 1)
      assert.equal(messages.some(message => message.type === 'success'), false)
      assert.match(messages[0].text, error instanceof RequestError ? /删除失败/ : /未能确认删除结果/)
      s.openDeleteConfirm(row(2))
      assert.equal(s.pendingDelete.value.id, 2)
    }
  })
  test(`${page}：成功后刷新失败仍保留成功与本地移除，失败核对也能复位`, async () => {
    for (const deleted of [true, false]) {
      const { state: s, messages } = mount(page, (url, opts) => {
        if (!opts) return Promise.reject(new Error('列表不可用'))
        return deleted ? {} : Promise.reject(new RequestError(404, '不存在'))
      })
      s[key].value = [row(1), row(2)]
      s.openDeleteConfirm(row(1))
      await s.confirmDelete()
      assert.deepEqual(ids(s[key].value), deleted ? [2] : [1, 2])
      assert.equal(messages.filter(message => message.type === 'success').length, deleted ? 1 : 0)
      assert.match(messages.at(-1).text, deleted ? /配置已删除，但列表刷新失败/ : /核对删除结果时列表加载失败/)
      assert.equal(s.deletePhase.value, 'idle')
    }
  })
  test(`${page}：删除前的旧 GET 不得重新插入已删行或显示过期错误`, async () => {
    for (const rejected of [false, true]) {
      const old = deferred()
      let reads = 0
      const { state: s, messages } = mount(page, (url, opts) => opts ? {} : ++reads === 1 ? old.promise : [row(2)])
      const loading = s.load()
      s.openDeleteConfirm(row(1))
      await s.confirmDelete()
      rejected ? old.reject(new Error('旧请求失败')) : old.resolve([row(1), row(2)])
      await loading
      assert.deepEqual(ids(s[key].value), [2])
      assert.equal(messages.filter(message => message.type === 'error').length, 0)
    }
  })
  test(`${page}：确认默认取消焦点，成功回退添加按钮，失败回到仍存在的来源`, async () => {
    let cancelFocus = 0, addFocus = 0, originFocus = 0
    const { state: s } = mount(page, () => [])
    s.cancelDeleteButton.value = { $el: { focus: () => cancelFocus++ } }
    s.pageAddButton.value = { $el: { focus: () => addFocus++ } }
    s.openDeleteConfirm(row(1))
    s.focusDeleteCancel()
    assert.equal(cancelFocus, 1)
    await s.confirmDelete()
    assert.equal(addFocus, 0, '退出动画尚未完成，不应提前抢焦点')
    await s.afterDeleteLeave()
    assert.equal(addFocus, 1)
    const refresh = deferred()
    let delayedFocus = 0
    const delayed = mount(page, (url, opts) => opts ? {} : refresh.promise)
    delayed.state.pageAddButton.value = { $el: { focus: () => delayedFocus++ } }
    delayed.state.openDeleteConfirm(row(1))
    const refreshing = delayed.state.confirmDelete()
    await turn()
    await delayed.state.afterDeleteLeave()
    assert.equal(delayedFocus, 0, '退出动画先完成时仍须等待刷新')
    refresh.resolve([])
    await refreshing
    assert.equal(delayedFocus, 1)
    const failed = mount(page, (url, opts) => opts ? Promise.reject(new RequestError(409, '冲突')) : [])
    failed.state.openDeleteConfirm(row(2), { isConnected: true, focus: () => originFocus++ })
    await failed.state.confirmDelete()
    await failed.state.afterDeleteLeave()
    assert.equal(originFocus, 1)
    failed.state.openDeleteConfirm(row(1))
    await failed.state.afterDeleteLeave()
    assert.equal(failed.state.deletePhase.value, 'confirming', '旧退出回调不得清理新确认')
    assert.equal(failed.state.pendingDelete.value.id, 1)
  })
  test(`${page}：卸载丢弃旧列表与删除回调，不刷新、不提示、不抢焦点`, async () => {
    const old = deferred()
    const loaded = mount(page, () => old.promise)
    const loading = loaded.state.load()
    loaded.unmount()
    old.resolve([row(1)])
    await loading
    assert.deepEqual(ids(loaded.state[key].value), [])
    const deletion = deferred(), calls = []
    const deleting = mount(page, (url, opts) => { calls.push({ url, opts }); return deletion.promise })
    deleting.state.openDeleteConfirm(row(1))
    const task = deleting.state.confirmDelete()
    deleting.unmount()
    deletion.resolve({})
    await task
    assert.equal(calls.length, 1)
    assert.equal(deleting.messages.length, 0)
    await deleting.state.confirmDelete()
    assert.equal(calls.length, 1)
  })
  test(`${page}：真实模板约束卡片式确认、关闭入口、提交禁用和危险按钮`, () => {
    const { template } = mount(page, () => [])
    const nodes = []
    function walk(node) { if (node.type === 1) nodes.push(node); for (const child of node.children || []) walk(child) }
    walk(baseParse(template))
    const event = (node, name) => node.props.find(prop => prop.type === 7 && prop.name === 'on' && prop.arg?.content === name)?.exp?.content
    const bound = (node, name) => node.props.find(prop => prop.type === 7 && prop.name === 'bind' && prop.arg?.content === name)?.exp?.content
    const attr = (node, name) => node.props.find(prop => prop.type === 6 && prop.name === name)?.value?.content
    const modal = nodes.find(node => node.tag === 'NModal' && bound(node, 'show') === 'showDeleteConfirm')
    assert.equal(attr(modal, 'preset'), 'card')
    assert.equal(event(modal, 'update:show'), 'updateDeleteConfirm')
    for (const name of ['closable', 'mask-closable', 'close-on-esc']) assert.equal(bound(modal, name), '!deleteBusy')
    assert.equal(event(modal, 'after-leave'), 'afterDeleteLeave')
    const button = nodes.find(node => node.tag === 'NButton' && event(node, 'click') === 'confirmDelete')
    assert.equal(attr(button, 'type'), 'error')
    assert.equal(attr(button, 'size'), 'large')
    assert.match(bound(button, 'disabled'), /deleteBusy/)
    assert.equal(bound(button, 'loading'), "deletePhase === 'deleting'")
  })
  test(`${page}：负向控制证明确认守卫与旧 GET 序号有判别力`, async () => {
    let deletes = 0
    const pending = deferred()
    const removedGuard = mount(page, (url, opts) => {
      if (opts) { deletes++; return pending.promise }
      return []
    }, source => source.replace("if (!pageActive || deletePhase.value !== 'confirming' || !pendingDelete.value) return", 'if (!pendingDelete.value) return'))
    removedGuard.state.openDeleteConfirm(row(1))
    const first = removedGuard.state.confirmDelete(), second = removedGuard.state.confirmDelete()
    assert.throws(() => assert.equal(deletes, 1))
    pending.resolve({})
    await Promise.all([first, second])
    const old = deferred()
    let reads = 0
    const removedSequence = mount(page, (url, opts) => opts ? {} : ++reads === 1 ? old.promise : [row(2)],
      source => source.replace('pageActive && sequence === loadSequence', 'pageActive'))
    const loading = removedSequence.state.load()
    removedSequence.state.openDeleteConfirm(row(1))
    await removedSequence.state.confirmDelete()
    old.resolve([row(1), row(2)])
    await loading
    assert.throws(() => assert.deepEqual(ids(removedSequence.state[key].value), [2]))
  })
}
