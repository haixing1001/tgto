<template>
  <div class="p-6">
    <!-- 顶部：面包屑 + 配置项搜索定位 + 保存 -->
    <div class="flex items-center gap-3 mb-4">
      <div class="flex items-center gap-1.5 text-xs text-slate-400 min-w-0">
        <span>{{ def.icon }} {{ def.name }}</span>
        <span class="text-slate-300 dark:text-slate-600">/</span>
        <span class="text-slate-600 dark:text-slate-300 font-medium truncate">{{ currentSubmenu.title }}</span>
      </div>
      <div class="relative flex-1 max-w-xs">
        <input
          v-model="search"
          class="input !py-1.5 text-xs"
          placeholder="搜索并定位配置项"
          @focus="showSearch = true"
          @blur="delayHideSearch"
        />
        <div v-if="showSearch && searchResults.length" class="absolute z-20 mt-1 w-full card shadow-lg max-h-64 overflow-y-auto">
          <div
            v-for="r in searchResults"
            :key="r.cardKey + '/' + (r.field || '')"
            class="px-3 py-2 text-xs cursor-pointer hover:bg-slate-50 dark:hover:bg-slate-700/60"
            @mousedown.prevent="locate(r)"
          >
            <span>{{ r.card }}</span>
            <template v-if="r.field"><span class="mx-1 text-slate-300">/</span><span class="text-brand-500">{{ r.field }}</span></template>
          </div>
        </div>
      </div>
      <div class="flex-1"></div>
      <button class="btn-primary" :disabled="saving" @click="save">{{ saving ? '保存中...' : '保存修改' }}</button>
    </div>

    <!-- 折叠卡片（当前子菜单内容直接平铺） -->
    <div class="space-y-4">
      <section v-for="card in currentSubmenu.cards" :id="'card-' + card.key" :key="card.key" class="card overflow-hidden">
        <div class="flex items-center justify-between px-5 py-3 cursor-pointer select-none" @click="toggleCard(card.key)">
          <div class="flex items-center gap-2">
            <h3 class="text-sm font-semibold">{{ card.title }}</h3>
            <span v-if="card.desc" class="text-[11px] text-slate-400">{{ card.desc }}</span>
          </div>
          <span class="text-slate-400 text-xs shrink-0">{{ expanded[card.key] === false ? '展开 ▸' : '收起 ▾' }}</span>
        </div>
        <div v-show="expanded[card.key] !== false" class="px-5 pb-5 pt-1">
          <div class="grid grid-cols-1 md:grid-cols-2 gap-x-4">
            <FieldRenderer
              v-for="field in card.fields"
              :key="field.key"
              v-model="local[field.key]"
              :field="field"
              @browse="onFieldBrowse"
            />
          </div>
          <p v-if="card.note" class="text-[11px] text-slate-400 mt-2">{{ card.note }}</p>

          <!-- 光鸭短信验证码登录面板（仅光鸭账号配置卡片展示） -->
          <div v-if="isGuangyaAccountCard(card)" class="gy-login-panel">
            <div class="gy-login-panel-title">短信验证码登录获取光鸭 Token</div>
            <div class="gy-login-warning">
              登录成功后会保存 access token、refresh token、设备 ID 和过期时间；后续光鸭接口请求会在过期前自动刷新 token。
            </div>
            <div class="gy-login-controls">
              <input
                v-model="gyCode"
                class="gy-login-input"
                type="text"
                placeholder="短信验证码"
                autocomplete="one-time-code"
                inputmode="numeric"
                @keyup.enter="gyLogin"
              />
              <button type="button" class="gy-login-btn" :disabled="gyBusy === 'send'" @click="gySendCode">
                {{ gyBusy === 'send' ? '发送中...' : '发送验证码' }}
              </button>
              <button type="button" class="gy-login-btn" :disabled="gyBusy === 'login'" @click="gyLogin">
                {{ gyBusy === 'login' ? '登录中...' : '验证并登录' }}
              </button>
              <button type="button" class="gy-login-btn-stop" :disabled="gyBusy === 'refresh'" @click="gyRun('refresh')">
                {{ gyBusy === 'refresh' ? '刷新中...' : '刷新 Token' }}
              </button>
              <button type="button" class="gy-login-btn-stop" :disabled="gyBusy === 'verify'" @click="gyRun('verify')">
                {{ gyBusy === 'verify' ? '校验中...' : '校验 Token' }}
              </button>
              <button type="button" class="gy-login-btn-stop" :disabled="gyBusy === 'clear'" @click="gyRun('clear')">
                {{ gyBusy === 'clear' ? '清除中...' : '清除登录' }}
              </button>
            </div>
            <div class="gy-login-status" :class="gyStatus.logged_in ? 'ok' : 'idle'">{{ gyStatus.status }}</div>
          </div>

          <!-- 天翼云盘扫码登录面板（仅天翼账号配置卡片展示） -->
          <TianyiQrLogin v-if="isTianyiAccountCard(card)" ref="tyQrPanel" />
          <Yun139LoginPanel v-if="isYun139AccountCard(card)" ref="y139Panel" />

          <!-- 移动云盘文件管理面板 -->
          <Yun139FileManager v-if="isYun139FilesCard(card)" />

          <!-- 天翼云盘文件管理面板（仅天翼文件管理卡片展示） -->
          <div v-if="isTianyiFilesCard(card)" class="ty-fm">
            <div class="ty-fm-toolbar">
              <div class="flex items-center gap-1 text-xs min-w-0">
                <button class="ty-fm-crumb" :disabled="tyFm.pathStack.length <= 1" @click="tyFmGoUp">⬆ 上级</button>
                <span v-for="(c, i) in tyFm.pathStack" :key="i" class="flex items-center gap-1 min-w-0">
                  <span class="text-slate-300 dark:text-slate-600">/</span>
                  <button class="ty-fm-crumb truncate" @click="tyFmJump(i)">{{ c.name }}</button>
                </span>
              </div>
              <div class="flex-1"></div>
              <button class="ty-fm-btn" @click="tyFmMkdir">新建文件夹</button>
              <button class="ty-fm-btn-stop" @click="tyFmLoad">{{ tyFm.loading ? '刷新中...' : '刷新' }}</button>
            </div>

            <div v-if="tyFm.error" class="ty-fm-error">{{ tyFm.error }}</div>

            <div class="ty-fm-table-wrap">
              <table class="ty-fm-table">
                <thead>
                  <tr>
                    <th class="w-8"><input type="checkbox" :checked="tyFmAllChecked" @change="tyFmToggleAll($event.target.checked)" /></th>
                    <th>名称</th>
                    <th class="w-24">大小</th>
                    <th class="w-40">修改时间</th>
                    <th class="w-44">操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="tyFm.loading"><td colspan="5" class="ty-fm-empty">加载中...</td></tr>
                  <tr v-else-if="!tyFm.items.length"><td colspan="5" class="ty-fm-empty">暂无文件</td></tr>
                  <tr v-for="it in tyFm.items" :key="it.id">
                    <td><input type="checkbox" :value="it.id" v-model="tyFm.checked" /></td>
                    <td>
                      <button v-if="it.isDir" class="ty-fm-name ty-fm-dir" @click="tyFmEnter(it)">📁 {{ it.name }}</button>
                      <span v-else class="ty-fm-name">📄 {{ it.name }}</span>
                    </td>
                    <td class="text-slate-400">{{ it.isDir ? '—' : tyFmSize(it.size) }}</td>
                    <td class="text-slate-400">{{ tyFmTime(it.updatedAt) }}</td>
                    <td class="ty-fm-ops">
                      <button class="ty-fm-op" @click="tyFmRename(it)">重命名</button>
                      <button class="ty-fm-op" @click="tyFmMove(it)">移动</button>
                      <button class="ty-fm-op" @click="tyFmCopy(it)">复制</button>
                      <button v-if="!it.isDir" class="ty-fm-op" @click="tyFmDownload(it)">下载</button>
                      <button class="ty-fm-op ty-fm-op-danger" @click="tyFmDelete(it)">删除</button>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div class="ty-fm-foot">
              <span v-if="tyFm.checked.length" class="text-brand-600 dark:text-brand-500 font-medium">已选 {{ tyFm.checked.length }} 项</span>
              <span v-else class="text-slate-400">共 {{ tyFm.items.length }} 项</span>
              <div class="flex-1"></div>
              <button v-if="tyFm.checked.length" class="text-xs ty-fm-btn-danger" @click="tyFmBulkDelete">一键删除所选 {{ tyFm.checked.length }} 项</button>
              <button v-else class="ty-fm-btn-stop" disabled>一键删除所选</button>
              <button class="ty-fm-btn-stop" @click="tyFmClearRecycle">清空回收站</button>
            </div>
          </div>

          <!-- 光鸭文件管理面板（仅光鸭文件管理卡片展示） -->
          <div v-if="isGuangyaFilesCard(card)" class="gy-fm">
            <div class="gy-fm-toolbar">
              <div class="flex items-center gap-1 text-xs min-w-0">
                <button class="gy-fm-crumb" :disabled="gyFm.pathStack.length <= 1" @click="gyFmGoUp">⬆ 上级</button>
                <span v-for="(c, i) in gyFm.pathStack" :key="i" class="flex items-center gap-1 min-w-0">
                  <span class="text-slate-300 dark:text-slate-600">/</span>
                  <button class="gy-fm-crumb truncate" @click="gyFmJump(i)">{{ c.name }}</button>
                </span>
              </div>
              <div class="flex-1"></div>
              <label class="gy-fm-toggle"><input type="checkbox" v-model="gyFm.recycle" @change="gyFmLoad" /> 回收站</label>
              <button class="gy-fm-btn" @click="gyFmMkdir">新建文件夹</button>
              <button class="gy-fm-btn-stop" @click="gyFmLoad">{{ gyFm.loading ? '刷新中...' : '刷新' }}</button>
            </div>

            <div v-if="gyFm.error" class="gy-fm-error">{{ gyFm.error }}</div>

            <div class="gy-fm-table-wrap">
              <table class="gy-fm-table">
                <thead>
                  <tr>
                    <th class="w-8"><input type="checkbox" :checked="gyFmAllChecked" @change="gyFmToggleAll($event.target.checked)" /></th>
                    <th>名称</th>
                    <th class="w-24">大小</th>
                    <th class="w-40">修改时间</th>
                    <th class="w-44">操作</th>
                  </tr>
                </thead>
                <tbody>
                  <tr v-if="gyFm.loading"><td colspan="5" class="gy-fm-empty">加载中...</td></tr>
                  <tr v-else-if="!gyFm.items.length"><td colspan="5" class="gy-fm-empty">暂无文件</td></tr>
                  <tr v-for="it in gyFm.items" :key="it.id">
                    <td><input type="checkbox" :value="it.id" v-model="gyFm.checked" /></td>
                    <td>
                      <button v-if="it.isDir" class="gy-fm-name gy-fm-dir" @click="gyFmEnter(it)">📁 {{ it.name }}</button>
                      <span v-else class="gy-fm-name">📄 {{ it.name }}</span>
                    </td>
                    <td class="text-slate-400">{{ it.isDir ? '—' : gyFmSize(it.size) }}</td>
                    <td class="text-slate-400">{{ gyFmTime(it.updatedAt) }}</td>
                    <td class="gy-fm-ops">
                      <button class="gy-fm-op" @click="gyFmRename(it)">重命名</button>
                      <button class="gy-fm-op" @click="gyFmMove(it)">移动</button>
                      <button v-if="!gyFm.recycle" class="gy-fm-op" @click="gyFmCopy(it)">复制</button>
                      <template v-if="!gyFm.recycle">
                        <button class="gy-fm-op" @click="gyFmDownload(it)">下载</button>
                        <button class="gy-fm-op gy-fm-op-danger" @click="gyFmDelete(it)">删除</button>
                      </template>
                      <template v-else>
                        <button class="gy-fm-op" @click="gyFmRestore(it)">还原</button>
                      </template>
                    </td>
                  </tr>
                </tbody>
              </table>
            </div>

            <div class="gy-fm-foot">
              <span v-if="gyFm.checked.length" class="text-brand-600 dark:text-brand-500 font-medium">已选 {{ gyFm.checked.length }} 项</span>
              <span v-else class="text-slate-400">共 {{ gyFm.items.length }} 项</span>
              <div class="flex-1"></div>
              <template v-if="!gyFm.recycle">
                <button v-if="gyFm.checked.length" class="text-xs gy-fm-btn-danger" @click="gyFmBulkDelete">一键删除所选 {{ gyFm.checked.length }} 项</button>
                <button v-else class="gy-fm-btn-stop" disabled>一键删除所选</button>
              </template>
              <template v-else>
                <button v-if="gyFm.checked.length" class="text-xs btn-primary" @click="gyFmBulkRestore">一键还原所选 {{ gyFm.checked.length }} 项</button>
                <button v-else class="gy-fm-btn-stop" disabled>一键还原所选</button>
                <button class="gy-fm-btn-danger" @click="gyFmClearRecycle">清空回收站</button>
              </template>
            </div>
          </div>

          <div v-if="card.actions" class="flex flex-wrap gap-2 mt-4 pt-3 border-t border-slate-100 dark:border-slate-700">
            <button
              v-for="action in card.actions"
              :key="action.key"
              class="text-xs"
              :class="action.type === 'danger' ? 'btn-danger' : 'btn-primary'"
              :disabled="strmRunning === action.key"
              @click="runAction(action)"
            >
              {{ strmRunning === action.key ? '生成中...' : action.label }}
            </button>
          </div>
          <div v-if="strmResult" class="mt-3 text-xs text-slate-500 bg-slate-50 dark:bg-slate-800/60 rounded-lg p-3">
            最近一次生成（{{ strmResult.mode === 'full' ? '全量重建' : '增量' }}）：共 {{ strmResult.total }} 个媒体文件，
            新增 {{ strmResult.generated }}，跳过 {{ strmResult.skipped }}，失败 {{ strmResult.failed }}，
            字幕 {{ strmResult.subtitles }}，海报 {{ strmResult.posters }}，耗时 {{ strmResult.duration_ms }}ms。
          </div>
        </div>
      </section>
    </div>

    <!-- 光鸭文件管理弹窗 -->
    <ModalDialog v-model="gyFm.dialog" :title="gyFm.dialogTitle" width="max-w-md">
      <div v-if="gyFm.dialogKind === 'mkdir' || gyFm.dialogKind === 'rename'" class="space-y-3">
        <div>
          <label class="label">{{ gyFm.dialogKind === 'mkdir' ? '文件夹名称' : '新名称' }}</label>
          <input v-model="gyFm.dialogInput" class="input text-sm" :placeholder="gyFm.dialogKind === 'mkdir' ? '请输入文件夹名称' : '请输入新名称'" @keyup.enter="gyFmDialogConfirm" />
        </div>
      </div>
      <div v-else-if="gyFm.dialogKind === 'move' || gyFm.dialogKind === 'copy'" class="space-y-3">
        <div>
          <label class="label">目标目录 ID（留空为根目录）</label>
          <input v-model="gyFm.dialogInput" class="input text-sm" placeholder="目标父目录 ID，留空转存到根目录" />
          <p class="text-[11px] text-slate-400 mt-1">可进入目标文件夹后从面包屑复制其 ID，或直接留空选择根目录。</p>
        </div>
      </div>
      <div v-else-if="gyFm.dialogKind === 'delete'" class="text-sm text-slate-600 dark:text-slate-300">
        <p>确定删除所选 {{ gyFm.checked.length }} 项吗？普通文件将移入回收站。</p>
        <ul v-if="gyFmCheckedNames.length" class="mt-2 max-h-32 overflow-y-auto space-y-0.5 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(n, i) in gyFmCheckedNames.slice(0, 5)" :key="i" class="truncate">{{ n }}</li>
          <li v-if="gyFmCheckedNames.length > 5" class="text-slate-400">……等共 {{ gyFmCheckedNames.length }} 项</li>
        </ul>
      </div>
      <div v-else-if="gyFm.dialogKind === 'restore'" class="text-sm text-slate-600 dark:text-slate-300">
        <p>确定还原所选 {{ gyFm.checked.length }} 项吗？</p>
        <ul v-if="gyFmCheckedNames.length" class="mt-2 max-h-32 overflow-y-auto space-y-0.5 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(n, i) in gyFmCheckedNames.slice(0, 5)" :key="i" class="truncate">{{ n }}</li>
          <li v-if="gyFmCheckedNames.length > 5" class="text-slate-400">……等共 {{ gyFmCheckedNames.length }} 项</li>
        </ul>
      </div>
      <div v-else-if="gyFm.dialogKind === 'clearRecycle'" class="text-sm text-slate-600 dark:text-slate-300">
        确定清空回收站吗？此操作将永久删除回收站内所有文件，不可恢复。
      </div>
      <template #footer>
        <button class="btn-ghost text-xs" @click="gyFm.dialog = false">取消</button>
        <button
          class="text-xs"
          :class="gyFm.dialogKind === 'delete' || gyFm.dialogKind === 'clearRecycle' ? 'btn-danger' : 'btn-primary'"
          :disabled="gyFm.dialogBusy"
          @click="gyFmDialogConfirm"
        >
          {{ gyFm.dialogBusy ? '处理中...' : '确定' }}
        </button>
      </template>
    </ModalDialog>

    <!-- 天翼云盘文件管理弹窗 -->
    <ModalDialog v-model="tyFm.dialog" :title="tyFm.dialogTitle" width="max-w-md">
      <div v-if="tyFm.dialogKind === 'mkdir' || tyFm.dialogKind === 'rename'" class="space-y-3">
        <div>
          <label class="label">{{ tyFm.dialogKind === 'mkdir' ? '文件夹名称' : '新名称' }}</label>
          <input v-model="tyFm.dialogInput" class="input text-sm" :placeholder="tyFm.dialogKind === 'mkdir' ? '请输入文件夹名称' : '请输入新名称'" @keyup.enter="tyFmDialogConfirm" />
        </div>
      </div>
      <div v-else-if="tyFm.dialogKind === 'move' || tyFm.dialogKind === 'copy'" class="space-y-3">
        <div>
          <label class="label">目标目录 ID（留空为个人云根目录）</label>
          <input v-model="tyFm.dialogInput" class="input text-sm" placeholder="目标父目录 ID，留空转存到根目录" />
          <p class="text-[11px] text-slate-400 mt-1">可进入目标文件夹后从面包屑复制其 ID，或直接留空选择根目录（-11）。</p>
        </div>
      </div>
      <div v-else-if="tyFm.dialogKind === 'delete'" class="text-sm text-slate-600 dark:text-slate-300">
        <p>确定删除所选 {{ tyFm.checked.length }} 项吗？文件将移入回收站。</p>
        <ul v-if="tyFmCheckedNames.length" class="mt-2 max-h-32 overflow-y-auto space-y-0.5 text-xs text-slate-500 dark:text-slate-400">
          <li v-for="(n, i) in tyFmCheckedNames.slice(0, 5)" :key="i" class="truncate">{{ n }}</li>
          <li v-if="tyFmCheckedNames.length > 5" class="text-slate-400">……等共 {{ tyFmCheckedNames.length }} 项</li>
        </ul>
      </div>
      <div v-else-if="tyFm.dialogKind === 'clearRecycle'" class="text-sm text-slate-600 dark:text-slate-300">
        确定清空回收站吗？此操作将永久删除回收站内所有文件，不可恢复。
      </div>
      <template #footer>
        <button class="btn-ghost text-xs" @click="tyFm.dialog = false">取消</button>
        <button
          class="text-xs"
          :class="tyFm.dialogKind === 'delete' || tyFm.dialogKind === 'clearRecycle' ? 'btn-danger' : 'btn-primary'"
          :disabled="tyFm.dialogBusy"
          @click="tyFmDialogConfirm"
        >
          {{ tyFm.dialogBusy ? '处理中...' : '确定' }}
        </button>
      </template>
    </ModalDialog>

    <!-- 文件夹浏览选择弹窗（用于配置项"默认转存目录"等） -->
    <ModalDialog v-model="folderPick.dialog" :title="folderPick.title" width="max-w-lg">
      <div class="space-y-3">
        <!-- 面包屑 -->
        <div class="flex items-center gap-1 flex-wrap text-xs text-slate-500 dark:text-slate-400">
          <template v-for="(c, i) in folderPick.pathStack" :key="i">
            <button
              class="hover:text-brand-500"
              :class="i === folderPick.pathStack.length - 1 ? 'font-medium text-brand-500' : ''"
              @click="folderPickJump(i)"
            >{{ c.name }}</button>
            <span v-if="i < folderPick.pathStack.length - 1">/</span>
          </template>
        </div>

        <!-- 加载/错误提示 -->
        <div v-if="folderPick.loading" class="text-xs text-slate-400 py-4 text-center">加载中...</div>
        <div v-else-if="folderPick.error" class="text-xs text-red-500 py-2">{{ folderPick.error }}</div>

        <!-- 文件夹列表 -->
        <div v-else class="max-h-64 overflow-y-auto border border-slate-200 dark:border-slate-700 rounded">
          <div v-if="!folderPick.items.length" class="text-xs text-slate-400 py-4 text-center">暂无子文件夹</div>
          <button
            v-for="it in folderPick.items"
            :key="it.id"
            class="w-full text-left px-3 py-1.5 text-sm hover:bg-slate-50 dark:hover:bg-slate-800 flex items-center gap-2"
            @click="folderPickEnter(it)"
          >
            <span>📁</span>
            <span class="truncate">{{ it.name }}</span>
          </button>
        </div>

        <!-- 当前选择 -->
        <div class="text-xs text-slate-500 dark:text-slate-400">
          当前目录：<span class="font-mono">{{ folderPickCurrentPath() }}</span>
        </div>
      </div>
      <template #footer>
        <button class="btn-ghost text-xs" @click="folderPick.dialog = false">取消</button>
        <button class="btn-primary text-xs" @click="folderPickConfirm">选择此目录</button>
      </template>
    </ModalDialog>
  </div>
