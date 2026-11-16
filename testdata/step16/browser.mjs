// Build8 Step 16 实际浏览器验收（Chrome DevTools Protocol）。
//
// 用法：node browser.mjs <ui|edge> <appBaseURL> <controlBaseURL>
// 该脚本不进入 npm test / Make / CI；由 `go test -tags step16` 启动。
import { spawn } from 'node:child_process'
import { mkdtempSync, writeFileSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'

const scenario = process.argv[2]
const app = process.argv[3]
const control = process.argv[4]
const chromeBin = process.env.CHROME_BIN || '/Applications/Google Chrome.app/Contents/MacOS/Google Chrome'

function log(obj) {
  process.stdout.write('STEP16_BROWSER ' + JSON.stringify(obj) + '\n')
}
function assert(cond, msg) {
  if (!cond) throw new Error(msg)
}
function sleep(ms) { return new Promise(r => setTimeout(r, ms)) }

async function launchChrome() {
  const profile = mkdtempSync(join(tmpdir(), 'fwalizer-step16-chrome-'))
  const args = [
    '--headless=new', '--disable-gpu', '--no-first-run', '--no-default-browser-check',
    '--disable-dev-shm-usage', '--remote-debugging-port=0', '--user-data-dir=' + profile,
    'about:blank',
  ]
  const proc = spawn(chromeBin, args, { stdio: ['ignore', 'ignore', 'pipe'] })
  const wsURL = await new Promise((resolve, reject) => {
    let buf = ''
    const timer = setTimeout(() => reject(new Error('等待 Chrome DevTools 超时: ' + buf)), 15000)
    proc.stderr.on('data', chunk => {
      buf += chunk.toString()
      const m = /DevTools listening on (ws:\/\/\S+)/.exec(buf)
      if (m) { clearTimeout(timer); resolve(m[1]) }
    })
    proc.on('exit', code => { clearTimeout(timer); reject(new Error('Chrome 提前退出: ' + code + ' ' + buf)) })
  })
  return { proc, wsURL, profile }
}

class CDP {
  constructor(wsURL) {
    this.ws = new WebSocket(wsURL)
    this.id = 0
    this.pending = new Map()
    this.events = []
    this.sessionId = null
    this.ready = new Promise((resolve, reject) => {
      this.ws.onopen = resolve
      this.ws.onerror = reject
    })
    this.ws.onmessage = ev => {
      const msg = JSON.parse(ev.data)
      if (msg.id) {
        const p = this.pending.get(msg.id)
        if (p) {
          this.pending.delete(msg.id)
          if (msg.error) p.reject(new Error(JSON.stringify(msg.error)))
          else p.resolve(msg.result)
        }
        return
      }
      this.events.push(msg)
    }
  }
  async send(method, params = {}, sessionId = this.sessionId) {
    await this.ready
    const id = ++this.id
    const payload = { id, method, params }
    if (sessionId) payload.sessionId = sessionId
    return await new Promise((resolve, reject) => {
      const timer = setTimeout(() => { this.pending.delete(id); reject(new Error('CDP 超时: ' + method)) }, 20000)
      this.pending.set(id, {
        resolve: v => { clearTimeout(timer); resolve(v) },
        reject: e => { clearTimeout(timer); reject(e) },
      })
      this.ws.send(JSON.stringify(payload))
    })
  }
  close() { try { this.ws.close() } catch {} }
}

async function evalJS(cdp, expression) {
  const res = await cdp.send('Runtime.evaluate', {
    expression, returnByValue: true, awaitPromise: true, userGesture: true,
  })
  if (res.exceptionDetails) {
    throw new Error('页面脚本异常: ' + (res.exceptionDetails.text || JSON.stringify(res.exceptionDetails)))
  }
  return res.result ? res.result.value : undefined
}

async function waitFor(cdp, expression, timeout = 20000, label = expression) {
  const deadline = Date.now() + timeout
  let last
  while (Date.now() < deadline) {
    try {
      last = await evalJS(cdp, expression)
      if (last) return last
    } catch (e) { last = e.message }
    await sleep(150)
  }
  throw new Error('等待超时: ' + label + '；last=' + last)
}

async function bodyText(cdp) { return await evalJS(cdp, 'document.body ? document.body.innerText : ""') }

async function waitText(cdp, text, timeout = 20000) {
  await waitFor(cdp, `document.body && document.body.innerText.includes(${JSON.stringify(text)})`, timeout, 'text=' + text)
}

async function navigate(cdp, url) {
  await cdp.send('Page.navigate', { url })
  await sleep(300)
  await waitFor(cdp, 'document.readyState === "complete"', 15000, 'document ready')
  await sleep(200)
}

async function reload(cdp) {
  await cdp.send('Page.reload', { ignoreCache: true })
  await sleep(500)
  await waitFor(cdp, 'document.readyState === "complete"', 15000, 'document ready')
}

async function clickText(cdp, selector, text) {
  const ok = await evalJS(cdp, `(() => {
    const nodes = [...document.querySelectorAll(${JSON.stringify(selector)})]
    const el = nodes.find(e => (e.innerText || e.textContent || '').trim().includes(${JSON.stringify(text)}))
    if (!el) return false
    el.click()
    return true
  })()`)
  assert(ok, `找不到可点击元素 ${selector}: ${text}`)
  await sleep(200)
}

async function selectedMenu(cdp) {
  return await evalJS(cdp, `(() => {
    const e = document.querySelector('.n-menu-item-content--selected') ||
      [...document.querySelectorAll('.n-menu-item-content')].find(x => /selected/.test(x.className))
    return e ? e.innerText.trim() : ''
  })()`)
}

async function inputByLabel(cdp, label) {
  return await evalJS(cdp, `(() => {
    const item = [...document.querySelectorAll('.n-form-item')].find(e => (e.innerText || '').includes(${JSON.stringify(label)}))
    if (!item) return null
    const input = item.querySelector('input, textarea')
    return input ? input.value : null
  })()`)
}

async function screenshot(cdp, name) {
  const res = await cdp.send('Page.captureScreenshot', { format: 'png', captureBeyondViewport: true })
  const path = '/tmp/fwalizer-step16-' + name + '.png'
  writeFileSync(path, Buffer.from(res.data, 'base64'))
  return path
}

async function postJSON(url, body) {
  const res = await fetch(url, { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body || {}) })
  const text = await res.text()
  let data
  try { data = JSON.parse(text) } catch { data = text }
  return { status: res.status, data }
}
async function putAppSettings(values) {
  const res = await fetch(app + '/api/settings', {
    method: 'PUT', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(values),
  })
  const text = await res.text()
  assert(res.status === 200, '保存设置失败: ' + res.status + ' ' + text)
}

