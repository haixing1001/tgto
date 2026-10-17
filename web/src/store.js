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
  driveStatus: {},
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
  try { sessionStorage.setItem('tgto_username', data.username || '') } catch (e) {}
  await bootstrap()
  return data
}

// 刷新页面后恢复会话：token 存在 sessionStorage（api.js 管理），
// 这里把它接回 state 并重新拉取一次全局状态；失效则回到登录页。
export async function restoreSession() {
  const saved = api.token
  if (!saved) return false
  state.token = saved
  try {
    await bootstrap()
    try { state.username = sessionStorage.getItem('tgto_username') || '' } catch (e) {}
    // 恢复会话后直接落到上次停留的视图（地址栏仍保持 /），避免先渲染成登录页
    state.currentView = readLastView() || DEFAULT_VIEW
    return true
  } catch (e) {
    api.setToken('')
    state.token = ''
    state.username = ''
    state.currentView = 'login'
    return false
  }
}

// 会话失效（401）统一处理：清状态、清地址栏、回登录页
export function handleUnauthorized() {
  api.setToken('')
  state.token = ''
  state.username = ''
  state.currentView = 'login'
  try { sessionStorage.removeItem('tgto_last_view') } catch (e) {}
  window.history.replaceState({ view: 'login' }, '', ROOT_PATH)
}

export function logout() {
  api.post('/api/auth/logout').catch(() => {})
  api.setToken('')
  state.token = ''
  state.username = ''
  state.currentView = 'login'
  try { sessionStorage.removeItem('tgto_last_view') } catch (e) {}
  window.history.replaceState({ view: 'login' }, '', ROOT_PATH)
}

export async function bootstrap() {
  const data = await api.get('/api/state')
  state.config = data.config || {}
  state.driveStatus = data.drive_status || {}
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

// ---------- 视图切换（纯内存状态，地址栏始终保持站点根路径） ----------
// 视图名不再写进 URL：无论点哪个菜单（含 /drive-guangya-account 这类网盘子菜单），
// 地址栏都停在 /，靠 history.state + state.currentView 驱动，浏览器前进/后退仍可用。
const ROOT_PATH = '/'
const DEFAULT_VIEW = 'global-settings'

const BASE_VIEWS = [
  'login', 'global-settings', 'log-center', 'transfer-history', 'organize-history', 'utility-tools',
  'media-discovery', 'trending', 'calendar', 'basic-config'
]

// 网盘配置项已升入主导航：每个「网盘 × 子菜单」一个独立视图，由 driveDefs 生成
const VIEWS = [...BASE_VIEWS, ...driveNavItems().map((d) => d.route)]

// 从 location.pathname 提取视图名（/media-discovery -> media-discovery），仅用于兼容旧书签
function viewFromPath() {
  const p = location.pathname.replace(/^\/+/, '').replace(/\/+$/, '')
  return p
}

// 地址栏收敛：把 /drive-guangya-account、/login 之类残留路径还原成 /
function cleanAddressBar(view, replace = true) {
  if (location.pathname === ROOT_PATH) return
  const state_ = { view }
  if (replace) window.history.replaceState(state_, '', ROOT_PATH)
  else window.history.pushState(state_, '', ROOT_PATH)
}

// 上次停留的视图（刷新后仍回到原页面，但地址栏不暴露）
function readLastView() {
  try {
    const v = sessionStorage.getItem('tgto_last_view')
    return VIEWS.includes(v) ? v : ''
  } catch (e) {
    return ''
  }
}

function saveLastView(view) {
  try { sessionStorage.setItem('tgto_last_view', view) } catch (e) {}
}

export function navigate(view) {
  if (!VIEWS.includes(view)) view = DEFAULT_VIEW
  if (view === state.currentView) {
    cleanAddressBar(view)
    return
  }
  state.currentView = view
  saveLastView(view)
  window.history.pushState({ view }, '', ROOT_PATH)
}

export function initRouter(onView) {
  // 首次进入：优先沿用旧书签里的视图（兼容 /drive-115 扁平路由），其次用上次停留的视图
  const incoming = viewFromPath()
  const legacy = resolveDriveRoute(incoming) || incoming
  const initial = VIEWS.includes(legacy) ? legacy : (readLastView() || DEFAULT_VIEW)

  // 地址栏一律还原为 /，不再出现任何视图名
  window.history.replaceState({ view: initial }, '', ROOT_PATH)
  state.currentView = state.token ? initial : 'login'

  window.addEventListener('popstate', (e) => {
    const v = e && e.state && e.state.view
    const next = VIEWS.includes(v) ? v : DEFAULT_VIEW
    state.currentView = state.token ? next : 'login'
    if (state.token) saveLastView(next)
    cleanAddressBar(state.currentView)
    if (onView) onView(state.currentView)
  })

  if (onView) onView(state.currentView)
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
