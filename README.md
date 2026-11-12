# 今日想看

飞牛影视媒体库随机选片器，用一个轻量网页决定今日想看。适合 NAS、本机电脑和小型 VPS，所有业务页面登录后使用。

保留功能：

- 飞牛连接、同步和封面缓存
- 随机选片、筛选和影片库
- Webhook
- Telegram 机器人：`getmovie` 随机，`meau` / `menu` 打开分类、年份与观看范围菜单
- 每日、每周、每月自动推送
- 每日或每周自动同步新影片与已观看状态
- 运行记录

## Docker Compose

```yaml
services:
  movie:
    image: stylle/today-random-movie-web:v0.5.1
    container_name: today-random-movie
    restart: always
    environment:
      - USER=admin # 首次初始化用户名；之后修改环境变量不会覆盖账号
      - PASSWORD=change-this-password # 必改：至少 8 字符；仅首次创建
    ports:
      - "8080:8080" # 左侧访问端口可改，右侧不改
    volumes:
      - ./data:/data # 必须保留；左侧可改为宿主机专用目录
```

首次启动时，`USER` 和 `PASSWORD` 创建管理员。之后修改环境变量不会覆盖已有账号。

启动：

```bash
docker compose pull
docker compose up -d
```

访问 `http://NAS-IP:8080`，登录后在“管理设置”中保存飞牛连接并同步。定时同步可在“连接与同步”页启用；年份筛选支持起止范围。

无需 `.env`、setup-token 或域名变量。端口和数据挂载必须保留；不要把实际部署配置、密码或数据库上传 Git。

## Telegram 选片

管理设置 → Telegram 机器人：填写独立的 Bot Token，向自己的机器人发送 `/start`，再点击「检测连接 / 识别私聊 ID」。核对私聊 ID 后启用并保存；默认关闭，无需新增环境变量或公网回调。

- `getmovie` / `/getmovie`：按当前范围随机影片，返回摘要及可用的飞牛播放链接。
- `meau` / `menu` / `/menu`：打开按钮菜单，设置媒体库（多选）、类型、地区、起止年份和排除已观看；支持分页。
- 初始为全部分类、排除已观看。后台「默认随机范围」供网站、TG 默认模式和新建推送规则共用；TG 自定义范围单独保存，点击「恢复后台默认」重新跟随，已有推送规则不受影响。

只响应绑定的私聊，不开放给其他用户或群聊。请勿与随手传等应用共用同一个机器人 Token；一个机器人只能由一个实例接收消息。Token 加密保存在 data，网页不回传已存 Token，留空表示不修改。消息和播放链接交给 Telegram 保存，机器人聊天不是端到端加密；播放仍需设备能访问飞牛并登录。

[部署与故障排查](docs/guide.md) · [镜像版 Compose](docker-compose.hub.yml) · [发布说明](docs/release.md)

## 重置管理员

```bash
docker exec -e USER=newadmin -e PASSWORD=new-password today-random-movie /app/today-random-movie admin-sync
```

执行后旧会话全部退出，新账号立即生效。

## 数据

`/data` 包含：

- `app.db`：账号、配置、影片索引、推送规则和记录
- `secret.key`：飞牛账号密码加密密钥
- `posters/`：封面缓存

备份整个 `/data` 即可。飞牛账号密码使用 AES-256-GCM 加密保存。

## 更新

```bash
docker compose pull
docker compose up -d
```

支持 `linux/amd64` 和 `linux/arm64`。

## 本地开发

```bash
cd frontend
npm install
npm run build

cd ../server
go test ./...
```

[MIT](LICENSE)
