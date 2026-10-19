// I8-R21：检查真实模板输出、可见消息、请求与计时器，不复制组件状态机。
import assert from 'node:assert/strict'
import test from 'node:test'
import { mount, deferred } from './helpers/compiled-component.mjs'
const button = (e, handler) => e.inspect().nodes.find(n => n.type?.name === 'NButton' && n.props?.onClick === handler)
const hint = e => e.inspect().nodes.find(n => n.props?.['data-testid'] === 'sync-health-hint')
const status = extra => ({ running: true, enabled: true, last_sync: null, last_success: '2000-01-01T00:00:00Z', last_round: null, ...extra })
const round = (outcome, extra = {}) => ({ outcome, total: outcome === 'idle' ? 0 : 1, failed: outcome === 'failed' ? 1 : 0,
  skipped: outcome === 'partial' ? 1 : 0, added: 0, deleted: 0, cleanup_deleted: 0,
  cleanup_observation_summary: { complete: true, observed_candidates: 0, observed_deferred: 0 }, ...extra })
function join(t, pending, tasks) {
  t.after(async () => { for (const p of pending) p.resolve({}); await Promise.all(tasks) })
}

test('I8R21 告警保存按钮真实模板覆盖加载与保存完整禁用矩阵', () => {
  const e = mount('views/Alerts.vue')
  for (const loadState of ['loading', 'error', 'ready']) for (const saving of [false, true]) {
    e.s.loadState.value = loadState; e.s.saving.value = saving
    const node = button(e, e.s.save)
    assert.equal(node.props.disabled, saving || loadState !== 'ready', `R21_DISABLED ${loadState}/${saving}`)
    assert.equal(node.props.loading, saving)
  }
})

test('I8R21 四种后端outcome和暂停决定提示，旧成功时间不派生异常', () => {
  const e = mount('views/Dashboard.vue')
  for (const outcome of ['idle', 'success', 'partial', 'failed']) for (const enabled of [true, false]) {
    e.s.status.value = status({ enabled, last_round: round(outcome) })
    assert.equal(hint(e)?.props.type, !enabled ? undefined : outcome === 'failed' ? 'error' : outcome === 'partial' ? 'warning' : undefined, `R21_OUTCOME ${outcome}/${enabled}`)
  }
  e.s.status.value = status()
  assert.equal(hint(e), undefined)
  e.s.status.value = status({ last_round: round('success', { cleanup_observation_summary: { complete: true, observed_candidates: 2, observed_deferred: 1 } }) })
  assert.equal(hint(e)?.props.type, 'warning')
  assert.match(e.inspect().text, /已观察残留 1 条/)
})

test('I8R21 同provider目标重排、增删后卡片key仍为target_id', () => {
  const e = mount('components/DryRunResults.vue')
  for (const ids of [[7, 9], [9, 7], [11, 9], [9]]) {
    const results = ids.map(target_id => ({ target_id, provider: 'tc_cvm(same)', domains: [], desired: [], to_add: [], cleanup_candidates: [], unsupported: [] }))
    const cards = e.inspect({ status: 'completed', results, warnings: [], error: '' }).nodes.filter(n => n.type?.name === 'NCard')
    assert.deepEqual(cards.map(n => n.key), ids, 'R21_TARGET_KEY')
  }
})

test('I8R21 模拟按钮large与明暗主题44px覆盖接线保持', () => {
  const app = mount('App.vue'), page = mount('views/RunTest.vue')
  assert.equal(button(page, page.s.runDryRun).props.size, 'large', 'R21_BUTTON_SIZE')
  for (const theme of ['light', 'dark']) {
    app.s.theme.value = theme
    const provider = app.inspect().nodes.find(n => n.type?.name === 'NConfigProvider')
    assert.equal(provider.props['theme-overrides'].Button.heightLarge, '44px', 'R21_BUTTON_THEME')
  }
})

