// 编译真实组件与共享缓存验证异步行为；浏览器交互验收另行记录。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import assert from 'node:assert/strict'
import nativeTest from 'node:test'
const test = (name, fn) => nativeTest(name, { timeout: 3000 }, fn)
// 外部负向控制读取隔离源码，正常执行读取当前工作树。
const root = process.env.FWALIZER_P316_SOURCE_ROOT || fileURLToPath(new URL('../', import.meta.url))
const require = createRequire(`${root}/package.json`)
const vue = require('vue'), ts = require('typescript')
const { parse, compileScript } = require('@vue/compiler-sfc')
const { baseParse } = require('@vue/compiler-dom')
const js = source => ts.transpileModule(source, { compilerOptions: { module: ts.ModuleKind.CommonJS, target: ts.ScriptTarget.ES2022 } }).outputText
const { RequestError } = evaluate(readFileSync(`${root}/src/api.ts`, 'utf8'), name => { throw new Error(`未预期的 API 依赖：${name}`) })
function evaluate(source, dependencies) {
  const module = { exports: {} }
  new Script(js(source)).runInNewContext({ module, exports: module.exports, require: dependencies, setTimeout, clearTimeout, window: { location: { reload() {} } } })
  return module.exports
}
function deferred() { let resolve, reject; const promise = new Promise((a,b) => { resolve=a; reject=b }); return { promise, resolve, reject } }
function mount(name, request, extra={}) {
  const source = readFileSync(`${root}/src/${name}`, 'utf8')
  const { descriptor } = parse(source)
  const compiled = compileScript(descriptor, { id: 'p316-probe' })
  const messages = []
  let unmount = () => {}, mounted = () => {}
  const route = vue.reactive({ path: '/rules', query: {}, hash: '' })
  const module = evaluate(compiled.content, name => {
    if (name==='vue') return { ...vue, onMounted(fn) { mounted=fn }, onUnmounted(fn) { unmount=fn } }
    if (name==='vue-router') return { useRoute: () => route, useRouter: () => ({ push: async () => {} }) }
    if (name==='naive-ui') return { NButton: { name: 'NButton' }, NSpace: { name: 'NSpace' }, useThemeVars: () => vue.ref({}), useMessage: () => Object.fromEntries(['success','error','warning'].map(kind => [kind, text => messages.push({ kind, text })])) }
    if (name.endsWith('/api')) return { request, RequestError }
    if (name.endsWith('constants')) return { cloudOptions: [], cloudLabelMap: {}, resourceIdHint: () => '' }
    if (name.endsWith('useSettings')) return { useSettings: () => ({ theme: vue.ref('light'), refresh: async () => {}, applyTheme() {}, setTheme() {}, tcReady: vue.ref(true), aliReady: vue.ref(true) }) }
    if (name.endsWith('useZones')) return { useZones: () => ({ load: async () => {}, regionOptions: () => [] }) }
    if (name.endsWith('useScannedResources')) return { useScannedResources: () => ({ load: async () => {}, resourceOptions: () => [], resourcesOf: () => [], clear: async () => {}, scan: async () => null, ...extra }) }
    throw new Error(name)
  })
  return { s: module.default.setup({}, { expose() {} }), messages, route, unmount: () => unmount(), mounted: () => mounted(), template: descriptor.template.content }
}
const turn = () => new Promise(resolve => setImmediate(resolve))
const target = { id: 8, cloud_type: 'tc_lighthouse', region: 'ap-guangzhou', resource_id: 'instance-fixture' }
const rule = { id: 8, host: 'probe.invalid', protocol: 'TCP', ports: '443', action: 'ACCEPT', comment: '', enable_ipv6: false, targets: [] }

