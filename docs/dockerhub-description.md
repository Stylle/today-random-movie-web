# 今日想看

飞牛影视 / FnTV 私有媒体库的 Web 随机选片器。

- Go + Vue 单镜像，SQLite，无外部数据库
- 服务端封面代理与缓存，解决公网 HTTPS 到内网图片的传输问题
- 类型、地区、年份、媒体库、已观看筛选
- 通用 Webhook 与每日/每周/每月自动推送
- 管理员登录、CSRF、限流、加密凭据、非 root 容器
- `linux/amd64` 与 `linux/arm64`

快速启动：

```yaml
services:
  movie:
    image: stylle/today-random-movie-web:latest
    restart: unless-stopped
    environment:
      TZ: Asia/Shanghai
      SETUP_TOKEN: replace-with-a-long-random-value
      PUBLIC_BASE_URL: https://movie.example.com
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - ./data:/data
```

完整说明、反向代理、安全与备份文档见 GitHub：
https://github.com/Stylle/today-random-movie-web
