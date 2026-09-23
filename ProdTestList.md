# ProdTestList.md — 长耗时 / 外部验收测试清单

> **用途：** 集中记录单次耗时较长、或需要远端与外部环境才能执行完成的验收项，便于安排时段批量处理。
> **状态时间：** 2026-09-23。Build6 Step 1、Step 2 与 Step 3 的本地门禁均已执行完毕（见第一、二、二之二节）；其余待办见第三节。
> **原则：** 本清单只记录"尚未执行/需重跑"的项；已通过的项如实记录命令与结果，绝不以更窄的命令替代原门禁。

---

## 一、Build6 Step 1 门禁：已全部完成（2026-09-23）

Build6 Step 1 规定的四道门禁均已在本机真实执行并通过：

| # | 命令 | 结果 | 备注 |
|---|------|------|------|
| 1 | `go test -race -count=100 ./notifier` | **通过**（`ok 45.358s`） | 100 轮 |
| 2 | `go test -race -count=100 ./webui/api` | **通过**（`ok 41.405s`） | 100 轮 |
| 3 | `go test -race -count=100 -timeout 40m ./syncer` | **通过**（`ok 665.887s`，0 次 `DATA RACE`） | **必须显式放宽 `-timeout`**，见下方说明 |
| 4 | `go test ./... -race`、`go vet ./...`、`git diff --check` | **通过** | 单轮全仓 + 静态检查 |

**关于第 3 项的超时（重要，后续复跑必读）：**

- Go 测试框架的默认包级超时是 **10 分钟**，而 `./syncer` 跑满 100 轮约需 **11 分钟**（单轮约 6.7 秒：既有 sleep/限速用例约 5 秒 + Step 1 新增的重试退避与轮次用例约 2 秒）。
- 首次按原命令（不带 `-timeout`）执行时，运行到 `600.642s` 被框架中断：`panic: test timed out after 10m0s`。完整日志计数显示每轮一次的标记出现 90 次，即 **100 轮中约 90 轮已完成，其中断言失败 0、`WARNING: DATA RACE` 0**，仅最后约 10 轮未执行——那次失败是**框架超时**，不是测试失败。
- 随后以 `-timeout 40m` 重跑同一测试集、同一轮次数，**完整通过**（`ok 665.887s`，exit=0）。未删减任何测试或轮次。
- 复跑命令（仓库根目录）：
  ```bash
  go test -race -count=100 -timeout 40m ./syncer
  ```
- 成功时 `go test` 不回显测试日志（未加 `-v`），日志文件里不会出现每轮标记，这是正常的；判定依据是最终的 `ok` 行与退出码。

---

## 二、Build6 Step 2 门禁：已全部完成（2026-09-23）

Build6 Step 2 规定的自动门禁已在本机真实执行并通过：

| # | 命令 | 结果 | 备注 |
|---|------|------|------|
| 1 | `cd webui/frontend && npm ci && npm run build` | **通过** | vue-tsc + vite 5.4.21，2818 模块 |
| 2 | `go test ./... -race` | **通过** | 根包（进程级用例）4.464s、config 1.551s、syncer 8.143s、webui/api 2.028s |
| 3 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 修改文件 `gofmt -l` 无输出 |
| 4 | `docker compose -f docker-compose.yml.example config --quiet` | **通过** | 渲染确认仅三个部署变量，healthcheck 为 `http://127.0.0.1:60200/api/health` |
| 5 | `docker build -f build/Dockerfile -t fwalizer:build6-step2 .` | **通过** | 三阶段构建，`CGO_ENABLED=0`，无 VERSION 注入 |
| 6 | 真实容器健康检查 | **通过** | 正常容器 `healthy`；进程存活但 HTTP 不可达时 `unhealthy`（无 pgrep fallback）；`WEBUI_PORT=61234` 覆盖时 healthcheck 跟随；`docker stop` 后 `ExitCode=0` |

**本 Step 仍待用户执行：**

| # | 项目 | 动作 | 原因 |
|---|------|------|------|
| 1 | 浏览器人工复核配置导入/导出 | 打开「全局设置」，执行一次导出与导入，确认文案与结果 | Step 2 改动了 `/api/settings` 返回集、导出内容与导出确认文案；未执行真实浏览器交互 |
| 2 | 远端 GitHub Actions 运行 | 推送分支/PR，观察工作流 | 工作流已去掉 build arg；远端结果无法由本地替代 |

---

## 二之二、Build6 Step 3 门禁：已全部完成（2026-09-23）

