// 四大网盘配置项的统一结构定义。
// 每个「网盘 × 配置项子菜单」对应左侧主导航的一个一级条目（独立 hash 路由），
// 页面仅渲染该子菜单下的折叠卡片，字段由 FieldRenderer 通用渲染。
//
// 路由模型（扁平化，子菜单升入主导航）：
//   主导航 route   : drive-<drive>-<submenu>  例：drive-139-account
//   旧路由（兼容） : drive-<drive>            例：drive-139 → 重定向到该网盘第一个子菜单

export const DRIVES = {
  cloud139: {
    key: 'cloud139',
    route: 'drive-139',
    name: '移动云盘',
    icon: '📱',
    color: '#0085d0',
    submenus: [
      {
        key: 'account',
        title: '移动云盘账号配置',
        cards: [
          {
            key: 'account',
            title: '移动云盘账号配置',
            desc: '中国移动云盘（139 / 和彩云）· 新的个人盘',
            fields: [
              { key: 'username', label: '登录账号', type: 'text', placeholder: '139 邮箱 / 手机号' },
              { key: 'password', label: '登录密码', type: 'password' },
              { key: 'mail_cookies', label: '邮箱 Cookie（可选）', type: 'textarea', span: 2, placeholder: 'mail.10086.cn 的 Cookie 头（key1=value1; key2=value2），账号密码登录时作为设备上下文' },
              { key: 'authorization', label: '授权令牌（可选，仅校验用）', type: 'password', span: 2, placeholder: 'yun.139.com 请求头 Authorization 里 Basic 之后的值' }
            ],
            note: '推荐「账号密码回退登录」：填写账号、密码后点下方「账号密码登录」。若返回风控错误，先在浏览器登录 mail.10086.cn，把含 Os_SSo_Sid 与 RMKEY 的完整 Cookie 贴到「邮箱 Cookie」再登录（自动走 Cookie 快速登录）。登录成功后令牌剩余不足 15 天自动续期，续期失败自动重登；也可只填授权令牌后点「校验令牌」。'
          }
        ]
      },
      {
        key: 'monitor',
        title: '移动云盘频道监控配置',
        cards: [
          {
            key: 'monitor',
            title: '移动云盘频道监控配置',
            fields: [
              { key: 'monitor_channels', label: '监控频道', type: 'tags', placeholder: '@Channel1, @Channel2', span: 2 },
              { key: 'qps', label: 'API 限速 QPS', type: 'number' },
              { key: 'burst', label: '突发 Burst', type: 'number' }
            ]
          }
        ]
      },
      {
        key: 'movie',
        title: '影视整理功能',
        cards: [
          {
            key: 'movie',
            title: '影视整理功能',
            fields: [
              { key: 'organize_enabled', label: '启用影视整理', type: 'switch' },
              { key: 'movie_dir', label: '电影归档目录', type: 'text' },
              { key: 'tv_dir', label: '剧集归档目录', type: 'text' },
              { key: 'local_organize_dir', label: '本地整理目录（沙盒）', type: 'text', span: 2, hint: '放入真实媒体文件后点击整理历史-立即整理' }
            ]
          }
        ]
      },
      {
        key: 'music',
        title: '音乐整理功能',
        cards: [
          {
            key: 'music',
            title: '音乐整理功能',
            fields: [
              { key: 'music_dir', label: '无损音乐归档目录', type: 'text' }
            ],
            note: '音乐按 MusicBrainz 规范重命名：{artist} - {album} (year) [FLAC]/{track} - {artist} - {title}.flac'
          }
        ]
      },
      {
        key: 'strm',
        title: 'STRM 生成',
        cards: [
          {
            key: 'strm',
            title: 'STRM 生成',
            fields: [
              { key: 'strm_enabled', label: '启用 STRM 生成', type: 'switch' },
              { key: 'local_media_dir', label: '本地媒体目录（源）', type: 'text' },
              { key: 'strm_dir', label: 'STRM 输出目录', type: 'text' },
              { key: 'strm_url_prefix', label: 'STRM 内容 URL 前缀', type: 'text', span: 2 }
            ],
            actions: [
              { key: 'strm_incremental', label: '立即增量生成 STRM', mode: 'incremental', type: 'primary' },
              { key: 'strm_full', label: '全量重建 STRM', mode: 'full', type: 'danger' }
            ]
          }
        ]
      },
      {
        key: 'share_strm',
        title: '分享 STRM 生成与整理',
        cards: [
          {
            key: 'share_strm',
            title: '分享 STRM 生成与整理',
            fields: [
              { key: 'share_strm_enabled', label: '启用分享 STRM', type: 'switch' }
            ],
            note: '分享 STRM 输出至 实用工具-移动云盘分享STRM失效检测 指定目录（默认 /app/strm/cloud139_share）。'
          }
        ]
      }
    ]
  },

  guangya: {
    key: 'guangya',
    route: 'drive-guangya',
    name: '光鸭云盘',
    icon: '🦆',
    color: '#f59e0b',
    // 登录方式：sms（短信验证码登录，token 持久化保存）
    loginMode: 'sms',
    submenus: [
      {
        key: 'account',
        title: '光鸭云盘账号配置',
        cards: [
          {
            key: 'account',
            title: '光鸭登录凭证',
            desc: '使用手机号短信验证码完成登录；登录成功后 access token 与 refresh token 会持久化保存。',
            fields: [
              { key: 'phone', label: '登录手机号', type: 'text', placeholder: '11 位手机号（保存后用于发送短信验证码）', span: 2 },
              { key: 'account', label: '备用账号（可选）', type: 'text' },
              { key: 'password', label: '备用密码（可选）', type: 'password' }
            ]
          }
        ]
      },
      {
        key: 'monitor',
        title: '频道监控配置',
        cards: [
          {
            key: 'monitor',
            title: '频道监控配置',
            fields: [
              { key: 'monitor_channels', label: '监控频道', type: 'tags', placeholder: '@Channel1, @Channel2', span: 2 }
            ]
          }
        ]
      },
      {
        key: 'transfer',
        title: '链接转存',
        cards: [
          {
            key: 'transfer',
            title: '链接转存',
            fields: [
              { key: 'transfer_target', label: '默认转存目录', type: 'folder', browseApi: 'guangya' }
            ]
          }
        ]
      },
      {
        key: 'rapid',
        title: '秒传JSON、PP与迅雷',
        cards: [
          {
            key: 'rapid',
            title: '秒传JSON、PP与迅雷',
            fields: [
              { key: 'rapid_json', label: '秒传 JSON 配置', type: 'textarea', span: 2, placeholder: '{"sign":"...","bucket":"..."}' },
              { key: 'pp_enabled', label: '启用 PP 解析', type: 'switch' },
              { key: 'xunlei_enabled', label: '启用迅雷链接', type: 'switch' }
            ]
          }
        ]
      },
      {
        key: 'offline',
        title: '离线设置',
        cards: [
          {
            key: 'offline',
            title: '离线设置',
            fields: [
              { key: 'offline_enabled', label: '启用离线下载', type: 'switch' }
            ]
          }
        ]
      },
      {
        key: 'files',
        title: '文件管理',
        cards: [
          {
            key: 'files',
            title: '光鸭云盘文件管理',
            desc: '浏览、新建、重命名、移动、复制、删除网盘文件，以及回收站还原。'
          }
        ]
      },
      {
        key: 'movie',
        title: '影视整理功能',
        cards: [
          {
            key: 'movie',
            title: '影视整理功能',
            fields: [
              { key: 'organize_enabled', label: '启用影视整理', type: 'switch' },
              { key: 'movie_dir', label: '电影归档目录', type: 'text' },
              { key: 'tv_dir', label: '剧集归档目录', type: 'text' },
              { key: 'local_organize_dir', label: '本地整理目录（沙盒）', type: 'text', span: 2 }
            ]
          }
        ]
      },
      {
        key: 'music',
        title: '音乐整理功能',
        cards: [
          {
            key: 'music',
            title: '音乐整理功能',
            fields: [
              { key: 'music_dir', label: '无损音乐归档目录', type: 'text' }
            ],
            note: '音乐按 MusicBrainz 规范重命名：{artist} - {album} (year) [FLAC]/{track} - {artist} - {title}.flac'
          }
        ]
      },
      {
        key: 'strm',
        title: 'STRM 生成',
        cards: [
          {
            key: 'strm',
            title: 'STRM 生成',
            fields: [
              { key: 'strm_enabled', label: '启用 STRM 生成', type: 'switch' },
              { key: 'local_media_dir', label: '本地媒体目录（源）', type: 'text' },
              { key: 'strm_dir', label: 'STRM 输出目录', type: 'text' },
              { key: 'strm_url_prefix', label: 'STRM 内容 URL 前缀', type: 'text', span: 2 }
            ],
            actions: [
              { key: 'strm_incremental', label: '立即增量生成 STRM', mode: 'incremental', type: 'primary' },
              { key: 'strm_full', label: '全量重建 STRM', mode: 'full', type: 'danger' }
            ]
          }
        ]
      },
      {
        key: 'extract',
        title: '小号媒体信息提取',
        cards: [
          {
            key: 'extract',
            title: '小号媒体信息提取',
            fields: [
              { key: 'extract_enabled', label: '启用小号媒体信息提取', type: 'switch' }
            ],
            note: '启用后转存任务将同步提取小号分享的媒体基础信息（片名 / 季集 / 体积）。'
          }
        ]
      }
    ]
  },

  tianyi: {
    key: 'tianyi',
    route: 'drive-tianyi',
    name: '天翼云盘',
    icon: '☁️',
    color: '#ef4444',
    submenus: [
      {
        key: 'account',
        title: '天翼账号配置',
        cards: [
          {
            key: 'account',
            title: '天翼账号配置',
            desc: '扫码登录优先',
            note: '推荐直接扫码登录（下方面板）。账号密码字段为可选项，仅在扫码登录失败时用于兜底。',
            fields: [
              { key: 'account', label: '登录账号（可选）', type: 'text' },
              { key: 'password', label: '登录密码（可选）', type: 'password' }
            ]
          }
        ]
      },
      {
        key: 'files',
        title: '文件管理',
        cards: [
          {
            key: 'files',
            title: '天翼云盘文件管理',
            desc: '浏览、新建、重命名、移动、复制、删除网盘文件，以及清空回收站。'
          }
        ]
      },
      {
        key: 'monitor',
        title: '频道监控配置',
        cards: [
          {
            key: 'monitor',
            title: '频道监控配置',
            fields: [
              { key: 'monitor_channels', label: '监控频道', type: 'tags', placeholder: '@Channel1, @Channel2', span: 2 },
              { key: 'target_dir', label: '默认转存目录', type: 'folder', browseApi: 'tianyi' }
            ]
          }
        ]
      }
    ]
  }
}

