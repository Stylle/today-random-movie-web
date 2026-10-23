<script setup>
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { store, loadConfig, loadSession, logout } from './store.js'
import HomeView from './views/HomeView.vue'
import LibraryView from './views/LibraryView.vue'
import LoginView from './views/LoginView.vue'
import AdminView from './views/AdminView.vue'

const routes = { today: HomeView, library: LibraryView, login: LoginView, admin: AdminView }
const route = ref('today')
const navOpen = ref(false)
const menuOpen = ref(false)
const theme = ref(localStorage.getItem('trm_theme') || 'system')
const bootError = ref('')

const component = computed(() => routes[route.value] || HomeView)
const routeProps = computed(() => ({ key: route.value }))

function syncRoute() {
  const wanted = location.hash.replace(/^#\/?/, '') || 'today'
  route.value = routes[wanted] ? wanted : 'today'
  navOpen.value = false
  menuOpen.value = false
}

function go(name) { location.hash = `#/${name}` }

function applyTheme(value) {
  theme.value = value
  localStorage.setItem('trm_theme', value)
  const dark = value === 'dark' || (value === 'system' && matchMedia('(prefers-color-scheme: dark)').matches)
  document.documentElement.dataset.theme = dark ? 'dark' : 'light'
}

function cycleTheme() {
  applyTheme(theme.value === 'light' ? 'dark' : theme.value === 'dark' ? 'system' : 'light')
}

async function signOut() { await logout(); go('today') }

const themeIcon = computed(() => theme.value === 'light' ? '☀' : theme.value === 'dark' ? '☾' : '◐')

onMounted(async () => {
  syncRoute()
  applyTheme(theme.value)
  window.addEventListener('hashchange', syncRoute)
  try { await Promise.all([loadConfig(), loadSession()]) } catch (error) { bootError.value = error.message }
  if (store.config.setupRequired) go('login')
})
onUnmounted(() => window.removeEventListener('hashchange', syncRoute))
</script>

<template>
  <div class="app-shell">
    <header class="topbar">
      <a class="brand" href="#/today" aria-label="首页">
        <img src="/logo.svg" alt="" />
        <span class="brand-copy"><b>{{ store.config.siteTitle }}</b><small>今天，就看这一部</small></span>
      </a>
      <button class="nav-toggle icon-btn" @click="navOpen = !navOpen" :aria-expanded="navOpen">{{ navOpen ? '×' : '☰' }}</button>
      <nav :class="{ open: navOpen }">
        <a href="#/today" :class="{ active: route === 'today' }">今日想看</a>
        <a href="#/library" :class="{ active: route === 'library' }">影片库</a>
        <a v-if="store.user" href="#/admin" :class="{ active: route === 'admin' }">管理设置</a>
      </nav>
      <div class="top-actions">
        <button class="icon-btn" @click="cycleTheme" :title="`主题：${theme}`">{{ themeIcon }}</button>
        <div v-if="store.user" class="user-wrap">
          <button class="user-chip" @click="menuOpen = !menuOpen"><span>{{ store.user.username[0].toUpperCase() }}</span>{{ store.user.username }} ▾</button>
          <div class="user-menu" v-if="menuOpen">
            <button @click="go('admin')">管理设置</button>
            <button class="danger-text" @click="signOut">退出登录</button>
          </div>
        </div>
        <button v-else class="ghost compact" @click="go('login')">登录</button>
      </div>
    </header>

    <div v-if="bootError" class="global-error">无法连接服务：{{ bootError }}</div>
    <main class="page-wrap"><component :is="component" v-bind="routeProps" /></main>
    <footer>今日想看 · 私有媒体库随机选片器 · 本产品使用 TMDB API，但未经 TMDB 认可或认证</footer>
  </div>
</template>
