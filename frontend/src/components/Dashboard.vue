<script setup>
import { computed, ref } from 'vue';
import { storeToRefs } from 'pinia';
import { useDashboardStore } from '../stores/dashboard';
import AppDatePicker from './AppDatePicker.vue';
import { openMemberCalendar, setSelectedDate, shiftSelectedDate, toast, toggleCheckin } from '../legacy-app';
import { confirmDialog } from '../ui/dialog';

const store = useDashboardStore();
const {
  visible,
  selectedDate,
  maxDate,
  isToday,
  groupName,
  weekText,
  overallPercent,
  doneSlots,
  totalSlots,
  memberCount,
  completed,
  taskCount,
  progressCards,
  members,
  monthLabel,
  ranking,
  leaderName,
  leaderNote,
  rankingFrom,
  rankingTo,
  activeCount,
} = storeToRefs(store);

const legend = [
  { key: 'daily_devotion', label: '灵修' },
  { key: 'weekly_book', label: '书籍' },
  { key: 'weekly_video', label: '视频' },
  { key: 'weekly_verse', label: '背经' },
];

const rankingMax = computed(() => Math.max(1, ...ranking.value.map((item) => item.total || 0)));
const hasTasks = computed(() => taskCount.value > 0 && progressCards.value.length > 0);
const selectedDayLabel = computed(() => {
  if (isToday.value) return '今日';
  const [, month, day] = String(selectedDate.value || '').split('-');
  return month && day ? `${Number(month)}月${Number(day)}日` : '所选日期';
});
const pendingTaskKeys = ref(new Set());
const exporting = ref(false);

function taskKey(member, item) {
  const task = item.taskForMember || item.task || {};
  return `${member.user_id}:${task.type || ''}:${task.part || ''}:${task.detail || item.title || ''}`;
}

function taskPending(member, item) {
  return pendingTaskKeys.value.has(taskKey(member, item));
}

function segmentHeight(count, total) {
  if (!count || !total) return 0;
  return Math.max(8, Math.round((count / total) * 100));
}

function stackHeight(total) {
  return Math.max(4, Math.round(((total || 0) / rankingMax.value) * 100));
}

function memberTaskTitle(member, state) {
  if (!member.isSelf) return `${member.name}的${state.title}：${state.done ? '已完成' : '未完成'}`;
  if (taskPending(member, state)) return `${state.title}：正在处理`;
  return `${state.title}：${state.done ? '点击撤销完成' : '点击打卡'}`;
}

async function runMemberToggle(member, item) {
  if (!member.isSelf) return;
  const key = taskKey(member, item);
  if (pendingTaskKeys.value.has(key)) return;
  if (item.done && !await confirmDialog({ title: '撤销完成记录', message: `确认撤销“${item.title}”的完成记录吗？`, confirmLabel: '确认撤销', tone: 'danger' })) return;
  pendingTaskKeys.value = new Set([...pendingTaskKeys.value, key]);
  try {
    await toggleCheckin(item.taskForMember, member);
  } finally {
    const next = new Set(pendingTaskKeys.value);
    next.delete(key);
    pendingTaskKeys.value = next;
  }
}

function escapeXML(value) {
  return String(value || '')
    .replaceAll('&', '&amp;')
    .replaceAll('<', '&lt;')
    .replaceAll('>', '&gt;')
    .replaceAll('"', '&quot;')
    .replaceAll("'", '&apos;');
}

