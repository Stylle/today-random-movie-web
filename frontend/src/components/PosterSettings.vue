<script setup>
import { t } from '../i18n'
defineProps(['settings','sortedProviders','busy','posterTest'])
const emit=defineEmits(['toggle','move','save','test','refresh'])
const toggleProvider = (...args) => emit('toggle',...args)
const moveProvider = (...args) => emit('move',...args)
const savePosterSettings = (...args) => emit('save',...args)
const testPosterSettings = (...args) => emit('test',...args)
const refreshPosters = (...args) => emit('refresh',...args)
</script>
<template>
<div class="panel poster-settings">
          <h3>{{ t('影视信息与封面接口') }}</h3>
          <div class="provider-order"><div class="provider-row" v-for="provider in sortedProviders" :key="provider.id">
           <span class="provider-rank">{{ settings.priorities[provider.id] || '—' }}</span><strong>{{ t(provider.name) }}</strong>
           <label class="check-row"><input type="checkbox" :checked="settings.priorities[provider.id]>0" :disabled="provider.id==='placeholder'" @change="toggleProvider(provider.id,$event.target.checked)" />{{ t(settings.priorities[provider.id]>0?'已启用':'已停用（0）') }}</label>
           <button class="ghost" :disabled="!settings.priorities[provider.id]||settings.priorities[provider.id]===1||provider.id==='placeholder'" @click="moveProvider(provider.id,-1)" :aria-label="'提高'+provider.name+'优先级'">↑</button><button class="ghost" :disabled="!settings.priorities[provider.id]||settings.priorities[provider.id]>=settings.priorities.placeholder-1||provider.id==='placeholder'" @click="moveProvider(provider.id,1)" :aria-label="'降低'+provider.name+'优先级'">↓</button>
          </div></div>
          <p class="muted small">使用箭头调整顺序，取消勾选即停用（优先级 0）。占位图始终作为最后兜底。已有缓存为只读，不从飞牛下载。</p>
          <p class="muted">{{ t('影片列表来自飞牛；图片由当前设备按已保存的数据源顺序加载，已有缓存仅作回退，不从飞牛下载图片。') }}</p>

          <p class="muted small">{{ t('豆瓣公开入口可能限制访问，无法查询时会继续使用 TMDB；不需要填写豆瓣密钥。') }}</p>
          <label class="field"><span>TMDB API Key / API Read Access Token</span><input v-model="settings.tmdbKey" type="password" autocomplete="new-password" :placeholder="settings.hasTmdbKey ? t('已保存；留空不修改') : t('填写 TMDB 密钥以启用备用封面查询')" /></label>
          <small>{{ t('密钥仅在服务端加密保存，不发送给浏览器或图片站。') }} <a href="https://www.themoviedb.org/settings/api" target="_blank" rel="noopener noreferrer">{{ t('获取 TMDB 密钥') }}</a></small>
          <p v-if="!settings.hasTmdbKey" class="muted small">{{ t('TMDB 尚未配置，将跳过该来源；其他已配置来源和缓存仍可使用。') }}</p>
          <label v-if="settings.hasTmdbKey" class="check-row"><input v-model="settings.clearTmdbKey" type="checkbox" /><span>{{ t('清除已保存的 TMDB 密钥') }}</span></label>
          <label class="field"><span>豆影 DoubanInfo API Key</span><input v-model="settings.douyingKey" type="password" autocomplete="new-password" :placeholder="settings.hasDouyingKey ? '已保存；留空不修改' : '填写豆影 API Key'" /></label>
          <small>豆影免费额度：10 次/分钟、200 次/天。应用持久化记录额度，超限跳过并使用下一个来源。<a href="https://doubaninfo.com/api/api_usage.php" target="_blank" rel="noopener noreferrer">官方接口说明与申请 Key</a></small>
          <label class="check-row"><input v-model="settings.clearDouyingKey" type="checkbox" /><span>清除已保存的豆影密钥</span></label>
          <div class="action-row"><button class="primary" :disabled="!!busy" @click="savePosterSettings">{{ t('保存封面设置') }}</button><button class="ghost" :disabled="!!busy" @click="testPosterSettings">{{ t('检测已保存的来源') }}</button><button class="ghost" :disabled="!!busy" @click="refreshPosters">{{ t('重新查询封面') }}</button></div>
          <p v-if="posterTest" role="status">{{ posterTest }}</p>
        </div>
</template>
