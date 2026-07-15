# AGP 运维命令速查

本文档面向当前 Go + MySQL + 前端分离版本，对应部署文件：

```bash
deploy/docker-compose.separated.yml
```

新环境一键部署与分组迁移脚本见：

```text
scripts/deploy-oneclick.sh
scripts/migrate-group.sh
docs/deploy-new-environment.md
docs/migrate-other-groups.md
```

## 启动与停止

在项目根目录执行：

```bash
cd /volume2/docker/discipleship

# 构建并启动 MySQL、后端、前端
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml up -d --build

# 查看运行状态
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml ps

# 查看全部服务日志
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml logs -f

# 只查看后端日志
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml logs -f backend

# 只查看 MySQL 日志
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml logs -f mysql

# 重启后端
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml restart backend

# 停止服务，保留数据卷目录 data/mysql 和 data/assets
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml down
```

默认访问地址：

```text
http://NAS_IP:2973
```

## NAS 升级与 502 排查

升级现有部署时，先备份数据库，再强制重建后端和前端镜像，避免 NAS 继续使用旧构建缓存：

```bash
cd /volume2/docker/discipleship
set -a; . ./.env; set +a
mkdir -p data/backups/mysql
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" agp-mysql mysqldump -u"$MYSQL_USER" "$MYSQL_DATABASE" > data/backups/mysql/agp-before-upgrade-$(date +%F-%H%M%S).sql
sudo git pull
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml build --no-cache backend frontend
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml up -d
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml ps
```

遇到 HTTP 502 时，先查看后端而不是删除数据库目录：

```bash
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml logs --tail=200 backend mysql
curl -fsS http://127.0.0.1:2973/api/health
```

当前版本的迁移器可以继续完成曾被 `Duplicate column name` 中断的迁移。若日志仍反复出现旧错误，通常表示后端镜像没有重建，请执行上面的 `build --no-cache backend`。升级会使旧登录令牌失效，用户重新登录一次即可。不要为修复 502 执行 `rm -rf data/mysql`；这会删除现有账号和打卡记录。

可在 `.env` 中把 `AGP_WEB_PORT` 改为其他端口，然后重建前端：

```bash
# .env
AGP_WEB_PORT=8088

# 终端
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml up -d --build
```

## MySQL 连接

数据库名、用户和密码来自 `.env`。先在当前终端安全载入：

```bash
set -a
. ./.env
set +a
```

容器名为 `agp-mysql`，宿主机端口默认为 `127.0.0.1:3307`。

小组表名为 `study_groups`。

进入 MySQL：

```bash
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" -it agp-mysql mysql -u"$MYSQL_USER" "$MYSQL_DATABASE"
```

使用 root 进入：

```bash
sudo docker exec -e MYSQL_PWD="$MYSQL_ROOT_PASSWORD" -it agp-mysql mysql -uroot "$MYSQL_DATABASE"
```

在宿主机直接执行一条 SQL：

```bash
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" -i agp-mysql mysql -u"$MYSQL_USER" "$MYSQL_DATABASE" -e "SHOW TABLES;"
```

导出数据库备份：

```bash
mkdir -p data/backups/mysql
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" agp-mysql mysqldump -u"$MYSQL_USER" "$MYSQL_DATABASE" > data/backups/mysql/agp-$(date +%F).sql
```

## 查询指定小组数据

以下 SQL 可在 MySQL 客户端中执行。先按小组编码或名称设置变量：

```sql
-- 推荐使用小组编码，例：agape-a
SET @group_code = '替换为小组编码';
SET @group_id = (
  SELECT id
  FROM study_groups
  WHERE code = @group_code
  LIMIT 1
);

SELECT @group_id AS group_id;
```

如果只知道小组名称：

```sql
SET @group_name = '替换为小组名称';
SET @group_id = (
  SELECT id
  FROM study_groups
  WHERE name = @group_name
  LIMIT 1
);

SELECT @group_id AS group_id;
```

查看小组基本信息：

```sql
SELECT id, code, name, description, status, created_at, updated_at
FROM study_groups
WHERE id = @group_id;
```

查看小组成员和角色：

```sql
SELECT
  m.id AS member_id,
  u.id AS user_id,
  u.username,
  u.display_name,
  m.member_name,
  u.is_super_admin,
  u.status AS user_status,
  GROUP_CONCAT(r.role ORDER BY r.role SEPARATOR ',') AS roles,
  m.joined_at
FROM group_members m
JOIN users u ON u.id = m.user_id
LEFT JOIN user_group_roles r
  ON r.group_id = m.group_id AND r.user_id = m.user_id
WHERE m.group_id = @group_id
  AND m.status = 1
GROUP BY m.id, u.id, u.username, u.display_name, m.member_name, u.is_super_admin, u.status, m.joined_at
ORDER BY m.id;
```

查看指定小组最近打卡记录：

```sql
SELECT
  cr.id,
  cr.logical_date,
  cr.checkin_time,
  u.username,
  u.display_name,
  cr.task_type,
  cr.status,
  cr.is_retro,
  cr.detail,
  cr.part,
  cr.deleted_at
FROM checkin_records cr
JOIN users u ON u.id = cr.user_id
WHERE cr.group_id = @group_id
  AND cr.deleted_at IS NULL
ORDER BY cr.logical_date DESC, cr.checkin_time DESC
LIMIT 100;
```

