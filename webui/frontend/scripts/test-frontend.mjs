// 统一前端行为入口：逐项预算由 Node 执行，父进程独立监督总预算。
import { spawn } from 'node:child_process'
import { readdirSync } from 'node:fs'
import { fileURLToPath } from 'node:url'
import { resolve } from 'node:path'

export const testDirectory = fileURLToPath(new URL('../', import.meta.url))
export const suites = {
  alerts: 'alerts-load', dashboard: 'dashboard-observation', dryrun: 'dryrun-state',
  state: 'p316-state', delete: 'delete-confirm', logs: 'logs-stream', import: 'import-presence',
  behavior: 'frontend-behavior', runner: 'test-runner',
}

export function selectFiles(args, cwd = testDirectory) {
  if (!args.length) {
    const files = readdirSync(resolve(cwd, 'tests')).filter(name => name.endsWith('.test.mjs')).sort()
    if (!files.length) throw new Error('未找到前端行为测试文件')
    return files.map(name => `tests/${name}`)
  }
  return [...new Set(args.map(name => {
    if (!Object.hasOwn(suites, name)) throw new Error(`未知前端测试入口：${name}`)
    return `tests/${suites[name]}.test.mjs`
  }))]
}

// budgetMs/graceMs 只供本文件的基础设施回归缩短故障注入时间，CLI固定120秒+1秒收尾。
export function runTests(files, { cwd = testDirectory, budgetMs = 120000, graceMs = 1000, stdio = 'inherit' } = {}) {
  if (!files.length) throw new Error('测试文件列表不能为空')
  return new Promise(resolveResult => {
    const env = { ...process.env }
    // 从基础设施测试内启动独立测试根进程，不能继承父测试worker身份。
    delete env.NODE_TEST_CONTEXT
    const child = spawn(process.execPath, ['--test', '--test-timeout=30000', ...files], {
      cwd, stdio, detached: true, env,
    })
    let exited = false, exitCode = 1, stopping = false, timedOut = false, escalation
    function killGroup(signal) {
      if (!child.pid) return
      try { process.kill(-child.pid, signal) }
      catch (error) { if (error.code !== 'ESRCH') throw error }
    }
    function finish() {
      if (!exited || escalation) return
      clearTimeout(budget)
      process.removeListener('SIGINT', interrupt)
      process.removeListener('SIGTERM', terminate)
      resolveResult({ exitCode, timedOut })
    }
    function stop(code) {
      if (stopping) return
      stopping = true
      exitCode = code
      killGroup('SIGTERM')
      // 即使直接子进程已退出，也保留此计时，回收仍在同组的测试worker。
      escalation = setTimeout(() => {
        killGroup('SIGKILL')
        escalation = undefined
        finish()
      }, graceMs)
    }
    const interrupt = () => stop(130)
    const terminate = () => stop(143)
    process.on('SIGINT', interrupt)
    process.on('SIGTERM', terminate)
    const budget = setTimeout(() => {
      timedOut = true
      console.error('FRONTEND_TEST_TIMEOUT：前端行为测试超过总预算')
      stop(1)
    }, budgetMs)
    child.on('error', error => {
      console.error('前端测试进程启动失败：', error.message)
      exited = true
      finish()
    })
    child.on('close', (code, signal) => {
      exited = true
      if (!stopping) exitCode = signal ? 1 : code ?? 1
      finish()
    })
  })
}

if (process.argv[1] && resolve(process.argv[1]) === fileURLToPath(import.meta.url)) {
  try {
    const result = await runTests(selectFiles(process.argv.slice(2)))
    process.exitCode = result.exitCode
  } catch (error) {
    console.error(error.message)
    process.exitCode = 1
  }
}
