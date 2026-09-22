# FWAlizer 功能构建计划（Build5：当前构建方案）

> **文档定位：** 本文档是 FWAlizer 的**当前构建方案**（依据 AGENTS.md §12.1：Build 文档为详细构建方案，非强规则），承接已存档的 [Build1-4](HistoryDocs/)（见 `HistoryDocs/`，仅核查）。
> - 设计记录：[Design4.md](./Design4.md)（当前设计记录；与 AGENTS.md 或用户决策冲突时以用户确认为准）
> - 编码指令：[AGENTS.md](./AGENTS.md)（**唯一强要求**：简单轻量化、不过度防御、内部使用导向、中文注释、log/slog、增量添加+精确删除）
> - 问题追踪：[Issue4.md](./Issue4.md)（当前问题记录）
> - 历史构建与问题记录：见 [HistoryDocs/](./HistoryDocs/)（Build1-4、Issue1-3、Design1-3，均已存档，仅核查）
>
> **执行原则（与 Build1-4 一致）：**
> - 每一步完成后均可编译、可测试。不跳步、不并行多步。
> - AI 执行指令：每次仅执行一个 Step，完成后运行验收命令，确认通过后再进入下一步。
> - **排序原则：先修复后构建、先安全后优化、先依赖后独立**。
> - 每步的新增逻辑必须配套单元测试（用户决策）。

---

## 一、构建进度追踪

| Step | 内容 | 设计依据 | 状态 |
|------|------|---------|------|
| 1 | 仓库身份收束与 Desktop 正式移除 | Design4 §二-17、§二-18 | ✅ 验收通过 |
| 2 | Docker 数据卷非 root 写权限修复 | AGENTS.md §八、Issue4 R4-01 | ✅ 验收通过 |
| 3 | WebUI 可配置监听地址与 Docker 端口映射修复 | AGENTS.md §二、Issue4 R4-02 | ✅ 验收通过 |

> 状态标记：☐ 未开始 / ◧ 进行中 / ✅ 验收通过

---

## 二、构建概要（文件清单总览）

| Step | 涉及文件 | 要点 |
|------|---------|------|
| 1 | `AGENTS.md`、`Design4.md`、`Build5.md`、`README.md`、`.env.example`、`go.mod`、Go 导入、构建与 CI 文件、Desktop 归档文件 | 更新仓库/module 身份；保留运行时兼容标识；移除 Desktop 范围 |
| 2 | `build/Dockerfile`、`README.md`、`Build5.md`、`Issue4.md` | 预创建并授权数据目录；统一 Docker 数据路径；提供旧卷无损迁移命令 |
| 3 | `config/`、`webui/server.go`、`main.go`、Docker 与配置示例、`README.md`、`AGENTS.md`、`Build5.md`、`Issue4.md` | 新增 `WEBUI_HOST`；运行时环境覆盖；Docker 容器内监听所有接口 |

---

## 三、构建顺序依赖图

```
Step 1 仓库身份收束与 Desktop 正式移除
Step 2 Docker 数据卷非 root 写权限修复
Step 3 WebUI 可配置监听地址与 Docker 端口映射修复
后续候选项（Design4 §三）逐项经用户确认后转为后续 Step
```

---

## 四、分步构建计划

### Step 1：仓库身份收束与 Desktop 正式移除

- **目标：** 将仓库与 Go module 身份统一为 `cloudhost-firewall-autoupdater`，删除 Desktop 源码及未来开发计划。
- **兼容边界：** 保留 `FWAlizer` 产品名、`fwalizer` 二进制、`FWALIZER_*` 环境变量、原数据目录、pidfile、配置导出文件名及 `ghcr.io/alcaprophet/fwalizer` 镜像名。
- **历史边界：** `HistoryDocs/` 保留当时事实，不批量改写历史代码片段或 Desktop 记录。
- **验收命令：**
  - `npm run build`（`webui/frontend/`）
  - `go test ./... -race`
  - `go vet ./...`
  - `go build -ldflags="-X github.com/alcaprophet/cloudhost-firewall-autoupdater/version.Version=dev" ./...`
  - 活跃文件旧 module、旧仓库 URL 及已移除 Desktop 实现引用静态检查
  - `git diff --check`
