<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-lg font-bold">追剧日历</h2>
        <p class="text-xs text-slate-400 mt-0.5">本周更新安排（周一至周日）</p>
      </div>
      <div class="flex items-center gap-2">
        <div class="flex rounded-lg overflow-hidden border border-slate-200 dark:border-slate-700">
          <button
            v-for="k in kinds"
            :key="k.key"
            class="px-3 py-1.5 text-xs"
            :class="kind === k.key ? 'bg-brand-500 text-white' : 'bg-white text-slate-500 dark:bg-slate-800 dark:text-slate-400'"
            @click="kind = k.key; load()"
          >
            {{ k.label }}
          </button>
        </div>
        <button class="btn-ghost text-xs" @click="shift(-7)">‹</button>
        <span class="text-xs text-slate-500 w-32 text-center">{{ rangeLabel }}</span>
        <button class="btn-ghost text-xs" @click="shift(7)">›</button>
        <button class="btn-ghost text-xs" @click="load">⟳ 刷新数据</button>
      </div>
    </div>

    <div class="grid grid-cols-7 gap-3">
      <div
        v-for="(day, i) in days"
        :key="day.date"
        class="card p-3 min-h-[280px] flex flex-col"
        :class="isToday(day.date) ? 'ring-2 ring-brand-500' : ''"
      >
        <div class="flex items-center justify-between mb-2">
          <span class="text-xs font-semibold" :class="isToday(day.date) ? 'text-brand-500' : 'text-slate-500'">{{ weekLabels[i] }}</span>
          <span class="text-[10px] text-slate-400 font-mono">{{ day.date.slice(5) }}</span>
        </div>
        <span v-if="isToday(day.date)" class="badge bg-brand-500 text-white self-start mb-2">今日</span>
        <div class="space-y-2 flex-1">
          <div v-for="item in day.items" :key="item.id + item.airing_date" class="rounded-lg p-2 text-[11px]" :style="{ background: item.poster_color + '22', borderLeft: '3px solid ' + item.poster_color }">
            <div class="font-semibold text-slate-700 dark:text-slate-200 truncate" :title="item.title">{{ item.title }}</div>
            <div class="text-slate-400 mt-0.5">{{ item.episode_label }}</div>
            <div class="text-slate-400 truncate">{{ (item.platforms || []).join(' / ') || '多平台' }}</div>
            <div class="text-slate-400 mt-0.5">{{ item.airing_date.slice(5) }} 更新</div>
          </div>
          <div v-if="!day.items.length" class="text-[10px] text-slate-300 dark:text-slate-600 text-center py-4">暂无更新</div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { computed, onMounted, ref } from 'vue'
import { toast } from '../store.js'
import { api } from '../api.js'

const kinds = [
  { key: 'all', label: '全部' },
  { key: 'tv', label: '剧集' },
  { key: 'movie', label: '电影' }
]
const kind = ref('all')
const date = ref(new Date())
const days = ref([])
const weekLabels = ['周一', '周二', '周三', '周四', '周五', '周六', '周日']

const rangeLabel = computed(() => {
  if (!days.value.length) return ''
  return days.value[0].date + ' ~ ' + days.value[6].date
})

function isToday(d) {
  const t = new Date()
  const p = (n) => String(n).padStart(2, '0')
  return d === `${t.getFullYear()}-${p(t.getMonth() + 1)}-${p(t.getDate())}`
}

function shift(deltaDays) {
  const d = new Date(date.value)
  d.setDate(d.getDate() + deltaDays)
  date.value = d
  load()
}

async function load() {
  try {
    const p = (n) => String(n).padStart(2, '0')
    const d = `${date.value.getFullYear()}-${p(date.value.getMonth() + 1)}-${p(date.value.getDate())}`
    const data = await api.get(`/api/calendar?date=${d}&kind=${kind.value}`)
    days.value = data.days || []
  } catch (e) {
    toast('error', '加载失败', e.message)
  }
}

onMounted(load)
</script>
