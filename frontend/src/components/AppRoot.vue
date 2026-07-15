<script setup>
import { computed, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import AppIcon from './AppIcon.vue';
import AdminConsole from './AdminConsole.vue';
import { useAppStateStore } from '../stores/appState';
import {
  api, closeCalendar, login, logout, openCalendarMonth, registerAccount,
  loadPublicLibrary, previewLibraryItem, refreshCurrentUser, setDefaultGroupAction,
  setSelectedDate, setTab, switchGroup, toast, toggleSidebar,
} from '../legacy-app';

const store = useAppStateStore();
const {
  booted, authenticated, user, tab, sidebarCollapsed, groups, currentGroupID,
  defaultGroupID, showGroupPicker, canAdmin, calendar, toast: toastMessage,
  networkBusy, online, publicLibrary, resourceLoading,
} = storeToRefs(store);

const authMode = ref('login');
const loginIdentifier = ref('');
const loginPassword = ref('');
const authError = ref('');
const authNotice = ref('');
const registrationGroups = ref([]);
const registerName = ref('');
const registerGroupID = ref('');
const registerEmail = ref('');
const registerPassword = ref('');
const registrationPreview = ref(null);
const profileUsername = ref('');
const profileEmail = ref('');
const avatarInput = ref(null);
const oldPassword = ref('');
const newPassword = ref('');
const confirmPassword = ref('');
const authSubmitting = ref(false);
const showLoginPassword = ref(false);
const showRegisterPassword = ref(false);
const profileSaving = ref(false);
const avatarUploading = ref(false);
const passwordSaving = ref(false);
const activeResourceKey = ref('');
const resourceSearch = ref('');
const openingResource = ref('');

const navigation = computed(() => [
  { id: 'home', label: '今日打卡', short: '打卡', icon: 'home' },
  { id: 'dashboard', label: '成长数据', short: '数据', icon: 'chart' },
  { id: 'resources', label: '资料库', short: '资料', icon: 'library' },
  { id: 'profile', label: '个人中心', short: '我的', icon: 'user' },
  ...(canAdmin.value ? [{ id: 'admin', label: '管理后台', short: '管理', icon: 'settings' }] : []),
]);

const activeGroup = computed(() => groups.value.find((group) => Number(group.id) === Number(currentGroupID.value)));
const activeResourceSection = computed(() => publicLibrary.value.find((section) => section.key === activeResourceKey.value) || publicLibrary.value[0] || null);
const filteredResourceItems = computed(() => {
  const query = resourceSearch.value.trim().toLocaleLowerCase();
  const items = activeResourceSection.value?.items || [];
  if (!query) return items;
  return items.filter((item) => `${item.title || ''} ${item.original_name || ''} ${item.folder || ''}`.toLocaleLowerCase().includes(query));
});
const calendarItemMap = computed(() => calendarItemsByDate(calendar.value?.items || []));
const pageMeta = computed(() => ({
  dashboard: ['成长数据', '查看小组进度与个人坚持'],
  resources: ['资料库', '本组共享的读物与学习资料'],
  profile: ['个人中心', '管理头像、账号与安全设置'],
  admin: ['管理后台', '管理任务、成员与学习资料'],
})[tab.value] || ['', '']);

watch(tab, (value) => { if (value === 'profile') syncProfile(); });
watch(publicLibrary, (sections) => {
  if (!sections?.some((section) => section.key === activeResourceKey.value)) activeResourceKey.value = sections?.[0]?.key || '';
}, { immediate: true });

function friendlyError(error) {
  const code = typeof error === 'string' ? error : error?.code;
  const fallback = typeof error === 'string' ? error : error?.message;
  return ({
    invalid_username_or_password: '邮箱、用户名或密码不正确',
    invalid_username: '用户名需为 3–64 位，以小写字母开头，可包含数字、点、短横线或下划线',
    invalid_password: '当前密码不正确',
    password_too_short: '新密码至少需要 8 位',
    roster_not_found: '姓名与所选门训组不匹配',
    roster_already_claimed: '该名单席位已经注册',
    email_exists: '该邮箱已经注册',
    weak_password: '密码至少需要 8 位',
    invalid_email: '请填写有效邮箱',
    profile_conflict: '用户名或邮箱已被使用',
    account_exists: '该姓名对应的账号已经存在，请直接登录或联系管理员',
    unauthorized: '登录已过期，请重新登录',
    forbidden: '你没有执行此操作的权限',
    password_change_required: '请先完成首次登录密码修改',
    request_timeout: '请求超时，请检查 NAS 网络后重试',
    network_error: '无法连接服务器，请检查网络',
    avatar_too_large: '头像文件过大',
    invalid_avatar: '请选择有效的 JPG 或 PNG 图片',
    avatar_required: '请先选择头像图片',
    avatar_failed: '头像处理失败，请更换图片后重试',
    register_failed: '账号创建失败，请稍后重试或联系管理员',
  })[code] || fallback || '操作失败，请稍后重试';
}

async function chooseAuthMode(mode) {
  authMode.value = mode; authError.value = ''; authNotice.value = '';
  if (mode === 'register' && !registrationGroups.value.length) {
    try { const data = await api('/auth/registration-groups'); registrationGroups.value = data.groups || []; }
    catch (error) { authError.value = friendlyError(error); }
  }
}

async function submitLogin() {
  if (authSubmitting.value) return;
  authError.value = '';
  if (!loginIdentifier.value.trim() || !loginPassword.value) return (authError.value = '请填写登录账号和密码');
  authSubmitting.value = true;
  try { await login(loginIdentifier.value, loginPassword.value); }
  catch (error) { authError.value = friendlyError(error); }
  finally { authSubmitting.value = false; }
}

async function previewRegistration() {
  registrationPreview.value = null; authError.value = '';
  if (!registerName.value || !registerGroupID.value) return;
  try { registrationPreview.value = await api('/auth/registration-preview', { method: 'POST', body: JSON.stringify({ name: registerName.value, group_id: Number(registerGroupID.value) }) }); }
  catch (error) { authError.value = friendlyError(error); }
}

async function submitRegister() {
  if (authSubmitting.value) return;
  authError.value = '';
  if (!registrationPreview.value?.matched || registrationPreview.value?.claimed) return (authError.value = '请先确认姓名与门训组匹配');
  if (!registerEmail.value.trim() || registerPassword.value.length < 8) return (authError.value = '请填写有效邮箱和至少 8 位密码');
  authSubmitting.value = true;
  try { await registerAccount({ name: registerName.value, group_id: Number(registerGroupID.value), email: registerEmail.value, password: registerPassword.value }); }
  catch (error) { authError.value = friendlyError(error); }
  finally { authSubmitting.value = false; }
}

function syncProfile() { profileUsername.value = user.value?.username || ''; profileEmail.value = user.value?.email || ''; }

function selectGroup(groupID) {
  if (!groupID) return;
  switchGroup(groupID).catch(() => {});
}

async function saveProfile() {
  if (profileSaving.value) return;
  profileSaving.value = true;
  try { await api('/auth/profile', { method: 'PUT', body: JSON.stringify({ username: profileUsername.value, email: profileEmail.value }) }); await refreshCurrentUser(); toast('个人资料已保存'); }
  catch (error) { toast(friendlyError(error)); }
  finally { profileSaving.value = false; }
}

async function uploadAvatar() {
  const file = avatarInput.value?.files?.[0]; if (!file) return;
  if (avatarUploading.value) return;
  if (file.size > 6 * 1024 * 1024) return toast('头像文件不能超过 6MB');
  avatarUploading.value = true;
  const body = new FormData(); body.append('avatar', file);
  try {
    await api('/auth/avatar', { method: 'POST', body, timeout: 2 * 60 * 1000 });
    await refreshCurrentUser(); toast('头像已更新');
  } catch (error) { toast(friendlyError(error)); }
  finally { avatarUploading.value = false; if (avatarInput.value) avatarInput.value.value = ''; }
}

async function changePassword() {
  if (passwordSaving.value) return;
  if (newPassword.value.length < 8) return toast('新密码至少需要 8 位');
  if (newPassword.value !== confirmPassword.value) return toast('两次输入的新密码不一致');
  passwordSaving.value = true;
  try {
    const identifier = user.value?.email || user.value?.username || '';
    await api('/auth/change-password', { method: 'POST', body: JSON.stringify({ old_password: oldPassword.value, new_password: newPassword.value }) });
    oldPassword.value = ''; newPassword.value = ''; confirmPassword.value = '';
    logout();
    authMode.value = 'login'; loginIdentifier.value = identifier; loginPassword.value = '';
    authNotice.value = '密码已更新，请使用新密码重新登录。';
  }
  catch (error) { toast(friendlyError(error)); }
  finally { passwordSaving.value = false; }
}

function resourceLabel(category) { return ({ book: '读物', passage: '课程读物', handout: '讲义', mentor: '导师资料', markdown: '文章', outline: '提纲', video: '视频' })[category] || '资料'; }
async function openResource(asset) {
  const key = asset.id || asset.url;
  if (!key || openingResource.value) return;
  openingResource.value = key;
  try { await previewLibraryItem(asset); }
  catch (error) { toast(`打开失败：${error.message}`); }
  finally { openingResource.value = ''; }
}
async function refreshResources() {
  try { await loadPublicLibrary(true); toast('NAS 资源已刷新'); }
  catch (_) { /* feedback is handled by the loader */ }
}

function calendarItemsByDate(items) {
  const map = new Map(); for (const item of items || []) { const list = map.get(item.date) || []; list.push(item); map.set(item.date, list); } return map;
}
function calendarDays(month) {
  const [year, mm] = String(month || '').split('-').map(Number); if (!year || !mm) return [];
  const first = new Date(year, mm - 1, 1); const total = new Date(year, mm, 0).getDate();
  return [...Array(first.getDay()).fill(null), ...Array.from({ length: total }, (_, index) => index + 1)];
}
function shiftMonth(month, delta) { const [year, mm] = String(month || '').split('-').map(Number); const date = new Date(year, mm - 1 + delta, 1); return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}`; }
async function chooseCalendarDate(day) { if (!day || !calendar.value?.month) return; const date = `${calendar.value.month}-${String(day).padStart(2, '0')}`; closeCalendar(); await setSelectedDate(date); }
</script>

<template>
  <div v-if="!booted" class="ios-boot-screen" role="status" aria-live="polite">
    <div class="ios-boot-logo">AGP</div><div class="ios-spinner"></div><b>正在连接门训打卡</b><span>请稍候，正在同步账号与今日任务…</span>
  </div>

  <div v-else-if="!authenticated" class="ios-auth-screen">
    <div class="ios-auth-orb orb-one"></div><div class="ios-auth-orb orb-two"></div>
    <section class="ios-auth-brand">
      <div class="ios-app-icon"><span>AGP</span></div>
      <div class="ios-brand-kicker">2026 门训生命季</div>
      <h1>让每一天的坚持，<br />成为生命的成长。</h1>
      <p>专注今日任务、同行进度和学习资源。简单打卡，持续成长。</p>
      <div class="ios-feature-row"><span><AppIcon name="check" />每日任务</span><span><AppIcon name="users" />小组同行</span><span><AppIcon name="chart" />成长记录</span></div>
    </section>
    <section class="ios-auth-card">
      <div class="ios-segmented-control"><button :class="{ active: authMode === 'login' }" type="button" @click="chooseAuthMode('login')">登录</button><button :class="{ active: authMode === 'register' }" type="button" @click="chooseAuthMode('register')">注册</button></div>
      <template v-if="authMode === 'login'">
        <header><h2>欢迎回来</h2><p>继续今天的门训旅程</p></header>
        <form class="ios-auth-form" @submit.prevent="submitLogin">
          <label><span>邮箱或用户名</span><input v-model="loginIdentifier" autocomplete="username" inputmode="email" placeholder="name@example.com" /></label>
          <label><span>密码</span><div class="ios-password-field"><input v-model="loginPassword" autocomplete="current-password" :type="showLoginPassword ? 'text' : 'password'" placeholder="请输入密码" /><button type="button" :aria-label="showLoginPassword ? '隐藏密码' : '显示密码'" @click="showLoginPassword = !showLoginPassword">{{ showLoginPassword ? '隐藏' : '显示' }}</button></div></label>
          <button :disabled="authSubmitting" type="submit">{{ authSubmitting ? '登录中…' : '登录' }}</button>
        </form>
      </template>
      <template v-else>
        <header><h2>创建账号</h2><p>请使用报名名单中的姓名和组别</p></header>
        <form class="ios-auth-form" @submit.prevent="submitRegister">
          <label><span>姓名</span><input v-model="registerName" placeholder="真实姓名" @blur="previewRegistration" /></label>
          <label><span>门训组</span><select v-model="registerGroupID" @change="previewRegistration"><option value="">请选择</option><option v-for="group in registrationGroups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label>
          <div v-if="registrationPreview?.matched" class="ios-roster-match" :class="{ error: registrationPreview.claimed }"><AppIcon :name="registrationPreview.claimed ? 'lock' : 'check'" /><div><b>{{ registrationPreview.canonical_name }}</b><small>{{ registrationPreview.claimed ? '该名单已经注册' : `用户名将设为 ${registrationPreview.suggested_username}` }}</small></div></div>
          <label><span>联系邮箱</span><input v-model="registerEmail" autocomplete="email" type="email" placeholder="用于登录" /></label>
          <label><span>密码</span><div class="ios-password-field"><input v-model="registerPassword" autocomplete="new-password" :type="showRegisterPassword ? 'text' : 'password'" placeholder="至少 8 位" /><button type="button" :aria-label="showRegisterPassword ? '隐藏密码' : '显示密码'" @click="showRegisterPassword = !showRegisterPassword">{{ showRegisterPassword ? '隐藏' : '显示' }}</button></div></label>
          <button :disabled="authSubmitting || registrationPreview?.claimed || !registrationPreview?.matched" type="submit">{{ authSubmitting ? '正在创建账号…' : '注册并进入' }}</button>
        </form>
      </template>
      <div v-if="authNotice" class="ios-form-success">{{ authNotice }}</div>
      <div v-if="authError" class="ios-form-error">{{ authError }}</div>
    </section>
  </div>

  <div v-else-if="user?.must_change_password" class="ios-force-password-screen">
    <section class="ios-force-password-card">
      <span class="force-lock"><AppIcon name="lock" :size="28" /></span>
      <small>ACCOUNT SECURITY</small><h1>首次登录，请设置新密码</h1>
      <p>当前使用的是管理员分配的临时密码。更新后会自动退出，请使用新密码重新登录。</p>
      <form class="ios-stack" @submit.prevent="changePassword">
        <label><span>当前临时密码</span><input v-model="oldPassword" autocomplete="current-password" type="password" /></label>
        <label><span>新密码</span><input v-model="newPassword" autocomplete="new-password" type="password" placeholder="至少 8 位" /></label>
        <label><span>再次输入新密码</span><input v-model="confirmPassword" autocomplete="new-password" type="password" /></label>
        <button :disabled="passwordSaving" type="submit">{{ passwordSaving ? '正在更新…' : '设置新密码' }}</button>
      </form>
      <button class="ios-text-button" type="button" @click="logout">退出当前账号</button>
    </section>
  </div>

  <div v-else class="ios-app-shell" :class="{ collapsed: sidebarCollapsed }">
    <aside class="ios-sidebar">
      <div class="ios-sidebar-brand"><div class="ios-mini-logo">A</div><div v-if="!sidebarCollapsed"><b>门训打卡</b><small>{{ activeGroup?.name || 'AGP' }}</small></div></div>
      <nav aria-label="主导航"><button v-for="item in navigation" :key="item.id" :class="{ active: tab === item.id }" :title="item.label" :aria-label="item.label" :aria-current="tab === item.id ? 'page' : undefined" type="button" @click="setTab(item.id)"><span><AppIcon :name="item.icon" :size="21" /></span><b v-if="!sidebarCollapsed">{{ item.label }}</b></button></nav>
      <div class="ios-sidebar-account"><button class="ios-account-button" type="button" aria-label="打开个人中心" @click="setTab('profile')"><img v-if="user?.avatar_url" :src="user.avatar_url" alt="个人头像" /><span v-else>{{ (user?.display_name || '?').slice(0,1) }}</span><div v-if="!sidebarCollapsed"><b>{{ user?.display_name }}</b><small>@{{ user?.username }}</small></div></button><button class="ios-logout-button" type="button" title="退出登录" aria-label="退出登录" @click="logout"><AppIcon name="logout" :size="19" /></button></div>
      <button class="ios-collapse-button" type="button" :title="sidebarCollapsed ? '展开导航' : '收起导航'" :aria-label="sidebarCollapsed ? '展开导航' : '收起导航'" @click="toggleSidebar"><AppIcon name="chevron" :size="18" /></button>
    </aside>

    <main class="ios-main-area">
      <header v-if="tab !== 'home' || groups.length > 1" class="ios-topbar" :class="{ home: tab === 'home' }">
        <div v-if="tab !== 'home'"><small>{{ activeGroup?.name || 'AGP' }}</small><h1>{{ pageMeta[0] }}</h1><p>{{ pageMeta[1] }}</p></div>
        <div v-if="groups.length > 1" class="ios-group-switcher"><span>当前小组</span><select :value="currentGroupID || ''" @change="selectGroup($event.target.value)"><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select><button v-if="currentGroupID && defaultGroupID !== currentGroupID" type="button" @click="setDefaultGroupAction(currentGroupID)">设为默认</button></div>
      </header>

      <div class="ios-content-area" :class="{ 'admin-content': tab === 'admin' }">
        <section v-if="showGroupPicker" class="ios-group-picker"><header><small>SELECT GROUP</small><h1>选择要进入的小组</h1><p>你的打卡与资料会按小组独立显示。</p></header><div><button v-for="group in groups" :key="group.id" type="button" @click="selectGroup(group.id)"><span><AppIcon name="users" /></span><div><b>{{ group.name }}</b><small>{{ group.code }}</small></div><AppIcon name="chevron" /></button></div></section>
        <div v-else-if="tab === 'home'" id="vue-checkin-workbench"></div>
        <div v-else-if="tab === 'dashboard'" id="vue-dashboard"></div>

        <section v-else-if="tab === 'resources'" class="ios-public-library">
          <div class="ios-library-toolbar">
            <div><small>GROUP LIBRARY</small><b>{{ publicLibrary.reduce((total, section) => total + Number(section.count || section.items?.length || 0), 0) }} 项学习资料</b><span>NAS 新增文件后点击刷新即可同步</span></div>
            <label class="ios-library-search"><span class="sr-only">搜索当前分类</span><input v-model="resourceSearch" type="search" placeholder="搜索标题、文件名或文件夹" /></label>
            <button class="ios-secondary-button" type="button" :disabled="resourceLoading" @click="refreshResources"><AppIcon name="refresh" :size="17" />{{ resourceLoading ? '同步中…' : '刷新 NAS' }}</button>
          </div>
          <div v-if="publicLibrary.length" class="ios-library-tabs" role="tablist" aria-label="资料分类"><button v-for="section in publicLibrary" :key="section.key" :class="{ active: activeResourceSection?.key === section.key }" :aria-selected="activeResourceSection?.key === section.key" role="tab" type="button" @click="activeResourceKey = section.key"><span>{{ section.label }}</span><small>{{ section.count ?? section.items?.length ?? 0 }}</small></button></div>
          <div v-if="resourceLoading && !publicLibrary.length" class="ios-resource-skeleton" role="status"><span v-for="index in 6" :key="index"></span><b>正在同步学习资料…</b></div>
          <div v-else-if="filteredResourceItems.length" class="ios-public-resource-grid"><button v-for="asset in filteredResourceItems" :key="asset.id || asset.url" :disabled="Boolean(openingResource)" type="button" @click="openResource(asset)"><span class="resource-cover"><AppIcon :name="asset.type === 'video' ? 'play' : 'file'" :size="28" /></span><div><small>{{ resourceLabel(asset.category) }}</small><b>{{ asset.title || asset.original_name }}</b><p><template v-if="asset.folder">{{ asset.folder }} / </template>{{ asset.original_name }}</p></div><span class="open-label">{{ openingResource === (asset.id || asset.url) ? '打开中…' : '打开' }} <AppIcon name="chevron" :size="14" /></span></button></div>
          <div v-else class="ios-empty large"><AppIcon name="library" :size="34" /><b>{{ resourceSearch ? '没有找到匹配资料' : '该分类暂无资料' }}</b><span>{{ resourceSearch ? '请尝试更短的关键词。' : '组长上传或 NAS 添加文件后会显示在这里。' }}</span></div>
        </section>

        <section v-else-if="tab === 'profile'" class="ios-profile-page">
          <div class="ios-profile-hero"><div class="profile-photo-wrap"><img v-if="user?.avatar_url" :src="user.avatar_url" alt="个人头像" /><span v-else>{{ (user?.display_name || '?').slice(0,1) }}</span><label :class="{ busy: avatarUploading }" title="更换头像" tabindex="0"><AppIcon name="plus" :size="17" /><span class="sr-only">{{ avatarUploading ? '正在上传头像' : '选择新头像' }}</span><input ref="avatarInput" type="file" accept="image/jpeg,image/png" :disabled="avatarUploading" @change="uploadAvatar" /></label></div><div><small>MY PROFILE</small><h2>{{ user?.display_name }}</h2><p>{{ activeGroup?.name || '门训成员' }} · JPG/PNG 不超过 6MB</p></div></div>
          <div class="ios-profile-grid"><article class="ios-panel"><div class="panel-heading"><div><small>ACCOUNT</small><h2>账号资料</h2></div></div><div class="ios-stack"><label><span>拼音用户名</span><input v-model="profileUsername" autocomplete="username" placeholder="小写字母开头" /></label><label><span>联系邮箱</span><input v-model="profileEmail" autocomplete="email" type="email" /></label><button :disabled="profileSaving" type="button" @click="saveProfile">{{ profileSaving ? '保存中…' : '保存资料' }}</button></div></article><article class="ios-panel"><div class="panel-heading"><div><small>SECURITY</small><h2>登录密码</h2></div></div><p class="panel-description">修改后会自动退出当前账号，请使用新密码重新登录。</p><div class="ios-stack"><label><span>当前密码</span><input v-model="oldPassword" autocomplete="current-password" type="password" /></label><label><span>新密码</span><input v-model="newPassword" autocomplete="new-password" type="password" placeholder="至少 8 位" /></label><label><span>确认新密码</span><input v-model="confirmPassword" autocomplete="new-password" type="password" /></label><button :disabled="passwordSaving" type="button" @click="changePassword">{{ passwordSaving ? '正在更新…' : '更新密码' }}</button></div></article></div>
          <article class="ios-panel ios-session-panel"><div><small>CURRENT SESSION</small><h2>当前登录</h2><p>{{ user?.display_name }} · {{ activeGroup?.name || '未选择小组' }}</p></div><button class="ios-danger-button" type="button" @click="logout"><AppIcon name="logout" :size="18" />退出当前账号</button></article>
        </section>

        <AdminConsole v-else-if="tab === 'admin' && canAdmin" />
      </div>

      <nav class="ios-mobile-tabbar" aria-label="移动端主导航"><button v-for="item in navigation" :key="item.id" :class="{ active: tab === item.id }" :aria-current="tab === item.id ? 'page' : undefined" :aria-label="item.label" type="button" @click="setTab(item.id)"><AppIcon :name="item.icon" :size="21" /><span>{{ item.short }}</span></button></nav>
    </main>
  </div>

  <div v-if="calendar" class="modal-backdrop" @click.self="closeCalendar"><div class="calendar-modal" role="dialog" aria-modal="true" aria-labelledby="member-calendar-title"><div class="calendar-head"><div><small class="ios-kicker">MEMBER CALENDAR</small><h2 id="member-calendar-title">{{ calendar.member?.member_name || calendar.member?.display_name }}</h2><p>{{ calendar.month }} 打卡月历</p></div><button class="ios-secondary-button" type="button" @click="closeCalendar">关闭</button></div><div class="calendar-switcher"><button type="button" aria-label="上一个月" @click="openCalendarMonth(calendar.member,shiftMonth(calendar.month,-1))">‹</button><strong>{{ calendar.month }}</strong><button type="button" aria-label="下一个月" @click="openCalendarMonth(calendar.member,shiftMonth(calendar.month,1))">›</button></div><div class="calendar-weekdays" aria-hidden="true"><span>日</span><span>一</span><span>二</span><span>三</span><span>四</span><span>五</span><span>六</span></div><div class="calendar-grid"><button v-for="(day,index) in calendarDays(calendar.month)" :key="index" class="calendar-day" :class="{ 'empty-day': !day, 'has-record': day && calendarItemMap.get(`${calendar.month}-${String(day).padStart(2,'0')}`)?.length }" :disabled="!day" :aria-label="day ? `${calendar.month}-${String(day).padStart(2,'0')}，${calendarItemMap.get(`${calendar.month}-${String(day).padStart(2,'0')}`)?.length || 0} 项打卡` : undefined" type="button" @click="chooseCalendarDate(day)"><template v-if="day"><b>{{ day }}</b><small>{{ calendarItemMap.get(`${calendar.month}-${String(day).padStart(2,'0')}`)?.length || 0 }} 项</small></template></button></div></div></div>

  <div v-if="networkBusy" class="ios-network-progress" role="progressbar" aria-label="正在同步数据"><span></span></div>
  <div v-if="!online" class="ios-offline-banner" role="alert"><AppIcon name="warning" :size="17" />当前已离线，操作将在网络恢复后重试。</div>
  <Transition name="toast"><div v-if="toastMessage" class="ios-global-toast" role="status" aria-live="polite"><AppIcon name="check" :size="18" /><span>{{ toastMessage }}</span></div></Transition>
</template>
