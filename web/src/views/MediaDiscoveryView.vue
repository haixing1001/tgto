<template>
  <div class="p-6">
    <div class="flex items-center justify-between mb-4">
      <div>
        <h2 class="text-lg font-bold">影视探索</h2>
        <p class="text-xs text-slate-400 mt-0.5">多源影视发现 · 配置 TMDB Key 后自动切换真实数据源</p>
      </div>
    </div>

    <!-- 数据源子标签 -->
    <div class="flex flex-wrap gap-1 border-b border-slate-200 dark:border-slate-700 mb-4">
      <button
        v-for="src in sources"
        :key="src.key"
        class="px-3 py-2 text-xs -mb-px border-b-2 transition-colors"
        :class="query.source === src.key ? 'border-brand-500 text-brand-600 dark:text-brand-500 font-medium' : 'border-transparent text-slate-500 hover:text-slate-700 dark:hover:text-slate-300'"
        @click="switchSource(src.key)"
      >
        {{ src.name }}
      </button>
    </div>

    <!-- 多维筛选面板 -->
    <div class="card p-4 mb-4">
      <div class="flex flex-wrap items-center gap-3">
        <input v-model="query.keyword" class="input !w-56 !py-1.5 text-xs" placeholder="搜索片名 / 原名" @keyup.enter="search" />
        <button class="btn-primary text-xs" @click="search">搜索</button>
        <button class="btn-ghost text-xs" @click="reset">清空</button>
        <button class="btn-ghost text-xs" @click="resetAll">重置筛选</button>
      </div>
      <div class="flex flex-wrap gap-3 mt-3">
        <select v-model="query.category" class="input !w-28 !py-1.5 text-xs" @change="search">
          <option v-for="c in meta.categories" :key="c" :value="c">{{ c }}</option>
        </select>
        <select v-model="query.genre" class="input !w-28 !py-1.5 text-xs" @change="search">
          <option v-for="g in meta.genres" :key="g" :value="g">{{ g }}</option>
        </select>
        <select v-model="query.region" class="input !w-28 !py-1.5 text-xs" @change="search">
          <option v-for="r in meta.regions" :key="r" :value="r">{{ r }}</option>
        </select>
        <select v-model="query.year" class="input !w-28 !py-1.5 text-xs" @change="search">
          <option v-for="y in meta.years" :key="y" :value="y">{{ y }}</option>
        </select>
        <select v-model="query.sort" class="input !w-24 !py-1.5 text-xs" @change="search">
          <option v-for="s in meta.sorts" :key="s" :value="s">{{ s }}</option>
        </select>
      </div>
    </div>

    <!-- 媒体海报网格 -->
    <div class="grid grid-cols-2 md:grid-cols-3 xl:grid-cols-6 gap-4">
      <div v-for="item in items" :key="item.id" class="card overflow-hidden flex flex-col group">
        <div class="relative h-40 flex items-end p-2" :style="{ background: `linear-gradient(135deg, ${item.poster_color}, #0f172a)` }">
          <span class="badge bg-black/50 text-amber-300 backdrop-blur">★ {{ item.rating.toFixed(1) }}</span>
          <span v-if="item.quality_tag" class="badge absolute top-2 right-2 bg-black/50 text-white backdrop-blur">{{ item.quality_tag }}</span>
          <div class="absolute inset-0 flex items-center justify-center text-white/90 font-bold text-lg text-center px-2 leading-tight">{{ item.title }}</div>
        </div>
        <div class="p-3 flex-1 flex flex-col">
          <div class="text-xs text-slate-400">{{ item.year || '----' }} · {{ item.region }} · {{ item.media_type === 'movie' ? '电影' : item.media_type === 'tv' ? '剧集' : '影人' }}</div>
          <div class="text-sm font-semibold mt-1 truncate" :title="item.title">{{ item.title }}</div>
          <div class="text-[10px] text-slate-400 truncate" :title="item.original_title">{{ item.original_title }}</div>
          <p class="text-[11px] text-slate-500 dark:text-slate-400 mt-2 line-clamp-3 flex-1">{{ item.overview || '暂无剧情简介' }}</p>
          <div class="flex items-center justify-between mt-2">
            <span class="text-[10px] text-slate-400 font-mono">TMDB: {{ item.tmdb_id || '-' }}</span>
            <button class="btn-primary text-[10px] !px-2 !py-1" @click="openTransfer(item)">检索并转存</button>
          </div>
        </div>
      </div>
      <div v-if="!items.length && !loading" class="col-span-full py-16 text-center text-slate-400 text-sm">没有符合条件的影视条目</div>
      <div v-if="loading" class="col-span-full py-16 text-center text-slate-400 text-sm">加载中...</div>
    </div>

    <!-- 分页 -->
    <div class="flex items-center justify-center gap-3 mt-5 text-xs text-slate-500">
      <button class="btn-ghost text-xs" :disabled="page <= 1" @click="page-- && search()">上一页</button>
      <span>第 {{ page }} 页 / 共 {{ total }} 条</span>
      <button class="btn-ghost text-xs" :disabled="page * 18 >= total" @click="page++ && search()">下一页</button>
    </div>

    <!-- 检索并转存弹窗 -->
    <ModalDialog v-model="transferModal" title="检索并转存" width="520px">
      <div class="space-y-3">
        <div class="text-xs text-slate-400">目标影片：<b class="text-slate-700 dark:text-slate-200">{{ activeItem.title }}</b>（TMDB: {{ activeItem.tmdb_id || '-' }}）</div>
        <div>
          <label class="label">分享链接（粘贴该影片的网盘分享地址）</label>
          <input v-model="transferForm.link" class="input" placeholder="https://123pan.com/s/xxxxxxx" />
        </div>
        <div>
          <label class="label">提取码（可选）</label>
          <input v-model="transferForm.code" class="input" />
        </div>
        <div>
          <label class="label">目标目录（可选）</label>
          <input v-model="transferForm.target" class="input" placeholder="留空使用网盘默认目录" />
        </div>
      </div>
      <template #footer>
        <button class="btn-ghost" @click="transferModal = false">取消</button>
        <button class="btn-primary" :disabled="submitting" @click="submitTransfer">{{ submitting ? '提交中...' : '立即转存' }}</button>
      </template>
    </ModalDialog>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { toast } from '../store.js'
