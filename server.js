const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const express = require('express');
const nodemailer = require('nodemailer');

const app = express();
const port = Number(process.env.PORT || 3000);
const host = process.env.HOST || '0.0.0.0';
const dataPath = path.resolve(process.env.DATABASE_PATH || path.join(__dirname, 'data', 'app.json'));
const recordsPath = path.resolve(process.env.RECORDS_PATH || path.join(path.dirname(dataPath), 'records.json'));
const weeklySchedulePath = path.resolve(process.env.WEEKLY_SCHEDULE_PATH || path.join(path.dirname(dataPath), 'weekly_schedule.json'));
const bundledWeeklySchedulePath = path.join(__dirname, 'weekly_schedule.json');
const recordsBackupEnabled = String(process.env.RECORDS_BACKUP_ENABLED || 'true').toLowerCase() !== 'false';
const recordsBackupDir = path.resolve(process.env.RECORDS_BACKUP_DIR || path.join(path.dirname(recordsPath), 'record-backups'));
const recordsBackupLimit = Math.max(1, Number(process.env.RECORDS_BACKUP_LIMIT || 120));
const sessionSecret = process.env.SESSION_SECRET || 'dev-secret-change-me';
const absenceAlertEnabled = String(process.env.ABSENCE_ALERT_ENABLED || '').toLowerCase() === 'true';
const absenceAlertTo = process.env.ABSENCE_ALERT_TO || '';
const absenceAlertDays = Math.max(1, Number(process.env.ABSENCE_ALERT_DAYS || 3));
const absenceAlertHour = Math.min(23, Math.max(0, Number(process.env.ABSENCE_ALERT_HOUR || 21)));
const absenceAlertTimezone = process.env.ABSENCE_ALERT_TIMEZONE || 'Asia/Shanghai';

const defaultMembers = ["顺丞", "信择", "明明", "亮亮", "杰涛", "嘉杰", "银华", "青青", "依萱", "喜悦", "胡蜜", "馨香", "虹伊", "刘欣", "义路"];
const defaultWeeklySchedule = [
  { start: "2026-04-06", end: "2026-04-12", title: "《基督是一切》马太福音：基督是我们的王", video: "新约圣经-01-211209-圣经引言(上)", verse: "罗马书 7:1-5", url: "http://nas.restinhim.online:5777/Newtestament/L1.mp4" },
  { start: "2026-04-13", end: "2026-04-19", title: "《基督是一切》马太福音：基督是我们的王", video: "新约圣经-02-190307-圣经引言(下)", verse: "罗马书 7:6-10", url: "http://nas.restinhim.online:5777/Newtestament/L2.mp4" },
  { start: "2026-04-20", end: "2026-04-26", title: "《基督是一切》马太福音：基督是我们的王", video: "新约圣经-03-220120-马太福音(上)", verse: "罗马书 7:11-15", url: "http://nas.restinhim.online:5777/Newtestament/L3.mp4" },
  { start: "2026-04-27", end: "2026-05-03", title: "《基督是一切》马太福音：基督是我们的王", video: "新约圣经-04-220120-马太福音(下)", verse: "罗马书 7:16-20", url: "http://nas.restinhim.online:5777/Newtestament/L4.mp4" },
  { start: "2026-05-04", end: "2026-05-10", title: "《基督是一切》基督是神的仆人--马可福音", video: "新约圣经-07-220714-马可福音(上)", verse: "罗马书 7:21-25", url: "http://nas.restinhim.online:5777/Newtestament/L5.mp4" },
  { start: "2026-05-11", end: "2026-05-17", title: "《基督是一切》基督是神的仆人--马可福音", video: "新约圣经-08-220714-马可福音(下)", verse: "罗马书 8:1-5", url: "http://nas.restinhim.online:5777/Newtestament/L6.mp4" },
  { start: "2026-05-18", end: "2026-05-24", title: "《基督是一切》基督是人子--路加福音", video: "新约圣经-09-220818-路加福音(上)", verse: "罗马书 8:6-10", url: "http://nas.restinhim.online:5777/Newtestament/L7.mp4" },
  { start: "2026-05-25", end: "2026-05-31", title: "《基督是一切》基督是人子--路加福音", video: "新约圣经-10-220818-路加福音(下)", verse: "罗马书 8:11-15", url: "http://nas.restinhim.online:5777/Newtestament/L8.mp4" },
  { start: "2026-06-01", end: "2026-06-07", title: "《基督是一切》基督是神的儿子--约翰福音", video: "新约圣经-11-220901-约翰福音(上)", verse: "罗马书 8:16-20", url: "http://nas.restinhim.online:5777/Newtestament/L9.mp4" },
  { start: "2026-06-08", end: "2026-06-14", title: "《基督是一切》基督是神的儿子--约翰福音", video: "新约圣经-12-220901-约翰福音(下)", verse: "罗马书 8:21-25", url: "http://nas.restinhim.online:5777/Newtestament/L10.mp4" }
];

