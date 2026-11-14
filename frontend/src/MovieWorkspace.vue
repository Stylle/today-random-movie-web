<script setup>
import { t } from './i18n'
import { computed, nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { store, loadConfig, loadSession, logout } from './store.js'
import HomeView from './views/HomeView.vue'
import LibraryView from './views/LibraryView.vue'
import LoginView from './views/LoginView.vue'
import AdminView from './views/AdminView.vue'
import AccountView from './views/AccountView.vue'
import AppearanceControls from './components/AppearanceControls.vue'
import UiIcon from './components/UiIcon.vue'
const routes = { today: HomeView, library: LibraryView, login: LoginView, admin: AdminView, account: AccountView }
const management = [
  { id: 'connection', label: '连接设置' }, { id: 'sources', label: '数据来源' }, { id: 'webhook', label: '回调通知' },
  { id: 'telegram', label: '电报助手' }, { id: 'schedules', label: '自动推送' },
  { id: 'history', label: '运行记录' }, { id: 'backup', label: '备份恢复' },
]
const route = ref('today'), adminSection = ref('connection'), navOpen = ref(false), bootError = ref('')
const component = computed(() => routes[route.value] || HomeView)
const routeProps = computed(() => route.value === 'admin' ? { section: adminSection.value } : {})
const heading = computed(() => route.value === 'account' ? '账号安全' : route.value === 'admin' ? management.find(x => x.id === adminSection.value)?.label : route.value === 'library' ? '影片库' : '今日想看')
let focusBeforeDrawer, previousOverflow = ''
watch(navOpen, async open => {
  if (open) {
    focusBeforeDrawer = document.activeElement
    previousOverflow = document.body.style.overflow
    document.body.style.overflow = 'hidden'
    await nextTick()
    document.querySelector('.movie-sidebar a')?.focus()
  } else {
    document.body.style.overflow = previousOverflow
    focusBeforeDrawer?.focus()
  }
})
function drawerKey(event) {
  if (!navOpen.value) return
  if (event.key === 'Escape') { navOpen.value = false; return }
  if (event.key !== 'Tab') return
  const nodes = [...document.querySelectorAll('.movie-sidebar a,.movie-sidebar button')].filter(el => !el.disabled && el.getClientRects().length)
  const first = nodes[0], last = nodes[nodes.length - 1]
  if (event.shiftKey && document.activeElement === first) { event.preventDefault(); last?.focus() }
  if (!event.shiftKey && document.activeElement === last) { event.preventDefault(); first?.focus() }
}
function resizeNavigation() { if (innerWidth > 1000) navOpen.value = false }
function syncRoute() {
  const [wanted = 'today', section = 'connection'] = location.hash.replace(/^#\/?/, '').split('/')
  route.value = store.authChecked && !store.user ? 'login' : routes[wanted] ? wanted : 'today'
  adminSection.value = management.some(x => x.id === section) ? section : 'connection'
  navOpen.value = false
}
// Authentication may succeed while the hash already points to the desired page.
watch(() => store.user, syncRoute)
const signOutError = ref(''), signingOut = ref(false)
async function signOut() {
  if (signingOut.value) return
  signingOut.value = true; signOutError.value = ''
  try { await logout(); location.hash = '#/login'; syncRoute() }
  catch { signOutError.value = '退出未完成，请检查网络后重试。当前会话仍可能有效。' }
  finally { signingOut.value = false }
}
onMounted(async () => {
  window.addEventListener('hashchange', syncRoute)
  window.addEventListener('keydown', drawerKey)
  window.addEventListener('resize', resizeNavigation)
  try { await Promise.all([loadConfig(), loadSession()]) } catch (error) { bootError.value = error.message }
  syncRoute()
})
onUnmounted(() => { window.removeEventListener('hashchange', syncRoute); window.removeEventListener('keydown', drawerKey); window.removeEventListener('resize', resizeNavigation); document.body.style.overflow = previousOverflow })
</script>
<template>
  <div class="app-shell" :class="{ 'movie-workspace': store.user, 'nav-open': navOpen }">
    <template v-if="store.user">
      <button v-if="navOpen" class="movie-nav-backdrop" @click="navOpen = false" :aria-label="t('关闭导航')"></button>
      <aside id="movie-navigation" class="movie-sidebar" :aria-label="t('主导航')">
        <a class="brand" href="#/today"><img src="/logo.svg" alt="" /><span class="brand-copy"><b>{{ store.config.siteTitle }}</b><small>{{ t('今天，就看这一部') }}</small></span></a>
        <nav aria-label="主导航" @click="navOpen = false">
          <a href="#/today" :class="{ active: route === 'today' }" :aria-current="route === 'today' ? 'page' : undefined"><UiIcon name="releases" />{{ t('今日想看') }}</a>
          <a href="#/library" :class="{ active: route === 'library' }" :aria-current="route === 'library' ? 'page' : undefined"><UiIcon name="files" />{{ t('影片列表') }}</a>
          <span class="movie-nav-label">{{ t('管理设置') }}</span>
          <a v-for="item in management" :key="item.id" :href="`#/admin/${item.id}`" :class="{ active: route === 'admin' && adminSection === item.id }" :aria-current="route === 'admin' && adminSection === item.id ? 'page' : undefined"><UiIcon :name="item.id" />{{ t(item.label) }}</a>
          <button type="button" data-about-open @click="navOpen = false"><UiIcon name="account" />{{ t('关于项目') }}</button>
          <a href="#/account" :class="{ active: route === 'account' }"><UiIcon name="account" />{{ t('账号安全') }}</a>
        </nav>
        <div class="movie-sidebar-foot"><div class="movie-account"><strong>{{ store.user.username }}</strong><button type="button" class="movie-signout account-signout" :title="t('退出登录')" :aria-label="t('退出登录')" :disabled="signingOut" @click="signOut"><svg class="ui-icon" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="1.7" stroke-linecap="round" stroke-linejoin="round" aria-hidden="true"><path d="M9 4H4v16h5 M14 8l4 4-4 4 M8 12h12"/></svg></button></div><small>© 2026 Stylle · MIT License</small></div>
      </aside>
      <header class="topbar movie-workspace-header">
        <button class="icon-btn movie-nav-toggle" @click="navOpen = !navOpen" :aria-expanded="navOpen" aria-controls="movie-navigation" :aria-label="t('主导航')"><UiIcon name="all" /></button>
        <strong>{{ t(heading) }}</strong>
        <div class="top-actions"><AppearanceControls :show-about="false" /></div>
      </header>
    </template>
    <header v-else class="topbar"><a class="brand" href="#/today"><img src="/logo.svg" alt="" /><b>{{ store.config.siteTitle }}</b></a><div class="top-actions"><AppearanceControls /></div></header>
    <div v-if="bootError" class="global-error">{{ t('无法连接服务：') }}{{ bootError }}</div>
    <div v-if="signOutError" class="global-error" role="alert">{{ t(signOutError) }} <button :disabled="signingOut" @click="signOut">{{ t('重试') }}</button></div>
    <main class="page-wrap"><component :is="component" :key="route" v-bind="routeProps" /></main>
    <footer class="site-footer"><span>{{ t('© 2026 Stylle · 今日想看 · MIT License') }}</span></footer>
  </div>
</template>
