<template>
  <div class="p-6 h-full flex flex-col">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-lg font-bold">日志中心</h2>
        <p class="text-xs text-slate-400 mt-0.5">内存环形缓冲（{{ state.logs.length }} / 1000 条）· 关键事件自动落库，高频轮询不写盘</p>
      </div>
      <div class="flex items-center gap-2">
        <select v-model="module" class="input !w-36 !py-1.5 text-xs">
          <option value="ALL">全部模块</option>
          <option v-for="m in modules" :key="m" :value="m">{{ m }}</option>
        </select>
        <button class="btn-ghost text-xs" @click="autoRefresh = !autoRefresh">
          {{ autoRefresh ? '⏸ 暂停自动刷新' : '▶ 开启自动刷新' }}
        </button>
        <button class="btn-ghost text-xs" @click="mockLogs">✨ 模拟新任务日志</button>
        <button class="btn-ghost text-xs" @click="download">⬇ 下载日志</button>
        <button class="btn-ghost text-xs" @click="refresh">⟳ 刷新</button>
      </div>
    </div>

    <div class="card flex-1 min-h-[480px] overflow-hidden flex flex-col">
      <div ref="consoleBox" class="flex-1 overflow-y-auto bg-slate-900 dark:bg-black p-4 font-mono text-xs leading-6">
        <div v-for="log in state.logs" :key="log.id" class="flex gap-3">
          <span class="text-slate-500 shrink-0">{{ fmtTime(log.time) }}</span>
          <span class="shrink-0 w-16 text-right" :class="levelClass(log.level)">[{{ log.level }}]</span>
          <span class="shrink-0 w-24 text-sky-400">[{{ log.module }}]</span>
          <span class="text-slate-200 break-all">{{ log.message }}</span>
        </div>
        <div v-if="!state.logs.length" class="text-slate-500">暂无日志记录</div>
      </div>
      <div class="px-4 py-2 border-t border-slate-200 dark:border-slate-700 text-[10px] text-slate-400 flex justify-between">
        <span>INFO 翠绿 / WARN 琥珀 / ERROR 玫瑰</span>
        <span>{{ autoRefresh ? '自动刷新：3s' : '自动刷新：已暂停' }}</span>
      </div>
    </div>
  </div>
</template>

<script setup>
import { nextTick, onUnmounted, ref } from 'vue'
import { state, refreshLogs, toast, fmtTime } from '../store.js'
import { api } from '../api.js'

const modules = ['System', 'Scheduler', 'Cloud115', 'Cloud123', 'CloudGuangYa', 'Tianyi', 'Organizer', 'STRM', 'Transfer', 'Config', 'API', 'Meta']
const module = ref('ALL')
const autoRefresh = ref(true)
const consoleBox = ref(null)
let timer = null

function levelClass(level) {
  return { INFO: 'text-emerald-400', WARN: 'text-amber-400', ERROR: 'text-rose-400' }[level] || 'text-slate-300'
}

async function refresh() {
  await refreshLogs(module.value)
  await nextTick()
  if (consoleBox.value) consoleBox.value.scrollTop = consoleBox.value.scrollHeight
}

async function mockLogs() {
  try {
    await api.post('/api/logs/mock', { count: 5 })
    await refresh()
    toast('info', '已生成模拟日志', '5 条模拟任务日志写入环形缓冲')
  } catch (e) {
    toast('error', '操作失败', e.message)
  }
}

async function download() {
  try {
    await api.download('/api/logs/download?module=' + encodeURIComponent(module.value), 'Tgto-runtime.log')
    toast('success', '下载开始', 'Tgto-runtime.log')
  } catch (e) {
    toast('error', '下载失败', e.message)
  }
}

// 自动刷新控制
const stop = setInterval(async () => {
  if (autoRefresh.value && state.token) {
    try {
      await refreshLogs(module.value)
      await nextTick()
      if (consoleBox.value) consoleBox.value.scrollTop = consoleBox.value.scrollHeight
    } catch {
      /* 忽略轮询错误 */
    }
  }
}, 3000)

onUnmounted(() => clearInterval(stop))

refresh()
</script>
