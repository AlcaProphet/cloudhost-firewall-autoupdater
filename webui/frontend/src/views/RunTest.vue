<script setup lang="ts">
// 模拟测试页（原「运行测试」页：移除连接测试 Tab 与双标签结构，路由 /dry-run）
// 连接测试能力保留在目标添加/编辑弹窗（POST /api/test-connection）
import { NButton, NSpace, useMessage } from 'naive-ui'
import DryRunResults from '../components/DryRunResults.vue'
import { useDryRun } from '../composables/useDryRun'

const message = useMessage()

// 模拟测试：按当前云资源目标与域名规则计算变更预览，不实际写入
const { loading, results, warnings, lastRunAt, run } = useDryRun()

async function runDryRun() {
  try {
    await run()
    message.success('模拟测试完成')
  } catch (e: any) {
    message.error(`模拟测试失败: ${e.message}`)
  }
}
</script>

<template>
  <div>
    <h2>模拟测试</h2>
    <NSpace vertical>
      <!-- 说明文字：模拟测试语义与使用建议 -->
      <div style="font-size: 14px; color: #888; margin-bottom: 4px">
        模拟测试按当前云资源目标与域名规则计算变更预览，不实际写入任何云防火墙规则；建议在修改目标或规则后执行，确认预期变更后再开启同步
      </div>
      <NSpace align="center">
        <NButton type="primary" size="large" :loading="loading" @click="runDryRun">执行模拟测试</NButton>
        <span v-if="lastRunAt" style="font-size: 14px; color: #999">
          上次执行：{{ lastRunAt.toLocaleTimeString() }}
        </span>
      </NSpace>
      <DryRunResults :results="results" :warnings="warnings" :has-run="lastRunAt !== null" />
    </NSpace>
  </div>
</template>
