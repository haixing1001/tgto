<template>
  <div class="p-6">
    <h2 class="text-lg font-bold mb-1">实用工具</h2>
    <p class="text-xs text-slate-400 mb-5">命令别名、AI 识别、发布组白名单、TMDB 替代源与正则映射</p>

    <div class="grid grid-cols-1 md:grid-cols-2 xl:grid-cols-3 gap-4">
      <!-- 1. 命令调整 -->
      <div class="card p-5 flex flex-col">
        <div class="flex items-start justify-between">
          <div>
            <h3 class="text-sm font-semibold">⌨️ 命令调整</h3>
            <p class="text-xs text-slate-400 mt-1">Telegram Bot 快捷指令别名映射</p>
          </div>
        </div>
        <textarea v-model="tools.command_alias" class="input font-mono text-xs mt-3 flex-1" rows="4" placeholder="/tr=/transfer&#10;/ls=/list"></textarea>
        <div class="flex justify-end mt-3">
          <button class="btn-ghost text-xs" @click="saveTools">保存</button>
        </div>
      </div>

      <!-- 2. AI 辅助识别 -->
      <div class="card p-5 flex flex-col">
        <div>
          <h3 class="text-sm font-semibold">🤖 AI 辅助识别</h3>
          <p class="text-xs text-slate-400 mt-1">OpenAI 兼容接口配置</p>
        </div>
        <div class="mt-3 space-y-2 flex-1">
          <div><label class="label">Base URL</label><input v-model="tools.ai_base_url" class="input !py-1.5 text-xs" placeholder="https://api.openai.com/v1" /></div>
          <div><label class="label">Model 名称</label><input v-model="tools.ai_model" class="input !py-1.5 text-xs" placeholder="gpt-4o-mini" /></div>
          <div><label class="label">API Key</label><input v-model="tools.ai_api_key" type="password" class="input !py-1.5 text-xs" /></div>
        </div>
        <div class="flex justify-end mt-3">
          <button class="btn-ghost text-xs" @click="saveTools">保存</button>
        </div>
      </div>

      <!-- 3. 自定义发布组 -->
      <div class="card p-5 flex flex-col">
        <div>
          <h3 class="text-sm font-semibold">🏷️ 自定义发布组</h3>
          <p class="text-xs text-slate-400 mt-1">压制组 / 发布组白名单（逗号分隔）</p>
        </div>
        <input v-model="tools.custom_groups" class="input text-xs mt-3 flex-1" placeholder="FRDS, CMCT, WiKi, CHD, HHWEB" />
        <p class="text-[10px] text-slate-400 mt-2">不在白名单内的发布组将在重命名时自动过滤。</p>
        <div class="flex justify-end mt-3">
          <button class="btn-ghost text-xs" @click="saveTools">保存</button>
        </div>
      </div>

      <!-- 4. TMDB 替代源设置 -->
      <div class="card p-5 flex flex-col">
        <div>
          <h3 class="text-sm font-semibold">🖼️ TMDB 替代源设置</h3>
          <p class="text-xs text-slate-400 mt-1">API 与图片 CDN 镜像地址</p>
        </div>
        <div class="mt-3 space-y-2 flex-1">
          <div><label class="label">TMDB API 镜像</label><input v-model="tools.tmdb_api_base" class="input !py-1.5 text-xs" /></div>
          <div><label class="label">图片 CDN 镜像</label><input v-model="tools.tmdb_image_base" class="input !py-1.5 text-xs" /></div>
        </div>
        <div class="flex justify-end mt-3">
          <button class="btn-ghost text-xs" @click="saveTools">保存</button>
        </div>
      </div>

      <!-- 5. TMDB 正则映射 -->
      <div class="card p-5 flex flex-col">
        <div>
          <h3 class="text-sm font-semibold">🎯 TMDB 正则映射</h3>
          <p class="text-xs text-slate-400 mt-1">长篇动漫 / 剧集强制绑定 tmdbid 与 season</p>
        </div>
        <textarea v-model="rulesText" class="input font-mono text-xs mt-3 flex-1" rows="4" placeholder="每行一条：正则|tmdbid|season&#10;例：海贼王.*|649|1"></textarea>
        <div class="flex justify-end mt-3">
          <button class="btn-ghost text-xs" @click="saveRules">保存规则</button>
        </div>
      </div>

      <!-- 6. 移动云盘分享 STRM 失效检测 -->
      <div class="card p-5 flex flex-col md:col-span-2 xl:col-span-1">
        <div>
          <h3 class="text-sm font-semibold">🔎 移动云盘分享 STRM 失效检测</h3>
          <p class="text-xs text-slate-400 mt-1">扫描分享 STRM 目录并校验链接存活状态</p>
        </div>
        <div class="mt-3 flex-1">
          <label class="label">STRM 目录</label>
          <input v-model="tools.share_strm_path" class="input !py-1.5 text-xs" />
        </div>
        <div class="flex justify-end gap-2 mt-3">
          <button class="btn-ghost text-xs" @click="saveTools">保存</button>
          <button class="btn-primary text-xs" :disabled="checking" @click="runCheck">{{ checking ? '检测中...' : '开始检测' }}</button>
        </div>
        <div v-if="checkResult" class="mt-3 text-xs space-y-1">
          <div>共 <b>{{ checkResult.total }}</b> 个 .strm · 存活 <b class="text-emerald-500">{{ checkResult.alive }}</b> · 失效 <b class="text-rose-500">{{ checkResult.dead }}</b></div>
          <div class="max-h-32 overflow-y-auto font-mono text-[10px] text-slate-400 space-y-0.5">
            <div v-for="(d, i) in (checkResult.detail || []).slice(0, 50)" :key="i" class="truncate">
              <span :class="d.status === '存活' ? 'text-emerald-500' : 'text-rose-500'">[{{ d.status }}]</span> {{ d.file }}
            </div>
          </div>
        </div>
      </div>
    </div>
  </div>
