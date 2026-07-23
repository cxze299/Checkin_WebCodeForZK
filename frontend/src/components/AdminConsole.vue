<script setup>
import { computed, nextTick, onBeforeUnmount, onMounted, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import AppIcon from './AppIcon.vue';
import AppDatePicker from './AppDatePicker.vue';
import { useAppStateStore } from '../stores/appState';
import { confirmDialog, promptDialog } from '../ui/dialog';
import {
  addWeekBinding, api, applyBindingSelection, applyOutlineSelection, createWeekDraft,
  deleteWeekDraft, downloadAdminExport, importLocalBackupJSON, importStudyWeeksExcel,
  librarySelectionValue, loadAdminData, previewLibraryItem, removeMember, removeWeekBinding,
  reloadApp, restoreWeekDraftDefaults, saveLearningConfig, saveWeekDraft, selectWeekForEditing,
  setAdminSection, setMemberAdmin, setResourceLibraryVisibility, toast, updateLearningValue, updateWeekBinding,
  updateWeekDraftField, uploadLibraryFile,
} from '../legacy-app';

const store = useAppStateStore();
const { user, adminSection, members, weeks, weekDraft, learningConfig, resourceLibrary, canEditLearning, adminLoading, groups, currentGroupID } = storeToRefs(store);

const uploadInput = ref(null);
const uploadCategory = ref('book');
const rosterInput = ref(null);
const rosterPreview = ref(null);
const rosterEntries = ref([]);
const rosterName = ref('');
const rosterGroupID = ref('');
const rosterLeader = ref(false);
const rosterMinor = ref(false);
const studyWeeksInput = ref(null);
const backupInput = ref(null);
const legacyConfigInput = ref(null);
const legacyRecordsInput = ref(null);
const legacyPreview = ref(null);
const activeLibraryKey = ref('');
const activeLibraryFolder = ref('');
const newFolderName = ref('');
const resourceSearch = ref('');
const rosterSearch = ref('');
const pendingAction = ref('');
const adminNav = ref(null);
let adminNavMobileQuery = null;

const settings = computed(() => learningConfig.value || {});
const daily = computed(() => settings.value.task_sections?.daily || {});
const devotion = computed(() => daily.value.devotion || {});
const scripture = computed(() => daily.value.scripture || {});
const libraryItems = computed(() => resourceLibrary.value.flatMap((section) => section.items || []));
const readingOptions = computed(() => libraryItems.value.filter((item) => ['markdown', 'pdf'].includes(item.type)));
const outlineOptions = computed(() => libraryItems.value.filter((item) => item.type === 'image'));
const activeLibrarySection = computed(() => resourceLibrary.value.find((section) => section.key === activeLibraryKey.value) || resourceLibrary.value[0] || null);
const visibleLibraryItems = computed(() => {
  let items = activeLibrarySection.value?.items || [];
  if (activeLibraryFolder.value) items = items.filter((item) => item.folder === activeLibraryFolder.value);
  const keyword = normalizedName(resourceSearch.value);
  if (!keyword) return items;
  return items.filter((item) => normalizedName(`${item.title || ''} ${item.original_name || ''} ${item.relative_path || ''} ${item.folder || ''}`).includes(keyword));
});
const currentWeek = computed(() => weeks.value.find((week) => weekStatus(week) === '进行中') || weeks.value[0]);
const completedProfiles = computed(() => members.value.filter((item) => item.username).length);
const activeGroupName = computed(() => groups.value.find((group) => Number(group.id) === Number(currentGroupID.value))?.name || '当前小组');
const isReadOnlyAdmin = computed(() => !canEditLearning.value);
const canManageResources = computed(() => Boolean(user.value?.is_super_admin || user.value?.roles?.some((role) => ['group_admin', 'group_leader'].includes(role))));
const resourceTargetWeek = computed(() => {
  if (!weekDraft.value) return null;
  if (!weekDraft.value.id) return weekDraft.value;
  return weeks.value.find((week) => Number(week.id) === Number(weekDraft.value.id)) || weekDraft.value;
});
const resourceTargetLabel = computed(() => {
  const target = resourceTargetWeek.value;
  if (!target) return '尚未选择周任务';
  const title = target.title || (target.id ? '未命名周任务' : '尚未保存的新周任务');
  return target.start && target.end ? `${title}（${target.start} — ${target.end}）` : title;
});
const filteredRosterEntries = computed(() => {
  const keyword = normalizedName(rosterSearch.value);
  if (!keyword) return rosterEntries.value;
  return rosterEntries.value.filter((entry) => normalizedName([
    entry.name,
    entry.group_name,
    entry.is_minor ? '辅修' : '主修',
    entry.is_leader ? '组长' : '',
    entry.claimed_by_user_id ? '已注册' : '待注册',
  ].join(' ')).includes(keyword));
});

watch(resourceLibrary, (sections) => {
  if (!sections?.some((section) => section.key === activeLibraryKey.value)) activeLibraryKey.value = sections?.[0]?.key || '';
}, { immediate: true });
watch(activeLibrarySection, (section) => {
  if (activeLibraryFolder.value && !(section?.folders || []).some((folder) => folder.path === activeLibraryFolder.value)) activeLibraryFolder.value = '';
});
watch(adminSection, () => centerActiveAdminSection());

const sections = computed(() => [
  { id: 'overview', label: '管理概览', icon: 'chart' },
  { id: 'learning', label: '任务计划', icon: 'calendar' },
  { id: 'members', label: '成员权限', icon: 'users' },
  { id: 'library', label: '学习资源', icon: 'library' },
  ...(user.value?.is_super_admin ? [{ id: 'roster', label: '报名名单', icon: 'file' }] : []),
  { id: 'data', label: '数据工具', icon: 'database' },
]);

function localDateKey(date = new Date()) {
  return `${date.getFullYear()}-${String(date.getMonth() + 1).padStart(2, '0')}-${String(date.getDate()).padStart(2, '0')}`;
}

function centerActiveAdminSection(behavior = 'smooth') {
  nextTick(() => {
    const nav = adminNav.value;
    if (!nav || !window.matchMedia('(max-width: 900px)').matches) return;
    const active = nav.querySelector('[aria-current="page"]');
    if (!active) return;
    const navRect = nav.getBoundingClientRect();
    const activeRect = active.getBoundingClientRect();
    const offset = activeRect.left - navRect.left - (nav.clientWidth - activeRect.width) / 2;
    const left = Math.max(0, Math.min(nav.scrollWidth - nav.clientWidth, nav.scrollLeft + offset));
    if (Math.abs(left - nav.scrollLeft) > 1) nav.scrollTo({ left, behavior });
  });
}

function handleAdminNavViewportChange(event) {
  if (event.matches) centerActiveAdminSection('auto');
}

function weekStatus(week) {
  const today = localDateKey();
  if (!week) return '未设置';
  if (today < week.start) return '即将开始';
  if (today > week.end) return '已结束';
  return '进行中';
}

function roleLabel(member) {
  if (member.is_super_admin) return '超级管理员';
  if (member.roles?.includes('group_leader')) return '组长';
  if (member.roles?.includes('group_admin')) return '管理员';
  return '成员';
}

function optionText(item) { return item.title || item.original_name || '未命名资源'; }
function updateLearning(path, value) { updateLearningValue(path, value); }
function isPending(key) { return pendingAction.value === key; }
async function withPending(key, action) {
  if (pendingAction.value) return;
  pendingAction.value = key;
  try { return await action(); }
  finally { pendingAction.value = ''; }
}

function validateWeekDates() {
  const draft = weekDraft.value;
  if (!draft?.start || !draft?.end) return '请完整填写开始日期和结束日期';
  if (draft.start > draft.end) return '结束日期不能早于开始日期';
  const conflict = weeks.value.find((week) => Number(week.id) !== Number(draft.id || 0) && draft.start <= week.end && draft.end >= week.start);
  if (conflict) return `日期与“${conflict.title || '未命名周任务'}”（${conflict.start} — ${conflict.end}）重叠`;
  return '';
}

async function saveWeekWithValidation() {
  if (!canEditLearning.value) return toast('当前账号只有任务只读权限');
  const error = validateWeekDates();
  if (error) return toast(error);
  await withPending('save-week', saveWeekDraft);
}

async function deleteWeekWithPending() {
  if (!canEditLearning.value) return;
  await withPending('delete-week', deleteWeekDraft);
}

async function changeMemberAdmin(member) {
  const key = `member-role-${member.member_id}`;
  await withPending(key, () => setMemberAdmin(member, !member.roles?.includes('group_admin')));
}

async function removeMemberWithPending(member) {
  const key = `remove-member-${member.member_id}`;
  await withPending(key, () => removeMember(member));
}

async function chooseSection(id) {
  setAdminSection(id);
  if (id === 'overview') await loadAdminData();
  if (id === 'roster') await loadRoster();
}

async function loadRoster() {
  return withPending('load-roster', async () => {
    try { const data = await api('/super-admin/roster'); rosterEntries.value = data.entries || []; }
    catch (error) { toast(error.message); }
  });
}

async function uploadResource() {
  if (!canEditLearning.value) return toast('当前账号只有资源只读权限');
  await withPending('upload-resource', () => uploadLibraryFile(uploadInput.value, uploadCategory.value));
}

async function toggleResourceVisibility(item) {
  if (!canManageResources.value || !item?.resource_key) return;
  const visible = !item.visible_in_library;
  await withPending(`resource-visibility-${item.resource_key}`, async () => {
    try {
      await setResourceLibraryVisibility(item, visible);
      toast(visible ? '已显示在成员资料库' : '已从成员资料库隐藏');
    } catch (error) {
      toast(`保存失败：${error.message}`);
    }
  });
}

function chooseLibrarySection(key) { activeLibraryKey.value = key; activeLibraryFolder.value = ''; resourceSearch.value = ''; }
async function syncNASResources() {
  await withPending('sync-nas', async () => {
    try { await loadAdminData(true, true); toast('NAS 资源目录已重新扫描'); }
    catch (error) { toast(`同步失败：${error.message}`); }
  });
}
async function createResourceFolder() {
  if (!user.value?.is_super_admin) return toast('只有超级管理员可以修改全局 NAS 文件夹');
  const section = activeLibrarySection.value;
  if (!section?.managed_root) return toast('请选择 Book、Passage、PPT 或 Mentor 分类');
  if (!newFolderName.value.trim()) return toast('请输入文件夹名称');
  await withPending('create-folder', async () => {
    try {
      const data = await api('/admin/resource-folders', { method: 'POST', body: JSON.stringify({ root: section.managed_root, parent: activeLibraryFolder.value, name: newFolderName.value }) });
      newFolderName.value = ''; await loadAdminData(true, true); activeLibraryFolder.value = data.path || '';
      toast('文件夹已创建，可以在 NAS 中向该目录上传文件');
    } catch (error) { toast(`创建失败：${error.message}`); }
  });
}
async function renameResourceFolder() {
  if (!user.value?.is_super_admin) return toast('只有超级管理员可以修改全局 NAS 文件夹');
  const section = activeLibrarySection.value;
  if (!section?.managed_root || !activeLibraryFolder.value) return;
  const currentName = activeLibraryFolder.value.split('/').pop();
  const name = await promptDialog({ title: '重命名文件夹', message: '请输入新的文件夹名称。已发布任务中的旧路径可能需要重新挂载。', defaultValue: currentName, confirmLabel: '确认重命名' });
  if (!name || name === currentName) return;
  const warning = `即将把“${activeLibraryFolder.value}”重命名为“${name}”。\n\n已发布任务如果保存了旧文件路径，重命名后可能无法打开，需要重新选择资源。确认继续吗？`;
  if (!await confirmDialog({ title: '确认重命名文件夹', message: warning, confirmLabel: '继续重命名', tone: 'danger' })) return;
  await withPending('rename-folder', async () => {
    try {
      const data = await api('/admin/resource-folders', { method: 'PUT', body: JSON.stringify({ root: section.managed_root, path: activeLibraryFolder.value, name }) });
      await loadAdminData(true, true); activeLibraryFolder.value = data.path || '';
      toast('文件夹已重命名，请检查引用该目录的已发布任务');
    } catch (error) { toast(`重命名失败：${error.message}`); }
  });
}

async function previewRoster() {
  const file = rosterInput.value?.files?.[0]; if (!file) return toast('请先选择 Excel 文件');
  await withPending('preview-roster', async () => {
    try {
      const body = new FormData(); body.append('file', file);
      const res = await fetch('/api/super-admin/roster/preview', { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('agp_token') || ''}` }, body });
      rosterPreview.value = await res.json(); if (!res.ok) toast(rosterPreview.value.error || '名单解析失败');
    } catch (error) { rosterPreview.value = null; toast(`名单解析失败：${error.message}`); }
  });
}

async function importRoster() {
  const file = rosterInput.value?.files?.[0]; if (!file || !rosterPreview.value?.row_count) return;
  if (!await confirmDialog({ title: '同步报名名单', message: `确认将预览中的 ${rosterPreview.value.row_count} 个名单席位同步到系统吗？`, confirmLabel: '确认同步' })) return;
  await withPending('import-roster', async () => {
    try {
      const body = new FormData(); body.append('file', file);
      const res = await fetch('/api/super-admin/roster/import', { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('agp_token') || ''}` }, body });
      const data = await res.json(); if (!res.ok) return toast(data.error || '名单同步失败');
      rosterPreview.value = null; toast(`已同步 ${data.imported} 条名单`);
      const refreshed = await api('/super-admin/roster'); rosterEntries.value = refreshed.entries || [];
    } catch (error) { toast(`名单同步失败：${error.message}`); }
  });
}

