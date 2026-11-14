// Build8 Step 13：真实 SFC 的 EdgeOne 配置页、设置页与路由行为。
import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { deferred, mount } from './helpers/compiled-component.mjs'
const require = createRequire(new URL('../package.json', import.meta.url))
const vue = require('vue')

const settings = {
  dns_enabled: 'true', edgeone_enabled: 'false', tag: 'auto-ddns', interval: '5m',
  edgeone_tag: 'edgeone', edgeone_interval: '72h',
}
const targets = [
  { id: 1, cloud_type: 'tc_cvm', region: 'ap-guangzhou', resource_id: 'sg-a' },
  { id: 2, cloud_type: 'ali_ecs', region: 'cn-hangzhou', resource_id: 'sg-b' },
]
const site = { zone_id: 'zone-a', target_ids: [1, 2], protocol: 'TCP', ports: '80,443', address_family: 'ipv4' }

function routerRequest(overrides = {}) {
  return async (url, opts = {}) => {
    if (overrides[url]) return overrides[url](opts)
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return targets
    if (url === '/api/edgeone/sites') return { sites: [site] }
    return {}
  }
}

test('B8S13 侧栏在域名规则后新增 EdgeOne，深链与历史导航跟随路由高亮', () => {
  const route = vue.reactive({ path: '/edgeone' })
  const app = mount('App.vue', async () => ({}), { route })
  const menu = app.inspect().nodes.find(n => n.type?.name === 'NMenu')
  assert.ok(menu, '侧栏 NMenu 必须渲染')
  assert.equal(menu.props.options.map(o => o.key).join(','), '/,/targets,/rules,/edgeone,/settings,/logs,/dry-run,/alerts')
  assert.equal(menu.props.value, '/edgeone')
  route.path = '/rules'
  assert.equal(app.inspect().nodes.find(n => n.type?.name === 'NMenu').props.value, '/rules')
  route.path = '/edgeone'
  assert.equal(app.inspect().nodes.find(n => n.type?.name === 'NMenu').props.value, '/edgeone')
})

test('B8S13 设置页提交 DNS 开关、两个 TAG 与 EdgeOne 间隔，不碰总暂停', async () => {
  const e = mount('views/Settings.vue', async url => url === '/api/settings' ? { ...settings } : [])
  await e.mounted()
  e.s.settings.value.dns_enabled = 'false'
  e.s.settings.value.edgeone_tag = 'edge-prod'
  e.s.settings.value.edgeone_interval = '36h'
  e.s.settings.value.sync_enabled = 'false'
  await e.s.save()
  const put = e.calls.find(([url, opts]) => url === '/api/settings' && opts?.method === 'PUT')
  assert.ok(put, '必须发生设置 PUT')
  const body = JSON.parse(put[1].body)
  assert.equal(body.dns_enabled, 'false')
  assert.equal(body.edgeone_tag, 'edge-prod')
  assert.equal(body.edgeone_interval, '36h')
  assert.equal(body.sync_enabled, undefined, '总暂停不得由设置页顺带提交')
  assert.equal(body.edgeone_enabled, undefined, '模块开关只从 EdgeOne 页提交')
})

test('B8S13 站点页加载、多目标显示与安全组影响说明', async () => {
  const e = mount('views/EdgeOne.vue', routerRequest())
  await e.mounted()
  const text = e.inspect().text
  assert.match(text, /zone-a/)
  assert.match(text, /tc_cvm · ap-guangzhou · sg-a/)
  assert.match(text, /ali_ecs · cn-hangzhou · sg-b/)
  assert.match(text, /规则会作用于整个安全组及其关联实例/)
  assert.match(text, /绑定清单应覆盖该站点的全部实际源站/)
  assert.equal(e.s.edgeEnabled.value, false)
})

test('B8S13 模块开关只提交字符串 edgeone_enabled', async () => {
  const e = mount('views/EdgeOne.vue', routerRequest())
  await e.mounted()
  await e.s.toggleEdgeOne()
  const put = e.calls.find(([url, opts]) => url === '/api/settings' && opts?.method === 'PUT')
  assert.ok(put)
  assert.deepEqual(JSON.parse(put[1].body), { edgeone_enabled: 'true' })
  assert.equal(e.s.settings.value.edgeone_enabled, 'true')
})

test('B8S13 创建站点提交整数 target_ids，空目标零请求', async () => {
  const requests = []
  const e = mount('views/EdgeOne.vue', async (url, opts = {}) => {
    requests.push([url, opts])
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return targets
    if (url === '/api/edgeone/sites' && opts.method === 'POST') return site
    if (url === '/api/edgeone/sites') return { sites: [site] }
    return {}
  })
  await e.mounted()
  e.s.openCreate()
  e.s.form.zone_id = 'zone-new'
  e.s.form.target_ids = [1, 2]
  e.s.form.protocol = 'TCP'
  e.s.form.ports = '80,443'
  e.s.form.address_family = 'dual'
  await e.s.saveSite()
  const post = requests.find(([url, opts]) => url === '/api/edgeone/sites' && opts.method === 'POST')
  assert.ok(post)
  const body = JSON.parse(post[1].body)
  assert.deepEqual(body.target_ids, [1, 2])
  assert.equal(typeof body.target_ids[0], 'number')
  assert.equal(body.zone_id, 'zone-new')
  assert.equal(body.protocol, 'TCP')
  assert.equal(body.ports, '80,443')
  assert.equal(body.address_family, 'dual')

  const before = requests.length
  e.s.openCreate()
  e.s.form.zone_id = 'zone-empty'
  e.s.form.target_ids = []
  await e.s.saveSite()
  assert.equal(requests.length, before, '空目标不得提交')
  assert.match(e.s.formError.value, /至少选择一个绑定目标/)
})

