<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
const props = defineProps({ movie: { type: Object, required: true }, loading: { type: String, default: 'eager' } })
const resolved = ref([])
const sources = computed(() => [...new Set([...resolved.value, ...(props.movie.posterSources || []), `/api/posters/${encodeURIComponent(props.movie.id)}?cache=only`])])
const index = ref(0)
const image = ref(null)
const visible = ref(props.loading !== 'lazy')
let timer
let observer
let lookup
watch(() => [props.movie.id, visible.value], async ([id, shown]) => {
 lookup?.abort(); resolved.value = []
 if (!id || !shown) return
 const controller = new AbortController(); lookup = controller
 try {
 const response = await fetch(`/api/movie-posters?id=${encodeURIComponent(id)}`, { credentials: 'same-origin', signal: controller.signal })
 if (!response.ok) return
 const data = await response.json()
 if (!controller.signal.aborted && id === props.movie.id) resolved.value = data.posterSources || []
 } catch {}
}, { immediate: true })
watch(image, element => {
  observer?.disconnect()
  if (!element || visible.value) return
  observer = new IntersectionObserver(entries => {
    if (entries.some(entry => entry.isIntersecting)) { visible.value = true; observer.disconnect() }
  })
  observer.observe(element)
}, { flush: 'post' })
function advance() {
  clearTimeout(timer)
  if (index.value < sources.value.length - 1) index.value++
}
function loaded() { clearTimeout(timer) }
watch(() => [props.movie.id, ...sources.value].join('|'), () => { index.value = 0 }, { flush: 'sync' })
watch(() => [sources.value[index.value], visible.value], () => {
  clearTimeout(timer)
  if (visible.value && index.value < sources.value.length - 1) timer = setTimeout(advance, 8000)
}, { immediate: true, flush: 'post' })
onBeforeUnmount(() => { clearTimeout(timer); observer?.disconnect(); lookup?.abort() })
</script>

<template><img ref="image" :key="`${movie.id}-${index}`" :src="sources[index]" :alt="movie.title" :loading="loading" referrerpolicy="no-referrer" @error="advance" @load="loaded" /></template>