</template>

<script setup>
import { computed, nextTick, onMounted, reactive, ref, watch } from 'vue'
import { state, saveConfig, toast } from '../store.js'
import { DRIVES, driveFromRoute, submenuFromRoute } from '../driveDefs.js'
import FieldRenderer from '../components/FieldRenderer.vue'
import ModalDialog from '../components/ModalDialog.vue'
import TianyiQrLogin from '../components/TianyiQrLogin.vue'
import Yun139LoginPanel from '../components/Yun139LoginPanel.vue'
import Yun139FileManager from '../components/Yun139FileManager.vue'
import { api } from '../api.js'

// 当前网盘与子菜单均来自 hash 路由（子菜单已升入左侧主导航）
const drive = computed(() => driveFromRoute(state.currentView))
const def = computed(() => DRIVES[drive.value])

const local = reactive({})
const expanded = reactive({})
const saving = ref(false)
const search = ref('')
const showSearch = ref(false)
const strmRunning = ref('')
const strmResult = ref(null)

// 光鸭短信验证码登录面板状态
const gyCode = ref('')
const gyBusy = ref('')
const gyStatus = reactive({ logged_in: false, status: '未检测到已保存的光鸭登录信息' })

function isGuangyaAccountCard(card) {
  return drive.value === 'guangya' && currentSubmenu.value.key === 'account' && card.key === 'account'
}

