const baseUrl = process.env.BASE_URL || 'http://127.0.0.1:3000';
const adminPassword = process.env.ADMIN_PASSWORD || '';
const nocodbUrl = process.env.NOCODB_URL || '';
const nocodbToken = process.env.NOCODB_TOKEN || '';

if (!adminPassword) {
  console.error('ADMIN_PASSWORD is required');
  process.exit(1);
}
if (!nocodbUrl || !nocodbToken) {
  console.error('NOCODB_URL and NOCODB_TOKEN are required');
  process.exit(1);
}

async function api(path, options = {}) {
  const headers = { ...(options.headers || {}) };
  if (options.body && !headers['Content-Type']) headers['Content-Type'] = 'application/json';
  const res = await fetch(`${baseUrl}${path}`, { ...options, headers });
  const text = await res.text();
  let body = {};
  try { body = text ? JSON.parse(text) : {}; } catch (_) {}
  if (!res.ok) throw new Error(body.error || text || `HTTP ${res.status}`);
  return body;
}

async function fetchJson(url, headers = {}) {
  const res = await fetch(url, { headers });
  const text = await res.text();
  if (!res.ok) throw new Error(`NocoDB HTTP ${res.status}: ${text}`);
  return text ? JSON.parse(text) : {};
}

async function fetchAllNocodbRecords() {
  const records = [];
  const limit = 1000;
  let offset = 0;
  for (;;) {
    const url = `${nocodbUrl}?limit=${limit}&offset=${offset}&_t=${Date.now()}`;
    const data = await fetchJson(url, { 'xc-token': nocodbToken });
    const list = Array.isArray(data.list) ? data.list : [];
    records.push(...list);
    if (list.length < limit) break;
    offset += limit;
  }
  return records;
}

async function main() {
  const login = await api('/api/admin/login', {
    method: 'POST',
    body: JSON.stringify({ password: adminPassword })
  });
  const token = login.token;

  const membersRes = await api('/api/state');
  const members = Array.isArray(membersRes.members) ? membersRes.members : [];
  const weeklySchedule = Array.isArray(membersRes.weeklySchedule) ? membersRes.weeklySchedule : [];

  const rawRecords = await fetchAllNocodbRecords();
  const mapped = rawRecords.map(record => ({
    姓名: record['姓名'] || record.name || '',
    打卡时间: record['打卡时间'] || record.checkin_time || record['打卡時間'] || '',
    逻辑日期: record['逻辑日期'] || record.logical_date || '',
    是否补签: record['是否补签'] || record.is_retro || '否',
    每日灵修: record['每日灵修'] || record.daily || null,
    周读物: record['周读物'] || record.book || null,
    周视频: record['周视频'] || record.video || null,
    周背经: record['周背经'] || record.verse || null,
    打卡详情: record['打卡详情'] || record.detail || ''
  }));

  await api('/api/admin/members', {
    method: 'PUT',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ members: members.length ? members : undefined })
  }).catch(() => {});

  if (weeklySchedule.length) {
    await api('/api/admin/weekly-schedule', {
      method: 'PUT',
      headers: { Authorization: `Bearer ${token}` },
      body: JSON.stringify({ weeklySchedule })
    }).catch(() => {});
  }

  await api('/api/import', {
    method: 'POST',
    headers: { Authorization: `Bearer ${token}` },
    body: JSON.stringify({ records: mapped })
  });

  console.log(`Imported ${mapped.length} records from NocoDB into ${baseUrl}`);
}

main().catch(err => {
  console.error(err.message || err);
  process.exit(1);
});
