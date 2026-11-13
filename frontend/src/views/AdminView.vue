<script setup>
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, jsonBody } from '../api.js'
import { store, loadConfig } from '../store.js'
import FilterPanel from '../components/FilterPanel.vue'

const tabs = [
  { id: 'connection', label: '连接与同步', icon: '⌁' },
  { id: 'webhook', label: 'Webhook', icon: '↗' },
  { id: 'telegram', label: 'Telegram 机器人', icon: '✈' },
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
const defaultFilters = ref({ libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: true })
const telegram = ref({ enabled: false, chatId: '', token: '', hasToken: false, revision: '', lastError: '', lastReceived: '', customRange: false })
const telegramLive = ref(false)
const telegramChats = ref([])
const telegramNotice = ref('')
const message = ref('')
const bad = ref(false)
const busy = ref('')
let poll

const freshSchedule = () => ({ name: '每日推荐', enabled: true, kind: 'daily', time: '20:00', timezone: 'Asia/Shanghai', weekday: 5, dayOfMonth: 1, notifyChannel: 'webhook', filters: JSON.parse(JSON.stringify(defaultFilters.value)) })
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
  const [s, w, rules, opts, job, syncRule, runs, tg, defaults] = await Promise.all([
    api('/api/admin/settings'), api('/api/admin/webhook'), api('/api/admin/schedules'),
    api('/api/movies/options'), api('/api/admin/sync'), api('/api/admin/sync-schedule'), api('/api/admin/history'),
    api('/api/admin/telegram'), api('/api/random/defaults'),
  ])
  settings.value = { ...s, fnPassword: '' }
  webhook.value = w
  schedules.value = rules.items
  options.value = opts
  sync.value = job
  syncSchedule.value = syncRule
  history.value = runs
  telegram.value = { ...tg, token: '' }
  telegramLive.value = !!tg.enabled
  defaultFilters.value = defaults.filters
  if (!editing.value) scheduleForm.value.filters = JSON.parse(JSON.stringify(defaults.filters))
  if (job.running) startPoll()
}

