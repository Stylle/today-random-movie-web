<script setup>
import MoviePoster from '../components/MoviePoster.vue'
import { t } from "./../i18n";
import { computed, onMounted, ref } from 'vue'
import { api, jsonBody } from '../api.js'
import { store } from '../store.js'
import FilterPanel from '../components/FilterPanel.vue'

const options = ref({ libraries: [], genres: [], years: [], countries: [] })
const filters = ref({ libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: true })
const defaultFilters = ref(null)
const movie = ref(null)
const history = ref([])
const cursor = ref(-1)
const busy = ref(false)
const error = ref('')
const detailOpen = ref(false)

const metadata = computed(() => movie.value ? [movie.value.year, movie.value.library, movie.value.duration].filter(Boolean).join(' · ') : '')

async function pick() {
  if (busy.value) return
  busy.value = true
  error.value = ''
  try {
    const result = await api('/api/random', { method: 'POST', body: jsonBody({ filters: filters.value }) })
    movie.value = result.movie
    history.value = history.value.slice(0, cursor.value + 1)
    history.value.push(result.movie)
    cursor.value = history.value.length - 1
  } catch (e) { error.value = e.message } finally { busy.value = false }
}

function previous() {
  if (cursor.value <= 0) return
  cursor.value--
  movie.value = history.value[cursor.value]
}

async function play() {
  if (!movie.value) return
  const tab = window.open('', '_blank')
  try {
    const result = await api(`/api/play?id=${encodeURIComponent(movie.value.id)}`)
    if (tab) tab.location = result.url
    else location.href = result.url
  } catch (e) {
    if (tab) tab.close()
    error.value = e.status === 401 ? '播放地址仅对管理员开放，请先登录。' : e.message
  }
}

onMounted(async () => {
  try {
    const [opts, defaults] = await Promise.all([api('/api/movies/options'), api('/api/random/defaults')])
    options.value = opts
    filters.value = defaults.filters
    defaultFilters.value = JSON.parse(JSON.stringify(defaults.filters))
    const available = options.value.libraries.reduce((sum, item) => sum + Number(item.count || 0), 0)
    if (available > 0) await pick()
  } catch (e) { error.value = e.message }
})
</script>

<template>
  <div class="home-view">
    <section class="intro-row">
      <div><span class="eyebrow">TONIGHT'S PICK</span><h1>{{ t('今日想看') }}</h1><p>{{ t('按偏好随机挑选，或在影片库中搜索。') }}</p></div>
      <div v-if="store.config.movieCount > 0" class="catalog-stat"><strong>{{ store.config.movieCount }}</strong><span>{{ t('部影片已就绪') }}</span></div>
    </section>

    <div class="picker-layout">
      <FilterPanel v-model="filters" :options="options" :reset-value="defaultFilters" />

      <section class="movie-stage" :class="{ empty: !movie }">
        <template v-if="movie">
          <div class="poster-wrap"><MoviePoster :movie="movie" @metadata="data => { if (!movie.overview) movie.overview=data.overview }" /><span v-if="movie.watched" class="watched-badge">{{ t('已看过') }}</span></div>
          <div class="movie-copy">
            <span class="pick-label">TODAY'S MOVIE</span>
            <h2>{{ movie.title }}</h2>
            <p class="movie-meta">{{ metadata }}</p>
            <div class="tags"><span v-for="tag in movie.genres" :key="tag">{{ tag }}</span><span v-if="movie.rating" class="rating">★ {{ Number(movie.rating).toFixed(1) }}</span></div>
            <p class="overview" :class="{ expanded: detailOpen }">{{ movie.overview || t('飞牛暂未提供这部影片的简介。或许，未知正是今晚最好的开场。') }}</p>
            <div class="hero-actions">
              <button class="primary large" @click="play">{{ t('▶ 开始播放') }}</button>
              <button class="ghost large" :disabled="busy" @click="pick">{{ busy ? t('正在挑选…') : t('↻ 换一部') }}</button>
              <button class="icon-btn history-btn" :disabled="cursor <= 0" @click="previous" :title="t('上一部')">←</button>
            </div>
            <button class="text-btn detail-link" @click="detailOpen = !detailOpen">{{ detailOpen ? t('收起详情') : t('查看完整信息') }}</button>
            <div v-if="detailOpen" class="detail-box"><b>{{ t('媒体库') }}</b><span>{{ movie.library }}</span><b>{{ t('地区') }}</b><span>{{ movie.countries?.join(' / ') || t('未知') }}</span></div>
          </div>
        </template>
        <div v-else class="empty-state">
          <div class="empty-reel">▶</div>
          <h2>{{ t('媒体库还没有准备好') }}</h2>
          <p v-if="store.user">{{ t('前往管理设置连接飞牛，然后执行第一次同步。') }}</p>
          <p v-else>{{ t('管理员完成飞牛连接与同步后，这里就会出现今晚的影片。') }}</p>
          <a v-if="store.user" href="#/admin" class="button-link primary">{{ t('去连接飞牛') }}</a>
        </div>
      </section>
    </div>
    <p class="inline-error" v-if="error">{{ error }}</p>
  </div>
</template>