async function exportRankingChart() {
  if (exporting.value) return;
  exporting.value = true;
  let svgUrl = '';
  try {
  const width = 1120;
  const height = 720;
  const left = 80;
  const right = 40;
  const top = 120;
  const bottom = 120;
  const chartWidth = width - left - right;
  const chartHeight = height - top - bottom;
  const items = ranking.value.slice(0, 16);
  const colors = {
    daily_devotion: '#0a84ff',
    weekly_book: '#8b5cf6',
    weekly_video: '#19bf7a',
    weekly_verse: '#f59e0b',
  };
  const slotWidth = chartWidth / Math.max(1, items.length);
  const barWidth = Math.max(26, Math.min(42, slotWidth * 0.48));
  const maxTotal = Math.max(1, ...items.map((item) => item.total || 0));
  const legendSvg = legend.map((item, index) => `
    <g transform="translate(${left + index * 170}, 54)">
      <rect width="14" height="14" rx="4" fill="${colors[item.key]}" />
      <text x="24" y="12" font-size="16" fill="#3b4452">${item.label}</text>
    </g>
  `).join('');
  const barSvg = items.map((item, index) => {
    const x = left + slotWidth * index + (slotWidth - barWidth) / 2;
    let offset = 0;
    const segments = legend.map((part) => {
      const count = Number(item.counts?.[part.key] || 0);
      if (!count) return '';
      const segmentHeightPx = Math.max(0, (count / maxTotal) * chartHeight);
      offset += segmentHeightPx;
      return `
        <rect x="${x}" y="${top + chartHeight - offset}" width="${barWidth}" height="${segmentHeightPx}" rx="8" fill="${colors[part.key]}" />
      `;
    }).join('');
    const label = escapeXML(String(item.member_name || item.display_name || '?').slice(0, 4));
    return `
      <g>
        <rect x="${x}" y="${top}" width="${barWidth}" height="${chartHeight}" rx="12" fill="rgba(15,23,42,0.05)" />
        ${segments}
        <text x="${x + barWidth / 2}" y="${top + chartHeight + 28}" text-anchor="middle" font-size="16" fill="#1f2937">${label}</text>
        <text x="${x + barWidth / 2}" y="${top + chartHeight + 52}" text-anchor="middle" font-size="13" fill="#6b7280">${item.total || 0} 次</text>
      </g>
    `;
  }).join('');
  const gridSvg = Array.from({ length: 5 }, (_, index) => {
    const value = Math.round((maxTotal / 4) * (4 - index));
    const y = top + (chartHeight / 4) * index;
    return `
      <g>
        <line x1="${left}" y1="${y}" x2="${width - right}" y2="${y}" stroke="rgba(15,23,42,0.08)" stroke-dasharray="6 6" />
        <text x="${left - 14}" y="${y + 5}" text-anchor="end" font-size="14" fill="#6b7280">${value}</text>
      </g>
    `;
  }).join('');
  const svg = `
    <svg xmlns="http://www.w3.org/2000/svg" width="${width}" height="${height}" viewBox="0 0 ${width} ${height}">
      <rect width="100%" height="100%" rx="32" fill="#ffffff"/>
      <text x="${left}" y="40" font-size="28" font-weight="700" fill="#111827">${escapeXML(groupName.value || '当前小组')}数据统计中心</text>
      <text x="${left}" y="80" font-size="18" fill="#6b7280">${escapeXML(monthLabel.value)} 分项总榜</text>
      ${legendSvg}
      ${gridSvg}
      ${barSvg}
    </svg>
  `;
  const svgBlob = new Blob([svg], { type: 'image/svg+xml;charset=utf-8' });
  svgUrl = URL.createObjectURL(svgBlob);
  const image = new Image();
  image.decoding = 'async';
  image.src = svgUrl;
  await new Promise((resolve, reject) => {
    image.onload = resolve;
    image.onerror = reject;
  });
  const canvas = document.createElement('canvas');
  canvas.width = width * 2;
  canvas.height = height * 2;
  const ctx = canvas.getContext('2d');
  ctx.scale(2, 2);
  ctx.fillStyle = '#ffffff';
  ctx.fillRect(0, 0, width, height);
  ctx.drawImage(image, 0, 0, width, height);
  URL.revokeObjectURL(svgUrl);
  svgUrl = '';
  const pngBlob = await new Promise((resolve) => canvas.toBlob(resolve, 'image/png'));
  if (!pngBlob) return;
  const url = URL.createObjectURL(pngBlob);
  const link = document.createElement('a');
  link.href = url;
  link.download = `${monthLabel.value}-bar-chart.png`;
  document.body.append(link);
  link.click();
  link.remove();
  setTimeout(() => URL.revokeObjectURL(url), 1000);
  } catch (error) {
    toast(`导出失败：${error.message || '无法生成图片'}`);
  } finally {
    if (svgUrl) URL.revokeObjectURL(svgUrl);
    exporting.value = false;
  }
}
</script>