const DRIVE_PREFIX = { cloud139: 'drive-139', guangya: 'drive-guangya', tianyi: 'drive-tianyi' }
const PREFIX_DRIVE = Object.fromEntries(Object.entries(DRIVE_PREFIX).map(([d, p]) => [p, d]))

// 每个网盘的账号凭证字段：用于主导航条目的「就绪绿灯」判定
//   - single：任一字段非空即视为已配置
//   - all：所有字段均非空才视为已配置
const DRIVE_CREDENTIALS = {
  cloud139: { keys: ['username', 'password'], mode: 'all', alt: ['authorization'] },
  guangya: { keys: ['phone'], mode: 'single' },
  tianyi: { keys: ['account'], mode: 'single' }
}

// 依据当前结构定义补齐每个子菜单的 route / driveKey / navKey 等扁平化字段
for (const [driveKey, def] of Object.entries(DRIVES)) {
  def.driveKey = driveKey
  def.navKey = DRIVE_PREFIX[driveKey]
  def.credentials = DRIVE_CREDENTIALS[driveKey] || { keys: [], mode: 'single' }
  def.submenus.forEach((sub) => {
    sub.driveKey = driveKey
    sub.route = `${def.navKey}-${sub.key}`
    sub.navKey = sub.route
  })
}

// 网盘是否「就绪」（左侧菜单绿点依据）：
//   alt 为「替代凭证」：任一非空即视为已配置（移动云盘可用令牌单独登录）；
//   优先用后端 /api/state 返回的真实登录态 drive_status（天翼是扫码会话，
//   登录只写 account、不写 password，用凭证字段判断永远不亮）；
//   没有该字段时退回凭证配置判断（移动云盘账号密码 / 光鸭手机号）。
export function driveReady(driveKey, config, driveStatus) {
  const def = DRIVES[driveKey]
  if (!def) return false
  const st = (driveStatus || {})[driveKey]
  if (st && typeof st.logged_in === 'boolean') return st.logged_in
  const cfg = (config && config[driveKey]) || {}
  if ((def.credentials.alt || []).some((k) => !!cfg[k])) return true
  const keys = def.credentials.keys || []
  if (!keys.length) return false
  const filled = keys.filter((k) => !!cfg[k])
  return def.credentials.mode === 'all' ? filled.length === keys.length : filled.length > 0
}

