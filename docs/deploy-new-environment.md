# 新环境一键部署

适用场景：

- 在一台新的服务器或 NAS Docker 环境中首次部署 AGP。
- 希望一次完成 `MySQL + backend + frontend` 启动。
- 需要在首次部署后顺手把旧平台的一组数据迁移进来。

脚本入口：

```bash
./scripts/deploy-oneclick.sh
```

## 前置条件

目标机器只需要准备：

- Docker
- Docker Compose Plugin（`docker compose`）

不要求本机预装 Go、Node、MySQL。

## 最简空库部署

如果你先只想把平台拉起来，不迁移任何历史数据：

```bash
cd /path/to/agp

export AGP_JWT_SECRET='替换为长随机字符串'
export BOOTSTRAP_SUPERADMIN_USERNAME='admin'
export BOOTSTRAP_SUPERADMIN_PASSWORD='替换为强密码'
export BOOTSTRAP_SUPERADMIN_DISPLAY_NAME='超级管理员'

./scripts/deploy-oneclick.sh
```

脚本会自动：

1. 创建 `data/mysql`、`data/assets`、`data/backups/mysql`
2. 构建并启动 `mysql / backend / frontend`
3. 等待 MySQL 就绪
4. 输出访问地址与管理员账号

## 首次部署并迁移首个小组

如果你要在新环境里直接把旧 JSON 数据一起迁移：

```bash
cd /path/to/agp

export AGP_JWT_SECRET='替换为长随机字符串'
export BOOTSTRAP_SUPERADMIN_USERNAME='admin'
export BOOTSTRAP_SUPERADMIN_PASSWORD='替换为强密码'

export PRIMARY_GROUP_CODE='agape-a'
export PRIMARY_GROUP_NAME='AGAPE A组'
export PRIMARY_GROUP_DEFAULT_PASSWORD='Abc12345'
export PRIMARY_CONFIG_PATH='/absolute/path/to/config.json'
export PRIMARY_RECORDS_PATH='/absolute/path/to/records.json'
export PRIMARY_NAME_MAP='/absolute/path/to/name-map.json'

./scripts/deploy-oneclick.sh
```

脚本会额外执行：

1. 首组数据 `dry-run`
2. 生成迁移报告到 `data/migration-reports/`
3. 正式导入该组数据

如果你只想先看迁移报告，不马上写库：

```bash
export PRIMARY_DRY_RUN_ONLY=true
./scripts/deploy-oneclick.sh
```

## 常用环境变量

### 部署相关

```bash
COMPOSE_PROJECT_NAME=agp
AGP_WEB_PORT=5112
AGP_MYSQL_PORT=3307
MYSQL_DATABASE=agp
MYSQL_USER=agp
MYSQL_PASSWORD=agp
MYSQL_ROOT_PASSWORD=agp-root
AGP_JWT_SECRET=...
BOOTSTRAP_SUPERADMIN_USERNAME=admin
BOOTSTRAP_SUPERADMIN_PASSWORD=...
BOOTSTRAP_SUPERADMIN_DISPLAY_NAME=超级管理员
```

### 首组迁移相关

```bash
PRIMARY_GROUP_CODE=agape-a
PRIMARY_GROUP_NAME='AGAPE A组'
PRIMARY_GROUP_DEFAULT_PASSWORD='Abc12345'
PRIMARY_CONFIG_PATH=/absolute/path/to/config.json
PRIMARY_RECORDS_PATH=/absolute/path/to/records.json
PRIMARY_NAME_MAP=/absolute/path/to/name-map.json
PRIMARY_ALLOW_DUPLICATE_AS_DELETED=false
PRIMARY_FAIL_ON_GENERATED_USERNAMES=false
PRIMARY_DRY_RUN_ONLY=false
RUN_PRIMARY_MIGRATION=auto
```

`RUN_PRIMARY_MIGRATION` 支持：

- `auto`：当迁移参数齐全时自动迁移
- `true`：强制迁移
- `false`：强制不迁移

## 输出结果

脚本结束后会打印：

- 前端访问地址
- MySQL 访问端口
- 超级管理员账号密码
- 首组迁移默认密码
- 迁移报告目录

## 建议的上线顺序

1. 先用 `PRIMARY_DRY_RUN_ONLY=true` 看报告
2. 检查 `generated_usernames`
3. 确认组编码、组名、成员账号映射无误
4. 再执行正式导入
5. 登录后台验证成员、周任务、学习内容和打卡记录