<template>
  <Teleport v-if="visible" to="#vue-dashboard">
    <div class="grid dashboard-page">
      <section class="today-hero dashboard-hero">
        <div class="today-copy">
          <div class="eyebrow">{{ groupName }}</div>
          <h2>小组打卡情况与统计</h2>
          <p>{{ weekText }}</p>
        </div>
        <div class="date-controls dashboard-date-controls" aria-label="选择统计日期">
          <button class="secondary" type="button" aria-label="查看前一天" @click="shiftSelectedDate(-1)">‹</button>
          <AppDatePicker
            :model-value="selectedDate"
            :max="maxDate"
            label="选择统计日期"
            @update:model-value="setSelectedDate"
          />
          <button class="secondary" type="button" :disabled="isToday" aria-label="查看后一天" @click="shiftSelectedDate(1)">›</button>
          <button v-if="!isToday" class="ghost" type="button" @click="setSelectedDate(maxDate)">回到今天</button>
        </div>
      </section>

      <section class="dashboard-overview" aria-labelledby="dashboard-overview-title">
        <div class="overview-primary">
          <span class="overview-kicker">{{ selectedDayLabel }}概览</span>
          <div class="overview-rate">
            <strong>{{ hasTasks ? `${overallPercent}%` : '—' }}</strong>
            <div><h2 id="dashboard-overview-title">小组完成率</h2><p>{{ hasTasks ? `已完成 ${doneSlots} / ${totalSlots} 项任务` : '所选日期没有安排任务' }}</p></div>
          </div>
          <div class="overview-progress" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="hasTasks ? overallPercent : 0">
            <span :style="{ width: `${hasTasks ? overallPercent : 0}%` }"></span>
          </div>
        </div>
        <div class="overview-metrics">
          <article><span>小组成员</span><strong>{{ memberCount }}</strong><small>当前小组</small></article>
          <article><span>完成项目</span><strong>{{ doneSlots }}</strong><small>{{ selectedDayLabel }}累计</small></article>
          <article><span>我的进度</span><strong>{{ completed }}/{{ taskCount }}</strong><small>{{ !taskCount ? '暂无任务' : (completed === taskCount ? '已经完成' : '继续坚持') }}</small></article>
        </div>
      </section>

      <section class="daily-progress-section">
        <div class="section-title dashboard-section-title">
          <div><span>DAILY PROGRESS</span><h2>{{ selectedDayLabel }}打卡进度</h2></div>
          <p>点击成员头像可查看个人月历</p>
        </div>
        <div v-if="hasTasks" class="dashboard-daily-layout">
          <aside class="task-progress-panel">
            <div class="subsection-heading"><b>任务完成情况</b><small>按任务查看全组进度</small></div>
            <div class="task-progress-row">
            <div v-for="card in progressCards" :key="`${card.task.type}:${card.task.part || ''}:${card.title}`" class="task-progress-card">
              <div class="task-progress-head">
                <span>{{ card.icon }}</span>
                <b>{{ card.title }}</b>
              </div>
              <div class="progress-track" role="progressbar" aria-valuemin="0" aria-valuemax="100" :aria-valuenow="card.percent" :aria-label="`${card.title}小组完成率 ${card.percent}%`">
                <span :style="{ width: `${card.percent}%` }"></span>
              </div>
              <small>{{ card.count }}/{{ card.total }}</small>
            </div>
          </div>
          </aside>
          <div class="member-progress-panel">
            <div class="subsection-heading"><b>成员状态</b><small>{{ memberCount }} 位成员</small></div>
          <div class="member-checkin-grid">
            <div v-for="member in members" :key="member.user_id" class="member-check-card">
              <div class="member-main">
                <button class="avatar avatar-button" type="button" :aria-label="`查看${member.name}的打卡月历`" @click="openMemberCalendar(member)">
                  <img v-if="member.avatar_url" :src="member.avatar_url" :alt="member.name" />
                  <span v-else>{{ member.avatar }}</span>
                </button>
                <div>
                  <b>{{ member.name }}{{ member.isSelf ? '（我）' : '' }}</b>
                  <div class="muted">{{ member.username }}</div>
                </div>
              </div>
              <div class="member-task-chips">
                <button
                  v-for="item in member.taskStates"
                  :key="`${member.user_id}:${item.task.type}:${item.task.part || ''}:${item.title}`"
                  class="member-task-chip"
                  :class="{ done: item.done, clickable: member.isSelf, pending: taskPending(member, item) }"
                  :title="memberTaskTitle(member, item)"
                  :aria-label="memberTaskTitle(member, item)"
                  :aria-pressed="item.done"
                  :aria-busy="taskPending(member, item)"
                  :disabled="!member.isSelf || taskPending(member, item)"
                  type="button"
                  @click="runMemberToggle(member, item)"
                >
                  <span class="member-task-code">{{ item.shortLabel || item.icon }}</span>
                </button>
              </div>
            </div>
          </div>
          </div>
        </div>
        <div v-else class="empty dashboard-empty"><b>{{ selectedDayLabel }}没有安排任务</b><span>该日期无需统计或补卡；如有疑问，请联系组长确认。</span></div>
      </section>

      <section class="stats-center">
        <div class="growth-heading">
          <div>
            <span>MONTHLY GROWTH</span>
            <h2>本月成长</h2>
            <p>{{ monthLabel }} · 按灵修、书籍、视频和背经累计完成次数</p>
          </div>
          <button class="ios-secondary-button" type="button" :disabled="exporting || !ranking.length" @click="exportRankingChart">{{ exporting ? '正在生成…' : '导出图表' }}</button>
        </div>

        <div class="growth-summary">
          <article><span>本月领先</span><strong>{{ leaderName }}</strong><small>{{ leaderNote }}</small></article>
          <article><span>活跃成员</span><strong>{{ activeCount }} 人</strong><small>本月至少完成 1 次</small></article>
          <article><span>统计周期</span><strong>{{ monthLabel }}</strong><small>{{ rankingFrom }} 至 {{ rankingTo }}</small></article>
        </div>

        <div v-if="ranking.length" class="bar-chart-card">
          <div class="bar-chart-meta">
            <div><strong>成员完成构成</strong><small>柱形高度代表本月累计完成次数</small></div>
            <div class="bar-legend">
              <span v-for="item in legend" :key="item.key" class="legend-item" :class="`legend-${item.key}`">
                <i></i>
                <span>{{ item.label }}</span>
              </span>
            </div>
          </div>
          <div class="bar-chart" role="img" :aria-label="`${groupName || '当前小组'}${monthLabel}打卡分项总榜`">
            <div v-for="member in ranking" :key="member.user_id || member.member_name" class="bar-item">
              <div class="bar-track">
                <div v-if="member.total" class="bar-stack" :style="{ height: `${stackHeight(member.total)}%` }">
                  <span
                    v-if="member.counts?.daily_devotion"
                    class="bar-segment devotion"
                    :style="{ height: `${segmentHeight(member.counts.daily_devotion, member.total)}%` }"
                    :title="`灵修 ${member.counts.daily_devotion} 次`"
                  ></span>
                  <span
                    v-if="member.counts?.weekly_book"
                    class="bar-segment book"
                    :style="{ height: `${segmentHeight(member.counts.weekly_book, member.total)}%` }"
                    :title="`书籍 ${member.counts.weekly_book} 次`"
                  ></span>
                  <span
                    v-if="member.counts?.weekly_video"
                    class="bar-segment video"
                    :style="{ height: `${segmentHeight(member.counts.weekly_video, member.total)}%` }"
                    :title="`视频 ${member.counts.weekly_video} 次`"
                  ></span>
                  <span
                    v-if="member.counts?.weekly_verse"
                    class="bar-segment verse"
                    :style="{ height: `${segmentHeight(member.counts.weekly_verse, member.total)}%` }"
                    :title="`背经 ${member.counts.weekly_verse} 次`"
                  ></span>
                </div>
                <span v-else class="bar-empty"></span>
              </div>
              <span class="bar-label">{{ (member.member_name || member.display_name || '?').slice(0, 4) }}</span>
              <small>{{ member.total || 0 }} 次</small>
            </div>
          </div>
        </div>
        <div v-else class="empty dashboard-empty"><b>{{ monthLabel }}暂无排行数据</b><span>成员完成打卡后，月度分项统计会显示在这里。</span></div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.dashboard-overview {
  display: grid;
  grid-template-columns: minmax(280px, 1.05fr) minmax(0, 1.95fr);
  overflow: hidden;
  border: 1px solid rgba(255,255,255,.85);
  border-radius: 30px;
  background: rgba(255,255,255,.76);
  box-shadow: var(--shadow-card);
}

