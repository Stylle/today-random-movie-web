<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, jsonBody } from '../api.js'
import { store, loadConfig } from '../store.js'
import FilterPanel from '../components/FilterPanel.vue'

const tabs = [
  { id: 'connection', label: '连接与同步', icon: '⌁' },
  { id: 'webhook', label: 'Webhook', icon: '↗' },
  { id: 'schedules', label: '自动推送', icon: '◷' },
  { id: 'history', label: '运行记录', icon: '≡' },
]
const tab = ref('connection')
const settings = ref(null)
const webhook = ref({ url: '', method: 'POST', bodyType: 'json', headers: '', bodyTemplate: '' })
const schedules = ref([])
const options = ref({ libraries: [], genres: [], years: [], countries: [] })
const sync = ref({ running: false })
const syncSchedule = ref({ enabled: false, kind: 'daily', time: '03:00', timezone: 'Asia/Shanghai', weekday: 1, lastRunAt: '' })
const history = ref({ random: [], webhooks: [] })
const message = ref('')
const bad = ref(false)
const busy = ref('')
let poll

const freshSchedule = () => ({ name: '每日推荐', enabled: true, kind: 'daily', time: '20:00', timezone: 'Asia/Shanghai', weekday: 5, dayOfMonth: 1, filters: { libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: true } })
const scheduleForm = ref(freshSchedule())
const editing = ref('')
const weekdays = ['星期日', '星期一', '星期二', '星期三', '星期四', '星期五', '星期六']
const webhookPlaceholder = computed(() => webhook.value.bodyType === 'json'
  ? '{\n  "title": "{{title}}",\n  "text": "{{text}}"\n}'
  : '{{text}}')

const cronPreview = computed(() => {
  const [hour = '20', minute = '00'] = scheduleForm.value.time.split(':')
  if (scheduleForm.value.kind === 'weekly') return `${minute} ${hour} * * ${scheduleForm.value.weekday}`
  if (scheduleForm.value.kind === 'monthly') return `${minute} ${hour} ${scheduleForm.value.dayOfMonth} * *`
  return `${minute} ${hour} * * *`
})

function flash(text, isBad = false) {
  message.value = text; bad.value = isBad
  setTimeout(() => { if (message.value === text) message.value = '' }, 5000)
}

async function loadAll() {
  const [s, w, rules, opts, job, syncRule, runs] = await Promise.all([
    api('/api/admin/settings'), api('/api/admin/webhook'), api('/api/admin/schedules'),
    api('/api/movies/options'), api('/api/admin/sync'), api('/api/admin/sync-schedule'), api('/api/admin/history'),
  ])
  settings.value = { ...s, fnPassword: '' }
  webhook.value = w
  schedules.value = rules.items
  options.value = opts
  sync.value = job
  syncSchedule.value = syncRule
  history.value = runs
  if (job.running) startPoll()
}

async function run(label, task) {
  busy.value = label
  try { await task(); flash('已保存并生效') } catch (e) { flash(e.message, true) } finally { busy.value = '' }
}

const saveFn = () => run('fn', async () => {
  await api('/api/admin/fnos', { method: 'PUT', body: jsonBody({ server: settings.value.fnServer, username: settings.value.fnUsername, password: settings.value.fnPassword, insecureTls: settings.value.fnInsecureTls }) })
  settings.value.fnPassword = ''
  settings.value.fnHasPassword = true
})

const testFn = () => run('test', async () => {
  const result = await api('/api/admin/fnos/test', { method: 'POST' })
  flash(`连接成功，读取到 ${result.libraries} 个电影媒体库`)
})

async function startSync() {
  try {
    sync.value = await api('/api/admin/sync', { method: 'POST' })
    flash('同步任务已开始')
    startPoll()
  } catch (e) { flash(e.message, true) }
}

function startPoll() {
  clearInterval(poll)
  poll = setInterval(async () => {
    try {
      sync.value = await api('/api/admin/sync')
      if (!sync.value.running) {
        clearInterval(poll)
        await Promise.all([loadConfig(), refreshHistory()])
        if (sync.value.error) flash(sync.value.error, true)
        else flash(`同步完成，共处理 ${sync.value.total || 0} 部影片`)
      }
    } catch (_) {}
  }, 1200)
}