import { api } from '../api.js'
import ModalDialog from '../components/ModalDialog.vue'

const sources = [
  { key: 'tmdb', name: 'TMDB 片库' },
  { key: 'douban', name: '豆瓣' },
  { key: 'anilist', name: 'AniList 动漫' },
  { key: 'bangumi', name: 'Bangumi 动漫' },
  { key: 'actor', name: '热门演员' }
]
const meta = reactive({
  categories: ['全部', '电影', '剧集'],
  genres: ['全部'],
  regions: ['全部'],
  years: ['全部'],
  sorts: ['评分']
})
const query = reactive({ source: 'tmdb', keyword: '', category: '全部', genre: '全部', region: '全部', year: '全部', sort: '评分' })
const items = ref([])
const total = ref(0)
const page = ref(1)
const loading = ref(false)
const transferModal = ref(false)
const activeItem = reactive({ title: '', tmdb_id: '' })
const transferForm = reactive({ link: '', code: '', target: '' })
const submitting = ref(false)

function switchSource(key) {
  query.source = key
  page.value = 1
  search()
}

function reset() {
  query.keyword = ''
  search()
}

function resetAll() {
  query.keyword = ''
  query.category = '全部'
  query.genre = '全部'
  query.region = '全部'
  query.year = '全部'
  query.sort = '评分'
  page.value = 1
  search()
}

async function search() {
  loading.value = true
  try {
    const params = new URLSearchParams({
      source: query.source,
      keyword: query.keyword,
      category: query.category,
      genre: query.genre,
      region: query.region,
      year: query.year,
      sort: query.sort,
      page: page.value
    })
    const data = await api.get('/api/discovery?' + params.toString())
    items.value = data.items || []
    total.value = data.total || 0
  } catch (e) {
    toast('error', '加载失败', e.message)
  } finally {
    loading.value = false
  }
}

async function loadMeta() {
  try {
    const data = await api.get('/api/meta/sources')
    meta.categories = data.categories
    meta.genres = data.genres
    meta.regions = data.regions
    meta.years = data.years
    meta.sorts = data.sorts
  } catch {
    /* 使用默认筛选项 */
  }
}

function openTransfer(item) {
  activeItem.title = item.title
  activeItem.tmdb_id = item.tmdb_id
  transferForm.link = ''
  transferForm.code = ''
  transferForm.target = ''
  transferModal.value = true
}

async function submitTransfer() {
  if (!transferForm.link.trim()) {
    toast('warn', '参数缺失', '请粘贴分享链接')
    return
  }
  submitting.value = true
  try {
    const row = await api.post('/api/transfer/submit', {
      link: transferForm.link.trim(),
      code: transferForm.code.trim(),
      target_dir: transferForm.target.trim(),
      channel: '影视探索检索'
    })
    toast('success', '已提交转存', `${row.drive_name || row.drive} → ${row.target_dir}（${row.status}）`)
    transferModal.value = false
  } catch (e) {
    toast('error', '转存失败', e.message)
  } finally {
    submitting.value = false
  }
}

onMounted(async () => {
  await loadMeta()
  await search()
})
</script>