fs.mkdirSync(path.dirname(dataPath), { recursive: true });
fs.mkdirSync(path.dirname(recordsPath), { recursive: true });
fs.mkdirSync(path.dirname(weeklySchedulePath), { recursive: true });
if (recordsBackupEnabled) fs.mkdirSync(recordsBackupDir, { recursive: true });

function readJsonFile(filePath, fallback) {
  try {
    if (fs.existsSync(filePath)) return JSON.parse(fs.readFileSync(filePath, 'utf8'));
  } catch (_) {}
  return fallback;
}

function safeTimestamp(date = new Date()) {
  return date.toISOString().replace(/[:.]/g, '-');
}

function atomicWriteJson(filePath, value) {
  const tmpPath = `${filePath}.tmp-${process.pid}-${Date.now()}`;
  fs.writeFileSync(tmpPath, JSON.stringify(value, null, 2));
  fs.renameSync(tmpPath, filePath);
}

function rotateRecordBackups() {
  if (!recordsBackupEnabled || !fs.existsSync(recordsBackupDir)) return;
  const backups = fs.readdirSync(recordsBackupDir)
    .filter(name => /^records-.*\.json$/.test(name))
    .map(name => {
      const filePath = path.join(recordsBackupDir, name);
      return { name, filePath, mtime: fs.statSync(filePath).mtimeMs };
    })
    .sort((a, b) => b.mtime - a.mtime);

  for (const backup of backups.slice(recordsBackupLimit)) {
    fs.unlinkSync(backup.filePath);
  }
}

function backupRecordsFile(reason = 'write') {
  if (!recordsBackupEnabled || !fs.existsSync(recordsPath)) return null;
  fs.mkdirSync(recordsBackupDir, { recursive: true });
  const backupPath = path.join(recordsBackupDir, `records-${safeTimestamp()}-${reason}.json`);
  fs.copyFileSync(recordsPath, backupPath);
  rotateRecordBackups();
  return backupPath;
}

function listRecordBackups(limit = 10) {
  if (!recordsBackupEnabled || !fs.existsSync(recordsBackupDir)) return [];
  return fs.readdirSync(recordsBackupDir)
    .filter(name => /^records-.*\.json$/.test(name))
    .map(name => {
      const filePath = path.join(recordsBackupDir, name);
      const stat = fs.statSync(filePath);
      return { name, path: filePath, size: stat.size, modifiedAt: stat.mtime.toISOString() };
    })
    .sort((a, b) => String(b.modifiedAt).localeCompare(String(a.modifiedAt)))
    .slice(0, limit);
}

function loadStore() {
  const saved = readJsonFile(dataPath, null);
  if (saved && typeof saved === 'object' && !Array.isArray(saved)) return saved;
  return {
    settings: {},
    members: defaultMembers.map((name, index) => ({ name, sort_order: index, active: 1 })),
    feedbacks: []
  };
}

