<script setup lang="ts">
// 应用外壳：侧边栏导航 + 全局主题（light/dark，DB 持久化，Build4 Step 5）
import { NLayout, NLayoutSider, NLayoutContent, NMenu, NConfigProvider, NMessageProvider, NSwitch, NTooltip } from 'naive-ui'
import { darkTheme } from 'naive-ui'
import { useRouter, useRoute } from 'vue-router'
import { computed, onMounted } from 'vue'
import { useSettings } from './composables/useSettings'

const router = useRouter()
const route = useRoute()
// 高亮跟随已经生效的路由，覆盖深链、页面内跳转与浏览器历史导航。
const activeKey = computed(() => route.path)
const { theme, applyTheme, setTheme } = useSettings()

// 全局主题覆盖：字号 16px + 按钮高度统一放大（明暗主题共用）
// 注意：naive-ui 组件字号为分尺寸变量（fontSizeTiny/Small/Medium/Large），需逐尺寸覆盖
const themeOverrides = {
  common: { fontSize: '16px' },
  Button: {
    heightLarge: '44px',
    heightMedium: '40px',
    heightSmall: '36px',
    heightTiny: '28px',
    fontSizeLarge: '16px',
    fontSizeMedium: '16px',
    fontSizeSmall: '15px',
    fontSizeTiny: '13px',
  },
  Input: {
    fontSizeLarge: '17px',
    fontSizeMedium: '16px',
    fontSizeSmall: '15px',
    fontSizeTiny: '15px',
  },
  InternalSelection: {
    fontSizeLarge: '17px',
    fontSizeMedium: '16px',
    fontSizeSmall: '15px',
    fontSizeTiny: '15px',
    heightLarge: '44px',
    heightMedium: '40px',
    heightSmall: '36px',
    heightTiny: '28px',
  },
  Form: {
    labelFontSizeTopLarge: '16px',
    labelFontSizeTopMedium: '16px',
    labelFontSizeLeftLarge: '16px',
    labelFontSizeLeftMedium: '16px',
  },
  DataTable: {
    fontSizeLarge: '16px',
    fontSizeMedium: '15px',
    fontSizeSmall: '14px',
  },
}

const menuOptions = [
  { label: '仪表盘', key: '/' },
  { label: '云资源管理', key: '/targets' },
  { label: '域名规则', key: '/rules' },
  { label: '全局设置', key: '/settings' },
  { label: '同步日志', key: '/logs' },
  { label: '模拟测试', key: '/dry-run' },
  { label: '告警配置', key: '/alerts' },
]

function handleMenuUpdate(key: string) {
  router.push(key)
}

onMounted(applyTheme)
</script>

<template>
  <NConfigProvider :theme="theme === 'dark' ? darkTheme : null" :theme-overrides="themeOverrides">
    <NMessageProvider>
      <NLayout has-sider style="height: 100vh">
        <NLayoutSider bordered :width="200">
          <div style="padding: 16px 16px 0; font-weight: bold; font-size: 18px; display: flex; justify-content: space-between; align-items: center">
            <span>FWAlizer</span>
            <!-- 主题切换：图标 + tooltip（Build4 Step 10：可读性增强） -->
            <NTooltip>
              <template #trigger>
                <span style="display: flex; align-items: center; gap: 4px; cursor: pointer">
                  <span style="font-size: 16px">{{ theme === 'dark' ? '🌙' : '☀️' }}</span>
                  <NSwitch size="small" :value="theme === 'dark'" @update:value="(v: boolean) => setTheme(v ? 'dark' : 'light')" />
                </span>
              </template>
              切换明暗主题
            </NTooltip>
          </div>
          <NMenu
            style="margin-top: 12px"
            :value="activeKey"
            :options="menuOptions"
            @update:value="handleMenuUpdate"
          />
        </NLayoutSider>
        <NLayoutContent content-style="padding: 24px;">
          <router-view />
        </NLayoutContent>
      </NLayout>
    </NMessageProvider>
  </NConfigProvider>
</template>
