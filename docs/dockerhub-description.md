# 今日想看

飞牛影视媒体库随机选片器。登录后使用，支持定时媒体同步、年份范围筛选、随机选片、Webhook 和每日/每周/每月自动推送。

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

首次启动使用 `USER`、`PASSWORD` 创建管理员，数据和缓存全部保存在 `/data`。

源码与完整说明：https://github.com/Stylle/today-random-movie-web