for (const failure of [false, true]) {
  test(`I8R21 Alerts读取${failure ? '失败' : '成功'}晚到不提示、不覆盖表单`, async t => {
    const p = deferred(), e = mount('views/Alerts.vue', () => p.promise)
    const before = JSON.stringify(e.s.email.value), data = { policy: e.s.policy.value, email: { ...e.s.email.value, host: 'late.invalid' }, webhook: e.s.webhook.value, uptime_kuma_push: e.s.push.value }
    const task = e.s.load(); join(t, [p], [task]); e.unmount()
    failure ? p.reject(new Error('fixture failure')) : p.resolve(data)
    await task
    assert.equal(e.messages.length, 0, 'R21_ALERT_LOAD_LATE')
    assert.equal(JSON.stringify(e.s.email.value), before)
    await e.s.load(); await e.s.save(); await e.s.testSend()
    assert.equal(e.calls.length, 1, '卸载后不再准入页面请求')
  })
  for (const unmount of [false, true]) {
    test(`I8R21 Alerts保存${failure ? '失败' : '成功'}，${unmount ? '离页无消息' : '在页保留消息'}`, async t => {
      const p = deferred(), e = mount('views/Alerts.vue', () => p.promise)
      e.s.loadState.value = 'ready'
      const task = e.s.save(); join(t, [p], [task]); if (unmount) e.unmount()
      failure ? p.reject(new Error('fixture failure')) : p.resolve({})
      await task
      assert.equal(e.messages.length, unmount ? 0 : 1, 'R21_ALERT_SAVE_MESSAGE')
      if (!unmount) assert.equal(e.messages[0].kind, failure ? 'error' : 'success')
      assert.equal(e.s.saving.value, false)
      assert.equal(e.calls.length, 1)
    })
  }
  test(`I8R21 测试邮件${failure ? '失败' : '成功'}晚到不发布结果且保留35秒请求预算`, async t => {
    const p = deferred(), e = mount('views/Alerts.vue', () => p.promise)
    const task = e.s.testSend(); join(t, [p], [task]); e.unmount()
    assert.equal(e.calls[0][2], 35000)
    failure ? p.reject(new Error('fixture failure')) : p.resolve({ success: true })
    await task
    assert.equal(e.s.testResult.value, null, 'R21_EMAIL_LATE')
    assert.equal(e.s.testing.value, false)
    assert.equal(e.calls.length, 1)
  })
  for (const unmount of [false, true]) {
    test(`I8R21 DryRun${failure ? '失败' : '成功'}，${unmount ? '离页不提示' : '在页正常提示'}`, async t => {
      const p = deferred(), e = mount('views/RunTest.vue', () => p.promise)
      const task = e.s.runDryRun(); join(t, [p], [task]); if (unmount) e.unmount()
      failure ? p.reject(new Error('fixture failure')) : p.resolve({ results: [], warnings: [] })
      await task
      assert.equal(e.messages.length, unmount ? 0 : 1, 'R21_DRYRUN_MESSAGE')
      if (!unmount) assert.equal(e.messages[0].kind, failure ? 'error' : 'info')
      if (unmount) { await e.s.runDryRun(); assert.equal(e.calls.length, 1) }
    })
  }
  test(`I8R21 Settings初读${failure ? '失败' : '成功'}晚到不再启动预加载`, async t => {
    const p = deferred(), e = mount('views/Settings.vue', () => p.promise)
    const task = e.mounted(); join(t, [p], [task]); e.unmount()
    failure ? p.reject(new Error('fixture failure')) : p.resolve({})
    await task
    assert.equal(e.messages.length, 0, 'R21_SETTINGS_LATE')
    assert.equal(e.calls.length, 1, 'R21_SETTINGS_PRELOAD')
  })
  test(`I8R21 Dashboard触发${failure ? '失败' : '成功'}晚到不提示、不创建延迟查询`, async t => {
    const p = deferred(), e = mount('views/Dashboard.vue', () => p.promise)
    const task = e.s.triggerSync(); join(t, [p], [task]); e.unmount()
    failure ? p.reject(new Error('fixture failure')) : p.resolve({})
    await task; await e.runTimers()
    assert.equal(e.messages.length, 0, 'R21_TRIGGER_MESSAGE')
    assert.equal(e.timers.size, 0, 'R21_TRIGGER_TIMER')
    assert.equal(e.calls.length, 1, 'R21_TRIGGER_LATE_FETCH')
  })
  test(`I8R21 Dashboard切换${failure ? '失败' : '成功'}晚到不提示、不追加查询`, async t => {
    const p = deferred(), e = mount('views/Dashboard.vue', () => p.promise)
    const task = e.s.toggleSync(); join(t, [p], [task]); e.unmount()
    failure ? p.reject(new Error('fixture failure')) : p.resolve({})
    await task
    assert.equal(e.messages.length, 0, 'R21_TOGGLE_MESSAGE')
    assert.equal(e.calls.length, 1, 'R21_TOGGLE_FETCH')
    assert.equal(e.s.switching.value, false)
  })
}