async function addRosterEntry() {
  await withPending('add-roster', async () => {
    try {
      await api('/super-admin/roster/entries', { method: 'POST', body: JSON.stringify({ name: rosterName.value, group_id: Number(rosterGroupID.value), is_leader: rosterLeader.value, is_minor: rosterMinor.value, status: 1 }) });
      rosterName.value = ''; rosterGroupID.value = ''; rosterLeader.value = false; rosterMinor.value = false;
      toast('名单席位已添加');
      const refreshed = await api('/super-admin/roster'); rosterEntries.value = refreshed.entries || [];
    } catch (error) { toast(error.message); }
  });
}

async function runExport(path, name, message) {
  await withPending(`export-${name}`, async () => {
    try { await downloadAdminExport(path, name, message); }
    catch (error) { toast(error.message); }
  });
}
async function runWeeksImport() {
  if (!studyWeeksInput.value?.files?.[0]) return toast('请先选择任务计划 Excel');
  if (!await confirmDialog({ title: '导入任务计划', message: `任务计划将导入“${activeGroupName.value}”，确认继续吗？`, confirmLabel: '开始导入' })) return;
  await withPending('import-weeks', async () => {
    try { await importStudyWeeksExcel(studyWeeksInput.value); await reloadApp(); await loadAdminData(true); }
    catch (error) { toast(error.message); }
  });
}
async function runBackupImport() {
  if (!backupInput.value?.files?.[0]) return toast('请先选择完整备份 JSON');
  if (!await confirmDialog({ title: '恢复完整备份', message: `完整备份将写入“${activeGroupName.value}”，并可能替换当前任务和打卡数据。`, confirmLabel: '进入最后确认', tone: 'danger' })) return;
  if (!await confirmDialog({ title: '最后确认', message: `立即恢复“${activeGroupName.value}”的完整备份？此操作无法在网页中撤销。`, confirmLabel: '立即恢复', tone: 'danger' })) return;
  await withPending('import-backup', async () => {
    try {
      await importLocalBackupJSON(backupInput.value);
      await reloadApp();
      await loadAdminData(true);
      toast('完整备份已恢复，页面数据已重新载入');
    } catch (error) { toast(error.message); }
  });
}

