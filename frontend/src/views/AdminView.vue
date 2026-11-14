<script setup>
import '../draft-test.js'
import { applySyncMetadata, currentSyncError } from '../sync-status.js'
import BotBindingPanel from '../components/BotBindingPanel.vue'
import BackupPanel from '../components/BackupPanel.vue'
import PosterSettings from '../components/PosterSettings.vue'
import UiIcon from '../components/UiIcon.vue';
import { t } from "./../i18n";
import { computed, onUnmounted, ref, watch } from 'vue'
import { api, jsonBody } from '../api.js'
import { store, loadConfig } from '../store.js'
import FilterPanel from '../components/FilterPanel.vue'

const tabs = [
  { id: 'connection', label: '连接设置', icon: '⌁' },
 { id: 'sources', label: '数据来源', icon: '⌁' },
  { id: 'webhook', label: '回调通知', icon: '↗' },
  { id: 'telegram', label: '电报助手', icon: '✈' },
  { id: 'schedules', label: '自动推送', icon: '◷' },
  { id: 'history', label: '运行记录', icon: '≡' },
 { id: 'backup', label: '备份恢复', icon: '↓' },
]
const props = defineProps({ section: { type: String, default: 'connection' } })
const tab = computed(() => props.section)
const settings = ref(null)
const webhook = ref({ url: '', method: 'POST', bodyType: 'json', headers: '', bodyTemplate: '' })
const schedules = ref([])
const options = ref({ libraries: [], genres: [], years: [], countries: [] })
const sync = ref({ running: false })
const syncSchedule = ref({ enabled: false, kind: 'daily', time: '03:00', timezone: 'Asia/Shanghai', weekday: 1, lastRunAt: '' })
const history = ref({ random: [], webhooks: [] })
const defaultFilters = ref({ libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: true })
const telegram = ref({ enabled: false, chatId: '', token: '', hasToken: false, revision: '', lastError: '', lastReceived: '', customRange: false })
const telegramLive = ref(false), bindingCommand = ref(''), bindingBot = ref('')
const bindingDirty = computed(() => !!telegram.value.token.trim() || telegram.value.enabled !== telegramLive.value)
let bindingExpiry = 0
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
  settings.value = { ...s, fnPassword: '', tmdbKey: '', clearTmdbKey: false, douyingKey: '', clearDouyingKey: false }
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
  if (busy.value) return
  busy.value = label
  try { await task(); if (successMessage) flash(successMessage) } catch (e) { flash(e.message, true) } finally { busy.value = '' }
}

const fnDraft = () => ({ server: settings.value.fnServer, username: settings.value.fnUsername, password: settings.value.fnPassword, insecureTls: settings.value.fnInsecureTls })
const saveFn = () => run('fn', async () => {
  await api('/api/admin/fnos', { method: 'PUT', body: jsonBody({ server: settings.value.fnServer, username: settings.value.fnUsername, password: settings.value.fnPassword, insecureTls: settings.value.fnInsecureTls }) })
  settings.value.fnPassword = ''
  settings.value.fnHasPassword = true
})

const testFn = () => run('test', async () => {
  const result = await window.IntegrationDraft.run(fnDraft, draft => api('/api/admin/fnos/test', { method: 'POST', body: jsonBody(draft) }))
  flash(`连接成功，读取到 ${result.libraries} 个电影媒体库（当前表单，未保存）`)
}, null)