const saveWebhook = () => run('webhook', () => api('/api/admin/webhook', { method: 'PUT', body: jsonBody(webhook.value) }))
const saveSyncSchedule = () => run('sync-schedule', async () => {
  syncSchedule.value = await api('/api/admin/sync-schedule', { method: 'PUT', body: jsonBody(syncSchedule.value) })
})
const testWebhook = () => run('webhook-test', async () => { await api('/api/admin/webhook/test', { method: 'POST' }); flash('测试消息已发送') })
function editRule(rule = null) {
  editing.value = rule?.id || ''
  scheduleForm.value = rule ? JSON.parse(JSON.stringify(rule)) : freshSchedule()
}

async function saveRule() {
  const method = editing.value ? 'PUT' : 'POST'
  const path = editing.value ? `/api/admin/schedules/${editing.value}` : '/api/admin/schedules'
  await run('schedule', async () => {
    await api(path, { method, body: jsonBody(scheduleForm.value) })
    const result = await api('/api/admin/schedules')
    schedules.value = result.items
    editRule()
  })
}

async function removeRule(id) {
  if (!confirm('确定删除这条自动推送规则？')) return
  await run('schedule-delete', async () => {
    await api(`/api/admin/schedules/${id}`, { method: 'DELETE' })
    schedules.value = schedules.value.filter(item => item.id !== id)
  })
}

async function runRule(id) {
  await run('schedule-run', async () => {
    const result = await api(`/api/admin/schedules/${id}/run`, { method: 'POST' })
    flash(`已推送：${result.movie.title}`)
    await refreshHistory()
  })
}

async function refreshHistory() { history.value = await api('/api/admin/history') }
async function purgePosters() {
  if (!confirm('清空封面缓存？下次访问时会从飞牛重新拉取。')) return
  await run('purge', async () => {
    const result = await api('/api/admin/posters/purge', { method: 'POST' })
    flash(`已清理 ${result.removed} 个缓存文件`)
  })
}

function ruleSummary(rule) {
  const time = `${rule.time} · ${rule.timezone}`
  if (rule.kind === 'weekly') return `每周${weekdays[rule.weekday]} ${time}`
  if (rule.kind === 'monthly') return `每月 ${rule.dayOfMonth} 日 ${time}`
  return `每天 ${time}`
}

watch(() => store.user?.id, async userID => {
  if (!userID || settings.value) return
  try { await loadAll() } catch (e) { flash(e.message, true) }
}, { immediate: true })
onUnmounted(() => clearInterval(poll))
</script>

