<script setup>
const props = defineProps({ modelValue: { type: Object, required: true }, options: { type: Object, default: () => ({ libraries: [], genres: [], years: [], countries: [] }) }, compact: Boolean })
const emit = defineEmits(['update:modelValue'])

function patch(key, value) { emit('update:modelValue', { ...props.modelValue, [key]: value }) }
function toggleLibrary(id) {
  const current = props.modelValue.libraries || []
  patch('libraries', current.includes(id) ? current.filter(value => value !== id) : [...current, id])
}
</script>

<template>
  <section class="filter-panel" :class="{ compact }">
    <div class="filter-head"><div><span class="eyebrow">FILTERS</span><h3>选片偏好</h3></div><button class="text-btn" @click="emit('update:modelValue', { libraries: [], genre: '', country: '', year: '', excludeWatched: true })">重置</button></div>
    <div class="library-chips" v-if="options.libraries?.length">
      <button v-for="library in options.libraries" :key="library.id" :class="{ on: modelValue.libraries?.includes(library.id) }" @click="toggleLibrary(library.id)">
        {{ library.name }} <small>{{ library.count }}</small>
      </button>
    </div>
    <div class="filter-grid">
      <label><span>类型</span><select :value="modelValue.genre" @change="patch('genre', $event.target.value)"><option value="">全部类型</option><option v-for="value in options.genres" :key="value">{{ value }}</option></select></label>
      <label><span>地区</span><select :value="modelValue.country" @change="patch('country', $event.target.value)"><option value="">全部地区</option><option v-for="value in options.countries" :key="value">{{ value }}</option></select></label>
      <label><span>年份</span><select :value="modelValue.year" @change="patch('year', $event.target.value)"><option value="">全部年份</option><option v-for="value in options.years" :key="value">{{ value }}</option></select></label>
      <label class="check-row"><input type="checkbox" :checked="modelValue.excludeWatched" @change="patch('excludeWatched', $event.target.checked)" /><span>排除已观看</span></label>
    </div>
  </section>
</template>