function isTianyiAccountCard(card) {
  return drive.value === 'tianyi' && currentSubmenu.value.key === 'account' && card.key === 'account'
}

// 移动云盘（139）账号密码登录面板
function isYun139AccountCard(card) {
  return drive.value === 'cloud139' && currentSubmenu.value.key === 'account' && card.key === 'account'
}

function isGuangyaFilesCard(card) {
  return drive.value === 'guangya' && currentSubmenu.value.key === 'files' && card.key === 'files'
}

function isTianyiFilesCard(card) {
  return drive.value === 'tianyi' && currentSubmenu.value.key === 'files' && card.key === 'files'
}

function isYun139FilesCard(card) {
  return drive.value === 'cloud139' && currentSubmenu.value.key === 'files' && card.key === 'files'
}

// ---------------- 天翼云盘文件管理 ----------------

// 天翼个人云根目录 ID（189 业务约定）
const TY_ROOT_FOLDER_ID = '-11'

const tyFm = reactive({
  loading: false,
  error: '',
  items: [],
  checked: [],
  // 路径栈：面包屑，每项 {id, name}；id 为 '-11' 表示根目录
  pathStack: [{ id: TY_ROOT_FOLDER_ID, name: '根目录' }],
  // 弹窗
  dialog: false,
  dialogKind: '',
  dialogTitle: '',
  dialogInput: '',
  dialogBusy: false,
  dialogTarget: null
})

