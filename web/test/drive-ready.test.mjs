// 网盘就绪绿点判定回归测试：node test/drive-ready.test.mjs
// 背景：天翼扫码登录只写 account、不写 password，旧判定要求 account+password 全填，
// 导致登录成功后菜单绿点永远不亮。现改为优先采用后端 drive_status.logged_in。
import { driveReady } from '../src/driveDefs.js'

const results = []
const check = (name, cond, extra = '') => results.push({ name, pass: !!cond, extra })

// 天翼：已扫码登录（后端给 logged_in=true，配置里只有 account）
check('天翼已登录 → 绿点',
  driveReady('tianyi', { tianyi: { account: '18912345678', password: '' } }, { tianyi: { logged_in: true } }) === true)

// 天翼：后端明确未登录时，即使配置里有账号密码也不亮（以真实会话为准）
check('天翼未登录（有账号密码）→ 不亮',
  driveReady('tianyi', { tianyi: { account: '18912345678', password: 'x' } }, { tianyi: { logged_in: false } }) === false)

// 天翼：清除登录后（logged_in=false 且账号已清空）→ 不亮
check('天翼清除登录 → 不亮',
  driveReady('tianyi', { tianyi: { account: '' } }, { tianyi: { logged_in: false } }) === false)

// 老后端没有 drive_status 字段时退回凭证判断（只填 account 也算配置完成）
check('无 drive_status 时按 account 兜底',
  driveReady('tianyi', { tianyi: { account: '18912345678' } }) === true)
check('无 drive_status 且无账号 → 不亮',
  driveReady('tianyi', { tianyi: {} }) === false)

// 其它网盘保持原有凭证判定
check('移动云盘账号+密码齐全 → 绿点', driveReady('cloud139', { cloud139: { username: '13800138000', password: 'p' } }) === true)
check('移动云盘只有账号 → 不亮', driveReady('cloud139', { cloud139: { username: '13800138000', password: '' } }) === false)
check('移动云盘仅授权令牌 → 绿点', driveReady('cloud139', { cloud139: { authorization: 'tok' } }) === true)
// 123 云盘已下线：不再出现在 DRIVES 里，判定一律 false
check('已下线的 123 云盘 → 不亮', driveReady('cloud123', { cloud123: { username: 'u', password: 'p' } }) === false)
check('光鸭有手机号 → 绿点', driveReady('guangya', { guangya: { phone: '13800000000' } }) === true)
check('未知网盘 → 不亮', driveReady('nope', {}) === false)

for (const r of results) console.log(`${r.pass ? 'PASS' : 'FAIL'}  ${r.name}${r.extra ? '  [' + r.extra + ']' : ''}`)
const failed = results.filter((r) => !r.pass)
console.log(`\n${results.length - failed.length}/${results.length} passed`)
process.exit(failed.length ? 1 : 0)