let store = loadStore();
let recordsStore = loadRecords(store);
let weeklyScheduleStore = loadWeeklySchedule(store);
const hadEmbeddedRecords = Array.isArray(store.records);
const hadEmbeddedWeeklySchedule = Array.isArray(store.weeklySchedule);
store.feedbacks = Array.isArray(store.feedbacks) ? store.feedbacks : [];
delete store.records;
delete store.weeklySchedule;

function saveStore() {
  const payload = { ...store };
  delete payload.records;
  delete payload.weeklySchedule;
  atomicWriteJson(dataPath, payload);
}

function loadRecords(configStore) {
  const saved = readJsonFile(recordsPath, null);
  if (Array.isArray(saved)) return saved;
  const legacy = Array.isArray(configStore.records) ? configStore.records : [];
  if (legacy.length > 0) {
    atomicWriteJson(recordsPath, legacy);
    return legacy;
  }
  return [];
}

function saveRecords(options = {}) {
  const { backup = true, reason = 'write' } = options;
  if (backup) backupRecordsFile(reason);
  atomicWriteJson(recordsPath, recordsStore);
}

function normalizeWeeklyPlan(plan, index = 0) {
  return {
    id: index + 1,
    start: String(plan.start || '').trim(),
    end: String(plan.end || '').trim(),
    title: String(plan.title || '').trim(),
    video: String(plan.video || '').trim(),
    verse: String(plan.verse || '').trim(),
    url: String(plan.url || '').trim(),
    outlineImage: String(plan.outlineImage || plan.outline_image || plan.image || '').trim(),
    sort_order: index
  };
}

function loadWeeklySchedule(configStore) {
  const saved = readJsonFile(weeklySchedulePath, null);
  if (Array.isArray(saved)) return saved.map(normalizeWeeklyPlan).filter(plan => plan.start && plan.end && plan.title);
  const bundled = readJsonFile(bundledWeeklySchedulePath, null);
  const legacy = Array.isArray(configStore.weeklySchedule) ? configStore.weeklySchedule : [];
  const source = legacy.length > 0 ? legacy : (Array.isArray(bundled) && bundled.length > 0 ? bundled : defaultWeeklySchedule);
  const cleaned = source.map(normalizeWeeklyPlan).filter(plan => plan.start && plan.end && plan.title);
  atomicWriteJson(weeklySchedulePath, cleaned);
  return cleaned;
}

function saveWeeklySchedule() {
  weeklyScheduleStore = weeklyScheduleStore
    .map(normalizeWeeklyPlan)
    .filter(plan => plan.start && plan.end && plan.title)
    .sort((a, b) => String(a.start).localeCompare(String(b.start)))
    .map(normalizeWeeklyPlan);
  atomicWriteJson(weeklySchedulePath, weeklyScheduleStore);
}

if (hadEmbeddedRecords || hadEmbeddedWeeklySchedule) saveStore();
if (!fs.existsSync(recordsPath)) saveRecords({ backup: false, reason: 'init' });

function getSetting(key) {
  return store.settings[key] || '';
}

function setSetting(key, value) {
  store.settings[key] = value;
  saveStore();
}

function hashPassword(password, salt = crypto.randomBytes(16).toString('hex')) {
  const hash = crypto.pbkdf2Sync(password, salt, 120000, 32, 'sha256').toString('hex');
  return `${salt}:${hash}`;
}

function verifyPassword(password, stored) {
  const [salt, hash] = String(stored || '').split(':');
  if (!salt || !hash) return false;
  const candidate = hashPassword(password, salt).split(':')[1];
  return crypto.timingSafeEqual(Buffer.from(candidate, 'hex'), Buffer.from(hash, 'hex'));
}

function signToken(payload) {
  const body = Buffer.from(JSON.stringify(payload)).toString('base64url');
  const sig = crypto.createHmac('sha256', sessionSecret).update(body).digest('base64url');
  return `${body}.${sig}`;
}

