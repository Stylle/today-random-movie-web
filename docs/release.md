# 维护者发布流程

## 已发布版本

- `v0.2.0`：已发布 `stylle/today-random-movie-web:v0.2.0`、`latest` 和 `1ab60bb`
- v0.2.0 平台镜像：`linux/amd64`（`sha256:ff15ece85b31...`）、`linux/arm64`（`sha256:48910d5bc2ed...`）
- v0.2.0 构建记录：https://github.com/Stylle/today-random-movie-web/actions/runs/39932267475
- `v0.1.0`：已发布 `stylle/today-random-movie-web:v0.1.0`、`latest` 和 `8c76110`
- 平台：`linux/amd64`、`linux/arm64`
- 多架构清单摘要：`sha256:de31355c1373e0586f916344f11713eba2978b9831b5aca1b70eb2d54efd4735`
- 构建记录：https://github.com/Stylle/today-random-movie-web/actions/runs/39919095005

## 发布前

```bash
cd frontend && npm ci && npm run build
cd ../server && go test ./... && go vet ./...
cd ..
docker build -t today-random-movie:qa .
SETUP_TOKEN=qa-only docker compose up -d
```

检查健康状态、首次初始化、登录、CSRF、飞牛连接、一次完整同步、封面缓存、Webhook 测试、计划立即运行以及手机端布局。生产数据不得用于破坏性测试。

## GitHub

1. 将通过测试的源码提交到 `main`。
2. 创建语义化版本标签，例如 `v0.2.0`。
3. 推送标签；Release workflow 会先测试，再构建 `linux/amd64`、`linux/arm64` 镜像。

仓库需要设置 Actions secrets：

- `DOCKERHUB_USERNAME`
- `DOCKERHUB_TOKEN`：只授予目标仓库读写权限的 Docker Hub PAT

## 手动 Docker Hub 发布

已完成 `docker login` 且 Buildx 可用时：

```bash
scripts/build-push.sh
```

Windows 使用 `scripts\build-push.bat`。脚本同时发布 Git 短哈希和 `latest`，并附带 provenance 与 SBOM。

推送后必须从 Docker Hub 重新拉取，而不是复用本地镜像，再执行健康检查与初始化烟雾测试。

## 回滚

将 Compose 中的镜像改为上一个版本或 Git 哈希标签，然后重新拉取和启动。镜像回滚不会回滚 `/data`；升级前应单独备份整个数据目录。