function normalizedName(value) { return String(value || '').trim().replace(/\s+/g, '').toLocaleLowerCase(); }
function titleText(value) { return Array.isArray(value) ? value.filter(Boolean).join(' / ') : String(value || ''); }
function boolValue(value, fallback = true) { return typeof value === 'boolean' ? value : fallback; }
function legacyBinding(item, fallbackType = 'pdf') {
  if (!item) return null;
  if (typeof item === 'string') return { title: item, url: '', type: fallbackType, asset_id: 0 };
  return { title: item.title || '', url: item.url || '', type: item.type || fallbackType, asset_id: 0 };
}

function currentMemberMap() {
  const result = new Map();
  for (const member of members.value) {
    const profile = {
      username: member.username,
      display_name: member.member_name || member.display_name || member.username,
      name_pinyin: member.name_pinyin || member.username,
      roles: Array.isArray(member.roles) ? member.roles : [],
    };
    for (const name of [member.member_name, member.display_name, member.username, member.name_pinyin]) {
      if (name) result.set(normalizedName(name), profile);
    }
  }
  return result;
}

function convertLegacyWeek(week) {
  const videoItems = Array.isArray(week.videos) ? week.videos.map((item) => legacyBinding(item, 'video')).filter(Boolean) : [];
  if (!videoItems.length && (week.video || week.url)) videoItems.push({ title: week.video || '本周视频', url: week.url || '', type: 'video', asset_id: 0 });
  return {
    start_date: week.start || week.start_date || '', end_date: week.end || week.end_date || '', title: titleText(week.title),
    verse_ref: week.verse || week.verse_ref || '', recite_text: week.reciteText || week.recite_text || '',
    book_enabled: boolValue(week.book_enabled), video_enabled: boolValue(week.video_enabled),
    verse_enabled: boolValue(week.verse_enabled), outline_enabled: boolValue(week.outline_enabled),
    readings: (Array.isArray(week.readings) ? week.readings : []).map((item) => legacyBinding(item, 'pdf')).filter(Boolean),
    videos: videoItems,
    outline: week.outlineImage ? { title: '提纲图片', url: week.outlineImage, type: 'image', asset_id: 0 } : legacyBinding(week.outline, 'image') || { title: '', url: '', type: 'image', asset_id: 0 },
  };
}

function recordRows(record, username) {
  const common = {
    username, logical_date: record.logical_date || '', checkin_time: record.checkin_time || '',
    part: record.part || '', detail: record.detail || '', note: record.note || '',
    is_retro: record.is_retro === true || record.is_retro === 1 || ['1','true','yes'].includes(String(record.is_retro || '').toLowerCase()),
  };
  const rows = [];
  const add = (taskType) => rows.push({ ...common, task_type: taskType, detail: common.detail || taskType });
  if (String(record.daily || '').toLowerCase() === 'done') add('daily_devotion');
  if (String(record.book || '').toLowerCase() === 'done') add('weekly_book');
  if (String(record.video || '').toLowerCase() === 'done') add('weekly_video');
  if (String(record.verse || '').toLowerCase() === 'done') add('weekly_verse');
  if (record.kind === 'reflection') add('reflection');
  if (record.kind === 'recite_exam') add('recite_exam');
  return rows;
}

