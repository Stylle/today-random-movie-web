# 今日想看 · 架构设计

## 目标

将原 Tkinter 客户端重建为适合公网部署的 Web 应用，同时保留飞牛影视媒体库读取、筛选、随机选片、回看与播放跳转能力。服务端统一代理飞牛 API 和封面，浏览器永远不接触飞牛账号、密码、令牌或内网图片地址。

## 系统边界

```text
浏览器 ── HTTPS ──> Today Random Movie (Go + Vue)
                         │
                         ├── SQLite：配置、会话、影片索引、计划和执行历史
                         ├── data/posters：按需封面缓存
                         ├── FnOS/FnTV：登录、媒体库、影片详情、封面
                         └── Webhook：企业微信/钉钉/飞书/Telegram/通用 HTTP
```

建议在公网入口前放 Caddy、Traefik 或 Nginx，TLS 在反向代理终止。应用本身只监听容器内 `8080`。

## 技术选型

- 后端：Go，单进程 HTTP 服务；静态前端由同一服务托管。
- 前端：Vue 3 + Vite，沿用图床项目的低饱和绿色、半透明面板、卡片和响应式视觉体系。
- 数据：SQLite WAL。所有可变数据都保存在 `/data` 卷。
- 密钥：首次启动生成 `/data/secret.key`；飞牛用户名和密码使用 AES-256-GCM 加密后入库。
- 部署：多阶段 Dockerfile；运行阶段使用非 root 用户；Compose 默认只绑定宿主机 `127.0.0.1:8080`。

## 主要模块

### 飞牛适配器

兼容 `/v/api/v1` 的当前接口，并保留常见字段别名：

- 登录：`/login`、`/user/loginByPassword`
- 媒体库：`/mdb/list`、`/mediadb/list`
- 列表与详情：`/item/list`、`/item/{guid}`
- 封面：`/sys/img/{path}`

请求签名、Cookie、令牌和请求头只存在于后端。同步时详情请求采用有界并发，避免一次刷新打满 NAS。

### 封面传输

`GET /api/posters/{movie-id}` 的处理顺序：

1. 命中本地文件缓存直接返回。
2. 未命中时由后端使用飞牛会话拉取，限制响应大小与超时。
3. 临时文件写完后原子替换，避免并发读到半张图片。
4. 输出 `ETag`、`Last-Modified`、`Cache-Control`，浏览器和 CDN 可复用。

这条链路不把内网 URL 暴露给公网浏览器，也避免 HTTP 内网封面在 HTTPS 页面中被浏览器拦截。

### 身份与公网安全

- 首次初始化需要一次性 `SETUP_TOKEN`；没有默认管理员密码。
- 密码使用 PBKDF2-SHA256（随机盐、高迭代次数）。
- 登录态使用高熵随机 Cookie；数据库只保存令牌哈希；Cookie 为 HttpOnly、SameSite=Lax。
- 修改类接口校验 CSRF 令牌；登录与随机接口有按 IP 限流。
- 仅在 `TRUSTED_PROXIES` 命中时读取转发 IP 头。
- 飞牛目标默认只允许私网/回环地址，阻止云元数据、链路本地、组播和不明重定向。
- 响应设置 CSP、`nosniff`、Referrer-Policy、Permissions-Policy 等安全头。
- 公开访客只能浏览和随机；连接信息、同步、播放内网地址、Webhook 与计划均为管理员能力。

### 定时推送

内置调度器每 30 秒检查一次规则，不依赖宿主机 crontab。规则支持：

- 每日：指定时区和 `HH:MM`
- 每周：指定星期和 `HH:MM`
- 每月：指定 1–28 日和 `HH:MM`

每个周期使用唯一执行键防止重复推送。推送前按该规则保存的筛选条件随机选择一部影片。Webhook 方法、请求头、JSON/Text 正文模板与 wusaqi 的设计保持一致，并支持测试发送、失败记录和手动立即运行。

## 可扩展点

- `MediaProvider` 可扩展 Jellyfin、Plex、Emby，而不改变前端随机流程。
- Webhook 渲染上下文可增加自定义字段；未来可拆分为多个通知目标。
- 当前 SQLite 适合单实例；多实例部署时可替换数据库并引入分布式调度锁。

## 数据与备份

需要备份的唯一目录是 `/data`，其中包含 SQLite、加密密钥和封面缓存。`secret.key` 必须与数据库一起备份，否则已保存的飞牛凭据无法解密。恢复时应保持文件权限，仅允许容器运行用户读取。
