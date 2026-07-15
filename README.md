# AGP 门训打卡平台

这是一个面向门训/小组学习场景的打卡与管理平台。当前主版本已经升级为前后端分离架构：

- 后端：Go
- 数据库：MySQL 8.0
- 前端：Vue 3 + Vite + Pinia
- 部署：Docker Compose

当前版本支持多小组隔离、按组学习内容配置、成员与权限管理、每日/周任务打卡、统计看板、资源库与旧数据迁移。

## 功能概览

- 多小组隔离：成员、打卡、周任务、资源按 `group_id` 隔离
- 权限体系：超级管理员、组长、小组管理员、普通成员
- 打卡工作台：首页展示当天学习任务与个人打卡记录
- 统计中心：小组完成率、成员矩阵、本月累计排行
- 学习内容管理：按组配置每日内容、周任务、视频、读物、背经、提纲图
- 资源库：支持按分类查看和上传 Markdown/PDF/视频/讲义资源
- 历史迁移：支持把旧 `config.json` 和 `records.json` 导入 MySQL 平台

## 目录结构

```text
.
├── backend/                     # Go 后端
│   ├── cmd/server/main.go
│   ├── cmd/migrate-json/main.go
│   ├── migrations/
│   └── Dockerfile
├── frontend/                    # Vue 3 + Vite + Pinia 前端
│   ├── src/main.js
│   ├── src/App.vue
│   ├── src/stores/
│   ├── src/legacy-app.js        # 当前前端业务运行时与状态桥接层
│   ├── src/styles.css
│   ├── package.json
│   ├── vite.config.js
│   ├── nginx.conf
│   └── Dockerfile
├── deploy/
│   └── docker-compose.separated.yml
├── scripts/
│   ├── deploy-oneclick.sh       # 新环境一键部署
│   └── migrate-group.sh         # 独立迁移其他组
├── docs/
│   ├── ops-commands.md
│   ├── deploy-new-environment.md
│   ├── migrate-other-groups.md
│   └── implementation-notes.md
├── Book/
├── PPT/
└── config.json                  # 旧数据迁移输入之一
```

## 快速开始

### 1. 直接启动当前平台

先生成环境文件并填写所有 `CHANGE_ME` 项，再启动服务：

```bash
cp .env.example .env
chmod 600 .env
# 使用 DSM 文本编辑器或本地编辑后上传 .env
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml up -d --build
```

默认访问地址：

```text
http://NAS_IP:2973
```

默认 MySQL 端口：

```text
127.0.0.1:3307
```

首个超级管理员账号默认为 `admin`，密码必须在 `.env` 中通过
`BOOTSTRAP_SUPERADMIN_PASSWORD` 设置；项目不再提供可用于生产环境的默认密码。

必须设置的密钥：

```dotenv
MYSQL_PASSWORD=替换为随机数据库密码
MYSQL_ROOT_PASSWORD=替换为随机 root 密码
AGP_JWT_SECRET=替换为至少32位的随机字符串
BOOTSTRAP_SUPERADMIN_PASSWORD=替换为至少12位的强密码
```

### 2. 本地检查

```bash
cd backend
go test ./...

cd ..
cd frontend
npm install
npm run build

cd ..
docker compose --env-file .env -f deploy/docker-compose.separated.yml config
```

## 新环境一键部署

如果你要在一台新的服务器、NAS 或 Docker 主机上直接部署：

```bash
sudo bash ./scripts/deploy-oneclick.sh
```

这个脚本会：

1. 初始化 `data/mysql`、`data/assets`、`data/backups/mysql`
2. 启动 `mysql / backend / frontend`
3. 等待 MySQL 就绪
4. 可选执行首个小组迁移

详细说明见：

- [docs/deploy-new-environment.md](docs/deploy-new-environment.md)

## 旧数据迁移

### 首次部署时迁移首个小组

```bash
export PRIMARY_GROUP_CODE='agape-a'
export PRIMARY_GROUP_NAME='AGAPE A组'
export PRIMARY_GROUP_DEFAULT_PASSWORD='Abc12345'
export PRIMARY_CONFIG_PATH='/absolute/path/to/config.json'
export PRIMARY_RECORDS_PATH='/absolute/path/to/records.json'
export PRIMARY_NAME_MAP='/absolute/path/to/name-map.json'

./scripts/deploy-oneclick.sh
```

### 已上线后继续迁移其他组

```bash
GROUP_CODE='agape-b' \
GROUP_NAME='AGAPE B组' \
CONFIG_PATH='/absolute/path/to/config.json' \
RECORDS_PATH='/absolute/path/to/records.json' \
NAME_MAP_PATH='/absolute/path/to/name-map.json' \
GROUP_DEFAULT_PASSWORD='Abc12345' \
EXECUTE_IMPORT=true \
sudo -E bash ./scripts/migrate-group.sh
```

默认建议先只做 dry-run，检查迁移报告后再导入。

详细说明见：

- [docs/migrate-other-groups.md](docs/migrate-other-groups.md)
- [docs/ops-commands.md](docs/ops-commands.md)

## 运行与运维

常用命令：

```bash
# 启动
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml up -d --build

# 查看状态
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml ps

# 查看日志
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml logs -f

# 停止
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml down
```

MySQL 进入方式：

```bash
set -a; . ./.env; set +a
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" -it agp-mysql mysql -u"$MYSQL_USER" "$MYSQL_DATABASE"
```

数据库备份：

```bash
mkdir -p data/backups/mysql
sudo docker exec -e MYSQL_PWD="$MYSQL_PASSWORD" agp-mysql mysqldump -u"$MYSQL_USER" "$MYSQL_DATABASE" > data/backups/mysql/agp-$(date +%F).sql
```

更多 SQL、迁移与维护命令见：

- [docs/ops-commands.md](docs/ops-commands.md)

## 当前实现规则

- 每个账号只有一个真实登录密码
- 未认证状态下不展示用户所属小组
- 小组默认密码只影响只属于该组的普通成员
- 小组管理员和组长不能操作超级管理员、同级或自己
- 打卡记录按小组隔离
- 未来日期禁止打卡
- PDF 读物通过后端裁页接口只暴露指定页范围

## 说明文档

- [docs/implementation-notes.md](docs/implementation-notes.md)：实现说明
- [docs/deploy-new-environment.md](docs/deploy-new-environment.md)：新环境部署
- [docs/migrate-other-groups.md](docs/migrate-other-groups.md)：分组迁移方案
- [docs/ops-commands.md](docs/ops-commands.md)：运维命令速查

## 迁移输入说明

当前仓库保留的旧数据输入主要是迁移所需配置与记录文件，例如：

- `config.json`
- `data/records.json`

它们仅用于迁移旧数据，不作为当前版本的运行入口。