async function previewLegacyRestore() {
  await withPending('preview-legacy', async () => {
    try {
      const configFile = legacyConfigInput.value?.files?.[0];
      if (!configFile) return toast('请先选择旧网站的 config.json');
      const config = JSON.parse(await configFile.text());
      const recordsFile = legacyRecordsInput.value?.files?.[0];
      const records = recordsFile ? JSON.parse(await recordsFile.text()) : [];
      if (!Array.isArray(config.weekly_schedule)) throw new Error('config.json 中没有 weekly_schedule');
      if (!Array.isArray(records)) throw new Error('打卡记录 JSON 必须是数组');
      const memberMap = currentMemberMap();
      const unmatched = new Set();
      const checkins = [];
      for (const record of records) {
        const member = memberMap.get(normalizedName(record.name));
        if (!member?.username) { if (record.name) unmatched.add(record.name); continue; }
        checkins.push(...recordRows(record, member.username));
      }
      const uniqueMembers = [...new Map([...memberMap.values()].map((item) => [item.username, item])).values()];
      legacyPreview.value = {
        payload: {
          version: 1, exported_at: new Date().toISOString(), group: { id: currentGroupID.value },
          settings: { site_info: config.site_info || {}, task_sections: config.task_sections || {}, mounted_files: config.mounted_files || {}, class_rep_shares: config.class_rep_shares || [] },
          members: uniqueMembers, weeks: config.weekly_schedule.map(convertLegacyWeek), checkins, feedbacks: [], assets: [],
        },
        weeks: config.weekly_schedule.length, sourceRecords: records.length, checkins: checkins.length, unmatched: [...unmatched],
      };
    } catch (error) { legacyPreview.value = null; toast(`解析失败：${error.message}`); }
  });
}

async function confirmLegacyRestore() {
  if (!legacyPreview.value?.payload) return;
  const message = `将用旧网站数据替换“${activeGroupName.value}”的任务设置和打卡记录。\n\n任务：${legacyPreview.value.weeks} 周\n打卡明细：${legacyPreview.value.checkins} 条\n\n确认进入最后确认吗？`;
  if (!await confirmDialog({ title: '恢复旧网站数据', message, confirmLabel: '进入最后确认', tone: 'danger' })) return;
  if (!await confirmDialog({ title: '最后确认', message: `立即覆盖“${activeGroupName.value}”的任务设置和打卡记录？此操作无法在网页中撤销。`, confirmLabel: '立即覆盖', tone: 'danger' })) return;
  await withPending('restore-legacy', async () => {
    try {
      await api('/admin/imports/local-backup', { method: 'POST', body: JSON.stringify(legacyPreview.value.payload), timeout: 15 * 60 * 1000 });
      legacyPreview.value = null;
      if (legacyConfigInput.value) legacyConfigInput.value.value = '';
      if (legacyRecordsInput.value) legacyRecordsInput.value.value = '';
      await reloadApp();
      await loadAdminData(true);
      toast('旧网站任务设置和打卡记录已恢复，页面数据已重新载入');
    } catch (error) { toast(`恢复失败：${error.message}`); }
  });
}

function selectResourceForTask(item) {
  if (!canEditLearning.value) return toast('当前账号只能预览资源，不能修改任务');
  if (!weekDraft.value) createWeekDraft();
  const value = librarySelectionValue(item);
  if (!value) return toast('该资源没有可用地址');
  const isImage = item.type === 'image' || item.category === 'outline';
  const isVideo = item.type === 'video' || item.category === 'video';
  if (isImage) applyOutlineSelection(value);
  else {
    const kind = isVideo ? 'videos' : 'readings';
    const list = weekDraft.value?.[kind] || [];
    let index = list.findIndex((entry) => !entry.title && !entry.url && !entry.asset_id);
    if (index < 0) { addWeekBinding(kind); index = list.length; }
    applyBindingSelection(kind, index, value);
  }
  setAdminSection('learning');
  toast('资源已选入当前任务，请确认周次后保存');
}

onMounted(() => {
  if (!['overview','learning','members','library','roster','data'].includes(adminSection.value)) setAdminSection('overview');
  adminNavMobileQuery = window.matchMedia('(max-width: 900px)');
  if (adminNavMobileQuery.addEventListener) adminNavMobileQuery.addEventListener('change', handleAdminNavViewportChange);
  else adminNavMobileQuery.addListener(handleAdminNavViewportChange);
  if (adminNavMobileQuery.matches) centerActiveAdminSection('auto');
  loadAdminData();
});

onBeforeUnmount(() => {
  if (!adminNavMobileQuery) return;
  if (adminNavMobileQuery.removeEventListener) adminNavMobileQuery.removeEventListener('change', handleAdminNavViewportChange);
  else adminNavMobileQuery.removeListener(handleAdminNavViewportChange);
});
</script>

