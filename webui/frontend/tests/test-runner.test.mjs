// 总预算用真实进程故障验证；缩短的预算不改变CLI固定的120秒默认值。
import assert from 'node:assert/strict'
import test from 'node:test'
import { mkdtempSync, writeFileSync, readFileSync, existsSync, rmSync, mkdirSync } from 'node:fs'
import { tmpdir } from 'node:os'
import { join } from 'node:path'
import { spawn } from 'node:child_process'
import { setTimeout as pause } from 'node:timers/promises'
import { runTests, selectFiles, suites } from '../scripts/test-frontend.mjs'
function fixture(t, source) {
  const cwd = mkdtempSync(join(tmpdir(), 'fwalizer-frontend-runner-'))
  t.after(() => rmSync(cwd, { recursive: true, force: true }))
  writeFileSync(join(cwd, 'fixture.mjs'), source)
  return cwd
}
const execute = cwd => runTests(['fixture.mjs'], { cwd, budgetMs: 1500, graceMs: 100, stdio: 'ignore' })
test('I8R21 runner保留普通成功与断言失败退出码', async t => {
  for (const failure of [false, true]) {
    const cwd = fixture(t, `import test from 'node:test'; test('fixture',()=>{${failure ? "throw new Error('expected assertion')" : ''}})`)
    const result = await execute(cwd)
    assert.equal(result.timedOut, false)
    assert.equal(result.exitCode, failure ? 1 : 0)
  }
})
for (const [name, source] of [
  ['未解Promise', "test('blocked',async()=>new Promise(()=>{}));setInterval(()=>{},1000)"],
  ['同步死循环', "test('blocked',()=>{while(true){}})"],
  ['遗留interval', "test('green',()=>setInterval(()=>{},1000))"],
]) {
  test(`I8R21 runner总预算终止${name}并明确非零`, async t => {
    const diagnostic = t.mock.method(console, 'error', () => {})
    const cwd = fixture(t, `import test from 'node:test';${source}`)
    const result = await execute(cwd)
    assert.equal(result.timedOut, true, 'R21_TOTAL_BUDGET')
    assert.equal(result.exitCode, 1)
    assert.match(diagnostic.mock.calls[0].arguments[0], /FRONTEND_TEST_TIMEOUT/)
  })
}
test('I8R21 runner在直接子进程退出后仍回收忽略TERM的同组worker', async t => {
  t.mock.method(console, 'error', () => {})
  const cwd = fixture(t, `import test from 'node:test';import {writeFileSync} from 'node:fs';
    process.on('SIGTERM',()=>{});writeFileSync('worker-pid',String(process.pid));test('worker',()=>setInterval(()=>{},1000))`)
  const result = await execute(cwd)
  assert.equal(result.timedOut, true)
  const pid = Number(readFileSync(join(cwd, 'worker-pid'), 'utf8'))
  // KILL已发出后等待OS回收该具体worker，不能把信号发送成功当作进程已消失。
  const deadline = Date.now() + 1500
  while (Date.now() < deadline) {
    try { process.kill(pid, 0) } catch (error) { if (error.code === 'ESRCH') return; throw error }
    await pause(10)
  }
  assert.fail('R21_WORKER_REAPED：同组worker未退出')
})
test('I8R21 runner零文件和未知别名失败，全部入口有明确文件', t => {
  const cwd = fixture(t, '')
  mkdirSync(join(cwd, 'tests'))
  assert.throws(() => selectFiles([], cwd), /未找到/)
  assert.throws(() => selectFiles(['missing']), /未知/)
  assert.throws(() => runTests([]), /不能为空/)
  for (const name of Object.keys(suites)) {
    const files = selectFiles([name])
    assert.equal(files.length, 1)
    assert.ok(existsSync(new URL(`../${files[0]}`, import.meta.url)))
    const pkg = JSON.parse(readFileSync(new URL('../package.json', import.meta.url), 'utf8'))
    assert.equal(pkg.scripts[`test:${name}`], `node scripts/test-frontend.mjs ${name}`, '专项别名不能绕过共同预算')
  }
})

for (const [signal, expected] of [['SIGINT', 130], ['SIGTERM', 143]]) {
  test(`I8R21 runner转发${signal}并保持非零退出`, async t => {
    const cwd = fixture(t, "import test from 'node:test';test('pending',()=>setInterval(()=>{},1000))")
    const runner = new URL('../scripts/test-frontend.mjs', import.meta.url).href
    writeFileSync(join(cwd, 'controller.mjs'), `import {runTests} from ${JSON.stringify(runner)};import {writeFileSync} from 'node:fs';
      const task=runTests(['fixture.mjs'],{cwd:process.cwd(),graceMs:50,stdio:'ignore'});writeFileSync('ready','yes');process.exitCode=(await task).exitCode`)
    const child = spawn(process.execPath, ['controller.mjs'], { cwd, stdio: 'ignore' })
    const exited = new Promise((resolve, reject) => { child.on('error', reject); child.on('close', resolve) })
    t.after(async () => { if (child.exitCode === null) child.kill('SIGTERM'); await exited })
    const deadline = Date.now() + 3000
    while (!existsSync(join(cwd, 'ready')) && Date.now() < deadline) await pause(10)
    assert.ok(existsSync(join(cwd, 'ready')), '控制进程必须实际启动')
    child.kill(signal)
    assert.equal(await exited, expected)
  })
}