</template>

<script setup>
import { onMounted, reactive, ref } from 'vue'
import { state, saveConfig, toast } from '../store.js'
import { api } from '../api.js'

const tools = reactive({
  command_alias: '',
  ai_base_url: '',
  ai_model: 'gpt-4o-mini',
  ai_api_key: '',
  custom_groups: '',
  tmdb_api_base: 'https://api.themoviedb.org',
  tmdb_image_base: 'https://image.tmdb.org',
  share_strm_path: '/app/strm/cloud139_share'
})
const rulesText = ref('')
const checking = ref(false)
const checkResult = ref(null)

onMounted(() => {
  const cfg = state.config.tools || {}
  Object.assign(tools, cfg)
  const rules = cfg.regex_rules || []
  rulesText.value = rules.map((r) => `${r.pattern || ''}|${r.tmdb_id || ''}|${r.season || ''}`).join('\n')
})

async function saveTools() {
  try {
    await saveConfig('tools', { ...tools })
    toast('success', '保存成功', '实用工具配置已生效')
  } catch (e) {
    toast('error', '保存失败', e.message)
  }
}


async function saveRules() {
  const rules = rulesText.value
    .split('\n')
    .map((line) => line.trim())
    .filter(Boolean)
    .map((line) => {
      const [pattern, tmdb_id, season] = line.split('|')
      return { pattern: (pattern || '').trim(), tmdb_id: (tmdb_id || '').trim(), season: Number(season || 0) }
    })
    .filter((r) => r.pattern)
  try {
    await saveConfig('tools', { regex_rules: rules })
    toast('success', '规则已保存', `${rules.length} 条强制映射规则生效`)
  } catch (e) {
    toast('error', '保存失败', e.message)
  }
}

async function runCheck() {
  checking.value = true
  try {
    await saveConfig('tools', { share_strm_path: tools.share_strm_path })
    checkResult.value = await api.post('/api/util/strm_check')
    toast('success', '检测完成', `存活 ${checkResult.value.alive} / 失效 ${checkResult.value.dead}`)
  } catch (e) {
    toast('error', '检测失败', e.message)
  } finally {
    checking.value = false
  }
}
</script>
