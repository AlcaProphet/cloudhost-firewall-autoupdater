// Build8 Step 14：仪表盘、站点状态与 Dry Run 三种 200 结果的可区分展示。
import assert from 'node:assert/strict'
import test from 'node:test'
import { deferred, mount } from './helpers/compiled-component.mjs'

const baseStatus = extra => ({
  running: true, enabled: true, last_sync: null, last_success: null, last_round: null,
  round_started_at: null, process_started_at: '2026-11-14T00:00:00Z',
  dns: { enabled: true, effective: true, next_due_at: '2026-11-14T00:05:00Z' },
  edgeone: { enabled: true, effective: true, next_due_at: '2026-11-17T00:00:00Z' },
  address_preflight: { state: 'clear', dns_complete: true, edgeone_complete: true, known_conflict: false, protected_cidrs: [] },
  ...extra,
})
const site = extra => ({
  zone_id: 'zone-a', target_ids: [1], current_version: 'v1', pending_version: '',
  planned_active_at: null, last_query_at: null, last_query_request_id: '', last_query_error: '',
  coverage: [], confirmation_state: 'not_attempted', confirmation_request_id: '',
  last_confirmation_error: '', actual_effective: false, actual_active_at: null, actual_confirmed: null,
  protected_cidrs: [], transition_base_version: 'v1', transition_pending_version: '',
  transition_updated_at: null, transition_error: '', ...extra,
})

function dashboardRequest(preflight, sites = []) {
  return async url => {
    if (url === '/api/sync/status') return baseStatus({ address_preflight: preflight })
    if (url === '/api/edgeone/status') return { enabled: true, effective: true, next_due_at: null, sites }
    if (url === '/api/targets' || url === '/api/rules') return []
    return {}
  }
}

for (const tc of [
  ['clear', { state: 'clear', dns_complete: true, edgeone_complete: true, known_conflict: false, protected_cidrs: [] }, 'success', /完整，未发现地址重叠/],
  ['overlap', { state: 'overlap', dns_complete: true, edgeone_complete: true, known_conflict: true, protected_cidrs: [], conflict: { dns_name: 'home.example', dns_address: '203.0.113.7', edgeone_prefix: '203.0.113.0/24', zone_id: 'zone-a' } }, 'error', /地址重叠/],
  ['data_unavailable', { state: 'data_unavailable', dns_complete: false, edgeone_complete: true, known_conflict: false, protected_cidrs: ['198.51.100.0/24'] }, 'warning', /数据不足：DNS 侧输入不完整.*单侧继续/],
  ['single_source', { state: 'single_source', dns_complete: true, edgeone_complete: true, known_conflict: false, protected_cidrs: [] }, 'info', /仅单项来源启用/],
]) {
  test(`B8S14 仪表盘预检 ${tc[0]} 使用独立提示`, async () => {
    const e = mount('views/Dashboard.vue', dashboardRequest(tc[1]))
    await e.mounted()
    await e.s.fetchStatus()
    const node = e.inspect().nodes.find(n => n.props?.['data-testid'] === 'preflight-hint')
    assert.ok(node, '必须渲染预检提示')
    assert.equal(node.props.type, tc[2])
    assert.match(e.inspect().text, tc[3])
  })
}

test('B8S14 仪表盘展示两侧启用/到期和四类站点确认状态', async () => {
  const sites = [
    site({ zone_id: 'zone-pending', confirmation_state: 'not_attempted' }),
    site({ zone_id: 'zone-unknown', confirmation_state: 'unknown' }),
    site({ zone_id: 'zone-accepted', confirmation_state: 'accepted' }),
    site({ zone_id: 'zone-effective', actual_effective: true }),
  ]
  const e = mount('views/Dashboard.vue', dashboardRequest(baseStatus({}).address_preflight, sites))
  await e.mounted()
  await e.s.fetchStatus()
  await e.s.fetchEdgeStatus()
  const text = e.inspect().text
  assert.match(text, /DNS：已启用并生效；下次到期：/)
  assert.match(text, /EdgeOne：已启用并生效；下次到期：/)
  assert.match(text, /待确认/)
  assert.match(text, /结果未知/)
  assert.match(text, /已确认，等待生效/)
  assert.match(text, /已生效/)
  assert.match(text, /暂无覆盖记录/)
  assert.match(text, /最近查询：暂无记录/)
})