.overview-primary {
  display: grid;
  align-content: center;
  gap: 15px;
  padding: 26px;
  background: linear-gradient(145deg, rgba(10,132,255,.13), rgba(139,92,246,.08));
}

.overview-kicker,
.dashboard-section-title span,
.growth-heading span {
  color: var(--primary);
  font-size: 11px;
  font-weight: 850;
  letter-spacing: .12em;
}

.overview-rate {
  display: flex;
  align-items: center;
  gap: 18px;
}

.overview-rate > strong {
  min-width: 108px;
  font-size: clamp(44px, 5vw, 68px);
  line-height: .9;
  letter-spacing: -.075em;
}

.overview-rate h2,
.overview-rate p {
  margin: 0;
}

.overview-rate h2 {
  font-size: 18px;
}

.overview-rate p {
  margin-top: 5px;
  color: var(--muted);
  font-size: 13px;
}

.overview-progress {
  height: 9px;
  overflow: hidden;
  border-radius: 999px;
  background: rgba(255,255,255,.72);
}

.overview-progress span {
  display: block;
  height: 100%;
  border-radius: inherit;
  background: linear-gradient(90deg, #0a84ff, #8b5cf6);
}

.overview-metrics {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  align-items: center;
  padding: 18px;
}

.overview-metrics article,
.growth-summary article {
  min-width: 0;
  display: grid;
  gap: 6px;
  padding: 18px 22px;
  border-right: 1px solid var(--line);
}

.overview-metrics article:last-child,
.growth-summary article:last-child {
  border-right: 0;
}

.overview-metrics span,
.growth-summary span {
  color: var(--muted);
  font-size: 12px;
  font-weight: 760;
}

.overview-metrics strong,
.growth-summary strong {
  overflow: hidden;
  font-size: 27px;
  letter-spacing: -.05em;
  text-overflow: ellipsis;
  white-space: nowrap;
}

.overview-metrics small,
.growth-summary small {
  overflow: hidden;
  color: var(--muted);
  text-overflow: ellipsis;
  white-space: nowrap;
}

.dashboard-section-title {
  align-items: end;
}

.dashboard-section-title > div {
  display: grid;
  gap: 5px;
}

.dashboard-section-title p {
  margin: 0;
  color: var(--muted);
  font-size: 13px;
}

.dashboard-daily-layout {
  display: grid;
  grid-template-columns: minmax(250px, .72fr) minmax(0, 1.8fr);
  gap: 16px;
  align-items: start;
}

.task-progress-panel,
.member-progress-panel {
  display: grid;
  gap: 14px;
  padding: 20px;
  border: 1px solid rgba(255,255,255,.82);
  border-radius: 28px;
  background: rgba(255,255,255,.68);
  box-shadow: var(--shadow-card);
}

.subsection-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 12px;
}

