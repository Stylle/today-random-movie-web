<script setup>
import { t } from "./../i18n";
import { ref } from 'vue'
import { login } from '../store.js'

const username = ref('')
const password = ref('')
const remember = ref(true)
const busy = ref(false)
const message = ref('')

async function submit() {
  if (busy.value) return
  busy.value = true
  message.value = ''
  try {
    const payload = { username: username.value.trim(), password: password.value, remember: remember.value }
    await login(payload)
    location.hash = '#/today'
  } catch (e) { message.value = e.message } finally { busy.value = false }
}
</script>

<template>
  <div class="login-wrap">
    <section class="login-card">
      <div class="login-mark"><img src="/logo.svg" alt="" /><div><h1>{{ t('管理员登录') }}</h1></div></div>
      <p>{{ t('登录后管理媒体库、选片偏好与通知。') }}</p>
      <label class="field"><span>{{ t('用户名') }}</span><input v-model="username" autocomplete="username" placeholder="admin" /></label>
      <label class="field"><span>{{ t('密码') }}</span><input v-model="password" type="password" autocomplete="current-password" :placeholder="t('请输入密码')" @keyup.enter="submit" /></label>
      <label class="remember"><input v-model="remember" type="checkbox" />{{ t('在这台设备上保持登录') }}</label>
      <button class="primary full" :disabled="busy" @click="submit">{{ busy ? t('请稍候…') : t('登录') }}</button>
      <p class="inline-error" v-if="message">{{ message }}</p>
      <small class="security-note">{{ t('请在可信设备上登录，使用完毕可主动退出。') }}</small>
    </section>
  </div>
</template>