test('B8S14 仪表盘 EdgeOne 状态乱序响应只接受最新请求', async () => {
  const a = deferred(), b = deferred()
  let calls = 0
  const e = mount('views/Dashboard.vue', async url => {
    if (url === '/api/edgeone/status') return ++calls === 1 ? a.promise : b.promise
    if (url === '/api/sync/status') return baseStatus({})
    return []
  })
  await e.mounted()
  const old = e.s.fetchEdgeStatus(), latest = e.s.fetchEdgeStatus()
  b.resolve({ enabled: true, effective: true, next_due_at: null, sites: [site({ zone_id: 'newest' })] })
  await latest
  a.resolve({ enabled: true, effective: true, next_due_at: null, sites: [site({ zone_id: 'stale' })] })
  await old
  const text = e.inspect().text
  assert.match(text, /newest/)
  assert.doesNotMatch(text, /stale/)
})

test('B8S14 EdgeOne 页面状态区区分覆盖/确认/查询记录并展示保护', async () => {
  const sites = [
    site({
      zone_id: 'zone-e', confirmation_state: 'accepted', actual_effective: true,
      last_query_at: '2026-11-14T01:02:03Z', protected_cidrs: ['198.51.100.0/24'],
      coverage: [{ target_id: 1, target_key: 'tc_cvm\u0000ap-guangzhou\u0000sg-a', complete: true, reasons: [] }],
    }),
  ]
  const e = mount('views/EdgeOne.vue', async url => {
    if (url === '/api/settings') return { edgeone_enabled: 'true' }
    if (url === '/api/targets') return [{ id: 1, cloud_type: 'tc_cvm', region: 'ap-guangzhou', resource_id: 'sg-a' }]
    if (url === '/api/edgeone/sites') return { sites: [{ zone_id: 'zone-e', target_ids: [1], protocol: 'TCP', ports: '443', address_family: 'ipv4' }] }
    if (url === '/api/edgeone/status') return { enabled: true, effective: true, next_due_at: null, sites }
    return {}
  })
  await e.mounted()
  await e.s.loadStatus()
  const text = e.inspect().text
  assert.match(text, /当前版本.*v1/)
  assert.match(text, /覆盖 1\/1/)
  assert.match(text, /已生效/)
  assert.match(text, /过渡保护：198\.51\.100\.0\/24/)
})

function preflight(state, extra = {}) {
  return { state, dns_complete: true, edgeone_complete: true, known_conflict: false, protected_cidrs: [], error: '', ...extra }
}
const unit = extra => ({
  target_key: 'tc_cvm\u0000ap-guangzhou\u0000sg-a', target_ids: [1], provider: 'tc_cvm(sg-a)',
  sites: [], protected_cidrs: [], rules_read: 2, current_rule_count: 2, planned_add: 1,
  transition_peak: 3, limit: null, limit_source: 'unknown', limit_exceeded: false,
  coverage_ready: true, data_complete: true, conflicts: [], cleanup_candidates: [], cleanup_deletable: [],
  cleanup_protected: [], error: '', ...extra,
})

for (const tc of [
  ['clear', preflight('clear'), 'success', /完整，未发现地址重叠/],
  ['overlap', preflight('overlap', { known_conflict: true, conflict: { dns_name: 'a', dns_address: '203.0.113.7', edgeone_prefix: '203.0.113.0/24', zone_id: 'zone-a' } }), 'error', /全局地址重叠/],
  ['data_unavailable', preflight('data_unavailable', { dns_complete: false }), 'warning', /数据不足：DNS 侧输入不完整.*单侧继续/],
  ['single_source', preflight('single_source'), 'info', /仅单项来源启用/],
]) {
  test(`B8S14 DryRun 同一 200 的 ${tc[0]} 结果可区分`, () => {
    const e = mount('components/DryRunResults.vue')
    const out = e.inspect({
      status: 'completed', results: [], warnings: [], preflight: tc[1], executionUnits: [unit()], error: '',
    })
    const alert = out.nodes.find(n => n.props?.['data-testid'] === 'dryrun-preflight')
    assert.ok(alert, '必须渲染 DryRun 预检提示')
    assert.equal(alert.props.type, tc[2])
    assert.match(out.text, tc[3])
  })
}

