# ProdTestList.md — 长耗时 / 外部验收测试清单

> **用途：** 集中记录单次耗时较长、或需要远端与外部环境才能执行完成的验收项，便于安排时段批量处理。
> **状态时间：** 2026-09-23。Build6 Step 1 的本地门禁已全部执行完毕（见第一节）；其余待办见第二节。
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

## 二、仍待执行的长耗时 / 外部验收（按后续 Step 归属）

| # | 项目 | 命令或动作 | 归属 | 说明 |
|---|------|-----------|------|------|
| 1 | 远端 GitHub Actions 真实运行 | 推送分支/PR（或 tag），观察 `Docker Build & Publish` 工作流，确认 race 测试真实运行且失败会阻止镜像推送 | Step 1（O5-02 收尾）、各 Step | 本地只能证明工作流文件内容与本地命令；远端结果必须在 GitHub 上确认。当前 Issue5 O5-02 保持 ◧ |
| 2 | 前端构建与依赖审计 | `cd webui/frontend && npm ci && npm run build && npm audit --audit-level=high` | Step 2、Step 6 | 耗时较长；本机 `~/.npm` 曾出现 root 所有文件，需临时 cache 绕过（非仓库问题） |
| 3 | Compose 配置校验 | `docker compose -f docker-compose.yml.example config --quiet` | Step 2、Step 3 | 需 Docker CLI |
| 4 | Docker 镜像构建 | `docker build -f build/Dockerfile -t fwalizer:build6 .` | Step 2、Step 3、Step 6 | 需 Docker 守护进程，数分钟；Step 2 会改动 Dockerfile（去 VERSION/ldflags、健康检查去 pgrep） |
| 5 | 进程级信号验收 | 真实二进制 SIGTERM/SIGINT、`docker stop`，验证"完成当前轮次再退出"与 SSE 退出 | Step 3 | 属进程级证据，不能由单元测试替代 |
| 6 | 真实外部链路 | 真实云 API 连接测试/扫描/增量写入/精确删除、SMTP 收件箱、各渠道 Webhook、浏览器人工验收 | Step 5、Step 7 | 需用户凭据与环境；不得用 mock 替代后标记通过 |

---

## 三、执行建议

1. **远端 CI 验证**可最早做：本地 Step 1 门禁已全绿，推一个分支或 PR 即可确认 race 命令在 GitHub Actions 上真实运行；这也是 O5-02 转为验收通过的唯一剩余条件。
2. 第 2～6 项与对应 Step 强相关，建议在实施该 Step 时一并执行，避免重复构建与重复等待。
3. 若未来把 `-count=100` 固化为 CI 门禁，建议单独拆一个 race job 并让 Docker 发布依赖其成功（Build6 / Issue5 O5-02 第 5 条已给出方向），同时注意上文的 `-timeout` 要求。