按日期范围查询打卡记录：

```sql
SET @start_date = '2026-06-01';
SET @end_date = '2026-06-30';

SELECT
  cr.logical_date,
  u.display_name,
  cr.task_type,
  cr.detail,
  cr.checkin_time
FROM checkin_records cr
JOIN users u ON u.id = cr.user_id
WHERE cr.group_id = @group_id
  AND cr.logical_date BETWEEN @start_date AND @end_date
  AND cr.deleted_at IS NULL
ORDER BY cr.logical_date, u.display_name, cr.task_type;
```

统计每日打卡数量：

```sql
SELECT
  logical_date,
  task_type,
  COUNT(*) AS total
FROM checkin_records
WHERE group_id = @group_id
  AND deleted_at IS NULL
GROUP BY logical_date, task_type
ORDER BY logical_date DESC, task_type;
```

统计成员维度打卡数量：

```sql
SELECT
  u.display_name,
  cr.task_type,
  COUNT(*) AS total
FROM checkin_records cr
JOIN users u ON u.id = cr.user_id
WHERE cr.group_id = @group_id
  AND cr.deleted_at IS NULL
GROUP BY u.id, u.display_name, cr.task_type
ORDER BY u.display_name, cr.task_type;
```

查看周计划：

```sql
SELECT
  id,
  start_date,
  end_date,
  title,
  verse_ref,
  book_enabled,
  video_enabled,
  verse_enabled,
  outline_enabled,
  sort_order,
  created_at,
  updated_at
FROM study_weeks
WHERE group_id = @group_id
ORDER BY start_date DESC;
```

查看资源文件：

```sql
SELECT
  id,
  category,
  title,
  original_name,
  storage_path,
  mime_type,
  file_size,
  created_at
FROM assets
WHERE group_id = @group_id
ORDER BY category, id DESC;
```

查看管理操作审计：

```sql
SELECT
  id,
  actor_user_id,
  action,
  target_type,
  target_id,
  ip,
  created_at
FROM audit_logs
WHERE group_id = @group_id
ORDER BY id DESC
LIMIT 100;
```

## 常用定位命令

检查后端健康：

```bash
curl -fsS http://127.0.0.1:2973/api/health
```

查看后端实际环境变量：

```bash
sudo docker exec agp-backend env | grep '^AGP_'
```

查看 MySQL 表：

```bash
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" -i agp-mysql mysql -u"$MYSQL_USER" "$MYSQL_DATABASE" -e "SHOW TABLES;"
```

查看打卡表分区：

```bash
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" -i agp-mysql mysql -u"$MYSQL_USER" "$MYSQL_DATABASE" -e "
SELECT PARTITION_NAME, PARTITION_DESCRIPTION, TABLE_ROWS
FROM information_schema.PARTITIONS
WHERE TABLE_SCHEMA = DATABASE()
  AND TABLE_NAME = 'checkin_records'
ORDER BY PARTITION_ORDINAL_POSITION;
"
```

## 旧 JSON 数据迁移

迁移工具路径：

```text
backend/cmd/migrate-json
```

先执行 dry-run，只解析 `config.json` 和 `data/records.json`，不写数据库：

```bash
cd /Users/bytedance/program/agp/backend

go run ./cmd/migrate-json \
  --group-code agape-a \
  --group-name "AGAPE A组" \
  --config ../config.json \
  --records ../data/records.json \
  --default-password "Abc12345" \
  --report-dir ../data/migration-reports \
  --dry-run=true
```

确认报告后执行真实导入：

```bash
cd /Users/bytedance/program/agp/backend

go run ./cmd/migrate-json \
  --dsn "agp:agp@tcp(127.0.0.1:3307)/agp?parseTime=true&multiStatements=false&charset=utf8mb4,utf8" \
  --group-code agape-a \
  --group-name "AGAPE A组" \
  --config ../config.json \
  --records ../data/records.json \
  --default-password "Abc12345" \
  --report-dir ../data/migration-reports \
  --dry-run=false
```

如果在 Docker Compose 网络内执行，DSN 中 MySQL 地址应使用服务名：

```text
agp:agp@tcp(mysql:3306)/agp?parseTime=true&multiStatements=false&charset=utf8mb4,utf8
```

如果要指定中文姓名到账号拼音的映射，创建 `name-map.json`：

```json
{
  "张迦勒": "zhangjiale",
  "陈思佳": "chensijia"
}
```

然后增加参数：

```bash
--name-map ../name-map.json
```

迁移报告会输出到：

```text
data/migration-reports/
```

当前导入规则：

- `config.json` 导入 `study_groups`、`group_settings`、`users`、`group_members`、`user_group_roles`、`study_weeks`、`study_tasks`、`assets`、`task_assets`。
- `records.json` 导入 `checkin_records`。
- `daily=done` -> `daily_devotion`。
- `book=done` -> `weekly_book`。
- `video=done` -> `weekly_video`。
- `verse=done` -> `weekly_verse`。
- `kind=reflection` -> `reflection`。
- `kind=recite_exam` -> `recite_exam`。
- 重复有效打卡默认跳过；如需保留为软删除记录，增加 `--allow-duplicate-as-deleted=true`。