test('B8S14 DryRun 执行单元显示容量、未知限额、保护与站点版本', () => {
  const e = mount('components/DryRunResults.vue')
  const out = e.inspect({
    status: 'completed', results: [], warnings: [],
    preflight: preflight('clear'),
    executionUnits: [unit({
      protected_cidrs: ['198.51.100.0/24'], cleanup_protected: [{ protocol: 'TCP', port: '443', cidr: '198.51.100.0/24' }],
      sites: [{ zone_id: 'zone-a', target_ids: [1], current_version: 'v1', pending_version: 'v2', planned_active_at: null, address_family: 'ipv4', data_complete: true, reasons: [], protected_cidrs: ['198.51.100.0/24'] }],
    })],
    error: '',
  })
  assert.match(out.text, /已读规则 2/)
  assert.match(out.text, /已知限额 未知/)
  assert.match(out.text, /站点 zone-a.*当前 v1.*待更新 v2/)
  assert.match(out.text, /保护 198\.51\.100\.0\/24/)
  assert.match(out.text, /受过渡保护 1/)
})

test('B8S14 总暂停时立即同步禁用，模拟页仍可独立执行', async () => {
  const e = mount('views/Dashboard.vue', async url => {
    if (url === '/api/sync/status') return baseStatus({ enabled: false })
    if (url === '/api/edgeone/status') return { enabled: true, effective: false, next_due_at: null, sites: [] }
    return []
  })
  await e.mounted()
  await e.s.fetchStatus()
  const buttons = e.inspect().nodes.filter(n => n.type?.name === 'NButton')
  assert.ok(e.inspect().text.includes('立即同步'), '必须渲染立即同步按钮')
  assert.ok(buttons.some(n => n.props && 'disabled' in n.props), '总暂停时正式触发必须禁用')
  const runTest = mount('views/RunTest.vue', async () => ({ results: [], warnings: [], preflight: preflight('single_source'), execution_units: [] }))
  await runTest.mounted()
  await runTest.s.runDryRun()
  assert.equal(runTest.calls.filter(([url]) => url === '/api/sync/dryrun').length, 1)
})

test('B8S14 仪表盘预检两侧均不足时阻断两侧', async () => {
  const e = mount('views/Dashboard.vue', dashboardRequest({
    state: 'data_unavailable', dns_complete: false, edgeone_complete: false,
    known_conflict: false, protected_cidrs: [], observed_at: '2026-11-14T00:01:00Z',
  }))
  await e.mounted()
  await e.s.fetchStatus()
  const node = e.inspect().nodes.find(n => n.props?.['data-testid'] === 'preflight-hint')
  assert.ok(node)
  assert.equal(node.props.type, 'warning')
  assert.match(e.inspect().text, /两侧输入均不完整.*阻断两侧/)
  assert.doesNotMatch(e.inspect().text, /单侧继续/)
})

test('B8S14 仪表盘未观察预检不误报数据不足', async () => {
  const e = mount('views/Dashboard.vue', dashboardRequest({
    state: 'data_unavailable', dns_complete: false, edgeone_complete: false,
    known_conflict: false, protected_cidrs: [],
  }))
  await e.mounted()
  await e.s.fetchStatus()
  const node = e.inspect().nodes.find(n => n.props?.['data-testid'] === 'preflight-hint')
  assert.ok(node)
  assert.equal(node.props.type, 'info')
  assert.match(e.inspect().text, /尚未完成首次全局预检/)
  assert.doesNotMatch(e.inspect().text, /数据不足：/)
})

test('B8S14 仪表盘站点状态读取失败明确提示，不冒充暂无记录', async () => {
  const e = mount('views/Dashboard.vue', async url => {
    if (url === '/api/sync/status') return baseStatus({})
    if (url === '/api/edgeone/status') throw new Error('fixture status failed')
    return []
  })
  await e.mounted()
  await e.s.fetchStatus()
  await e.s.fetchEdgeStatus()
  const node = e.inspect().nodes.find(n => n.props?.['data-testid'] === 'edge-status-error')
  assert.ok(node, '必须渲染站点状态错误提示')
  assert.match(e.inspect().text, /读取站点状态失败：fixture status failed/)
  assert.doesNotMatch(e.inspect().text, /暂无站点运行状态记录/)
})

test('B8S14 DryRun 双侧不足提示阻断两侧', () => {
  const e = mount('components/DryRunResults.vue')
  const out = e.inspect({
    status: 'completed', results: [], warnings: [],
    preflight: preflight('data_unavailable', { dns_complete: false, edgeone_complete: false }),
    executionUnits: [], error: '',
  })
  const alert = out.nodes.find(n => n.props?.['data-testid'] === 'dryrun-preflight')
  assert.ok(alert)
  assert.match(out.text, /两侧输入均不完整.*阻断两侧/)
  assert.doesNotMatch(out.text, /单侧继续/)
})

