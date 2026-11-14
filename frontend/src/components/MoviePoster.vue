<script setup>
import { computed, onBeforeUnmount, ref, watch } from 'vue'
const emit=defineEmits(['metadata'])
const props = defineProps({ movie: { type: Object, required: true }, loading: { type: String, default: 'eager' } })
const resolved = ref([])
const failed=ref([])
let douyingAttempted=false
let extraLookup
const order = ref(props.movie.fallbackOrder || ['douban','tmdb','douying','cache','placeholder'])
const placeholder='data:image/svg+xml,'+encodeURIComponent('<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 400 600"><rect width="400" height="600" fill="#343849"/><path d="M155 235l100 65-100 65z" fill="#aaa9d8"/></svg>')
const sources = computed(() => {
 const urls = [...new Set([...resolved.value, ...(props.movie.posterSources || [])])], result=[]
 for(const provider of order.value){
  if(provider==='cache')result.push(`/api/posters/${encodeURIComponent(props.movie.id)}?cache=only&strict=1`)
  else if(provider==='placeholder')result.push(placeholder)
  else result.push(...urls.filter(value => provider==='douban'?value.includes('.doubanio.com/'):provider==='tmdb'?value.startsWith('https://image.tmdb.org/'):value.startsWith('https://doubaninfo.com/')))
 }
 const available=result.filter(source=>!failed.value.includes(source));return available.length?available:[placeholder]
})
watch(()=>props.movie.id,()=>{failed.value=[];douyingAttempted=false;extraLookup?.abort()})
const index = ref(0)
const image = ref(null)
const visible = ref(props.loading !== 'lazy')
let timer
let observer
let lookup
let lookupRetry
const lookupAttempt=ref(0)
watch(() => [props.movie.id, visible.value, lookupAttempt.value], async ([id, shown]) => {
 lookup?.abort(); clearTimeout(lookupRetry); resolved.value = []
 if (!id || !shown) return
 const controller = new AbortController(); lookup = controller
 try {
 const response = await fetch(`/api/movie-posters?id=${encodeURIComponent(id)}`, { credentials: 'same-origin', signal: controller.signal })
 if (!response.ok) return
 const data = await response.json()
 if (!controller.signal.aborted && id === props.movie.id) { resolved.value = [...resolved.value,...(data.posterSources || [])]; order.value = data.fallbackOrder || order.value; if(data.overview) emit('metadata',{overview:data.overview}); if (data.status?.includes('rate_limited')) lookupRetry=setTimeout(()=>lookupAttempt.value++,31000) }
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
async function advance() {
 clearTimeout(timer)
 const source=sources.value[index.value];if(source!==placeholder)failed.value=[...failed.value,source];index.value=0
 if(douyingAttempted||!order.value.includes('douying')||sources.value.some(url=>url.startsWith('https://doubaninfo.com/')))return
 const earlier=order.value.slice(0,order.value.indexOf('douying'))
 const hasEarlier=sources.value.some(value=>earlier.some(provider=>provider==='cache'?value.startsWith('/api/posters/'):provider==='douban'?value.includes('.doubanio.com/'):provider==='tmdb'?value.startsWith('https://image.tmdb.org/'):false))
 if(hasEarlier)return
 douyingAttempted=true
 const id=props.movie.id,controller=new AbortController();extraLookup=controller
 try{const response=await fetch(`/api/movie-posters?id=${encodeURIComponent(id)}&fallback=douying`,{credentials:'same-origin',signal:controller.signal});if(!response.ok)return;const data=await response.json();if(!controller.signal.aborted&&props.movie.id===id){resolved.value=[...resolved.value,...(data.posterSources||[])];if(data.status==='rate_limited')lookupRetry=setTimeout(()=>{douyingAttempted=false;advance()},31000)}}catch{}
}
function loaded() { clearTimeout(timer) }
watch(() => [props.movie.id, ...sources.value].join('|'), () => { index.value = 0 }, { flush: 'sync' })
watch(() => [sources.value[index.value], visible.value], () => {
  clearTimeout(timer)
  if (visible.value && index.value < sources.value.length - 1) timer = setTimeout(advance, 8000)
}, { immediate: true, flush: 'post' })
onBeforeUnmount(() => { clearTimeout(timer); observer?.disconnect(); lookup?.abort(); clearTimeout(lookupRetry); extraLookup?.abort() })
</script>

<template><img ref="image" :key="`${movie.id}-${index}`" :src="sources[index]" :alt="movie.title" :loading="loading" referrerpolicy="no-referrer" @error="advance" @load="loaded" /></template>
