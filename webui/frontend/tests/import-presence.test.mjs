// 执行真实请求封装、Settings 组件与模板；不替代浏览器人工验收。
import { readFileSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { createRequire } from 'node:module'
import { Script } from 'node:vm'
import test from 'node:test'
import assert from 'node:assert/strict'
const root=process.env.FWALIZER_I843_SOURCE_ROOT || fileURLToPath(new URL('../',import.meta.url))
const require=createRequire(`${root}/package.json`)
const vue=require('vue'),ts=require('typescript')
const {parse,compileScript,compileTemplate}=require('@vue/compiler-sfc')
function deferred(){let resolve,reject;const promise=new Promise((r,j)=>{resolve=r;reject=j});return{promise,resolve,reject}}
const detail=(fields=['alerts.policy','alerts.email'],total=fields.length)=>({error:'fixture missing',missing_fields:fields,missing_fields_total:total,missing_fields_truncated:total>fields.length})
const response=(data,status=400)=>({ok:status>=200&&status<300,status,json:async()=>data})
function mount(){
 const calls=[],messages=[],timers=new Map(),delays=[];let reply=response({}),unmount=()=>{},reloads=0,timerID=0
 const components=Object.fromEntries(['NForm','NFormItem','NInput','NSelect','NButton','NSpace','NCard','NGrid','NGi','NModal'].map(name=>[name,{name}]))
 function evaluate(source,dependencies){
  const mod={exports:{}}
  const result=ts.transpileModule(source,{reportDiagnostics:true,compilerOptions:{module:ts.ModuleKind.CommonJS,target:ts.ScriptTarget.ES2022}})
  assert.equal((result.diagnostics||[]).filter(x=>x.category===ts.DiagnosticCategory.Error).length,0)
  new Script(result.outputText).runInNewContext({module:mod,exports:mod.exports,require:dependencies,Error,AbortController,
   fetch:(url,opts)=>{calls.push({url,opts});return reply},window:{location:{reload(){reloads++}}},
   setTimeout:(fn,delay)=>{delays.push(delay);const id=++timerID;timers.set(id,fn);return id},clearTimeout:id=>timers.delete(id)})
  return mod.exports
 }
 const api=evaluate(readFileSync(`${root}/src/api.ts`,'utf8'),name=>{throw new Error(name)})
 const source=readFileSync(`${root}/src/views/Settings.vue`,'utf8'),{descriptor}=parse(source)
 const script=compileScript(descriptor,{id:'i843-probe'})
 const template=compileTemplate({source:descriptor.template.content,filename:'Settings.vue',id:'i843-probe',compilerOptions:{bindingMetadata:script.bindings}})
 assert.deepEqual(template.errors,[])
 function deps(name){
  if(name==='vue')return{...vue,onMounted(){},onUnmounted(fn){unmount=fn}}
  if(name==='naive-ui')return{...components,useThemeVars:()=>({}),useMessage:()=>Object.fromEntries(['success','warning','error'].map(kind=>[kind,text=>messages.push({kind,text})]))}
  if(name==='../api')return api
  if(name.endsWith('useZones'))return{useZones:()=>({load:async()=>{},regionOptions:()=>[]})}
  if(name.endsWith('useScannedResources'))return{useScannedResources:()=>({load:async()=>{},scan:async()=>null,clear:async()=>{},resourcesOf:()=>[]})}
  throw new Error(name)
 }
 const s=evaluate(script.content,deps).default.setup({},{expose(){}}),render=evaluate(template.code,deps).render
 function inspect(){const text=[],lis=[],modals=[];function visit(node){
  if(typeof node==='string'){text.push(node);return}if(Array.isArray(node)){node.forEach(visit);return}if(!node)return
  if(node.type?.name==='NModal'){modals.push(node.props);if(!node.props.show)return}
  if(node.type==='li')lis.push(node.children)
  if(node.children&&typeof node.children==='object'&&!Array.isArray(node.children)){for(const[key,slot]of Object.entries(node.children))if(key!=='_'&&typeof slot==='function')visit(slot())}else visit(node.children)
 }visit(render({},[],{},vue.proxyRefs(s),{},{}));return{text:text.join('\n'),lis,modals}}
 return{api,s,calls,messages,timers,delays,inspect,unmount:()=>unmount(),reply:r=>{reply=r},reloads:()=>reloads,runTimers:()=>{for(const fn of timers.values())fn();timers.clear()}}
}
const fileEvent=file=>({target:{files:[file],value:'fixture'}})
async function select(h,content='{"version":3}'){const file=new File([content],'fixture.json',{type:'application/json'});await h.s.importConfig(fileEvent(file));return file}
test('真实请求封装携带结构化缺失详情，其他错误保留原形态',async()=>{
 const h=mount();h.reply(response(detail()));await assert.rejects(h.api.request('/fixture'),err=>{assert.equal(err.status,400);assert.deepEqual(Array.from(err.missingFields.fields),['alerts.policy','alerts.email']);return true})
 for(const [status,data]of [[500,detail()],[400,{...detail(),missing_fields_total:1}],[400,{...detail(),missing_fields_truncated:true}],[400,{...detail(),missing_fields:[null]}],[400,{error:'plain'}],[400,detail([],0)],[400,detail(Array(101).fill('alerts'),101)],[400,{...detail(),missing_fields_total:2.5}],[400,{...detail(),missing_fields_total:Number.MAX_SAFE_INTEGER+1}],[400,{...detail(),missing_fields_truncated:'false'}],[400,detail([''],1)],[400,detail(['alerts'],101)]]){
  h.reply(response(data,status));await assert.rejects(h.api.request('/fixture'),err=>{assert.equal(err.missingFields,undefined);assert.equal(err.message,data.error);return true})
 }
})
test('真实组件与请求封装保留原 File，重复字段及非法 UTF8 不被重写',async()=>{
 for(const content of ['{"version":3,"version":3}',new Uint8Array([123,34,120,34,58,34,255,34,125])]){
  const h=mount(),file=await select(h,content);h.reply(response({error:'strict reject'}));await h.s.confirmImport()
  assert.equal(h.calls.length,1);assert.equal(h.calls[0].opts.body,file);assert.equal(h.calls[0].opts.headers['Content-Type'],'application/json')
  assert.deepEqual(new Uint8Array(await h.calls[0].opts.body.arrayBuffer()),new Uint8Array(await file.arrayBuffer()))
  assert.equal(h.s.pendingImport.value,null);assert.equal(h.reloads(),0);assert.equal(h.s.showImportFailure.value,false);assert.equal(h.messages[0].text,'导入失败: strict reject')
 }
})
test('持久弹窗渲染字段列表，关闭保留详情，新选择清空旧结果',async()=>{
 const h=mount();await select(h);h.reply(response(detail()));await h.s.confirmImport()
 assert.equal(h.s.showImportFailure.value,true);assert.equal(h.s.importing.value,false);assert.equal(h.s.pendingImport.value,null)
 const view=h.inspect();assert.deepEqual(view.lis,['alerts.policy','alerts.email']);assert.ok(view.text.includes('2 项'));assert.equal(h.messages.length,0)
 h.s.showImportFailure.value=false;assert.equal(h.s.importFailure.value.total,2)
 await select(h,'{invalid');assert.equal(h.s.importFailure.value,null);assert.equal(h.s.showImportFailure.value,false);assert.equal(h.s.showImportConfirm.value,false)
})
test('100项截断信息与总数进入真实模板',async()=>{
 const h=mount();await select(h);const fields=Array.from({length:100},(_,i)=>`targets[${i}].export_id`);h.reply(response(detail(fields,4000)));await h.s.confirmImport()
 const view=h.inspect();assert.equal(view.lis.length,100);assert.ok(view.text.includes('前 100 项'));assert.ok(view.text.includes('4000'))
})
test('在途导入阻止重复提交和文件替换；明确失败不刷新，之后可重选',async(t)=>{
 const h=mount();await select(h);const pending=deferred();h.reply(pending.promise);const first=h.s.confirmImport();t.after(async()=>{pending.resolve(response(detail()));await first});const duplicate=h.s.confirmImport();await select(h,'{"version":4}')
 assert.equal(h.calls.length,1);assert.equal(h.s.importing.value,true);assert.equal(h.s.pendingImport.value,null)
 pending.resolve(response(detail()));await first;await duplicate;assert.equal(h.reloads(),0);assert.equal(h.timers.size,0);await select(h);assert.equal(h.s.showImportConfirm.value,true)
})
test('成功刷新窗口仍阻止重复导入，600ms刷新计时在卸载时清理',async()=>{
 const h=mount();await select(h);h.reply(response({},200));await h.s.confirmImport();assert.equal(h.s.importing.value,true);assert.equal(h.timers.size,1);assert.deepEqual(h.delays,[600])
 await select(h);await h.s.confirmImport();assert.equal(h.calls.length,1);h.unmount();assert.equal(h.timers.size,0);h.runTimers();assert.equal(h.reloads(),0)
 const fresh=mount();await select(fresh);fresh.reply(response({},200));await fresh.s.confirmImport();fresh.runTimers();assert.equal(fresh.reloads(),1)
})
test('读取文件与请求晚到结果在卸载后不打开弹窗、不提示、不刷新',async(t)=>{
 const reading=mount(),pendingRead=deferred();const file=new File(['{}'],'fixture.json');file.text=()=>pendingRead.promise
 const read=reading.s.importConfig(fileEvent(file));t.after(async()=>{pendingRead.resolve('{}');await read});reading.unmount();pendingRead.resolve('{}');await read;assert.equal(reading.s.pendingImport.value,null);assert.equal(reading.s.showImportConfirm.value,false)
 for(const reply of [response(detail()),response({},200)]){const h=mount();await select(h);const pending=deferred();h.reply(pending.promise);const request=h.s.confirmImport();t.after(async()=>{pending.resolve(reply);await request});h.unmount();pending.resolve(reply);await request;assert.equal(h.messages.length,0);assert.equal(h.s.showImportFailure.value,false);assert.equal(h.timers.size,0);assert.equal(h.reloads(),0)}
})
test('取消确认清除敏感文件引用，未确认时不能触发导入',async()=>{
 const h=mount();await select(h);h.s.cancelImport();assert.equal(h.s.pendingImport.value,null);await h.s.confirmImport();assert.equal(h.calls.length,0)
})

test('确认弹窗的关闭事件清除待提交文件，读取期间不替换文件',async(t)=>{
 const h=mount();await select(h);const modal=h.inspect().modals.find(m=>m.title==='确认导入完整配置')
 const listeners=modal['onUpdate:show'];for(const fn of Array.isArray(listeners)?listeners:[listeners])fn(false)
 assert.equal(h.s.pendingImport.value,null);assert.equal(h.s.showImportConfirm.value,false)
 const waiting=deferred(),file=new File(['{}'],'first.json');file.text=()=>waiting.promise
 const first=h.s.importConfig(fileEvent(file));t.after(async()=>{waiting.resolve('{}');await first});await select(h,'{"version":4}');assert.equal(h.s.pendingImport.value,null)
 waiting.resolve('{}');await first;assert.equal(h.s.pendingImport.value,file);assert.equal(h.s.importFilename.value,'first.json')
})
