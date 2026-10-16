// 轻量 API 封装：统一注入 Bearer Token，401 自动跳回登录页。

let token = sessionStorage.getItem('tgto_token') || ''

export const api = {
  get token() {
    return token
  },
  setToken(t) {
    token = t || ''
    if (token) {
      sessionStorage.setItem('tgto_token', token)
    } else {
      sessionStorage.removeItem('tgto_token')
    }
  },

  async request(method, path, body) {
    const headers = { 'Content-Type': 'application/json' }
    if (token) headers['Authorization'] = 'Bearer ' + token
    let resp
    try {
      resp = await fetch(path, {
        method,
        headers,
        body: body === undefined ? undefined : JSON.stringify(body)
      })
    } catch (e) {
      throw new Error('网络请求失败：' + e.message)
    }
    if (resp.status === 401) {
      api.setToken('')
      window.dispatchEvent(new Event('tgto:unauthorized'))
      throw new Error('登录已过期，请重新登录')
    }
    const data = await resp.json().catch(() => ({}))
    if (data.code !== 0) {
      throw new Error(data.message || ('请求失败 (' + resp.status + ')'))
    }
    return data.data
  },

  get(path) {
    return api.request('GET', path)
  },
  post(path, body) {
    return api.request('POST', path, body || {})
  },

  // 下载类接口：带鉴权拉取 Blob 后触发浏览器下载
  async download(path, filename) {
    const headers = {}
    if (token) headers['Authorization'] = 'Bearer ' + token
    const resp = await fetch(path, { headers })
    if (!resp.ok) throw new Error('下载失败 (' + resp.status + ')')
    const blob = await resp.blob()
    const url = URL.createObjectURL(blob)
    const a = document.createElement('a')
    a.href = url
    a.download = filename
    document.body.appendChild(a)
    a.click()
    a.remove()
    URL.revokeObjectURL(url)
  }
}