// 全部扁平化的网盘配置项条目（用于生成路由白名单）
export function driveNavItems() {
  const out = []
  for (const def of Object.values(DRIVES)) {
    for (const sub of def.submenus) {
      out.push({ driveKey: def.driveKey, subKey: sub.key, route: sub.route, label: sub.title })
    }
  }
  return out
}

// 主导航「云盘支持」分组：每个网盘一个可展开条目，其子菜单为二级条目
export function driveGroups() {
  return Object.values(DRIVES).map((def) => ({
    driveKey: def.driveKey,
    route: def.navKey,
    name: def.name,
    icon: def.icon,
    items: def.submenus.map((sub) => ({
      key: sub.route,
      label: sub.title,
      icon: '·'
    }))
  }))
}

// 当前 route -> 网盘 section key（兼容旧扁平路由与新的 drive+submenu 路由）
export function driveFromRoute(view) {
  for (const def of Object.values(DRIVES)) {
    if (view === def.navKey) return def.driveKey
    if (view.startsWith(def.navKey + '-')) return def.driveKey
  }
  return 'cloud139'
}

// 当前 route -> 子菜单 key；旧扁平路由回落到该网盘的第一个子菜单
export function submenuFromRoute(view) {
  const drive = driveFromRoute(view)
  const def = DRIVES[drive]
  const prefix = def.navKey + '-'
  if (view.startsWith(prefix)) return view.slice(prefix.length)
  return def.submenus[0].key
}

// 旧扁平路由 -> 规范化的子菜单 route（用于 hash 重定向，去掉查询串与多余斜杠）
export function resolveDriveRoute(view) {
  if (!view) return null
  const clean = view.split('?')[0].replace(/\/+$/, '')
  const drive = driveFromRoute(clean)
  const def = DRIVES[drive]
  if (!def) return null
  if (clean === def.navKey) return def.submenus[0].route
  if (def.submenus.some((s) => s.route === clean)) return clean
  return null
}