test('路由：深链与路由变化决定高亮，导航请求不提前改写高亮', async () => {
  const {s,route} = mount('App.vue', async () => {})
  assert.equal(s.activeKey.value, '/rules')
  route.path='/settings'; assert.equal(s.activeKey.value, '/settings')
  await s.handleMenuUpdate('/targets')
  assert.equal(s.activeKey.value, '/settings', 'a navigation request cannot preempt the confirmed route')
  route.path='/targets'; assert.equal(s.activeKey.value, '/targets')
  route.path='/rules'; assert.equal(s.activeKey.value, '/rules')
})
for (const [page,save,row] of [['Targets','saveTarget',target],['Rules','saveRule',rule]]) {
  test(`${page}: 重复保存只发一次创建请求，并锁定表单切换`, async () => {
    const pending=deferred(), calls=[]
    const {s}=mount(`views/${page}.vue`, (url,opts) => { calls.push([url,opts]); return opts ? pending.promise : [] })
    s.openAdd(); const original=JSON.stringify(s.form.value)
    const first=s[save](); const second=s[save]()
    assert.equal(calls.filter(([,o])=>o).length,1,'duplicate create request')
    s.openEdit(row); assert.equal(s.editingId.value,null,'cannot switch form while saving')
    assert.equal(JSON.stringify(s.form.value),original)
    s.openDeleteConfirm(row); assert.equal(s.deletePhase.value,'idle')
    pending.resolve({}); await Promise.all([first, second])
    assert.equal(s.showModal.value,false)
  })
  test(`${page}: 明确失败保留编辑，解锁后允许重试`, async () => {
    let failures=true
    const {s,messages}=mount(`views/${page}.vue`, (url,opts) => { if (opts && failures) throw new RequestError(400,'fixture invalid'); return opts ? {} : [] })
    s.openEdit(row); const original=JSON.stringify(s.form.value)
    await s[save](); assert.equal(s.showModal.value,true); assert.equal(JSON.stringify(s.form.value),original)
    assert.equal(messages.some(m=>m.kind==='success'),false)
    failures=false; await s[save](); assert.equal(s.showModal.value,false)
  })
  test(`${page}: 卸载后抑制晚到响应及列表读取`, async () => {
    for (const reject of [false,true]) {
      const pending=deferred(), calls=[]
      const {s,messages,unmount}=mount(`views/${page}.vue`, (...args)=> { calls.push(args); return pending.promise })
      s.openAdd(); const saving=s[save](); unmount()
      reject ? pending.reject(new RequestError(500,'fixture fail')) : pending.resolve({})
      await saving
      assert.equal(messages.length,0,'late message after unload')
      assert.equal(calls.length,1,'late list GET after unload')
    }
  })
  test(`${page}: 保存锁持续到刷新结束，刷新失败单独提示`, async () => {
    const refreshing=deferred(), calls=[]
    const {s,messages}=mount(`views/${page}.vue`, (url,opts)=> { calls.push([url,opts]); return opts ? {} : refreshing.promise })
    s.openEdit(row); const operation=s[save](); await turn()
    s.openAdd(); const duplicate=s[save]()
    assert.equal(calls.filter(([,o])=>o).length,1,'refresh is part of saving lifecycle')
    refreshing.reject(new Error('fixture refresh fail')); await operation; await duplicate; await turn()
    assert.ok(messages.some(m=>m.kind==='success'&&m.text==='更新成功'))
    assert.ok(messages.some(m=>m.kind==='error'&&m.text.includes('配置已保存')))
    s.openAdd(); assert.equal(s.showModal.value,true)
  })
  test(`${page}: 真实模板锁定关闭入口、表单与保存按钮`, ()=> {
    const {template}=mount(`views/${page}.vue`, async()=>[])
    const elements = []
    function visit(node) {
      if (node.type === 1) elements.push(node)
      for (const child of node.children || []) visit(child)
    }
    visit(baseParse(template))
    const binding = (node, name) => node.props.find(prop => prop.type === 7 && prop.name === 'bind' && prop.arg?.content === name)?.exp?.content
    const modal = elements.find(node => node.tag === 'NModal' && node.props.some(prop => prop.type === 7 && prop.name === 'model' && prop.exp?.content === 'showModal'))
    assert.ok(modal)
    for (const name of ['closable', 'mask-closable', 'close-on-esc']) assert.equal(binding(modal, name), '!saving')
    const button = elements.find(node => node.tag === 'NButton' && node.props.some(prop => prop.type === 7 && prop.name === 'on' && prop.exp?.content === save))
    assert.equal(binding(button, 'loading'), 'saving')
    assert.equal(binding(button, 'disabled'), 'saving')
    const form = elements.find(node => node.tag === 'NForm' && binding(node, 'model') === 'form')
    assert.equal(binding(form, 'disabled'), 'saving')
    if (page === 'Rules') {
      const ports = elements.find(node => node.tag === 'NInput' && node.props.some(prop => prop.type === 7 && prop.name === 'model' && prop.exp?.content === 'form.ports'))
      assert.equal(binding(ports, 'disabled'), "saving || form.protocol === 'ICMP'", '独立端口 disabled 不得覆盖保存锁')
    }
  })
}
test('设置：省略隐藏主题，复合和小数时长提交 API', async()=> {
  for (const interval of ['1h30m','1.5h',' 30s ','+30s','1us','1ns']) {
    const calls=[]; const {s}=mount('views/Settings.vue',async(...args)=>{calls.push(args);return {}})
    s.settings.value={theme:'light',interval, tag:'fixture',tc_access_key:'',unknown_key:'drop'}
    await s.save(); assert.equal(calls.length,1,`blocked ${interval}`)
    const body=JSON.parse(calls[0][1].body)
    assert.equal(Object.hasOwn(body,'theme'),false); assert.equal(Object.hasOwn(body,'unknown_key'),false)
    assert.equal(body.interval,interval); assert.equal(body.tc_access_key,'')
  }
})
test('设置：拒绝重复提交，保留输入，卸载后无提示',async()=> {
  const pending=deferred(), calls=[]; const {s,messages,unmount}=mount('views/Settings.vue',(...args)=>{calls.push(args);return pending.promise})
  s.settings.value={interval:'5m'}
  const p=s.save(); const second=s.save(); assert.equal(calls.length,1)
  unmount(); pending.reject(new RequestError(400,'fixture invalid')); await p; await second
  assert.equal(s.settings.value.interval,'5m');assert.equal(messages.length,0)
})
for (const failures of [0,1,2]) {
  test(`扫描清空：${failures} 个产品失败时反馈真实结果`,async()=> {
    let count=0; const {s,messages}=mount('views/Settings.vue',async()=>{}, {clear: async()=> { if (count++ < failures) throw new RequestError(500,'fixture clear fail') }})
    s.clearTarget.value='tc';s.clearConfirm.value=true
    await s.confirmClearScan()
    assert.equal(count,2)
    assert.equal(messages.filter(m=>m.kind==='success').length,failures===0?1:0)
    if(failures) assert.ok(messages.some(m=>m.kind==='error'&&m.text.includes(failures===1?'部分':'未能确认')))
  })
}
test('扫描清空：拒绝重复确认，清空时不启动扫描',async()=> {
  const pending=deferred(), deletes=[], scans=[]
  const {s}=mount('views/Settings.vue',async()=>{}, {clear:ct=>{deletes.push(ct);return pending.promise},scan:async(...args)=>{scans.push(args);return null}})
  s.clearTarget.value='tc';s.clearConfirm.value=true
  const p=s.confirmClearScan();const second=s.confirmClearScan()
  assert.deepEqual(deletes,['tc_lighthouse','tc_cvm'])
  s.tcScan.product='tc_lighthouse';s.tcScan.region='ap-guangzhou';await s.runScan(s.tcScan)
  assert.equal(scans.length,0)
  pending.resolve();await p; await second
})
test('扫描清空：同厂商扫描在途时拒绝，卸载后不提示',async()=> {
  let count=0;const pending=deferred();const {s,messages,unmount}=mount('views/Settings.vue',async()=>{}, {clear:()=>{count++;return pending.promise}})
  s.clearTarget.value='tc';s.clearConfirm.value=true;s.tcScan.loading=true
  const blocked=s.confirmClearScan(); assert.equal(count,0); await blocked
  s.tcScan.loading=false;const p=s.confirmClearScan();const before=messages.length;unmount();pending.resolve();await p
  assert.equal(messages.length,before)
})
function scanned(request) {
  const module=evaluate(readFileSync(`${root}/src/composables/useScannedResources.ts`,'utf8'),name=> name==='vue'?vue:name==='../api'?{request}: (()=>{throw new Error(name)})())
  return module.useScannedResources()
}
test('扫描缓存：DELETE 失败传播错误并保留旧建议',async()=> {
  const fixture=[{cloud_type:'tc_cvm',resource_id:'sg-fixture',resource_name:'',region:'ap-guangzhou'}]
  const s=scanned(async(url,opts)=> {if(opts)throw new RequestError(500,'fixture fail');return fixture})
  await s.load('tc_cvm')
  await assert.rejects(s.clear('tc_cvm'),/fixture fail/)
  assert.equal(s.resourcesOf('tc_cvm')[0].resource_id,'sg-fixture')
})
test('扫描缓存：清空前 GET 晚到不恢复已删除建议',async()=> {
  const pending=deferred(); const s=scanned(async(url,opts)=>opts?{}:pending.promise)
  const p=s.load('tc_cvm');await s.clear('tc_cvm');pending.resolve([{resource_id:'old'}]);await p
  assert.equal(s.resourcesOf('tc_cvm').length,0)
})

