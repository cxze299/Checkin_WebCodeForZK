<script setup>
import { computed } from 'vue';
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

function taskLocked(task) {
  return Boolean(isFuture.value && !task.ownRecord);
}

function taskStatus(task) {
  if (task.ownRecord) return '已完成';
  if (taskLocked(task)) return '未开始';
  return isToday.value ? '待打卡' : '待补卡';
}

function actionText(task) {
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
  if (task.ownRecord && !window.confirm(`确认撤销“${task.title}”的完成记录吗？`)) return;
  await toggleCheckin(task);
}

function taskSubtitle(task) {
  return task.ownRecord ? `已完成 ${task.detail || task.title}` : (task.summary || '阅读内容后可直接完成打卡');
}
</script>

<template>
  <Teleport v-if="visible" to="#vue-checkin-workbench">
    <div class="grid">
      <section class="today-hero">
        <div class="today-copy">
          <div class="eyebrow">{{ selectedDateLabel }}</div>
          <h2>{{ title }}</h2>
          <p>{{ weekText }}</p>
        </div>
        <div class="date-controls">
          <button class="secondary" type="button" @click="shiftSelectedDate(-1)">‹</button>
          <input
            type="date"
            :value="selectedDate"
            :max="maxDate"
            @change="setSelectedDate($event.target.value)"
          />
          <button class="secondary" type="button" :disabled="isToday" @click="shiftSelectedDate(1)">›</button>
          <button v-if="!isToday" class="ghost" type="button" @click="setSelectedDate(maxDate)">回到今天</button>
        </div>
        <div class="today-score">
          <strong>{{ completed }}/{{ total }}</strong>
          <span>我的完成</span>
        </div>
        <div class="personal-progress" :aria-label="`完成进度 ${progressPercent}%`">
          <span :style="{ width: `${progressPercent}%` }"></span>
        </div>
      </section>

      <div v-if="allDone" class="completion-banner">
        <span class="completion-icon">✓</span>
        <div><b>今天的任务已全部完成</b><p>感谢你的坚持，明天继续同行。</p></div>
      </div>

      <div v-if="tasks.length" class="task-board">
        <article
          v-for="task in tasks"
          :key="`${task.type}:${task.part || ''}:${task.title}`"
          class="task-option"
          :class="{ done: task.ownRecord }"
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
              :disabled="taskLocked(task)"
              @click="runToggle(task)"
            >
              {{ actionText(task) }}
            </button>
          </div>
        </article>
      </div>
      <div v-else class="empty task-empty"><b>这一天还没有安排任务</b><span>可以先休息，或联系组长确认学习计划。</span></div>

      <section>
        <div class="section-title">
          <h2>当天完成记录</h2>
        </div>
        <div v-if="!ownItems.length" class="empty">暂无记录</div>
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