const providerDefinitions=[{id:'douban',name:'豆瓣公开信息'},{id:'tmdb',name:'TMDB'},{id:'douying',name:'豆影 DoubanInfo'},{id:'cache',name:'已有缓存'},{id:'placeholder',name:'占位图'}]
const sortedProviders=computed(()=>[...providerDefinitions].sort((a,b)=>(settings.value?.priorities[a.id]||99)-(settings.value?.priorities[b.id]||99)))
function normalizeProviders(order){const priorities={douban:0,tmdb:0,douying:0,cache:0,placeholder:0};order.filter(x=>x!=='placeholder').concat(['placeholder']).forEach((id,index)=>priorities[id]=index+1);settings.value.priorities=priorities}
function toggleProvider(id,on){const order=sortedProviders.value.filter(p=>settings.value.priorities[p.id]>0&&p.id!==id).map(p=>p.id);if(on)order.splice(Math.max(0,order.indexOf('placeholder')),0,id);normalizeProviders(order)}
function moveProvider(id,direction){const order=sortedProviders.value.filter(p=>settings.value.priorities[p.id]>0).map(p=>p.id),index=order.indexOf(id),next=index+direction;if(index<0||next<0||next>=order.length||order[next]==='placeholder')return;[order[index],order[next]]=[order[next],order[index]];normalizeProviders(order)}
const posterTest = ref('')
const savePosterSettings = () => run('poster-save', async () => {
 const result = await api('/api/admin/poster-settings', { method: 'PUT', body: jsonBody({ tmdbKey: settings.value.tmdbKey, clearKey: settings.value.clearTmdbKey, doubanEnabled: settings.value.priorities.douban > 0, priorities: settings.value.priorities, douyingKey: settings.value.douyingKey, clearDouyingKey: settings.value.clearDouyingKey }) })
 settings.value.hasTmdbKey = result.hasTmdbKey; settings.value.hasDouyingKey=result.hasDouyingKey; settings.value.tmdbKey = ''; settings.value.clearTmdbKey = false; settings.value.douyingKey = ''; settings.value.clearDouyingKey = false; posterTest.value = ''
}, '数据源设置已保存')
const providerStatus = value => t(({ ok: '可用', unavailable: '连接失败或被限制', unauthorized: '认证失败或访问被限制', missing_key: '尚未配置密钥', not_found: '未匹配到影片', rate_limited: '已达调用额度，稍后重试', disabled: '已关闭' })[value] || '查询失败')
const testPosterSettings = () => run('poster-test', async () => {
 const result = await api('/api/admin/posters/test', { method: 'POST' })
 posterTest.value = `豆瓣：${providerStatus(result.douban)}；TMDB：${providerStatus(result.tmdb)}；豆影：${providerStatus(result.douying)}`
}, '封面来源检测完成')
const refreshPosters = () => run('poster-refresh', () => api('/api/admin/posters/refresh', { method: 'POST' }), '已重置封面查询缓存，返回影片页面后自动重新查询')

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
        const latest = await api('/api/admin/settings')
        applySyncMetadata(settings.value, latest)
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
const testWebhook = () => run('webhook-test', async () => { await window.IntegrationDraft.run(() => webhook.value, draft => api('/api/admin/webhook/test', { method: 'POST', body: jsonBody(draft) })); flash('当前表单测试消息已发送，配置未保存') }, null)
const saveTelegram = (enabled = true) => run('telegram', async () => {
 await api('/api/admin/telegram',{method:'PUT',body:jsonBody({enabled,token:telegram.value.token.trim(),revision:telegram.value.revision,bindFlow:true})})
 telegram.value={...await api('/api/admin/telegram'),token:''};telegramLive.value=!!telegram.value.enabled;bindingCommand.value=''
})
const generateBinding = () => run('telegram-bind',async()=>{const result=await api('/api/admin/telegram/pair',{method:'POST'});bindingCommand.value=result.command;bindingBot.value=result.bot;bindingExpiry=Date.now()+result.expiresIn*1000},null)
const unbindTelegram = () => run('telegram-unbind',async()=>{await api('/api/admin/telegram/unbind',{method:'POST'});telegram.value={...await api('/api/admin/telegram'),token:''};bindingCommand.value=''},'已解除绑定')
const saveDefaultFilters = () => run('default-filters', async () => {
  const result = await api('/api/admin/random-defaults', { method: 'PUT', body: jsonBody({ filters: defaultFilters.value }) })
  defaultFilters.value = result.filters
})
const telegramPoll = setInterval(async () => {
  if (tab.value !== 'telegram' || !store.user || document.hidden) return
  try {
    const result = await api('/api/admin/telegram')
    telegramLive.value = !!result.enabled
    if(result.revision!==telegram.value.revision || (bindingExpiry && Date.now()>=bindingExpiry)) bindingCommand.value=''
    Object.assign(telegram.value, { revision:result.revision, lastError: result.lastError, lastReceived: result.lastReceived, customRange: result.customRange, bound:result.bound, chatId:result.chatId })
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
  <div v-if="!store.authChecked" class="loading-card">{{ t('正在确认登录状态…') }}</div>
  <div v-else-if="!store.user" class="empty-state standalone"><div class="empty-reel">◇</div><h2>{{ t('这里需要管理员权限') }}</h2><p>{{ t('登录后才能连接飞牛和配置推送。') }}</p><a href="#/login" class="button-link primary">{{ t('去登录') }}</a></div>
  <div v-else class="admin-layout">


    <section class="admin-content" v-if="settings">
 <BackupPanel v-if="tab === 'backup'" base="/api/admin" />
      <PosterSettings v-if="tab === 'sources'" :settings="settings" :sorted-providers="sortedProviders" :busy="busy" :poster-test="posterTest" @toggle="toggleProvider" @move="moveProvider" @save="savePosterSettings" @test="testPosterSettings" @refresh="refreshPosters" />
      <template v-if="tab === 'connection'">
        <div class="section-title"><div><span class="eyebrow">FNOS / FNTV</span><h1>{{ t('连接与同步') }}</h1><p>{{ t('凭据只在服务端加密保存，浏览器不会接触飞牛会话。') }}</p></div></div>
        <div class="panel">
          <h3>{{ t('飞牛连接') }}</h3>
          <div class="field-grid">
            <label class="field span-2"><span>{{ t('飞牛影视地址') }}</span><input v-model="settings.fnServer" :placeholder="t('http://192.168.1.20:32587/v 或飞牛内网穿透地址')" /><small>{{ t('支持局域网地址、HTTPS 域名和飞牛内网穿透地址。') }}</small></label>
            <label class="field"><span>{{ t('账号') }}</span><input v-model="settings.fnUsername" autocomplete="off" /></label>
            <label class="field"><span>{{ t('密码') }}</span><input v-model="settings.fnPassword" type="password" :placeholder="settings.fnHasPassword ? t('已保存；留空不修改') : t('飞牛密码')" autocomplete="new-password" /></label>
          </div>
          <label class="check-row warning-check"><input v-model="settings.fnInsecureTls" type="checkbox" /><span>{{ t('允许自签名 HTTPS 证书（仅可信内网 NAS）') }}</span></label>
          <p class="muted">{{ t('测试使用当前表单，不会保存或启用配置；密钥留空沿用已保存的密钥。') }}</p><div class="action-row form-actions-sticky"><button class="primary" :disabled="!!busy" @click="saveFn">{{ busy === 'fn' ? t('保存中…') : t('保存连接') }}</button><button class="ghost" :disabled="!!busy" @click="testFn">{{ busy === 'test' ? t('测试中…') : t('测试连接') }}</button></div>
        </div>
        <div class="panel sync-panel">
          <div><h3>{{ t('媒体库索引') }}</h3><p>{{ t('同步影片信息；缺失的外部封面地址会在访问影片时自动查询，无需重新同步影片列表。') }}</p><small v-if="settings.lastSyncAt">{{ t('上次尝试：') }}{{ new Date(settings.lastSyncAt).toLocaleString() }}</small></div>
          <div class="sync-action"><button class="primary" :disabled="sync.running" @click="startSync">{{ sync.running ? t('同步进行中…') : t('立即同步') }}</button><button class="ghost" @click="purgePosters">{{ t('清理封面缓存') }}</button></div>
          <div class="progress" v-if="sync.running"><div :style="{ width: sync.total ? `${sync.done / sync.total * 100}%` : '8%' }"></div></div>
          <p class="sync-state" v-if="sync.stage">{{ sync.stage }}<template v-if="sync.total"> · {{ sync.done }} / {{ sync.total }}</template></p>
          <p class="inline-error" v-if="currentSyncError(sync, settings)">{{ currentSyncError(sync, settings) }}</p>
        </div>
        <div class="panel">
          <div class="panel-title-row"><div><h3>{{ t('定时同步') }}</h3><small>{{ t('自动执行完整同步，更新新增影片、简介和已观看状态。') }}</small></div><span class="status-dot" :class="{ on: syncSchedule.enabled }">{{ syncSchedule.enabled ? t('已启用') : t('未启用') }}</span></div>
          <div class="field-grid">
            <label class="field"><span>{{ t('周期') }}</span><select v-model="syncSchedule.kind"><option value="daily">{{ t('每日') }}</option><option value="weekly">{{ t('每周') }}</option></select></label>
            <label class="field"><span>{{ t('同步时间') }}</span><input v-model="syncSchedule.time" type="time" /></label>
            <label v-if="syncSchedule.kind === 'weekly'" class="field"><span>{{ t('星期') }}</span><select v-model.number="syncSchedule.weekday"><option v-for="(day, index) in weekdays" :key="day" :value="index">{{ day }}</option></select></label>
            <label class="field"><span>{{ t('时区') }}</span><input v-model="syncSchedule.timezone" placeholder="Asia/Shanghai" /></label>
          </div>
          <label class="check-row"><input v-model="syncSchedule.enabled" type="checkbox" /><span>{{ t('启用定时同步') }}</span></label>
          <small v-if="syncSchedule.lastRunAt" class="schedule-last-run">{{ t('上次自动触发：') }}{{ new Date(syncSchedule.lastRunAt).toLocaleString() }}</small>
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="!!busy" @click="saveSyncSchedule">{{ busy === 'sync-schedule' ? t('保存中…') : t('保存同步规则') }}</button></div>
        </div>
      </template>

      <template v-if="tab === 'webhook'">
        <div class="section-title"><div><span class="eyebrow">DELIVERY</span><h1>Webhook</h1><p>{{ t('兼容企业微信、钉钉、飞书、Telegram Bot 与通用 HTTP 接口。') }}</p></div></div>
        <div class="panel">
          <div class="field-grid">
            <label class="field span-2"><span>Webhook URL</span><input v-model="webhook.url" placeholder="https://example.com/webhook" /></label>
            <label class="field"><span>{{ t('请求方法') }}</span><select v-model="webhook.method"><option>POST</option><option>PUT</option><option>GET</option></select></label>
            <label class="field"><span>{{ t('正文类型') }}</span><select v-model="webhook.bodyType"><option value="json">JSON</option><option value="text">Text</option></select></label>
            <label class="field span-2"><span>{{ t('自定义请求头（JSON）') }}</span><textarea v-model="webhook.headers" rows="3" placeholder='{"Authorization":"Bearer ..."}'></textarea></label>
            <label class="field span-2"><span>{{ t('正文模板（留空使用内置格式）') }}</span><textarea v-model="webhook.bodyTemplate" rows="8" :placeholder="webhookPlaceholder"></textarea><small v-pre>{{ t('变量：{{title}} {{text}} {{movie}} {{year}} {{genres}} {{rating}} {{fields}}') }}</small></label>
          </div>
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="!!busy" @click="saveWebhook">{{ busy === 'webhook' ? t('保存中…') : t('保存 Webhook') }}</button><button class="ghost" :disabled="!!busy || !webhook.url" @click="testWebhook">{{ busy === 'webhook-test' ? t('发送中…') : t('发送测试') }}</button></div>
        </div>
      </template>

      <template v-if="tab === 'telegram'">
        <div class="section-title"><div><span class="eyebrow">TELEGRAM</span><h1>{{ t('机器人选片') }}</h1><p>{{ t('发送 getmovie 随机一部，发送 menu 设置范围。影片结果仅保留再随机与打开飞牛两个按钮。') }}</p></div></div>
        <BotBindingPanel v-model="telegram.token" :configured="telegram.hasToken" :enabled="telegram.enabled" @update:enabled="telegram.enabled=$event" :bound="telegram.bound" :dirty="bindingDirty" :busy="!!busy" :bot="bindingBot" :command="bindingCommand" :error="telegram.lastError" @save="saveTelegram(telegram.enabled)" @generate="generateBinding" @unbind="unbindTelegram" @toggle="saveTelegram(!telegram.enabled)" />
      </template>

      <template v-if="tab === 'schedules'">
        <div class="section-title"><div><span class="eyebrow">BUILT-IN CRON</span><h1>{{ t('自动推送') }}</h1><p>{{ t('应用内置调度器，无需在宿主机维护 crontab。') }}</p></div></div>
        <div class="schedule-grid">
          <article class="rule-card" v-for="rule in schedules" :key="rule.id" :class="{ off: !rule.enabled }"><div class="rule-head"><span class="rule-icon">◷</span><span class="status-dot on" v-if="rule.enabled">{{ t('启用') }}</span><span class="status-dot" v-else>{{ t('停用') }}</span></div><h3>{{ rule.name }}</h3><p>{{ ruleSummary(rule) }}</p><p>{{ channelLabel(rule.notifyChannel) }}</p><small v-if="rule.lastRunAt">{{ t('上次：') }}{{ new Date(rule.lastRunAt).toLocaleString() }}</small><div class="rule-actions"><button class="text-btn" :disabled="!!busy" @click="runRule(rule.id)">{{ t('立即运行') }}</button><button class="text-btn" @click="editRule(rule)">{{ t('编辑') }}</button><button class="text-btn danger-text" @click="removeRule(rule.id)">{{ t('删除') }}</button></div></article>
          <button class="rule-card add-rule" @click="editRule()"><span>＋</span><b>{{ t('新建推送规则') }}</b><small>{{ t('每日、每周或每月') }}</small></button>
        </div>
        <div class="panel rule-editor">
          <div class="panel-title-row"><h3>{{ editing ? t('编辑规则') : t('新建规则') }}</h3><code>{{ cronPreview }}</code></div>
          <div class="field-grid">
            <label class="field"><span>{{ t('规则名称') }}</span><input v-model="scheduleForm.name" /></label>
            <label class="field"><span>{{ t('时区') }}</span><input v-model="scheduleForm.timezone" placeholder="Asia/Shanghai" /></label>
            <label class="field"><span>{{ t('周期') }}</span><select v-model="scheduleForm.kind"><option value="daily">{{ t('每日') }}</option><option value="weekly">{{ t('每周') }}</option><option value="monthly">{{ t('每月') }}</option></select></label>
            <label class="field"><span>{{ t('推送时间') }}</span><input v-model="scheduleForm.time" type="time" /></label>
            <label class="field span-2"><span>{{ t('通知渠道') }}</span><select v-model="scheduleForm.notifyChannel"><option value="webhook">Webhook</option><option value="telegram">{{ t('Telegram 机器人') }}</option><option value="both">{{ t('Webhook + Telegram（同时发送）') }}</option></select><small>{{ t('机器人通知发送到「Telegram 机器人」中已保存并启用的绑定私聊；无需再填写 Token。') }}</small></label>
            <label v-if="scheduleForm.kind === 'weekly'" class="field"><span>{{ t('星期') }}</span><select v-model.number="scheduleForm.weekday"><option v-for="(day, index) in weekdays" :key="day" :value="index">{{ day }}</option></select></label>
            <label v-if="scheduleForm.kind === 'monthly'" class="field"><span>{{ t('每月日期') }}</span><input v-model.number="scheduleForm.dayOfMonth" type="number" min="1" max="28" /></label>
          </div>
          <p v-if="scheduleForm.notifyChannel !== 'webhook' && !telegramLive" class="inline-error">{{ t('机器人尚未启用。可以先保存规则，但运行前请配置并启用 Telegram 机器人。') }}</p>
          <label class="check-row"><input v-model="scheduleForm.enabled" type="checkbox" /><span>{{ t('启用这条规则') }}</span></label>
          <FilterPanel v-model="scheduleForm.filters" :options="options" compact />
          <div class="action-row form-actions-sticky"><button class="primary" :disabled="!!busy" @click="saveRule">{{ busy === 'schedule' ? t('保存中…') : editing ? t('保存修改') : t('创建规则') }}</button><button v-if="editing" class="ghost" @click="editRule()">{{ t('取消编辑') }}</button></div>
        </div>
      </template>

      <template v-if="tab === 'history'">
        <div class="section-title"><div><span class="eyebrow">AUDIT TRAIL</span><h1>{{ t('运行记录') }}</h1><p>{{ t('最近 100 条随机与通知结果；双渠道分别记录，部分失败请核对后再运行。') }}</p></div><button class="ghost compact" @click="refreshHistory">{{ t('刷新') }}</button></div>
        <div class="panel"><h3>{{ t('最近选片') }}</h3><div class="table-wrap mobile-records"><table><thead><tr><th>{{ t('时间') }}</th><th>{{ t('影片') }}</th><th>{{ t('来源') }}</th></tr></thead><tbody><tr v-for="row in history.random" :key="row.id"><td :data-label="t('时间')">{{ new Date(row.created_at).toLocaleString() }}</td><td :data-label="t('影片')">{{ row.title }} <small>{{ row.year }}</small></td><td :data-label="t('来源')">{{ row.source }}</td></tr></tbody></table></div></div>
        <div class="panel"><h3>{{ t('通知记录') }}</h3><div class="table-wrap mobile-records"><table><thead><tr><th>{{ t('时间') }}</th><th>{{ t('渠道') }}</th><th>{{ t('事件 / 规则') }}</th><th>{{ t('状态') }}</th><th>{{ t('错误') }}</th></tr></thead><tbody><tr v-for="row in (history.notifications || history.webhooks)" :key="row.id"><td :data-label="t('时间')">{{ new Date(row.created_at).toLocaleString() }}</td><td :data-label="t('渠道')">{{ channelLabel(row.channel) }}</td><td :data-label="t('事件 / 规则')">{{ row.event === 'test' ? t('测试通知') : row.event === 'movie_pick' ? t('影片推荐') : row.event }}<small v-if="row.schedule_id"> · {{ row.rule_name || row.schedule_id }}</small></td><td :data-label="t('状态')"><span :class="row.ok ? 'ok-text' : 'danger-text'">{{ row.ok ? (row.channel === 'telegram' ? t('已发送') : `HTTP ${row.status_code}`) : t('失败') }}</span></td><td :data-label="t('错误')">{{ row.error }}</td></tr></tbody></table></div></div>
      </template>
    </section>
    <div v-if="message" role="status" aria-live="polite" class="toast" :class="{ bad }">{{ message }}</div>
  </div>
</template>
