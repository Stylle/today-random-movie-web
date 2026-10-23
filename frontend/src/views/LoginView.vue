<script setup>
import { ref } from 'vue'
import { store, login, setup } from '../store.js'

const username = ref('')
const password = ref('')
const setupToken = ref('')
const remember = ref(true)
const busy = ref(false)
const message = ref('')

async function submit() {
  if (busy.value) return
  busy.value = true
  message.value = ''
  try {
    const payload = { username: username.value.trim(), password: password.value, remember: remember.value }
    if (store.config.setupRequired) await setup({ ...payload, setupToken: setupToken.value.trim() })
    else await login(payload)
    location.hash = '#/admin'
  } catch (e) { message.value = e.message } finally { busy.value = false }
}
</script>

<template>
  <div class="login-wrap">
    <section class="login-card">
      <div class="login-mark"><img src="/logo.svg" alt="" /><div><span class="eyebrow">{{ store.config.setupRequired ? 'FIRST RUN' : 'WELCOME BACK' }}</span><h1>{{ store.config.setupRequired ? '初始化站点' : '管理员登录' }}</h1></div></div>
      <p>{{ store.config.setupRequired ? '创建第一个管理员。初始化令牌来自 Compose 环境变量或首次启动日志。' : '登录后可以连接飞牛、同步媒体库并配置自动推送。' }}</p>
      <label v-if="store.config.setupRequired" class="field"><span>初始化令牌</span><input v-model="setupToken" type="password" autocomplete="one-time-code" placeholder="SETUP_TOKEN" /></label>
      <label class="field"><span>用户名</span><input v-model="username" autocomplete="username" placeholder="admin" /></label>
      <label class="field"><span>密码</span><input v-model="password" type="password" autocomplete="current-password" placeholder="至少 12 位" @keyup.enter="submit" /></label>
      <label class="remember"><input v-model="remember" type="checkbox" />在这台设备上保持登录</label>
      <button class="primary full" :disabled="busy" @click="submit">{{ busy ? '请稍候…' : store.config.setupRequired ? '创建管理员' : '登录' }}</button>
      <p class="inline-error" v-if="message">{{ message }}</p>
      <small class="security-note">登录使用 HttpOnly 会话 Cookie，并对修改操作执行 CSRF 校验。</small>
    </section>
  </div>
</template>
