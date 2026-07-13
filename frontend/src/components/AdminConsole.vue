<script setup>
import { computed, onMounted, ref } from 'vue';
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

const settings = computed(() => learningConfig.value || {});
const daily = computed(() => settings.value.task_sections?.daily || {});
const devotion = computed(() => daily.value.devotion || {});
const scripture = computed(() => daily.value.scripture || {});
const libraryItems = computed(() => resourceLibrary.value.flatMap((section) => section.items || []));
const readingOptions = computed(() => libraryItems.value.filter((item) => ['markdown', 'pdf'].includes(item.type)));
const outlineOptions = computed(() => libraryItems.value.filter((item) => item.type === 'image'));
const currentWeek = computed(() => weeks.value.find((week) => weekStatus(week) === '进行中') || weeks.value[0]);
const completedProfiles = computed(() => members.value.filter((item) => item.username).length);

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
        <header class="ios-page-heading"><div><small>LIBRARY</small><h1>学习资源</h1><p>上传一次，即可在周任务中重复使用。</p></div></header>
        <article class="ios-upload-panel"><span class="upload-illustration"><AppIcon name="upload" :size="28" /></span><div><b>上传新资源</b><small>支持 PDF、Markdown、图片等学习资料</small></div><select v-model="uploadCategory"><option value="book">PDF 读物</option><option value="markdown">Markdown</option><option value="handout">讲义</option><option value="outline">提纲图片</option></select><input ref="uploadInput" type="file" /><button :disabled="!canEditLearning" type="button" @click="uploadResource">上传</button></article>
        <div class="ios-resource-sections"><section v-for="section in resourceLibrary" :key="section.key || section.label" class="ios-panel"><div class="panel-heading"><div><h2>{{ section.label }}</h2><small>{{ section.count || 0 }} 项</small></div></div><div v-if="section.items?.length" class="ios-resource-list"><button v-for="item in section.items" :key="item.id || item.url" type="button" @click="previewLibraryItem(item)"><span><AppIcon name="file" /></span><div><b>{{ item.title || item.original_name }}</b><small>{{ item.original_name || '点击预览' }}</small></div><AppIcon name="chevron" /></button></div><div v-else class="ios-empty compact">暂无资源</div></section></div>
      </section>

      <section v-else-if="adminSection === 'roster' && user?.is_super_admin" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>ROSTER</small><h1>报名名单</h1><p>Excel 批量同步与临时补录集中管理。</p></div></header>
        <div class="ios-panel-grid"><article class="ios-panel"><div class="panel-heading"><div><h2>Excel 名单同步</h2><small>读取分表和红色组长标记</small></div></div><div class="ios-stack"><input ref="rosterInput" type="file" accept=".xlsx" /><button class="ios-secondary-button" type="button" @click="previewRoster">先预览</button><div v-if="rosterPreview?.row_count" class="roster-preview-result"><b>{{ rosterPreview.row_count }}</b><span>个席位</span><b>{{ rosterPreview.leader_count }}</b><span>位组长</span><button type="button" @click="importRoster">确认同步</button></div></div></article><article class="ios-panel"><div class="panel-heading"><div><h2>临时补录</h2><small>适合新增成员或辅修关系</small></div></div><div class="ios-stack"><input v-model="rosterName" placeholder="标准姓名" /><select v-model="rosterGroupID"><option value="">选择门训组</option><option v-for="group in groups" :key="group.id" :value="group.id">{{ group.name }}</option></select><div class="ios-toggle-grid"><label><input v-model="rosterLeader" type="checkbox" />组长</label><label><input v-model="rosterMinor" type="checkbox" />辅修</label></div><button :disabled="!rosterName || !rosterGroupID" type="button" @click="addRosterEntry">添加名单席位</button></div></article></div>
        <article class="ios-panel"><div class="panel-heading"><div><h2>当前名单</h2><small>{{ rosterEntries.length }} 个席位</small></div><button class="ios-text-button" type="button" @click="loadRoster">刷新</button></div><div class="ios-roster-list"><div v-for="entry in rosterEntries.slice(0,120)" :key="entry.id"><span class="member-avatar-fallback small">{{ entry.name.slice(0,1) }}</span><div><b>{{ entry.name }}</b><small>{{ entry.group_name }} · {{ entry.is_minor ? '辅修' : '主修' }}</small></div><span v-if="entry.is_leader" class="ios-role 组长">组长</span><span v-else-if="entry.claimed_by_user_id" class="ios-status active">已注册</span><span v-else class="ios-status">待注册</span></div></div></article>
      </section>

      <section v-else-if="adminSection === 'data'" class="ios-admin-page">
        <header class="ios-page-heading"><div><small>DATA</small><h1>数据工具</h1><p>导出报表和执行恢复操作。</p></div></header>
        <div class="ios-panel-grid"><article class="ios-panel"><div class="panel-heading"><div><h2>导出数据</h2><small>下载当前小组的数据文件</small></div></div><div class="ios-action-list"><button type="button" @click="runExport('/admin/exports/checkins-detail','checkins-detail.csv','打卡明细已下载')"><AppIcon name="database" /><span><b>打卡明细</b><small>CSV 格式</small></span><AppIcon name="chevron" /></button><button type="button" @click="runExport('/admin/exports/daily-summary','daily-summary.csv','每日汇总已下载')"><AppIcon name="chart" /><span><b>每日汇总</b><small>CSV 格式</small></span><AppIcon name="chevron" /></button><button type="button" @click="runExport('/admin/exports/study-weeks','study-weeks.xlsx','任务计划已下载')"><AppIcon name="calendar" /><span><b>任务计划</b><small>Excel 格式</small></span><AppIcon name="chevron" /></button><button type="button" @click="runExport('/admin/exports/local-backup','local-backup.json','备份已下载')"><AppIcon name="database" /><span><b>完整备份</b><small>JSON 格式</small></span><AppIcon name="chevron" /></button></div></article><article class="ios-panel"><div class="panel-heading"><div><h2>导入与恢复</h2><small>写入当前小组，请谨慎操作</small></div></div><div class="ios-stack"><label><span>导入任务计划 Excel</span><input ref="studyWeeksInput" type="file" accept=".xlsx,.xlsm" /></label><button class="ios-secondary-button" :disabled="!canEditLearning" type="button" @click="runWeeksImport">导入任务计划</button><label><span>恢复本地备份 JSON</span><input ref="backupInput" type="file" accept=".json" /></label><button class="ios-danger-button" :disabled="!canEditLearning" type="button" @click="runBackupImport">恢复备份</button></div></article></div>
      </section>
    </main>
  </div>
</template>
