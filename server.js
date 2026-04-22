const crypto = require('crypto');
const fs = require('fs');
const path = require('path');
const express = require('express');

const app = express();
const port = Number(process.env.PORT || 3000);
const host = process.env.HOST || '0.0.0.0';
const dataPath = path.resolve(process.env.DATABASE_PATH || path.join(__dirname, 'data', 'app.json'));
const sessionSecret = process.env.SESSION_SECRET || 'dev-secret-change-me';

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

function loadStore() {
  try {
    if (fs.existsSync(dataPath)) return JSON.parse(fs.readFileSync(dataPath, 'utf8'));
  } catch (_) {}
  return {
    settings: {},
    members: defaultMembers.map((name, index) => ({ name, sort_order: index, active: 1 })),
    weeklySchedule: defaultWeeklySchedule.map((plan, index) => ({ id: index + 1, ...plan, sort_order: index })),
    records: []
  };
}

let store = loadStore();

function saveStore() {
  fs.writeFileSync(dataPath, JSON.stringify(store, null, 2));
}

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
  return [...store.weeklySchedule].sort((a, b) => (a.start || '').localeCompare(b.start || ''));
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
    打卡详情: row.detail
  };
}

function listRecords() {
  return [...store.records].sort((a, b) => String(b.checkin_time).localeCompare(String(a.checkin_time)) || (b.id - a.id)).map(recordToClient);
}

function nextId(list) {
  return list.reduce((max, item) => Math.max(max, Number(item.id || item.Id || 0)), 0) + 1;
}

app.disable('x-powered-by');
app.use(express.json({ limit: '10mb' }));
app.use((req, res, next) => {
  const blocked = [/^\/data\//, /^\/node_modules\//, /^\/\.env/, /^\/server\.js$/, /^\/package(-lock)?\.json$/, /^\/docker-compose\.yml$/, /^\/Dockerfile$/];
  if (blocked.some(pattern => pattern.test(req.path))) return res.sendStatus(404);
  next();
});
app.use(express.static(__dirname, { extensions: ['html'] }));

app.get('/api/health', (req, res) => res.json({ ok: true }));
app.get('/api/state', (req, res) => res.json({ members: listMembers(), weeklySchedule: listWeeklySchedule(), records: listRecords(), adminConfigured: Boolean(getSetting('admin_password_hash')) }));
app.get('/api/admin/status', (req, res) => res.json({ configured: Boolean(getSetting('admin_password_hash')) }));

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
  const cleaned = plans.map(plan => ({
    start: String(plan.start || '').trim(),
    end: String(plan.end || '').trim(),
    title: String(plan.title || '').trim(),
    video: String(plan.video || '').trim(),
    verse: String(plan.verse || '').trim(),
    url: String(plan.url || '').trim()
  })).filter(plan => plan.start && plan.end && plan.title);
  if (cleaned.length === 0) return res.status(400).json({ error: 'weekly_schedule_required' });
  store.weeklySchedule = cleaned.map((plan, index) => ({ id: index + 1, ...plan, sort_order: index }));
  saveStore();
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
  const id = nextId(store.records);
  const record = { id, name, checkin_time: String(body.checkinTime || new Date().toISOString()), logical_date: logicalDate, is_retro: body.isRetro ? '是' : '否', daily: null, book: null, video: null, verse: null, detail: String(body.detail || '') };
  record[column] = '已完成';
  store.records.push(record);
  saveStore();
  res.status(201).json(recordToClient(record));
});

app.delete('/api/checkins/:id', (req, res) => {
  const id = Number(req.params.id);
  store.records = store.records.filter(record => record.id !== id);
  saveStore();
  res.json({ ok: true });
});

app.post('/api/import', requireAdmin, (req, res) => {
  const records = Array.isArray(req.body.records) ? req.body.records : [];
  store.records = records.map((record, index) => ({
    id: index + 1,
    name: record['姓名'] || record.name || '',
    checkin_time: record['打卡时间'] || record.checkin_time || new Date().toISOString(),
    logical_date: record['逻辑日期'] || record.logical_date || '',
    is_retro: record['是否补签'] || record.is_retro || '否',
    daily: record['每日灵修'] || record.daily || null,
    book: record['周读物'] || record.book || null,
    video: record['周视频'] || record.video || null,
    verse: record['周背经'] || record.verse || null,
    detail: record['打卡详情'] || record.detail || ''
  }));
  saveStore();
  res.json({ records: listRecords() });
});

app.get(/.*/, (req, res) => res.sendFile(path.join(__dirname, 'index.html')));

app.listen(port, host, () => {
  console.log(`ZK checkin app listening on http://${host}:${port}`);
  console.log(`Data file: ${dataPath}`);
});
