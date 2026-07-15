<script setup>
import { computed, ref } from 'vue';
import { storeToRefs } from 'pinia';
import { useCheckinWorkbenchStore } from '../stores/checkinWorkbench';
import { openTaskContent, setSelectedDate, shiftSelectedDate, toggleCheckin } from '../legacy-app';

const store = useCheckinWorkbenchStore();
const {
  visible,
  selectedDate,
  maxDate,
  selectedDateLabel,
  title,
  weekText,
  completed,
  total,
  isToday,
  isFuture,
  tasks,
  ownItems,
} = storeToRefs(store);

const progressPercent = computed(() => total.value ? Math.round((completed.value / total.value) * 100) : 0);
const allDone = computed(() => total.value > 0 && completed.value === total.value);
const pendingTaskKeys = ref(new Set());
const completionTitle = computed(() => isToday.value ? '今天的任务已全部完成' : `${selectedDateLabel.value}的任务已全部完成`);
const completionNote = computed(() => isToday.value ? '感谢你的坚持，明天继续同行。' : '这一天的学习记录已经补齐。');
const recordsTitle = computed(() => isToday.value ? '今日完成记录' : `${selectedDateLabel.value}完成记录`);
const emptyTitle = computed(() => isToday.value ? '今天还没有安排任务' : `${selectedDateLabel.value}没有安排任务`);
const emptyNote = computed(() => isToday.value ? '可以稍后再来，或联系组长确认今天的学习计划。' : '该日期无需补卡；如有疑问，请联系组长确认。');

function taskKey(task) {
  return `${task.type || ''}:${task.part || ''}:${task.detail || task.title || ''}`;
}

function taskPending(task) {
  return pendingTaskKeys.value.has(taskKey(task));
}

function taskLocked(task) {
  return Boolean(isFuture.value && !task.ownRecord);
}

function taskStatus(task) {
  if (taskPending(task)) return '处理中';
  if (task.ownRecord) return '已完成';
  if (taskLocked(task)) return '未开始';
  return isToday.value ? '待打卡' : '待补卡';
}

function actionText(task) {
  if (taskPending(task)) return task.ownRecord ? '正在撤销…' : '正在保存…';
  if (task.ownRecord) return '撤销完成';
  if (taskLocked(task)) return '未开始';
  return isToday.value ? '立即打卡' : '补卡';
}

function contentActionText(task) {
  if (task.type === 'weekly_video') return '观看视频';
  if (task.type === 'weekly_verse') return '查看经文';
  return '打开内容';
}

function taskTypeLabel(type) {
  return ({ daily_devotion: '每日灵修', weekly_book: '周读物', weekly_video: '周视频', weekly_verse: '背经' })[type] || '学习任务';
}

async function runToggle(task) {
  const key = taskKey(task);
  if (pendingTaskKeys.value.has(key)) return;
  if (task.ownRecord && !window.confirm(`确认撤销“${task.title}”的完成记录吗？`)) return;
  pendingTaskKeys.value = new Set([...pendingTaskKeys.value, key]);
  try {
    await toggleCheckin(task);
  } finally {
    const next = new Set(pendingTaskKeys.value);
    next.delete(key);
    pendingTaskKeys.value = next;
  }
}

function taskSubtitle(task) {
  return task.ownRecord ? `已完成 ${task.detail || task.title}` : (task.summary || '阅读内容后可直接完成打卡');
}
</script>