- **验收结果（2026-09-21）：** 前端生产构建、`go test ./... -race`、`go vet ./...`、新 module 路径版本注入编译、静态残留检查与 `git diff --check` 全部通过；GitHub 仓库已重命名，本地 `origin` 已更新并验证可访问。

### Step 2：Docker 数据卷非 root 写权限修复

- **目标：** 修复 WebUI 模式挂载 Docker 命名卷后，`appuser` 无法写入 `/app/data/fwalizer.pid` 与 SQLite 文件、导致容器持续重启的问题。
- **实现边界：** 最终业务进程继续以 `appuser` 运行；不通过 root 常驻、放宽全局权限或删除旧卷绕过问题。
- **兼容处理：** 镜像同时预创建 `/app/data` 与历史默认目录 `/home/appuser/.config/fwalizer`；README 统一推荐 `/app/data`，并提供旧命名卷的一次性无损 `chown` 命令。
- **验收命令：**
  - `docker build -f build/Dockerfile --build-arg VERSION=local -t fwalizer:permission-test .`
  - 使用全新命名卷启动后，检查容器用户、目录所有权、pidfile、SQLite 数据库与健康状态
  - 使用预置为 root 所有的命名卷复现旧卷问题，执行文档迁移命令后再次验证启动
  - `go test ./...`
  - `git diff --check`
- **验收结果（2026-09-21）：** Docker 镜像构建通过；全新命名卷下业务进程为 `appuser`（UID/GID 1000），`/app/data`、pidfile 与 SQLite 数据库均归该用户所有，容器健康；root 所有的旧卷可稳定复现原权限错误，执行无损迁移后保留原文件并健康启动；Compose 配置校验、前端生产构建、`go test ./...`、`go vet ./...` 与 `git diff --check` 通过。

### Step 3：WebUI 可配置监听地址与 Docker 端口映射修复

- **目标：** 保留本机运行默认仅回环访问，同时使 Docker 端口映射、主机 IP 和独立反向代理可访问 WebUI。
- **实现边界：** 新增 `WEBUI_HOST`，默认 `127.0.0.1`；仅在用户或 Docker 示例显式设置 `0.0.0.0` 时对外监听；不引入新 HTTP 框架或应用层网络访问控制。
- **验收命令：**
  - `go test ./... -race`
  - `go vet ./...`
  - `docker compose -f docker-compose.yml.example config --quiet`
  - 构建 Docker 镜像，以 `WEBUI_HOST=0.0.0.0` 和宿主机端口映射启动，从宿主机请求 `/api/health`
  - 分别以默认值和 `WEBUI_HOST=0.0.0.0` 启动真实进程，检查监听地址并请求 `/api/health`
  - `git diff --check`
- **验收结果（2026-09-21）：** 默认运行时仅监听 `127.0.0.1`；设置 `WEBUI_HOST=0.0.0.0` 后监听所有接口，并已通过实际局域网 IP 请求健康端点；Docker 镜像构建后通过宿主机端口映射成功请求容器健康端点；race 测试、前端生产构建、`go vet ./...`、Compose 配置校验与 `git diff --check` 通过。

---

## 五、候选构建项（待用户决策，逐项转 Step）

| # | 候选 | 说明 | 来源 |
|---|------|------|------|
| 1 | DNS 失败写入历史记录 | `EventDNSFailed` 落库（`result=failed`），提升故障可见性；需 `main.go` 增加订阅 | Design4 §三-1 |
| 2 | 日志页「暂停输出」按钮 | 高日志量时前端暂停渲染 | Design4 §三-2 |
| 3 | 主题设置项加入全局设置页 | 复用 `theme` 键，双入口管理 | Design4 §三-3 |
| 4 | 运行测试「上次执行」持久化 | 刷新后保留结果（当前内存态） | Design4 §三-4 |

> 候选转 Step 流程：用户确认 → 在本文件追加 Step（含目标/前置/参考代码/验收命令）→ 按序执行。

---

## 六、变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.0 | 2026-08-02 | 初始版本：作为当前构建方案（承接已存档 Build1-4），候选构建项见第五节 |
| v1.1 | 2026-09-21 | Step 1 验收通过：仓库身份收束与 Desktop 正式移除 |
| v1.2 | 2026-09-21 | Step 2 验收通过：修复 Docker 数据卷非 root 写权限问题 |
| v1.3 | 2026-09-21 | Step 3 验收通过：WebUI 监听地址可配置，Docker 端口映射可访问 |
