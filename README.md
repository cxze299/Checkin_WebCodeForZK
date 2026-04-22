# Checkin_WebCodeForZK

科大门训打卡系统。当前版本已经升级为：

- 前端：保留原来的单页打卡界面
- 后端：Node.js + Express
- 数据库：JSON 文件，默认保存到 `./data/app.json`
- 部署：适合群晖 NAS 的 Container Manager / Docker Compose

## 本地启动

需要 Node.js 20 或更高版本。

```bash
npm install
npm run start
```

默认访问：

```text
http://localhost:3000
```

首次点击页面里的“管理与导出”，会设置管理员密码。之后成员管理、周任务管理、备份导入都需要登录后台。

## 群晖 NAS 部署

推荐用群晖 Container Manager：

1. 把整个项目目录上传到 NAS，例如 `/volume1/docker/zk-checkin`。
2. 编辑 `docker-compose.yml`，把 `SESSION_SECRET` 改成一段足够长的随机字符串。
3. 在 Container Manager 里用项目方式导入 `docker-compose.yml`。
4. 启动后访问：

```text
http://你的NAS地址:3000
```

数据会保存在项目目录的 `data/app.json`，这个目录已经通过 volume 挂载到容器外，升级容器不会丢数据。

## 备份

建议定期备份：

```text
data/app.json
```

网页后台里的“导出本地备份 JSON”也可以导出成员、周任务和打卡记录。

## 从 NocoDB 迁移

如果你旧系统的数据还在 NocoDB，可以直接跑迁移脚本：

```bash
ADMIN_PASSWORD=你的管理员密码 \
NOCODB_URL=https://你的nocodb地址/api/v2/tables/你的表/records \
NOCODB_TOKEN=你的xc-token \
BASE_URL=http://127.0.0.1:3000 \
node migrate-nocodb.js
```

迁移前建议先在后台初始化一次管理员密码，再执行这个脚本。脚本会把 NocoDB 里的记录导入当前 JSON 数据库。

## 常用维护

如果使用 SSH：

```bash
cd /volume1/docker/zk-checkin
docker compose up -d --build
docker compose logs -f
```

停止：

```bash
docker compose down
```

## 环境变量

可参考 `.env.example`：

- `PORT`：服务端口，默认 `3000`
- `HOST`：监听地址，默认 `0.0.0.0`
- `DATABASE_PATH`：JSON 数据路径
- `SESSION_SECRET`：管理员登录 token 签名密钥，部署前必须修改