<template>
  <div v-if="!store.authChecked" class="loading-card">正在确认登录状态…</div>
  <div v-else-if="!store.user" class="empty-state standalone"><div class="empty-reel">◇</div><h2>这里需要管理员权限</h2><p>登录后才能连接飞牛和配置推送。</p><a href="#/login" class="button-link primary">去登录</a></div>
  <div v-else class="admin-layout">
    <aside class="admin-nav"><div class="admin-nav-title"><span class="eyebrow">CONTROL ROOM</span><h2>管理设置</h2></div><button v-for="item in tabs" :key="item.id" :class="{ active: tab === item.id }" @click="tab = item.id"><span>{{ item.icon }}</span>{{ item.label }}</button></aside>

    <section class="admin-content" v-if="settings">
      <template v-if="tab === 'connection'">
        <div class="section-title"><div><span class="eyebrow">FNOS / FNTV</span><h1>连接与同步</h1><p>凭据只在服务端加密保存，浏览器不会接触飞牛会话。</p></div><span class="status-dot" :class="{ on: settings.lastSyncAt }">{{ settings.lastSyncAt ? '已同步' : '未同步' }}</span></div>
        <div class="panel">
          <h3>飞牛连接</h3>
          <div class="field-grid">
            <label class="field span-2"><span>飞牛影视地址</span><input v-model="settings.fnServer" placeholder="http://192.168.1.20:32587/v 或飞牛内网穿透地址" /><small>支持局域网地址、HTTPS 域名和飞牛内网穿透地址。</small></label>
            <label class="field"><span>账号</span><input v-model="settings.fnUsername" autocomplete="off" /></label>
            <label class="field"><span>密码</span><input v-model="settings.fnPassword" type="password" :placeholder="settings.fnHasPassword ? '已保存；留空不修改' : '飞牛密码'" autocomplete="new-password" /></label>
          </div>
          <label class="check-row warning-check"><input v-model="settings.fnInsecureTls" type="checkbox" /><span>允许自签名 HTTPS 证书（仅可信内网 NAS）</span></label>
          <div class="action-row"><button class="primary" :disabled="busy === 'fn'" @click="saveFn">{{ busy === 'fn' ? '保存中…' : '保存连接' }}</button><button class="ghost" :disabled="busy === 'test'" @click="testFn">{{ busy === 'test' ? '测试中…' : '测试连接' }}</button></div>
        </div>
        <div class="panel sync-panel">
          <div><h3>媒体库索引</h3><p>读取飞牛影片信息；封面在访问时缓存到 data/posters。</p><small v-if="settings.lastSyncAt">上次同步：{{ new Date(settings.lastSyncAt).toLocaleString() }}</small></div>
          <div class="sync-action"><button class="primary" :disabled="sync.running" @click="startSync">{{ sync.running ? '同步进行中…' : '立即同步' }}</button><button class="ghost" @click="purgePosters">清理封面缓存</button></div>
          <div class="progress" v-if="sync.running"><div :style="{ width: sync.total ? `${sync.done / sync.total * 100}%` : '8%' }"></div></div>
          <p class="sync-state" v-if="sync.stage">{{ sync.stage }}<template v-if="sync.total"> · {{ sync.done }} / {{ sync.total }}</template></p>
          <p class="inline-error" v-if="settings.lastSyncError">{{ settings.lastSyncError }}</p>
        </div>
        <div class="panel">
          <div class="panel-title-row"><div><h3>定时同步</h3><small>自动执行完整同步，更新新增影片、简介和已观看状态。</small></div><span class="status-dot" :class="{ on: syncSchedule.enabled }">{{ syncSchedule.enabled ? '已启用' : '未启用' }}</span></div>
          <div class="field-grid">
            <label class="field"><span>周期</span><select v-model="syncSchedule.kind"><option value="daily">每日</option><option value="weekly">每周</option></select></label>
            <label class="field"><span>同步时间</span><input v-model="syncSchedule.time" type="time" /></label>
            <label v-if="syncSchedule.kind === 'weekly'" class="field"><span>星期</span><select v-model.number="syncSchedule.weekday"><option v-for="(day, index) in weekdays" :key="day" :value="index">{{ day }}</option></select></label>
            <label class="field"><span>时区</span><input v-model="syncSchedule.timezone" placeholder="Asia/Shanghai" /></label>
          </div>
          <label class="check-row"><input v-model="syncSchedule.enabled" type="checkbox" /><span>启用定时同步</span></label>
          <small v-if="syncSchedule.lastRunAt" class="schedule-last-run">上次自动触发：{{ new Date(syncSchedule.lastRunAt).toLocaleString() }}</small>
          <div class="action-row"><button class="primary" :disabled="busy === 'sync-schedule'" @click="saveSyncSchedule">{{ busy === 'sync-schedule' ? '保存中…' : '保存同步规则' }}</button></div>
        </div>
      </template>

      <template v-if="tab === 'webhook'">
        <div class="section-title"><div><span class="eyebrow">DELIVERY</span><h1>Webhook</h1><p>兼容企业微信、钉钉、飞书、Telegram Bot 与通用 HTTP 接口。</p></div></div>
        <div class="panel">
          <div class="field-grid">
            <label class="field span-2"><span>Webhook URL</span><input v-model="webhook.url" placeholder="https://example.com/webhook" /></label>
            <label class="field"><span>请求方法</span><select v-model="webhook.method"><option>POST</option><option>PUT</option><option>GET</option></select></label>
            <label class="field"><span>正文类型</span><select v-model="webhook.bodyType"><option value="json">JSON</option><option value="text">Text</option></select></label>
            <label class="field span-2"><span>自定义请求头（JSON）</span><textarea v-model="webhook.headers" rows="3" placeholder='{"Authorization":"Bearer ..."}'></textarea></label>
            <label class="field span-2"><span>正文模板（留空使用内置格式）</span><textarea v-model="webhook.bodyTemplate" rows="8" :placeholder="webhookPlaceholder"></textarea><small v-pre>变量：{{title}} {{text}} {{movie}} {{year}} {{genres}} {{rating}} {{fields}}</small></label>
          </div>
          <div class="action-row"><button class="primary" :disabled="busy === 'webhook'" @click="saveWebhook">{{ busy === 'webhook' ? '保存中…' : '保存 Webhook' }}</button><button class="ghost" :disabled="busy === 'webhook-test' || !webhook.url" @click="testWebhook">{{ busy === 'webhook-test' ? '发送中…' : '发送测试' }}</button></div>
        </div>
      </template>

      <template v-if="tab === 'schedules'">
        <div class="section-title"><div><span class="eyebrow">BUILT-IN CRON</span><h1>自动推送</h1><p>应用内置调度器，无需在宿主机维护 crontab。</p></div></div>
        <div class="schedule-grid">
          <article class="rule-card" v-for="rule in schedules" :key="rule.id" :class="{ off: !rule.enabled }"><div class="rule-head"><span class="rule-icon">◷</span><span class="status-dot on" v-if="rule.enabled">启用</span><span class="status-dot" v-else>停用</span></div><h3>{{ rule.name }}</h3><p>{{ ruleSummary(rule) }}</p><small v-if="rule.lastRunAt">上次：{{ new Date(rule.lastRunAt).toLocaleString() }}</small><div class="rule-actions"><button class="text-btn" @click="runRule(rule.id)">立即运行</button><button class="text-btn" @click="editRule(rule)">编辑</button><button class="text-btn danger-text" @click="removeRule(rule.id)">删除</button></div></article>
          <button class="rule-card add-rule" @click="editRule()"><span>＋</span><b>新建推送规则</b><small>每日、每周或每月</small></button>
        </div>
        <div class="panel rule-editor">
          <div class="panel-title-row"><h3>{{ editing ? '编辑规则' : '新建规则' }}</h3><code>{{ cronPreview }}</code></div>
          <div class="field-grid">
            <label class="field"><span>规则名称</span><input v-model="scheduleForm.name" /></label>
            <label class="field"><span>时区</span><input v-model="scheduleForm.timezone" placeholder="Asia/Shanghai" /></label>
            <label class="field"><span>周期</span><select v-model="scheduleForm.kind"><option value="daily">每日</option><option value="weekly">每周</option><option value="monthly">每月</option></select></label>
            <label class="field"><span>推送时间</span><input v-model="scheduleForm.time" type="time" /></label>
            <label v-if="scheduleForm.kind === 'weekly'" class="field"><span>星期</span><select v-model.number="scheduleForm.weekday"><option v-for="(day, index) in weekdays" :key="day" :value="index">{{ day }}</option></select></label>
            <label v-if="scheduleForm.kind === 'monthly'" class="field"><span>每月日期</span><input v-model.number="scheduleForm.dayOfMonth" type="number" min="1" max="28" /></label>
          </div>
          <label class="check-row"><input v-model="scheduleForm.enabled" type="checkbox" /><span>启用这条规则</span></label>
          <FilterPanel v-model="scheduleForm.filters" :options="options" compact />
          <div class="action-row"><button class="primary" :disabled="busy === 'schedule'" @click="saveRule">{{ busy === 'schedule' ? '保存中…' : editing ? '保存修改' : '创建规则' }}</button><button v-if="editing" class="ghost" @click="editRule()">取消编辑</button></div>
        </div>
      </template>

      <template v-if="tab === 'history'">
        <div class="section-title"><div><span class="eyebrow">AUDIT TRAIL</span><h1>运行记录</h1><p>最近 100 条随机与 Webhook 结果。</p></div><button class="ghost compact" @click="refreshHistory">刷新</button></div>
        <div class="panel"><h3>最近选片</h3><div class="table-wrap"><table><thead><tr><th>时间</th><th>影片</th><th>来源</th></tr></thead><tbody><tr v-for="row in history.random" :key="row.id"><td>{{ new Date(row.created_at).toLocaleString() }}</td><td>{{ row.title }} <small>{{ row.year }}</small></td><td>{{ row.source }}</td></tr></tbody></table></div></div>
        <div class="panel"><h3>Webhook</h3><div class="table-wrap"><table><thead><tr><th>时间</th><th>事件</th><th>状态</th><th>错误</th></tr></thead><tbody><tr v-for="row in history.webhooks" :key="row.id"><td>{{ new Date(row.created_at).toLocaleString() }}</td><td>{{ row.event }}</td><td><span :class="row.ok ? 'ok-text' : 'danger-text'">{{ row.ok ? `HTTP ${row.status_code}` : '失败' }}</span></td><td>{{ row.error }}</td></tr></tbody></table></div></div>
      </template>
    </section>
    <div v-if="message" class="toast" :class="{ bad }">{{ message }}</div>
  </div>
</template>