const tyFmAllChecked = computed(() => tyFm.items.length > 0 && tyFm.checked.length === tyFm.items.length)

// 已选文件项（用于确认弹窗中列出名称，超过 5 项时折叠）。
const tyFmCheckedItems = computed(() => tyFm.items.filter((it) => tyFm.checked.includes(it.id)))
const tyFmCheckedNames = computed(() => tyFmCheckedItems.value.map((it) => (it.isDir ? '📁 ' : '📄 ') + it.name))

function tyFmCurrentParent() {
  return tyFm.pathStack[tyFm.pathStack.length - 1].id
}

async function tyFmLoad() {
  tyFm.loading = true
  tyFm.error = ''
  tyFm.checked = []
  try {
    const res = await api.get(`/api/drive/tianyi/files?folder_id=${encodeURIComponent(tyFmCurrentParent())}&page=1&page_size=1000`)
    tyFm.items = tyFmNormalizeItems(res)
  } catch (e) {
    tyFm.error = e.message || '加载失败'
    tyFm.items = []
  } finally {
    tyFm.loading = false
  }
}

// 归一化天翼返回的文件夹/文件列表：api.get 已解包 data.data，
// 所以 res 直接是 {folders, files, count}。
function tyFmNormalizeItems(res) {
  const folders = []
  const files = []
  if (res) {
    if (Array.isArray(res.folders)) folders.push(...res.folders)
    if (Array.isArray(res.files)) files.push(...res.files)
    // 兼容嵌套 data 格式
    if (res.data && Array.isArray(res.data.folders)) folders.push(...res.data.folders)
    if (res.data && Array.isArray(res.data.files)) files.push(...res.data.files)
  }
  const mapItem = (it, isDir) => ({
    id: String(it.id ?? it.fileId ?? it.file_id ?? ''),
    name: it.name ?? it.fileName ?? it.file_name ?? '',
    size: Number(it.size ?? it.fileSize ?? 0),
    isDir,
    updatedAt: it.lastOpTime ?? it.last_op_time ?? it.createDate ?? it.create_date ?? it.updateTime ?? it.mtime ?? 0
  })
  return [
    ...folders.map((it) => mapItem(it, true)),
    ...files.map((it) => mapItem(it, false))
  ].filter((it) => it.id)
}