test('设置：独立验证主题只由侧栏更新', async()=> {
  const calls=[];const {s}=mount('views/Settings.vue',async(...args)=>{calls.push(args);return {}})
  s.settings.value={interval:'5m',theme:'light'};await s.save()
  assert.equal(calls.length,1)
  assert.equal(Object.hasOwn(JSON.parse(calls[0][1].body),'theme'),false)
})
test('设置：独立验证复合时长进入 API',async()=> {
  const calls=[];const {s}=mount('views/Settings.vue',async(...args)=>{calls.push(args);return {}})
  s.settings.value={interval:'1h30m'};await s.save()
  assert.equal(calls.length,1)
  assert.equal(JSON.parse(calls[0][1].body).interval,'1h30m')
})
test('扫描联合：真实缓存与设置页如实反馈部分清空',async()=> {
  const fixture = ct => [{cloud_type:ct,resource_id:`fixture-${ct}`,resource_name:'',region:'ap-guangzhou'}]
  const request = async(url,opts)=> {
    const ct=new URL(url,'http://fixture.invalid').searchParams.get('cloud_type')
    if(opts && ct==='tc_lighthouse')throw new RequestError(500,'fixture delete fail')
    return opts?{}:fixture(ct)
  }
  const cache=scanned(request)
  await cache.load('tc_lighthouse');await cache.load('tc_cvm')
  const {s,messages}=mount('views/Settings.vue',request,{clear:cache.clear,resourcesOf:cache.resourcesOf})
  s.clearTarget.value='tc';s.clearConfirm.value=true;await s.confirmClearScan()
  assert.equal(messages.some(m=>m.kind==='success'),false,'partial failure cannot report complete success')
  assert.ok(messages.some(m=>m.kind==='error'&&m.text.includes('部分')))
  assert.equal(cache.resourcesOf('tc_lighthouse').length,1)
  assert.equal(cache.resourcesOf('tc_cvm').length,0)
})