function verifyToken(token) {
  const [body, sig] = String(token || '').split('.');
  if (!body || !sig) return null;
  const expected = crypto.createHmac('sha256', sessionSecret).update(body).digest('base64url');
  if (sig !== expected) return null;
  const payload = JSON.parse(Buffer.from(body, 'base64url').toString('utf8'));
  if (!payload.exp || payload.exp < Date.now()) return null;
  return payload;
}

function requireAdmin(req, res, next) {
  const auth = req.headers.authorization || '';
  const token = auth.startsWith('Bearer ') ? auth.slice(7) : '';
  if (!verifyToken(token)) return res.status(401).json({ error: 'unauthorized' });
  next();
}

function listMembers() {
  return store.members.filter(m => m.active !== 0).sort((a, b) => (a.sort_order || 0) - (b.sort_order || 0)).map(m => m.name);
}

function listWeeklySchedule() {
  return [...weeklyScheduleStore].sort((a, b) => (a.start || '').localeCompare(b.start || ''));
}

function recordToClient(row) {
  return {
    Id: row.id,
    姓名: row.name,
    打卡时间: row.checkin_time,
    逻辑日期: row.logical_date,
    是否补签: row.is_retro,
    每日灵修: row.daily,
    周读物: row.book,
    周视频: row.video,
    周背经: row.verse,
    打卡详情: row.detail,
    分享记录: row.note || '',
    类型: row.kind || '',
    部分: row.part || ''
  };
}

function listRecords() {
  return [...recordsStore].sort((a, b) => String(b.checkin_time).localeCompare(String(a.checkin_time)) || (b.id - a.id)).map(recordToClient);
}

function recentRecords(limit = 20) {
  return listRecords().slice(0, Math.min(100, Math.max(1, Number(limit || 20))));
}

function listFeedbacks() {
  return [...(store.feedbacks || [])].sort((a, b) => String(b.created_at).localeCompare(String(a.created_at)) || (b.id - a.id));
}

function nextId(list) {
  return list.reduce((max, item) => Math.max(max, Number(item.id || item.Id || 0)), 0) + 1;
}

function getZonedDateParts(date = new Date()) {
  const parts = new Intl.DateTimeFormat('en-CA', {
    timeZone: absenceAlertTimezone,
    year: 'numeric',
    month: '2-digit',
    day: '2-digit',
    hour: '2-digit',
    hour12: false
  }).formatToParts(date).reduce((acc, part) => {
    acc[part.type] = part.value;
    return acc;
  }, {});
  return {
    date: `${parts.year}-${parts.month}-${parts.day}`,
    hour: Number(parts.hour || 0)
  };
}

function shiftDateString(dateStr, offsetDays) {
  const date = new Date(`${dateStr}T12:00:00Z`);
  date.setUTCDate(date.getUTCDate() + offsetDays);
  return date.toISOString().slice(0, 10);
}

function dailyDoneDatesByMember() {
  const done = new Map(listMembers().map(name => [name, new Set()]));
  for (const record of recordsStore || []) {
    if (!record.name || !record.daily || !record.logical_date) continue;
    if (!done.has(record.name)) done.set(record.name, new Set());
    done.get(record.name).add(String(record.logical_date).slice(0, 10));
  }
  return done;
}

function findAbsentMembers(days = absenceAlertDays, endDate = getZonedDateParts().date) {
  const dateRange = Array.from({ length: days }, (_, index) => shiftDateString(endDate, index - days + 1));
  const done = dailyDoneDatesByMember();
  return listMembers().map(name => {
    const dates = done.get(name) || new Set();
    const missingDates = dateRange.filter(date => !dates.has(date));
    return { name, missingDates };
  }).filter(item => item.missingDates.length === days);
}