test('B8S14 DryRun 仅 EdgeOne 站点目标显示真实计划，不冒充无适用规则', () => {
  const e = mount('components/DryRunResults.vue')
  const result = {
    target_id: 1, execution_target_key: 'tc_cvm\u0000ap-guangzhou\u0000sg-a',
    provider: 'tc_cvm(sg-a)', domains: [], edgeone_sites: ['zone-a'],
    desired: [], satisfied_by_owned: [], satisfied_by_external: [],
    to_add: [{ protocol: 'TCP', port: '443', action: 'ACCEPT', cidr: '198.51.100.0/24', desc: '[edgeone]' }],
    cleanup_candidates: [], cleanup_deferred: [], dns_errors: [], unsupported: [], conflicts: [],
    coverage_ready: false, error: '',
  }
  const out = e.inspect({
    status: 'completed', results: [result], warnings: [],
    preflight: preflight('single_source'), executionUnits: [], error: '',
  })
  assert.match(out.text, /EdgeOne 站点：zone-a/)
  assert.match(out.text, /待新增/)
  assert.doesNotMatch(out.text, /无适用规则/)
  assert.doesNotMatch(out.text, /正式同步将跳过此目标/)
})

test('B8S14 仪表盘未观察但已知冲突仍提示阻断', async () => {
  const e = mount('views/Dashboard.vue', dashboardRequest({
    state: 'data_unavailable', dns_complete: false, edgeone_complete: false,
    known_conflict: true, protected_cidrs: [],
  }))
  await e.mounted()
  await e.s.fetchStatus()
  const node = e.inspect().nodes.find(n => n.props?.['data-testid'] === 'preflight-hint')
  assert.ok(node)
  assert.equal(node.props.type, 'error')
  assert.match(e.inspect().text, /存在已查实地址重叠；正式同步保持保守阻断/)
})

test('B8S14 DryRun 复用执行单元并按物理目标统计新增和清理', () => {
  const e = mount('components/DryRunResults.vue')
  const targetKey = 'tc_cvm\u0000ap-guangzhou\u0000sg-a'
  const change = { protocol: 'TCP', port: '443', cidr: '198.51.100.0/24' }
  const result = { provider: 'tc_cvm(sg-a)', domains: [], edgeone_sites: [], execution_target_key: targetKey }
  const out = e.inspect({
    status: 'completed', warnings: [], error: '', preflight: preflight('single_source'),
    results: [
      { ...result, target_id: 1, edgeone_sites: ['zone-a'], to_add: [change, change], cleanup_candidates: [change, change] },
      { ...result, target_id: 2 },
    ],
    executionUnits: [unit({ target_key: targetKey, target_ids: [1, 2], planned_add: 1, cleanup_candidates: [change] })],
  })
  assert.match(out.text, /共用物理目标/)
  assert.match(out.text, /查看共同执行单元/)
  assert.doesNotMatch(out.text, /正式同步将跳过此目标/)
  for (const label of ['待新增', '清理候选']) {
    const stat = out.nodes.find(n => n.props?.label === label)
    assert.equal(stat?.props.value, 1, label)
  }
})

test('B8S14 预检读库失败时两侧样本完整仍提示阻断', async () => {
  const blocked = preflight('data_unavailable', { error: '地址冲突记录读取失败' })
  const dry = mount('components/DryRunResults.vue').inspect({
    status: 'completed', results: [], warnings: [], preflight: blocked, executionUnits: [], error: '',
  })
  assert.match(dry.text, /正式同步会阻断两侧/)
  assert.doesNotMatch(dry.text, /单侧继续/)
  const dash = mount('views/Dashboard.vue', dashboardRequest(blocked))
  await dash.mounted()
  await dash.s.fetchStatus()
  assert.match(dash.inspect().text, /正式同步已阻断两侧/)
  assert.doesNotMatch(dash.inspect().text, /单侧继续/)
})

test('B8S14 单源运行仅提示历史冲突', async () => {
  const history = preflight('single_source', { known_conflict: true })
  const dash = mount('views/Dashboard.vue', dashboardRequest(history))
  await dash.mounted()
  await dash.s.fetchStatus()
  const node = dash.inspect().nodes.find(n => n.props?.['data-testid'] === 'preflight-hint')
  assert.equal(node?.props.type, 'info')
  assert.match(dash.inspect().text, /保留历史地址冲突记录.*重新双开后复查/)
  assert.doesNotMatch(dash.inspect().text, /正式同步.*阻断/)
})
