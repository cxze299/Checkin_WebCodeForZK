<script setup>
import { computed, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import AppIcon from './AppIcon.vue';
import AdminConsole from './AdminConsole.vue';
import { useAppStateStore } from '../stores/appState';
import {
  api, closeCalendar, login, logout, openCalendarMonth, registerAccount,
  refreshCurrentUser, setDefaultGroupAction, setSelectedDate, setTab,
  switchGroup, toast, toggleSidebar,
} from '../legacy-app';

const store = useAppStateStore();
const { authenticated, user, tab, sidebarCollapsed, groups, currentGroupID, defaultGroupID, showGroupPicker, resources, canAdmin, calendar } = storeToRefs(store);

const authMode = ref('login');
const loginIdentifier = ref('');
const loginPassword = ref('');
const authError = ref('');
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

const navigation = computed(() => [
  { id: 'home', label: '今日打卡', short: '打卡', icon: 'home' },
  { id: 'dashboard', label: '成长数据', short: '数据', icon: 'chart' },
  { id: 'resources', label: '资料库', short: '资料', icon: 'library' },
  { id: 'profile', label: '个人中心', short: '我的', icon: 'user' },
  ...(canAdmin.value ? [{ id: 'admin', label: '管理后台', short: '管理', icon: 'settings' }] : []),
]);

const activeGroup = computed(() => groups.value.find((group) => Number(group.id) === Number(currentGroupID.value)));
const pageMeta = computed(() => ({
  dashboard: ['成长数据', '查看小组进度与个人坚持'],
  resources: ['资料库', '本组共享的读物与学习资料'],
  profile: ['个人中心', '管理头像、账号与安全设置'],
  admin: ['管理后台', '管理任务、成员与学习资料'],
})[tab.value] || ['', '']);

watch(tab, (value) => { if (value === 'profile') syncProfile(); });

function friendlyError(code) {
  return ({ invalid_username_or_password: '邮箱、用户名或密码不正确', invalid_password: '当前密码不正确', password_too_short: '新密码至少需要 8 位', roster_not_found: '姓名与所选门训组不匹配', roster_already_claimed: '该名单席位已经注册', email_exists: '该邮箱已经注册', weak_password: '密码至少需要 8 位', invalid_email: '请填写有效邮箱', profile_conflict: '用户名或邮箱已被使用' })[code] || code;
}

async function chooseAuthMode(mode) {
  authMode.value = mode; authError.value = '';
  if (mode === 'register' && !registrationGroups.value.length) {
    try { const data = await api('/auth/registration-groups'); registrationGroups.value = data.groups || []; }
    catch (error) { authError.value = friendlyError(error.message); }
  }
}

async function submitLogin() {
  authError.value = '';
  try { await login(loginIdentifier.value, loginPassword.value); }
  catch (error) { authError.value = friendlyError(error.message); }
}

async function previewRegistration() {
  registrationPreview.value = null; authError.value = '';
  if (!registerName.value || !registerGroupID.value) return;
  try { registrationPreview.value = await api('/auth/registration-preview', { method: 'POST', body: JSON.stringify({ name: registerName.value, group_id: Number(registerGroupID.value) }) }); }
  catch (error) { authError.value = friendlyError(error.message); }
}

async function submitRegister() {
  authError.value = '';
  try { await registerAccount({ name: registerName.value, group_id: Number(registerGroupID.value), email: registerEmail.value, password: registerPassword.value }); }
  catch (error) { authError.value = friendlyError(error.message); }
}

function syncProfile() { profileUsername.value = user.value?.username || ''; profileEmail.value = user.value?.email || ''; }

async function saveProfile() {
  try { await api('/auth/profile', { method: 'PUT', body: JSON.stringify({ username: profileUsername.value, email: profileEmail.value }) }); await refreshCurrentUser(); toast('个人资料已保存'); }
  catch (error) { toast(friendlyError(error.message)); }
}

async function uploadAvatar() {
  const file = avatarInput.value?.files?.[0]; if (!file) return;
  const body = new FormData(); body.append('avatar', file);
  const res = await fetch('/api/auth/avatar', { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('agp_token') || ''}` }, body });
  const data = await res.json().catch(() => ({})); if (!res.ok) return toast(friendlyError(data.error));
  await refreshCurrentUser(); toast('头像已更新');
}

async function changePassword() {
  if (newPassword.value.length < 8) return toast('新密码至少需要 8 位');
  if (newPassword.value !== confirmPassword.value) return toast('两次输入的新密码不一致');
  try { await api('/auth/change-password', { method: 'POST', body: JSON.stringify({ old_password: oldPassword.value, new_password: newPassword.value }) }); oldPassword.value = ''; newPassword.value = ''; confirmPassword.value = ''; toast('登录密码已更新'); }
  catch (error) { toast(friendlyError(error.message)); }
}

function resourceLabel(category) { return ({ book: '读物', handout: '讲义', markdown: '文章', outline: '提纲', video: '视频' })[category] || '资料'; }
function openResource(asset) { window.open(`/api/assets/${asset.id}/download`, '_blank', 'noopener'); }

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
  <div v-if="!authenticated" class="ios-auth-screen">
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
          <label><span>邮箱或用户名</span><input v-model="loginIdentifier" autocomplete="username" placeholder="name@example.com" @keydown.enter="submitLogin" /></label>
          <label><span>密码</span><input v-model="loginPassword" autocomplete="current-password" type="password" placeholder="请输入密码" @keydown.enter="submitLogin" /></label>
          <button type="submit">登录</button>
        </form>
      </template>
      <template v-else>
        <header><h2>创建账号</h2><p>请使用报名名单中的姓名和组别</p></header>
        <form class="ios-auth-form" @submit.prevent="submitRegister">
          <label><span>姓名</span><input v-model="registerName" placeholder="真实姓名" @blur="previewRegistration" /></label>
          <label><span>门训组</span><select v-model="registerGroupID" @change="previewRegistration"><option value="">请选择</option><option v-for="group in registrationGroups" :key="group.id" :value="group.id">{{ group.name }}</option></select></label>
          <div v-if="registrationPreview?.matched" class="ios-roster-match" :class="{ error: registrationPreview.claimed }"><AppIcon :name="registrationPreview.claimed ? 'lock' : 'check'" /><div><b>{{ registrationPreview.canonical_name }}</b><small>{{ registrationPreview.claimed ? '该名单已经注册' : `用户名将设为 ${registrationPreview.suggested_username}` }}</small></div></div>
          <label><span>联系邮箱</span><input v-model="registerEmail" autocomplete="email" type="email" placeholder="用于登录" /></label>
          <label><span>密码</span><input v-model="registerPassword" autocomplete="new-password" type="password" placeholder="至少 8 位" @keydown.enter="submitRegister" /></label>
          <button :disabled="registrationPreview?.claimed" type="submit">注册并进入</button>
        </form>
      </template>
      <div v-if="authError" class="ios-form-error">{{ authError }}</div>
    </section>
  </div>

  <div v-else class="ios-app-shell" :class="{ collapsed: sidebarCollapsed }">
    <aside class="ios-sidebar">
      <div class="ios-sidebar-brand"><div class="ios-mini-logo">A</div><div v-if="!sidebarCollapsed"><b>门训打卡</b><small>{{ activeGroup?.name || 'AGP' }}</small></div></div>
      <nav><button v-for="item in navigation" :key="item.id" :class="{ active: tab === item.id }" :title="item.label" type="button" @click="setTab(item.id)"><span><AppIcon :name="item.icon" :size="21" /></span><b v-if="!sidebarCollapsed">{{ item.label }}</b></button></nav>
      <div class="ios-sidebar-account"><button class="ios-account-button" type="button" @click="setTab('profile')"><img v-if="user?.avatar_url" :src="user.avatar_url" alt="头像" /><span v-else>{{ (user?.display_name || '?').slice(0,1) }}</span><div v-if="!sidebarCollapsed"><b>{{ user?.display_name }}</b><small>@{{ user?.username }}</small></div></button><button class="ios-logout-button" type="button" title="退出登录" @click="logout"><AppIcon name="logout" :size="19" /></button></div>
      <button class="ios-collapse-button" type="button" :title="sidebarCollapsed ? '展开导航' : '收起导航'" @click="toggleSidebar"><AppIcon name="chevron" :size="18" /></button>
    </aside>

    <main class="ios-main-area">
      <header v-if="tab !== 'home' || groups.length > 1" class="ios-topbar" :class="{ home: tab === 'home' }">
        <div v-if="tab !== 'home'"><small>{{ activeGroup?.name || 'AGP' }}</small><h1>{{ pageMeta[0] }}</h1><p>{{ pageMeta[1] }}</p></div>
        <div v-if="groups.length > 1" class="ios-group-switcher"><span>当前小组</span><select :value="currentGroupID || ''" @change="$event.target.value && switchGroup($event.target.value)"><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select><button v-if="currentGroupID && defaultGroupID !== currentGroupID" type="button" @click="setDefaultGroupAction(currentGroupID)">设为默认</button></div>
      </header>

      <div class="ios-content-area" :class="{ 'admin-content': tab === 'admin' }">
        <section v-if="showGroupPicker" class="ios-group-picker"><header><small>SELECT GROUP</small><h1>选择要进入的小组</h1><p>你的打卡与资料会按小组独立显示。</p></header><div><button v-for="group in groups" :key="group.id" type="button" @click="switchGroup(group.id)"><span><AppIcon name="users" /></span><div><b>{{ group.name }}</b><small>{{ group.code }}</small></div><AppIcon name="chevron" /></button></div></section>
        <div v-else-if="tab === 'home'" id="vue-checkin-workbench"></div>
        <div v-else-if="tab === 'dashboard'" id="vue-dashboard"></div>

        <section v-else-if="tab === 'resources'" class="ios-public-library">
          <div v-if="resources.length" class="ios-public-resource-grid"><button v-for="asset in resources" :key="asset.id" type="button" @click="openResource(asset)"><span class="resource-cover"><AppIcon name="file" :size="28" /></span><div><small>{{ resourceLabel(asset.category) }}</small><b>{{ asset.title }}</b><p>{{ asset.original_name }}</p></div><span class="open-label">打开 <AppIcon name="chevron" :size="14" /></span></button></div>
          <div v-else class="ios-empty large"><AppIcon name="library" :size="34" /><b>资料库还是空的</b><span>组长上传资料后会显示在这里。</span></div>
        </section>

        <section v-else-if="tab === 'profile'" class="ios-profile-page">
          <div class="ios-profile-hero"><div class="profile-photo-wrap"><img v-if="user?.avatar_url" :src="user.avatar_url" alt="个人头像" /><span v-else>{{ (user?.display_name || '?').slice(0,1) }}</span><label title="更换头像"><AppIcon name="plus" :size="17" /><input ref="avatarInput" type="file" accept="image/jpeg,image/png" @change="uploadAvatar" /></label></div><div><small>MY PROFILE</small><h2>{{ user?.display_name }}</h2><p>{{ activeGroup?.name || '门训成员' }}</p></div></div>
          <div class="ios-profile-grid"><article class="ios-panel"><div class="panel-heading"><div><small>ACCOUNT</small><h2>账号资料</h2></div></div><div class="ios-stack"><label><span>拼音用户名</span><input v-model="profileUsername" placeholder="小写字母开头" /></label><label><span>联系邮箱</span><input v-model="profileEmail" type="email" /></label><button type="button" @click="saveProfile">保存资料</button></div></article><article class="ios-panel"><div class="panel-heading"><div><small>SECURITY</small><h2>登录密码</h2></div></div><p class="panel-description">只修改您自己的登录密码，不影响其他成员。</p><div class="ios-stack"><label><span>当前密码</span><input v-model="oldPassword" autocomplete="current-password" type="password" /></label><label><span>新密码</span><input v-model="newPassword" autocomplete="new-password" type="password" placeholder="至少 8 位" /></label><label><span>确认新密码</span><input v-model="confirmPassword" autocomplete="new-password" type="password" /></label><button type="button" @click="changePassword">更新密码</button></div></article></div>
        </section>

        <AdminConsole v-else-if="tab === 'admin' && canAdmin" />
      </div>

      <nav class="ios-mobile-tabbar"><button v-for="item in navigation" :key="item.id" :class="{ active: tab === item.id }" type="button" @click="setTab(item.id)"><AppIcon :name="item.icon" :size="21" /><span>{{ item.short }}</span></button></nav>
    </main>
  </div>

  <div v-if="calendar" class="modal-backdrop" @click="$event.target.className === 'modal-backdrop' && closeCalendar()"><div class="calendar-modal"><div class="calendar-head"><div><small class="ios-kicker">MEMBER CALENDAR</small><h2>{{ calendar.member?.member_name || calendar.member?.display_name }}</h2><p>{{ calendar.month }} 打卡月历</p></div><button class="ios-secondary-button" type="button" @click="closeCalendar">关闭</button></div><div class="calendar-switcher"><button type="button" @click="openCalendarMonth(calendar.member,shiftMonth(calendar.month,-1))">‹</button><strong>{{ calendar.month }}</strong><button type="button" @click="openCalendarMonth(calendar.member,shiftMonth(calendar.month,1))">›</button></div><div class="calendar-weekdays"><span>日</span><span>一</span><span>二</span><span>三</span><span>四</span><span>五</span><span>六</span></div><div class="calendar-grid"><button v-for="(day,index) in calendarDays(calendar.month)" :key="index" class="calendar-day" :class="{ 'empty-day': !day, 'has-record': day && calendarItemsByDate(calendar.items).get(`${calendar.month}-${String(day).padStart(2,'0')}`)?.length }" :disabled="!day" type="button" @click="chooseCalendarDate(day)"><template v-if="day"><b>{{ day }}</b><small>{{ calendarItemsByDate(calendar.items).get(`${calendar.month}-${String(day).padStart(2,'0')}`)?.length || 0 }} 项</small></template></button></div></div></div>
</template>