<template>
  <div class="ios-admin-layout">
    <aside ref="adminNav" class="ios-admin-nav" role="navigation" aria-label="管理功能导航">
      <div class="ios-admin-nav-title"><small>ADMIN CONSOLE</small><b>小组管理</b></div>
      <button v-for="section in sections" :key="section.id" :class="{ active: adminSection === section.id }" :aria-current="adminSection === section.id ? 'page' : undefined" type="button" @click="chooseSection(section.id)">
        <AppIcon :name="section.icon" :size="19" /><span>{{ section.label }}</span><AppIcon class="admin-nav-chevron" name="chevron" :size="16" />
      </button>
    </aside>

    <main class="ios-admin-main">
      <div v-if="isReadOnlyAdmin && ['learning','library','data'].includes(adminSection)" class="ios-roster-match" role="status">
        <AppIcon name="lock" :size="18" />
        <div><b>当前为小组管理员只读模式</b><small>可以查看资源、导出数据和管理成员；只有组长或超级管理员可以修改任务、上传资源或执行恢复。</small></div>
      </div>
      <div v-if="adminLoading && !['overview','members','roster','data'].includes(adminSection)" class="ios-loading">正在载入管理数据…</div>

      <section v-else-if="adminSection === 'overview'" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>OVERVIEW</small><h1>管理概览</h1><p>今天需要关注的内容集中在这里。</p></div></header>
        <div class="ios-metric-grid">
          <button type="button" @click="chooseSection('members')"><span class="metric-icon blue"><AppIcon name="users" /></span><b>{{ members.length }}</b><small>当前组成员</small></button>
          <button type="button" @click="chooseSection('learning')"><span class="metric-icon purple"><AppIcon name="calendar" /></span><b>{{ weeks.length }}</b><small>周任务计划</small></button>
          <button type="button" @click="chooseSection('library')"><span class="metric-icon green"><AppIcon name="library" /></span><b>{{ libraryItems.length }}</b><small>可用学习资源</small></button>
          <button type="button" @click="chooseSection('members')"><span class="metric-icon orange"><AppIcon name="check" /></span><b>{{ completedProfiles }}</b><small>已启用账号</small></button>
        </div>
        <div class="ios-panel-grid">
          <article class="ios-panel current-plan-panel">
            <div class="panel-heading"><div><small>CURRENT PLAN</small><h2>当前周任务</h2></div><button class="ios-text-button" type="button" @click="chooseSection('learning')">管理计划</button></div>
            <div v-if="currentWeek" class="current-plan-card"><span class="ios-status active">{{ weekStatus(currentWeek) }}</span><h3>{{ currentWeek.title || '未命名周任务' }}</h3><p>{{ currentWeek.start }} — {{ currentWeek.end }}</p><div class="plan-tags"><span v-if="currentWeek.book_enabled !== false">读物</span><span v-if="currentWeek.video_enabled !== false">视频</span><span v-if="currentWeek.verse_enabled !== false">背经</span></div></div>
            <div v-else class="ios-empty"><AppIcon name="calendar" :size="28" /><b>还没有周任务</b><span>创建第一周计划后，成员即可开始打卡。</span><button :disabled="!canEditLearning" type="button" @click="chooseSection('learning'); createWeekDraft()">创建周任务</button></div>
          </article>
          <article class="ios-panel quick-actions-panel">
            <div class="panel-heading"><div><small>QUICK ACTIONS</small><h2>常用操作</h2></div></div>
            <button :disabled="!canEditLearning" type="button" @click="chooseSection('learning'); createWeekDraft()"><span><AppIcon name="plus" /></span><div><b>新建周任务</b><small>安排读物、视频与背经</small></div><AppIcon name="chevron" /></button>
            <button type="button" @click="chooseSection('library')"><span><AppIcon name="upload" /></span><div><b>上传学习资料</b><small>添加 PDF、图片或 Markdown</small></div><AppIcon name="chevron" /></button>
            <button type="button" @click="chooseSection('members')"><span><AppIcon name="users" /></span><div><b>查看成员权限</b><small>管理本组管理员</small></div><AppIcon name="chevron" /></button>
          </article>
        </div>
      </section>

      <section v-else-if="adminSection === 'learning'" class="ios-admin-page">
        <header class="ios-page-heading heading-with-action"><div><small>PLANNER</small><h1>任务计划</h1><p>先选择一周，再编辑该周需要完成的内容。</p></div><button :disabled="!canEditLearning || !!pendingAction" type="button" @click="createWeekDraft"><AppIcon name="plus" :size="18" />新建周任务</button></header>
        <div class="ios-planner-layout">
          <aside class="ios-week-list">
            <div class="list-caption">全部计划 · {{ weeks.length }}</div>
            <button v-for="week in weeks" :key="week.id" :class="{ active: Number(weekDraft?.id) === Number(week.id) }" type="button" @click="selectWeekForEditing(week.id)"><span class="ios-status" :class="{ active: weekStatus(week) === '进行中' }">{{ weekStatus(week) }}</span><b>{{ week.title || '未命名周任务' }}</b><small>{{ week.start }} — {{ week.end }}</small></button>
            <button v-if="!weeks.length" class="empty-week-button" :disabled="!canEditLearning" type="button" @click="createWeekDraft">＋ 创建第一周</button>
          </aside>

          <div class="ios-planner-content">
            <article v-if="weekDraft" class="ios-panel ios-week-editor">
              <div class="panel-heading"><div><small>{{ weekDraft.id ? 'EDIT WEEK' : 'NEW WEEK' }}</small><h2>{{ weekDraft.id ? '编辑周任务' : '新建周任务' }}</h2></div></div>
              <div class="ios-form-grid">
                <label class="span-2"><span>任务名称</span><input :disabled="!canEditLearning" :value="weekDraft.title || ''" placeholder="例如：马可福音（上）" @change="updateWeekDraftField('title', $event.target.value)" /></label>
                <label><span>开始日期</span><AppDatePicker :disabled="!canEditLearning" :model-value="weekDraft.start || ''" label="选择开始日期" @update:model-value="updateWeekDraftField('start', $event)" /></label>
                <label><span>结束日期</span><AppDatePicker :disabled="!canEditLearning" :model-value="weekDraft.end || ''" label="选择结束日期" @update:model-value="updateWeekDraftField('end', $event)" /></label>
              </div>
              <div v-if="validateWeekDates()" class="ios-form-error" role="alert">{{ validateWeekDates() }}</div>

              <div class="ios-editor-section"><div class="editor-section-heading"><div><span class="section-number">1</span><div><b>周读物</b><small>成员本周需要阅读的材料</small></div></div><button class="ios-text-button" :disabled="!canEditLearning" type="button" @click="addWeekBinding('readings')">＋ 添加</button></div><div class="ios-binding-row" v-for="(item,index) in weekDraft.readings || []" :key="`r-${index}`"><input :disabled="!canEditLearning" :value="item.title || ''" :aria-label="`第 ${index + 1} 项读物标题`" placeholder="读物标题" @change="updateWeekBinding('readings',index,'title',$event.target.value)" /><select :disabled="!canEditLearning" :value="librarySelectionValue(item)" :aria-label="`为第 ${index + 1} 项读物选择资源`" @change="applyBindingSelection('readings',index,$event.target.value)"><option value="">选择资源</option><option v-for="option in readingOptions" :key="librarySelectionValue(option)" :value="librarySelectionValue(option)">{{ optionText(option) }}</option></select><button class="icon-danger" :disabled="!canEditLearning" type="button" :aria-label="`删除第 ${index + 1} 项读物`" @click="removeWeekBinding('readings',index)">×</button></div></div>

              <div class="ios-editor-section"><div class="editor-section-heading"><div><span class="section-number">2</span><div><b>本周视频</b><small>支持多条 NAS Web 视频链接，成员可分别完成</small></div></div><button class="ios-text-button" :disabled="!canEditLearning" type="button" @click="addWeekBinding('videos')">＋ 添加</button></div><div class="ios-binding-row" v-for="(item,index) in (weekDraft.videos || [])" :key="`v-${index}`"><input :disabled="!canEditLearning" :value="item.title || ''" :aria-label="`第 ${index + 1} 条视频标题`" placeholder="视频标题" @change="updateWeekBinding('videos',index,'title',$event.target.value)" /><input :disabled="!canEditLearning" :value="item.url || ''" :aria-label="`第 ${index + 1} 条视频链接`" placeholder="https://…/video.mp4" @change="updateWeekBinding('videos',index,'url',$event.target.value)" /><button class="icon-danger" :disabled="!canEditLearning" type="button" :aria-label="`删除第 ${index + 1} 条视频`" @click="removeWeekBinding('videos',index)">×</button></div></div>

              <div class="ios-editor-section"><div class="editor-section-heading"><div><span class="section-number">3</span><div><b>背经与提纲</b><small>设置本周背诵范围和辅助资料</small></div></div></div><div class="ios-form-grid"><label><span>经文范围</span><input :disabled="!canEditLearning" :value="weekDraft.verse_ref || ''" placeholder="罗马书 8:1-5" @change="updateWeekDraftField('verse_ref',$event.target.value)" /></label><label><span>提纲图片</span><select :disabled="!canEditLearning" :value="librarySelectionValue(weekDraft.outline)" @change="applyOutlineSelection($event.target.value)"><option value="">暂不设置</option><option v-for="item in outlineOptions" :key="librarySelectionValue(item)" :value="librarySelectionValue(item)">{{ optionText(item) }}</option></select></label><label class="span-2"><span>背诵原文</span><textarea :disabled="!canEditLearning" rows="4" :value="weekDraft.recite_text || ''" @change="updateWeekDraftField('recite_text',$event.target.value)"></textarea></label></div></div>

              <details class="ios-disclosure"><summary>显示与高级选项 <span>按需展开</span></summary><div class="ios-toggle-grid"><label><input :disabled="!canEditLearning" type="checkbox" :checked="weekDraft.book_enabled !== false" @change="updateWeekDraftField('book_enabled',$event.target.checked)" />显示周读物</label><label><input :disabled="!canEditLearning" type="checkbox" :checked="weekDraft.video_enabled !== false" @change="updateWeekDraftField('video_enabled',$event.target.checked)" />显示视频</label><label><input :disabled="!canEditLearning" type="checkbox" :checked="weekDraft.verse_enabled !== false" @change="updateWeekDraftField('verse_enabled',$event.target.checked)" />显示背经</label><label><input :disabled="!canEditLearning" type="checkbox" :checked="weekDraft.outline_enabled !== false" @change="updateWeekDraftField('outline_enabled',$event.target.checked)" />显示提纲</label></div><button class="ios-text-button" :disabled="!canEditLearning" type="button" @click="restoreWeekDraftDefaults">恢复默认内容</button></details>
              <footer class="ios-editor-actions"><button v-if="weekDraft.id" class="ios-danger-button" :disabled="!canEditLearning || !!pendingAction" type="button" @click="deleteWeekWithPending">{{ isPending('delete-week') ? '删除中…' : '删除任务' }}</button><button :disabled="!canEditLearning || !!pendingAction || !!validateWeekDates()" type="button" @click="saveWeekWithValidation">{{ isPending('save-week') ? '保存中…' : '保存周任务' }}</button></footer>
            </article>

            <details class="ios-panel ios-daily-settings"><summary><div><small>DAILY SETTINGS</small><b>每日固定内容</b><span>灵修与读经规则通常只需设置一次</span></div><AppIcon name="chevron" /></summary><div class="ios-form-grid"><label><span>每日任务名称</span><input :disabled="!canEditLearning" :value="daily.label || ''" @change="updateLearning(['task_sections','daily','label'],$event.target.value)" /></label><label><span>灵修标题</span><input :disabled="!canEditLearning" :value="devotion.title || ''" @change="updateLearning(['task_sections','daily','devotion','title'],$event.target.value)" /></label><label><span>灵修起始日期</span><AppDatePicker :disabled="!canEditLearning" :model-value="devotion.numbered_start_date || ''" label="选择灵修起始日期" @update:model-value="updateLearning(['task_sections','daily','devotion','numbered_start_date'],$event)" /></label><label><span>灵修起始篇号</span><input :disabled="!canEditLearning" type="number" min="1" :value="devotion.numbered_start || 1" @change="updateLearning(['task_sections','daily','devotion','numbered_start'],Number($event.target.value || 1))" /></label><label><span>读经名称</span><input :disabled="!canEditLearning" :value="scripture.label || ''" @change="updateLearning(['task_sections','daily','scripture','label'],$event.target.value)" /></label><label><span>当前书卷</span><input :disabled="!canEditLearning" :value="scripture.book || ''" @change="updateLearning(['task_sections','daily','scripture','book'],$event.target.value)" /></label><label><span>读经起始日期</span><AppDatePicker :disabled="!canEditLearning" :model-value="scripture.start_date || ''" label="选择读经起始日期" @update:model-value="updateLearning(['task_sections','daily','scripture','start_date'],$event)" /></label><label><span>起始章</span><input :disabled="!canEditLearning" type="number" min="1" :value="scripture.start_chapter || 1" @change="updateLearning(['task_sections','daily','scripture','start_chapter'],Number($event.target.value || 1))" /></label></div><footer class="ios-editor-actions"><button :disabled="!canEditLearning || !!pendingAction" type="button" @click="withPending('save-daily', saveLearningConfig)">{{ isPending('save-daily') ? '保存中…' : '保存每日设置' }}</button></footer></details>
          </div>
        </div>
      </section>

      <section v-else-if="adminSection === 'members'" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>PEOPLE</small><h1>成员与权限</h1><p>成员通过报名名单注册；这里只管理本组权限。</p></div></header>
        <div class="ios-member-toolbar"><span>共 {{ members.length }} 位成员</span><span class="ios-info-chip">组长 {{ members.filter(m => m.roles?.includes('group_leader')).length }}</span><span class="ios-info-chip">管理员 {{ members.filter(m => m.roles?.includes('group_admin')).length }}</span></div>
        <div class="ios-member-list"><article v-for="member in members" :key="member.member_id" class="ios-member-row"><img v-if="member.avatar_url" :src="member.avatar_url" :alt="member.member_name" /><span v-else class="member-avatar-fallback">{{ (member.member_name || member.display_name || '?').slice(0,1) }}</span><div class="member-copy"><b>{{ member.member_name || member.display_name }}</b><small>@{{ member.username }}</small></div><span class="ios-role" :class="roleLabel(member)">{{ roleLabel(member) }}</span><div class="member-row-actions"><button v-if="!member.is_super_admin && !member.roles?.includes('group_leader')" class="ios-text-button" :disabled="!!pendingAction" type="button" @click="changeMemberAdmin(member)">{{ isPending(`member-role-${member.member_id}`) ? '处理中…' : (member.roles?.includes('group_admin') ? '取消管理员' : '设为管理员') }}</button><button v-if="member.user_id !== user?.id && !member.is_super_admin && !member.roles?.includes('group_leader')" class="ios-text-danger" :disabled="!!pendingAction" type="button" @click="removeMemberWithPending(member)">{{ isPending(`remove-member-${member.member_id}`) ? '移出中…' : '移出本组' }}</button></div></article></div>
      </section>

      <section v-else-if="adminSection === 'library'" class="ios-admin-page">
        <header class="ios-page-heading heading-with-action"><div><small>LIBRARY</small><h1>学习资源</h1><p>决定哪些资料展示给成员；周任务挂载是独立操作，不影响资料库可见性。</p></div><button class="ios-secondary-button" :disabled="!!pendingAction" type="button" @click="syncNASResources">{{ isPending('sync-nas') ? '同步中…' : '同步 NAS' }}</button></header>
        <article class="ios-upload-panel"><span class="upload-illustration"><AppIcon name="upload" :size="28" /></span><div><b>上传新资源</b><small>上传到网站资源库；不会写入下方选中的 NAS 文件夹</small></div><select v-model="uploadCategory" :disabled="!canEditLearning || !!pendingAction" aria-label="上传资源类型"><option value="book">PDF 读物</option><option value="markdown">Markdown</option><option value="handout">讲义</option><option value="outline">提纲图片</option></select><input ref="uploadInput" :disabled="!canEditLearning || !!pendingAction" aria-label="选择要上传的资源文件" type="file" /><button :disabled="!canEditLearning || !!pendingAction" type="button" @click="uploadResource">{{ isPending('upload-resource') ? '上传中…' : '上传' }}</button></article>
        <div class="ios-library-tabs"><button v-for="section in resourceLibrary" :key="section.key || section.label" :class="{ active: activeLibrarySection?.key === section.key }" :aria-pressed="activeLibrarySection?.key === section.key" type="button" @click="chooseLibrarySection(section.key)"><span>{{ section.label }}</span><small>{{ section.count || 0 }}</small></button></div>
        <section v-if="activeLibrarySection" class="ios-panel ios-file-browser">
          <div class="file-browser-toolbar"><div><small>当前分类</small><h2>{{ activeLibrarySection.label }}</h2><small>先设置成员资料库可见性；需要时再挂载到 {{ resourceTargetLabel }}</small></div><div v-if="activeLibrarySection.managed_root && user?.is_super_admin" class="folder-create"><input v-model="newFolderName" :disabled="!!pendingAction" aria-label="新建 NAS 文件夹名称" placeholder="新文件夹名称" @keydown.enter.prevent="createResourceFolder" /><button class="ios-secondary-button" :disabled="!!pendingAction" type="button" @click="createResourceFolder">{{ isPending('create-folder') ? '创建中…' : '新建文件夹' }}</button></div></div>
          <div class="ios-form-grid"><label class="span-2"><span>搜索当前分类</span><input v-model.trim="resourceSearch" type="search" placeholder="输入标题、文件名或文件夹" /></label></div>
          <div v-if="activeLibrarySection.managed_root" class="ios-folder-strip"><button :class="{ active: !activeLibraryFolder }" type="button" @click="activeLibraryFolder = ''">全部文件</button><button v-for="folder in activeLibrarySection.folders || []" :key="folder.path" :class="{ active: activeLibraryFolder === folder.path }" type="button" @click="activeLibraryFolder = folder.path"><AppIcon name="library" :size="15" />{{ folder.path }}</button></div>
          <div v-if="activeLibraryFolder" class="folder-current"><span>当前文件夹：<b>{{ activeLibraryFolder }}</b></span><button v-if="user?.is_super_admin" class="ios-text-button" :disabled="!!pendingAction" type="button" @click="renameResourceFolder">{{ isPending('rename-folder') ? '重命名中…' : '重命名' }}</button></div>
          <div v-if="visibleLibraryItems.length" class="ios-resource-list ios-scroll-resource-list"><div v-for="item in visibleLibraryItems" :key="item.resource_key || item.id || item.url" class="ios-resource-row"><button class="resource-preview" type="button" @click="previewLibraryItem(item)"><span><AppIcon name="file" /></span><div><b>{{ item.title || item.original_name }}</b><small><template v-if="item.folder">{{ item.folder }} / </template>{{ item.original_name || '点击预览' }}</small><em :class="{ visible: item.visible_in_library }">{{ item.visible_in_library ? '成员资料库已显示' : '成员资料库未显示' }}</em></div></button><div class="ios-resource-actions"><button :class="item.visible_in_library ? 'ios-secondary-button' : 'ios-resource-use'" :disabled="!canManageResources || !!pendingAction" type="button" @click="toggleResourceVisibility(item)">{{ isPending(`resource-visibility-${item.resource_key}`) ? '保存中…' : (item.visible_in_library ? '移出资料库' : '显示到资料库') }}</button><button class="ios-text-button" :disabled="!canEditLearning || !!pendingAction" type="button" :aria-label="`将 ${item.title || item.original_name} 挂载到 ${resourceTargetLabel}`" @click="selectResourceForTask(item)">挂载到周任务</button></div></div></div><div v-else class="ios-empty compact">{{ resourceSearch ? '没有匹配的资源，请更换关键词' : '当前文件夹暂无可用资源' }}</div>
        </section>
      </section>

      <section v-else-if="adminSection === 'roster' && user?.is_super_admin" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>ROSTER</small><h1>报名名单</h1><p>Excel 批量同步与临时补录集中管理。</p></div></header>
        <div class="ios-panel-grid"><article class="ios-panel"><div class="panel-heading"><div><h2>Excel 名单同步</h2><small>读取分表和红色组长标记</small></div></div><div class="ios-stack"><input ref="rosterInput" :disabled="!!pendingAction" aria-label="选择报名名单 Excel" type="file" accept=".xlsx" /><button class="ios-secondary-button" :disabled="!!pendingAction" type="button" @click="previewRoster">{{ isPending('preview-roster') ? '解析中…' : '先预览' }}</button><div v-if="rosterPreview?.row_count" class="roster-preview-result"><b>{{ rosterPreview.row_count }}</b><span>个席位</span><b>{{ rosterPreview.leader_count }}</b><span>位组长</span><button :disabled="!!pendingAction" type="button" @click="importRoster">{{ isPending('import-roster') ? '同步中…' : '确认同步' }}</button></div></div></article><article class="ios-panel"><div class="panel-heading"><div><h2>临时补录</h2><small>适合新增成员或辅修关系</small></div></div><div class="ios-stack"><input v-model="rosterName" :disabled="!!pendingAction" aria-label="标准姓名" placeholder="标准姓名" /><select v-model="rosterGroupID" :disabled="!!pendingAction" aria-label="选择门训组"><option value="">选择门训组</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select><div class="ios-toggle-grid"><label><input v-model="rosterLeader" :disabled="!!pendingAction" type="checkbox" />组长</label><label><input v-model="rosterMinor" :disabled="!!pendingAction" type="checkbox" />辅修</label></div><button :disabled="!rosterName || !rosterGroupID || !!pendingAction" type="button" @click="addRosterEntry">{{ isPending('add-roster') ? '添加中…' : '添加名单席位' }}</button></div></article></div>
        <article class="ios-panel"><div class="panel-heading"><div><h2>当前名单</h2><small>显示 {{ filteredRosterEntries.length }} / {{ rosterEntries.length }} 个席位</small></div><button class="ios-text-button" :disabled="!!pendingAction" type="button" @click="loadRoster">{{ isPending('load-roster') ? '刷新中…' : '刷新' }}</button></div><div class="ios-stack"><label><span>搜索名单</span><input v-model.trim="rosterSearch" type="search" placeholder="姓名、小组、主辅修或注册状态" /></label></div><div v-if="filteredRosterEntries.length" class="ios-roster-list"><div v-for="entry in filteredRosterEntries" :key="entry.id"><span class="member-avatar-fallback small">{{ entry.name.slice(0,1) }}</span><div><b>{{ entry.name }}</b><small>{{ entry.group_name }} · {{ entry.is_minor ? '辅修' : '主修' }}</small></div><span v-if="entry.is_leader" class="ios-role 组长">组长</span><span v-else-if="entry.claimed_by_user_id" class="ios-status active">已注册</span><span v-else class="ios-status">待注册</span></div></div><div v-else class="ios-empty compact">没有匹配的名单席位</div></article>
      </section>

      <section v-else-if="adminSection === 'data'" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>DATA</small><h1>数据工具</h1><p>导出报表和执行恢复操作。</p></div></header>
        <div class="ios-panel-grid"><article class="ios-panel"><div class="panel-heading"><div><h2>导出数据</h2><small>下载当前小组的数据文件</small></div></div><div class="ios-action-list"><button :disabled="!!pendingAction" type="button" @click="runExport('/admin/exports/checkins-detail','checkins-detail.csv','打卡明细已下载')"><AppIcon name="database" /><span><b>打卡明细</b><small>CSV 格式</small></span><AppIcon name="chevron" /></button><button :disabled="!!pendingAction" type="button" @click="runExport('/admin/exports/daily-summary','daily-summary.csv','每日汇总已下载')"><AppIcon name="chart" /><span><b>每日汇总</b><small>CSV 格式</small></span><AppIcon name="chevron" /></button><button :disabled="!!pendingAction" type="button" @click="runExport('/api/admin/exports/study-weeks','study-weeks.xlsx','任务计划已下载')"><AppIcon name="calendar" /><span><b>任务计划</b><small>Excel 格式</small></span><AppIcon name="chevron" /></button><button :disabled="!!pendingAction" type="button" @click="runExport('/admin/exports/local-backup','local-backup.json','备份已下载')"><AppIcon name="database" /><span><b>完整备份</b><small>JSON 格式</small></span><AppIcon name="chevron" /></button></div></article><article class="ios-panel"><div class="panel-heading"><div><h2>导入与恢复</h2><small>写入 {{ activeGroupName }}，请谨慎操作</small></div></div><div class="ios-stack"><label><span>导入任务计划 Excel</span><input ref="studyWeeksInput" :disabled="!canEditLearning || !!pendingAction" type="file" accept=".xlsx,.xlsm" /></label><button class="ios-secondary-button" :disabled="!canEditLearning || !!pendingAction" type="button" @click="runWeeksImport">{{ isPending('import-weeks') ? '导入中…' : '导入任务计划' }}</button><label><span>恢复新版完整备份 JSON</span><input ref="backupInput" :disabled="!canEditLearning || !!pendingAction" type="file" accept=".json" /></label><button class="ios-danger-button" :disabled="!canEditLearning || !!pendingAction" type="button" @click="runBackupImport">{{ isPending('import-backup') ? '恢复中…' : '恢复新版备份' }}</button></div></article></div>
        <article class="ios-panel ios-legacy-restore"><div class="panel-heading"><div><h2>从旧网站恢复</h2><small>直接读取旧版 config.json 和 records.json，恢复到 {{ activeGroupName }}</small></div></div><div class="ios-form-grid"><label><span>旧网站 config.json（必选）</span><input ref="legacyConfigInput" :disabled="!canEditLearning || !!pendingAction" type="file" accept=".json" @change="legacyPreview = null" /></label><label><span>旧网站打卡记录 records.json（可选）</span><input ref="legacyRecordsInput" :disabled="!canEditLearning || !!pendingAction" type="file" accept=".json" @change="legacyPreview = null" /></label></div><div class="legacy-actions"><button class="ios-secondary-button" :disabled="!canEditLearning || !!pendingAction" type="button" @click="previewLegacyRestore">{{ isPending('preview-legacy') ? '解析中…' : '解析并预览' }}</button><button v-if="legacyPreview" class="ios-danger-button" :disabled="!canEditLearning || !!pendingAction" type="button" @click="confirmLegacyRestore">{{ isPending('restore-legacy') ? '恢复中…' : '确认恢复到当前组' }}</button></div><div v-if="legacyPreview" class="legacy-preview"><div><b>{{ legacyPreview.weeks }}</b><span>周任务</span></div><div><b>{{ legacyPreview.sourceRecords }}</b><span>原始打卡记录</span></div><div><b>{{ legacyPreview.checkins }}</b><span>转换后打卡明细</span></div><p v-if="legacyPreview.unmatched.length"><strong>{{ legacyPreview.unmatched.length }} 个姓名未匹配，不会导入：</strong>{{ legacyPreview.unmatched.slice(0,12).join('、') }}<template v-if="legacyPreview.unmatched.length > 12"> 等</template></p><p v-else class="success-copy">所有打卡姓名均已匹配当前组成员。</p></div></article>
      </section>
    </main>
  </div>
</template>
