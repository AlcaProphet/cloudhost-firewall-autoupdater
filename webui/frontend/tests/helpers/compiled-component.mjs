// 编译真实SFC并观察模板输出；依赖桩只控制网络、生命周期和时间，不复制业务判断。
import { readFileSync } from 'node:fs'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import { fileURLToPath } from 'node:url'
import assert from 'node:assert/strict'
export const root = process.env.FWALIZER_FRONTEND_SOURCE_ROOT || fileURLToPath(new URL('../../', import.meta.url))
const require = createRequire(`${root}/package.json`)
const vue = require('vue'), ts = require('typescript')
const { parse, compileScript, compileTemplate } = require('@vue/compiler-sfc')
const cache = new Map()
const read = path => readFileSync(`${root}/src/${path}`, 'utf8')
export function deferred() {
  let resolve, reject
  const promise = new Promise((a, b) => { resolve = a; reject = b })
  return { promise, resolve, reject }
}
export function mount(path, request = async () => ({}), options = {}) {
  const mounted = [], unmounted = [], messages = [], calls = [], timers = new Map(), intervals = new Map()
  let id = 0
  const observedRequest = (...args) => { calls.push(args); return request(...args) }
  const dependencies = name => {
    if (name === 'vue') return { ...vue, onMounted: fn => mounted.push(fn), onUnmounted: fn => unmounted.push(fn), resolveComponent: name => ({ name }) }
    if (name === 'vue-router') return { useRoute: () => vue.reactive({ path: '/' }), useRouter: () => ({ push() {} }) }
    if (name === 'naive-ui') return new Proxy({
      useThemeVars: () => vue.ref({}),
      useMessage: () => Object.fromEntries(['error', 'success', 'info', 'warning'].map(kind => [kind, text => messages.push({ kind, text })])),
    }, { get: (obj, key) => key in obj ? obj[key] : { name: key } })
    if (name.endsWith('/api')) return { request: observedRequest }
    if (name.endsWith('useSettings')) return { useSettings: () => ({ refresh: options.refresh || (async () => {}), tcReady: vue.ref(true), aliReady: vue.ref(true), applyTheme() {}, setTheme() {}, theme: vue.ref('light') }) }
    if (name.endsWith('useDryRun')) return evaluate(read('composables/useDryRun.ts'))
    if (name.endsWith('useZones')) return { useZones: () => ({ load: async () => calls.push(['zones']), regionOptions: () => [] }) }
    if (name.endsWith('useScannedResources')) return { useScannedResources: () => ({ load: async ct => calls.push(['scanned', ct]), scan: async () => null, clear: async () => {}, resourcesOf: () => [] }) }
    if (name.endsWith('constants')) return { cloudLabelMap: {} }
    if (name.endsWith('DryRunResults.vue')) return { __esModule: true, default: { name: 'DryRunResults' } }
    throw new Error(`未预期依赖：${name}`)
  }
  function evaluate(source) {
    const module = { exports: {} }
    const code = ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
    new Script(code).runInNewContext({ module, exports: module.exports, require: dependencies, Error, Date,
      setTimeout: (fn, ms) => { const key = ++id; timers.set(key, { fn, ms }); return key }, clearTimeout: key => timers.delete(key),
      setInterval: (fn, ms) => { const key = ++id; intervals.set(key, { fn, ms }); return key }, clearInterval: key => intervals.delete(key),
      window: { location: { reload() { calls.push(['reload']) } } },
    })
    return module.exports
  }
  if (!cache.has(path)) {
    const { descriptor } = parse(read(path)), script = compileScript(descriptor, { id: 'r21' })
    const template = compileTemplate({ source: descriptor.template.content, filename: path, id: 'r21', compilerOptions: { bindingMetadata: script.bindings } })
    assert.deepEqual(template.errors, [])
    cache.set(path, { script: script.content, template: template.code })
  }
  const compiled = cache.get(path), props = vue.reactive({})
  const state = evaluate(compiled.script).default.setup(props, { expose() {} })
  const render = evaluate(compiled.template).render
  function inspect(nextProps = {}) {
    Object.assign(props, nextProps)
    const nodes = [], texts = []
    function visit(node) {
      if (Array.isArray(node)) return node.forEach(visit)
      if (typeof node === 'string' || typeof node === 'number') { texts.push(String(node)); return }
      if (!node || typeof node !== 'object' || node.type === vue.Comment) return
      nodes.push(node)
      if (node.children && typeof node.children === 'object' && !Array.isArray(node.children)) {
        for (const [key, value] of Object.entries(node.children)) if (key !== '_' && typeof value === 'function') visit(value())
      } else visit(node.children)
    }
    visit(render({}, [], props, vue.proxyRefs(state), {}, {}))
    return { nodes, text: texts.join(' ').replace(/\s+/g, ' ') }
  }
  return { s: state, inspect, messages, calls, timers, intervals,
    mounted: () => Promise.all(mounted.map(fn => fn())), unmount: () => unmounted.forEach(fn => fn()),
    runTimers: async () => { const pending = [...timers.values()]; timers.clear(); for (const timer of pending) await timer.fn() },
  }
}
