# 今日想看

飞牛影视媒体库随机选片器。所有页面登录后使用。

保留功能：

- 飞牛连接、同步和封面缓存
- 随机选片、筛选和影片库
- Webhook
- 每日、每周、每月自动推送
- 每日或每周自动同步新影片与已观看状态
- 运行记录

## Docker Compose

```yaml
services:
  movie:
    image: stylle/today-random-movie-web:v0.4.0
    container_name: today-random-movie
    restart: always
    environment:
      - USER=admin
      - PASSWORD=change-this-password
    ports:
      - "8080:8080"
    volumes:
      - /vol2/1000/03_Docker_Data/today-random-movie/data:/data
```

首次启动时，`USER` 和 `PASSWORD` 创建管理员。之后修改环境变量不会覆盖已有账号。

启动：

```bash
docker compose pull
docker compose up -d
```

访问 `http://NAS-IP:8080`，登录后在“管理设置”中保存飞牛连接并同步。定时同步可在“连接与同步”页启用；年份筛选支持起止范围。

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
