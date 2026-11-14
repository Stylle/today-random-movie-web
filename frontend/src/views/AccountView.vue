<script setup>
import { ref } from 'vue'
import { t } from '../i18n'
import { api } from '../api'
import { store } from '../store'
const current = ref(''), next = ref(''), confirm = ref(''), busy = ref(false), error = ref('')
async function save() {
  error.value = ''
  if (next.value !== confirm.value) { error.value = '两次新密码不一致'; return }
  busy.value = true
  try {
    await api('/api/auth/password', { method: 'POST', body: JSON.stringify({ currentPassword: current.value, newPassword: next.value }) })
    current.value = ''; next.value = ''; confirm.value = ''; store.user = null; location.hash = '#/login'
  } catch (e) { error.value = e.message }
  finally { busy.value = false }
}
</script>
<template>
  <section class="card account-security">
    <h1>{{ t('账号安全') }}</h1>
    <p class="muted">{{ t('修改后旧密码与所有设备的登录状态立即失效，请使用新密码重新登录。') }}</p>
    <form @submit.prevent="save">
      <label>{{ t('当前密码') }}<input v-model="current" type="password" autocomplete="current-password" maxlength="256" required /></label>
      <label>{{ t('新密码') }}<input v-model="next" type="password" autocomplete="new-password" minlength="8" maxlength="256" required /></label>
      <label>{{ t('确认新密码') }}<input v-model="confirm" type="password" autocomplete="new-password" minlength="8" maxlength="256" required /></label>
      <p v-if="error" role="alert">{{ t(error) }}</p>
      <button class="primary" :disabled="busy">{{ t(busy ? '正在保存…' : '修改密码并重新登录') }}</button>
    </form>
  </section>
</template>
<style scoped>.account-security{max-width:580px;margin:24px auto;padding:28px}.account-security form{display:grid;gap:16px;margin-top:24px}.account-security label{display:grid;gap:8px}.account-security input{width:100%}</style>
