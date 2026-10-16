// 全局响应式状态（轻量 Store，替代 Vuex/Pinia）
import { reactive } from 'vue'
import { api } from './api.js'
import { driveNavItems, resolveDriveRoute } from './driveDefs.js'

export const state = reactive({
  token: '',
  username: '',
  version: '0.0.0',
  config: {},
  transferHistory: [],
  organizeHistory: [],
  logs: [],
  currentView: 'login',
  theme: 'dark',
  firstRunBannerClosed: false,
  logAutoRefresh: false,
  toasts: [],
  loading: {}
})

// ---------- 主题 ----------
export function applyTheme(theme) {
  state.theme = theme === 'light' ? 'light' : 'dark'
  if (state.theme === 'dark') {
    document.documentElement.classList.add('dark')
  } else {
    document.documentElement.classList.remove('dark')
  }
}

export function toggleTheme() {
  const next = state.theme === 'dark' ? 'light' : 'dark'
  applyTheme(next)
  saveConfig('ui', { theme: next })
}

// ---------- 提示 ----------
export function toast(type, title, message) {
  const id = Date.now() + Math.random()
  state.toasts.push({ id, type, title, message })
  setTimeout(() => {
    const i = state.toasts.findIndex((t) => t.id === id)
    if (i >= 0) state.toasts.splice(i, 1)
  }, 3600)
}

// ---------- 登录 / 状态 ----------
export async function login(username, password) {
  const data = await api.post('/api/auth/login', { username, password })
  api.setToken(data.token)
  state.token = data.token
  state.username = data.username
  state.version = data.version
  await bootstrap()
  return data
}

export function logout() {
  api.post('/api/auth/logout').catch(() => {})
  api.setToken('')
  state.token = ''
  state.username = ''
  state.currentView = 'login'
  window.history.pushState({ view: 'login' }, '', '/login')
}

export async function bootstrap() {
  const data = await api.get('/api/state')
  state.config = data.config || {}
  state.transferHistory = data.transfer_history || []
  state.organizeHistory = data.organize_history || []
  state.logs = data.logs || []
  state.version = data.version || state.version
  applyTheme((state.config.ui || {}).theme || 'dark')
}

export async function saveConfig(section, patch) {
  const saved = await api.post('/api/config/' + section, patch)
  state.config[section] = saved
  return saved
}

export async function refreshLogs(module = '') {
  const data = await api.get('/api/logs?module=' + encodeURIComponent(module) + '&limit=300')
  state.logs = data.logs || []
  return data
}

// ---------- 路由（History API 路径驱动，无 hash、无 vue-router） ----------
const BASE_VIEWS = [
  'login', 'global-settings', 'log-center', 'transfer-history', 'organize-history', 'utility-tools',
  'media-discovery', 'trending', 'calendar', 'basic-config'
]

// 网盘配置项已升入主导航：每个「网盘 × 子菜单」一个独立路由，由 driveDefs 生成
const VIEWS = [...BASE_VIEWS, ...driveNavItems().map((d) => d.route)]

// 从 location.pathname 提取视图名（/media-discovery -> media-discovery）
function viewFromPath() {
  const p = location.pathname.replace(/^\/+/, '').replace(/\/+$/, '')
  return p
}

export function navigate(view) {
  if (!VIEWS.includes(view)) view = 'global-settings'
  window.history.pushState({ view }, '', '/' + view)
  state.currentView = view
}

export function initRouter(onView) {
  const sync = () => {
    let view = viewFromPath()
    // 兼容旧扁平路由（如 /drive-123）→ 重定向到对应网盘的第一个配置项
    const legacy = resolveDriveRoute(view)
    if (legacy && legacy !== view) {
      window.history.replaceState({ view: legacy }, '', '/' + legacy)
      view = legacy
    }
    const resolved = VIEWS.includes(view) ? view : (state.token ? 'global-settings' : 'login')
    state.currentView = state.token ? resolved : 'login'
    if (onView) onView(state.currentView)
  }
  window.addEventListener('popstate', sync)
  sync()
}

// ---------- 工具 ----------
export function fmtTime(iso) {
  if (!iso) return '-'
  const d = new Date(iso)
  if (isNaN(d.getTime())) return iso
  const p = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${p(d.getMonth() + 1)}-${p(d.getDate())} ${p(d.getHours())}:${p(d.getMinutes())}:${p(d.getSeconds())}`
}

export function statusBadgeClass(status) {
  switch (status) {
    case '成功':
      return 'bg-emerald-100 text-emerald-700 dark:bg-emerald-500/15 dark:text-emerald-400'
    case '失败':
      return 'bg-rose-100 text-rose-700 dark:bg-rose-500/15 dark:text-rose-400'
    case '跳过':
      return 'bg-amber-100 text-amber-700 dark:bg-amber-500/15 dark:text-amber-400'
    case '已回退':
      return 'bg-slate-200 text-slate-600 dark:bg-slate-600/30 dark:text-slate-300'
    default:
      return 'bg-slate-100 text-slate-600 dark:bg-slate-700 dark:text-slate-300'
  }
}
