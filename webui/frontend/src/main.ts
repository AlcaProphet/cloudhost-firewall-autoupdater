import { createApp } from 'vue'
import { createRouter, createWebHashHistory } from 'vue-router'
import App from './App.vue'

const router = createRouter({
  history: createWebHashHistory(),
  routes: [
    { path: '/', component: () => import('./views/Dashboard.vue') },
    { path: '/targets', component: () => import('./views/Targets.vue') },
    { path: '/rules', component: () => import('./views/Rules.vue') },
    { path: '/edgeone', component: () => import('./views/EdgeOne.vue') },
    { path: '/settings', component: () => import('./views/Settings.vue') },
    { path: '/logs', component: () => import('./views/Logs.vue') },
    { path: '/dry-run', component: () => import('./views/RunTest.vue') },
    { path: '/alerts', component: () => import('./views/Alerts.vue') },
  ]
})

createApp(App).use(router).mount('#app')
