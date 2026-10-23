<script setup>
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
    location.hash = '#/admin'
  } catch (e) { message.value = e.message } finally { busy.value = false }
}
</script>

<template>
  <div class="login-wrap">
    <section class="login-card">
      <div class="login-mark"><img src="/logo.svg" alt="" /><div><span class="eyebrow">WELCOME BACK</span><h1>管理员登录</h1></div></div>
      <p>账号由 Docker 的 USER、PASSWORD 在首次启动时创建。</p>
      <label class="field"><span>用户名</span><input v-model="username" autocomplete="username" placeholder="admin" /></label>
      <label class="field"><span>密码</span><input v-model="password" type="password" autocomplete="current-password" placeholder="至少 12 位" @keyup.enter="submit" /></label>
      <label class="remember"><input v-model="remember" type="checkbox" />在这台设备上保持登录</label>
      <button class="primary full" :disabled="busy" @click="submit">{{ busy ? '请稍候…' : '登录' }}</button>
      <p class="inline-error" v-if="message">{{ message }}</p>
      <small class="security-note">账号保存在 data/app.db，飞牛密码使用 data/secret.key 加密。</small>
    </section>
  </div>
</template>
