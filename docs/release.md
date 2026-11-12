# 发布

- `v0.5.1`：新增可开关 Telegram 指令选片与分页范围菜单；统一后台默认范围，默认排除已观看；Token 加密保存、私聊白名单、接收去重与响应重试。
- `v0.5.0`：标签误指向旧提交，构建已取消；请使用 `v0.5.1` 或更新版本。

- `v0.4.0`：Docker Hub `v0.4.0`、`latest`、`16861e1`（amd64/arm64）
- `v0.3.0`：Docker Hub `v0.3.0`、`latest`、`f3fe96b`（amd64/arm64）
- `v0.2.0`：Docker Hub `v0.2.0`、`latest`、`1ab60bb`
- `v0.1.0`：Docker Hub `v0.1.0`、`8c76110`

推送版本标签后，GitHub Actions 自动测试并发布 `linux/amd64`、`linux/arm64`：

```bash
git tag v0.5.1
git push origin v0.5.1
```
