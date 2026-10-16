<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-lg font-bold">榜单推荐</h2>
        <p class="text-xs text-slate-400 mt-0.5">主流流媒体平台热门内容聚合</p>
      </div>
      <div class="flex items-center gap-2">
        <select v-model="region" class="input !w-28 !py-1.5 text-xs" @change="load">
          <option value="全部">全部地区</option>
          <option value="美国">美国</option>
          <option value="中国大陆">中国大陆</option>
          <option value="日本">日本</option>
          <option value="韩国">韩国</option>
        </select>
        <button class="btn-ghost text-xs" @click="load">⟳ 刷新数据</button>
      </div>
    </div>

    <!-- 流媒体平台切换栏 -->
    <div class="flex flex-wrap gap-1 border-b border-slate-200 dark:border-slate-700 mb-3">
      <button
        v-for="src in sources"
        :key="src.key"
        class="px-3 py-2 text-xs -mb-px border-b-2 transition-colors"
        :class="platform === src.key ? 'border-brand-500 text-brand-600 dark:text-brand-500 font-medium' : 'border-transparent text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'"
        @click="switchPlatform(src.key)"
      >
        {{ src.name }}
      </button>
    </div>

    <!-- 榜单类型子标签 -->
    <div class="flex flex-wrap gap-1 mb-4">
      <button
        v-for="chart in charts"
        :key="chart.key"
        class="px-3 py-1.5 rounded-full text-xs transition-colors"
        :class="activeChart === chart.key ? 'bg-brand-500 text-white' : 'bg-slate-100 text-slate-600 hover:bg-slate-200 dark:bg-slate-700 dark:text-slate-300 dark:hover:bg-slate-600'"
        @click="switchChart(chart.key)"
      >
        {{ chart.name }}
      </button>
    </div>

    <!-- 横滑榜单卡片 -->
    <div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-5 gap-4">
      <div v-for="(item, i) in items" :key="item.id + i" class="card overflow-hidden flex flex-col">
        <div class="relative h-36 flex items-end p-2" :style="{ background: `linear-gradient(135deg, ${item.poster_color}, #0f172a)` }">
          <span class="badge bg-black/50 text-white backdrop-blur">TOP {{ i + 1 }}</span>
          <span class="badge absolute top-2 right-2 bg-black/50 text-amber-300 backdrop-blur">★ {{ item.rating.toFixed(1) }}</span>
          <div class="absolute inset-0 flex items-center justify-center text-white/90 font-bold text-center px-2 leading-tight">{{ item.title }}</div>
        </div>
        <div class="p-3">
          <div class="flex flex-wrap gap-1 mb-2">
            <span v-for="p in (item.platforms || [])" :key="p" class="badge bg-brand-500/10 text-brand-600 dark:text-brand-500">{{ p }}</span>
          </div>
          <div class="text-xs text-slate-400">{{ item.year || '----' }} · {{ item.region }} · {{ item.media_type === 'movie' ? '电影' : '剧集' }}</div>
          <div class="text-sm font-semibold mt-1 truncate" :title="item.title">{{ item.title }}</div>
          <p class="text-[11px] text-slate-500 dark:text-slate-400 mt-1 line-clamp-2">{{ item.overview || '' }}</p>
        </div>
      </div>
      <div v-if="!items.length && !loading" class="col-span-full py-16 text-center text-slate-400 text-sm">暂无榜单数据</div>
      <div v-if="loading" class="col-span-full py-16 text-center text-slate-400 text-sm">加载中...</div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, ref } from 'vue'
import { toast } from '../store.js'
import { api } from '../api.js'

const sources = ref([])
const charts = ref([])
const platform = ref('all')
const activeChart = ref('all')
const region = ref('全部')
const items = ref([])
const loading = ref(false)

function switchPlatform(key) {
  platform.value = key
  load()
}
function switchChart(key) {
  activeChart.value = key
  load()
}

async function load() {
  loading.value = true
  try {
    const params = new URLSearchParams({ platform: platform.value, chart: activeChart.value, region: region.value })
    const data = await api.get('/api/trending?' + params.toString())
    items.value = data.items || []
    if (data.sources && data.sources.length && !sources.value.length) {
      sources.value = [{ key: 'all', name: '全部平台' }, ...data.sources]
    }
    if (data.charts && !charts.value.length) charts.value = data.charts
  } catch (e) {
    toast('error', '加载失败', e.message)
  } finally {
    loading.value = false
  }
}

onMounted(load)
</script>
