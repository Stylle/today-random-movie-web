# 今日想看

飞牛影视 / FnTV 私有媒体库的 Web 随机选片器。

- Go + Vue 单镜像，SQLite，无外部数据库
- 服务端封面代理与缓存，解决公网 HTTPS 到内网图片的传输问题
- 类型、地区、年份、媒体库、已观看筛选
- 通用 Webhook 与每日/每周/每月自动推送
- 管理员登录、CSRF、限流、加密凭据、只读且无 capabilities 的容器
- 可选 TMDB 中文简介、评分、分类和海报，失败自动回退飞牛
- `linux/amd64` 与 `linux/arm64`

快速启动：

```yaml
services:
  movie:
    image: stylle/today-random-movie-web:latest
    container_name: today-random-movie
    restart: always
    environment:
      - TZ=Asia/Shanghai
      - SETUP_TOKEN=replace-with-a-long-random-value
    ports:
      - "127.0.0.1:8080:8080"
    volumes:
      - /vol2/1000/03_Docker_Data/today-random-movie/data:/data
    read_only: true
    tmpfs:
      - /tmp:size=32m,mode=1777
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
```

只有 `SETUP_TOKEN`（首次初始化）和 `/data` 挂载路径需要修改。创建首个管理员后可删除 `SETUP_TOKEN`。公网地址、飞牛连接、TMDB 和 Webhook 均在管理页面配置；私网反向代理和飞牛内网穿透会自动识别。

完整说明、反向代理、安全与备份文档见 GitHub：
https://github.com/Stylle/today-random-movie-web