function tyFmEnter(it) {
  if (!it.isDir) return
  tyFm.pathStack.push({ id: it.id, name: it.name })
  tyFmLoad()
}

function tyFmGoUp() {
  if (tyFm.pathStack.length > 1) {
    tyFm.pathStack.pop()
    tyFmLoad()
  }
}

function tyFmJump(index) {
  tyFm.pathStack = tyFm.pathStack.slice(0, index + 1)
  tyFmLoad()
}

function tyFmToggleAll(checked) {
  tyFm.checked = checked ? tyFm.items.map((it) => it.id) : []
}

function tyFmSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = Math.floor(Math.log(bytes) / Math.log(1024))
  i = Math.min(i, units.length - 1)
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

function tyFmTime(ts) {
  if (!ts) return '—'
  const ms = ts > 1e12 ? ts : ts * 1000
  const d = new Date(ms)
  if (isNaN(d.getTime())) return '—'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function tyFmOpenDialog(kind, title, input, target) {
  tyFm.dialogKind = kind
  tyFm.dialogTitle = title
  tyFm.dialogInput = input || ''
  tyFm.dialogTarget = target || null
  tyFm.dialog = true
}

function tyFmMkdir() {
  tyFmOpenDialog('mkdir', '新建文件夹', '')
}

function tyFmRename(it) {
  tyFmOpenDialog('rename', `重命名「${it.name}」`, it.name, it)
}

function tyFmMove(it) {
  tyFm.checked = [it.id]
  tyFmOpenDialog('move', `移动「${it.name}」`, '', it)
}

function tyFmCopy(it) {
  tyFm.checked = [it.id]
  tyFmOpenDialog('copy', `复制「${it.name}」`, '', it)
}

function tyFmDownload(it) {
  api.post('/api/drive/tianyi/files/download_url', { file_id: it.id }).then((res) => {
    const url = (res && (res.url || res.downloadUrl || res.download_url || res.fileDownloadUrl)) || (res && res.data && (res.data.url || res.data.fileDownloadUrl))
    if (url) {
      window.open(url, '_blank', 'noopener')
    } else {
      toast('info', '下载链接', JSON.stringify(res))
    }
  }).catch((e) => toast('error', '获取下载链接失败', e.message))
}

function tyFmDelete(it) {
  tyFm.checked = [it.id]
  tyFmOpenDialog('delete', `删除「${it.name}」`, '', it)
}

function tyFmBulkDelete() {
  if (!tyFm.checked.length) return
  tyFmOpenDialog('delete', `删除所选 ${tyFm.checked.length} 项`, '')
}

function tyFmClearRecycle() {
  tyFmOpenDialog('clearRecycle', '清空回收站', '')
}

// 从已选项构造批量任务 items：{fileId, fileName, isFolder}
function tyFmBatchItems() {
  return tyFmCheckedItems.value.map((it) => ({
    fileId: it.id,
    fileName: it.name,
    isFolder: it.isDir ? 1 : 0
  }))
}

async function tyFmDialogConfirm() {
  const kind = tyFm.dialogKind
  tyFm.dialogBusy = true
  try {
    if (kind === 'mkdir') {
      const name = tyFm.dialogInput.trim()
      if (!name) { toast('warn', '请输入文件夹名称'); return }
      await api.post('/api/drive/tianyi/files/mkdir', { dir_name: name, parent_id: tyFmCurrentParent() })
      toast('success', '已创建文件夹', name)
      tyFm.dialog = false
      tyFmLoad()
    } else if (kind === 'rename') {
      const name = tyFm.dialogInput.trim()
      if (!name) { toast('warn', '请输入新名称'); return }
      await api.post('/api/drive/tianyi/files/rename', {
        file_id: tyFm.dialogTarget.id,
        new_name: name,
        is_folder: tyFm.dialogTarget.isDir ? 1 : 0
      })
      toast('success', '已重命名', name)
      tyFm.dialog = false
      tyFmLoad()
    } else if (kind === 'move') {
      await api.post('/api/drive/tianyi/files/move', {
        items: tyFmBatchItems(),
        target_folder_id: tyFm.dialogInput.trim() || TY_ROOT_FOLDER_ID
      })
      toast('success', '已移动', `${tyFm.checked.length} 项`)
      tyFm.dialog = false
      tyFmLoad()
    } else if (kind === 'copy') {
      await api.post('/api/drive/tianyi/files/copy', {
        items: tyFmBatchItems(),
        target_folder_id: tyFm.dialogInput.trim() || TY_ROOT_FOLDER_ID
      })
      toast('success', '已复制', `${tyFm.checked.length} 项`)
      tyFm.dialog = false
      tyFmLoad()
    } else if (kind === 'delete') {
      await api.post('/api/drive/tianyi/files/delete', { items: tyFmBatchItems() })
      toast('success', '已删除', `${tyFm.checked.length} 项`)
      tyFm.dialog = false
      tyFmLoad()
    } else if (kind === 'clearRecycle') {
      await api.post('/api/drive/tianyi/files/recycle/clear', {})
      toast('success', '回收站已清空')
      tyFm.dialog = false
    }
  } catch (e) {
    toast('error', '操作失败', e.message)
  } finally {
    tyFm.dialogBusy = false
  }
}

// ---------------- 文件夹浏览选择（配置项用） ----------------

const folderPick = reactive({
  dialog: false,
  title: '选择文件夹',
  loading: false,
  error: '',
  items: [],
  pathStack: [],
  // 回调：确认选择后写入的字段 key
  fieldKey: '',
  driveKey: ''
})

// FieldRenderer 的 @browse 事件处理
function onFieldBrowse(field) {
  const apiType = field.browseApi
  if (!apiType) return
  folderPick.driveKey = apiType
  folderPick.fieldKey = field.key
  folderPick.title = field.label || '选择文件夹'
  folderPick.dialog = true
  folderPick.error = ''
  // 初始化路径栈
  if (apiType === 'tianyi') {
    folderPick.pathStack = [{ id: TY_ROOT_FOLDER_ID, name: '根目录' }]
  } else {
    folderPick.pathStack = [{ id: '', name: '根目录' }]
  }
  folderPickLoad()
}

// 加载当前目录的子文件夹
async function folderPickLoad() {
  folderPick.loading = true
  folderPick.error = ''
  try {
    const parentId = folderPick.pathStack[folderPick.pathStack.length - 1].id
    let res
    if (folderPick.driveKey === 'tianyi') {
      res = await api.get(`/api/drive/tianyi/files?folder_id=${encodeURIComponent(parentId)}&page=1&page_size=1000`)
    } else if (folderPick.driveKey === 'guangya') {
      const params = new URLSearchParams({ parent_id: parentId, page: '0', page_size: '200', order_by: '3', sort_type: '1' })
      res = await api.get('/api/drive/guangya/files?' + params.toString())
    }
    // 归一化：只保留文件夹
    let list = []
    if (folderPick.driveKey === 'tianyi') {
      const folders = (res && res.folders) || []
      list = folders.map((it) => ({ id: String(it.id), name: it.name }))
    } else {
      if (Array.isArray(res)) list = res
      else if (res && Array.isArray(res.data)) list = res.data
      else if (res && res.data && Array.isArray(res.data.list)) list = res.data.list
      list = list.filter((it) => it.isDir || it.is_dir || Number(it.resType) === 2)
        .map((it) => ({ id: String(it.id ?? it.fileId), name: it.name ?? it.fileName }))
    }
    folderPick.items = list
  } catch (e) {
    folderPick.error = e.message || '加载失败'
    folderPick.items = []
  } finally {
    folderPick.loading = false
  }
}

// 进入子文件夹
function folderPickEnter(it) {
  folderPick.pathStack.push({ id: it.id, name: it.name })
  folderPickLoad()
}

// 跳转到面包屑某一层
function folderPickJump(index) {
  folderPick.pathStack = folderPick.pathStack.slice(0, index + 1)
  folderPickLoad()
}

// 当前路径显示
function folderPickCurrentPath() {
  return folderPick.pathStack.map((c) => c.name).join(' / ')
}

// 确认选择
function folderPickConfirm() {
  const current = folderPick.pathStack[folderPick.pathStack.length - 1]
  // 天翼用文件夹 ID，光鸭用文件夹 ID（后端配置存的是路径名或 ID）
  // 这里写入文件夹 ID；如果是根目录则写入空串
  const value = current.id || ''
  if (folderPick.fieldKey) {
    local[folderPick.fieldKey] = value
  }
  folderPick.dialog = false
  toast('success', '已选择目录', folderPickCurrentPath())
}

// ---------------- 光鸭文件管理 ----------------

const gyFm = reactive({
  recycle: false,
  loading: false,
  error: '',
  items: [],
  checked: [],
  // 路径栈：面包屑，每项 {id, name}；id 为空串表示根目录
  pathStack: [{ id: '', name: '根目录' }],
  // 弹窗
  dialog: false,
  dialogKind: '',
  dialogTitle: '',
  dialogInput: '',
  dialogBusy: false,
  dialogTarget: null
})

const gyFmAllChecked = computed(() => gyFm.items.length > 0 && gyFm.checked.length === gyFm.items.length)

// 已选文件项（用于确认弹窗中列出名称，超过 5 项时折叠）。
const gyFmCheckedItems = computed(() => gyFm.items.filter((it) => gyFm.checked.includes(it.id)))
const gyFmCheckedNames = computed(() => gyFmCheckedItems.value.map((it) => (it.isDir ? '📁 ' : '📄 ') + it.name))

function gyFmCurrentParent() {
  return gyFm.pathStack[gyFm.pathStack.length - 1].id
}

async function gyFmLoad() {
  gyFm.loading = true
  gyFm.error = ''
  gyFm.checked = []
  try {
    const params = new URLSearchParams({
      parent_id: gyFmCurrentParent(),
      page: '0',
      page_size: '200',
      order_by: '3',
      sort_type: '1'
    })
    if (gyFm.recycle) params.set('dir_type', '4')
    const res = await api.get('/api/drive/guangya/files?' + params.toString())
    gyFm.items = gyFmNormalizeItems(res)
  } catch (e) {
    gyFm.error = e.message || '加载失败'
    gyFm.items = []
  } finally {
    gyFm.loading = false
  }
}

// 归一化后端返回的文件列表（兼容 data 嵌套与多种字段名）。
function gyFmNormalizeItems(res) {
  let list = []
  if (Array.isArray(res)) list = res
  else if (res && Array.isArray(res.data)) list = res.data
  else if (res && res.data && Array.isArray(res.data.list)) list = res.data.list
  else if (res && res.data && res.data.files && Array.isArray(res.data.files)) list = res.data.files
  return list.map((it) => ({
    id: String(it.id ?? it.fileId ?? it.file_id ?? ''),
    name: it.name ?? it.fileName ?? it.file_name ?? '',
    size: Number(it.size ?? it.fileSize ?? 0),
    // 目录判定：resType=2 为目录（上游约定），兼容 isDir/is_dir 字段。
    isDir: Boolean(it.isDir ?? it.is_dir ?? (Number(it.resType) === 2)),
    updatedAt: it.updatedAt ?? it.updated_at ?? it.updateTime ?? it.utime ?? it.mtime ?? it.ctime ?? 0
  })).filter((it) => it.id)
}

function gyFmEnter(it) {
  if (!it.isDir) return
  gyFm.pathStack.push({ id: it.id, name: it.name })
  gyFmLoad()
}

function gyFmGoUp() {
  if (gyFm.pathStack.length > 1) {
    gyFm.pathStack.pop()
    gyFmLoad()
  }
}

function gyFmJump(index) {
  gyFm.pathStack = gyFm.pathStack.slice(0, index + 1)
  gyFmLoad()
}

function gyFmToggleAll(checked) {
  gyFm.checked = checked ? gyFm.items.map((it) => it.id) : []
}

function gyFmSize(bytes) {
  if (!bytes) return '0 B'
  const units = ['B', 'KB', 'MB', 'GB', 'TB']
  let i = Math.floor(Math.log(bytes) / Math.log(1024))
  i = Math.min(i, units.length - 1)
  return (bytes / Math.pow(1024, i)).toFixed(i === 0 ? 0 : 1) + ' ' + units[i]
}

function gyFmTime(ts) {
  if (!ts) return '—'
  const ms = ts > 1e12 ? ts : ts * 1000
  const d = new Date(ms)
  if (isNaN(d.getTime())) return '—'
  const pad = (n) => String(n).padStart(2, '0')
  return `${d.getFullYear()}-${pad(d.getMonth() + 1)}-${pad(d.getDate())} ${pad(d.getHours())}:${pad(d.getMinutes())}`
}

function gyFmOpenDialog(kind, title, input, target) {
  gyFm.dialogKind = kind
  gyFm.dialogTitle = title
  gyFm.dialogInput = input || ''
  gyFm.dialogTarget = target || null
  gyFm.dialog = true
}

function gyFmMkdir() {
  gyFmOpenDialog('mkdir', '新建文件夹', '')
}

function gyFmRename(it) {
  gyFmOpenDialog('rename', `重命名「${it.name}」`, it.name, it)
}

function gyFmMove(it) {
  gyFm.checked = [it.id]
  gyFmOpenDialog('move', `移动「${it.name}」`, '', it)
}

function gyFmCopy(it) {
  gyFm.checked = [it.id]
  gyFmOpenDialog('copy', `复制「${it.name}」`, '', it)
}

function gyFmDownload(it) {
  api.post('/api/drive/guangya/files/download_url', { file_id: it.id }).then((res) => {
    const url = (res && (res.url || res.downloadUrl || res.download_url)) || (res && res.data && (res.data.url || res.data.downloadUrl))
    if (url) {
      window.open(url, '_blank', 'noopener')
    } else {
      toast('info', '下载链接', JSON.stringify(res))
    }
  }).catch((e) => toast('error', '获取下载链接失败', e.message))
}

function gyFmDelete(it) {
  gyFm.checked = [it.id]
  gyFmOpenDialog('delete', `删除「${it.name}」`, '', it)
}

function gyFmRestore(it) {
  gyFm.checked = [it.id]
  gyFmOpenDialog('restore', `还原「${it.name}」`, '', it)
}

function gyFmBulkDelete() {
  if (!gyFm.checked.length) return
  gyFmOpenDialog('delete', `删除所选 ${gyFm.checked.length} 项`, '')
}

function gyFmBulkRestore() {
  if (!gyFm.checked.length) return
  gyFmOpenDialog('restore', `还原所选 ${gyFm.checked.length} 项`, '')
}

function gyFmClearRecycle() {
  gyFmOpenDialog('clearRecycle', '清空回收站', '')
}

async function gyFmDialogConfirm() {
  const kind = gyFm.dialogKind
  gyFm.dialogBusy = true
  try {
    if (kind === 'mkdir') {
      const name = gyFm.dialogInput.trim()
      if (!name) { toast('warn', '请输入文件夹名称'); return }
      await api.post('/api/drive/guangya/files/mkdir', { dir_name: name, parent_id: gyFmCurrentParent() })
      toast('success', '已创建文件夹', name)
      gyFm.dialog = false
      gyFmLoad()
    } else if (kind === 'rename') {
      const name = gyFm.dialogInput.trim()
      if (!name) { toast('warn', '请输入新名称'); return }
      await api.post('/api/drive/guangya/files/rename', { file_id: gyFm.dialogTarget.id, new_name: name })
      toast('success', '已重命名', name)
      gyFm.dialog = false
      gyFmLoad()
    } else if (kind === 'move') {
      const res = await api.post('/api/drive/guangya/files/move', { file_ids: gyFm.checked, parent_id: gyFm.dialogInput.trim() })
      await gyFmWaitTask(res, '已移动', `${gyFm.checked.length} 项`)
    } else if (kind === 'copy') {
      const res = await api.post('/api/drive/guangya/files/copy', { file_ids: gyFm.checked, parent_id: gyFm.dialogInput.trim() })
      await gyFmWaitTask(res, '已复制', `${gyFm.checked.length} 项`)
    } else if (kind === 'delete') {
      const res = await api.post('/api/drive/guangya/files/delete', { file_ids: gyFm.checked })
      await gyFmWaitTask(res, '已删除', `${gyFm.checked.length} 项`)
    } else if (kind === 'restore') {
      const res = await api.post('/api/drive/guangya/files/recycle', { file_ids: gyFm.checked })
      await gyFmWaitTask(res, '已还原', `${gyFm.checked.length} 项`)
    } else if (kind === 'clearRecycle') {
      const res = await api.post('/api/drive/guangya/files/recycle/clear', {})
      await gyFmWaitTask(res, '回收站已清空', '')
    }
  } catch (e) {
    toast('error', '操作失败', e.message)
  } finally {
    gyFm.dialogBusy = false
  }
}

// 等待异步任务完成（轮询 task_status），完成后关闭弹窗并刷新列表。
async function gyFmWaitTask(res, okTitle, okDetail) {
  const taskId = gyFmExtractTaskId(res)
  if (!taskId) {
    gyFm.dialog = false
    gyFmLoad()
    toast('success', okTitle, okDetail)
    return
  }
  toast('info', '任务处理中', '后台执行，完成后自动刷新...')
  const done = await gyFmPollTask(taskId)
  gyFm.dialog = false
  gyFmLoad()
  if (done) {
    toast('success', okTitle, okDetail)
  } else {
    toast('warn', '任务仍在处理', '请稍后手动刷新查看结果')
  }
}

// 从响应中提取 taskId（兼容 data 嵌套）。
function gyFmExtractTaskId(res) {
  if (!res) return ''
  if (res.taskId) return String(res.taskId)
  if (res.data && res.data.taskId) return String(res.data.taskId)
  if (res.data && res.data.data && res.data.data.taskId) return String(res.data.data.taskId)
  return ''
}

// 轮询任务状态直到完成（status=2），最多等约 15 秒。
async function gyFmPollTask(taskId) {
  for (let i = 0; i < 15; i++) {
    try {
      const res = await api.post('/api/drive/guangya/files/task_status', { task_id: taskId })
      const status = res?.data?.status ?? res?.data?.data?.status ?? res?.status
      if (Number(status) === 2 || Number(status) === 3) return true
      if (Number(status) === 4 || Number(status) === 5) return false
    } catch {
      /* 轮询失败不打断流程 */
    }
    await new Promise((r) => setTimeout(r, 1000))
  }
  return false
}

async function gyRefreshStatus() {
  try {
    const res = await api.get('/api/drive/guangya/status')
    gyStatus.logged_in = !!res.logged_in
    gyStatus.status = res.status || '未知状态'
  } catch {
    /* 状态查询失败不打扰面板 */
  }
}

async function gySendCode() {
  // 手机号在「登录手机号」输入框里，先落盘再发送
  await save()
  gyBusy.value = 'send'
  try {
    const res = await api.post('/api/drive/guangya/sms/send')
    toast('success', '验证码已发送', res.status)
    await gyRefreshStatus()
  } catch (e) {
    toast('error', '发送失败', e.message)
  } finally {
    gyBusy.value = ''
  }
}

async function gyLogin() {
  if (!gyCode.value.trim()) {
    toast('warn', '请输入验证码', '先点击「发送验证码」获取短信验证码')
    return
  }
  gyBusy.value = 'login'
  try {
    const res = await api.post('/api/drive/guangya/sms/login', { code: gyCode.value.trim() })
    toast('success', '登录成功', res.status)
    gyCode.value = ''
    await gyRefreshStatus()
  } catch (e) {
    toast('error', '登录失败', e.message)
  } finally {
    gyBusy.value = ''
  }
}

async function gyRun(op) {
  gyBusy.value = op
  try {
    const res = await api.post(`/api/drive/guangya/${op === 'refresh' ? 'token/refresh' : op === 'verify' ? 'token/verify' : 'login/clear'}`)
    toast('success', '操作完成', res.status)
    await gyRefreshStatus()
  } catch (e) {
    toast('error', '操作失败', e.message)
  } finally {
    gyBusy.value = ''
  }
}

const currentSubmenu = computed(() => {
  const key = submenuFromRoute(state.currentView)
  return def.value.submenus.find((s) => s.key === key) || def.value.submenus[0]
})

// 当前子菜单涉及的配置字段（保存时只提交这些字段）
const currentFields = computed(() => currentSubmenu.value.cards.flatMap((c) => c.fields))

// 加载配置分区到本地表单；路由切换时只重置当前子菜单的字段
function loadLocal() {
  const cfg = state.config[drive.value] || {}
  for (const f of currentFields.value) {
    local[f.key] = cfg[f.key]
  }
}

onMounted(() => {
  loadLocal()
  if (drive.value === 'guangya') {
    gyRefreshStatus()
    if (currentSubmenu.value.key === 'files') gyFmLoad()
  }
  if (drive.value === 'tianyi' && currentSubmenu.value.key === 'files') {
    nextTick(() => tyFmLoad())
  }
})
watch(() => state.currentView, () => {
  loadLocal()
  if (drive.value === 'guangya') {
    gyRefreshStatus()
    if (currentSubmenu.value.key === 'files') gyFmLoad()
  }
  if (drive.value === 'tianyi' && currentSubmenu.value.key === 'files') {
    tyFmLoad()
  }
})

function toggleCard(key) {
  expanded[key] = !(expanded[key] !== false)
}

// 搜索：在当前子菜单内匹配卡片标题 / 字段标签
const searchResults = computed(() => {
  const kw = search.value.trim().toLowerCase()
  if (!kw) return []
  const out = []
  for (const card of currentSubmenu.value.cards) {
    if (card.title.toLowerCase().includes(kw)) {
      out.push({ cardKey: card.key, card: card.title })
    }
    for (const f of card.fields) {
      if (f.label.toLowerCase().includes(kw)) {
        out.push({ cardKey: card.key, card: card.title, field: f.label })
      }
    }
  }
  return out.slice(0, 12)
})

function delayHideSearch() {
  setTimeout(() => (showSearch.value = false), 150)
}

function locate(r) {
  expanded[r.cardKey] = true
  search.value = ''
  showSearch.value = false
  setTimeout(() => {
    const el = document.getElementById('card-' + r.cardKey)
    if (el) el.scrollIntoView({ behavior: 'smooth', block: 'start' })
  }, 60)
}

async function save() {
  saving.value = true
  try {
    // 增量保存：仅提交当前子菜单的字段，后端按字段合并
    const patch = {}
    for (const f of currentFields.value) {
      patch[f.key] = local[f.key]
    }
    await saveConfig(drive.value, patch)
    toast('success', def.value.name + ' · ' + currentSubmenu.value.title + ' 已保存', '所有修改已持久化')
  } catch (e) {
    toast('error', '保存失败', e.message)
  } finally {
    saving.value = false
  }
}

async function runAction(action) {
  if (!action.mode) return
  strmRunning.value = action.key
  try {
    const res = await api.post('/api/strm/generate', { drive_type: drive.value, mode: action.mode })
    strmResult.value = res
    toast('success', 'STRM 生成完成', `新增 ${res.generated} / 跳过 ${res.skipped} / 总计 ${res.total}`)
  } catch (e) {
    toast('error', 'STRM 生成失败', e.message)
  } finally {
    strmRunning.value = ''
  }
}
</script>
