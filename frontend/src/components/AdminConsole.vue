<script setup>
import { computed, onMounted, ref, watch } from 'vue';
import { storeToRefs } from 'pinia';
import AppIcon from './AppIcon.vue';
import { useAppStateStore } from '../stores/appState';
import {
  addWeekBinding, api, applyBindingSelection, applyOutlineSelection, createWeekDraft,
  deleteWeekDraft, downloadAdminExport, importLocalBackupJSON, importStudyWeeksExcel,
  librarySelectionValue, loadAdminData, previewLibraryItem, removeMember, removeWeekBinding,
  restoreWeekDraftDefaults, saveLearningConfig, saveWeekDraft, selectWeekForEditing,
  setAdminSection, setMemberAdmin, toast, updateLearningValue, updateWeekBinding,
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

const settings = computed(() => learningConfig.value || {});
const daily = computed(() => settings.value.task_sections?.daily || {});
const devotion = computed(() => daily.value.devotion || {});
const scripture = computed(() => daily.value.scripture || {});
const libraryItems = computed(() => resourceLibrary.value.flatMap((section) => section.items || []));
const readingOptions = computed(() => libraryItems.value.filter((item) => ['markdown', 'pdf'].includes(item.type)));
const outlineOptions = computed(() => libraryItems.value.filter((item) => item.type === 'image'));
const activeLibrarySection = computed(() => resourceLibrary.value.find((section) => section.key === activeLibraryKey.value) || resourceLibrary.value[0] || null);
const visibleLibraryItems = computed(() => {
  const items = activeLibrarySection.value?.items || [];
  if (!activeLibraryFolder.value) return items;
  return items.filter((item) => item.folder === activeLibraryFolder.value);
});
const currentWeek = computed(() => weeks.value.find((week) => weekStatus(week) === '进行中') || weeks.value[0]);
const completedProfiles = computed(() => members.value.filter((item) => item.username).length);

watch(resourceLibrary, (sections) => {
  if (!sections?.some((section) => section.key === activeLibraryKey.value)) activeLibraryKey.value = sections?.[0]?.key || '';
}, { immediate: true });
watch(activeLibrarySection, (section) => {
  if (activeLibraryFolder.value && !(section?.folders || []).some((folder) => folder.path === activeLibraryFolder.value)) activeLibraryFolder.value = '';
});

const sections = computed(() => [
  { id: 'overview', label: '管理概览', icon: 'chart' },
  { id: 'learning', label: '任务计划', icon: 'calendar' },
  { id: 'members', label: '成员权限', icon: 'users' },
  { id: 'library', label: '学习资源', icon: 'library' },
  ...(user.value?.is_super_admin ? [{ id: 'roster', label: '报名名单', icon: 'file' }] : []),
  { id: 'data', label: '数据工具', icon: 'database' },
]);

function weekStatus(week) {
  const today = new Date().toISOString().slice(0, 10);
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

async function chooseSection(id) {
  setAdminSection(id);
  if (id === 'overview') await loadAdminData();
  if (id === 'roster') await loadRoster();
}

async function loadRoster() {
  try { const data = await api('/super-admin/roster'); rosterEntries.value = data.entries || []; }
  catch (error) { toast(error.message); }
}

async function uploadResource() { await uploadLibraryFile(uploadInput.value, uploadCategory.value); }

function chooseLibrarySection(key) { activeLibraryKey.value = key; activeLibraryFolder.value = ''; }
async function syncNASResources() {
  try { await loadAdminData(true); toast('NAS 资源目录已重新扫描'); }
  catch (error) { toast(`同步失败：${error.message}`); }
}
async function createResourceFolder() {
  const section = activeLibrarySection.value;
  if (!section?.managed_root) return toast('请选择 Book、Passage、PPT 或 Mentor 分类');
  if (!newFolderName.value.trim()) return toast('请输入文件夹名称');
  try {
    const data = await api('/admin/resource-folders', { method: 'POST', body: JSON.stringify({ root: section.managed_root, parent: activeLibraryFolder.value, name: newFolderName.value }) });
    newFolderName.value = ''; await loadAdminData(true); activeLibraryFolder.value = data.path || '';
    toast('文件夹已创建，可以在 NAS 中向该目录上传文件');
  } catch (error) { toast(`创建失败：${error.message}`); }
}
async function renameResourceFolder() {
  const section = activeLibrarySection.value;
  if (!section?.managed_root || !activeLibraryFolder.value) return;
  const currentName = activeLibraryFolder.value.split('/').pop();
  const name = window.prompt('新的文件夹名称', currentName);
  if (!name || name === currentName) return;
  try {
    const data = await api('/admin/resource-folders', { method: 'PUT', body: JSON.stringify({ root: section.managed_root, path: activeLibraryFolder.value, name }) });
    await loadAdminData(true); activeLibraryFolder.value = data.path || '';
    toast('文件夹已重命名');
  } catch (error) { toast(`重命名失败：${error.message}`); }
}

async function previewRoster() {
  const file = rosterInput.value?.files?.[0]; if (!file) return toast('请先选择 Excel 文件');
  const body = new FormData(); body.append('file', file);
  const res = await fetch('/api/super-admin/roster/preview', { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('agp_token') || ''}` }, body });
  rosterPreview.value = await res.json(); if (!res.ok) toast(rosterPreview.value.error || '名单解析失败');
}

async function importRoster() {
  const file = rosterInput.value?.files?.[0]; if (!file || !rosterPreview.value?.row_count) return;
  const body = new FormData(); body.append('file', file);
  const res = await fetch('/api/super-admin/roster/import', { method: 'POST', headers: { Authorization: `Bearer ${localStorage.getItem('agp_token') || ''}` }, body });
  const data = await res.json(); if (!res.ok) return toast(data.error || '名单同步失败');
  rosterPreview.value = null; toast(`已同步 ${data.imported} 条名单`); await loadRoster();
}

async function addRosterEntry() {
  try {
    await api('/super-admin/roster/entries', { method: 'POST', body: JSON.stringify({ name: rosterName.value, group_id: Number(rosterGroupID.value), is_leader: rosterLeader.value, is_minor: rosterMinor.value, status: 1 }) });
    rosterName.value = ''; rosterGroupID.value = ''; rosterLeader.value = false; rosterMinor.value = false;
    toast('名单席位已添加'); await loadRoster();
  } catch (error) { toast(error.message); }
}

async function runExport(path, name, message) { try { await downloadAdminExport(path, name, message); } catch (error) { toast(error.message); } }
async function runWeeksImport() { try { await importStudyWeeksExcel(studyWeeksInput.value); } catch (error) { toast(error.message); } }
async function runBackupImport() { try { await importLocalBackupJSON(backupInput.value); } catch (error) { toast(error.message); } }

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
}

async function confirmLegacyRestore() {
  if (!legacyPreview.value?.payload) return;
  const message = `将用旧网站数据替换当前组的任务设置和打卡记录。\n\n任务：${legacyPreview.value.weeks} 周\n打卡明细：${legacyPreview.value.checkins} 条\n\n确认继续吗？`;
  if (!window.confirm(message)) return;
  try {
    await api('/admin/imports/local-backup', { method: 'POST', body: JSON.stringify(legacyPreview.value.payload) });
    legacyPreview.value = null;
    if (legacyConfigInput.value) legacyConfigInput.value.value = '';
    if (legacyRecordsInput.value) legacyRecordsInput.value.value = '';
    await loadAdminData(true); toast('旧网站任务设置和打卡记录已恢复');
  } catch (error) { toast(`恢复失败：${error.message}`); }
}

function selectResourceForTask(item) {
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
  loadAdminData();
});
</script>

<template>
  <div class="ios-admin-layout">
    <aside class="ios-admin-nav">
      <div class="ios-admin-nav-title"><small>ADMIN CONSOLE</small><b>小组管理</b></div>
      <button v-for="section in sections" :key="section.id" :class="{ active: adminSection === section.id }" type="button" @click="chooseSection(section.id)">
        <AppIcon :name="section.icon" :size="19" /><span>{{ section.label }}</span><AppIcon class="admin-nav-chevron" name="chevron" :size="16" />
      </button>
    </aside>

    <main class="ios-admin-main">
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
            <div v-else class="ios-empty"><AppIcon name="calendar" :size="28" /><b>还没有周任务</b><span>创建第一周计划后，成员即可开始打卡。</span><button type="button" @click="chooseSection('learning'); createWeekDraft()">创建周任务</button></div>
          </article>
          <article class="ios-panel quick-actions-panel">
            <div class="panel-heading"><div><small>QUICK ACTIONS</small><h2>常用操作</h2></div></div>
            <button type="button" @click="chooseSection('learning'); createWeekDraft()"><span><AppIcon name="plus" /></span><div><b>新建周任务</b><small>安排读物、视频与背经</small></div><AppIcon name="chevron" /></button>
            <button type="button" @click="chooseSection('library')"><span><AppIcon name="upload" /></span><div><b>上传学习资料</b><small>添加 PDF、图片或 Markdown</small></div><AppIcon name="chevron" /></button>
            <button type="button" @click="chooseSection('members')"><span><AppIcon name="users" /></span><div><b>查看成员权限</b><small>管理本组管理员</small></div><AppIcon name="chevron" /></button>
          </article>
        </div>
      </section>

      <section v-else-if="adminSection === 'learning'" class="ios-admin-page">
        <header class="ios-page-heading heading-with-action"><div><small>PLANNER</small><h1>任务计划</h1><p>先选择一周，再编辑该周需要完成的内容。</p></div><button type="button" @click="createWeekDraft"><AppIcon name="plus" :size="18" />新建周任务</button></header>
        <div class="ios-planner-layout">
          <aside class="ios-week-list">
            <div class="list-caption">全部计划 · {{ weeks.length }}</div>
            <button v-for="week in weeks" :key="week.id" :class="{ active: Number(weekDraft?.id) === Number(week.id) }" type="button" @click="selectWeekForEditing(week.id)"><span class="ios-status" :class="{ active: weekStatus(week) === '进行中' }">{{ weekStatus(week) }}</span><b>{{ week.title || '未命名周任务' }}</b><small>{{ week.start }} — {{ week.end }}</small></button>
            <button v-if="!weeks.length" class="empty-week-button" type="button" @click="createWeekDraft">＋ 创建第一周</button>
          </aside>

          <div class="ios-planner-content">
            <article v-if="weekDraft" class="ios-panel ios-week-editor">
              <div class="panel-heading"><div><small>{{ weekDraft.id ? 'EDIT WEEK' : 'NEW WEEK' }}</small><h2>{{ weekDraft.id ? '编辑周任务' : '新建周任务' }}</h2></div></div>
              <div class="ios-form-grid">
                <label class="span-2"><span>任务名称</span><input :value="weekDraft.title || ''" placeholder="例如：马可福音（上）" @change="updateWeekDraftField('title', $event.target.value)" /></label>
                <label><span>开始日期</span><input type="date" :value="weekDraft.start || ''" @change="updateWeekDraftField('start', $event.target.value)" /></label>
                <label><span>结束日期</span><input type="date" :value="weekDraft.end || ''" @change="updateWeekDraftField('end', $event.target.value)" /></label>
              </div>

              <div class="ios-editor-section"><div class="editor-section-heading"><div><span class="section-number">1</span><div><b>周读物</b><small>成员本周需要阅读的材料</small></div></div><button class="ios-text-button" type="button" @click="addWeekBinding('readings')">＋ 添加</button></div><div class="ios-binding-row" v-for="(item,index) in weekDraft.readings || []" :key="`r-${index}`"><input :value="item.title || ''" placeholder="读物标题" @change="updateWeekBinding('readings',index,'title',$event.target.value)" /><select :value="librarySelectionValue(item)" @change="applyBindingSelection('readings',index,$event.target.value)"><option value="">选择资源</option><option v-for="option in readingOptions" :key="librarySelectionValue(option)" :value="librarySelectionValue(option)">{{ optionText(option) }}</option></select><button class="icon-danger" type="button" @click="removeWeekBinding('readings',index)">×</button></div></div>

              <div class="ios-editor-section"><div class="editor-section-heading"><div><span class="section-number">2</span><div><b>本周视频</b><small>支持 NAS Web 视频链接</small></div></div><button class="ios-text-button" type="button" @click="addWeekBinding('videos')">＋ 添加</button></div><div class="ios-binding-row" v-for="(item,index) in weekDraft.videos || []" :key="`v-${index}`"><input :value="item.title || ''" placeholder="视频标题" @change="updateWeekBinding('videos',index,'title',$event.target.value)" /><input :value="item.url || ''" placeholder="https://…/video.mp4" @change="updateWeekBinding('videos',index,'url',$event.target.value)" /><button class="icon-danger" type="button" @click="removeWeekBinding('videos',index)">×</button></div></div>

              <div class="ios-editor-section"><div class="editor-section-heading"><div><span class="section-number">3</span><div><b>背经与提纲</b><small>设置本周背诵范围和辅助资料</small></div></div></div><div class="ios-form-grid"><label><span>经文范围</span><input :value="weekDraft.verse_ref || ''" placeholder="罗马书 8:1-5" @change="updateWeekDraftField('verse_ref',$event.target.value)" /></label><label><span>提纲图片</span><select :value="librarySelectionValue(weekDraft.outline)" @change="applyOutlineSelection($event.target.value)"><option value="">暂不设置</option><option v-for="item in outlineOptions" :key="librarySelectionValue(item)" :value="librarySelectionValue(item)">{{ optionText(item) }}</option></select></label><label class="span-2"><span>背诵原文</span><textarea rows="4" :value="weekDraft.recite_text || ''" @change="updateWeekDraftField('recite_text',$event.target.value)"></textarea></label></div></div>

              <details class="ios-disclosure"><summary>显示与高级选项 <span>按需展开</span></summary><div class="ios-toggle-grid"><label><input type="checkbox" :checked="weekDraft.book_enabled !== false" @change="updateWeekDraftField('book_enabled',$event.target.checked)" />显示周读物</label><label><input type="checkbox" :checked="weekDraft.video_enabled !== false" @change="updateWeekDraftField('video_enabled',$event.target.checked)" />显示视频</label><label><input type="checkbox" :checked="weekDraft.verse_enabled !== false" @change="updateWeekDraftField('verse_enabled',$event.target.checked)" />显示背经</label><label><input type="checkbox" :checked="weekDraft.outline_enabled !== false" @change="updateWeekDraftField('outline_enabled',$event.target.checked)" />显示提纲</label></div><button class="ios-text-button" type="button" @click="restoreWeekDraftDefaults">恢复默认内容</button></details>
              <footer class="ios-editor-actions"><button v-if="weekDraft.id" class="ios-danger-button" :disabled="!canEditLearning" type="button" @click="deleteWeekDraft">删除任务</button><button :disabled="!canEditLearning" type="button" @click="saveWeekDraft">保存周任务</button></footer>
            </article>

            <details class="ios-panel ios-daily-settings"><summary><div><small>DAILY SETTINGS</small><b>每日固定内容</b><span>灵修与读经规则通常只需设置一次</span></div><AppIcon name="chevron" /></summary><div class="ios-form-grid"><label><span>每日任务名称</span><input :value="daily.label || ''" @change="updateLearning(['task_sections','daily','label'],$event.target.value)" /></label><label><span>灵修标题</span><input :value="devotion.title || ''" @change="updateLearning(['task_sections','daily','devotion','title'],$event.target.value)" /></label><label><span>灵修起始日期</span><input type="date" :value="devotion.numbered_start_date || ''" @change="updateLearning(['task_sections','daily','devotion','numbered_start_date'],$event.target.value)" /></label><label><span>灵修起始篇号</span><input type="number" min="1" :value="devotion.numbered_start || 1" @change="updateLearning(['task_sections','daily','devotion','numbered_start'],Number($event.target.value || 1))" /></label><label><span>读经名称</span><input :value="scripture.label || ''" @change="updateLearning(['task_sections','daily','scripture','label'],$event.target.value)" /></label><label><span>当前书卷</span><input :value="scripture.book || ''" @change="updateLearning(['task_sections','daily','scripture','book'],$event.target.value)" /></label><label><span>读经起始日期</span><input type="date" :value="scripture.start_date || ''" @change="updateLearning(['task_sections','daily','scripture','start_date'],$event.target.value)" /></label><label><span>起始章</span><input type="number" min="1" :value="scripture.start_chapter || 1" @change="updateLearning(['task_sections','daily','scripture','start_chapter'],Number($event.target.value || 1))" /></label></div><footer class="ios-editor-actions"><button :disabled="!canEditLearning" type="button" @click="saveLearningConfig">保存每日设置</button></footer></details>
          </div>
        </div>
      </section>

      <section v-else-if="adminSection === 'members'" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>PEOPLE</small><h1>成员与权限</h1><p>成员通过报名名单注册；这里只管理本组权限。</p></div></header>
        <div class="ios-member-toolbar"><span>共 {{ members.length }} 位成员</span><span class="ios-info-chip">组长 {{ members.filter(m => m.roles?.includes('group_leader')).length }}</span><span class="ios-info-chip">管理员 {{ members.filter(m => m.roles?.includes('group_admin')).length }}</span></div>
        <div class="ios-member-list"><article v-for="member in members" :key="member.member_id" class="ios-member-row"><img v-if="member.avatar_url" :src="member.avatar_url" :alt="member.member_name" /><span v-else class="member-avatar-fallback">{{ (member.member_name || member.display_name || '?').slice(0,1) }}</span><div class="member-copy"><b>{{ member.member_name || member.display_name }}</b><small>@{{ member.username }}</small></div><span class="ios-role" :class="roleLabel(member)">{{ roleLabel(member) }}</span><div class="member-row-actions"><button v-if="!member.is_super_admin && !member.roles?.includes('group_leader')" class="ios-text-button" type="button" @click="setMemberAdmin(member,!member.roles?.includes('group_admin'))">{{ member.roles?.includes('group_admin') ? '取消管理员' : '设为管理员' }}</button><button v-if="member.user_id !== user?.id && !member.is_super_admin && !member.roles?.includes('group_leader')" class="ios-text-danger" type="button" @click="removeMember(member)">移出本组</button></div></article></div>
      </section>

      <section v-else-if="adminSection === 'library'" class="ios-admin-page">
        <header class="ios-page-heading heading-with-action"><div><small>LIBRARY</small><h1>学习资源</h1><p>从 NAS 同步目录，在网页中预览并选入周任务。</p></div><button class="ios-secondary-button" type="button" @click="syncNASResources">同步 NAS</button></header>
        <article class="ios-upload-panel"><span class="upload-illustration"><AppIcon name="upload" :size="28" /></span><div><b>上传新资源</b><small>支持 PDF、Markdown、图片等学习资料</small></div><select v-model="uploadCategory"><option value="book">PDF 读物</option><option value="markdown">Markdown</option><option value="handout">讲义</option><option value="outline">提纲图片</option></select><input ref="uploadInput" type="file" /><button :disabled="!canEditLearning" type="button" @click="uploadResource">上传</button></article>
        <div class="ios-library-tabs"><button v-for="section in resourceLibrary" :key="section.key || section.label" :class="{ active: activeLibrarySection?.key === section.key }" type="button" @click="chooseLibrarySection(section.key)"><span>{{ section.label }}</span><small>{{ section.count || 0 }}</small></button></div>
        <section v-if="activeLibrarySection" class="ios-panel ios-file-browser">
          <div class="file-browser-toolbar"><div><small>当前分类</small><h2>{{ activeLibrarySection.label }}</h2></div><div v-if="activeLibrarySection.managed_root" class="folder-create"><input v-model="newFolderName" :disabled="!canEditLearning" placeholder="新文件夹名称" @keydown.enter="createResourceFolder" /><button class="ios-secondary-button" :disabled="!canEditLearning" type="button" @click="createResourceFolder">新建文件夹</button></div></div>
          <div v-if="activeLibrarySection.managed_root" class="ios-folder-strip"><button :class="{ active: !activeLibraryFolder }" type="button" @click="activeLibraryFolder = ''">全部文件</button><button v-for="folder in activeLibrarySection.folders || []" :key="folder.path" :class="{ active: activeLibraryFolder === folder.path }" type="button" @click="activeLibraryFolder = folder.path"><AppIcon name="library" :size="15" />{{ folder.path }}</button></div>
          <div v-if="activeLibraryFolder" class="folder-current"><span>当前文件夹：<b>{{ activeLibraryFolder }}</b></span><button class="ios-text-button" :disabled="!canEditLearning" type="button" @click="renameResourceFolder">重命名</button></div>
          <div v-if="visibleLibraryItems.length" class="ios-resource-list ios-scroll-resource-list"><div v-for="item in visibleLibraryItems" :key="item.id || item.url" class="ios-resource-row"><button class="resource-preview" type="button" @click="previewLibraryItem(item)"><span><AppIcon name="file" /></span><div><b>{{ item.title || item.original_name }}</b><small><template v-if="item.folder">{{ item.folder }} / </template>{{ item.original_name || '点击预览' }}</small></div></button><button class="ios-resource-use" type="button" @click="selectResourceForTask(item)">选入任务</button></div></div><div v-else class="ios-empty compact">当前文件夹暂无可用资源</div>
        </section>
      </section>

      <section v-else-if="adminSection === 'roster' && user?.is_super_admin" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>ROSTER</small><h1>报名名单</h1><p>Excel 批量同步与临时补录集中管理。</p></div></header>
        <div class="ios-panel-grid"><article class="ios-panel"><div class="panel-heading"><div><h2>Excel 名单同步</h2><small>读取分表和红色组长标记</small></div></div><div class="ios-stack"><input ref="rosterInput" type="file" accept=".xlsx" /><button class="ios-secondary-button" type="button" @click="previewRoster">先预览</button><div v-if="rosterPreview?.row_count" class="roster-preview-result"><b>{{ rosterPreview.row_count }}</b><span>个席位</span><b>{{ rosterPreview.leader_count }}</b><span>位组长</span><button type="button" @click="importRoster">确认同步</button></div></div></article><article class="ios-panel"><div class="panel-heading"><div><h2>临时补录</h2><small>适合新增成员或辅修关系</small></div></div><div class="ios-stack"><input v-model="rosterName" placeholder="标准姓名" /><select v-model="rosterGroupID"><option value="">选择门训组</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select><div class="ios-toggle-grid"><label><input v-model="rosterLeader" type="checkbox" />组长</label><label><input v-model="rosterMinor" type="checkbox" />辅修</label></div><button :disabled="!rosterName || !rosterGroupID" type="button" @click="addRosterEntry">添加名单席位</button></div></article></div>
        <article class="ios-panel"><div class="panel-heading"><div><h2>当前名单</h2><small>{{ rosterEntries.length }} 个席位</small></div><button class="ios-text-button" type="button" @click="loadRoster">刷新</button></div><div class="ios-roster-list"><div v-for="entry in rosterEntries.slice(0,120)" :key="entry.id"><span class="member-avatar-fallback small">{{ entry.name.slice(0,1) }}</span><div><b>{{ entry.name }}</b><small>{{ entry.group_name }} · {{ entry.is_minor ? '辅修' : '主修' }}</small></div><span v-if="entry.is_leader" class="ios-role 组长">组长</span><span v-else-if="entry.claimed_by_user_id" class="ios-status active">已注册</span><span v-else class="ios-status">待注册</span></div></div></article>
      </section>

      <section v-else-if="adminSection === 'data'" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>DATA</small><h1>数据工具</h1><p>导出报表和执行恢复操作。</p></div></header>
        <div class="ios-panel-grid"><article class="ios-panel"><div class="panel-heading"><div><h2>导出数据</h2><small>下载当前小组的数据文件</small></div></div><div class="ios-action-list"><button type="button" @click="runExport('/admin/exports/checkins-detail','checkins-detail.csv','打卡明细已下载')"><AppIcon name="database" /><span><b>打卡明细</b><small>CSV 格式</small></span><AppIcon name="chevron" /></button><button type="button" @click="runExport('/admin/exports/daily-summary','daily-summary.csv','每日汇总已下载')"><AppIcon name="chart" /><span><b>每日汇总</b><small>CSV 格式</small></span><AppIcon name="chevron" /></button><button type="button" @click="runExport('/api/admin/exports/study-weeks','study-weeks.xlsx','任务计划已下载')"><AppIcon name="calendar" /><span><b>任务计划</b><small>Excel 格式</small></span><AppIcon name="chevron" /></button><button type="button" @click="runExport('/admin/exports/local-backup','local-backup.json','备份已下载')"><AppIcon name="database" /><span><b>完整备份</b><small>JSON 格式</small></span><AppIcon name="chevron" /></button></div></article><article class="ios-panel"><div class="panel-heading"><div><h2>导入与恢复</h2><small>写入当前小组，请谨慎操作</small></div></div><div class="ios-stack"><label><span>导入任务计划 Excel</span><input ref="studyWeeksInput" type="file" accept=".xlsx,.xlsm" /></label><button class="ios-secondary-button" :disabled="!canEditLearning" type="button" @click="runWeeksImport">导入任务计划</button><label><span>恢复新版完整备份 JSON</span><input ref="backupInput" type="file" accept=".json" /></label><button class="ios-danger-button" :disabled="!canEditLearning" type="button" @click="runBackupImport">恢复新版备份</button></div></article></div>
        <article class="ios-panel ios-legacy-restore"><div class="panel-heading"><div><h2>从旧网站恢复</h2><small>直接读取旧版 config.json 和 records.json，恢复到当前小组</small></div></div><div class="ios-form-grid"><label><span>旧网站 config.json（必选）</span><input ref="legacyConfigInput" type="file" accept=".json" @change="legacyPreview = null" /></label><label><span>旧网站打卡记录 records.json（可选）</span><input ref="legacyRecordsInput" type="file" accept=".json" @change="legacyPreview = null" /></label></div><div class="legacy-actions"><button class="ios-secondary-button" :disabled="!canEditLearning" type="button" @click="previewLegacyRestore">解析并预览</button><button v-if="legacyPreview" class="ios-danger-button" :disabled="!canEditLearning" type="button" @click="confirmLegacyRestore">确认恢复到当前组</button></div><div v-if="legacyPreview" class="legacy-preview"><div><b>{{ legacyPreview.weeks }}</b><span>周任务</span></div><div><b>{{ legacyPreview.sourceRecords }}</b><span>原始打卡记录</span></div><div><b>{{ legacyPreview.checkins }}</b><span>转换后打卡明细</span></div><p v-if="legacyPreview.unmatched.length"><strong>{{ legacyPreview.unmatched.length }} 个姓名未匹配，不会导入：</strong>{{ legacyPreview.unmatched.slice(0,12).join('、') }}<template v-if="legacyPreview.unmatched.length > 12"> 等</template></p><p v-else class="success-copy">所有打卡姓名均已匹配当前组成员。</p></div></article>
      </section>
    </main>
  </div>
</template>
