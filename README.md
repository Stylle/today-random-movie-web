# 今日想看（Today Random Movie Web）

一个面向飞牛影视 / FnTV 私有媒体库的 Web 随机选片器。它参考了本地版 [today-random-movie](https://github.com/Stylle/today-random-movie) 的功能流程，重建为适合 Docker 与公网反向代理部署的 Go + Vue 应用。

![Go](https://img.shields.io/badge/Go-1.26-00ADD8?logo=go&logoColor=white)
![Vue](https://img.shields.io/badge/Vue-3-42b883?logo=vuedotjs&logoColor=white)
![Docker](https://img.shields.io/badge/Docker-amd64%20%7C%20arm64-2496ED?logo=docker&logoColor=white)

## 主要功能

- 飞牛影视登录、媒体库与影片索引同步；兼容常见 `/v/api/v1` 接口和字段差异。
- 媒体库、类型、地区、年份、观看状态筛选。
- 当前会话上一部回看；服务端最近 30 次去重，影片池耗尽后自动放宽。
- 图床项目同系的低饱和绿色 UI、明暗主题与手机端布局。
- 服务端封面代理：按需拉取、15 MiB 上限、原子落盘、ETag 与浏览器缓存。
- 管理员登录、HttpOnly Cookie、CSRF、登录/随机/封面限流与安全响应头。
- 通用 Webhook：GET/POST/PUT、自定义请求头、JSON/Text 模板和测试发送。
- 内置“类 cron”调度：每日、每周、每月规则，各自拥有时区、时间和选片筛选条件。
- 单镜像、SQLite、健康检查、非 root 和只读根文件系统。

## 为什么封面传输不再直接走浏览器

本地版可以直接访问 NAS 地址，但公网 HTTPS 页面直接加载内网 HTTP 图片会遇到混合内容、CORS、地址泄露和客户端无法路由等问题。本项目让后端持有飞牛会话并代理封面：

```text
浏览器 ── /api/posters/{id} ──> 应用缓存 ──未命中──> 飞牛 /sys/img
```

浏览器只看到本站 URL；重复请求由本地缓存、ETag、浏览器或 CDN 消化。详细设计见 [docs/architecture.md](docs/architecture.md)。

## Docker Compose 部署

### 1. 准备目录和环境变量

```bash
mkdir today-random-movie && cd today-random-movie
curl -O https://raw.githubusercontent.com/Stylle/today-random-movie-web/main/docker-compose.hub.yml
cp docker-compose.hub.yml docker-compose.yml
```

创建 `.env`，至少填写一条高熵初始化令牌：

```dotenv
SETUP_TOKEN=请替换为至少32位随机字符串
TZ=Asia/Shanghai
PUBLIC_BASE_URL=https://movie.example.com
TRUSTED_PROXIES=172.16.0.0/12
```

### 2. 启动

```bash
docker compose pull
docker compose up -d
docker compose logs -f movie
```

默认仅监听宿主机 `127.0.0.1:8080`。打开页面后输入 `.env` 中的 `SETUP_TOKEN`，创建首个管理员，再依次完成：

1. “管理设置 → 连接与同步”保存飞牛地址和账号。
2. 测试连接后执行第一次同步。
3. 在“Webhook”保存并测试推送目标。
4. 在“自动推送”创建每日/每周/每月规则。

若确实要直接暴露端口，可在 `.env` 设置 `BIND_ADDRESS=0.0.0.0`，但更推荐反向代理 HTTPS。

## Caddy 反向代理示例

```caddyfile
movie.example.com {
    reverse_proxy 127.0.0.1:8080
    encode zstd gzip
}
```

`TRUSTED_PROXIES` 只填写实际代理容器或主机网段。不要写 `0.0.0.0/0`。

## 飞牛连接说明

- 地址可填 `http://192.168.1.20:32587/v`；应用会定位 `/v/api/v1`。
- 默认只允许私网/回环飞牛目标。若飞牛确实位于公网地址，显式设置 `ALLOW_PUBLIC_FNOS=true`。
- 自签名 HTTPS 可在管理页单独启用；只应对可信 NAS 使用。
- 凭据使用 `/data/secret.key` 做 AES-256-GCM 加密后写入 SQLite，密钥和数据库必须一起备份。
- “允许访客获取播放地址”默认关闭，因为返回值通常是访客不可达的内网 URL。

## Webhook 模板

留空正文模板时会发送包含 `title`、`text`、`event`、`time`、`site`、影片字段的 JSON。自定义模板可使用：

```json
{
  "title": "{{title}}",
  "text": "{{text}}",
  "movie": "{{movie}}",
  "poster": "{{poster_url}}"
}
```

可用变量：`title`、`text`、`event`、`time`、`site`、`movie`、`year`、`genres`、`rating`、`overview`、`poster_url`、`fields`。`fields` 在 JSON 模板中可不加引号以嵌入对象。

计划使用每条规则自己的 IANA 时区和 `HH:MM`。调度器会在对应周期先写入唯一执行键，再执行一次推送，容器重启或同一分钟多次检查都不会重复发送。

## 更新与回滚

```bash
docker compose pull
docker compose up -d
```

发布同时提供 `latest`、版本号和 Git 短哈希标签。生产环境建议锁定版本号：

```yaml
image: stylle/today-random-movie-web:v0.1.0
```

回滚只需改回旧标签并重新 `docker compose up -d`。升级前先备份 `data/`。

## 备份

唯一需要持久化的是 `./data:/data`：

- `app.db`：配置、会话、影片索引、规则和历史。
- `secret.key`：飞牛凭据加密密钥。
- `posters/`：可删除并自动重建的封面缓存。

数据库和 `secret.key` 必须作为同一个备份集。不要把它们提交到 Git。

## 本地开发

要求 Node.js 24+ 与 Go 1.26+。

```bash
cd frontend
npm install
npm run dev
```

另一个终端：

```bash
cd server
go run .
```

生产构建与测试：

```bash
cd frontend && npm ci && npm run build
cd ../server && go test ./...
```

## 主要 API

| 方法 | 路径 | 说明 |
| --- | --- | --- |
| `GET` | `/api/health` | 健康检查 |
| `GET` | `/api/public/config` | 公开站点信息 |
| `GET` | `/api/movies/options` | 筛选选项 |
| `GET` | `/api/movies` | 分页影片库 |
| `POST` | `/api/random` | 按筛选条件随机 |
| `GET` | `/api/posters/{id}` | 封面代理/缓存 |
| `POST` | `/api/auth/login` | 管理员登录 |
| `POST` | `/api/admin/sync` | 启动媒体库同步 |
| `PUT` | `/api/admin/webhook` | Webhook 配置 |
| `POST` | `/api/admin/schedules` | 新建自动推送规则 |

## 安全注意事项

上线前请阅读 [SECURITY.md](SECURITY.md)。公网部署的最小要求是 HTTPS、长随机初始化令牌、准确的可信代理范围、关闭公开播放，以及不要公开 `/data`。

## 项目结构

```text
frontend/              Vue 3 UI
server/                Go API、飞牛适配器、调度与 SQLite
docs/architecture.md   架构与安全边界
docker-compose.yml     本地构建部署
docker-compose.hub.yml Docker Hub 镜像部署
.github/workflows/     测试与多架构发布
```

## License

[MIT](LICENSE)