async function getJSON(url) {
  const res = await fetch(url)
  const text = await res.text()
  let data
  try { data = JSON.parse(text) } catch { data = text }
  return { status: res.status, data }
}

async function waitStatus(cdp, pred, timeout = 30000) {
  const deadline = Date.now() + timeout
  let last
  while (Date.now() < deadline) {
    const r = await getJSON(control + '/status')
    last = r.data
    if (r.status === 200 && pred(last)) return last
    await sleep(300)
  }
  throw new Error('等待同步状态超时: ' + JSON.stringify(last))
}

async function waitEdgeStatus(cdp, pred, timeout = 30000) {
  const deadline = Date.now() + timeout
  let last
  while (Date.now() < deadline) {
    const r = await getJSON(control + '/edge-status')
    last = r.data
    if (r.status === 200 && pred(last)) return last
    await sleep(300)
  }
  throw new Error('等待站点状态超时: ' + JSON.stringify(last))
}

async function checkHealth(want) {
  const r = await getJSON(control + '/health')
  assert(r.status === want, `health=${r.status}, want ${want}; ${JSON.stringify(r.data)}`)
  return r.data
}

async function runUI(cdp) {
  await navigate(cdp, app + '/#/edgeone')
  await waitText(cdp, 'EdgeOne 回源')
  let text = await bodyText(cdp)
  assert(text.includes('lhins-step16-a') && text.includes('lhins-step16-b'), 'EdgeOne 页面未显示两个物理目标')
  assert(text.includes('已启用'), 'EdgeOne 模块开关未回显为已启用')
  assert((await selectedMenu(cdp)).includes('EdgeOne 回源'), '侧栏未高亮 EdgeOne 回源')
  log({ step: 'deep-link', selected: await selectedMenu(cdp), screenshot: await screenshot(cdp, 'ui-edgeone') })

  await clickText(cdp, '.n-menu-item-content', '全局设置')
  await waitText(cdp, '全局设置')
  assert((await selectedMenu(cdp)).includes('全局设置'), '侧栏未跟随设置页')
  const values = {
    tag: await inputByLabel(cdp, 'DNS TAG'),
    edgeTag: await inputByLabel(cdp, 'EdgeOne TAG'),
    edgeInterval: await inputByLabel(cdp, 'EdgeOne 间隔'),
    dns: await inputByLabel(cdp, 'DNS 服务器'),
    interval: await inputByLabel(cdp, 'DNS 间隔'),
  }
  assert(values.tag === 'auto-ddns', 'DNS TAG 回显 = ' + values.tag)
  assert(values.edgeTag === 'edgeone', 'EdgeOne TAG 回显 = ' + values.edgeTag)
  assert(values.edgeInterval === '72h', 'EdgeOne 间隔回显 = ' + values.edgeInterval)
  assert(values.interval === '5m', 'DNS 间隔回显 = ' + values.interval)
  assert(values.dns && values.dns.includes('127.0.0.1:'), 'DNS 服务器回显 = ' + values.dns)
  const dnsSwitchOn = await evalJS(cdp, `(() => {
    const item = [...document.querySelectorAll('.n-form-item')].find(e => (e.innerText || '').includes('DNS 同步'))
    const sw = item && item.querySelector('.n-switch')
    if (!sw) return null
    return sw.classList.contains('n-switch--active') || sw.getAttribute('aria-checked') === 'true'
  })()`)
  assert(dnsSwitchOn === true, 'DNS 同步开关回显不为开启: ' + dnsSwitchOn)
  log({ step: 'settings-echo', values, dnsSwitchOn, screenshot: await screenshot(cdp, 'ui-settings') })

  await evalJS(cdp, 'history.back()')
  await waitText(cdp, 'EdgeOne 回源')
  assert((await selectedMenu(cdp)).includes('EdgeOne 回源'), '后退后侧栏未高亮')
  await evalJS(cdp, 'history.forward()')
  await waitText(cdp, '全局设置')
  assert((await selectedMenu(cdp)).includes('全局设置'), '前进后侧栏未高亮')
  log({ step: 'history-back-forward' })

  await navigate(cdp, app + '/#/edgeone')
  const imported = await postJSON(control + '/import-v4', {})
  assert(imported.status === 200, '导入 v4 = ' + imported.status + ' ' + JSON.stringify(imported.data))
  await reload(cdp)
  await navigate(cdp, app + '/#/edgeone')
  await waitText(cdp, 'EdgeOne 回源')
  await waitText(cdp, '8443')
  text = await bodyText(cdp)
  assert(text.includes('lhins-step16-a') && text.includes('lhins-step16-b'), '导入刷新后多目标绑定丢失')
  log({ step: 'import-refresh', sitePort: '8443', screenshot: await screenshot(cdp, 'ui-import-refresh') })

  await navigate(cdp, app + '/#/')
  await waitText(cdp, '仪表盘')
  await waitText(cdp, '已暂停')
  const disabledBefore = await evalJS(cdp, `[...document.querySelectorAll('button')].some(b => b.textContent.includes('立即同步') && b.disabled)`)
  assert(disabledBefore, '暂停时立即同步未禁用')
  await clickText(cdp, 'button', '开启同步')
  await waitStatus(null, s => s.enabled === true, 15000)
  await clickText(cdp, 'button', '暂停同步')
  await waitStatus(null, s => s.enabled === false, 15000)
  await waitText(cdp, '已暂停')
  log({ step: 'pause-resume' })

  await navigate(cdp, app + '/#/dry-run')
  await waitText(cdp, '模拟测试')
  await clickText(cdp, 'button', '执行模拟测试')
  await waitText(cdp, '已配置目标', 30000)
  await waitFor(cdp, `!!document.querySelector('[data-testid="dryrun-preflight"]')`, 30000, 'dryrun preflight')
  const preflightText = await evalJS(cdp, `document.querySelector('[data-testid="dryrun-preflight"]').innerText`)
  assert(preflightText.includes('全局预检完整'), 'Dry Run 预检文案 = ' + preflightText)
  const dryStats = await bodyText(cdp)
  assert(dryStats.includes('待新增'), 'Dry Run 缺少待新增统计')
  log({ step: 'pause-dryrun', preflight: preflightText, screenshot: await screenshot(cdp, 'ui-dryrun') })

  await navigate(cdp, app + '/#/logs')
  await waitText(cdp, '同步日志')
  await waitText(cdp, '历史记录')
  log({ step: 'logs-page' })
}