async function run(label, task, successMessage = '已保存并生效') {
  busy.value = label
  try { await task(); flash(successMessage) } catch (e) { flash(e.message, true) } finally { busy.value = '' }
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
const saveTelegram = () => run('telegram', async () => {
  await api('/api/admin/telegram', { method: 'PUT', body: jsonBody({ enabled: telegram.value.enabled, token: telegram.value.token.trim(), chatId: telegram.value.chatId.trim(), revision: telegram.value.revision }) })
  telegram.value = { ...await api('/api/admin/telegram'), token: '' }
  telegramLive.value = !!telegram.value.enabled
})
const testTelegram = () => run('telegram-test', async () => {
  const result = await api('/api/admin/telegram/test', { method: 'POST', body: jsonBody({ token: telegram.value.token.trim() }) })
  telegramChats.value = result.chats || []
  if (telegramChats.value.length === 1) telegram.value.chatId = telegramChats.value[0]
  telegramNotice.value = telegramChats.value.length ? '连接正常，已检测到私聊 ID，请核对后保存。' : `连接正常。请向 @${result.username || '你的机器人'} 发送 /start 后重新检测；启用期间仅显示已绑定 ID。`
})
const sendTelegramTest = () => run('telegram-send-test', () => api('/api/admin/telegram/send-test', { method: 'POST' }), '测试通知已发送到绑定的私聊')
const saveDefaultFilters = () => run('default-filters', async () => {
  const result = await api('/api/admin/random-defaults', { method: 'PUT', body: jsonBody({ filters: defaultFilters.value }) })
  defaultFilters.value = result.filters
})
const telegramPoll = setInterval(async () => {
  if (tab.value !== 'telegram' || !store.user || document.hidden) return
  try {
    const result = await api('/api/admin/telegram')
    telegramLive.value = !!result.enabled
    Object.assign(telegram.value, { lastError: result.lastError, lastReceived: result.lastReceived, customRange: result.customRange })
  } catch (_) {}
}, 15000)
function editRule(rule = null) {
  editing.value = rule?.id || ''
  scheduleForm.value = rule ? { ...JSON.parse(JSON.stringify(rule)), notifyChannel: rule.notifyChannel || 'webhook' } : freshSchedule()
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
  busy.value = 'schedule-run'
  try {
    const result = await api(`/api/admin/schedules/${id}/run`, { method: 'POST' })
    flash(`已推送：${result.movie.title}`)
  } catch (e) { flash(e.message, true) }
  finally { busy.value = ''; await refreshHistory().catch(() => {}) }
}

const channelLabel = channel => ({ webhook: 'Webhook', telegram: 'Telegram 机器人', both: 'Webhook + Telegram' }[channel || 'webhook'] || '未知渠道')

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
onUnmounted(() => { clearInterval(poll); clearInterval(telegramPoll) })
</script>

<template>
  <div v-if="!store.authChecked" class="loading-card">正在确认登录状态…</div>
  <div v-else-if="!store.user" class="empty-state standalone"><div class="empty-reel">◇</div><h2>这里需要管理员权限</h2><p>登录后才能连接飞牛和配置推送。</p><a href="#/login" class="button-link primary">去登录</a></div>
  <div v-else class="admin-layout">
    <label class="admin-section-picker"><span>管理栏目</span><select v-model="tab" aria-label="管理栏目"><option v-for="item in tabs" :key="item.id" :value="item.id">{{ item.label }}</option></select></label>
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
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="busy === 'fn'" @click="saveFn">{{ busy === 'fn' ? '保存中…' : '保存连接' }}</button><button class="ghost" :disabled="busy === 'test'" @click="testFn">{{ busy === 'test' ? '测试中…' : '测试连接' }}</button></div>
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
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="busy === 'sync-schedule'" @click="saveSyncSchedule">{{ busy === 'sync-schedule' ? '保存中…' : '保存同步规则' }}</button></div>
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
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="busy === 'webhook'" @click="saveWebhook">{{ busy === 'webhook' ? '保存中…' : '保存 Webhook' }}</button><button class="ghost" :disabled="busy === 'webhook-test' || !webhook.url" @click="testWebhook">{{ busy === 'webhook-test' ? '发送中…' : '发送测试' }}</button></div>
        </div>
      </template>

      <template v-if="tab === 'telegram'">
        <div class="section-title"><div><span class="eyebrow">TELEGRAM</span><h1>机器人选片</h1><p>发送 getmovie 随机一部，发送 menu 设置范围。影片结果仅保留再随机与打开飞牛两个按钮。</p></div></div>
        <div class="panel">
          <label class="check-row"><input v-model="telegram.enabled" type="checkbox" /><span>启用 Telegram 机器人（默认关闭）</span></label>
          <div class="field-grid">
            <label class="field span-2"><span>Bot Token</span><input v-model="telegram.token" type="password" autocomplete="new-password" :placeholder="telegram.hasToken ? 'Token 已加密保存；留空保持不变' : '从 @BotFather 获取专用机器人 Token'" /></label>
            <label class="field"><span>允许的私聊 Chat ID</span><input v-model="telegram.chatId" inputmode="numeric" placeholder="先私聊机器人发送 /start，再检测" /></label>
            <label v-if="telegramChats.length" class="field"><span>检测到的私聊</span><select v-model="telegram.chatId"><option value="">请核对选择</option><option v-for="id in telegramChats" :key="id" :value="id">{{ id }}</option></select></label>
          </div>
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="busy === 'telegram'" @click="saveTelegram">{{ busy === 'telegram' ? '保存中…' : '保存机器人设置' }}</button><button class="ghost" :disabled="busy === 'telegram-test'" @click="testTelegram">检测连接 / 识别私聊 ID</button><button class="ghost" :disabled="busy === 'telegram-send-test' || !telegramLive" @click="sendTelegramTest">发送测试通知</button></div>
          <p v-if="telegramNotice" class="muted">{{ telegramNotice }}</p>
          <p class="muted">{{ telegramLive ? '已启用' : '已关闭' }} · TG 范围：{{ telegram.customRange ? '自定义（在机器人菜单中恢复默认）' : '跟随后台默认' }}<span v-if="telegram.lastReceived"> · 最近处理 {{ new Date(telegram.lastReceived).toLocaleString() }}</span></p>
          <p v-if="telegram.lastError" class="inline-error">{{ telegram.lastError }}</p>
          <p class="muted">只响应指定私聊，不开放给其他人或群聊；无需公网回调。请为本项目创建独立机器人，不与随手传等服务共用 Token。机器人聊天不是端到端加密；推荐详情和飞牛链接会交给 Telegram 保存。</p>
        </div>
        <div class="panel">
          <h3>默认随机范围</h3><p class="muted">网站首次进入、TG 默认范围和新建推送规则共用此设置；初始为全部分类、排除已观看。TG 菜单自定义不会修改这里，已有推送规则也不会被覆盖。</p>
          <FilterPanel v-model="defaultFilters" :options="options" compact />
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="busy === 'default-filters'" @click="saveDefaultFilters">保存默认范围</button></div>
        </div>
      </template>

      <template v-if="tab === 'schedules'">
        <div class="section-title"><div><span class="eyebrow">BUILT-IN CRON</span><h1>自动推送</h1><p>应用内置调度器，无需在宿主机维护 crontab。</p></div></div>
        <div class="schedule-grid">
          <article class="rule-card" v-for="rule in schedules" :key="rule.id" :class="{ off: !rule.enabled }"><div class="rule-head"><span class="rule-icon">◷</span><span class="status-dot on" v-if="rule.enabled">启用</span><span class="status-dot" v-else>停用</span></div><h3>{{ rule.name }}</h3><p>{{ ruleSummary(rule) }}</p><p>{{ channelLabel(rule.notifyChannel) }}</p><small v-if="rule.lastRunAt">上次：{{ new Date(rule.lastRunAt).toLocaleString() }}</small><div class="rule-actions"><button class="text-btn" :disabled="busy === 'schedule-run'" @click="runRule(rule.id)">立即运行</button><button class="text-btn" @click="editRule(rule)">编辑</button><button class="text-btn danger-text" @click="removeRule(rule.id)">删除</button></div></article>
          <button class="rule-card add-rule" @click="editRule()"><span>＋</span><b>新建推送规则</b><small>每日、每周或每月</small></button>
        </div>
        <div class="panel rule-editor">
          <div class="panel-title-row"><h3>{{ editing ? '编辑规则' : '新建规则' }}</h3><code>{{ cronPreview }}</code></div>
          <div class="field-grid">
            <label class="field"><span>规则名称</span><input v-model="scheduleForm.name" /></label>
            <label class="field"><span>时区</span><input v-model="scheduleForm.timezone" placeholder="Asia/Shanghai" /></label>
            <label class="field"><span>周期</span><select v-model="scheduleForm.kind"><option value="daily">每日</option><option value="weekly">每周</option><option value="monthly">每月</option></select></label>
            <label class="field"><span>推送时间</span><input v-model="scheduleForm.time" type="time" /></label>
            <label class="field span-2"><span>通知渠道</span><select v-model="scheduleForm.notifyChannel"><option value="webhook">Webhook</option><option value="telegram">Telegram 机器人</option><option value="both">Webhook + Telegram（同时发送）</option></select><small>机器人通知发送到「Telegram 机器人」中已保存并启用的绑定私聊；无需再填写 Token。</small></label>
            <label v-if="scheduleForm.kind === 'weekly'" class="field"><span>星期</span><select v-model.number="scheduleForm.weekday"><option v-for="(day, index) in weekdays" :key="day" :value="index">{{ day }}</option></select></label>
            <label v-if="scheduleForm.kind === 'monthly'" class="field"><span>每月日期</span><input v-model.number="scheduleForm.dayOfMonth" type="number" min="1" max="28" /></label>
          </div>
          <p v-if="scheduleForm.notifyChannel !== 'webhook' && !telegramLive" class="inline-error">机器人尚未启用。可以先保存规则，但运行前请配置并启用 Telegram 机器人。</p>
          <label class="check-row"><input v-model="scheduleForm.enabled" type="checkbox" /><span>启用这条规则</span></label>
          <FilterPanel v-model="scheduleForm.filters" :options="options" compact />
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="busy === 'schedule'" @click="saveRule">{{ busy === 'schedule' ? '保存中…' : editing ? '保存修改' : '创建规则' }}</button><button v-if="editing" class="ghost" @click="editRule()">取消编辑</button></div>
        </div>
      </template>

      <template v-if="tab === 'history'">
        <div class="section-title"><div><span class="eyebrow">AUDIT TRAIL</span><h1>运行记录</h1><p>最近 100 条随机与通知结果；双渠道分别记录，部分失败请核对后再运行。</p></div><button class="ghost compact" @click="refreshHistory">刷新</button></div>
        <div class="panel"><h3>最近选片</h3><div class="table-wrap mobile-records"><table><thead><tr><th>时间</th><th>影片</th><th>来源</th></tr></thead><tbody><tr v-for="row in history.random" :key="row.id"><td data-label="时间">{{ new Date(row.created_at).toLocaleString() }}</td><td data-label="影片">{{ row.title }} <small>{{ row.year }}</small></td><td data-label="来源">{{ row.source }}</td></tr></tbody></table></div></div>
        <div class="panel"><h3>通知记录</h3><div class="table-wrap mobile-records"><table><thead><tr><th>时间</th><th>渠道</th><th>事件 / 规则</th><th>状态</th><th>错误</th></tr></thead><tbody><tr v-for="row in (history.notifications || history.webhooks)" :key="row.id"><td data-label="时间">{{ new Date(row.created_at).toLocaleString() }}</td><td data-label="渠道">{{ channelLabel(row.channel) }}</td><td data-label="事件 / 规则">{{ row.event === 'test' ? '测试通知' : row.event === 'movie_pick' ? '影片推荐' : row.event }}<small v-if="row.schedule_id"> · {{ row.rule_name || row.schedule_id }}</small></td><td data-label="状态"><span :class="row.ok ? 'ok-text' : 'danger-text'">{{ row.ok ? (row.channel === 'telegram' ? '已发送' : `HTTP ${row.status_code}`) : '失败' }}</span></td><td data-label="错误">{{ row.error }}</td></tr></tbody></table></div></div>
      </template>
    </section>
    <div v-if="message" class="toast" :class="{ bad }">{{ message }}</div>
  </div>
</template>
