import { reactive } from 'vue'
import { api } from './api.js'

export const store = reactive({
  config: { siteTitle: '今日想看', movieCount: 0 },
  user: null,
  authChecked: false,
})

export async function loadConfig() {
  store.config = await api('/api/public/config')
  document.title = store.config.siteTitle || '今日想看'
}

export async function loadSession() {
  try {
    const result = await api('/api/auth/me')
    store.user = result.user
  } catch (error) {
    if (error.status !== 401) throw error
    store.user = null
  } finally {
    store.authChecked = true
  }
}

export async function login(payload) {
  const result = await api('/api/auth/login', { method: 'POST', body: JSON.stringify(payload) })
  store.user = result.user
  return result
}

export async function logout() {
  try { await api('/api/auth/logout', { method: 'POST' }) } catch (_) {}
  store.user = null
}