function getMailer() {
  if (!process.env.SMTP_HOST || !process.env.SMTP_USER || !process.env.SMTP_PASS) return null;
  return nodemailer.createTransport({
    host: process.env.SMTP_HOST,
    port: Number(process.env.SMTP_PORT || 465),
    secure: String(process.env.SMTP_SECURE || 'true').toLowerCase() !== 'false',
    auth: {
      user: process.env.SMTP_USER,
      pass: process.env.SMTP_PASS
    }
  });
}

async function sendAbsenceAlert({ force = false } = {}) {
  const today = getZonedDateParts().date;
  if (!force && getSetting('last_absence_alert_date') === today) return { skipped: 'already_sent_today' };
  const absentMembers = findAbsentMembers(absenceAlertDays, today);
  if (absentMembers.length === 0) {
    if (!force) setSetting('last_absence_alert_date', today);
    return { sent: false, absentMembers: [] };
  }

  const mailer = getMailer();
  if (!mailer || !absenceAlertTo) return { sent: false, absentMembers, error: 'mail_not_configured' };

  const lines = absentMembers.map(item => `${item.name}: ${item.missingDates.join(', ')}`);
  await mailer.sendMail({
    from: process.env.SMTP_FROM || process.env.SMTP_USER,
    to: absenceAlertTo,
    subject: `门训打卡提醒：${absentMembers.length} 人连续 ${absenceAlertDays} 天未打卡`,
    text: [
      `以下成员连续 ${absenceAlertDays} 天没有每日灵修打卡：`,
      '',
      ...lines,
      '',
      `检查日期：${today}`,
      '此邮件由门训打卡系统自动发送。'
    ].join('\n')
  });
  setSetting('last_absence_alert_date', today);
  return { sent: true, absentMembers };
}

function startAbsenceAlertTimer() {
  if (!absenceAlertEnabled) return;
  setInterval(() => {
    const now = getZonedDateParts();
    if (now.hour !== absenceAlertHour) return;
    sendAbsenceAlert().catch(error => console.error('Absence alert failed:', error.message));
  }, 1000 * 60 * 30);
  setTimeout(() => {
    const now = getZonedDateParts();
    if (now.hour >= absenceAlertHour) {
      sendAbsenceAlert().catch(error => console.error('Absence alert failed:', error.message));
    }
  }, 1000 * 20);
}

