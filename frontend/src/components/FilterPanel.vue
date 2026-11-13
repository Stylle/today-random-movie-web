<script setup>
import { t } from "./../i18n";
import { computed } from 'vue'

const props = defineProps({
  modelValue: { type: Object, required: true },
  options: { type: Object, default: () => ({ libraries: [], genres: [], years: [], countries: [] }) },
  compact: Boolean,
  excludeWatchedDefault: { type: Boolean, default: true },
  resetValue: { type: Object, default: null },
})
const emit = defineEmits(['update:modelValue'])

const years = computed(() => [...new Set(props.options.years || [])].sort((a, b) => Number(a) - Number(b)))

function patch(key, value) {
  const next = { ...props.modelValue, [key]: value }
  if (key === 'ratingMin' || key === 'ratingMax') { if (value === '') delete next[key]; else next[key] = Number(value) }
  if (key === 'yearFrom' || key === 'yearTo') delete next.year
  emit('update:modelValue', next)
}
function toggleLibrary(id) {
  const current = props.modelValue.libraries || []
  patch('libraries', current.includes(id) ? current.filter(value => value !== id) : [...current, id])
}
function reset() {
  if (props.resetValue) { emit('update:modelValue', JSON.parse(JSON.stringify(props.resetValue))); return }
  emit('update:modelValue', { libraries: [], genre: '', country: '', yearFrom: '', yearTo: '', excludeWatched: props.excludeWatchedDefault })
}
</script>

<template>
  <section class="filter-panel" :class="{ compact }">
    <div class="filter-head"><div><span class="eyebrow">FILTERS</span><h3>{{ t('选片偏好') }}</h3></div><button class="text-btn" @click="reset">{{ t('重置') }}</button></div>
    <div class="library-chips" v-if="options.libraries?.length">
      <button v-for="library in options.libraries" :key="library.id" :class="{ on: modelValue.libraries?.includes(library.id) }" @click="toggleLibrary(library.id)">
        {{ library.name }} <small>{{ library.count }}</small>
      </button>
    </div>
    <div class="filter-grid">
      <label><span>{{ t('类型') }}</span><select :value="modelValue.genre" @change="patch('genre', $event.target.value)"><option value="">{{ t('全部类型') }}</option><option v-for="value in options.genres" :key="value">{{ value }}</option></select></label>
      <label><span>{{ t('地区') }}</span><select :value="modelValue.country" @change="patch('country', $event.target.value)"><option value="">{{ t('全部地区') }}</option><option v-for="value in options.countries" :key="value">{{ value }}</option></select></label>
      <fieldset class="year-range"><legend>{{ t('年份范围') }}</legend><label><span class="sr-only">{{ t('起始年份') }}</span><select :value="modelValue.yearFrom" :aria-label="t('起始年份')" @change="patch('yearFrom', $event.target.value)"><option value="">{{ t('最早') }}</option><option v-for="value in years" :key="`from-${value}`" :value="value">{{ value }} {{ t('年') }}</option></select></label><i>{{ t('至') }}</i><label><span class="sr-only">{{ t('结束年份') }}</span><select :value="modelValue.yearTo" :aria-label="t('结束年份')" @change="patch('yearTo', $event.target.value)"><option value="">{{ t('最新') }}</option><option v-for="value in years" :key="`to-${value}`" :value="value">{{ value }} {{ t('年') }}</option></select></label></fieldset>
      <fieldset class="year-range rating-range"><legend>{{ t('评分范围') }}</legend><label><span class="sr-only">{{ t('最低评分') }}</span><select :value="modelValue.ratingMin ?? ''" aria-label="最低评分" @change="patch('ratingMin', $event.target.value)"><option value="">{{ t('不限') }}</option><option v-for="n in 11" :key="n" :value="n-1">{{ n-1 }}{{ t('分') }}</option></select></label><i>{{ t('至') }}</i><label><span class="sr-only">{{ t('最高评分') }}</span><select :value="modelValue.ratingMax ?? ''" aria-label="最高评分" @change="patch('ratingMax', $event.target.value)"><option value="">10{{ t('分') }}</option><option v-for="n in 11" :key="n" :value="n-1">{{ n-1 }}{{ t('分') }}</option></select></label></fieldset><small class="rating-hint">{{ t('无评分影片按 10 分参与筛选') }}</small>
      <label class="check-row"><input type="checkbox" :checked="modelValue.excludeWatched" @change="patch('excludeWatched', $event.target.checked)" /><span>{{ t('排除已观看') }}</span></label>
    </div>
  </section>
</template>