test('B8S13 保存失败保留表单，重复提交只发一个请求，离页晚到不提示', async () => {
  const pending = deferred()
  let postCalls = 0
  const e = mount('views/EdgeOne.vue', async (url, opts = {}) => {
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return targets
    if (url === '/api/edgeone/sites' && opts.method === 'POST') { postCalls++; return pending.promise }
    if (url === '/api/edgeone/sites') return { sites: [site] }
    return {}
  })
  await e.mounted()
  e.s.openCreate()
  e.s.form.zone_id = 'zone-fail'
  e.s.form.target_ids = [1]
  e.s.form.ports = '443'
  const first = e.s.saveSite()
  const duplicate = e.s.saveSite()
  assert.equal(postCalls, 1, '重复点击只允许一个 POST')
  pending.reject(new Error('fixture save failure'))
  await Promise.all([first, duplicate])
  assert.equal(e.s.showForm.value, true, '失败后表单必须保留')
  assert.match(e.s.formError.value, /fixture save failure/)
  assert.equal(e.s.form.zone_id, 'zone-fail')
  assert.equal(e.s.form.target_ids[0], 1)

  const late = deferred()
  const latePage = mount('views/EdgeOne.vue', async (url, opts = {}) => {
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return targets
    if (url === '/api/edgeone/sites' && opts.method === 'POST') return late.promise
    if (url === '/api/edgeone/sites') return { sites: [site] }
    return {}
  })
  await latePage.mounted()
  latePage.s.openCreate()
  latePage.s.form.zone_id = 'zone-late'
  latePage.s.form.target_ids = [1]
  latePage.s.form.ports = '443'
  const task = latePage.s.saveSite()
  latePage.unmount()
  late.resolve(site)
  await task
  assert.equal(latePage.messages.length, 0, '离页后不得发布晚到成功提示')
})

test('B8S13 导入后重新挂载从 API 回显站点', async () => {
  const imported = { zone_id: 'zone-imported', target_ids: [2], protocol: 'UDP', ports: '53', address_family: 'ipv6' }
  const e = mount('views/EdgeOne.vue', async url => {
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return targets
    if (url === '/api/edgeone/sites') return { sites: [imported] }
    return {}
  })
  await e.mounted()
  const text = e.inspect().text
  assert.match(text, /zone-imported/)
  assert.match(text, /ali_ecs · cn-hangzhou · sg-b/)
  assert.match(text, /UDP/)
  assert.match(text, /ipv6/)
})

test('B8S13 设置保存失败保留用户输入并显示后台错误', async () => {
  const e = mount('views/Settings.vue', async (url, opts = {}) => {
    if (url === '/api/settings' && opts.method === 'PUT') throw new Error('edgeone_interval 非法')
    if (url === '/api/settings') return { ...settings }
    return []
  })
  await e.mounted()
  e.s.settings.value.edgeone_interval = 'not-a-duration'
  await e.s.save()
  assert.equal(e.s.settings.value.edgeone_interval, 'not-a-duration', '失败后必须保留输入')
  assert.ok(e.messages.some(m => m.kind === 'error' && /edgeone_interval 非法/.test(m.text)))
})

test('B8S13 站点编辑与删除调用正确 API', async () => {
  const requests = []
  const e = mount('views/EdgeOne.vue', async (url, opts = {}) => {
    requests.push([url, opts])
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return targets
    if (url === '/api/edgeone/sites' && (!opts.method || opts.method === 'GET')) return { sites: [site] }
    if (url.startsWith('/api/edgeone/sites/') && opts.method === 'PUT') return site
    if (url.startsWith('/api/edgeone/sites/') && opts.method === 'DELETE') return { message: '删除成功' }
    return {}
  })
  await e.mounted()
  e.s.openEdit(site)
  e.s.form.ports = '8443'
  await e.s.saveSite()
  const put = requests.find(([url, opts]) => url === '/api/edgeone/sites/zone-a' && opts.method === 'PUT')
  assert.ok(put, '编辑必须 PUT 原 ZoneId')
  const body = JSON.parse(put[1].body)
  assert.equal(body.ports, '8443')
  assert.deepEqual(body.target_ids, [1, 2])
  e.s.askDelete(site)
  await e.s.confirmDelete()
  const del = requests.find(([url, opts]) => url === '/api/edgeone/sites/zone-a' && opts.method === 'DELETE')
  assert.ok(del, '删除必须 DELETE 原 ZoneId')
})

test('B8S13 无目标选项时站点表单不能提交', async () => {
  const e = mount('views/EdgeOne.vue', async url => {
    if (url === '/api/settings') return { ...settings }
    if (url === '/api/targets') return []
    if (url === '/api/edgeone/sites') return { sites: [] }
    return {}
  })
  await e.mounted()
  e.s.openCreate()
  e.s.form.zone_id = 'zone-empty-options'
  await e.s.saveSite()
  assert.match(e.s.formError.value, /至少选择一个绑定目标/)
  assert.equal(e.calls.filter(([url, opts]) => url === '/api/edgeone/sites' && opts?.method === 'POST').length, 0)
})
