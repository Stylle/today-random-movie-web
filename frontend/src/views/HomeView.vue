<script setup>
import { computed, onMounted, ref } from 'vue'
import { api, jsonBody } from '../api.js'
import { store } from '../store.js'
import FilterPanel from '../components/FilterPanel.vue'

const options = ref({ libraries: [], genres: [], years: [], countries: [] })
const filters = ref({ libraries: [], genre: '', country: '', year: '', excludeWatched: true })
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
    options.value = await api('/api/movies/options')
    const available = options.value.libraries.reduce((sum, item) => sum + Number(item.count || 0), 0)
    if (available > 0) await pick()
  } catch (e) { error.value = e.message }
})
</script>

<template>
  <div class="home-view">
    <section class="intro-row">
      <div><span class="eyebrow">TONIGHT'S PICK</span><h1>别再翻了，<em>今晚看这部。</em></h1><p>从你的飞牛媒体库里，按偏好认真地随机一次。</p></div>
      <div class="catalog-stat"><strong>{{ store.config.movieCount || 0 }}</strong><span>部影片已就绪</span></div>
    </section>

    <div class="picker-layout">
      <FilterPanel v-model="filters" :options="options" />

      <section class="movie-stage" :class="{ empty: !movie }">
        <template v-if="movie">
          <div class="poster-wrap"><img :src="movie.posterUrl" :alt="movie.title" /><span v-if="movie.watched" class="watched-badge">已看过</span></div>
          <div class="movie-copy">
            <span class="pick-label">TODAY'S MOVIE</span>
            <h2>{{ movie.title }}</h2>
            <p class="movie-meta">{{ metadata }}</p>
            <div class="tags"><span v-for="tag in movie.genres" :key="tag">{{ tag }}</span><span v-if="movie.rating" class="rating">★ {{ movie.rating }}</span><span v-if="movie.metadataSource === 'tmdb'" class="source-badge">TMDB</span></div>
            <p class="overview">{{ movie.overview || '飞牛暂未提供这部影片的简介。或许，未知正是今晚最好的开场。' }}</p>
            <div class="hero-actions">
              <button class="primary large" @click="play">▶ 开始播放</button>
              <button class="ghost large" :disabled="busy" @click="pick">{{ busy ? '正在挑选…' : '↻ 换一部' }}</button>
              <button class="icon-btn history-btn" :disabled="cursor <= 0" @click="previous" title="上一部">←</button>
            </div>
            <button class="text-btn detail-link" @click="detailOpen = !detailOpen">{{ detailOpen ? '收起详情' : '查看完整信息' }}</button>
            <div v-if="detailOpen" class="detail-box"><b>媒体库</b><span>{{ movie.library }}</span><b>地区</b><span>{{ movie.countries?.join(' / ') || '未知' }}</span></div>
          </div>
        </template>
        <div v-else class="empty-state">
          <div class="empty-reel">▶</div>
          <h2>媒体库还没有准备好</h2>
          <p v-if="store.user">前往管理设置连接飞牛，然后执行第一次同步。</p>
          <p v-else>管理员完成飞牛连接与同步后，这里就会出现今晚的影片。</p>
          <a v-if="store.user" href="#/admin" class="button-link primary">去连接飞牛</a>
        </div>
      </section>
    </div>
    <p class="inline-error" v-if="error">{{ error }}</p>
  </div>
</template>