app.disable('x-powered-by');
app.use(express.json({ limit: '10mb' }));
app.use('/api', (req, res, next) => {
  res.setHeader('Access-Control-Allow-Origin', '*');
  res.setHeader('Access-Control-Allow-Headers', 'Content-Type, Authorization');
  res.setHeader('Access-Control-Allow-Methods', 'GET, POST, PUT, DELETE, OPTIONS');
  if (req.method === 'OPTIONS') return res.sendStatus(204);
  next();
});
app.use((req, res, next) => {
  const blocked = [/^\/data\//, /^\/node_modules\//, /^\/\.env/, /^\/server\.js$/, /^\/package(-lock)?\.json$/, /^\/docker-compose\.yml$/, /^\/Dockerfile$/];
  if (blocked.some(pattern => pattern.test(req.path))) return res.sendStatus(404);
  next();
});
app.use(express.static(__dirname, { extensions: ['html'] }));

app.get('/api/health', (req, res) => res.json({ ok: true }));
app.get('/api/state', (req, res) => res.json({ members: listMembers(), weeklySchedule: listWeeklySchedule(), records: listRecords(), adminConfigured: Boolean(getSetting('admin_password_hash')) }));
app.get('/api/admin/status', (req, res) => res.json({ configured: Boolean(getSetting('admin_password_hash')) }));
app.get('/api/admin/storage/status', requireAdmin, (req, res) => {
  let writable = true;
  let error = '';
  try {
    fs.mkdirSync(path.dirname(recordsPath), { recursive: true });
    fs.accessSync(path.dirname(recordsPath), fs.constants.W_OK);
    if (recordsBackupEnabled) fs.accessSync(recordsBackupDir, fs.constants.W_OK);
  } catch (err) {
    writable = false;
    error = err.message;
  }

  res.json({
    recordsPath,
    recordsCount: recordsStore.length,
    recordsFileExists: fs.existsSync(recordsPath),
    recordsFileSize: fs.existsSync(recordsPath) ? fs.statSync(recordsPath).size : 0,
    backupEnabled: recordsBackupEnabled,
    backupDir: recordsBackupDir,
    backupLimit: recordsBackupLimit,
    latestBackups: listRecordBackups(8),
    writable,
    error
  });
});
app.get('/api/admin/recent-records', requireAdmin, (req, res) => res.json({ records: recentRecords(req.query.limit) }));
app.post('/api/admin/records/backup', requireAdmin, (req, res) => {
  const backupPath = backupRecordsFile('manual');
  res.json({ ok: true, backupPath, latestBackups: listRecordBackups(8) });
});
app.get('/api/admin/feedback', requireAdmin, (req, res) => res.json({ feedbacks: listFeedbacks() }));
app.get('/api/admin/absence-alert/status', requireAdmin, (req, res) => res.json({
  enabled: absenceAlertEnabled,
  to: absenceAlertTo,
  days: absenceAlertDays,
  hour: absenceAlertHour,
  timezone: absenceAlertTimezone,
  lastSentDate: getSetting('last_absence_alert_date') || '',
  absentMembers: findAbsentMembers(absenceAlertDays)
}));
app.post('/api/admin/absence-alert/test', requireAdmin, async (req, res) => {
  try {
    res.json(await sendAbsenceAlert({ force: true }));
  } catch (error) {
    res.status(500).json({ error: error.message || 'send_failed' });
  }
});

app.post('/api/admin/login', (req, res) => {
  const password = String(req.body.password || '').trim();
  if (!password) return res.status(400).json({ error: 'password_required' });
  const stored = getSetting('admin_password_hash');
  if (!stored) setSetting('admin_password_hash', hashPassword(password));
  else if (!verifyPassword(password, stored)) return res.status(401).json({ error: 'invalid_password' });
  res.json({ token: signToken({ role: 'admin', exp: Date.now() + 1000 * 60 * 60 * 12 }) });
});

app.post('/api/admin/password', requireAdmin, (req, res) => {
  const password = String(req.body.password || '').trim();
  if (!password) return res.status(400).json({ error: 'password_required' });
  setSetting('admin_password_hash', hashPassword(password));
  res.json({ ok: true });
});

app.put('/api/admin/members', requireAdmin, (req, res) => {
  const members = Array.isArray(req.body.members) ? [...new Set(req.body.members.map(name => String(name).trim()).filter(Boolean))] : [];
  if (members.length === 0) return res.status(400).json({ error: 'members_required' });
  store.members = members.map((name, index) => ({ name, sort_order: index, active: 1 }));
  saveStore();
  res.json({ members: listMembers() });
});

app.put('/api/admin/weekly-schedule', requireAdmin, (req, res) => {
  const plans = Array.isArray(req.body.weeklySchedule) ? req.body.weeklySchedule : [];
  const cleaned = plans.map(normalizeWeeklyPlan).filter(plan => plan.start && plan.end && plan.title);
  if (cleaned.length === 0) return res.status(400).json({ error: 'weekly_schedule_required' });
  weeklyScheduleStore = cleaned;
  saveWeeklySchedule();
  res.json({ weeklySchedule: listWeeklySchedule() });
});

app.post('/api/checkins', (req, res) => {
  const body = req.body || {};
  const name = String(body.name || body['姓名'] || '').trim();
  const type = String(body.type || '').trim();
  const logicalDate = String(body.logicalDate || '').trim();
  if (!name || !type || !logicalDate) return res.status(400).json({ error: 'name_type_logicalDate_required' });
  const columns = { 每日灵修: 'daily', 周读物: 'book', 周视频: 'video', 周背经: 'verse' };
  const column = columns[type];
  if (!column) return res.status(400).json({ error: 'invalid_type' });
  const id = nextId(recordsStore);
  const record = { id, name, checkin_time: String(body.checkinTime || new Date().toISOString()), logical_date: logicalDate, is_retro: body.isRetro ? '是' : '否', daily: null, book: null, video: null, verse: null, detail: String(body.detail || ''), note: String(body.note || '').trim() };
  record[column] = '已完成';
  recordsStore.push(record);
  saveRecords({ reason: 'checkin' });
  res.status(201).json(recordToClient(record));
});

app.delete('/api/checkins/:id', (req, res) => {
  const id = Number(req.params.id);
  recordsStore = recordsStore.filter(record => record.id !== id);
  saveRecords({ reason: 'delete' });
  res.json({ ok: true });
});

app.post('/api/reflections', (req, res) => {
  const body = req.body || {};
  const name = String(body.name || '').trim();
  const note = String(body.note || '').trim();
  const part = String(body.part || '其他').trim();
  const logicalDate = String(body.logicalDate || '').trim();
  if (!name || !note || !logicalDate) return res.status(400).json({ error: 'name_note_logicalDate_required' });
  const id = nextId(recordsStore);
  const date = new Date(`${logicalDate}T12:00:00`);
  const month = Number.isNaN(date.getTime()) ? '' : `${date.getMonth() + 1}月${date.getDate()}日 `;
  const record = {
    id,
    name,
    checkin_time: String(body.createdAt || new Date().toISOString()),
    logical_date: logicalDate,
    is_retro: '否',
    daily: null,
    book: null,
    video: null,
    verse: null,
    detail: `【得着】${month}${part}`,
    note,
    kind: 'reflection',
    part
  };
  recordsStore.push(record);
  saveRecords({ reason: 'reflection' });
  res.status(201).json(recordToClient(record));
});

app.post('/api/feedback', (req, res) => {
  const body = req.body || {};
  const message = String(body.message || '').trim();
  if (!message) return res.status(400).json({ error: 'message_required' });
  const feedback = {
    id: nextId(store.feedbacks || []),
    name: String(body.name || '').trim(),
    contact: String(body.contact || '').trim(),
    message,
    page: String(body.page || '').trim(),
    created_at: String(body.createdAt || new Date().toISOString()),
    user_agent: req.headers['user-agent'] || ''
  };
  store.feedbacks = Array.isArray(store.feedbacks) ? store.feedbacks : [];
  store.feedbacks.push(feedback);
  saveStore();
  res.status(201).json({ ok: true, feedback });
});

app.post('/api/import', requireAdmin, (req, res) => {
  const records = Array.isArray(req.body.records) ? req.body.records : [];
  recordsStore = records.map((record, index) => ({
    id: index + 1,
    name: record['姓名'] || record.name || '',
    checkin_time: record['打卡时间'] || record.checkin_time || new Date().toISOString(),
    logical_date: record['逻辑日期'] || record.logical_date || '',
    is_retro: record['是否补签'] || record.is_retro || '否',
    daily: record['每日灵修'] || record.daily || null,
    book: record['周读物'] || record.book || null,
    video: record['周视频'] || record.video || null,
    verse: record['周背经'] || record.verse || null,
    detail: record['打卡详情'] || record.detail || '',
    note: record['分享记录'] || record.note || '',
    kind: record['类型'] || record.kind || '',
    part: record['部分'] || record.part || ''
  }));
  saveRecords({ reason: 'import' });
  res.json({ records: listRecords() });
});

app.get(/.*/, (req, res) => res.sendFile(path.join(__dirname, 'index.html')));

app.listen(port, host, () => {
  console.log(`ZK checkin app listening on http://${host}:${port}`);
  console.log(`Data file: ${dataPath}`);
  if (absenceAlertEnabled) {
    console.log(`Absence alert enabled: ${absenceAlertDays} days, ${absenceAlertHour}:00 ${absenceAlertTimezone}`);
  }
});
startAbsenceAlertTimer();
