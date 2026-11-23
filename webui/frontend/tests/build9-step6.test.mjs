// Build9 Step 6：执行真实 SFC 的请求、并发守卫、持久恢复与模板语义。
import assert from 'node:assert/strict'
import test from 'node:test'
import { createRequire } from 'node:module'
import { deferred, mount } from './helpers/compiled-component.mjs'
const require = createRequire(new URL('../package.json', import.meta.url))
const { nextTick } = require('vue')
const turn = () => new Promise(resolve => setImmediate(resolve))
const target = (id = 1, resource_id = 'lh-a') => ({ id, cloud_type: 'tc_lighthouse', region: 'ap-test', resource_id })
const targets = [target(), target(2), target(3, 'lh-b'), { ...target(4), cloud_type: 'tc_cvm' }]
const cfg = (overrides = {}) => ({ ...target(), enabled: false, extra_ipv4_allow: 0, pending_review: false, pending_since: '', target_ids: [1, 2], ...overrides })
const site = { zone_id: 'zone-a', target_ids: [2], protocol: 'TCP', ports: '80,443', address_family: 'ipv4' }
const puts = e => e.calls.filter(([, opts]) => opts?.method === 'PUT')
const posts = e => e.calls.filter(([, opts]) => opts?.method === 'POST')
function editor(request = async url => url === '/api/edgeone/sites' ? { sites: [site] } : cfg()) {
  return mount('components/LighthouseCompressionEditor.vue', request, { props: { target: target() } })
}
function storage() {
  const data = new Map()
  return { getItem: key => data.get(key) || null, setItem: (key, value) => data.set(key, value) }
}
function preview(overrides = {}) {
  return { preview_id: 'preview-a', tag: 'edgeone', scope: 'selected', selected_target_ids: [1], all_read: false,
    expires_at: new Date(Date.now() + 600000).toISOString(), summary: { targets: 2, targets_read: 1, targets_unread: 1, deletable_rows: 1, skipped_rows: 1 }, warnings: ['部分目标未读'],
    targets: [{ target_ids: [1, 2], cloud_type: 'tc_lighthouse', region: 'ap-test', resource_id: 'lh-a', sites: [site], status: 'read', revision: '9', deletable_rows: 1, skipped_rows: 1, retained_rows: 4, dns_dependency: { status: 'confirmed', reason: '最后覆盖将被撤除' }, rows: [{ status: 'deletable', protocol: 'TCP', port: '80,443', source: '192.0.2.0/24', action: 'ACCEPT', description: '[edgeone]' }, { status: 'skipped', reason: '定位歧义', protocol: 'TCP', port: '80', source: '198.51.100.0/24' }] },
    { target_ids: [3], cloud_type: 'tc_lighthouse', region: 'ap-test', resource_id: 'lh-b', sites: [], status: 'unread', reason: '读取失败', revision: '', deletable_rows: 0, skipped_rows: 0, retained_rows: 0, dns_dependency: { status: 'unverified' }, rows: [] }], ...overrides }
}
const op = (overrides = {}) => ({ operation_id: 'op-a', preview_id: 'preview-a', tag: 'edgeone', status: 'queued', switch_action: 'unchanged', cleared: false, disable_edgeone: false, targets: [], ...overrides })
function cleanup(request, store = storage()) {
  return mount('components/EdgeOneCleanup.vue', request, { props: { targets, edgeEnabled: true }, storage: store })
}
test('B9S6 实例读取别名和全部站点；开启必须确认，取消零写入，显式 0 可保存', async () => {
  const e = editor(async (url, opts) => url === '/api/edgeone/sites' ? { sites: [site] } : cfg(opts?.method === 'PUT' ? { enabled: true } : {}))
  await e.mounted()
  e.s.enabled.value = true
  e.s.requestSave()
  assert.equal(e.s.confirming.value, true)
  assert.equal(puts(e).length, 0)
  assert.match(e.inspect().text, /zone-a.*TCP \/ 80,443/)
  assert.match(e.inspect().text, /尚未测得容量与放行范围/)
  e.s.confirming.value = false
  assert.equal(puts(e).length, 0)
  e.s.requestSave()
  await e.s.save()
  assert.deepEqual(JSON.parse(puts(e)[0][1].body), { enabled: true, extra_ipv4_allow: 0 })
  assert.equal(e.s.config.value.target_ids.join(','), '1,2')
})
test('B9S6 非法或缺失额度不提交；读取失败不允许保存', async () => {
  const e = editor(); await e.mounted()
  for (const allowance of [null, -1, 1.5, 4294967297]) { e.s.allowance.value = allowance; e.s.requestSave() }
  assert.equal(puts(e).length, 0)
  assert.match(e.s.error.value, /明确输入/)
  const failed = editor(async () => { throw new Error('fixture') }); await failed.mounted(); failed.s.requestSave()
  assert.equal(puts(failed).length, 0)
})
test('B9S6 实例保存单在途、关闭后待复查可见；卸载后晚到不提示', async () => {
  const pending = deferred()
  const e = editor(async (url, opts) => opts?.method === 'PUT' ? pending.promise : url === '/api/edgeone/sites' ? { sites: [site] } : cfg({ enabled: true }))
  await e.mounted(); e.s.enabled.value = false
  const first = e.s.save(); await e.s.save()
  assert.equal(puts(e).length, 1)
  pending.resolve(cfg({ pending_review: true, pending_since: '2026-11-23' })); await first
  assert.match(e.inspect().text, /关闭后待复查/)
  assert.match(e.inspect().text, /关闭开关不会立即撤销云端放行/)
  const late = deferred(), x = editor(() => late.promise), task = x.mounted(); x.unmount(); late.resolve(cfg()); await task
  assert.equal(x.s.config.value, null)
})
test('B9S6 新绑、改绑、多实例及别名即时提示；过时读取不能覆盖新选择', async () => {
  const old = deferred()
  const e = mount('components/LighthouseBindingHints.vue', async url => url.includes('/1/') ? old.promise : cfg({ resource_id: 'lh-b', enabled: true, extra_ipv4_allow: 42 }), { props: { targets, targetIds: [1, 2, 3, 4] } })
  assert.equal(e.s.instances.value.length, 2)
  assert.match(e.inspect().text, /状态未知/)
  assert.match(e.inspect().text, /绕过 EdgeOne/)
  e.inspect({ targetIds: [3] }); await nextTick(); await turn()
  old.resolve(cfg()); await turn()
  assert.equal(e.s.states.value[1], undefined)
  assert.match(e.inspect().text, /已开启.*42/)
  assert.equal(puts(e).length, 0)
  assert.equal(posts(e).length, 0)
})
test('B9S6 已有绑定和编辑表单均接入提示；深链打开指定实例且不自动保存', async () => {
  const edge = mount('views/EdgeOne.vue', async url => url === '/api/targets' ? targets : url === '/api/edgeone/sites' ? { sites: [site] } : {})
  await edge.mounted(); edge.s.openEdit(site)
  assert.equal(edge.inspect().nodes.filter(n => n.type?.name === 'LighthouseBindingHints').length, 2)
  const e = mount('views/Targets.vue', async url => url === '/api/targets' ? targets : {}, { route: { query: { compression: '2' } } })
  await e.mounted()
  assert.equal(e.s.editingId.value, 2)
  assert.equal(e.inspect().nodes.find(n => n.type?.name === 'LighthouseCompressionEditor').props.target.id, 2)
  e.s.form.value.resource_id = 'different'
  assert.equal(e.inspect().nodes.some(n => n.type?.name === 'LighthouseCompressionEditor'), false)
  assert.equal(puts(e).length, 0)
})
const compression = (overrides = {}) => ({ applicable: true, enabled: true, assumed: false, pending_review: false, pending_known: true, mode: 'lossless', s0_rows: 8, other_new_rows: 2, add_rows: 86, normal_cap: 96, reserved: 4, hard_cap: 100, normal_budget: 86, peak: 96, borrowed: 0, expected_cleanup: 0, expected_after: 96, risk_limit: 10, current_extra_ipv4: 0, extra_ipv4: 8, max_extra_run: 4, per_port: [{ Protocol: 'TCP', Port: '443', ExtraIPv4: 8, MaxExtraRun: 4, Ranges: ['192.0.2.4-192.0.2.7'] }], rows: [], blockers: [], warnings: [], reserved_restored: null, residual_rows: 0, ...overrides })
test('B9S6 模拟区分有效、假设、未知和退出残留；零和 null 显示不同', () => {
  const e = mount('components/CompressionPreview.vue', undefined, { props: { compression: compression(), dataComplete: true } })
  assert.match(e.inspect().text, /有效规划/)
  assert.match(e.inspect().text, /预计清理后空位 4/)
  assert.match(e.inspect().text, /TCP \/ 443.*192.0.2.4/)
  const assumed = e.inspect({ compression: compression({ assumed: true, enabled: false, pending_review: true, peak: null, risk_limit: 0 }) }).text
  assert.match(assumed, /假设启用的只读测算/)
  assert.match(assumed, /共存峰值 未知/)
  assert.match(assumed, /风险额度 0/)
  assert.match(assumed, /关闭后待复查/)
  const unknown = e.inspect({ dataComplete: false, compression: compression({ peak: null, pending_known: false, blockers: [{ Code: 'capacity', Message: '名额不足' }] }) }).text
  assert.match(unknown, /实际占用 未知/)
  assert.match(unknown, /待复查状态未知/)
  assert.match(unknown, /capacity：名额不足/)
  assert.doesNotMatch(unknown, /有效规划/)
})
test('B9S6 DryRun 物理执行单元接线保留压缩原 DTO', () => {
  const c = compression(), e = mount('components/DryRunResults.vue')
  const result = e.inspect({ status: 'completed', results: [], warnings: [], error: '', executionUnits: [{ target_key: 'lh', target_ids: [1, 2], provider: 'lh', sites: [], protected_cidrs: [], conflicts: [], cleanup_candidates: [], cleanup_deletable: [], cleanup_protected: [], compression: c, data_complete: false }] })
  assert.deepEqual(JSON.parse(JSON.stringify(result.nodes.find(n => n.type?.name === 'CompressionPreview').props.compression)), c)
  assert.match(result.text, /过渡期峰值 未知/)
})
test('B9S6 清理空选择零请求、显式全部、物理别名合并且保留解绑目标', async () => {
  const e = cleanup(async () => preview())
  await e.s.buildPreview(); assert.equal(posts(e).length, 0)
  assert.equal(e.s.options.value.length, 3)
  assert.match(e.s.options.value[0].label, /#1.*#2/)
  e.s.all.value = true
  await e.s.buildPreview()
  assert.deepEqual(JSON.parse(posts(e)[0][1].body), { all: true })
  assert.equal(e.s.disable.value, false)
  const text = e.inspect().text
  for (const pattern of [/TAG \[edgeone\]/, /可删 1/, /跳过 1/, /未读 1/, /最后覆盖将被撤除/, /已解绑／无绑定/, /可能中断回源/, /影响全部站点/]) assert.match(text, pattern)
  const table = e.inspect().nodes.find(n => n.type?.name === 'NDataTable')
  assert.equal(table.props.data[1].reason, '定位歧义')
  assert.ok(table.props.columns.some(c => c.key === 'reason'))
  e.s.cancel(); await e.s.confirm()
  assert.equal(posts(e).length, 1)
})
test('B9S6 预览重复点击单请求、未读不授权，过期需重新预览', async () => {
  const pending = deferred(), e = cleanup(() => pending.promise)
  e.s.selected.value = [1]
  const first = e.s.buildPreview(); await e.s.buildPreview()
  assert.equal(posts(e).length, 1)
  pending.resolve(preview({ preview_id: '' })); await first; await e.s.confirm()
  assert.equal(posts(e).length, 1)
  e.s.preview.value = preview({ expires_at: '2000-01-01T00:00:00Z' }); await e.s.confirm()
  assert.match(e.s.error.value, /预览已过期/)
  assert.equal(posts(e).length, 1)
})
test('B9S6 确认重复点击单请求，默认保持开关且只提交预览 ID 和明确布尔值', async () => {
  const pending = deferred(), e = cleanup(async url => url.endsWith('/preview') ? preview() : pending.promise)
  e.s.selected.value = [1]; await e.s.buildPreview()
  const first = e.s.confirm(), duplicate = e.s.confirm()
  assert.equal(posts(e).length, 2)
  assert.deepEqual(JSON.parse(posts(e)[1][1].body), { preview_id: 'preview-a', disable_edgeone: false })
  pending.resolve(op()); await Promise.all([first, duplicate])
  assert.equal(e.s.operation.value.operation_id, 'op-a')
  assert.equal(e.timers.size, 1)
  e.unmount(); assert.equal(e.timers.size, 0)
})
test('B9S6 网络中断用原预览和原关闭选项核对受理；刷新恢复进度只发 GET', async () => {
  const store = storage(); let attempt = 0
  const e = cleanup(async url => url.endsWith('/preview') ? preview() : ++attempt === 1 ? Promise.reject(new Error('disconnected')) : op(), store)
  e.s.selected.value = [1]; await e.s.buildPreview(); e.s.disable.value = true; await e.s.confirm()
  assert.match(e.s.error.value, /尚未确认/)
  assert.equal(e.s.pending.value.disable_edgeone, true)
  e.s.disable.value = false; await e.s.confirm()
  assert.deepEqual(JSON.parse(posts(e)[2][1].body), { preview_id: 'preview-a', disable_edgeone: true })
  e.unmount()
  const restored = cleanup(async () => op({ status: 'completed' }), store); await restored.mounted()
  assert.equal(posts(restored).length, 0)
  assert.equal(restored.calls[0][0], '/api/edgeone/firewall-cleanup/op-a')
  assert.equal(restored.timers.size, 0)
})
test('B9S6 中断且尚无操作 ID 时跨页面恢复只在显式核对后提交原授权', async () => {
  const store = storage(), e = cleanup(async url => url.endsWith('/preview') ? preview() : Promise.reject(new Error('lost')), store)
  e.s.selected.value = [1]; await e.s.buildPreview(); await e.s.confirm(); e.unmount()
  const restored = cleanup(async () => op(), store); await restored.mounted()
  assert.equal(posts(restored).length, 0)
  await restored.s.buildPreview(); assert.equal(posts(restored).length, 0)
  await restored.s.confirm(); assert.equal(posts(restored).length, 1)
})
test('B9S6 独立目标部分成功与未知计数可见；completed 不冒充清空', async () => {
  const result = op({ status: 'completed', targets: [{ ...target(), target_ids: [1, 2], confirmed_deleted: 2, absent_rows: 3, remaining_rows: 1, readback_verified: true, outcome_unknown: false, status: 'partial' }, { ...target(3), target_ids: [3], confirmed_deleted: 0, absent_rows: null, remaining_rows: null, readback_verified: false, outcome_unknown: true, status: 'unknown' }] })
  const e = cleanup(async url => url.endsWith('/preview') ? preview() : result)
  e.s.selected.value = [1]; await e.s.buildPreview(); await e.s.confirm()
  const text = e.inspect().text
  assert.match(text, /云确认删除 2；回读已不存在 3；当前 TAG 残留 1/)
  assert.match(text, /回读已不存在 未知；当前 TAG 残留 未知/)
  assert.match(text, /提交结果未知/)
  assert.match(text, /尚未证明已清空/)
  e.s.operation.value = { ...result, cleared: true }
  assert.match(e.inspect().text, /已清空：所有所选目标均可信回读/)
})
test('B9S6 进度读取断开后继续回读；离页旧响应不更新 UI', async () => {
  let reads = 0
  const e = cleanup(async url => url.endsWith('/preview') ? preview() : url.endsWith('/op-a') ? ++reads === 1 ? Promise.reject(new Error('lost')) : op({ status: 'completed' }) : op())
  e.s.selected.value = [1]; await e.s.buildPreview(); await e.s.confirm(); await e.runTimers()
  assert.match(e.s.error.value, /进度读取失败/)
  await e.runTimers(); assert.equal(e.s.operation.value.status, 'completed'); assert.equal(e.timers.size, 0)
  const late = deferred(), x = cleanup(() => late.promise); x.s.operation.value = op()
  const read = x.s.readResult(); x.unmount(); late.resolve(op({ cleared: true })); await read
  assert.equal(x.s.operation.value.cleared, false)
})
test('B9S6 v5 服务端预览列出开启实例和额度，原文件未经改写才确认导入', async () => {
  const file = new File(['{"version":5,"original":"bytes"}'], 'v5.json')
  const e = mount('views/Settings.vue', async url => url.endsWith('/preview') ? { version: 5, compression_enabled_count: 1, lighthouse_compression: [{ ...cfg({ enabled: true, extra_ipv4_allow: 123 }), target_count: 2 }] } : {})
  await e.s.importConfig({ target: { files: [file], value: 'x' } })
  assert.equal(posts(e).length, 1)
  assert.equal(posts(e)[0][0], '/api/config/import/preview')
  assert.equal(posts(e)[0][1].body, file)
  assert.match(e.inspect().text, /显式开启 1 个物理实例/)
  assert.match(e.inspect().text, /lh-a：额外放行 IPv4 上限 123/)
  await e.s.confirmImport()
  assert.equal(posts(e)[1][0], '/api/config/import')
  assert.equal(posts(e)[1][1].body, file)
})
test('B9S6 导入预览失败或尚在途不得导入、替换文件；取消零导入', async () => {
  const pending = deferred(), file = new File(['{"version":4}'], 'old.json')
  const e = mount('views/Settings.vue', () => pending.promise)
  const first = e.s.importConfig({ target: { files: [file], value: '' } })
  await turn(); await e.s.confirmImport(); assert.equal(posts(e).length, 1)
  await e.s.importConfig({ target: { files: [file], value: '' } }); assert.equal(posts(e).length, 1)
  pending.reject(new Error('strict preview rejection')); await first
  assert.equal(e.s.showImportConfirm.value, false)
  assert.equal(e.s.pendingImport.value, null)
  assert.match(e.messages[0].text, /导入预览失败/)
  const old = mount('views/Settings.vue', async () => ({ version: 4, compression_enabled_count: 0, lighthouse_compression: [] }))
  await old.s.importConfig({ target: { files: [file], value: '' } })
  assert.match(old.inspect().text, /旧版包默认关闭/)
  old.s.cancelImport(); await old.s.confirmImport(); assert.equal(posts(old).length, 1)
})
test('B9S6 预览配置失效 409 与引擎 503 明确拒绝可重新预览', async () => {
  for (const status of [409, 503]) {
    let e
    e = cleanup(async url => {
      if (url.endsWith('/preview')) return preview()
      throw new e.RequestError(status, 'rejected')
    })
    e.s.selected.value = [1]; await e.s.buildPreview(); await e.s.confirm()
    assert.equal(e.s.pending.value, null)
    assert.equal(e.s.show.value, false)
    assert.match(e.s.error.value, /重新预览/)
    await e.s.buildPreview(); assert.equal(posts(e).length, 3)
  }
})
test('B9S6 受理后 503 携带操作 ID 时只读回查；重启 404 后须新预览', async () => {
  let e
  e = cleanup(async url => {
    if (url.endsWith('/preview')) return preview()
    if (url.endsWith('/op-rejected')) return op({ operation_id: 'op-rejected', status: 'rejected', reason_code: 'engine_unavailable' })
    const error = new e.RequestError(503, 'queue rejected'); error.operationId = 'op-rejected'; throw error
  })
  e.s.selected.value = [1]; await e.s.buildPreview(); await e.s.confirm()
  assert.equal(e.s.operation.value.status, 'rejected')
  assert.equal(posts(e).length, 2)
  assert.equal(e.s.pending.value, null)
  let missing
  missing = cleanup(async () => { throw new missing.RequestError(404, 'not found') })
  missing.s.operation.value = op(); await missing.s.readResult()
  assert.equal(missing.s.operation.value.status, 'unavailable')
  assert.equal(missing.s.running.value, false)
  assert.equal(missing.timers.size, 0)
  assert.match(missing.s.error.value, /重新预览核对云端现状/)
})
test('B9S6 资源表单在实例设置保存时拒绝修改、删除与切换', async () => {
  const e = mount('views/Targets.vue', async () => targets)
  e.s.targets.value = targets; e.s.openEdit(target())
  e.s.compressionSaving.value = true
  e.s.openAdd(); e.s.openEdit(target(3, 'lh-b')); e.s.openDeleteConfirm(target()); await e.s.saveTarget(); await e.s.testConnection()
  assert.equal(e.s.editingId.value, 1)
  assert.equal(e.calls.length, 0)
  assert.equal(e.s.deletePhase.value, 'idle')
})