test('I8R21 已创建的触发timer与周期timer在离页时清理，捕获的回调也不再查询', async t => {
  const e = mount('views/Dashboard.vue', async url => url.endsWith('/status') ? status() : [])
  await e.mounted(); await e.s.triggerSync(); t.after(() => e.unmount())
  assert.equal(e.intervals.size, 1); assert.equal(e.timers.size, 1)
  const interval = [...e.intervals.values()][0], timeout = [...e.timers.values()][0]
  assert.equal(interval.ms, 5000); assert.equal(timeout.ms, 1000)
  e.unmount(); const before = e.calls.length
  interval.fn(); timeout.fn(); await e.s.triggerSync(); await e.s.toggleSync()
  assert.equal(e.calls.length, before, 'R21_CAPTURED_TIMER')
  assert.equal(e.intervals.size, 0); assert.equal(e.timers.size, 0, 'R21_TIMER_CLEAR')
})

for (const newerFirst of [false, true]) {
  test(`I8R21 状态响应${newerFirst ? '新先完成' : '旧先完成'}只接受最新发起请求`, async t => {
    const a = deferred(), b = deferred(); let count = 0
    const e = mount('views/Dashboard.vue', () => ++count === 1 ? a.promise : b.promise)
    const old = e.s.fetchStatus(), latest = e.s.fetchStatus(); join(t, [a, b], [old, latest])
    if (newerFirst) { b.resolve(status({ enabled: false })); await latest; a.resolve(status()); await old }
    else { a.resolve(status()); await old; b.resolve(status({ enabled: false })); await latest }
    assert.match(e.inspect().text, /已暂停/, 'R21_STATUS_SEQUENCE')
  })
  test(`I8R21 统计响应${newerFirst ? '新先完成' : '旧先完成'}保留最新目标与规则数量`, async t => {
    const a = deferred(), b = deferred(); let count = 0
    const e = mount('views/Dashboard.vue', () => ++count <= 2 ? a.promise : b.promise)
    const old = e.s.fetchStats(), latest = e.s.fetchStats(); join(t, [a, b], [old, latest])
    if (newerFirst) { b.resolve([{}, {}]); await latest; a.resolve([{}]); await old }
    else { a.resolve([{}]); await old; b.resolve([{}, {}]); await latest }
    assert.match(e.inspect().text, /云资源目标 2 个/, 'R21_STATS_SEQUENCE')
    assert.match(e.inspect().text, /域名规则 2 条/)
  })
}

test('I8R21 最新查询失败保留已知状态，不接收旧在途成功响应', async t => {
  const a = deferred(), b = deferred(); let count = 0
  const e = mount('views/Dashboard.vue', () => ++count === 1 ? a.promise : b.promise)
  e.s.status.value = status({ enabled: false })
  const old = e.s.fetchStatus(), latest = e.s.fetchStatus(); join(t, [a, b], [old, latest])
  b.reject(new Error('fixture failure')); await latest; a.resolve(status()); await old
  assert.match(e.inspect().text, /已暂停/, 'R21_FAILED_LATEST')
})

test('I8R21 Settings在页初读仍预加载四类共享缓存', async () => {
  const e = mount('views/Settings.vue')
  await e.mounted()
  assert.equal(e.calls.filter(([name]) => name === 'scanned').length, 4)
})

for (const failure of [false, true]) {
  test(`I8R21 Dashboard在页触发${failure ? '失败' : '成功'}仍提示并延迟查询`, async () => {
    const e = mount('views/Dashboard.vue', async url => {
      if (url.endsWith('/trigger') && failure) throw new Error('fixture failure')
      return status()
    })
    await e.s.triggerSync()
    assert.equal(e.messages[0]?.kind, failure ? 'error' : 'success')
    assert.equal(e.calls[0][0], '/api/sync/trigger')
    assert.equal(e.calls[0][1].method, 'POST')
    assert.equal(e.timers.size, 1)
    await e.runTimers()
    assert.equal(e.calls[1][0], '/api/sync/status')
    e.unmount()
  })
  test(`I8R21 Dashboard在页切换${failure ? '失败' : '成功'}仍提示并立即查询`, async () => {
    const e = mount('views/Dashboard.vue', async url => {
      if (url.endsWith('/pause') && failure) throw new Error('fixture failure')
      return status({ enabled: failure })
    })
    await e.s.toggleSync()
    assert.equal(e.messages[0]?.kind, failure ? 'error' : 'success')
    assert.equal(e.calls[0][0], '/api/sync/pause')
    assert.equal(e.calls[0][1].method, 'POST')
    assert.equal(e.calls[1][0], '/api/sync/status')
    e.unmount()
  })
}