| # | 命令 / 动作 | 结果 | 备注 |
|---|------------|------|------|
| 1 | `go test -race -count=1 ./webui ./webui/api ./...` | **通过** | 0 次 `DATA RACE`；root 9.494s、webui 2.235s、webui/api 1.777s、syncer 7.921s |
| 2 | `go vet ./...` / `go build ./...` / `git diff --check` | **通过** | 改动文件 `gofmt -l` 无输出 |
| 3 | 真实二进制 SIGTERM / SIGINT | **通过** | `exit=0`；日志顺序 `收到停止信号 → 开始 HTTP 关闭 → 同步引擎停止 → HTTP 关闭完成`；退出后端口不再监听、pidfile 已清理 |
| 4 | 同步轮次在途时发 SIGTERM | **通过** | 1 目标 1 规则（不可达 DNS，重试 5.1s）：`开始同步` → `收到停止信号` → `同步失败(ERROR)` → `同步完成 耗时=5.117s` → `同步引擎停止`，`exit=0` |
| 5 | 真实 `/api/sync/events` 长连接 + SIGTERM | **通过** | 日志出现 `服务器关闭，同步事件 SSE 退出`，信号到退出 0.04s，未触发强制关闭 |
| 6 | HTTP 绑定失败（不可解析主机） | **通过** | `exit=1`，输出 `WebUI 监听失败: … can't assign requested address`，日志无 `开始同步` |
| 7 | `docker build -t fwalizer:build6-step3 .` + 容器 health/stop | **通过** | `healthy`（5s）；`docker stop` 0.19s、`ExitCode=0`；容器/volume 已清理 |

**Step 3 的证据边界（不得扩大解释）：**

- 容器为 0 targets 空库轮次，`docker stop` 只证明 HTTP/进程收尾顺序，**不**证明有真实同步负载时“完成当前轮次”；该语义由第 4 项真实二进制在途轮次证据支撑。
- `EACCES` 权限错误需低端口 + 非 root 才能构造，本轮以 `EADDRNOTAVAIL` 等非占用类错误代表“非 EADDRINUSE 不随机降级”。
- 进程外未制造 listener 被关闭的 Serve 运行错误并断言退出码；该路径由自动测试 `TestServeRuntimeErrorSurfacedToCaller` + `TestShutdownNormalizesServeResult` 覆盖，属证据层差异。

---

## 三、仍待执行的长耗时 / 外部验收（按后续 Step 归属）

| # | 项目 | 命令或动作 | 归属 | 说明 |
|---|------|-----------|------|------|
| 1 | 远端 GitHub Actions 真实运行 | 推送分支/PR（或 tag），观察 `Docker Build & Publish` 工作流，确认 race 测试真实运行且失败会阻止镜像推送 | Step 1（O5-02 收尾）、各 Step | 本地只能证明工作流文件内容与本地命令；远端结果必须在 GitHub 上确认。当前 Issue5 O5-02 保持 ◧ |
| 2 | 前端依赖审计 | `cd webui/frontend && npm audit --audit-level=high` | Step 6 | Step 2 的 `npm ci && npm run build` 已通过；审计仍为 Step 6 范围 |
| 3 | 进程级信号验收（完整） | 真实二进制 SIGTERM/SIGINT、`docker stop`，验证"完成当前轮次再退出"与 SSE 退出 | Step 3 | **已于 2026-09-23 完成**（见二之二节第 3～7 项）；不再作为待办 |
| 4 | 真实外部链路 | 真实云 API 连接测试/扫描/增量写入/精确删除、SMTP 收件箱、各渠道 Webhook、浏览器人工验收 | Step 5、Step 7 | 需用户凭据与环境；不得用 mock 替代后标记通过 |

---

## 四、执行建议

1. **远端 CI 验证**可最早做：本地 Step 1、Step 2 与 Step 3 门禁已全绿，推一个分支或 PR 即可确认 race 命令在 GitHub Actions 上真实运行；这也是 O5-02 转为验收通过的唯一剩余条件。
2. 第二节列出的两项待办（浏览器复核导入导出、远端 CI）建议由用户安排执行。
3. 若未来把 `-count=100` 固化为 CI 门禁，建议单独拆一个 race job 并让 Docker 发布依赖其成功（Build6 / Issue5 O5-02 第 5 条已给出方向），同时注意上文的 `-timeout` 要求。
4. Step 3 的证据边界（容器 0 targets、`EACCES` 不可构造、进程外 Serve 运行错误）已记录在二之二节；若后续 Step 需要更强证据，应在有真实同步负载与真实外部环境时补做。