.subsection-heading b {
  font-size: 16px;
}

.subsection-heading small {
  color: var(--muted);
}

.task-progress-row {
  grid-template-columns: 1fr;
}

.member-checkin-grid {
  grid-template-columns: repeat(2, minmax(0, 1fr));
}

.growth-heading {
  display: flex;
  align-items: end;
  justify-content: space-between;
  gap: 18px;
  margin-top: 30px;
}

.growth-heading > div {
  display: grid;
  gap: 5px;
}

.growth-heading h2,
.growth-heading p {
  margin: 0;
}

.growth-heading h2 {
  font-size: 26px;
  letter-spacing: -.05em;
}

.growth-heading p {
  color: var(--muted);
}

.growth-summary {
  display: grid;
  grid-template-columns: repeat(3, minmax(0, 1fr));
  padding: 8px;
  border: 1px solid rgba(255,255,255,.82);
  border-radius: 24px;
  background: rgba(255,255,255,.62);
}

.bar-chart-meta > div:first-child {
  display: grid;
  gap: 3px;
}

.bar-chart-meta small {
  color: var(--muted);
}

.member-task-chip.pending {
  cursor: wait;
}

.member-task-chip:disabled:not(.pending) {
  opacity: 1;
}

.dashboard-empty {
  min-height: 160px;
}

@media (max-width: 620px) {
  .dashboard-overview,
  .dashboard-daily-layout {
    grid-template-columns: 1fr;
  }

  .overview-primary {
    padding: 22px;
  }

  .overview-metrics,
  .growth-summary {
    grid-template-columns: 1fr;
  }

  .overview-metrics article,
  .growth-summary article {
    grid-template-columns: 1fr auto;
    align-items: center;
    padding: 14px;
    border-right: 0;
    border-bottom: 1px solid var(--line);
  }

  .overview-metrics article:last-child,
  .growth-summary article:last-child {
    border-bottom: 0;
  }

  .overview-metrics strong,
  .growth-summary strong {
    grid-column: 2;
    grid-row: 1 / span 2;
    font-size: 22px;
  }

  .member-checkin-grid {
    grid-template-columns: 1fr;
  }

  .dashboard-section-title,
  .growth-heading {
    align-items: flex-start;
    flex-direction: column;
  }

  .growth-heading button {
    width: 100%;
  }

  .dashboard-date-controls {
    display: grid;
    grid-template-columns: 44px minmax(0, 1fr) 44px;
    width: 100%;
  }

  .dashboard-date-controls :deep(.app-date-field) {
    min-width: 0;
  }

  .dashboard-date-controls .ghost {
    grid-column: 1 / -1;
    width: 100%;
  }
}
</style>