<template>
  <Teleport v-if="visible" to="#vue-checkin-workbench">
    <div class="grid checkin-workbench-page">
      <section class="today-hero">
        <div class="today-copy">
          <div class="eyebrow">{{ selectedDateLabel }}</div>
          <h2>{{ title }}</h2>
          <p>{{ weekText }}</p>
        </div>
        <div class="date-controls workbench-date-controls" aria-label="选择打卡日期">
          <button class="secondary" type="button" aria-label="查看前一天" @click="shiftSelectedDate(-1)">‹</button>
          <input
            type="date"
            :value="selectedDate"
            :max="maxDate"
            aria-label="打卡日期"
            @change="setSelectedDate($event.target.value)"
          />
          <button class="secondary" type="button" :disabled="isToday" aria-label="查看后一天" @click="shiftSelectedDate(1)">›</button>
          <button v-if="!isToday" class="ghost" type="button" @click="setSelectedDate(maxDate)">回到今天</button>
        </div>
        <div class="today-score">
          <strong>{{ total ? `${completed}/${total}` : '—' }}</strong>
          <span>我的完成</span>
        </div>
        <div
          class="personal-progress"
          role="progressbar"
          aria-valuemin="0"
          aria-valuemax="100"
          :aria-valuenow="progressPercent"
          :aria-label="`我的任务完成进度 ${progressPercent}%`"
        >
          <span :style="{ width: `${progressPercent}%` }"></span>
        </div>
      </section>

      <div v-if="allDone" class="completion-banner" role="status">
        <span class="completion-icon" aria-hidden="true">✓</span>
        <div><b>{{ completionTitle }}</b><p>{{ completionNote }}</p></div>
      </div>

      <div v-if="tasks.length" class="task-board">
        <article
          v-for="task in tasks"
          :key="`${task.type}:${task.part || ''}:${task.title}`"
          class="task-option"
          :class="{ done: task.ownRecord, pending: taskPending(task) }"
          :aria-busy="taskPending(task)"
        >
          <div class="task-head">
            <span class="task-icon">{{ task.ownRecord ? '✓' : task.icon }}</span>
            <span class="task-state-badge" :class="{ done: task.ownRecord }">{{ taskStatus(task) }}</span>
          </div>

          <div
            class="task-copy"
          >
            <span class="task-title">{{ task.title }}</span>
            <span class="task-subtitle">{{ taskSubtitle(task) }}</span>
          </div>

          <div v-if="task.type === 'daily_devotion' && task.contentLinks?.length > 1" class="task-link-list">
            <button
              v-for="link in task.contentLinks"
              :key="`${link.label}:${link.url}`"
              class="task-link-pill"
              type="button"
              :title="link.title || link.label"
              @click="openTaskContent(task, link)"
            >
              {{ link.label }}
            </button>
          </div>

          <div class="task-actions">
            <button
              v-if="task.contentLinks?.length"
              class="secondary content-action"
              type="button"
              @click="openTaskContent(task)"
            >
              {{ contentActionText(task) }} →
            </button>
            <button
              :class="task.ownRecord ? 'ghost' : 'ok'"
              type="button"
              :disabled="taskLocked(task) || taskPending(task)"
              :aria-label="`${actionText(task)}：${task.title}`"
              @click="runToggle(task)"
            >
              {{ actionText(task) }}
            </button>
          </div>
        </article>
      </div>
      <div v-else class="empty task-empty"><b>{{ emptyTitle }}</b><span>{{ emptyNote }}</span></div>

      <section>
        <div class="section-title">
          <h2>{{ recordsTitle }}</h2>
        </div>
        <div v-if="!ownItems.length" class="empty">该日期暂无完成记录</div>
        <div v-else class="my-checkin-list">
          <div v-for="item in ownItems" :key="item.id" class="my-checkin-item">
            <span class="checkin-ok">✓</span>
            <div><b>{{ item.detail || item.part || taskTypeLabel(item.task_type) }}</b><small>{{ taskTypeLabel(item.task_type) }} · {{ item.logical_date }}</small></div>
            <span class="pill">已完成</span>
          </div>
        </div>
      </section>
    </div>
  </Teleport>
</template>

<style scoped>
.task-option.pending {
  opacity: 0.78;
}

@media (max-width: 620px) {
  .workbench-date-controls {
    display: grid;
    grid-template-columns: 44px minmax(0, 1fr) 44px;
    width: 100%;
  }

  .workbench-date-controls input {
    min-width: 0;
  }

  .workbench-date-controls .ghost {
    grid-column: 1 / -1;
    width: 100%;
  }
}
</style>