for (const [page, save, key, row] of [['Targets', 'saveTarget', 'targets', target], ['Rules', 'saveRule', 'rules', rule]]) {
  test(`${page}：保存前的列表读取晚到不覆盖保存后的结果`, async () => {
    const old = deferred()
    let reads = 0
    const { s } = mount(`views/${page}.vue`, async (url, opts) => {
      if (opts) return {}
      return ++reads === 1 ? old.promise : [row]
    })
    const previous = s.load()
    s.openAdd()
    await s[save]()
    old.resolve([])
    await previous
    assert.equal(s[key].value.length, 1)
    assert.equal(s[key].value[0].id, row.id)
  })
  test(`${page}：网络结果不确定时保留表单，提示核对且不自动重试`, async () => {
    let calls = 0
    const { s, messages } = mount(`views/${page}.vue`, async () => {
      ++calls
      throw new Error('fixture connection lost')
    })
    s.openEdit(row)
    const before = JSON.stringify(s.form.value)
    await s[save]()
    assert.equal(calls, 1)
    assert.equal(s.showModal.value, true)
    assert.equal(JSON.stringify(s.form.value), before)
    assert.equal(s.saving.value, false)
    assert.ok(messages.some(m => m.kind === 'error' && m.text.includes('未能确认保存结果')))
    assert.equal(messages.some(m => m.kind === 'success'), false)
  })
}
