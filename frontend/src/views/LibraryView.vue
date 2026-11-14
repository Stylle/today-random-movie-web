<script setup>
import MoviePoster from '../components/MoviePoster.vue'
import { t } from "./../i18n";
import { nextTick, onMounted, onUnmounted, ref, watch } from 'vue'
import { pageSizeForColumns } from '../library-layout.mjs'
import { api } from '../api.js'
import FilterPanel from '../components/FilterPanel.vue'

const options = ref({ libraries: [], genres: [], years: [], countries: [] })
const filters = ref({ libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: false })
const query = ref('')
const items = ref([])
const total = ref(0)
const page = ref(1)
const busy = ref(false)
const error = ref('')
const selected = ref(null)
const grid = ref(null)
const pageSize = ref(28)
let timer, observer, request
let generation = 0, ready = false, active = true

async function load() {
  if (!active) return
  request?.abort()
  const id = ++generation
  const controller = new AbortController()
  request = controller
  error.value = ''
  busy.value = true
  const params = new URLSearchParams({ page: String(page.value), pageSize: String(pageSize.value), q: query.value, yearFrom: filters.value.yearFrom, yearTo: filters.value.yearTo, genre: filters.value.genre, country: filters.value.country, excludeWatched: String(filters.value.excludeWatched) })
  for (const key of ['ratingMin', 'ratingMax']) { if (filters.value[key] != null) params.set(key, String(filters.value[key])) }
  filters.value.libraries.forEach(value => params.append('library', value))
  try {
    const result = await api('/api/movies?' + params, { signal: controller.signal })
    if (id !== generation) return
    items.value = result.items
    total.value = result.total
  } catch (e) { if (id === generation && e.name !== 'AbortError') error.value = e.message } finally { if (id === generation) busy.value = false }
}

function measureGrid() {
  if (!grid.value) return
  const columns = getComputedStyle(grid.value).gridTemplateColumns.trim().split(/\s+/).filter(Boolean).length
  const next = pageSizeForColumns(columns)
  if (next !== pageSize.value) pageSize.value = next
}
function changePage(next) {
  page.value = next
  clearTimeout(timer)
  load()
}
watch([filters, query, pageSize], () => {
  if (!ready) return
  clearTimeout(timer)
  request?.abort()
  generation++
  page.value = 1
  busy.value = true
  timer = setTimeout(load, 220)
}, { deep: true })
onMounted(async () => {
  await nextTick()
  measureGrid()
  if (typeof ResizeObserver !== 'undefined') {
    observer = new ResizeObserver(measureGrid)
    observer.observe(grid.value)
  }
  try {
    const result = await api('/api/movies/options')
    if (!active) return
    options.value = result
    ready = true
    await load()
  } catch (e) { error.value = e.message }
})
onUnmounted(() => { active = false; ready = false; clearTimeout(timer); request?.abort(); generation++; observer?.disconnect() })
</script>

<template>
  <div>
    <section class="page-heading"><div><span class="eyebrow">YOUR LIBRARY</span><h1>{{ t('影片库') }}</h1><p>{{ t('所有影片都来自最近一次飞牛同步。') }}</p></div><label class="search-box">⌕<input v-model="query" :placeholder="t('搜索片名…')" /></label></section>
    <FilterPanel v-model="filters" :options="options" :exclude-watched-default="false" compact />
    <div class="result-line"><span>{{ t('共') }} {{ total }} {{ t('部影片') }}</span><span v-if="busy">{{ t('正在载入…') }}</span></div>
    <div ref="grid" class="movie-grid" :aria-busy="busy">
      <button class="movie-card" v-for="item in items" :key="item.id" @click="selected = item">
        <div class="card-poster"><MoviePoster :movie="item" loading="lazy" @metadata="data => { if (!item.overview) item.overview=data.overview }" /><span v-if="item.rating">★ {{ Number(item.rating).toFixed(1) }}</span></div>
        <b>{{ item.title }}</b><small>{{ [item.year, item.library].filter(Boolean).join(' · ') }}</small>
      </button>
    </div>
    <p class="inline-error" v-if="error">{{ error }}</p>
    <div class="pagination" v-if="total > pageSize"><button class="ghost" :disabled="page <= 1" @click="changePage(page - 1)">{{ t('上一页') }}</button><span>{{ page }} / {{ Math.ceil(total / pageSize) }}</span><button class="ghost" :disabled="page >= Math.ceil(total / pageSize)" @click="changePage(page + 1)">{{ t('下一页') }}</button></div>

    <div class="modal-layer" v-if="selected" @click.self="selected = null">
      <article class="movie-modal"><button class="modal-close" @click="selected = null">×</button><MoviePoster :movie="selected" @metadata="data => { if (!selected.overview) selected.overview=data.overview }" /><div><span class="eyebrow">MOVIE DETAILS</span><h2>{{ selected.title }}</h2><p class="movie-meta">{{ [selected.year, selected.library, selected.duration].filter(Boolean).join(' · ') }}</p><div class="tags"><span v-for="tag in selected.genres" :key="tag">{{ tag }}</span></div><p class="overview">{{ selected.overview || t('暂无简介。') }}</p></div></article>
    </div>
  </div>
</template>
