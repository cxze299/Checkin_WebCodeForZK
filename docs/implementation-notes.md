# 前后端分离实现说明

本次实现为当前使用中的前后端分离版本说明。

## 新增目录

- `backend/`：Go 后端服务。
  - `cmd/server/main.go`：HTTP API、认证、权限、打卡、成员、小组、资源、分区维护。
  - `migrations/`：MySQL 8.0 初始化 SQL。
  - `Dockerfile`：Go 后端镜像。
- `frontend/`：Vue 3 + Vite + Pinia 浏览器 Web 前端。
  - `index.html`
  - `src/main.js`
  - `src/App.vue`
  - `src/components/AppRoot.vue`：Vue 化的全局页面壳，负责登录、布局、导航、小组选择、资源页、管理页、月历和 toast。
  - `src/components/Dashboard.vue`：Vue 化的统计页，负责小组完成率、成员打卡矩阵和月度分项排行。
  - `src/components/CheckinWorkbench.vue`：Vue 化的首页打卡工作台，负责日期切换、任务卡、打卡操作和我的记录。
  - `src/components/ContentViewer.vue`：Vue 化的内容查看器，负责 Markdown/PDF/视频/图片弹窗与关联资料栏。
  - `src/stores/`
  - `src/stores/appState.js`：全局页面壳 Pinia 状态。
  - `src/stores/dashboard.js`：统计页 Pinia 状态。
  - `src/stores/checkinWorkbench.js`：首页打卡工作台 Pinia 状态。
  - `src/stores/contentViewer.js`：内容查看器 Pinia 状态。
  - `src/legacy-app.js`：前端业务运行时与状态桥接层，负责 API 调用、状态计算和动作函数；主页面 DOM 已迁出到 Vue。
  - `src/styles.css`
  - `package.json`
  - `vite.config.js`
  - `nginx.conf`
  - `Dockerfile`
- `deploy/docker-compose.separated.yml`：MySQL + Go 后端 + Nginx 前端部署。

## 运行方式

```bash
cp .env.example .env
# 填写所有 CHANGE_ME 项后：
sudo docker compose --env-file .env -f deploy/docker-compose.separated.yml up -d --build
```

默认访问端口：

```text
http://NAS_IP:2973
```

首个超级管理员账号默认为 `admin`，但项目不提供默认生产密码。部署前必须在 `.env` 中设置：

```dotenv
MYSQL_PASSWORD=随机数据库密码
MYSQL_ROOT_PASSWORD=随机 root 密码
AGP_JWT_SECRET=至少32位随机字符串
BOOTSTRAP_SUPERADMIN_PASSWORD=至少12位强密码
```

## 已实现的核心规则

- 每个账号只有一个实际登录密码，可使用邮箱或用户名登录。
- 未认证状态下不展示用户所属小组，避免通过用户名枚举小组归属。
- 小组默认密码只用于新建本组用户初始化密码，以及按规则批量重置成员密码。
- 组长修改本组默认密码时：
  - 仅影响只属于该组、非组长、非超级管理员的成员账号；
  - 多小组成员不被覆盖；
  - 组长本人不被覆盖；
  - API 返回受影响账号数，前端提示影响范围。
- 超级管理员可以全局重置所有非超级管理员账号密码。
- 首期启用 MySQL 分区表 `checkin_records`，后端启动时会检查并提前创建未来分区。
- 打卡记录按 `group_id` 隔离，成员只能撤销本人最近 7 天记录；管理员纠错删除走管理接口并写审计。

## 本地检查

```bash
cd backend
go build ./cmd/server

cd ..
cd frontend
npm install
npm run build

cd ..
docker compose --env-file .env -f deploy/docker-compose.separated.yml config
```

## 后续建议

- 增加旧 JSON 数据迁移 CLI，将 `config.json`、`data/records.json` 导入 MySQL。
- 继续把 `src/legacy-app.js` 中的业务计算和 API 动作迁入更清晰的 Pinia actions / composables。
- 根据 NAS 实际路径调整 `AGP_ASSETS_ROOT`，并把 `/data/agp/assets` 纳入备份。
