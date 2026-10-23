<script setup>
import { computed } from 'vue'

const props = defineProps({
  modelValue: { type: Object, required: true },
  options: { type: Object, default: () => ({ libraries: [], genres: [], years: [], countries: [] }) },
  compact: Boolean,
  excludeWatchedDefault: { type: Boolean, default: true },
})
const emit = defineEmits(['update:modelValue'])

const years = computed(() => [...new Set(props.options.years || [])].sort((a, b) => Number(a) - Number(b)))

function patch(key, value) {
  const next = { ...props.modelValue, [key]: value }
  if (key === 'yearFrom' || key === 'yearTo') delete next.year
  emit('update:modelValue', next)
}
function toggleLibrary(id) {
  const current = props.modelValue.libraries || []
  patch('libraries', current.includes(id) ? current.filter(value => value !== id) : [...current, id])
}
function reset() {
  emit('update:modelValue', { libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: props.excludeWatchedDefault })
}
</script>

<template>
  <section class="filter-panel" :class="{ compact }">
    <div class="filter-head"><div><span class="eyebrow">FILTERS</span><h3>选片偏好</h3></div><button class="text-btn" @click="reset">重置</button></div>
    <div class="library-chips" v-if="options.libraries?.length">
      <button v-for="library in options.libraries" :key="library.id" :class="{ on: modelValue.libraries?.includes(library.id) }" @click="toggleLibrary(library.id)">
        {{ library.name }} <small>{{ library.count }}</small>
      </button>
    </div>
    <div class="filter-grid">
      <label><span>类型</span><select :value="modelValue.genre" @change="patch('genre', $event.target.value)"><option value="">全部类型</option><option v-for="value in options.genres" :key="value">{{ value }}</option></select></label>
      <label><span>地区</span><select :value="modelValue.country" @change="patch('country', $event.target.value)"><option value="">全部地区</option><option v-for="value in options.countries" :key="value">{{ value }}</option></select></label>
      <fieldset class="year-range"><legend>年份范围</legend><label><span class="sr-only">起始年份</span><select :value="modelValue.yearFrom" aria-label="起始年份" @change="patch('yearFrom', $event.target.value)"><option value="">最早</option><option v-for="value in years" :key="`from-${value}`" :value="value">{{ value }} 年</option></select></label><i>至</i><label><span class="sr-only">结束年份</span><select :value="modelValue.yearTo" aria-label="结束年份" @change="patch('yearTo', $event.target.value)"><option value="">最新</option><option v-for="value in years" :key="`to-${value}`" :value="value">{{ value }} 年</option></select></label></fieldset>
      <label class="check-row"><input type="checkbox" :checked="modelValue.excludeWatched" @change="patch('excludeWatched', $event.target.checked)" /><span>排除已观看</span></label>
    </div>
  </section>
</template>