async function runEdge(cdp) {
  await navigate(cdp, app + '/#/')
  await waitText(cdp, '仪表盘')
  // 冲突：当前 EdgeOne 网段覆盖 DNS 地址。
  let r = await postJSON(control + '/state/teo', {
    zone: 'zone-states', current_version: 'v1', current_v4: ['198.18.0.10/32'], next_version: '', next_v4: [],
  })
  assert(r.status === 200, '设置冲突 TEO 状态失败')
  await clickText(cdp, 'button', '开启同步')
  await waitStatus(null, s => s.address_preflight && s.address_preflight.state === 'overlap', 30000)
  await waitText(cdp, '地址重叠', 30000)
  await checkHealth(503)
  log({ step: 'conflict', hint: await evalJS(cdp, `document.querySelector('[data-testid="preflight-hint"]').innerText`), screenshot: await screenshot(cdp, 'edge-conflict') })

  // 恢复：完整且无重叠。
  r = await postJSON(control + '/state/teo', {
    zone: 'zone-states', current_version: 'v1', current_v4: ['203.0.113.0/24'], next_version: '', next_v4: [],
  })
  assert(r.status === 200, '设置恢复 TEO 状态失败')
  await clickText(cdp, 'button', '立即同步')
  await waitStatus(null, s => s.address_preflight && s.address_preflight.state === 'clear', 30000)
  await waitText(cdp, '全局预检完整', 30000)
  await checkHealth(200)
  log({ step: 'recovery', screenshot: await screenshot(cdp, 'edge-recovery') })

  // 单侧继续：先让 EdgeOne 缓存过期，再注入查询不完整；完整 DNS 应继续。
  await putAppSettings({ edgeone_interval: '1s' })
  const paused = await postJSON(control + '/sync/pause', {})
  assert(paused.status === 200 && paused.data.http_status === 200, '暂停失败: ' + JSON.stringify(paused))
  await waitStatus(null, s => s.enabled === false, 15000)
  await sleep(1500)
  r = await postJSON(control + '/state/teo', {
    zone: 'zone-states', current_version: 'v1', current_v4: ['203.0.113.0/24'], next_version: '', next_v4: [], describe_mode: 'incomplete',
  })
  assert(r.status === 200, '设置单侧失败 TEO 状态失败')
  const singleResume = await postJSON(control + '/sync/resume', {})
  assert(singleResume.status === 200 && singleResume.data.http_status === 200, '单侧恢复触发失败: ' + JSON.stringify(singleResume))
  await waitStatus(null, s => s.address_preflight && s.address_preflight.state === 'data_unavailable' && s.address_preflight.dns_complete && !s.address_preflight.edgeone_complete, 30000)
  await waitStatus(null, s => s.edgeone && s.edgeone.last_outcome === 'incomplete', 30000)
  await waitText(cdp, '单侧继续', 30000)
  await checkHealth(503)
  log({ step: 'single-side', hint: await evalJS(cdp, `document.querySelector('[data-testid="preflight-hint"]').innerText`), screenshot: await screenshot(cdp, 'edge-single-side') })

  // 恢复确认状态机：先 unknown，再 accepted。
  await putAppSettings({ edgeone_interval: '72h' })
  r = await postJSON(control + '/state/teo', {
    zone: 'zone-states', current_version: 'v1', current_v4: ['203.0.113.0/24'],
    next_version: 'v2', next_v4: ['198.51.100.0/24'], confirm_mode: 'block',
  })
  assert(r.status === 200, '设置确认阻塞状态失败')
  const confirmTrigger = await postJSON(control + '/sync/trigger', {})
  assert(confirmTrigger.status === 200 && confirmTrigger.data.http_status === 202, '确认触发失败: ' + JSON.stringify(confirmTrigger))
  const started = await getJSON(control + '/confirm-started?zone=zone-states')
  assert(started.status === 200 && started.data.started, '确认请求未进入替身')
  await navigate(cdp, app + '/#/edgeone')
  await waitText(cdp, '结果未知', 20000)
  log({ step: 'confirmation-unknown', screenshot: await screenshot(cdp, 'edge-unknown') })
  await postJSON(control + '/release-confirm?zone=zone-states', {})
  await waitEdgeStatus(null, s => s.sites && s.sites[0] && s.sites[0].confirmation_state === 'accepted', 40000)
  await navigate(cdp, app + '/#/edgeone')
  await waitText(cdp, '已确认，等待生效', 20000)
  log({ step: 'confirmation-accepted', screenshot: await screenshot(cdp, 'edge-accepted') })

  // B 生效：当前版本与网段均匹配，站点显示已生效。
  r = await postJSON(control + '/state/teo', {
    zone: 'zone-states', current_version: 'v2', current_v4: ['198.51.100.0/24'], next_version: '', next_v4: [],
  })
  assert(r.status === 200, '设置生效状态失败')
  await navigate(cdp, app + '/#/')
  const effectiveTrigger = await postJSON(control + '/sync/trigger', {})
  assert(effectiveTrigger.status === 200 && effectiveTrigger.data.http_status === 202, '生效触发失败: ' + JSON.stringify(effectiveTrigger))
  await waitEdgeStatus(null, s => s.sites && s.sites[0] && s.sites[0].actual_effective === true, 40000)
  await navigate(cdp, app + '/#/edgeone')
  await waitText(cdp, '已生效', 20000)
  log({ step: 'effective', screenshot: await screenshot(cdp, 'edge-effective') })
}

async function main() {
  assert(['ui', 'edge'].includes(scenario), '未知场景: ' + scenario)
  const { proc, wsURL } = await launchChrome()
  const cdp = new CDP(wsURL)
  try {
    await cdp.ready
    const t = await cdp.send('Target.createTarget', { url: 'about:blank' })
    const attached = await cdp.send('Target.attachToTarget', { targetId: t.targetId, flatten: true })
    cdp.sessionId = attached.sessionId
    await cdp.send('Page.enable')
    await cdp.send('Runtime.enable')
    await cdp.send('Emulation.setDeviceMetricsOverride', { width: 1280, height: 900, deviceScaleFactor: 1, mobile: false })
    if (scenario === 'ui') await runUI(cdp)
    else await runEdge(cdp)
    log({ scenario, result: 'pass' })
  } finally {
    cdp.close()
    proc.kill('SIGKILL')
  }
}

main().catch(err => {
  process.stderr.write('STEP16_BROWSER FAIL ' + (err && err.stack ? err.stack : String(err)) + '\n')
  process.exit(1)
})
