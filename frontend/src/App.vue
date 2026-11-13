<script setup>
import { t } from "./i18n";
import { computed, onMounted, onUnmounted, ref } from 'vue'
import { store, loadConfig, loadSession, logout } from './store.js'
import HomeView from './views/HomeView.vue'
import LibraryView from './views/LibraryView.vue'
import LoginView from './views/LoginView.vue'
import AdminView from './views/AdminView.vue'
import AppearanceControls from './components/AppearanceControls.vue'

const routes = { today: HomeView, library: LibraryView, login: LoginView, admin: AdminView }
const route = ref('today')
const menuOpen = ref(false)
const bootError = ref('')

const component = computed(() => routes[route.value] || HomeView)
const routeProps = computed(() => ({ key: route.value }))

function syncRoute() {
  const wanted = location.hash.replace(/^#\/?/, '') || 'today'
  route.value = store.authChecked && !store.user ? 'login' : routes[wanted] ? wanted : 'today'
  menuOpen.value = false
}

function go(name) { location.hash = `#/${name}` }

async function signOut() { await logout(); go('today') }


onMounted(async () => {
  syncRoute()
  window.addEventListener('hashchange', syncRoute)
  try { await Promise.all([loadConfig(), loadSession()]) } catch (error) { bootError.value = error.message }
  if (!store.user) go('login')
})
onUnmounted(() => window.removeEventListener('hashchange', syncRoute))
</script>

<template>
  <div class="app-shell" data-drawer-host>
    <header class="topbar">
      <a class="brand" href="#/today" :aria-label="t('首页')">
        <img src="/logo.svg" alt="" />
        <span class="brand-copy"><b>{{ store.config.siteTitle }}</b><small>{{ t('今天，就看这一部') }}</small></span>
      </a>
      <button class="drawer-toggle icon-btn" type="button" data-drawer-toggle aria-controls="movie-nav" aria-expanded="false" :aria-label="t('展开导航')">☰</button>
      <button class="drawer-backdrop" type="button" data-drawer-close :aria-label="t('关闭导航')"></button>
      <nav id="movie-nav" data-drawer :aria-label="t('主导航')">
        <button class="drawer-close" type="button" data-drawer-close>{{ t('关闭导航') }}</button>
        <a v-if="store.user" href="#/today" :class="{ active: route === 'today' }">{{ t('今日想看') }}</a>
        <a v-if="store.user" href="#/library" :class="{ active: route === 'library' }">{{ t('影片库') }}</a>
        <a v-if="store.user" href="#/admin" :class="{ active: route === 'admin' }">{{ t('管理设置') }}</a>
      </nav>
      <div class="top-actions">
        <AppearanceControls />
        <div v-if="store.user" class="user-wrap">
          <button class="user-chip" @click="menuOpen = !menuOpen"><span>{{ store.user.username[0].toUpperCase() }}</span>{{ store.user.username }} ▾</button>
          <div class="user-menu" v-if="menuOpen">
            <button @click="go('admin')">{{ t('管理设置') }}</button>
            <button class="danger-text" @click="signOut">{{ t('退出登录') }}</button>
          </div>
        </div>
        <button v-else class="ghost compact" @click="go('login')">{{ t('登录') }}</button>
      </div>
    </header>

    <div v-if="bootError" class="global-error">{{ t('无法连接服务：') }}{{ bootError }}</div>
    <main class="page-wrap"><component :is="component" v-bind="routeProps" /></main>
    <footer class="site-footer"><span>{{ t('© 2026 Stylle · 今日想看 · MIT License') }}</span><button type="button" data-about-open>{{ t('关于与许可') }}</button></footer>
  </div>
</template>
