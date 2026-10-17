// 路由 / 会话回归测试（node 直接执行，无需浏览器）：
//   npm test
// 覆盖：地址栏不暴露视图路径、旧深链收敛、刷新恢复登录、popstate 后退、登出与 401。
// 做法：用最小 DOM/History/sessionStorage 桩真实执行 src/store.js（不 mock 业务逻辑本身）。

const entries = [{ state: null, url: '/' }]
let idx = 0
const listeners = {}

globalThis.location = { pathname: '/drive-guangya-account', origin: 'http://127.0.0.1:9058' }
globalThis.window = {
  location: globalThis.location,
  history: {
    get state() { return entries[idx].state },
    pushState(state, _t, url) {
      entries.splice(idx + 1)
      entries.push({ state, url })
      idx++
      globalThis.location.pathname = url
    },
    replaceState(state, _t, url) {
      entries[idx] = { state, url }
      globalThis.location.pathname = url
    }
  },
  addEventListener(ev, fn) { (listeners[ev] ||= []).push(fn) },
  removeEventListener() {},
  dispatchEvent() {}
}
const kv = new Map([['tgto_token', 'tok-restored'], ['tgto_username', 'admin']])
globalThis.sessionStorage = {
  getItem: (k) => (kv.has(k) ? kv.get(k) : null),
  setItem: (k, v) => kv.set(k, String(v)),
  removeItem: (k) => kv.delete(k)
}
globalThis.document = {
  createElement: () => ({ style: {}, setAttribute() {}, appendChild() {} }),
  createTextNode: (t) => ({ text: t }),
  querySelector: () => null,
  head: { appendChild() {} },
  body: { appendChild() {} },
  documentElement: { classList: { add() {}, remove() {} } }
}
globalThis.fetch = async () => ({
  status: 200,
  json: async () => ({
    code: 0,
    data: {
      version: '9.9.9',
      config: { ui: { theme: 'dark' } },
      transfer_history: [],
      organize_history: [],
      logs: [],
      drive_status: { tianyi: { logged_in: true, account: '18912345678' } }
    }
  })
})

const { state, initRouter, navigate, restoreSession, logout, handleUnauthorized } = await import('../src/store.js')

const results = []
const check = (name, cond, extra = '') => results.push({ name, pass: !!cond, extra })

initRouter()
check('init 后地址栏为 /', location.pathname === '/', location.pathname)
check('无会话时进入登录页', state.currentView === 'login', state.currentView)

const ok = await restoreSession()
check('restoreSession 恢复会话', ok === true && state.token === 'tok-restored', `ok=${ok} token=${state.token}`)
check('恢复后不再停在登录页', state.currentView !== 'login', state.currentView)
check('bootstrap 写入 driveStatus', state.driveStatus?.tianyi?.logged_in === true, JSON.stringify(state.driveStatus))

location.pathname = '/drive-guangya-account'
initRouter()
check('旧深链视图被识别', state.currentView === 'drive-guangya-account', state.currentView)
check('旧深链地址栏收敛为 /', location.pathname === '/', location.pathname)

navigate('drive-139-account')
check('切换到网盘子菜单', state.currentView === 'drive-139-account', state.currentView)
check('网盘子菜单地址栏为 /', location.pathname === '/', location.pathname)

navigate('trending')
check('切换普通页地址栏为 /', location.pathname === '/', location.pathname)

entries.push({ state: { view: 'drive-139-account' }, url: '/' })
idx++
for (const fn of listeners.popstate || []) fn({ state: { view: 'drive-139-account' } })
check('popstate 后退切回上一视图', state.currentView === 'drive-139-account', state.currentView)
check('popstate 后地址栏仍为 /', location.pathname === '/', location.pathname)

logout()
check('登出后回登录页', state.currentView === 'login', state.currentView)
check('登出后地址栏为 /', location.pathname === '/', location.pathname)

state.token = 'tok-x'
handleUnauthorized()
check('401 后清空 token', state.token === '' && sessionStorage.getItem('tgto_token') === null, state.token)

for (const r of results) console.log(`${r.pass ? 'PASS' : 'FAIL'}  ${r.name}${r.extra ? '  [' + r.extra + ']' : ''}`)
const failed = results.filter((r) => !r.pass)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
process.exit(failed.length ? 1 : 0)
