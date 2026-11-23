// Real Chrome, real HTTP and real product DOM; network abort happens after receipt.
import { launchChrome, CDP, evalJS, waitFor, bodyText, waitText, navigate, reload, clickText, screenshot, sleep } from '../step16/browser.mjs'
import { rmSync } from 'node:fs'
const app = process.argv[2]
const assert = (v, message) => { if (!v) throw new Error(message) }
const log = value => process.stdout.write('B9S7_BROWSER ' + JSON.stringify(value) + '\n')
async function api(path, body) {
  const r = await fetch(app + path, body === undefined ? {} : { method: 'POST', headers: { 'Content-Type': 'application/json' }, body: JSON.stringify(body) })
  assert(r.ok, path + ' status=' + r.status)
  return r.json()
}
async function setAllowance(cdp, value) {
  await evalJS(cdp, `(() => {
    const item = [...document.querySelectorAll('.n-form-item')].find(e => e.innerText.includes('额外放行 IPv4 上限'))
    const input = item.querySelector('input')
    input.focus(); input.select()
  })()`)
  await cdp.send('Input.insertText', { text: String(value) })
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyDown', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 })
  await cdp.send('Input.dispatchKeyEvent', { type: 'keyUp', key: 'Tab', code: 'Tab', windowsVirtualKeyCode: 9 })
  await sleep(200)
}
async function toggleCompression(cdp) {
  const ok = await evalJS(cdp, `(() => {
    const card=[...document.querySelectorAll('.n-card')].find(e=>e.innerText.includes('实例设置单独保存'))
    const toggle=card?.querySelector('[role="switch"]'); if(!toggle || toggle.getAttribute('aria-disabled')==='true') return false
    toggle.click(); return true
  })()`)
  assert(ok, 'compression switch unavailable')
}
async function exactButton(cdp, label, twice = false) {
  const ok = await evalJS(cdp, `(() => {const b=[...document.querySelectorAll('button')].find(e=>e.innerText.trim()===${JSON.stringify(label)});if(!b||b.disabled)return false;b.click();${twice ? 'b.click();' : ''}return true})()`)
  assert(ok, 'button unavailable: ' + label)
  await sleep(150)
}
async function checkbox(cdp, label) {
  const ok=await evalJS(cdp,`(() => {const e=[...document.querySelectorAll('.n-checkbox')].find(e=>e.innerText.includes(${JSON.stringify(label)}));if(!e)return false;e.click();return true})()`)
  assert(ok,'checkbox unavailable: '+label)
}
async function run(cdp) {
  await cdp.send('Network.enable')
  await navigate(cdp, app + '/#/edgeone')
  await waitText(cdp,'策略压缩已关闭')
  await waitText(cdp,'保存绑定不会开启压缩')
  await clickText(cdp,'button','前往实例设置')
  await waitText(cdp,'保存实例压缩设置')
  await waitFor(cdp,`[...document.querySelectorAll('button')].some(e=>e.innerText.trim()==='保存实例压缩设置'&&!e.disabled)`)
  await setAllowance(cdp,2)
  await toggleCompression(cdp)
  await exactButton(cdp,'保存实例压缩设置')
  await waitText(cdp,'确认启用实例策略压缩')
  await exactButton(cdp,'取消')
  assert(!(await api('/api/targets/1/lighthouse-compression')).enabled,'cancel saved settings')
  await exactButton(cdp,'保存实例压缩设置')
  await exactButton(cdp,'确认保存实例设置',true)
  await waitText(cdp,'实例压缩设置已保存')
  const settings=await api('/api/targets/1/lighthouse-compression')
  assert(settings.enabled&&settings.extra_ipv4_allow===2,'settings not saved')
  const settingsPuts=cdp.events.filter(e=>e.method==='Network.requestWillBeSent'&&e.params.request.method==='PUT'&&e.params.request.url===app+'/api/targets/1/lighthouse-compression').length
  assert(settingsPuts===1,'duplicate settings save: '+settingsPuts)
  await sleep(350)
  log({step:'instance-save',screenshot:await screenshot(cdp,'b9s7-instance')})
  await navigate(cdp,app+'/#/dry-run')
  await clickText(cdp,'button','执行模拟测试')
  await waitText(cdp,'有效规划')
  await waitText(cdp,'共存峰值 96')
  await waitText(cdp,'峰值额外 IPv4 2')
  log({step:'lossy-preview',screenshot:await screenshot(cdp,'b9s7-preview')})
  await api('/api/sync/resume',{})
  const deadline=Date.now()+20000
  while(Date.now()<deadline){const s=await api('/api/sync/status');if(s.last_round?.added===1&&!s.round_started_at)break;await sleep(100)}
  assert((await api('/api/sync/status')).last_round?.added===1,'formal compression failed')
  await api('/api/sync/pause',{})
  await navigate(cdp,app+'/#/edgeone')
  await waitText(cdp,'策略压缩已开启')
  await checkbox(cdp,'全部已配置目标')
  await exactButton(cdp,'只读预览清理范围')
  await waitText(cdp,'确认删除 EdgeOne TAG 规则')
  await waitText(cdp,'可删 1 行')
  await exactButton(cdp,'取消')
  await exactButton(cdp,'只读预览清理范围')
  await waitText(cdp,'确认删除 EdgeOne TAG 规则')
  await checkbox(cdp,'清理前关闭 EdgeOne 回源同步')

  // Fail only the accepted response. Server already processed POST; browser sees a network error.
  let aborted=false
  const nativeOnMessage=cdp.ws.onmessage
  cdp.ws.onmessage=event=>{
    nativeOnMessage(event)
    const message=JSON.parse(event.data)
    if(message.method==='Fetch.requestPaused'){
      const p=message.params
      if(!aborted&&p.request.method==='POST'&&p.request.url===app+'/api/edgeone/firewall-cleanup'){
        aborted=true
        cdp.send('Fetch.failRequest',{requestId:p.requestId,errorReason:'ConnectionReset'}).catch(e=>{throw e})
      }else cdp.send('Fetch.continueRequest',{requestId:p.requestId}).catch(e=>{throw e})
    }
  }
  await cdp.send('Fetch.enable',{patterns:[{urlPattern:app+'/api/edgeone/firewall-cleanup',requestStage:'Response'}]})
  await exactButton(cdp,'确认删除预览规则',true)
  await waitText(cdp,'受理结果尚未确认')
  assert(aborted,'response was not aborted')
  const beforeRetry=cdp.events.filter(e=>e.method==='Network.requestWillBeSent'&&e.params.request.method==='POST'&&e.params.request.url===app+'/api/edgeone/firewall-cleanup').length
  assert(beforeRetry===1,'double click or network failure automatically resubmitted: '+beforeRetry)
  await cdp.send('Fetch.disable')
  await reload(cdp)
  await waitText(cdp,'上次受理结果尚未确认')
  await exactButton(cdp,'核对受理结果')
  await waitText(cdp,'操作 ')
  // Reload with an operation id: only GET may resume progress polling.
  const saved=await evalJS(cdp,`JSON.parse(sessionStorage.getItem('fwalizer-edgeone-cleanup'))`)
  assert(saved.operation_id&&!saved.pending,'operation recovery did not replace pending id')
  const postCount=()=>cdp.events.filter(e=>e.method==='Network.requestWillBeSent'&&e.params.request.method==='POST'&&e.params.request.url===app+'/api/edgeone/firewall-cleanup').length
  assert(postCount()===2,'explicit idempotent retry missing/duplicated')
  await reload(cdp)
  await waitText(cdp,'已清空：所有所选目标均可信回读且当前 TAG 零残留',30000)
  assert(postCount()===2,'refresh submitted a new confirmation')
  const operation=await api('/api/edgeone/firewall-cleanup/'+saved.operation_id)
  assert(operation.cleared&&operation.targets[0].confirmed_deleted===1,'cleanup result wrong')
  await evalJS(cdp, `[...document.querySelectorAll('.n-alert')].find(e => e.innerText.includes('已清空：所有所选目标'))?.scrollIntoView({ block: 'center' })`)
  await sleep(300)
  log({step:'accepted-response-aborted-and-recovered',postCount:postCount(),operation,screenshot:await screenshot(cdp,'b9s7-cleanup')})
  const errors=cdp.events.filter(e=>e.method==='Runtime.exceptionThrown')
  assert(errors.length===0,'page exception: '+JSON.stringify(errors))
  log({result:'pass'})
}
const chrome=await launchChrome()
const cdp=new CDP(chrome.wsURL)
try{
  await cdp.ready
  const target=await cdp.send('Target.createTarget',{url:'about:blank'})
  cdp.sessionId=(await cdp.send('Target.attachToTarget',{targetId:target.targetId,flatten:true})).sessionId
  await cdp.send('Page.enable');await cdp.send('Runtime.enable')
  await cdp.send('Emulation.setDeviceMetricsOverride',{width:1440,height:1000,deviceScaleFactor:1,mobile:false})
  await run(cdp)
}catch(e){
  log({result:'fail',body:await bodyText(cdp).catch(()=>''),screenshot:await screenshot(cdp,'b9s7-fail').catch(()=>''),error:e.stack})
  process.exitCode=1
}finally{
  cdp.close();chrome.proc.kill('SIGKILL')
  await new Promise(resolve=>chrome.proc.once('exit',resolve))
  rmSync(chrome.profile,{recursive:true,force:true})
}
