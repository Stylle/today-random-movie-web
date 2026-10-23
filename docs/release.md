# 发布

- `v0.3.0`：Docker Hub `v0.3.0`、`latest`、`f3fe96b`（amd64/arm64）
- `v0.2.0`：Docker Hub `v0.2.0`、`latest`、`1ab60bb`
- `v0.1.0`：Docker Hub `v0.1.0`、`8c76110`

推送版本标签后，GitHub Actions 自动测试并发布 `linux/amd64`、`linux/arm64`：

```bash
git tag v0.3.0
git push origin v0.3.0
```
