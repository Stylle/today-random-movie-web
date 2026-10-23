<script setup>
import { onMounted, ref, watch } from 'vue'
import { api } from '../api.js'
import FilterPanel from '../components/FilterPanel.vue'

const options = ref({ libraries: [], genres: [], years: [], countries: [] })
const filters = ref({ libraries: [], genre: '', country: '', year: '', excludeWatched: false })
const query = ref('')
const items = ref([])
const total = ref(0)
const page = ref(1)
const busy = ref(false)
const error = ref('')
const selected = ref(null)
let timer

async function load(reset = false) {
  if (reset) page.value = 1
  busy.value = true
  const params = new URLSearchParams({ page: String(page.value), pageSize: '30', q: query.value, year: filters.value.year, genre: filters.value.genre, country: filters.value.country, excludeWatched: String(filters.value.excludeWatched) })
  filters.value.libraries.forEach(value => params.append('library', value))
  try {
    const result = await api('/api/movies?' + params)
    items.value = result.items
    total.value = result.total
  } catch (e) { error.value = e.message } finally { busy.value = false }
}

watch([filters, query], () => { clearTimeout(timer); timer = setTimeout(() => load(true), 220) }, { deep: true })
watch(page, () => load(false))
onMounted(async () => { try { options.value = await api('/api/movies/options'); await load() } catch (e) { error.value = e.message } })
</script>

<template>
  <div>
    <section class="page-heading"><div><span class="eyebrow">YOUR LIBRARY</span><h1>影片库</h1><p>所有影片都来自最近一次飞牛同步。</p></div><label class="search-box">⌕<input v-model="query" placeholder="搜索片名…" /></label></section>
    <FilterPanel v-model="filters" :options="options" compact />
    <div class="result-line"><span>共 {{ total }} 部影片</span><span v-if="busy">正在载入…</span></div>
    <div class="movie-grid">
      <button class="movie-card" v-for="item in items" :key="item.id" @click="selected = item">
        <div class="card-poster"><img :src="item.posterUrl" loading="lazy" :alt="item.title" /><span v-if="item.rating">★ {{ item.rating }}</span></div>
        <b>{{ item.title }}</b><small>{{ [item.year, item.library].filter(Boolean).join(' · ') }}</small>
      </button>
    </div>
    <p class="inline-error" v-if="error">{{ error }}</p>
    <div class="pagination" v-if="total > 30"><button class="ghost" :disabled="page <= 1" @click="page--">上一页</button><span>{{ page }} / {{ Math.ceil(total / 30) }}</span><button class="ghost" :disabled="page >= Math.ceil(total / 30)" @click="page++">下一页</button></div>

    <div class="modal-layer" v-if="selected" @click.self="selected = null">
      <article class="movie-modal"><button class="modal-close" @click="selected = null">×</button><img :src="selected.posterUrl" :alt="selected.title" /><div><span class="eyebrow">MOVIE DETAILS</span><h2>{{ selected.title }}</h2><p class="movie-meta">{{ [selected.year, selected.library, selected.duration].filter(Boolean).join(' · ') }}</p><div class="tags"><span v-for="tag in selected.genres" :key="tag">{{ tag }}</span></div><p class="overview">{{ selected.overview || '暂无简介。' }}</p></div></article>
    </div>
  </div>
</template>
