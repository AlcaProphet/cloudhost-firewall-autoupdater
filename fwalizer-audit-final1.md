# FWAlizer 全量代码审核报告（最终版）

| 项 | 内容 |
|---|---|
| 仓库 | `/Users/kyle/Desktop/Repo/cloudhost-firewall-autoupdater` |
| 审计快照分支 / HEAD | `main` / `11918fb945fe9dfe2a86ead5bc833b14dd156a68` |
| 审计快照开始前工作树 | **干净**（0 tracked 改动、0 非忽略未跟踪文件）；这是历史审计快照，不表示当前工作树状态 |
| 历史复核基线（2026-09-30 批次） | `main` / `d6d208edd86187e1795318ed555e8e96c5219df9`；该批次复核开始时 `main == origin/main`、工作树干净（仅 `webui/frontend/dist/`、`webui/frontend/node_modules/` 为既有 ignored 产物）。**该行是历史批次记录，不代表当前 HEAD；当前状态见下方「最近实施基线（2026-10-03 P3-12 文件权限）」** |
| 审核性质 | 原始全量代码审核为**只读**；2026-09-30 批次只静态核验既有修复并更新本文与 `Issue7.md`，未修改代码，未运行构建、测试或格式化（属该批次记录） |
| 审核方式 | 8 路并行子代理分模块审核 + 主代理亲自覆盖超时范围 + 判别性探针独立复现 + 交叉复核裁决 |
| 审核范围（审计快照值） | 快照 `11918fb` 口径：233 个 tracked 文件；**53/53 生产 Go 文件**（10,890 行）；62 个测试文件（20,457 行）；18 个前端源文件（2,212 行）；6 个构建/部署/CI 文件；8 份合同文档。**当前值为 244 tracked / 55 生产 Go（12,606 行）/ 69 测试文件（24,896 行）/ 18 前端源（2,361 行）/ 同样 6 个构建文件与 8 份合同文档**（新增 `provider/plan.go`、`syncer/target.go`） |
| 报告版本 | final1（已剔除全部被驳回/误报项，并纳入用户 7 项决策） |

> **阅读规则（2026-09-30 第二轮更新）**：本报告同时保留“审计快照事实”“后续状态补记”和“定案前历史分析”。当前强约束以 [AGENTS.md](./AGENTS.md) 为准；P1-01 当前设计与实施证据入口以 [Issue7.md](./Issue7.md) 为准；[Design5.md](./Design5.md) 记录当前设计方向。本报告用于保存审计证据与整理剩余问题，不建立第二套实施合同。正文各独立 finding 中写作“当前基线 `34aa9b8`”“当前基线 `d6d208e`”的段落均是相应批次的历史快照，不是现时 HEAD；**现时基线一律以下方「最近实施基线（2026-10-03 P3-12 文件权限）」小节为准**。源码、测试与 `AGENTS.md` 的行号均属引用时快照，后续定位应同时使用 finding ID、符号名与测试名；执行改动后行号会整体位移，请按符号检索而非按行号定位。

### 最近实施基线（2026-10-03 P3-12 文件权限）

- **实施前基线**：`main / 99f3fe2a2f17f096492f19908b4691bccddd2db9`，本地 `origin/main / c08f2e18985f1edca2c8ad01641531afae745c7a`，ahead 3；工作树与暂存区干净，前序 P3-10 已提交为 `99f3fe2`。本轮未 fetch、未提交、未推送，本地跟踪引用不保证远端现时状态。
- **授权与范围**：用户确认本聊天研究推荐的细化 A，并授权正式实施及文档同步。生产固定 `run.go`、`config/deployment.go`、`config/store.go`、`config/pidfile.go`；两份新增权限/进程回归、两份对应签名/部署测试；本文、AGENTS、README，共 11 文件。不改 flock 算法、锁文件保留、SQLite schema/事务/DSN、Provider/DNS/同步/告警/HTTP/前端或依赖，不合并 P3-13/P3-14。
- **正式门禁**：macOS Go `1.25.0` 下新回归及相关 pidfile/P3-10 收尾定向 race 20 轮、真实产品进程新回归 race 20 轮通过；macOS Go `1.25.0` 与 Linux Go `1.25.14` 全量 12 包 race 各一轮、vet/build 通过。六类源码 overlay 与一类打开前辅助文件检查的容器负向控制按目标断言变红；正式 Linux/amd64 产品二进制的五类权限失败、新/旧目录及新/旧/root-owned 卷和恢复均有证据，详见 P3-12 当前实施补记。
- **状态与边界**：P3-12 本地收口，I-07 的 P3-13/P3-14 仍未完成。Linux 全量门禁在 arm64 Go 容器运行；amd64 静态产品在 linux/aarch64 Docker VM 兼容执行，不代表原生 amd64 主机验收。未执行完整发布镜像重建/compose、前端构建、浏览器、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR，单次全量绿色不外推长期稳定；源码/测试/文档尚未提交。

### 前序实施基线（2026-10-03 P3-10 剩余错误处理，历史批次）

> 该批次其后已提交为 `99f3fe2`；以下保留当时实施记录。

- **实施前基线**：`main / f6b3757a49b6db4846dc0126f80b297c919ce9d4`，本地 `origin/main / c08f2e18985f1edca2c8ad01641531afae745c7a`，ahead 2；工作树与暂存区干净。前序 P3-09 已提交 `f6b3757`。本轮未 fetch、未提交、未推送，本地跟踪引用不保证远端现时状态。
- **授权与范围**：用户确认本聊天的研究推荐并授权正式修复及文档同步。固定九文件：生产 `config/store.go`、`webui/api/logstream.go`、`webui/api/sync.go`、`webui/server.go`；新增 Store/SSE/health 三份回归；本文与 AGENTS。不修改 pidfile、权限、MultiHandler、事件/前端协议、健康模型、SQLite schema/事务策略、Provider、DNS、目标同步或依赖。
- **正式门禁**：新回归与既有日志格式/回放/缓存控制 `-race -count=20`、外部渲染 writer 故障注入 `-race -count=20`、受影响三包完整 race 已通过；十类正式 overlay 负向控制均由行为断言变红，全量 12 包 race 一轮（含既有真实产品二进制进程回归）、vet/build、受影响 Go 文件 gofmt 与 diff-check 通过。完整结果见 P3-10 当前实施补记，研究候选结果不冒充本轮正式结果。
- **证据边界**：Go `1.26.6 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Go 1.25/Linux、前端构建、P3-10 专门进程级故障验收/浏览器、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。不外推长期稳定或外部通过，源码/测试/文档尚未提交。

### 前序实施基线（2026-10-02 P3-09 SQLite 写事务，历史批次）

> 该批次其后已提交为 `f6b3757`；以下保留当时实施记录。

- **实施前基线**：`main / ba822928e7b5c8d7300df36519185d637a6c36f6`，本地 `origin/main / c08f2e1`，ahead 1；工作树与暂存区干净。前序 P3-07 已提交为 `ba82292`。本轮未 fetch、未提交、未推送，本地跟踪引用不保证远端现时状态。
- **授权与范围**：用户在本聊天确认推荐方案 A 并授权正式修复与同步文档。固定 7 文件：唯一生产文件 `config/store.go`；DSN 测试、新增 `config/store_txlock_test.go`、协调器测试；AGENTS、本文、Issue6。生产仅新增独立 DSN 参数 `_txlock=immediate` 与注释，不改变协调器、schema/API、前端、依赖、连接池上限或 5000ms busy_timeout。
- **正式门禁**：定向新回归与 DSN 结构检查 `-race -count=20`、5 秒入口超时/恢复定向回归、受影响两包完整 race、vet/build 已通过；三类正式 overlay 负向控制均由行为断言变红。全量 12 包 race 一轮、受影响 Go 文件 gofmt 与 diff-check 均通过；单次全量绿色不外推长期稳定，完整结果见下方 P3-09 当前实施补记。
- **证据边界**：Go `1.26.6 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR，不外推长期稳定或外部通过。源码/测试/文档尚未提交。

### 前序实施基线（2026-10-02 P3-07 旧同步流程清理，历史批次）

> 该批次其后已提交为 `ba82292`；以下保留当时实施记录。

- **实施前基线**：`main == origin/main == c08f2e18985f1edca2c8ad01641531afae745c7a`，工作树与暂存区干净。引用研究聊天的 `ahead 4` 已是历史状态；本轮未 fetch、未提交、未推送，本地跟踪引用不保证远端现时状态。P3-06 已提交为 `c08f2e1`。
- **授权与范围**：用户依据引用聊天「研究并修复 P3-07」的结论与推荐授权再次检查、正式修复并同步文档。方案 B 固定 11 文件：`syncer/retry.go` 删除不可达流程；三份旧测试与新 `syncer/target_retry_test.go`；`provider/plan_test.go` 的描述回归；`provider/tc_lighthouse.go` 仅注释；AGENTS、本文、Issue6、Issue7。现生产状态机与 Provider 写入、DNS、健康、API/schema、前端和 SDK 零改动。
- **正式门禁**：12 个新目标链场景及迁移的共享描述回归 `-race -count=20` 通过；六类正式 overlay 负向控制均由对应断言变红；受影响两包 race 一轮、全量 12 包 race 一轮、vet/build、受影响 Go 文件 gofmt 与 diff-check 通过，详见 P3-07 当前实施补记。源码/测试/文档尚未提交。
- **证据边界**：本机 Go `1.26.6 darwin/arm64`，使用既有 ignored 前端 dist；本轮未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，不外推长期稳定或外部验收。

### 前序实施基线（2026-10-02 P3-06 CVM 入站配额，历史批次）

> 该批次其后已提交为 `c08f2e1`；以下保留当时实施记录。

- **实施前基线**：`main / 82ad2dc20cd78664880e12f0beff7e32e8245372`，本地 `origin/main / ac0ee62fe62166641c9cf1ef8e8043231545e577`，ahead 3；工作树与暂存区干净。本轮未 fetch、未提交、未推送；本地跟踪引用不代表远端最新状态。前序 P3-05 已提交为 `82ad2dc`。
- **授权与范围**：用户已在「研究并修复 P3-06」聊天选择并定型 B，本聊天进一步检查候选内容后按明确授权正式实施并回写文档。范围固定 9 文件：唯一生产逻辑 `provider/tc_cvm.go`；`syncer/retry.go` 仅注释；测试 `provider/request_mock_test.go`、`syncer/retry_test.go`、`syncer/target_test.go`；文档本文、AGENTS、Issue7、ProdTestList。I-10 仅本地 P3-06 子项收口，其他事项继续独立追踪。
- **证据边界**：本机 Go `1.26.6 darwin/arm64`；正式门禁、负向控制与外部边界见 P3-06 当前实施补记。未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，不外推长期稳定绿色或外部验收。本轮源码/测试/文档尚未提交。

### 前序实施基线（2026-10-02 P3-05 普通 JSON 响应禁缓存，历史批次）

> 该批次其后已提交为 `82ad2dc`；以下保留当时实施记录。


- **实施前基线**：`main / ebf8f1902e159c467b01ede13630c25244c9444d`，本地 `origin/main / ac0ee62fe62166641c9cf1ef8e8043231545e577`，ahead 2；工作树与暂存区干净。本轮未 fetch、未提交、未推送；本地跟踪引用不代表远端最新状态。前序 P3-04 已提交为 `ebf8f19`。
- **授权与范围**：用户在「研究并优化 P3-05 修复方案」聊天确认本次方案 B，完成五文件范围与仓库外候选准备后授权正式修复及文档回写。生产仅 `webui/api/deps.go`，新增 `webui/api/cachepolicy_test.go` 与 `webui/server_cachepolicy_test.go`，文档仅本文与 AGENTS；I-07 其他事项继续独立追踪。
- **证据边界**：本机 Go `1.26.6 darwin/arm64`；正式门禁与负向控制见 P3-05 当前实施补记。未执行前端构建、产品真实二进制/浏览器、Docker/compose、Go 1.25/Linux、真实云、SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，不外推长期稳定绿色或外部验收。本轮源码/测试/文档尚未提交。

### 前序实施基线（2026-10-02 P3-04 日志 SSE 续传，历史批次）

> 该批次其后已提交为 `ebf8f19`；以下保留当时实施记录。

- **实施前基线**：`main / 88154cd483a3088299e4cb4eec3cb61e936df758`，本地 `origin/main / ac0ee62fe62166641c9cf1ef8e8043231545e577`，ahead 1；工作树与暂存区干净。未 fetch、未提交、未推送；本地跟踪引用不代表远端最新状态。前序 P3-02 已提交为 `88154cd`。
- **授权与范围**：用户确认方案 B、进一步定型并完成仓库外候选验证后，授权正式修复及文档回写。生产仅 `webui/api/logstream.go`、`webui/frontend/src/views/Logs.vue`；后端协议回归、既有 SSE 写失败夹具、前端 Node 测试与脚本入口配套调整；文档为本文、AGENTS 与 ProdTestList。P3-04 已本地修复，源码/测试/本轮文档尚未提交。
- **证据边界**：本机 Go `1.26.6 darwin/arm64`、Node `v26.7.0`；正式门禁与负向控制见 P3-04 实施补记。未执行真实浏览器自动重连、Go 1.25/Linux、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，不外推长期稳定绿色或外部验收。

### 前序实施基线（2026-10-02 P3-02 每轮半开探测，历史批次）

> 该批次其后已提交为 `88154cd`；以下保留当时实施记录。


- **实施前基线**：`main == origin/main == ac0ee62fe62166641c9cf1ef8e8043231545e577`，工作树与暂存区干净。未 fetch、未提交、未推送；本地跟踪引用不代表远端最新状态。
- **授权与范围**：用户确认 B，完成仓库外候选/旧实现对照后授权正式修复与文档回写。生产为 `dns/circuitbreaker.go`、`syncer/dns_round.go`、`syncer/target.go`、`syncer/syncer.go`；测试新增轮次回归并调整两个既有测试文件；文档更新本文、AGENTS 与 Issue7。P3-01 已提交为 `ac0ee62`，本轮 P3-02 尚未提交。
- **证据边界**：本机 Go `1.26.6 darwin/arm64`；本轮门禁见 P3-02 实施补记。未执行 Go 1.25/Linux、前端、浏览器、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。不外推长期稳定绿色或外部验收。

### 前序实施基线（2026-09-30 P3-01 熔断器淘汰，历史批次）

> 该批次其后已提交为 `ac0ee62`；以下保留当时实施记录。原值身份与不改变半开探测的历史边界已由后续 P3-02 授权替代。

- **实施前基线**：`main / 901d642`、本地 `origin/main / 7ff37b1`、ahead 3，工作树与暂存区干净。本轮未 fetch、未提交、未推送；本地跟踪引用不代表远端最新状态。
- **授权与范围**：用户确认细化后的 A 并授权实施。生产改动仅 `dns/circuitbreaker.go` 与 `syncer/state.go`；回归修改两个对应测试文件及既有 API 导入测试夹具；文档回写本文与 AGENTS。实现和门禁见 P3-01 实施补记。
- **证据边界**：Go `1.26.4 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、浏览器、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。不外推全仓长期稳定绿色或外部验收。

### 前序实施基线（2026-09-30 P3-26 / P3-25 资源扫描，历史批次）

> 该批次改动其后已提交为 `901d642`；以下保留当时实施记录。

- **实施前基线**：`main / ec82d10`、本地 `origin/main / 7ff37b1`、ahead 2，工作树干净；本轮未 fetch、未提交、未推送。
- **授权与范围**：用户确认研究及临时副本验证后的推荐方案并授权实施。仅修改 ECS 资源扫描生产路径（`provider/scan.go`），新增 Provider/API 回归并回写本文与 AGENTS 状态。同步防火墙路径、API handler/响应契约、SQLite schema、前端与 SDK 均未改动。P3-25、P3-26 分别保留证据与 finding 编号。
- **本轮证据**：正式新增回归定向 `go test -race ./provider ./webui/api -run 'TestScanECS|TestDecodeECSScanPage' -count=20` 通过；临时副本恢复 HEAD 原生产代码，正式 token 回归及五类结构异常缓存回归均重新变红，证明判别力；原生 nil response/body 转换测试、真实 SDK → API → 临时 SQLite 缓存不变/完整替换/合法空结果清空均已覆盖。全量 race/vet/build/diff-check 结果见 P3-26 门禁补记。
- **证据边界**：Go `1.26.4 darwin/arm64`，不是 Go 1.25 或 Linux 验收；未执行前端构建、浏览器、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 与远端 CI/GHCR。真实零资源响应是否省略集合仍未确认，缺失/null 集合失败是用户确认的保守兼容策略。不得外推长期稳定绿色或外部验收。

### 前序实施基线（2026-09-30 P2-07，历史批次）

- **实施前基线**：`main / 064b794`，本地 `origin/main / 7ff37b1`，ahead 1；工作树与暂存区干净。`064b794` 是既有 P2-06 修复提交，其原「尚未提交」状态已按 Git 更正。本轮未 fetch、未提交、未推送，本地跟踪引用不代表远端最新状态。
- **授权与范围**：用户确认定型方案后授权修复 P2-07 并更新对应文档。生产变更仅在 `Targets.vue` / `Rules.vue`；新增轻量组件回归与 `test:delete` 脚本；更新本文、`ProdTestList.md` 和 README。Go API、协调器、Syncer、Provider、数据库 Schema 与 AGENTS 强要求零改动。
- **本轮证据**：真实页面脚本/模板 20 项回归（含两页各自的确认守卫与列表序号负向控制）、既有告警页 5 项回归、前端 build；`go test -race ./webui/api ./config`、`go vet ./...`、`go build ./...`、`git diff --check` 本地通过。最新前端已重新嵌入临时真实二进制，独立临时 SQLite + 浏览器/API/故障注入联合验收通过，详见 P2-07 实施补记。
- **证据边界**：本轮未执行全量 race、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma、远端 CI/GHCR；本项局部浏览器证据不代表 PT-B7/PT-I7 的其他回归或产品全项通过。下方第二轮核验及其他批次统计保留为历史快照。

### 最近核验基线（2026-09-30 第二轮，历史批次）

| 项 | 值 |
|---|---|
| 分支 / HEAD | `main` / `b84531b10a9dbbf1321b4eb131f8391a6eea0c4a` |
| 工作树 / 暂存区 | 干净（0 tracked 改动、0 非忽略未跟踪文件）；暂存区空；无 stash |
| 本地远端跟踪引用 | `origin/main` = `d6d208edd86187e1795318ed555e8e96c5219df9` |
| 相对 `origin/main` | **ahead 7 / behind 0**（`fd298ef`、`7aaa3f2`、`c23305e`、`7acf303`、`c5cc79d`、`93e0e4b`、`b84531b`） |
| ignored 产物 | `webui/frontend/dist/`、`webui/frontend/node_modules/`（2 项；`fwalizer` 二进制当前不存在） |
| 本轮性质 | 只读真实性核验 + 按用户批准的改进方案回写文档与测试 |
| 本轮实际写入 | 文档：`fwalizer-audit-final1.md`、`AGENTS.md`、`Build7.md`、`Issue7.md`、`Design5.md`、`ProdTestList.md`；测试：新增 `webui/api/alertset_subscription_test.go`、在 `notifier/webhook_response_test.go` 追加 `TestWebhookResponseSlotHeldUntilClose`。**生产代码零改动** |
| 本轮门禁结果 | 本轮门禁（**仅本批次**，不外推长期稳定绿色）：`gofmt -l .`（无输出）、`go vet ./...`（退出码 0）、`go build ./...`（退出码 0）、`git diff --check`（退出码 0）；定向 `go test ./notifier ./webui/api -race -count=1`（12.6s / 16.6s 通过）；全量 12 包 `go test ./... -race -count=1 -timeout=20m` **全部 ok**；新增两个用例各 `-race -count=20` 通过。**仍未执行**：前端 `npm run build`、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma、浏览器、远端 CI/GHCR。 |

> **本地远端跟踪引用不代表远端服务器最新状态**（本轮未执行 `git fetch`）。前序批次基线按时间保留、不删除：`11918fb`（原始审计快照）、`34aa9b8`（2026-09-30 早期研究批次）、`d6d208e`（2026-09-30 复核批次 = 当时 `origin/main`）、`fd298ef`（I-01 实施前，ahead 1）、`7aaa3f2`（I-01 已提交，ahead 2）、`c23305e`（P2-02 核验，ahead 3）、`7acf303`（P2-04，ahead 4）、`c5cc79d`（P3-03，ahead 5）、`93e0e4b`（P2-05，ahead 6）、`b84531b`（P2-05 文档回写，ahead 7）。

---

> **I-01 后续实施补记（2026-09-30）**：用户已授权执行 P1-02/P3-08 修复，基线 `fd298ef`（main ahead 1、开始时干净）。本轮修改 pidfile 源码/测试、README 与本文并取得本地门禁和 Docker 证据。**该批次改动已提交为 `7aaa3f2`（不再处于未提交状态）**；`d6d208e` 与 §14 的“只读复核”描述保留为此前批次历史，当前 I-01 状态见下表与 P1-02 实施补记。

> **I-02 / P3-03 后续实施补记（2026-09-30）**：用户确认补强方案并授权执行；本批次基线 `main / 7acf303`（提交时 ahead 4，开始时干净）。仅修改 `internal/health/push.go`、既有 `push_test.go` 与审计/Build7 记录；P3-03 本地修复与门禁已完成。**P3-03 该批次改动已提交为 `c5cc79d`（不再处于未提交、未推送状态）**。P2-04 已在本轮基线中提交为 `7acf303`，I-02 两项本地修复已收口。旧基线仍为历史复核记录；本轮证据见下方「P3-03 当前实施补记」，不外推真实 Uptime Kuma 或远端 CI/GHCR。

> **I-05 / P2-05 后续实施补记（2026-09-30）**：本批次基线 main / c5cc79d（提交时 ahead 5，开始时干净）。按用户授权采用方案 A 完成 Webhook 响应校验与本地门禁，仅修改 notifier 源码/测试及本文；修复已提交为 `93e0e4b`（该提交时相对 origin/main ahead 6；其后 `b84531b` 仅回写文档，当前 ahead 7）。当前状态见 P2-05 本轮补记；真实 Webhook、产品浏览器和当前改动的远端 CI/GHCR 未执行，头部旧快照仍作历史保留。

## 0. 当前状态、执行索引与 TODOLIST

### 0.1 状态图例

| 标记 | 含义 |
|---|---|
| ✅ | 已修复且有回归证据；保留原 finding 作为正向控制，不再列入待修复项 |
| 🟠 | 产品语义或方案已定案，但代码尚未实施 |
| 🔵 | 已确认的独立问题或已有用户决策，尚无完成证据 |
| ⏳ | 必须由真实环境或人工验收确认，自动化证据不可替代 |
| 📚 | 历史审计材料或旧排序，仅用于追溯，不得作为当前执行入口 |
| 🚫 | 已驳回候选，不得据此实施 |

> finding ID 表示审计严重级别，不表示施工顺序；施工顺序只以本节 TODO 阶段和 Issue7 Step 编号表达。
>
> 本报告引用的 `AGENTS.md` 行号属**引用时快照**（`AGENTS.md` 后续仍会增删行）；定位请优先使用条款名称与关键词，例如「TAG Trim 后必须非空…最多 48 个 Unicode 字符」「配置变更协调器」「所有二次确认使用 `NModal preset="card"`」。

### 0.2 当前问题状态索引

| 范围 | 当前状态 | 当前入口 |
|---|---|---|
| P0-01 | ✅ 已由 `108e528` 修复；原证据与回归必须保留 | Issue7 Step 1 重构 functional key 时保持绿色回归 |
| P1-01 | ✅ 本地主体与 R7-01～R7-07 完整复核项均已实施并提交（R7-05 与文档回写提交为 `d6d208e`）；⏳ 真实云/浏览器/当前 revision 远端 CI 仍未执行 | [Issue7.md](./Issue7.md) §12.3 实施证据、§12.4 已提交补强、§12.5 R7 修复与本地门禁 |
| P1-02、P3-08 | ✅ I-01 已修复并**提交为 `7aaa3f2`**，取得文件锁、真实进程与 Linux/amd64 Docker 回归证据 | 见 P1-02 实施补记；不外推远端 CI/GHCR 或网络文件系统验收 |
| P2-01 | ✅ 已实施（canonical family/协议归一化，IPv6 ICMP 与云端协议别名收敛） | Issue7 Step 1；保留原审计红灯作为正向控制 |
| P2-03 | ✅ 已实施（能力矩阵产出 `unsupported_*`，目标 `partial` 且冻结清理） | Issue7 Step 1；旧“仅 WARN/Dry Run、不计 skipped”决策仅作历史记录 |
| P2-02 | ✅ 已实施；2026-09-30 在 `7aaa3f2` 基线定向 race 连续 3 次通过（每批 ≤100、部分成功与 S2 残留核验）；⏳ 真实 ECS 验收未执行 | Issue7 Step 3；本条当前实施补记；PT-I7-05 |
| P2-08、P2-09 | ✅ 已实施（Dashboard 只消费后端 outcome；Dry Run 以 `target_id` 为 key） | Issue7 Step 4 |
| P2-04 | ✅ 已修复并提交为 `7acf303`；真实 Uptime Kuma 验收未执行 | RuntimeState 先发布再唤醒，判别性用例覆盖两条分支 |
| P3-01 | ✅ 已提交为 `ac0ee62`；淘汰与裁剪继续保留，域名身份由后续 P3-02 统一 | I-08；历史实施补记保留 |
| P3-02 | ✅ 已提交为 `88154cd`；正常 attempt 新解析、已熔断域名每轮协调半开探测、失败按轮计数 | 后续独立授权，实施补记与 Issue7 §12.6 |
| P3-03 | ✅ 已修复并**提交为 `c5cc79d`**；真实 Uptime Kuma 验收未执行 | 非空非法 URL 使用有下限的 timer/Wake/Stop 等待；见 P3-03 实施补记 |
| P2-05 | ✅ 已按推荐方案 A 本地实施；门禁结果见 finding 补记，已提交为 `93e0e4b`；⏳ 真实 Webhook 未验收 | I-05 / P2-05 本轮实施补记 |
| P2-06 | ✅ 已按完善后的方案 A 本地修复并取得组件、浏览器/API 与 SQLite 联合证据；已提交为 `064b794` | I-03 / P2-06 实施补记；不外推 PT-B7-07 全项或外部验收 |
| P2-07 | ✅ 按定型方案本地修复，组件与真实二进制/浏览器/API/临时 SQLite 联合验收通过；已提交为 `ec82d10` | I-04 / P2-07 实施补记；不外推其他页面、真实云或远端验收 |
| P3-06 | ✅ 已按独立授权方案 B 本地修复：入站计数、完整性判断与数组下界；旧“四方向合计不是过度保守”的判断已被官方配额正文更正 | 见 P3-06 当前实施补记；已提交 `c08f2e1`；真实响应/模板统计/提额情况待 PT-I7-03 |
| P3-07 | ✅ 已按 B 删除三个不可达旧函数，有效回归迁到现生产目标链，旧专属测试/夹具已清理；已提交 `ba82292` | I-09 本地收口；12 个目标场景、描述回归、六类负向控制与正式门禁见补记 |
| P3-25（同步路径） | ✅ 已实施（重复/未推进 token 立即 `snapshot_incomplete`，本 attempt 零删除） | Issue7 Step 1 |
| P3-25（资源扫描路径） | ✅ 已本地修复，尚未提交；重复/未推进/环路返回 ErrSnapshotIncomplete，不返回半截资源 | I-19；TestScanECSTokenProgress 与 API 缓存回归 |
| P3-26（新增，资源扫描分页中途空响应） | ✅ 已本地修复，尚未提交；结构异常失败并保留缓存，有效空数组按 token 分页 | 见 P3-26 实施补记；独立于 P3-25 |
| P3-11、P3-22 与 Dry Run 相关 P3-16 | ✅ 已实施（目标级日志上抛写库错误；Dry Run 每目标一次快照；`RunTest.vue` 44px 已修复） | Issue7 Step 4 |
| P3-04 | ✅ 已按 B 本地修复并提交为 `ebf8f19`；日志 ID/游标续传/reset 与前端去重 | I-10；实施补记；真实浏览器待 PT-AUDIT-01 |
| P3-05 | ✅ 已按本次方案 B 实施普通 JSON 响应统一 `no-store`；本地门禁记录见实施补记，已提交为 `82ad2dc` | I-07 仅本子项收口；settings 凭据回显与独立响应边界保持 |
| P3-09 | ✅ 已按 A 本地修复并提交为 `f6b3757`；写事务 immediate，ReadOnly 仍 deferred | I-10 仅本子项收口；正式回归、负向控制与保留边界见 P3-09 当前实施补记 |
| P3-10 | ✅ 已按细化 A 本地修复四类剩余错误处理，正式门禁通过；已提交 `99f3fe2` | I-07 仅本子项；正式回归/安全元数据/故障注入与负向控制见 P3-10 当前实施补记 |
| P3-12 | ✅ 已按细化 A 本地实施目录/DB/WAL/SHM/锁权限迁移与失败即退出；取消默认路径 `.` 回退，尚未提交 | I-07 仅本子项收口；跨平台 Go1.25、真实产品进程、Docker旧卷恢复与负向控制见当前实施补记 |
| P3-16 其余子项 | 🔵 未修复（侧边栏高亮、保存 in-flight 守卫、`theme` 双写、清空扫描误报成功、前端正则过严） | 独立问题队列 I-10 |
| 其余 P3 | 🔵/⏳ 未修复、文档清理或待真实验证 | 按 0.4 的独立队列处理 |

### 0.3 当前唯一串行主线：Issue7

- [x] **Step 0｜文档合同**：已完成；只代表定性与实施合同完成，不代表代码修复。
- [x] **Step 1｜纯规划器与失败先行用例**：已完成（严格 TAG 所有权、唯一 `FunctionalKey`、能力矩阵、目标级纯 planner、快照 revision、ECS token 保护）。
- [x] **Step 2｜目标级先增后验**：已完成（`S0 → Add(S0 版本) → S1 → 覆盖验证`；Add/验证失败时删除调用恒为 0）。
- [x] **Step 3｜四平台条件清理**：已完成（Lighthouse→CVM→SWAS→ECS 串行；安全门 + S1 定位 + S2 强制 + 残留计数）。
- [x] **Step 4｜Dry Run、事件、日志、Dashboard 与健康口径**：主体已实施；R7-02 已提交为 `eab4bea`，R7-03 已提交为 `297ccfe`，R7-04/R7-06/R7-07 已提交为 `b19d271`，R7-05 已按测试-only 边界修复并提交为 `d6d208e`。真实外部验收仍按 Issue7/ProdTestList 独立保留。
- [x] **Step 5｜完整门禁与真实云验收**：本地部分已完成（`go test -race`/vet/build/gofmt/前端/compose/docker build/真实二进制/容器验收与文档闭环）；R7-06/R7-07 修复后记录的两个 GOGC 压力门禁、两包 20 轮 race、全量 race 连续 3 次及静态/前端门禁均通过。**本次只读核验没有重跑这些门禁**；真实云与浏览器仍待人工执行（PT-I7-01～07），当前 revision 的远端 CI/GHCR 仍未执行。

**主线停止条件**：必须逐 Step 实施和验收；任一 Step 未满足 [Issue7.md](./Issue7.md) 的停止条件时，不进入下一 Step。不得把 mock、单测、本地进程或 Docker 结果外推为真实云验收。

### 0.4 独立问题队列（不与 Issue7 主线编号混用）

以下顺序是整理后的 backlog，不表示已授权实施；每次仍应只处理一个问题并保留判别性测试。

- [x] **I-01｜P1-02 + P3-08**：已以 `flock` 替换 PID 判活；残留 PID、真实内核锁屏障、GC、并发启动、SIGTERM/SIGKILL 后复用与 Linux/amd64 持久卷 Docker 回归均本地通过。源码、测试、README 和本文已更新并**提交为 `7aaa3f2`**；P3-12 后续已独立实施权限迁移，见其当前补记。
- [x] **I-02｜P2-04 + P3-03**：两项本地修复已收口。P2-04 已提交为 `7acf303`；P3-03 按独立授权修复非空非法 URL 的无限等待，并限制异常间隔（非正值 60s、正值不足 20s 按 20s）。定向 race、快速用例 20 轮 race、全量 12 包 race 连续 3 次、vet/build/前端 build 与格式/diff-check 均通过（历史执行记录，本轮未重跑）；**P3-03 已提交为 `c5cc79d`**，真实 Uptime Kuma 与远端 CI/GHCR 未执行。
- [x] **I-03｜P2-06**：按用户确认的完善方案 A 本地修复；三态加载、完整响应验证、整体赋值、保存入口与按钮双重守卫、重复提交保护已落地。组件测试与独立临时数据库的浏览器/API 联合验证通过，已提交为 `064b794`；证据见 P2-06。
- [x] **I-04｜P2-07**：目标与规则删除已增加卡片式确认、四阶段状态/对象快照、列表请求顺序保护与焦点恢复；20 项组件回归、前端构建与真实二进制/浏览器/API/临时 SQLite 联合验收通过，尚未提交。详见 P2-07。
- [x] **I-05｜P2-05**：已按推荐方案 A 本地实施三渠道明确成功响应校验、16 KiB 有界读取与安全错误；门禁结果见 P2-05 补记，已提交为 `93e0e4b`；真实 Webhook 仍未验收。
- [ ] **I-06｜P3-23、P3-24**：串行修复 ticker Reset 与 pause/resume 通知合并，不和 Issue7 状态机重构混做。
- [ ] **I-07｜P3-12、P3-14、P3-10、P3-13**：文件权限与 HTTP/error 一致性；P3-05 已按本次方案 B 本地修复并独立收口，见实施补记；P3-11 已由 Issue7 Step 4 修复；P3-10 已按细化 A 本地修复并提交 `99f3fe2`；P3-12 已按细化 A 本地修复并收口，见各自当前实施补记。P3-14/P3-13 仍未修复，I-07 整体保持未完成。
- [ ] **I-08｜P3-01、P3-02、P3-21、P3-15**：P3-01 已提交 `ac0ee62`；P3-02 按后续独立授权 B 已提交 `88154cd`，详见实施补记；P3-21/P3-15 继续独立追踪，不由本批次关闭。
- [x] **I-09｜P3-07**：2026-10-02 按独立授权方案 B 本地收口：再次证明现生产入口不调用旧链，删除 `retrySync` / `retrySyncDetailed` / `truncateDesc`，迁移有效回归并清理过期测试与夹具；正式门禁及六类负向控制通过，已提交 `ba82292`。详见 P3-07 当前实施补记与 Issue6 §7.9 / Issue7 §12.7；独立残留计数观察不由本项关闭。
- [ ] **I-10｜其余独立 P3**：P3-04 已按 B 本地修复并提交 `ebf8f19`，真实浏览器仍待 PT-AUDIT-01；P3-06 已独立按 B 本地修复并提交 `c08f2e1`，真实响应、模板统计与提额情况待 PT-I7-03；P3-09 已按 A 本地修复并提交为 `f6b3757`；P3-16 非 Dry Run 子项、P3-19、P3-20 继续独立追踪，I-10 整体保持未完成。
- [ ] **I-11｜P3-17、P3-18**：仅做文档/注释闭环；不得与业务语义修改混在同一批次。
- [x] **I-12｜Issue7 R7-01（P1）**：已修复并提交为 `b80b1b0`；可重试清理失败耗尽后保持 S1 已覆盖的 success + cleanup_deferred 强语义与每 attempt 重读快照。
- [x] **I-13｜Issue7 R7-02（P2）**：已修复并提交为 `eab4bea`；补全目标事件 `cleanup_deleted`/`duration_ms`/canonical `unsupported`，并以真实 publisher/EventBus/SQLite 整链证明清理 `2/1/1` 落库一致。
- [x] **I-14｜Issue7 R7-03（P2）**：已按用户裁决 A 修复并提交为 `297ccfe`；Dry Run 覆盖所有已配置目标，无适用规则目标只返回未调度空骨架且零 DNS/云 API/planner/限速，正式同步统计口径不变。
- [x] **I-15｜Issue7 R7-04（P3）**：✅ **已修复并提交为 `b19d271`。** 当前使用私有 `cleanupResult{deleted,deferred}` 直接传递清理结果，可信 S2 planner 是最终 `cleanup_deferred` 的唯一来源；幂等 NotFound 不虚增 `deleted/cleanup_deleted`，S2 不可信时继续 `failed` 并保留保守 fallback。当前实现与判别性测试已在 `b19d271`/`d6d208e` 及现时 HEAD `b84531b` 静态复核保留；本次未重跑门禁。以下为修复前缺陷链、固定语义与验收设计的历史记录。

  **修复前证据与缺陷链（历史基线 `34aa9b8`）：** 当时 `syncer/target.go` 先以 S1 的 `plan1.CleanupCandidates` 记录候选，再由 `len(plan1.CleanupCandidates) - cleanupResolved` 间接计算 deferred；`runTargetCleanup` 返回的 `resolved` 来自 `DeleteResult.Resolved`，而错误型幂等 NotFound 可得到零值 `DeleteResult{Deleted: 0, Resolved: 0}`。该分支虽会执行 S2，但当时的 `verifyCleanupResult` 只检查 `plan2.ToAdd`，丢弃 `plan2.CleanupCandidates`，调用方仍按旧 S1 候选减 `Resolved` 计算。因此“1 条候选被并发者先删除、Delete 返回 NotFound、S2 已无候选”会错误记录 `cleanup_candidates=1 / cleanup_deleted=0 / cleanup_deferred=1`，合同要求为 `1 / 0 / 0`。该缺陷已由 `b19d271` 按下述固定语义修复。

  **固定语义与最小修复设计：** `cleanup_candidates` 保留 S1 planner 看到的候选数；`cleanup_deleted` 只计云端明确确认的实际删除数，NotFound 不计入；成功取得并通过验证的 S2 后，`cleanup_deferred` 直接取 `len(plan2.CleanupCandidates)`，包括仍存在但不可删除的 Owned 候选；未执行或未成功取得可信 S2 时，使用 S1 与 Provider 已确认进度的 fallback，不把不完整 planner 结果当最终状态。建议删除私有 `cleanupResolved` 间接推导链，使 `runTargetCleanup` 返回明确的 `deleted` 与 `deferred`（例如私有 `cleanupResult`），并让 `verifyCleanupResult` 返回 S2 最终候选数；`runTargetAttempt` 直接消费该结果，同时保持 NotFound 的 `deleted/cleanup_deleted=0`、S2 Describe/完整性/覆盖验证失败为 `failed`。正常 Delete 成功、PartialDeleteError 或幂等 NotFound 均须按既有安全语义执行必要 S2；不得跳过 S2。

  **受影响文件与符号：** 生产主线集中在 `syncer/target.go` 的 `targetResult`、`syncTarget`、`runTargetAttempt`、`runTargetCleanup`、`verifyCleanupResult`；`syncer/syncer.go` 的整轮汇总和 `webui/api/logwriter.go` 的写库字段读取原则上无需改动，只应继续消费修正后的 `targetResult`。判别性测试涉及 `syncer/target_test.go` 的目标级夹具、`syncer/cleanup_test.go` 的清理场景，必要时扩展 `syncer/round_summary_test.go` 与 `webui/api/logwriter_test.go` 证明事件、整轮汇总和 SQLite 生产链传播一致。不得借本项修改 `provider.DeleteResult` 语义、四平台 Provider 删除实现、`isIdempotentDelete`、`provider.PlanTarget`、OperationalHealth、API/数据库 Schema，或 R7-06/R7-07 的测试夹具与生产超时参数。

  **判别性验收：** 修复前以下当前目标级用例应能暴露缺陷，修复后必须通过：① 单候选 Delete 返回错误型 NotFound、S2 无候选，断言 `1/0/0`、`success`、S2 已执行；② 两候选 NotFound 后 S2 只剩一条，断言 `2/0/1`，证明不是把 NotFound 全部清零；③ 两候选均仍存在，断言 `2/0/2`；④ NotFound 后 S2 Describe 失败或覆盖验证失败，断言 `failed`、发布同步错误，且不把不完整 S2 当最终计数，按既定 fallback 保留未证实残留；⑤ 正常删除、普通删除失败、可重试删除耗尽及 ECS 部分删除的既有正向控制继续成立。还应至少补一条生产链断言，证明目标事件、`EventSyncComplete`/`RoundSummary` 与 `sync_logs` 对 NotFound 后 S2 收敛均记录 `cleanup_candidates=1`、`cleanup_deleted=0`、`cleanup_deferred=0`。本地验收建议先执行 `go test ./syncer ./webui/api -race -count=1`，再按 Issue7 §12.5.4 顺序进行全量 race、vet、build、前端与 diff-check；这些命令本轮未执行，不能预先写成通过。

  **风险、外部依赖与边界：** 主要风险是把“已确认不再存在”误计为实际删除，或把 S2 的原因条目数/可删除数误当残留规则数；必须分别保持 `Deleted`、`Resolved` 与最终 `CleanupCandidates` 的语义。S2 可能发现 S1 之后新出现的 Owned 残留，因此按当前合同允许 `cleanup_deferred` 大于 `cleanup_candidates`，不新增上限约束。该缺陷可由本地 Syncer/Provider 夹具、事件/汇总与 SQLite 生产链完全证明，不依赖真实云账号；真实腾讯云/阿里云的 NotFound 错误形态、四平台分类、并发删除后的 S2 回读，以及 Lighthouse/CVM 版本保护仍是独立的 PT-I7 外部验收，当前均未执行，本地通过不得外推为真实云通过。

  **用户决策记录：** 当前 AGENTS.md、Issue7 §5.4、§5.5、§12.5.1 对本项没有直接冲突，不需要用户裁决。唯一边界选项是：A（推荐）按现合同允许 S2 发现的新 Owned 残留使 `cleanup_deferred > cleanup_candidates`，因为两字段分别来自 S1 与成功 S2；B 新增 `deferred <= candidates` 约束，但会掩盖 S2 的新残留并改变既定字段语义。推荐 A，影响是实现与验收必须断言“最终 S2 候选数”而不能用 S1 候选数作上限；若用户要求 B，应在实施前停止并重新修订 Issue7 §12.5.1、计数合同和测试矩阵。

  **实施边界核对：** `b19d271` 未修改 Provider 的 `DeleteResult.Resolved` 定义、未扩大 `isIdempotentDelete`、未跳过 S2、未把 NotFound 计入 `cleanup_deleted`，也未改变 `success + cleanup_deferred`/OperationalHealth 语义；上述停止条件继续作为回归边界保留。
- [x] **I-16｜Issue7 R7-05（P3）**：✅ **已于 2026-09-30 按测试-only 边界修复并提交为 `d6d208e`。** 失效的数组非 `null` 字符串门禁已替换为结构化 JSON 合同检查；实际 Dry Run 输出未发现生产 `null`，故未修改生产 DTO、planner、API 或前端。

  **修复前证据：** 基线 `syncer/dryrun_test.go:209-233` 的 `TestDryRun_ArraysNeverNull` 字段列表已经包含引号（如 `"results"`），但断言再次拼接 `"`，实际搜索 `""results"":null`，无法匹配合法 JSON 的 `"results":null`。因此即使响应退化为 `{"results":null}`，旧测试也会通过；它同时不检查字段存在、JSON 类型或目标结果内部数组。

  **生产侧正向证据与准确结论：** 当前源码有充分的非空初始化：`syncer/syncer.go:602-617` 的 `emptyDryRunResult` 初始化目标级全部数组，`:630-631` 初始化 `DryRunResponse.Results`/`Warnings`，`:646-653` 对无适用规则目标返回该骨架；`syncer/target.go:455-467` 的 `ruleHosts` 使用 `make`；`provider/plan.go:434-447` 初始化规划结果数组。修复前这些静态初始化与内存级 nil 检查不能证明最终 JSON 形状；本轮新增结构化序列化测试后，三种真实输出的数组非 `null` 自动化证据已成立，且未发现生产缺陷。

  **实际修复：** 仅修改 `syncer/dryrun_test.go`。`requireJSONArray` 与 `validateDryRunArrayJSON` 使用 `map[string]json.RawMessage` 检查顶层 `results`、`warnings` 及每个目标的 `domains`、`desired`、`satisfied_by_owned`、`satisfied_by_external`、`to_add`、`cleanup_candidates`、`cleanup_deferred`、`dns_errors`、`unsupported`、`conflicts`：字段必须存在、非 `null` 且为 JSON array。正向覆盖有适用规则目标、R7-03 无适用规则骨架、零目标/零结果；负向对两个顶层字段和十个目标数组字段逐一注入 `null`、缺失、对象类型，并拒绝 `null` 目标项。结构化测试未发现生产响应为 `null`。

  **受影响文件与明确排除：** 实际只修改 `syncer/dryrun_test.go` 的 imports、`TestDryRun_ArraysNeverNull`、测试 helper、成功形状与负向控制。未修改 `syncer/syncer.go`、`provider/plan.go`、WebUI API、前端 Dry Run 页面、R7-04 清理计数逻辑、R7-06/R7-07 测试夹具或生产超时参数。

  **判别性验收结果：** 三种真实 `DryRun()` JSON 成功形状与逐字段负向控制均通过；`go test ./syncer -race -count=1` 通过。R7-06/R7-07 的两个 GOGC 压力门禁通过；`go test ./syncer -race -count=20 -timeout=20m` 以 892.213s 通过（首次未加 `-timeout` 时因 Go 默认 10m 总超时中止，不记为通过），`go test ./provider -race -count=20 -timeout=20m` 以 42.256s 通过；全量 12 包 race 连续 3 次、vet、build、前端 build 与 diff-check 均通过。

  **风险、外部依赖与范围边界：** 主要风险是继续把无判别力的测试当作数组序列化合同，导致未来 `null`、缺失或错误类型回归无法被门禁发现；反向样本必须保留，且必须使用 `json.RawMessage` 保留结构差异。R7-05 本身可由本地源码与 JSON 测试完全证明，不依赖真实腾讯云/阿里云、浏览器、SMTP、Webhook、Uptime Kuma 或远端 CI/GHCR；但这不改变 P1-01 的整体边界，PT-I7-01～07 及真实外部验收仍未执行，本项本地证据不得外推为真实云或浏览器通过。

  **用户决策记录：** 用户授权按推荐选项 A 实施：仅修复 `syncer/dryrun_test.go`，使用 `json.RawMessage` 做两层结构化数组检查并加入真实 `null`/缺失/非数组负向控制，保持生产 DTO 与序列化逻辑不变。实际测试未发现生产 `null`，因此未进入选项 B 的生产修复边界。

  **停止条件核对：** 已完成结构化成功形状、逐字段负向控制、定向 race 与多轮稳定性门禁；未修改生产 DTO、planner、API、前端、序列化逻辑或其他 R7 项，也未把测试修复写成生产缺陷修复。I-16 可按本地测试证据关闭，但外部验收边界不变。
- [x] **I-17｜Issue7 R7-06（门禁可靠性）**：✅ **已修复并提交为 `b19d271`。** `syncer/retry_test.go` 的 `realHTTPTimeoutError` 现以 mutex 保存全部 accepted `net.Conn`，cleanup 按“关闭 listener → 等待 Accept goroutine → 关闭并清空连接”有界回收；真实 `*url.Error`、`Client.Timeout exceeded while awaiting headers` 与 `isRetryable=true` 断言均保留。提交未修改生产 `isRetryable`，也未把 reset/EOF 扩入重试集合。该项与 R7-07 保持独立测试、独立命令和独立结论；本次只读核验未重跑门禁。
- [x] **I-18｜Issue7 R7-07（门禁可靠性）**：✅ **已修复并提交为 `b19d271`。** `provider/ali_timeout_test.go` 的 `aliBlockingServer` 现同样持有并有界回收全部 accepted `net.Conn`；四条阿里云构造路径、150ms 下限、应用层 timeout 断言以及生产 10s Connect/30s Read 默认值均保留。提交未修改生产 Provider、SDK 调用链或超时配置；本次只读核验未重跑门禁。

  **2026-09-30 实施补记：** I-17/R7-06、I-18/R7-07 与 I-15/R7-04 已提交为 `b19d271`：accepted connections 均持有至 cleanup 并有界回收；私有 `cleanupResult` 直接传递实际删除与最终残留，可信 S2 planner 成为 `cleanup_deferred` 唯一最终来源。I-16/R7-05 随后按测试-only 边界修复并与文档回写提交为 `d6d208e`，结构化数组合同与逐字段负向控制均成立，未修改生产代码。提交中记录的两个 GOGC 压力门禁、两包 `-race -count=20`、全量 race 连续 3 次、vet、build、前端 build、diff-check 均本地通过；本次只读核验未重跑。真实云、浏览器与当前 revision 远端 CI/GHCR 仍未执行。

  **回归边界：** 必须继续保留四条阿里云构造路径、150ms 下限、`assertAliTimeout` 应用层 timeout 断言，以及生产 `newAliOpenAPIConfig` 的 10s `ConnectTimeout` / 30s `ReadTimeout` 正向默认值控制；不得修改生产超时、SDK、请求调用链或超时分类。本项与 R7-06 同属 accepted connection 生命周期根因，但继续保持独立文件、符号、断言与证据，不能合并成单一结论。
- [x] **I-19｜P3-25（资源扫描路径）**：✅ **已本地修复，尚未提交**（2026-09-30，实施前 `ec82d10`）。用户确认 P3-26 研究方案后授权同时加入已验证的 token 保护。`scanAliECS` 维护历史 token 集合，原样比较/传递 token，重复或未推进即返回 `nil, ErrSnapshotIncomplete`，不返回半截结果；不增加页数上限，不修改同步路径、SDK、API 或缓存 schema。

  **独立判别性证据：** `TestScanECSTokenProgress` 覆盖正常两页、`T1 → T1` 与 `T1 → T2 → T1`，分别断言两次正常请求、两次/三次后失败、错误哨兵及 nil 资源。mock 超出预期页后固定返回 400，让缺陷路径必然结束，避免靠超时判定；临时副本恢复 HEAD 原实现后重复/环路回归变红。`TestScanECSCacheIntegrity` 的 token 子项走实际 SDK/API/SQLite，证明 `success:false` 与旧缓存全部记录不变。

  **范围与证据边界：** P3-26 的结构异常与有效空页语义本次经用户单独研究、验证并确认后一起实施，两个 finding 仍分别验收。定向两包回归 race 20 轮通过，全量门禁见 P3-26；真实云异常响应与正常多页兼容未验收。不宣称不断产生全新 token 的异常服务有总时间硬上界，不改生产超时或引入固定页数上限。此前“仅 token 保护”的方案与 `34aa9b8` 基线属于历史设计，已由本次用户确认方案替代。

**I-15～I-19 串行状态：** I-17+I-18、I-15、I-16 已按顺序完成，统一压力/race/vet/build/前端/diff-check 门禁亦已完成；I-19 资源扫描分页已在 2026-09-30 经后续授权本地修复，见上方独立补记。不同 finding 仍须逐项串行，R7-06/R7-07 仅因同根因在同一批次连续处理；本次 R7-05 授权不包含 I-19。

### 0.5 真实外部与人工验收

- [ ] **PT-B7-01～09（9 项）**：仍未执行；真实 SMTP、收件箱、Webhook、Uptime Kuma、浏览器、当前 revision 的远端 CI/GHCR、SWAS Remark 上限均不得写成已通过。PT-B7-02/03 沿用人工免除决定，但仍无真实通过结论。
- [ ] **PT-I7-01～07（7 项）**：Issue7 Step 1～4 主体与 R7-01～R7-07 本地核验项已实施，真实清单仍**未执行**；四云写入/删除安全、异常分页零删除、目标级 Dry Run 与浏览器回归均需真实或人工证据（PT-I7-07 为残留/收敛观察项）。本地通过不能替代真实验收。
- **外部/人工验收登记合计：16 项（PT-B7 9 + PT-I7 7），均不可表述为当前通过。**
- [ ] P3-06 已取得默认入站/出站各 100 条的官方正文并本地修复；真实账号的零规则响应形态、模板统计与提额情况仍待 PT-I7-03。P3-19/P3-20 的真实 SMTP/MTA 表现继续保留为外部不确定性。

---

## 1. 审核结论摘要

| 项 | 结果 |
|---|---|
| **P0** | **1** |
| **P1** | **2** |
| **P2** | **9** |
| **P3** | **25 + 新增 P3-26 = 26**（审计当时发现统计为 25；第二轮核验新增 1 项） |
| **当前状态补记（2026-09-30 第二轮）** | P0-01 已于 `108e528` 修复并加回归；R7-01～R7-07 本地核验项已完成并进入当前提交历史（R7-05 为 `d6d208e`）；P3-11/P3-22 已修复；P3-25 当时仅同步路径已修复；资源扫描路径现已在 P3-26 同批授权下本地修复，见 I-19。**第二轮核验补充**：P1-02/P3-08 已提交 `7aaa3f2`、P3-03 已提交 `c5cc79d`（原"尚未提交"已订正）；P3-06 当时结论订正（该历史判断后已由 2026-10-02 官方正文与 B 实施替代）；P3-16 的 `RunTest.vue` 44px 已修复；P3-17 的 `export_test.go:461` 已修复；P3-21 的 Webhook drain 子项部分已修复、Push 字节上限子项仍未修复；**新增 P3-26**（ECS 资源扫描分页中途空响应 → 静默截断并覆盖缓存）。上述 P0～P3 数量仍是审计当时的发现统计，不等于当前未修复数 |
| 会实际破坏云端防火墙规则的问题 | **有，已实测复现**（P0-01、P1-01、P2-01） |
| ✅/⏳ **P1-01 本地核验项已实施，外部验收未完成** | 2026-09-29 定为“目标级完整期望集 + TAG 所有权 + comment 纯可读 + 先增后验 + 平台化条件删除 + 可接受残留”；Step 0～5 主体提交 `28559ed`，F1/F5 补强提交 `38bdc19`，R7-01～R7-04/R7-06/R7-07 提交于既有修复链，R7-05 与文档回写提交为 `d6d208e`；**真实云/浏览器/当前 revision 远端 CI 仍未执行** |
| ⏳ **未执行的外部/人工验收** | **16 项登记边界：PT-B7-01～09（9 项）+ PT-I7-01～07（7 项）**；其中 PT-B7-02/03 为人工免除但仍无真实通过结论 |
| Goroutine / 连接 / 订阅泄漏 | **未发现** |
| 无界内存 | DNS 熔断器历史域名累积（P3-01）已本地修复；条目数受该快照配置域名数约束，不是固定绝对内存上限 |
| 核心同步静默停止 | **未发现** |
| 明确凭据泄漏 | **未发现**（唯一残余是 P3-12 同机文件权限与 P3-19 的 SMTP 诊断文本回显） |
| 整体质量判断 | 架构与并发设计**优秀**（事务、快照、凭据、生命周期、SSE 五条主线干净，正向控制密度很高）；缺陷集中在**规则身份与端口比较层**（会造成持续删改云端规则）与**清理收尾** |

### 一句话结论

审计当时确认两条会每轮重复删改生产防火墙规则的路径：P0-01 已于 `108e528` 修复；P1-01 Step 1～5 主体已提交为 `28559ed`，F1/F5 补强已提交为 `38bdc19`，R7-01～R7-07 均已完成并进入当前提交历史（R7-05 为 `d6d208e`）；本地核验项已收口，但真实云/浏览器/当前 revision 远端 CI 仍未执行，不能写成外部验收或发布闭环；P3-25 当时只完成同步路径；资源扫描路径本轮已本地修复，见 I-19。当前入口见 [Issue7.md](./Issue7.md) §12.3～§12.5。

> ⚠️ **可立即执行与待决的区分**：
> - **P0-01（阿里云端口 key 不对称）已修复**：提交 `108e528`，回归 `TestDiff_AliyunPortRoundTripConverges`；Issue7 Step 1 的新规划器已保持该绿色回归。
> - **P1-01 本地核验项已实施**：严格 TAG 命名空间、canonical `FunctionalKey`、目标级 planner、先增后验状态机与四平台条件清理主线已落地；核验补强提交为 `38bdc19`，R7-01～R7-07 均已进入当前提交历史（R7-05 为 `d6d208e`）。真实四云/浏览器/当前 revision 远端 CI 仍待执行。
> - **P1-02/P3-08 已按后续用户授权修复并提交为 `7aaa3f2`**：使用非阻塞独占 `flock`，真实进程与 Linux/amd64 Docker 回归通过，详见实施补记；远端 CI/GHCR 未执行。

### 本次审核中被驳回的候选（**请勿据其动手**）

| 候选 | 裁决与依据 |
|---|---|
| "`slog.TextHandler.Handle` 按 level 二次过滤 → WebUI 日志丢行" | **驳回**。Go 源码 `log/slog/handler.go` 中 `TextHandler.Handle` **不含任何 level 判断**（过滤只在 `commonHandler.enabled`）；且 `MultiHandler.Handle`（`app/logutil.go:30`）已按 `h.Enabled(ctx, r.Level)` 逐子 handler 正确门控。不存在双重过滤 |
| "发布顺序与 AGENTS.md 相反" | **驳回（但引出真实缺陷）**。这是审计快照中的历史结论：当时 AGENTS.md:216 原文为"…监督器唤醒 → Uptime Kuma Push 唤醒 → `RuntimeState`"，与当时代码**顺序一致**；该分派误读了文档。真正的缺陷不是"不一致"，而是该顺序本身会引发 P2-04。2026-09-29 Step 0 已把当前强合同修订为先发布 `RuntimeState` 再唤醒，代码已于 2026-09-30 独立修复并提交为 `7acf303`，见 P2-04 实施补记 |
| `SaveAlertEmailTx` / `SaveAlertWebhookTx` / `NormalizeResourceID` / `syncer/ratelimit.go` 无引用（"死代码"） | **驳回**。分别在 `config/store.go:1297`、`:1300`、`config/validate.go:84`、`syncer/syncer.go:813` 有**生产调用**。纯标识符 grep 对"仅包内自用"与"经 HTTP 路由驱动"的符号会产生假阳性 |
| "SPA 深链 404（缺少 history fallback）" | **驳回**。`webui/frontend/src/main.ts:6` 使用 `createWebHashHistory()`，深链与刷新经 URL hash 正常工作 |

### 上一版报告的两处自我更正

1. 我曾给出"P0 = 0"的结论，**该结论错误**——第八路子代理（同步/DNS/Provider，超时后返回）发现了 P0-01，我已用自写探针独立复现。
2. 我曾把"容器陈旧 pidfile 崩溃循环"列为待验证的 **P1**，第一次用 `--volumes-from` 的测试**未能复现**（该测试方法有缺陷，卷未被真正复用）；改用持久命名卷受控重测后**确认成立**，故保留为 P1。

---

## 2. 确认发现

### P0-01｜阿里云 SWAS/ECS 端口比较 key 不对称 → 每轮"删除并重建全部 TCP/UDP 规则"（✅ 已修复）

**审计当时最严重的缺陷，已由主代理独立复现；2026-09-29 已于 `108e528` 修复。**

#### 机制（三段确定性推导，与云端回传形态无关）

1. **期望侧**：`buildDesired`（`provider/common.go:207`）用 `p.ConvertPorts` 生成端口。SWAS（`provider/ali_swas.go:185-192`）与 ECS（`provider/ali_ecs.go:193-200`）对每个端口调用 `portconv.ToSlash`，产出斜杠形态：`"443"→"443/443"`、`"8000-8010"→"8000/8010"`（`internal/portconv/portconv.go:26-35`）。
2. **现有侧**：`GetRules` 把云端端口经 `normalizeSWASPort`（`provider/ali_swas.go:87,196-207`）/`normalizeECSPort`（`provider/ali_ecs.go:87,204-216`）归一化，**凡含 `/` 必被剥离**：`"443/443"→"443"`、`"8000/8010"→"8000-8010"`。
3. **比较层不对称**：`normalizePortForCompare`（`provider/common.go:95-104`）**只**为 `ICMP/ICMPV6` 与 `-1/-1` 做等价归一，其余仅 `ToUpper`。因此期望 key 恒为 `"443/443"`，现有 key 恒为 `"443"`，**永不相等**。

代码注释本身承认了这个不对称（`provider/common.go:93`）：

> 「desired 侧为云厂商格式、existing 侧为归一化格式，**避免 ICMP 规则永不收敛**」

即：作者当年**只为 ICMP 打了补丁，漏掉了斜杠格式本身**。

#### 复现证据（主代理自写探针，经 `go test -overlay` 虚拟映射，仓库零写入）

```
portconv.Parse("443")                     = ["443"]
SWAS.ConvertPorts("443")                  = ["443/443"]      ← 期望侧 Port
normalizeSWASPort("443/443")              = "443"            ← 现有侧 Port
normalizeSWASPort("8000/8010")            = "8000-8010"
normalizePortForCompare("TCP","443/443")  = "443/443"        ← 未归一
normalizePortForCompare("TCP","443")      = "443"
>>> ToAdd=1 ToDelete=1   （期望 0/0 —— 云端规则本已正确）
FAIL
```

范围端口 `8000/8010` 同样为 **1/1**；ICMP 为 **0/0**（这正是"ICMP 被单独打补丁"的直接反证）。

#### 影响面（精确）

**仅 `ali_swas` 与 `ali_ecs`。** Lighthouse（`tc_lighthouse.go:196-206`）与 CVM（`tc_cvm.go:211-213`）的 `ConvertPorts` 不使用 `ToSlash`，key 可收敛。端口为 `ALL` 的 TCP/UDP 规则因两侧都映射为 `ALL` 而收敛。

#### 实际影响

稳态下 `to_add` 与 `to_delete` 包含**同一条规则**（内容相同），因此**不是净规则丢失**，而是：

1. **每轮删除 → 重建**：删除与重建之间存在规则**不存在的窗口**。若重建失败（配额 `FirewallRuleLimitExceed`、限流、网络中断、进程在窗口内被 SIGKILL），该轮结束时规则处于**已删除**状态 → 端口不可达。
2. **写配额翻倍 + 每轮空转**：SWAS 限速 100 次/60 秒，ECS 无明确限制但仍有往返延迟。
3. **可观测性完全失效**：`added`/`deleted` 永久非零，`outcome` 仍为 `success`（`failed==0 && skipped==0`）→ 运行健康**永不告警**，Push 不上报 DOWN，Dry Run 永远显示"全量替换"，用户无法从中发现异常。
4. **真实丢失场景（与 IP 变更叠加）**：IP 集收缩时，旧 IP 规则在 `to_delete`、新 IP 在 `to_add`；执行顺序是**先删后建**（`syncer/retry.go:72-116`），若 `CreateRules` 失败则旧 IP 已删、新 IP 未加 → 该域名的放行被完全拆除（此时 `failed` 会计数并告警）。

#### 为什么现有测试未发现

Diff 用例的 `existing` 传 `nil` 或手写统一格式 fixture，**没有任何 `GetRules→Diff` 往返用例**；`provider/request_mock_test.go` 的 SWAS/ECS 用例只单测 `CreateRules`/`DeleteRules`/`ConvertPorts`，不入 Diff。

#### 最小判别性测试

mock 让 `ListFirewallRules` 回传 `Port="443"`（以及 `"8000/8010"`），断言第二轮 `ToAdd==0 && ToDelete==0`；当前为 1/1。

#### 推荐整改

在**唯一比较归一化点** `normalizePortForCompare` 中把斜杠形态与短横形态统一，`CreateRules` 的线格式保持不变：

```go
// provider/common.go —— normalizePortForCompare
// 追加：斜杠形态归一为与现有侧一致的形态
if strings.Contains(port, "/") {
    parts := strings.SplitN(port, "/", 2)
    if parts[0] == parts[1] {
        return strings.ToUpper(parts[0])                  // "443/443" → "443"
    }
    return strings.ToUpper(parts[0] + "-" + parts[1])     // "8000/8010" → "8000-8010"
}
```

- **是否允许破坏性改动**：否（纯内部比较逻辑，不影响外部契约）
- **是否与 AGENTS.md 冲突**：**是**。实际行为违反 §三「只用**增量**添加 + 精确删除」的意图（实为每轮全量删+建）；不违反"不得使用全量覆盖 API"的字面要求
- **需用户决策**：无

#### 实施补记（2026-09-29）

- 提交：`108e5285fd88cd5a0bbc2dbecee81042091f19fa`。
- 实现：`normalizePortForCompare` 补齐斜杠形态归一化，`443/443 → 443`、`8000/8010 → 8000-8010`，线上 Create 格式不变。
- 回归：`TestDiff_AliyunPortRoundTripConverges` 覆盖 SWAS/ECS 单端口和范围端口往返收敛。
- Issue7 Step 1 会替换更大范围的 functional key/规划路径，因此该回归是必保留的正向控制，不是待重做的红灯修复。

---

### P1-01｜规则身份仅由 `desc` 决定，comment 可为空 → 同目标两条规则**互相删除**并逐轮震荡

**已由主代理独立复现。**

> ✅ **状态：已实施（2026-09-29，Issue7 Step 1～5 本地完成，本地提交 `28559ed`；核验补强见 Issue7 §12.4）。**
>
> 缺陷本身已确认并复现。最终设计不在 description 中编码 host/协议/端口，不强制 comment 唯一或必填，不新增本地—云端映射表，TAG 上限仍为 48。
>
> **定案要点**：TAG 是唯一操作授权，comment 只用于可读；功能身份改为归一化的地址族/CIDR/协议/端口/action；同一目标先汇总完整期望集，再先 Add、重读验证、最后经安全门条件 Delete。详见第 8 节与 [Issue7.md](./Issue7.md)。

#### 机制

`Diff` 只按 `r.Description == desc` 判定归属（`provider/common.go:142-148`），不在 `desiredKeys` 内即 `toDelete`（`provider/common.go:175-182`）。而

```go
desc = truncateDesc(tag.Format(tagStr, rule.Comment), p.CloudType())   // syncer/retry.go:56
```

`tag.Format(tag, "")` 返回 **`"[TAG]"`**（`internal/tag/tag.go:12-14`）。`comment` **允许为空**（`config/validate.go:97`，`webui/api/rules.go` 原样透传），因此两条都留空 comment 的规则 **desc 完全相同**。

#### 复现证据

```
规则A（comment 空，a.example.com）首轮: ToAdd=1
规则B（comment 空，b.example.com）对 A 的既有规则:
    将被删除: cidr=1.1.1.1/32 desc="[auto-dns]"
>>> ToDelete=1   （B 的同步把 A 的规则列入待删除）
FAIL
```

#### 实际影响

同一目标上两条规则（comment 相同、解析 IP 不同）会**互相删除对方**：每轮后处理的规则删掉先处理规则的规则，只保留自己的 IP。稳定状态是"**只有最后一条规则的 IP 被放行，其余域名的放行被持续拆除**"，确定性不可达；每轮 `added`/`deleted` 非零但 `failed==0` → **运行健康不告警**。

**加重因素**：`truncateDesc` 把 Lighthouse 截断到 64 rune、SWAS 截断到 50 rune（`syncer/retry.go:195-209`）→ **长 comment 也会碰撞**（前 64/50 字符相同即可）。

**边界（正确的部分）**：只删除本工具自己的规则，**不触碰非本工具规则**。

#### 为什么现有测试未发现

所有 Diff/同步用例的描述**互不相同**——`TestDiff_DomainIsolation`（`provider/common_test.go:227`）恰恰是用"不同描述"来断言"不删其他域名"，**测试假设与生产默认值方向相反**；`TestRetrySync_EmptyCommentDesc` 只有单条规则。

#### 决策状态：已定案，此前候选均不再是待选实施方案

| 子项 | 状态 | 说明 |
|---|---|---|
| **身份形态** | ✅ **已定案** | description 只承担 TAG 所有权 + comment 可读；个体身份被目标级功能 key/完整期望集取代 |
| **唯一性校验** | ✅ **不引入** | comment 可空、重复和修改；不强制唯一 |
| **长度超限行为** | ✅ **只截断可读部分** | 长度不再影响功能身份；TAG 上限仍为 48 |
| **旧规则迁移** | ✅ **仍然有效** | 用户已确认**无历史兼容负担**，因此任何最终方案都**不需要** orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份形态无关，独立成立 |
| **TAG 上限收紧（48→32）** | 🔶 **已撤回** | 该收紧**仅为配合方案 A** 而提出。`AGENTS.md:179` 未修改，`maxTagRunes` 仍为 **48**（`config/validate.go:34`） |

**📚 历史授权边界（2026-09-29 Step 0 时）：** 当时尚未获得后续代码实施授权，故不得进入 Step 1。该句不代表当前状态；后续授权已在本地完成 Step 1～5 与 R7-01～R7-07 本地核验项。TAG 上限 48 与 Lighthouse/SWAS 描述容量仍会挤压 comment 的可读长度，但不再影响功能身份或 P1-01 已实施的主线。

**跨方案共通的前提**（无论最终选哪个方案都成立）：

- `hasPrefix`/`Parse` 以 `[TAG]` 前缀识别归属的机制**不能取消**（AGENTS §三 要求），因此 `[TAG] ` 前缀的容量开销是固定成本。
- 云端描述字段的上限：Lighthouse **64**（`TencentLighthouseAPIGuide/添加防火墙规则.md:15`）、SWAS **50**（来源存疑，见下）、ECS **512**（`AliyunECSAPIGuide/AuthorizeSecurityGroup.md:128`）、CVM 未声明上限。
- ⚠️ **不确定性**：SWAS 的 `Remark ≤ 50` 仅见于代码注释（`syncer/retry.go:200`），**仓内 API 文档查无出处**（已在 6 个提及 `Remark` 的文件中核查），需真实云确认（含字符/字节口径）。已登记为 **ProdTestList.md PT-B7-09**。
- 🔶 **与 TAG 上限的关系**：现行 `maxTagRunes = 48` 与上述描述字段上限之间**已经**存在紧张关系（TAG 用满 48 时 `[TAG] ` 占 51 runes，仅剩约 13 runes）。这属于同一字段的容量约束，研究本项时应一并考虑；但**本项修复不以收紧 TAG 为前提**，此前提出的 48→32 收紧**已随方案 A 一并撤回**。

---

### P1-02｜陈旧 pidfile 与非原子判活导致启动失败或双实例（容器 PID 1 可形成崩溃循环）

**当前状态：✅ P1-02/P3-08（I-01）已于 2026-09-30 按用户“开始执行修复”授权修复，并提交为 `7aaa3f2`。** 实施基线为 `fd298efde6d2f9b20bf1a78d43288172204c6a34`（提交时 `main` 相对 `origin/main` ahead 1，开始时工作树干净）。下述历史失效链与复现保留用于追溯，不代表当前实现。

#### 历史失效链与影响

启动调用链为 `main → run → LoadDeploymentConfig → MkdirAll → runWebUI → GetPidFilePath → WritePidFile`。旧实现先 `ReadFile → 解析 PID → processExists/Signal(0)`，再 `os.WriteFile` 覆盖写入；正常退出删除文件，异常退出不执行清理。

1. Dockerfile 直接启动应用，应用通常为容器 PID 1；Compose 将 `/app/data` 持久化并使用 `restart: unless-stopped`。SIGKILL/OOM-kill 后残留 `"1\n"`，新实例对自身判活成功并拒绝启动，形成重启循环。
2. 普通 Linux/macOS 部署中，残留 PID 被无关进程复用时同样误拒绝启动，因此整个问题并非 Docker 专有。
3. 两个实例可能同时通过判活检查再覆盖写入，造成 TOCTOU 双实例；任一实例正常退出还可能删除共享文件。

#### 历史实测证据与可信边界

原审计用持久命名卷预置 `/app/data/fwalizer.pid` 内容 `1`，启动旧 `fwalizer:audit` 得到 `state=exited exit=1`，日志「FWAlizer 已在运行 (PID: 1)」。首次 `--volumes-from` 未真正复用卷，不能视为反证；改用持久命名卷后稳定复现。该证据是历史原缺陷复现，与以下修复后验收分开记录。

#### 已实施的最小修复

- `config/pidfile.go` 保持 `WritePidFile(path) (cleanup func(), err error)` 与 `fwalizer.pid` 路径；以 `O_CREATE|O_RDWR, 0600` 打开，先取得锁再截断并写 PID，不在打开时使用 `O_TRUNC`。
- `config/pidfile_unix.go` 保持精确 `linux || darwin`，以标准库 `syscall.Flock(LOCK_EX|LOCK_NB)` 替代 `processExists`；没有新增依赖，没有 PID 判活 fallback。
- `EWOULDBLOCK/EAGAIN` 表示同目录已有持锁者；PID 仅通过同一文件描述符有界读取作诊断，空、损坏或读取失败都不能绕过锁。其他系统错误保留原因。
- cleanup 捕获 `*os.File`，保证运行期间文件对象存活；只 `Close` 释放锁，不删除文件，关闭错误记录 WARN。加锁、截断、写入失败分支均关闭文件。
- `run.go` 接入位置和退出顺序不变；锁仍在打开 SQLite 前取得，覆盖整个应用生命周期。
- 已选择最小权限边界：只保证新建文件请求 `0600`；已有锁文件、数据目录、DB/WAL/SHM 的权限迁移留给 P3-12。
- `README.md` 更新文件锁、残留文件无需清理、禁止运行中删除/替换、升级前停止旧实例及本地文件系统边界。

#### 判别性验收与当前证据

| 层级 | 本轮结果与证明范围 |
|---|---|
| L0 静态 | `processExists`、`Signal(0)` 与无锁覆盖写已从 pidfile 生产代码移除；平台 tag 保持 `linux || darwin` |
| L1 config | 预置 PID 1、当前 PID、空、损坏内容，无持锁均成功；外部内核锁持有时均拒绝且不覆盖；GC 后锁不丢失；释放后保留同一文件且可复用；新建权限 `0600`；不同目录独立；路径错误与 EBADF 保留原因 |
| L1 重复门禁 | `go test ./config -race -run TestPidFile -count=20` 通过；最终定向测试含新增 EBADF 用例的 `GOGC=1 go test ./config -race -run TestPidFile -count=50` 通过 |
| L1 负向控制 | 临时 `go test -overlay` 恢复旧 PID-only 写入算法，运行 `TestPidFileRejectsKernelLock` 的损坏内容子用例，按预期因“竞争者绕过真实内核锁”失败；实际仓库源码未覆盖，临时目录已清理 |
| L2 真实进程 | 顺序拒绝、内核锁屏障、同步释放两个启动者一成一败、预置 PID 1、SIGTERM/SIGKILL 后同目录重启通过；五处旧“文件删除”断言统一改为“文件保留且锁可重新取得” |
| L2 重复门禁 | 锁/重启及相关 SIGTERM/SIGINT/绑定失败/SSE 回归 `-race -count=5` 通过；并发夹具改为唯一 Wait 所有者并有界回收后，`go test . -race -run TestProcessPidFile -count=20 -timeout=10m` 通过 |
| L3 Docker | `docker build --platform linux/amd64 -f build/Dockerfile -t fwalizer:i01-linux-amd64 .` 通过；uid 1000；持久命名卷预置 PID 1 启动 healthy；共享该卷的第二容器 exit=1 且因锁竞争拒绝，原实例仍 healthy；SIGKILL exit=137 后替换容器同卷 healthy；正常 stop 0.158s、exit=0，文件保留；临时容器与卷已清理 |
| L4 工程门禁 | 全仓 12 包 `go test ./... -race -count=3 -timeout=20m` 通过；最终进程夹具收尾调整后再执行全仓 `-race -count=1 -timeout=20m` 亦通过；vet、build、前端 `npm run build` 通过；`CGO_ENABLED=0` 的 Linux/amd64、Darwin/arm64、Darwin/amd64 构建通过；gofmt 与 diff-check 通过 |

全仓三轮门禁开始于最终进程夹具收尾调整之前（生产源码未再改变），其后定向 20 轮与全仓追加一轮针对最终测试版本执行，不将不同版本证据混写。Linux/amd64 容器证明 Linux 运行行为；Darwin 本机进程测试证明 macOS 运行行为，交叉编译本身只证明可构建。

#### 风险与外部边界

- 文件锁文件长期保留，残留 PID 不表示实例仍在运行；运行中不得手工删除或替换文件，否则 inode 改变可能产生两把独立锁。
- 升级前先停止旧 PID-only 实例；混跑旧版本与新版本不保证互斥，不重新引入 PID 判活兼容层。
- 保证范围为 Linux/macOS 本地文件系统与底层为本地文件系统的 Docker 卷，不外推 NFS/网络文件系统；不引入分布式锁。
- 本项不改变 P3-12 的权限迁移待办，也不关闭 P3-10 的其余错误处理待办。Issue7 未修改。
- 未执行真实云、DNS、SMTP、Webhook、Uptime Kuma、浏览器或远端 CI/GHCR；这些链路不属于本项前置，也不能被本地通过替代。改动已提交为 `7aaa3f2`，未推送、未发布。

---

### P2-01｜IPv6+ICMP 规则 key 不对称 → 同款每轮删+建（Lighthouse / CVM）

> **当前状态：✅ 已修复并由 Issue7 Step 1 的 canonical functional key 回归覆盖。** 本节前半保留修复前的审计快照、复现证据与原整改方向；原“推荐整改”不再是待实施计划。

**已由主代理独立复现。**

- **机制**：期望 key 用配置值 `ICMP`（协议改写**只发生在 `CreateRules`**：`tc_lighthouse.go:117-122` 改 `ICMPv6`、`tc_cvm.go:126-131` 改 `ICMPV6`），而 `GetRules` 原样回传云端协议（`tc_lighthouse.go:79-89`、`tc_cvm.go:90`）→ `keyOf`/`keyOfAction` 的 `ToUpper(Protocol)` 不相等。
- **复现证据**：

  | 平台 | 云端回传 Protocol | 结果 |
  |---|---|---|
  | Lighthouse | `ICMPv6` | `ToAdd=1 ToDelete=1` |
  | CVM | `ICMPV6` | `ToAdd=1 ToDelete=1` |
  | 对照：IPv4 ICMP / IPv6 TCP | — | `0/0` |

- **影响**：与 P0-01 同机制（每轮删+重建），但作用面窄（仅 ICMP + IPv6 + 域名有 AAAA 记录）。AGENTS §三 明确 IPv6+ICMP 走 ICMPv6，因此这是**文档要求的能力**而非边缘用法。
- **审计快照中的推荐整改（已完成）**：在比较/功能 key 归一化中，当存在 IPv6 CIDR 时把 `ICMP`/`ICMPv6`/`ICMPV6` 归一到同一别名；线格式不变。
- **判别性测试**：`existing` 传 `Protocol="ICMPv6"`（Lighthouse）/`"ICMPV6"`（CVM）+ `Ipv6CidrBlock`，断言 `ToAdd==0 && ToDelete==0`。
- **是否与 AGENTS.md 冲突**：否

#### 当前实施补记（2026-09-30）

- Issue7 Step 1 已把 address family 纳入 canonical `FunctionalKey`，并对 IPv6 ICMP 与云端 `ICMPv6`/`ICMPV6` 做双向归一化。
- 当前判别性证据为 Issue7 的 planner/key 用例与 `TestDiff_AliyunPortRoundTripConverges` 等既有正向控制；本项不再列入剩余修复队列。

---

### P2-02｜ECS 删除不分批：>100 条 RuleID 塞进单个 `RevokeSecurityGroup`

> **当前状态（2026-09-30）：✅ 已由 Issue7 Step 3 修复，本轮研究确认现有实现无需重复整改；⏳ 真实 ECS 验收未执行。** 以下六项保留修复前审计快照，旧源码行号、逐规则同步单元以及 `failed/unhealthy` 影响描述不代表当前实现；当前行为与证据见下方实施补记。

- **证据**：`provider/ali_ecs.go:163-190` 一次性把全部 `RuleID` 传给 `RevokeSecurityGroup`；而 **create 是分批的**（`:111-126` 使用 `batchRules(rules, 100)`）。`PlatformAPIDocs/AliyunECSAPIGuide/RevokeSecurityGroup.md` 明确 `SecurityGroupRuleId` **数组长度 0~100**。
- **触发条件**：单个 (provider, 规则) 单元待删规则 >100——例如域名 IP 集大幅收缩（TCP+UDP 拆分后 50+ 个 IP 即达 100），或修改端口列表（60 个端口 × TCP+UDP = 120 条）。
- **实际影响**：删除请求被云端拒绝 → 该单元每轮 `failed` → 整轮 failed → **运行健康持续 unhealthy 并反复告警**；同时**旧规则（旧 IP 的放行）永久残留在安全组里，安全面持续扩大**，需人工清理。
- **推荐整改**：`DeleteRules` 复用 `batchRules(rules, 100)` 逐批提交，并把已确认批次累加进 `PartialDeleteError.Deleted`。
- **判别性测试**：mock 增加"`SecurityGroupRuleId` 数量 >100 则返回 400"的约束，用 150 条断言产生 2 个请求。
- **是否与 AGENTS.md 冲突**：否（§十一 要求遵守 `PlatformAPIDocs/` 的参数限制）

#### 当前实施补记（2026-09-30）

- **核验基线与范围**：`main` / `7aaa3f2`，相对 `origin/main` ahead 2，研究开始时工作树干净。本轮只读核对源码并运行下列针对性测试；随后按用户要求只更新本文，不修改生产代码、测试、Issue7 或人工验收清单，不提交、不推送。头部与 §14 的 `d6d208e` 仍是此前批次的历史复核基线，本项以此处基线为准。
- **API 上限与成因**：仓内 `PlatformAPIDocs/AliyunECSAPIGuide/RevokeSecurityGroup.md` 与本轮查询的[阿里云官方文档](https://help.aliyun.com/zh/ecs/developer-reference/api-ecs-2014-05-26-revokesecuritygroup)均规定 `SecurityGroupRuleId` 数组长度为 0～100。旧实现的超限请求属于确定性构造错误，重发相同请求不能解决；当前分批已消除该根因。
- **已实施方案**：`provider/ali_ecs.go` 的 `AliECS.DeleteRules` 在发出任何请求前检查全部候选的 RuleID 非空，使用 `ecsDeleteBatchSize=100` 与 `batchStrings` 按输入顺序切批并串行提交；150 条固定拆为 100+50。任一批失败立即停止后续批次，保留此前已确认的 `DeleteResult.Deleted/Resolved`；已有成功批次时同时返回 `PartialDeleteError.Deleted`。当前使用 RuleID 切片分批，不是历史建议中的 `batchRules`（该函数用于新增的 `RuleAction`）。
- **部分成功与健康语义**：`syncer/target.go` 的 `runTargetCleanup` 对成功或部分成功删除取得 S2，并以同一纯 planner 重新验证期望覆盖；可信 S2 的实际清理候选数是最终 `cleanup_deferred` 的唯一来源。例如第一批确认删除 100 条、第二批失败，若 S2 仍覆盖完整期望且剩余 50 条候选，则目标为 `success`，`deleted/cleanup_deleted=100`、`cleanup_deferred=50`，不因清理延后本身转为 unhealthy。S2 Describe 或覆盖验证失败仍为 `failed`，不能用前批成功掩盖最终状态异常。
- **重试边界**：部分成功分支优先完成 S2 核验，确认覆盖后收敛为清理延后，不在本次调用内继续补发失败批次；后续轮次基于新快照重新规划。未确认删除进度的可重试清理错误可进入整目标重试，不沿用旧 attempt 的删除定位。幂等 NotFound 不虚增实际删除计数，仍需 S2 核验覆盖与残留。
- **本轮自动化证据**：以下命令在 `provider` 与 `syncer` 两包均通过，`-count=3` 连续执行三次：

  ```bash
  go test ./provider ./syncer -race -run '^(TestRequest_ECSDelete.*|TestCleanup_ECS.*|TestCleanup_PartialDeleteKeepsConfirmedAndDefersRest|TestCleanup_IdempotentNotFound.*)$' -count=3
  ```

  覆盖 `TestRequest_ECSDeleteBatches100`（实际 SDK 请求数为 2、每批为 100/50）、`TestRequest_ECSDeleteSecondBatchFailureKeepsConfirmedProgress`（保留 100 条确认计数）、`TestRequest_ECSDeleteRejectsMissingLocator`（缺定位零请求），以及同步层 ECS 候选交接、部分删除后的 S2/100+50 统计、幂等 NotFound 与 S2 不可信失败语义。Provider 证据来自真实 SDK 指向本地 HTTP mock，同步层证据来自探针 Provider；本轮未重跑全仓 race/vet/build、前端或 Docker 门禁，不能把本次定向绿色外推为全仓或真实云验收通过。
- **可选测试补强（建议，尚未实施）**：断言两批 RuleID 拼接后与输入完全一致（无遗漏、无重复、顺序稳定）；增加 250 条候选且第二批失败的用例，证明第三批不会发送（现有 150 条只有两批，不能单独证明停止后续批次）；补充 0/100/101 条边界，并可给 mock 增加超过 100 条即返回错误的约束。这些属于证据补强，不表示生产分批修复仍未完成。
- **真实验收边界**：`ProdTestList.md` PT-I7-05 仍未执行。后续在隔离测试安全组验证超过 100 个候选的真实分批清理，并确认期望规则与非当前 TAG 规则完整保留；当前 revision 的远端 CI/GHCR 也无本轮通过结论。本项状态为“本地已修复并取得定向回归证据，真实云验收待执行”。

---

### P2-03｜云端能力限制被静默丢弃（SWAS/ECS IPv6、ECS ICMPv6）

> **当前状态：✅ 已由 Issue7 Step 1 按当前强合同修复。** 下方首段保留修复前审计快照；其中“只补 WARN/Dry Run、不计 skipped”的旧决策已被 `unsupported → partial` 与清理冻结语义取代。

- **证据**：`provider/common.go:215-223` 两处裸 `continue`——`ip.IsIPv6 && !supportsIPv6(...)` 与 ECS+ICMP+IPv6——**无日志、无 skipped**；`unsupportedReason`（`:192-197`）**只**覆盖 SWAS DROP；仅 ECS+ICMP 有一条 WARN（`syncer/syncer.go:895-903`），但同样不计 skipped。
- **实际影响**：用户以为 IPv6 放行已生效，实际从未创建；界面、日志、统计、运行健康**全无痕迹**（SWAS 连 WARN 都没有）。与 Issue6 A11「云端能力限制必须如实列为 skipped」的口径不一致（SWAS DROP 已按此改造，这两类漏改）。
- **审计快照中的旧用户决策（已取代）：先只补 WARN 日志 + Dry Run 展示，不计入 skipped**

  | 子项 | 内容 |
  |---|---|
  | WARN | SWAS/ECS 跳过 IPv6 地址、ECS 跳过 ICMPv6 时输出与既有 ECS-ICMPv6 WARN 同级的 `slog.Warn`（含域名、协议、原因） |
  | Dry Run | 在目标单元的展示层体现（不改变 `RoundSummary` 语义、不影响 `outcome`） |
  | 不计 skipped | 避免因永久性能力缺失把整轮判为 `partial` → 健康持续 unhealthy → 反复告警 |

- **实现选择（建议采用前者）**：
  - 优先：给 `DryRunResult` 追加**独立字段**（如 `CapabilitySkips []RuleChange`），语义清晰、不影响整轮判定；
  - 次选：仅补 WARN，Dry Run 不动（改动最小）。
- **是否与 AGENTS.md 冲突**：否

#### 当前实施补记（2026-09-30）

- 当前能力矩阵统一产出结构化 `unsupported_*`/`PlanIssue`，不把不可实施项静默丢弃；目标结论为 `partial`，并冻结清理，避免把未能实施的期望误判为可安全删除。
- Dry Run、目标级事件与 SQLite 日志均保留结构化 `unsupported` 详情；该语义取代本节旧的“不计 skipped”方案。原始决策表仍保留在第 9 节，仅用于追溯。

---

### P2-04｜Push 心跳“唤醒”早于 `RuntimeState` 发布 → 启用后首条心跳可能被延后

> **当前状态（2026-09-30）**：✅ **已按用户“开始执行修复”的独立授权修复并提交为 `7acf303`。** 本批次实施基线为 `main / c23305e`（提交时相对 `origin/main` ahead 3，开工时工作树干净）。P3-03 未并入本次修复，真实 Uptime Kuma 与远端 CI/GHCR 仍未执行。以下缺陷分析保留修复前事实，实施结果见本节末尾补记。

- **强合同与修复前实现**：当前 [AGENTS.md](./AGENTS.md) 的 §十一要求 commit 后按“日志级别 → 告警集合 → `RuntimeState` → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒”发布，并明确“唤醒时新运行时必须已可见”。但 `webui/api/deps.go` 的 `Deps.applyCandidate` 修复前实际顺序为：日志级别 → 告警集合 → `Health.Wake()` → `Push.Wake()` → `Syncer.ApplyState()` / `Runtime.Apply()`；`ConfigCoordinator` 在 commit 后直接调用该 apply，没有额外屏障，`Syncer.ApplyState` 最终才替换 `RuntimeManager` 指针。
- **缺陷链**：`internal/health/push.go` 的 Pusher 循环每轮首先读取 `p.config()`（生产接线中来自同一个 `RuntimeManager` 的 `Snapshot()`）。如果配置由关闭改为启用时，Pusher 在 `ApplyState` 前被 `Push.Wake()` 唤醒，它可能读到旧的关闭配置；“未启用或 URL 为空”分支随后只等待 `wake/stop`，没有 interval 定时器。本次保存产生的 wake 已被旧配置路径消费后，新的启用状态可能要等下一次配置保存或重启才触发首条心跳。
- **触发条件与影响**：只需在运行期间把 `uptime_kuma_push` 从关闭保存为启用，且 Pusher 恰好落在 `Wake → ApplyState` 窗口即可触发。影响是新配置不会按本次保存及时发出首个 Push；若 Uptime Kuma 侧已有监控状态，可能继续看到旧状态或误判 DOWN。该结论来自源码时序分析，不是“真实 Uptime Kuma 已复现”；真实 Push 的 DOWN/恢复通知仍未执行。已在途的旧配置 HTTP 请求不属于本项必须取消的对象。

**已采用的最小修复设计**

- 只调整 `webui/api/deps.go` 的 `Deps.applyCandidate` 发布顺序：日志级别 → 告警集合 → **`Syncer.ApplyState` / `Runtime.Apply` 发布 `RuntimeState`** → `Health.Wake()` → `Push.Wake()`。
- `Syncer.ApplyState` / `RuntimeManager.Apply` 是无网络、无数据库、无失败返回值的内存发布；提前发布不会改变“commit 后不重新读库、不构造 Provider、不访问网络”的协调器边界，也不需要新增 mutex、channel barrier、等待 Push 完成或让配置 API 等待外部 HTTP 请求。
- 保持启动阶段既有的“先启动 Syncer、等待进入运行态，再启动 Supervisor/Push”顺序；P2-04 是运行期间热更新竞态，不应借此修改 `run.go`、`RuntimeManager`、`Supervisor` 或 Pusher 的内部同步机制。
- 代码注释须同步当前顺序：`webui/api/coordinator.go` 的协调器说明、`webui/server.go` 的运行时接线说明，以及仍记录旧顺序的测试注释。AGENTS.md 已在 Step 0 修订为当前强合同，本项不再修改 AGENTS.md 行文。

**受影响文件与边界**

| 范围 | 文件/符号 | 影响 |
|---|---|---|
| 核心实现 | `webui/api/deps.go` / `Deps.applyCandidate` | 将 RuntimeState 发布移到 Health/Push 两次 Wake 之前；生产 `Syncer != nil` 与最小 `Runtime != nil` 分支均须保持该顺序 |
| 注释闭环 | `webui/api/coordinator.go`、`webui/server.go` | 删除旧的“Health/Push 先于 RuntimeState”叙述，记录完整发布顺序 |
| 判别性测试 | `webui/api/alertset_test.go`，必要时复用 `webui/api/testenv_test.go` | 覆盖 Wake 内读取共享 RuntimeManager 时新配置已经可见；保留 PUT/import/非法输入回归 |
| 现有回归 | `webui/api/operational_test.go` | 仅同步旧顺序注释或增加调用顺序断言，不以 Wake 次数断言替代可见性用例 |
| 明确不改 | `internal/health/push.go`、`syncer/state.go`、`syncer/syncer.go`、`run.go` | P2-04 最小修复不扩大到 Push 重试、运行时锁机制或启动接线 |

**判别性验收设计**

1. 初始共享 `RuntimeManager` 使用 Push disabled 状态；候选状态设置 `Enabled=true` 和合法的新 URL，interval 取较长值，避免周期请求干扰。
2. 注入一个 `Push.Wake()` 探针，在 `Wake()` 内立即执行与生产 Pusher 相同的 `RuntimeManager.Snapshot()`，断言读到 `Enabled=true` 且 URL 为新值；对 `Health.Wake()` 使用同样的可见性探针，确认两次唤醒都发生在发布之后。
3. 覆盖 `d.Syncer != nil` 的生产接线分支，以及 `d.Syncer == nil && d.Runtime != nil` 的最小测试分支；测试必须明确两个消费者使用同一个 `RuntimeManager`，不能用不同对象制造假阳性。
4. 保留现有 `PUT /api/alerts`、`POST /api/config/import` 和非法输入回归：非法输入不得 commit、Apply 或 Wake；合法事务 commit 后只做无失败内存发布和唤醒。
5. 本地集成层可用 mock Push HTTP 服务验证“启动时关闭 → API 保存启用 → 有界时间内收到使用新 URL 的首条请求”；该层只能证明本地接线，不替代真实 Uptime Kuma 验收。

**P3-03 是否与本项合并：选项、推荐与影响**

- **A（推荐，独立处理）**：本项只修复 RuntimeState 先发布再 Wake；P3-03 另行处理 `buildPushURL` 失败后的 interval 重试。当前正常 HTTP 状态错误、网络错误、超时和非 `ok` JSON 已按 interval 继续尝试，不能把 P3-03 泛化成“所有首次 Push 失败都静默”。优点是补丁和验收单一、不会改变生产超时或健康语义；代价是非法配置仍可能等待下一次配置唤醒，需在独立议题中明确接受或修复。
- **B**：与本项同时为 `sendOnce` 返回 false 的非法 URL 分支增加周期性重试。影响是需要另行决定重试间隔、WARN 频率、是否在 operational health/UI 中暴露配置错误，以及如何避免脏数据库造成日志噪声；范围超出 P2-04 的最小时序修复，当前不推荐直接并入。

**风险、外部依赖与停止条件**

- 风险较低但需保持边界：RuntimeState 提前发布是无失败内存操作；已经开始的旧 Health 检查或旧 Push HTTP 请求可以继续完成，本项不增加完成屏障，也不要求配置 API 等待网络请求。
- 该问题可由共享 `RuntimeManager`、Wake 探针、webui/api 定向测试和本地 mock Push 完全证明，不依赖真实云账号或真实 Uptime Kuma。真实 Uptime Kuma HTTP Monitor、Push 的 DOWN/恢复通知、真实云 API、浏览器和远端 CI 仍是独立外部边界，当前均无通过结论。
- 在判别性用例证明 Wake 时已经看到新配置、两条 apply 分支均通过、注释无旧顺序残留前，保持 P2-04 未修复；I-02 还须独立完成 P3-03 才能关闭；不得以仅检查 Wake 次数、最终状态或单次绿色运行替代时序断言。
- 若修复引入等待 Push/Health 完成、数据库或网络操作，修改 `Pusher`/`Supervisor` 内部同步、改变 shutdown 语义、把 P3-03 重试语义混入，或无法证明生产接线共享同一 RuntimeManager，应停止并重新审查范围。

**决策与合同关系**

- 用户在本轮明确授权“开始执行修复”，已采用“`ApplyState` 提到两次 `Wake()` 之前”的最小代码修复。
- 修复后的代码已与 AGENTS.md §十一发布顺序一致。Issue7 中的 P1-01 越界记录仍为当时授权边界；本次是独立实施，不改写该历史记录，不顺带关闭 P3-03。

**实施补记（2026-09-30）**

- `Deps.applyCandidate` 的完整 RuntimeState 发布分支已移到 Health/Push 唤醒之前；仍保留 Syncer 优先、最小接线 `Runtime.Apply` 与原告警状态日志。协调器、服务器接线与测试注释同步新顺序；Build7 仅追加历史顺序纠正说明。
- 新增 `TestApplyCandidatePublishesBeforeWake`、`TestAlertsAndImportPublishBeforeWake`：两条分支均在 Health/Push 的 Wake 内读取同一个 RuntimeManager，验证本次候选已可见、Health 先于 Push；PUT/import 与非法输入零发布零唤醒均覆盖。生产分支使用真实 Syncer，不以 stub 自行实现 ApplyState 制造假阳性。
- 新增 `TestPutAlertsEnablesPushAfterPublication`：真实 Pusher + 临时 SQLite + PUT API + 本地 HTTP mock，用有界 channel 固定配置重读时序；初始关闭，保存启用并设置 `1h` interval，要求 2 秒内收到新 URL 的首条请求。健康状态采用受控夹具、Syncer 主循环不运行，本用例证明发布接线与 Push 首发，不代表完整进程或真实 Uptime Kuma 验收。
- 负向控制：新增的三个正式用例在修改生产顺序之前全部失败，且 Runtime/Syncer 两个分支均明确读到旧状态；顺序修复后定向 `-race -count=20` 通过。最终门禁：`go test ./... -race -count=3 -timeout=10m`（12 包）、`go vet ./...`、`go build ./...`、前端 `npm run build`、gofmt 与 `git diff --check` 均通过。本次未执行 Docker、真实云、浏览器、真实 Uptime Kuma 或远端 CI/GHCR；该批次代码与文档已提交为 `7acf303`。
- 未修改 `internal/health/push.go`、Supervisor、RuntimeManager、Syncer 或启动接线；不等待消费者完成，不取消保存前已开始的旧检查/请求，不改变 HTTP 超时、健康、重试与 shutdown 语义。通知仍可合并，保证的是发布后唤醒触发新读取，不是每次保存对应一条独立心跳。

---

### P2-05｜Webhook "成功"仅看 HTTP 状态码，忽略响应体业务错误码 → 告警静默失效

> **I-05 当前实施补记（2026-09-30，优先于下方研究历史）**：用户授权开始修复；实施前基线为 main / c5cc79d（ahead 5，工作树干净）。仅修改 notifier 响应处理、相关测试及本文；修复已提交为 `93e0e4b`，当前相对 origin/main ahead 6。

- **当前实现**：Webhook 先拒绝非 2xx，再通过 io.LimitReader 最多读取 16 KiB + 1 字节。超过 16 KiB、读取失败、空/未知/非法响应均失败；原有 HTTP 10 秒上限覆盖正文读取，在途名额保持到读取及关闭结束。
- **渠道合同**：钉钉要求明确的整数 errcode=0；Slack 要求 HTTP 200 且 TrimSpace 后正文恰为 ok；飞书采用方案 A，接受 code=0 或只有旧 StatusCode=0 的响应，两个字段同时出现时必须全部合法且为零。map[string]json.RawMessage 配合整数指针区分缺失/null 与明确零，不以消息文本或默认零值判断成功。
- **飞书规范补证**：研究验证阶段已通过浏览器读取[当前官方自定义机器人文档](https://open.feishu.cn/document/client-docs/bot-v3/add-custom-bot)（页面标注最后更新 2025-03-27），成功示例同时包含 code=0 与 StatusCode=0，并注明后者为历史兼容冗余字段、不建议使用；业务错误使用非零 code。此证据是规范核验，不是真实端点投递。
- **错误安全**：业务错误仅保留安全渠道名、固定类别、整数业务码；读取/JSON/关闭错误不输出或包装原始错误与响应正文。业务失败沿既有 EventBus 产生安全 WARN；关闭错误单独记录固定 response_close WARN，平台已明确接受时不因关闭错误改判业务失败。
- **回归覆盖**：新增 webhook_response_test.go，覆盖三渠道成功/业务失败、缺字段/null/类型错误、双字段冲突、空体/非法 JSON/尾随 JSON、响应体关闭及在途释放、16 KiB 边界与实际读取量、读取错误、三渠道 EventBus 安全 WARN、真实本地 HTTP 连续交替响应与生产默认 10 秒正文 deadline。正文与限流成功夹具改为对应渠道合法成功响应，保留原内容、在途上限和跨热重载断言。
- **本地门禁**：定向响应测试 -race -count=20 已通过；notifier 整包 -race -count=3 已通过；go vet ./...、go build ./... 已通过。全量 12 包 go test ./... -race -count=1 -timeout=20m 与最终 git diff --check 均通过。三轮重复证据仅覆盖 notifier 包，不将单次全量绿色外推为全仓长期稳定绿色。
- **外部边界**：真实钉钉/飞书/Slack 接收、产品浏览器与当前改动的远端 CI/GHCR 未执行；PT-B7-03 保持未执行/人工免除，非已通过。未增加重试、补发、持久化投递状态或健康联动；未改 EventBus、配置 API、SQLite、前端、Uptime Kuma Push 或生产 HTTP 超时。16 KiB 为本地工程上限，并非平台官方最大响应保证。

以下保留修复前审计与研究记录，不能作为当前未修复结论。


- **研究阶段状态（历史）**：当时问题真实存在、尚未修复，研究阶段只更新审计记录，没有修改代码或运行门禁。 它与 Issue7/P1-01 主线正交，不能因 P1-01 Step 0～5 主体完成而关闭，也不能把已有 Uptime Kuma Push 的严格 `{"ok":true}` 判断外推到普通 Webhook。
- **研究阶段基线与工作树证据（历史）**：当前为 `main` / `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；本轮核验时工作树唯一已跟踪修改是本审计文档，源码与测试没有本轮修改。该状态是当前核验结论，不沿用历史快照中的 HEAD 或“工作树干净”措辞。
- **修复前生产缺陷链（历史）**：`EventBus.Publish` 异步调用 `WebhookNotifier.OnEvent`；`notifier/webhook.go:120-129` 发起 POST 后只关闭响应体，`notifier/webhook.go:126-128` 只判断 `resp.StatusCode >= 300`，从不读取或解析 `resp.Body`。因此所有 2xx 响应都返回 `nil`，即使响应体明确表示业务失败。EventBus 只有在 `OnEvent` 返回错误时才记录“事件处理失败”WARN，所以该路径既被当成发送成功，也不会产生告警日志。
- **修复前测试缺口（历史）**：`notifier/webhook_content_test.go:19-29` 的假 transport 固定返回 `HTTP 200 + {}`，当前只验证正文、格式和确定性，没有 `200 + {"errcode":310000,...}` 的失败控制；`notifier/webhook_security_test.go:75-119` 等现有测试也没有覆盖 `errcode`、`errmsg` 或响应体业务状态，全仓未发现对应的判别性解析测试。故“测试通过”不能证明当前 Webhook 已正确接受消息。
- **外部协议依据与准确边界**：
  - 钉钉常见成功语义为 JSON `errcode == 0`；关键词、签名或 IP 白名单校验失败、Token 不存在、机器人停用等错误可以表现为 HTTP 200 但 `errcode != 0`，例如 `310000`、`300001`、`400102`。参考[阿里云钉钉通知错误码文档](https://www.alibabacloud.com/help/en/sls/error-codes)及[关于 HTTP 200 与 `errcode=310000` 的说明](https://www.alibabacloud.com/help/zh/cms/cloudmonitor-1-0/support/the-dingtalk-robot-that-sets-the-alarm-contact-reported-an-error-the-signature-sent-by-the-robot-does-not-match)。
  - Slack Incoming Webhook 的官方口径是成功通常返回 HTTP 200 且正文为纯文本 `ok`；其他正文，即使仍为 200，也不能按成功处理。HTTP 400/403/404/410/500 等则属于 HTTP 层失败。参考[Slack Incoming Webhooks](https://api.slack.com/messaging/webhooks)和[Slack 错误状态说明](https://api.slack.com/changelog/2016-05-17-changes-to-errors-for-incoming-webhooks)。
  - 飞书资料同时出现 `code/msg` 业务错误形态和 `StatusCode/StatusMessage` 成功形态；公开官方开发者社区示例展示了非零 `code`（如 `19001`、`19024`）等错误，但该页面不能单独作为当前接口唯一规范。实施前必须以当前飞书机器人文档或真实端点响应确认最终兼容范围，不能简单把钉钉的 `errcode` 规则套到飞书。
- **实际影响**：钉钉等端点若返回 HTTP 200 业务失败，`OnEvent` 返回 nil，日志没有失败 WARN，用户会误以为告警已发送而实际未送达。这是告警静默失效，不是普通 HTTP 网络错误；现有 Webhook 错误安全测试只能约束传输错误时的敏感信息，不覆盖业务失败响应。
- **研究方案（本轮已实施）**：只修改 Webhook 响应处理和其判别性测试，不改变告警订阅、EventBus 异步模型、每渠道在途限流、配置契约、Webhook 重试策略或 Uptime Kuma Push。`OnEvent` 尾部应按“非 2xx → 有界读取响应体 → 按 channel 校验业务成功 → 明确成功才返回 nil”的顺序处理；建议增加包内私有 `validateWebhookResponse(channel string, status int, body []byte) error` 或等价 helper。响应体读取必须有小的上限，避免第三方返回异常大正文导致无界内存占用。
  - 钉钉：要求 JSON 中存在 `errcode` 且为 `0` 才成功；非零返回安全错误，例如 `channel=dingtalk category=business_response code=310000`。
  - Slack：要求 HTTP 200 且 `strings.TrimSpace(body) == "ok"`；其他、空或无法确认的正文失败。
  - 飞书：推荐只接受已确认的显式成功形态，同时支持资料中已确认的 `code == 0` 与 `StatusCode == 0` 两种形式；已识别的非零 `code`/`StatusCode` 失败，空响应、非法 JSON、未知对象及只有 `msg` 没有状态码均失败。若产品只使用一种当前飞书形态，可在实施前选择更窄的单一合同。
  - 未知、空或无法确认的 2xx 响应统一失败闭合，避免“无法证明已接受”被当成成功。错误不得包含完整 URL、query token、签名、路径、原始 `errmsg`/`msg`、完整响应体或请求正文；最多保留安全渠道名、错误类别和必要的业务码。EventBus 收到该错误后应沿现有路径产生安全 WARN。
- **受影响文件与明确排除**：主要生产文件为 `notifier/webhook.go`（`WebhookNotifier.OnEvent` 与响应校验 helper）；测试文件为 `notifier/webhook_content_test.go`（按渠道改用真实成功响应）和 `notifier/webhook_security_test.go`（业务失败与敏感信息边界）。不应修改 `webui/api/alertset.go`、告警配置 API、SQLite schema、前端告警页、EventBus 异步投递方式、Webhook 重试策略或 Uptime Kuma Push 已有 `{"ok":true}` 解析逻辑。
- **判别性验收合同（修复前应能暴露，修复后必须通过）**：
  1. 钉钉：`HTTP 200 + {"errcode":0,"errmsg":"ok"}` 返回 nil；`HTTP 200 + {"errcode":310000,"errmsg":"keywords not in content"}` 返回 error，错误含 `channel=dingtalk` 与 `code=310000`，不含 `errmsg`、URL 或 token。
  2. 飞书：已确认的成功响应返回 nil；已确认的非零 `code`/`StatusCode` 返回 error；空响应、非法 JSON、未识别 JSON 返回 error；错误不带 `msg` 原文。
  3. Slack：`HTTP 200 + "ok"` 返回 nil；`HTTP 200 + "invalid_token"`、`HTTP 200 + "{}"`、HTTP 200 空正文均返回 error。
  4. 公共控制：非 2xx 仍返回安全 `http_status` 错误；业务失败经 EventBus 产生 WARN；WARN 只含安全渠道名、错误类别和必要业务码；既有 transport error 的 URL/token 不泄漏测试继续通过；三个渠道连续多次响应的判定保持稳定。
- **验收层级与外部边界**：静态检查需证明 2xx 不再无条件成功且错误不带 URL/响应正文；`httptest` 或自定义 `RoundTripper` 覆盖三渠道成功、业务失败、空体、非法体和非 2xx；随后执行受影响包的 race/vet/build 等本地门禁，以证明响应读取没有引入回归。真实钉钉、飞书、Slack 端点和凭据仍需单独人工验收；当前没有任何证据可把真实 Webhook 写成已通过，`ProdTestList.md` 的 PT-B7-03 仍应保持未执行/按用户决定跳过的边界。Mock、本地测试、真实 SMTP、Uptime Kuma、真实云 API、浏览器和远端 CI/GHCR 均不能替代真实 Webhook 接收证据。
- **研究阶段决策选项（历史，本轮采用 A）**：

  | 选项 | 飞书响应兼容范围 | 影响 |
  |---|---|---|
  | **A（推荐）** | 同时接受已确认的 `code == 0` 与 `StatusCode == 0`；明确非零失败，未知/空/非法响应失败 | 兼容当前资料中两种已见形态，并采用失败闭合；测试矩阵较完整，实施前仍需以当前飞书文档或真实响应确认字段合同 |
  | B | 只接受当前实际使用的一种明确成功结构 | 逻辑更窄、误接受面更小，但可能拒绝项目现有端点的另一种合法响应；需用户确认当前实际形态 |
  | C | 所有 2xx 或任意 JSON 视为成功 | 保留现状的静默丢告警风险，无法满足 P2-05 修复目标，不推荐 |

  钉钉的 `errcode == 0` 与 Slack 的正文 `ok` 检查是独立于该选择的固定修复方向；本轮已按用户“按照你的推荐”及“开始修复”授权采用 A；两个状态字段同时存在时必须都合法且为零。
- **研究阶段风险与停止条件（历史）**：主要风险是把渠道协议错误抽象为一个通用字段，或为了兼容而把任意 2xx 当成功，继续造成告警静默丢失；未知响应必须失败闭合，响应体读取必须有界。若实施时修改 EventBus 异步/限流/重试、告警配置 API、Uptime Kuma Push、生产 HTTP 超时，或把原始 URL、token、请求正文、响应正文/`errmsg`/`msg` 写入错误和 WARN，应立即停止并重新审查范围。未补齐三渠道判别性响应测试、未完成安全错误断言以及受影响包门禁前，保持 I-05/P2-05 未修复；不得以单次绿色运行或真实端点的 HTTP 200 代替业务层成功证据。
- **是否与 AGENTS.md 冲突**：当前最小修复方向不冲突。AGENTS.md §9.1 已明确 Uptime Kuma Push 的 `{"ok":true}` 成功口径；P2-05 只是为普通 Webhook 渠道补齐各自协议的业务成功判断，不改变既有配置、异步投递或外部验收边界。

---

### P2-06｜告警页加载失败后仍可保存，用初始默认值覆盖全部真实告警配置并提示"保存成功"

- **状态（2026-09-30）**：✅ **按用户确认的完善方案 A 已本地修复并验收，I-03 本地收口；源码、测试与该批次文档已提交为 `064b794`。** 本轮基于 `main == origin/main` / `7ff37b1` 的干净工作树实施；不改变后端四对象完整覆盖合同，不并入 Issue7 完成结论，也不外推 PT-B7-07 全项或真实外部验收。
- **原问题与触发链（保留）**：`Alerts.vue` 初始化四组合法默认表单；原 `load()` 失败仅提示、按对象分别赋值；原 `save()` 与按钮没有加载守卫。GET 失败、尚未返回或 200 缺对象时，用户仍能发送完整四对象 PUT，后端原子覆盖真实 SMTP 密码、Webhook/Push URL 与触发策略，随后提示保存成功。禁用渠道允许空 SMTP 主机/URL，因此默认载荷可以通过后端严格校验。此项是前端状态机问题，不是后端校验缺失。
- **原报告更正**：告警 GET 没有传入 `timeoutMs`，当前没有应用层主动计时超时；通用 `request()` 会吞掉 JSON 解码失败并返回 `null`。本次守卫同时覆盖请求挂起与非 JSON/`null` 响应，未改动通用封装或新增超时。
- **已实施方案**：生产改动仅在 `webui/frontend/src/views/Alerts.vue`。
  1. `loadState` 为 `loading / ready / error`，初始为 `loading`；每次 `load()` 开始重新锁定保存。
  2. GET 使用 `unknown` 接收，组件内类型守卫检查顶层与四个分组均为非 null、非数组对象，全部必需开关为 boolean，其余字段为 string（含密码、URL、Webhook channel）。合法空字符串与 false 不被误拒绝；不复制后端业务校验、不补默认值填缺字段。
  3. 先验证完整响应，再同步替换四组表单，最后进入 ready；错误响应不会部分更新表单。
  4. 保存按钮在未 ready 或 saving 时禁用；`save()` 入口独立检查 ready 和 saving，阻断直接调用与重复在途 PUT。普通 PUT 失败保留 ready 与当前编辑，允许重试。
  5. 加载中和失败均有持续页面提示；失败提示刷新重试。测试邮件保留原有独立八字段 POST，不受加载状态限制。
- **轻量回归证据**：新增 `webui/frontend/tests/alerts-load.test.mjs` 和 `npm run test:alerts`，使用已有 Vue SFC 编译器、TypeScript、Vue 与 Node 内置 test/vm，直接编译真实组件脚本，替换请求与消息依赖；未引入新依赖或测试框架。5 个测试通过：挂起/拒绝读取无 PUT、77 种对象/子字段缺失/null/类型错误整体拒绝、合法默认字段与敏感值保留、重复 PUT/失败后重试、成功后重载失败重新锁定及测试邮件独立八字段 POST。修复前 `7ff37b1` 组件源码负向控制运行“不完整载荷”用例，明确因请求次数 `2 != 1` 失败（原保存仍发送 PUT）；修复后同用例通过。
- **浏览器/API/SQLite 联合证据**：本次前端 build 后构建真实二进制，以独立 `/tmp/fwalizer-p206-data.Atfayo/config.db`、后端 loopback `61206` 与临时故障代理 `61207` 验收，未接触用户实际业务数据库。
  1. 预置标记密码、Webhook/Push URL 与启用的触发开关。浏览器先后进入 GET 500、200 缺 Push 对象与 GET 挂起场景，均显示禁用保存；截至正常保存前代理记录 `puts=0`，无保存成功提示，四组 GET 配置与初始快照一致，SQLite 敏感列保持不变。
  2. GET 失败时点击测试邮件仍产生独立 POST（代理 `tests=1`、`puts=0`）；响应为本地模拟，不是真实 SMTP 接受证据。
  3. 完整 GET 后保存按钮恢复可用，浏览器修改邮件主题并保存，代理记录单条 PUT 与页面“保存成功”；真实 API 返回四组配置只改变主题，SQLite 主题落库且密码保持原值，URL/策略保持不变。
  4. 浏览器截图保存在本轮本地可视化目录 `p206-save-disabled.jpg`；只证明本项交互，不把整个 PT-B7-07 标为通过。
- **本地门禁**：`npm run test:alerts`、`npm run build`、`go test -race ./webui/api ./config -count=1`、`go vet ./...`、`go build ./...`、`git diff --check` 均通过。未跑全量 Go race、多轮稳定性、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR；这些不是本项已取得的证据。
- **范围与残余边界**：未修改 `api.ts`、`types.ts`、后端 DTO/事务/schema、测试邮件 API、导入导出或其他页面。GET 四次独立读取的内部一致性与旧页面覆盖新配置需独立追踪；本次不宣称已解决并发编辑冲突。与 AGENTS.md 的完整覆盖、独立测试邮件和轻量化约束一致。

---

### P2-07｜目标与规则的行内删除缺少二次确认（已本地修复）

- **当前状态**：✅ **I-04 本地修复与验收已收口，尚未提交**。实施前基线为 `064b794`；研究时仓库外原型不算修复证据，本节记录的是实际生产页面修改后的验收。
- **原问题与影响（历史）**：两页的行内按钮分别直接进入 `deleteTarget(row)` / `deleteRule(row)` 并发送 DELETE，没有待删除对象、取消路径与提交中守卫；一次误触即可删除 SQLite 配置。目标引用 409 仅保护显式 `rules.targets` 引用，不提供前端确认；空数组表示适用于全部目标，不构成某个 ID 的显式引用。规则删除没有该引用兜底。
- **已实施方案**：
  1. 两页各自使用 `NModal preset="card"` 与红色大号确认按钮。行内删除只打开弹窗；目标显示数据库 ID/中文云产品/资源 ID/地域，规则显示数据库 ID/域名/协议/端口/动作/IP 版本/适用目标。摘要采用类型明确的副本，规则目标数组独立复制；请求固定使用确认时捕获的 ID。
  2. 页面内 `idle → confirming → deleting → refreshing → idle` 四阶段；打开/确认/取消函数各自检查阶段。确认是唯一 DELETE 出口，首个 await 前同步进入 deleting；提交期间保留摘要和 loading，禁用添加/编辑/删除与取消、关闭、遮罩、Esc。取消路径清空对象并且零 DELETE。
  3. 成功立即移除当前行并提示一次成功，再等待列表刷新；刷新失败单独提示「配置已删除，但列表刷新失败」。409/404/其他 HTTP 错误显示后端错误，网络异常提示删除结果不确定并刷新核对，均不自动重试 DELETE。
  4. `loadSequence` 仅允许最新列表响应写入或显示加载错误；开始删除使旧读取失效，避免过期 GET 重新插入已删行。卸载使旧页面读取与删除后续回调失效，不继续刷新、提示或抢焦点；离开页面不等于服务端取消。
  5. 默认焦点落在取消；取消沿组件既有路径恢复焦点。删除结束后的焦点同时等待列表刷新与弹窗退出：成功回到添加按钮，失败优先回到仍存在的来源按钮。**真实浏览器发现表格复用按钮时，FocusTrap 的晚到恢复会覆盖提前设置的焦点，已修正并重新构建、验收。** `afterDeleteLeave` 只处理退出完成与焦点，不清理业务阶段或 pending 对象，旧退出回调不得清空新确认。
  6. 弹窗宽度为 `min(520px, calc(100vw - 32px))`，摘要允许长文本换行。说明删除目标不会立即清理云规则；删除规则可能改变后续期望并触发安全清理，无适用规则目标会被跳过，当前在途轮次可能继续使用旧快照。
- **轻量回归**：新增 `webui/frontend/tests/delete-confirm.test.mjs`，通过既有 Vue SFC 编译器/TypeScript/Vue/Node 内置 test/vm 编译真实脚本，并检查真实模板与表格按钮绑定；`npm run test:delete` **20/20 通过**。覆盖零请求取消、单在途/刷新锁定、ID/数组副本、取消 A 再确认 B、409/404/500/网络异常、成功刷新失败与失败核对失败、旧 GET/旧错误丢弃、卸载、两条完成时序的焦点与旧退出回调。每页分别移除确认阶段守卫、列表序号条件的负向控制能够识别重复 DELETE 和旧行重现，不复制一套独立状态机。
- **浏览器/API/SQLite 联合证据**：
  - 使用最新 `npm run build` 产物重新编译的真实二进制；数据目录为独立临时目录，SQLite `sync_enabled=false`，告警默认关闭。仅本机 loopback；目标、域名及资源 ID 都是临时合成夹具。故障代理只转发真实 Go API 并记录请求/状态，注入删除响应延迟、单次列表 500 或提交后丢失响应；正常成功、409 与 404 均来自真实后端，未模拟其业务判断。
  - **两页取消按钮/关闭按钮/Esc/遮罩均零 DELETE**；请求记录与 SQLite 联合证明目标/规则保留。两页确认双击各只出现一条 DELETE，提交期间关闭入口不可用；正常成功行消失，SQLite 中对应 ID 已删除。目标 409 保留目标和规则引用；其他客户端先删除后，两个页面的确认分别取得真实 404、显示错误并刷新复位，随后可操作另一行。
  - 两页分别注入「DELETE 已成功、后续 GET 500」：同时显示删除成功与刷新失败，行已移除，数据库已删除，未补发 DELETE；分别注入「后端提交成功但响应丢失」：提示结果不确定，GET 核对后行消失，每项仍仅一条 DELETE。旧 GET 与卸载边界由组件故障注入覆盖，不冒称浏览器已注入该时序。
  - 默认取消焦点与成功回退添加按钮已浏览器确认；375×812 视口下长资源 ID/长域名及显式适用目标完整换行，两页确认框均为 **343px**，左右边界为 **16/359px**，`scrollWidth == clientWidth`，按钮可见；视口已恢复。
- **门禁与产物**：`npm run test:delete`、`npm run test:alerts`（5/5）、`npm run build`、`go test -race ./webui/api ./config`、`go vet ./...`、`go build ./...`、`git diff --check` 本地通过。浏览器截图、代理请求记录与临时数据库位于本轮临时验收目录；实例及浏览器页在验收后关闭，不写入用户原有配置。
- **范围与残余边界**：生产仅修改两页与前端测试脚本入口；后端 DELETE、目标引用 409、404/成功事务语义、ConfigCoordinator、Schema、Syncer、Provider 和强要求文档不变。确认不构成 API 授权，也不提供跨标签页版本校验、回收站或云规则撤销。本轮未执行全量 race、Docker、真实云/通知服务及远端 CI/GHCR；不把本项局部浏览器结果外推为 PT-B7/PT-I7 全项通过。
- **停止条件结论**：取消零 DELETE、确认单请求、目标引用保护、正常删除、失败恢复、前端构建和真实浏览器/API/SQLite 证据均已取得，满足 P2-07 本地收口条件。当前无本项待修代码，仍待用户后续安排提交/推送与产品整体外部验收。

---

### P2-08｜仪表盘把 `idle` 轮次误报为"最近一轮未完整成功"

> **当前状态：✅ 已由 Issue7 Step 4 修复（`Dashboard.vue:52-70` 只消费后端 `last_round.outcome`，不再自行按 `lastSync !== lastSuccess` 派生"停滞"）。** 以下为修复前审计快照。

- **证据**：`webui/frontend/src/views/Dashboard.vue:48-70`——`failed`/`partial` 提前 return，其余落到 `:63-65` 的 `lastSync !== lastSuccess` 比较。后端 `syncer/syncer.go:751-755` **每轮**都更新 `lastSync`，**仅 `RoundSuccess`** 更新 `lastSuccess`；`outcomeOf`（`:776-788`）中 `Total==0 → idle`（≠success）。
- **决定性证据**：`webui/sync_status_test.go:161` 的 `assertLastSuccessPreserved` **断言 idle 后 `last_success` 被保留**——证明"`last_sync` 前进而 `last_success` 不动"是**有意的后端契约**，不是缺陷。因此 `:63-65` 分支**唯一可达输入就是 `idle`**，净效果即误报。
- **违反语义**：AGENTS.md:151「`idle` 与空目标/空规则视为正常」
- **实际影响**：仪表盘显示黄色警告"最近一轮同步未取得完整成功"，而 `/api/health/operational` 同时返回 **200** → 前端把健康显示为异常，引导用户排查不存在的问题。
- **推荐整改**：`healthHint` 对 `outcome === 'idle'` 显式返回 null。
- **判别性测试**：跑出一轮 success → 删除全部规则 → 等下一轮 → 出现该警告，同时 `curl /api/health/operational` 返回 200
- **是否与 AGENTS.md 冲突**：否（属补齐 §9.1 语义）

---

### P2-09｜模拟测试结果以 `domain` 作 `v-for` key，同域名多规则时 key 重复且两张表无法区分

> **当前状态：✅ 已由 Issue7 Step 4 修复（`components/DryRunResults.vue:137` 改为以 `target_id` 为 key 并目标卡片化）。** 以下为修复前审计快照。

- **证据**：`webui/frontend/src/components/DryRunResults.vue:106` `v-for="item in items" :key="item.domain"`；后端 `syncer/syncer.go:604-638` **每条规则**产出一个 `DryRunResult{Provider: p.Name(), Domain: rule.Host}`；`rules` 表无 host 唯一约束（`config/store.go:147-155`），`NormalizeRule` 不做 host 去重。
- **触发条件**：同一域名 ≥2 条规则且适用于同一目标（如 `api.example.com` 的 TCP 443 + UDP 443——这是 TCP+UDP 拆分的自然结果）。
- **实际影响**：同父节点下 key 重复（Vue 要求 key 唯一，dev 报警、生产可能 patch 到错误节点复用旧行）；且两张表用同一个 `<h4>{{item.domain}}</h4>`，**用户无法判断哪张对应哪条规则**——而该页是开启同步前**唯一的变更预览门禁**。
- **推荐整改**：key 改为 `${item.domain}-${index}`，标题加入 protocol/ports（如 `api.example.com · TCP/443`）。
- **是否与 AGENTS.md 冲突**：否

---

### P3-26｜ECS 资源扫描结构异常 → 静默截断并覆盖缓存（✅ 已本地修复，尚未提交）

**来源与原问题：** 2026-09-30 第二轮核验新增。原 `scanAliECS` 遇到缺失 Body/集合便在 token 检查前 `break`，随后返回此前累计的资源和 nil 错误；API 据此覆盖当前云产品+地域的缓存。首页结构缺失会清空旧缓存；后续页缺失/null 会用半截结果替换缓存；数组 null 元素还可能 panic。原报告“丢弃已累计 resources”措辞不准确，实际是丢弃后续查询机会并把已累计部分当完整结果返回。扫描缓存只服务展示与自动补全，不参与防火墙同步写入。

**方案更正与用户确认：** 原“仅首页空响应成功、后续页空响应失败”设计撤销。官方文档按返回 `NextToken` 为空/缺失判定末页，没有明确保证后续页非空；token 模式不返回 TotalCount。故有效空数组与结构异常必须分开，不能仅按页序号判断。用户确认保守策略：缺失/null 集合失败；显式空数组合法并按 token 分页。参考 [官方 DescribeSecurityGroups 文档](https://www.alibabacloud.com/help/en/ecs/developer-reference/api-ecs-2014-05-26-describesecuritygroups)。

**已实施：**

- `scanAliECS` 通过纯转换函数 `decodeECSScanPage` 验证并映射 SDK 页，resp/Body/集合缺失、数组 null 元素、资源 ID 缺失或空统一返回包装 `ErrSnapshotIncomplete` 的错误；名称允许为空。任何失败返回 nil 资源，不带出之前的累计结果。
- 有效页追加后只按返回 token 判定结束；显式空首页/中间页/末页均正常处理。成功零资源输出为非 nil 空数组，允许既有覆盖式缓存清除真正已无资源的地域。
- 同次实施 P3-25 资源扫描 token 历史集合保护；错误不含原始 token。未修改同步 Provider、API handler、SQLite/前端/SDK，不增加重试或固定页数上限。
- API 既有错误分支在 `ReplaceScannedResources` 前短路，返回 `success:false`，前端沿既有失败路径保留旧内存数据并显示错误。

**独立回归与判别力：**

- `TestScanECSIncompletePage`：经真实 SDK 验证后续页缺集合/null 数组/null 元素/缺 ID，断言 `errors.Is(err, ErrSnapshotIncomplete)` 和 nil 资源。
- `TestDecodeECSScanPageMissingResponse`：直接覆盖 HTTP mock 不能构造的原生 nil resp 与 nil Body；此证据与 `{}` JSON 解码明确区分。
- `TestScanECSCacheIntegrity`：14 类真实 SDK → 实际 handler → 临时 SQLite 用例，含正常两页、有效空首页/中间页/末页、缺失/null 结构、非法元素/ID，以及 P3-25 重复/环路。失败时旧记录（含数据库 ID、名称及其他地域）完全不变；成功时资源 ID 内容正确，合法零结果清除对应地域旧缓存。
- 临时副本恢复 `HEAD:provider/scan.go`，五类正式结构异常缓存用例全部变红，复现首屏清空或后续半截覆盖；P3-25 正式 token 回归也变红。正式工作树始终保留修复，无负向控制改写。

**本轮门禁：** 定向 Provider/API 新回归 `-race -count=20` 通过；全量 12 包 `go test -race ./... -count=1 -timeout=20m`、`go vet ./...`、`go build ./...`、`gofmt` 与 `git diff --check` 通过。全量 race 为本轮一次结果，不外推长期稳定绿色。

**保留边界：** 本地 Go 1.26.4/macOS arm64。未执行 Go 1.25/Linux、前端构建、浏览器、Docker、真实云或远端 CI/GHCR；没有真实 SMTP/Webhook/Uptime Kuma 证据。官方资料未明确零资源时是否一定显式返回空数组，真实云若省略/null 集合将按确认策略失败并保留缓存，后续仅基于实际证据调整兼容形式。token 保护只证明重复/环路立即失败，不证明无限新 token 服务有总时限。本项可记录为本地修复完成，不记录为真实云或无保留发布验收完成。

---

### P3 清单（原 25 项 + 新增 P3-26，确认但低风险）

| ID | 结论 | 关键证据 | 备注 |
|---|---|---|---|
| P3-01 | **✅ 已提交 `ac0ee62`**：成功即删除，普通发布按新配置域名复制正数计数 | RecordSuccess 使用 delete；CloneForDomains 遍历配置域名并用 DomainKey 查找正数计数，重建新 map 且不按历史大小预分配；BuildRuntimeState 从 published.DomainRules 提取域名；导入保持 Reset | 成功清理、配置裁剪、阈值与快照隔离、并发回归均覆盖；详见下方 **P3-01 实施补记** |
| P3-02 | **✅ 已提交为 `88154cd`**：轮初已熔断域名每轮协调一次半开探测 | dnsRound 与 DomainKey；正常 attempt 新解析，失败原始错误只在本轮复用；轮末全失败域名加一次 | 计数不随目标/重试数放大，顺序无关；恢复新解析、DNS 失败/零删除与 Dry Run 独立均覆盖，详见下方实施补记 |
| P3-03 | **✅ 已按用户确认的补强方案修复并提交为 `c5cc79d`（2026-09-30）**。历史缺陷：启用且非空 URL 的 `buildPushURL` 失败后，`sendOnce=false` 导致 Run 保持未激活并只等 Wake/Stop，丢失周期检查。普通网络/HTTP/响应失败原本仍按周期继续，不能泛化为全部失败无限静默 | `Pusher.Run` 的失败分支新增 timer/Wake/Stop；timer 到期直接回循环顶部重读配置。`invalidURLRetryInterval` 对非正值回退 60s、正值不足 20s 按 20s，有效长间隔保持原值。真实 timer 用例同时验证周期校验与下限；旧等待逻辑及删除下限的两个 overlay 负向控制均精确失败，修复版通过 | 生产修改仅限 `internal/health/push.go`，测试扩展既有 `push_test.go`；不改 Store、发布顺序、健康/告警、正常发送周期、HTTP 超时或生命周期。定向 race、快速用例 20 轮 race、全量 race 连续 3 轮、vet/build/前端 build 与格式/diff-check 已通过，详情见下方实施补记。关闭/空 URL 保持纯等待；timer 不自动读 SQLite；持续非法地址不会自动变为合法；频繁 Wake 不受全局 WARN 限流。真实 Uptime Kuma DOWN/恢复及远端 CI/GHCR 均未执行 |
| P3-04 | **✅ 已按 B 本地修复并提交为 `ebf8f19`**：日志 SSE 重连按游标增量续传，前端按事件 ID 去重 | `LogBroadcaster.Subscribe(cursor)` 锁内回放入队后注册；实例标识+序号；`reset` 基准事件；`Logs.vue` BigInt 游标与生命周期清理 | 同文不同 ID 保留；缓存过期/实例改变明确重置；定向重复 race、真实 HTTP 流与组件脚本回归通过；真实浏览器未执行，详见下方实施补记 |
| P3-05 | ✅ 已按用户确认的本次方案 B 本地修复；仅本子项收口，I-07 其他事项未修复 | `GET /api/settings` 返回四个凭据的既有合同保持；所有经 `writeJSON` 输出的普通 JSON 成功与错误响应，在提交头前统一设置单一 `Cache-Control: no-store` | 唯一生产文件 `webui/api/deps.go`；两份新增测试与本文、AGENTS 配套回写。SSE 成功 `no-cache`、导出独立 `no-store`、静态响应与 mux 自动错误边界保持。正式门禁与负向控制见下方实施补记；本轮已提交为 `82ad2dc` |
| P3-05（历史研究） | **历史只读研究记录（已由 2026-10-02 本次方案 B 实施替代）**：当时问题真实存在、未修复，继续保留在 I-07。 已修复/不存在结论均不成立。本轮仅更新审计记录，未修改源码、测试或依赖，未运行构建、测试或格式化 | **当前基线**：HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`（**历史基线，非当前 HEAD；当前见头部「最近核验基线」小节**），`main == origin/main`；工作树原有修改仅涉及本报告。`webui/api/deps.go:213-220` 注册 `GET /api/settings`；`webui/api/settings.go:15-20` 定义并在 `:33-50` 复制 `tc_access_id`、`tc_access_key`、`ali_access_id`、`ali_access_key` 到响应，再经 `writeJSON` 返回；`webui/api/deps.go:229-240` 的 `writeJSON` 只设置 `Content-Type`，不设置 `Cache-Control`。全仓生产代码中 `no-store` 仅见于 operational health、alerts、config export 等其他端点，未发现为 settings 补头的全局 middleware。现有 `webui/api/settings_alerts_test.go:13-52` 验证键集合/默认值/凭据回显，`redact_test.go:98-110` 验证既有脱敏契约，但均未判定缓存头。Issue7 已将本项留在 I-07，且 2026-09-29 用户裁决确认本轮越界不修复 | **后续设计与决策项（尚未授权实施）**：**A（推荐）**：仅在 `webui/api/settings.go` 的 `handleGetSettings` 入口设置 `w.Header().Set("Cache-Control", "no-store")`，使成功和数据库读取失败响应都保持禁缓存语义；新增/扩展本地 HTTP 测试精确断言该值。保持四个凭据字段的既有回显契约，不修改通用 `writeJSON`，避免扩大所有 JSON API 的缓存策略；不改前端、存储、配置导入导出或认证边界。**B**：接受现状，依赖浏览器/中间件配置或后续统一 HTTP 缓存策略；不建议，因为当前没有全局补头证据，敏感设置响应仍可能被缓存。是否授权单独处理 I-07/P3-05 需用户决策，本记录不擅自裁决。**影响文件**：实施时必改 `webui/api/settings.go`；建议改 `webui/api/settings_alerts_test.go`（或新增同包测试）。明确不改 `webui/api/deps.go` 的通用 `writeJSON`/路由、`Settings.vue`、数据库 schema、Provider、DNS、告警和配置包逻辑 | **判别性验收**：L0 静态确认 `no-store` 只加在 settings handler，未改变通用 JSON 响应策略、凭据字段或导入导出行为；L1 本地 HTTP 用例同时断言 `GET /api/settings` 为 200、JSON `Content-Type` 不变、四个凭据 sentinel 仍按既有契约返回，且 `Cache-Control` **恰为** `no-store`，并保留失败响应也带该头的控制；L2 运行受影响包测试，随后按项目门禁执行 race、vet、build 与 `git diff --check`；L3 可选地用真实本地二进制 `curl -i /api/settings` 核对最终响应头。当前所有实施/门禁命令均未执行，不得提前写成通过 | **风险、外部边界与停止条件**：风险低，改变仅影响浏览器和中间缓存，可能增加设置页重新请求，但不会移除响应中的凭据、清理 Vue 内存/DevTools/服务端日志，也不替代认证或脱敏。无需真实云、DNS、SMTP、Webhook、Uptime Kuma、浏览器或远端 CI/GHCR；本地 handler/HTTP 测试足以证明本项，浏览器 DevTools 只能补充，不能替代判别性测试。不要额外加入 `Pragma`/`Expires`、移除 GET 凭据回显、统一重构所有敏感 API 或引入认证。如果修复扩大到通用 `writeJSON`、凭据产品契约、认证/会话、配置导入导出或其他 I-07 项，停止并重新确认范围；在用户裁决、判别性 HTTP 回归测试及受影响门禁完成前，保持 P3-05/I-07 未修复，不得把本地通过外推为外部链路通过。 |
| P3-06 | **✅ 已按 B 本地修复（2026-10-02）**：出站不占入站额度，缺失计数停止新增 | `checkRuleLimit` 仅用两个完整入站统计，取其合计与 Ingress 条目数的较大值；不完整统计回退明确数组；无可用计数或缺失响应返回 `ErrSnapshotIncomplete`。官方默认入站/出站各 100 条，见下方补记 | 90 无 WARN、91～100 WARN 且允许、超过 100 整批拒绝；保持 S0 创建版本、S1 删除定位与先增后验。正式 SDK/目标链回归及负向控制见补记；真实云未执行，尚未提交 |
| P3-06（历史订正） | **📚 已由 2026-10-02 官方正文与用户决策替代，以下保留当时判断，不再作为当前合同。🔵 结论已订正（2026-09-30 第二轮核验）：原"实际配额为每方向 100"不成立，"只统计入站"的修法已撤销——现有合计判定与合同及官方定义一致，不属过度保守** | `provider/tc_cvm.go:262-268` 的 `checkRuleLimit` 把 `IngressIPv4+IngressIPv6+EgressIPv4+EgressIPv6` 相加与 100 比较（原引 `:234-241` 为快照行号）。**`AGENTS.md:85` 强要求写"腾讯云 CVM 安全组规则上限 **100 条**"，无方向限定词**；`PlatformAPIDocs/TencentCVMAPIGuide/查询用户安全组配额.md:58-63` 的 `SecurityGroupLimitSet` 只有**单一** `"SecurityGroupPolicyLimit": 100`，无任何方向字段；官方 API 定义该字段为 "Maximum number of rules under the security group"（**安全组内规则上限**，非每方向）。仓内所有"单个方向"表述（`安全组添加规则.md:18`、`删除安全组规则.md:36`、`批量修改安全组规则.md:6`、`替换单条安全组规则.md:6`）均为**请求形态**约束（一次请求只能操作一个方向），**不是配额口径**；`创建安全组和规则.md:19` 还明确"请求中可以同时指定入站和出站" | **订正结论**：合计判定与 `AGENTS.md:85` 及官方定义**一致**，不是过度保守；**撤销"修法：只统计入站计数"**——按原建议改动会使安全组规则总数可能超过云端 100 上限并被 API 拒绝。**残余未知**：若腾讯另存在**叠加**于安全组级上限之外的每方向上限，则可放行更多入站；该口径由 **PT-I7-03**（真实账号）确认。**未取得"每方向 100"的官方依据**：官方"使用限制/配额"叙述页在本环境 DNS 不可达（`cloud.tencent.com`/`www.tencentcloud.com`/`intl.cloud.tencent.com` 等均解析到非公网 IP），本轮仅取得可检索的官方 API 字段定义；**未确认前不改 CVM 代码** |
| P3-07 | **✅ 已按 B 本地清理（2026-10-02）**：删除不可达旧流程，回归迁入生产链 | `retrySync`、`retrySyncDetailed`、`truncateDesc` 与专属 imports 已删除；三份旧测试删除 15 项旧入口回归和专属夹具，12 个目标链场景及共享描述回归正式落地。保留现用重试判定/超时夹具/GetRules/旧 Diff | 当前正式链为 `syncAll → runRound → syncTarget → runTargetAttempt / runTargetCleanup`；机械替换方案已改为按现生产语义迁移。完整对照见 Issue6 §7.9，门禁与负向控制见下方补记；I-09 本地收口，尚未提交 |
| P3-08 | ✅ 已随 P1-02/I-01 修复并提交为 `7aaa3f2` | 原 PID 判活的身份误判与 TOCTOU 已由同一文件上的非阻塞独占 `flock` 取代；真实内核锁屏障、GC、并发、SIGKILL/重启均有判别证据 | 详细实现、重复门禁和 Linux/amd64 Docker 证据见 P1-02 实施补记 | 网络文件系统、旧 PID-only 版本混跑不保证；已有文件/目录/DB 权限迁移仍属 P3-12 |
| P3-09 | **✅ 已按 A 本地修复（2026-10-02），已提交为 `f6b3757`**：DSN 独立 `_txlock=immediate`，在读之前预留写事务 | 当前驱动对 `ReadOnly=true` 跳过 beginMode；正式 Store/协调器先读后写、真实日志/扫描写入、只读并行快照与取消回收均覆盖 | 保持单一 busy_timeout PRAGMA、一次性 WAL、现有发布顺序；5 秒写锁超时仍可能 BUSY，重试不保证成功；见下方当前实施补记 |
| P3-09（历史证据，已由下方实施补记替代修法/状态） | SQLite 写事务为 deferred，先读后写存在 WAL read→write 升级（`SQLITE_BUSY_SNAPSHOT`，`busy_timeout` 不生效）；且注释理由与驱动实现矛盾 | `config/store.go:32-34` `BeginTx(ctx,nil)`；`:77-78` 注释以"会让只读事务申请写锁"为由拒绝 `_txlock`；**但驱动 `modernc.org/sqlite@v1.54.0/tx.go:23` 为 `if !opts.ReadOnly && c.beginMode != ""`——只读事务根本不加 beginMode，该理由不成立** | 影响仅"偶发 500、重试即成功、不损坏数据"。修法：DSN 加 `_txlock=immediate`（`ReadOnly` 事务不受影响），并同步修正注释与 `config/store_dsn_test.go:142` 的断言 |
| P3-10 | **✅ 已按细化后的 A 本地修复（2026-10-03），已提交 `99f3fe2`** | Store 初始化失败统一关闭并合并关闭错误；日志渲染错误向 Handler 返回，失败不更新缓存/序号/投递；同步 SSE 安全 WARN 后跳过坏事件、继续连接；静态 health 写失败 Debug | 四个生产文件、三份新增回归及本文/AGENTS。`bytes.Buffer.Write` 的 error 保证为 nil，渲染是规范性补齐；不将返回 Handler 错误写成自动告警。正式门禁与负向控制见下方实施补记，I-07 其他子项独立追踪 |
| P3-10（修复前历史证据） | **历史记录（由 2026-10-03 当前实施补记替代当前状态）。** **问题真实存在，当前仍未修复；属于独立队列 I-07，不能标记完成。** 原为五类问题、六处返回值处理缺口；I-01 已替换 pidfile 删除并处理 Close 错误，当前剩余四类、五处，不是“均不可失败”或已被其他修复覆盖 | pidfile 子项已由 I-01 收口；`config/store.go:118/124` 两处失败收尾的 `db.Close()` 未检查；`webui/api/logstream.go:97` `_ = h.Handle(...)`；`webui/api/sync.go:127-130` `json.Marshal` 失败静默跳过事件；`webui/server.go:298` 静态 `/api/health` 的 `w.Write` 忽略 | 与 AGENTS §十一“所有 error 必须处理”冲突。SSE 序列化失败会静默丢事件，是影响最实质的一项；其余缺口通常低概率或当前 writer 实际少失败，但仍须补齐可观测错误处理 |
| P3-11 | `StoreLogWriter.OnEvent` 吞掉写库失败（审计快照） | `webui/api/logwriter.go:83-86` 记 WARN 后 `return nil` → `notifier/bus.go:114` 的"事件处理失败"路径**永不可达** | ✅ 已由 Issue7 Step 4 修复：`AddSyncLog` 错误直接返回给 EventBus；保留原行作为历史红灯，不再列入剩余队列 |
| P3-12 | **✅ 已按细化 A 本地修复（2026-10-03），尚未提交；仅本子项收口，I-07 继续未完成。** | 最终目录显式 `0700`；SQLite 首次访问前主库 `0600`，已有 WAL/SHM 打开前及初始化后收紧；缺失不预创建；同 FD 取得 flock 后锁文件 `0600`；任一失败停止启动，初始化后失败统一关闭并保留关闭错误 | 默认目录无法确定时返回错误，不再回退 `.`；显式专用目录不依赖 HOME。正式跨平台 Go1.25 门禁、重复定向 race、真实产品/卷恢复与负向控制见当前实施补记；不递归 chmod、不自动 chown、不改 SQLite/互斥业务语义 |
| P3-12（修复前历史证据） | **以下保留修复前快照，当前状态由上行与实施补记替代。** **真实存在，当前未修复；状态保持“未完成/待独立处理”，对应 I-07。** 数据目录、SQLite DB 及 pidfile 没有应用层权限收敛，不能把既有启动/Docker 证据外推为权限验收通过 | `run.go:41` 使用 `os.MkdirAll(..., 0755)` 且无后续 `Chmod`；I-01 新建锁文件已请求 `0600`，但已有文件未 `Chmod`；`config/store.go:111-128` 直接 `sql.Open`，没有预创建/显式 `Chmod` DB；DB 明文保存云 AK/SK、SMTP 凭据、Webhook URL、Uptime Kuma Push URL。当前仍无已有文件/目录权限迁移；新建锁文件 `0600` 不足以关闭本项 | 同机其他非特权用户可能遍历数据目录或读取明文敏感配置。**既有用户决策仍固定为数据目录 `0700`、数据库和 pidfile/lock 文件 `0600`**；对已存在目录/文件必须显式 `Chmod`，`MkdirAll`/`WriteFile` 的请求模式不能替代迁移收敛。本轮只更新审计记录，未修改源码、测试或其他文档，未构建、未运行测试或格式化 |
| P3-13 | **问题真实存在，当前未修复；继续归入独立队列 I-07。** 本轮仅完成只读核验与本审计条目更新，未修改源码、测试或其他文件，未构建、未运行测试或格式化；不可标记完成。 | **当前基线与精确缺陷链：** HEAD 为 `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树已有唯一 tracked 修改为本报告。`app/logutil.go:28-37` 的 `MultiHandler.Handle` 按顺序处理子 Handler，但 `:31-33` 在第一个启用 Handler 返回错误时立即 `return`，不会继续分发。生产组装顺序由 `run.go:99-100` 与 `app/logutil.go:85-88` 固定为 `stdout TextHandler → WebUI LogBroadcaster`；因此 stdout 写入失败（例如管道断裂）时，WebUI broadcaster、日志环形缓冲与 SSE 订阅者不会收到同一条日志，形成静默丢行。当前 `app/logutil_test.go` 只有日志级别和初始化测试，没有错误注入、继续分发、多错误聚合或 Disabled Handler 语义测试。已修复/不存在核对未发现收口证据；`Issue7.md:584-591` 明确本项是越界的正交问题，AGENTS §十一的 error 处理要求与 MultiHandler 文件归属也不构成已修复证据。另需注意 Go `slog.Logger` 会丢弃底层 Handler 返回的错误，故核心验收必须直接调用 `MultiHandler.Handle`，不能只通过 `slog.Logger` 观察返回值。 | **后续设计与用户决策（尚未授权实施）：** 影响生产代码仅限 `app/logutil.go` 的 `MultiHandler.Handle`，测试限于 `app/logutil_test.go`；`run.go` 的组装顺序、`webui/api/logstream.go`、SSE 协议、前端、日志格式和同步业务均明确不改。**A（推荐）**：继续遍历全部启用 Handler，收集错误；零错误返回 nil，单个错误原样返回，多个错误用标准库 `errors.Join` 返回，以保留单错误身份并让 `errors.Is/errors.As` 识别全部错误；保持现有顺序、`Enabled` 过滤、同步串行、`WithAttrs`/`WithGroup` 行为，不重试、不异步、不在错误路径再次写日志，避免 stdout 失败时递归。**B**：继续调用后续 Handler，但多个错误只返回第一个；实现更小，但丢失后续错误信息和 `errors.Is` 可观测性，不推荐。**C**：继续分发并吞掉/另行记录 Handler 错误；会削弱现有 error 处理契约，且可能形成日志递归，不推荐。当前仅记录 A 为推荐，不擅自替用户裁决或实施。 | **判别性验收、风险、外部边界与停止条件：** L0 静态确认不再在单个 Handler 出错后提前返回，所有启用 Handler 均尝试，多个错误经 `errors.Join` 聚合，未新增递归日志，顺序仍为 stdout 后 WebUI。L1 直接调用 `MultiHandler.Handle` 至少覆盖：前一 Handler 返回 sentinel 错误而后一 Handler 仍被调用并收到原始 Record；两个 Handler 出错且返回值可分别 `errors.Is`；中间 Handler 出错不阻断后续成功 Handler；Disabled Handler 的 `Handle` 不调用且不贡献错误；全成功及全 Disabled 控制组返回 nil。修复授权后再执行 `go test ./app`、`go test ./app -race`，随后按项目门禁执行全量 race、vet、build 与 `git diff --check`；本轮这些命令均未执行，不能预先写成通过。L3 可选用受控 stdout 管道断裂补充进程级证明，但不是必要前置。风险低：`errors.Join` 可能改变多错误 `Error()` 文本；不保留单错误原值会改变兼容性，故 A 原样返回单错误；继续串行调用不解决 Handler 阻塞，也不处理 panic。无需真实腾讯云、阿里云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR，本地 fake `slog.Handler` 已足以证明根因修复；真实 stdout 管道仅是补充边界。若实现改变 Handler 顺序、SSE/前端协议、日志格式、同步业务，加入重试/异步队列，修改 `slog` 递归错误路径，或把本地通过外推为外部链路通过，应立即停止并回到方案审查；在用户决策、判别性测试、受影响门禁和文档闭环完成前，保持 P3-13/I-07 未修复。 |
| P3-14 | **🔵 问题真实存在，当前未修复，继续归入 I-07。** 本轮仅完成只读核验与本审计条目更新，未修改源码、测试或其他文件，未构建、未运行测试或格式化。 | **当前基线**：`main` / HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`（**历史基线，非当前 HEAD；当前见头部「最近核验基线」小节**），`main == origin/main`；本轮前工作树已有修改仅涉及本报告。`webui/api/sync.go:24` 的 `/api/sync/trigger`、`:76` 的 `/api/sync/dryrun` 在 `Syncer == nil` 时返回 HTTP 400；`:42、:61` 的 `/api/sync/pause` 与 `/api/sync/resume` 同样如此；`:94` 的 `/api/sync/events` 在 `EventBus == nil` 时返回 HTTP 400；`webui/api/logstream.go:131` 的 `/api/logs/stream` 在 `LogBroadcaster == nil` 时返回 HTTP 400。这些分支表示服务端依赖未接线或尚未就绪，不是客户端请求格式错误。对照 `webui/api/operational.go:46-52`，`Health == nil` 已按同类不可用语义返回 503，说明仓库已有正确范式。`/api/sync/status` 的 `Syncer == nil` 分支在 `webui/api/sync.go:16` 返回 200 且 `running:false`，并由 `webui/server_test.go:1119-1151` 作为未接入 Syncer 的状态查询场景固定覆盖；本条暂不扩大到该观察接口。现有 `webui/api/sync_test.go`、`webui/api/sse_test.go` 与相关日志流测试没有对上述 nil 依赖精确断言 503。前端 `webui/frontend/src/views/Logs.vue:45-49` 只创建 `EventSource` 并处理消息，没有应用层不可用提示或恢复逻辑；浏览器对该具体非 2xx 响应的重连表现本轮未人工验证。 | **后续最小设计与用户决策（尚未授权实施）**：A（推荐）仅把五个 handler 中六个未就绪依赖分支（trigger、dryrun、pause、resume、events、logs）传给 `writeError` 的状态码从 `http.StatusBadRequest` 改为 `http.StatusServiceUnavailable`，保留现有错误文案、JSON 形状和响应头；补充判别性本地 HTTP 测试。`/api/sync/status` 继续保持 200，Syncer 已接线但暂停的操作仍为 409，Dry Run 冲突仍为 409，SSE 能力不支持仍为 500，正常路径状态码不变。B：把 `/api/sync/status` 的 nil 分支也改为 503；不推荐，会改变已有状态查询兼容语义，应另立状态接口决策。C：同时补前端 `Logs.vue` 的错误提示或 EventSource 重连；可作为独立 UI 议题，不纳入 P3-14 后端最小修复。影响文件限定为 `webui/api/sync.go`、`webui/api/logstream.go` 及 `webui/api/sync_test.go`、`webui/api/sse_test.go`/日志流测试；不改 `writeError`、`webui/api/deps.go`、`run.go`、`operational.go`、前端、Provider、SSE 生命周期或重试策略。是否授权 A 需用户决策，本条不擅自裁决。 | **判别性验收、风险、外部边界与停止条件**：L0 静态确认上述六个 nil 分支均为 503，`/api/sync/status` 仍为 200，其他 409/500/200 分支未改变。L1 使用 `Deps{Syncer:nil}` 分别请求 `POST /api/sync/trigger`、`/api/sync/dryrun`、`/api/sync/pause`、`/api/sync/resume`，使用 `Deps{EventBus:nil}` 请求 `GET /api/sync/events`，使用 `Deps{LogBroadcaster:nil}` 请求 `GET /api/logs/stream`；每项精确断言 503、`application/json; charset=utf-8` 与 `{"error":"..."}` 形状，并证明不访问 Store/协调器、不调用 `probeSSE`、不建立订阅。保留正常 Syncer 触发 202、暂停触发 409、正常 pause/resume 200、正常 SSE 200、SSE 能力缺失 500、nil Syncer 的 status 200 等负向控制。L2 在获得授权后执行受影响包测试，再按项目门禁执行全量 race、vet、build 与 `git diff --check`；当前这些命令均未执行，不能预先写成通过，也不得以单次绿色外推为稳定绿色。L3 可选地用本地真实二进制和 `curl -i`核对最终状态码与 JSON 头部。风险低，主要是让调用方正确识别服务端不可用，前端仍可能因缺少应用层 `onerror`/恢复 UI 而只显示空日志；浏览器人工回归仅补充用户表现，不是后端关闭必要条件。该项不依赖真实腾讯云、阿里云、DNS、SMTP、Webhook、Uptime Kuma、Docker 或远端 CI/GHCR。未完成上述 nil 依赖判别性测试和受影响门禁前保持 P3-14/I-07 未修复；若扩大到 `/api/sync/status`、通用错误封装、运行时接线、前端重连、`Retry-After` 或 SSE 生命周期，或把浏览器未验证行为写成确定结论，应立即停止并重新审查范围。 |
| P3-15 | 被丢弃的告警事件**每条约一条 WARN**，故障期日志洪水 | `notifier/inflight.go:62-67` `logDropped` 对每个被丢弃事件都记 WARN；`notifier/bus.go:113` 每事件每订阅者一个 goroutine | 500 个 DNS 失败事件 → 约 496 条 WARN。量级不大且信息有用，属可聚合项 |
| P3-16 | 前端 UI/状态偏离（AGENTS §十一）——**6 子项中 1 项已修复** | ✅ ~~`RunTest.vue:32` 缺 `size="large"`（唯一漏网，**属明确漏改**）~~ **已修复**：`RunTest.vue:32` 现有 `size="large"`（与 §0.2 一致）；其余 5 子项仍成立：`App.vue:10/67` 侧边栏高亮不随路由（刷新/深链后停在"仪表盘"）；`Targets.vue:106-121/210`、`Rules.vue:71-84/165` 保存按钮无 in-flight 守卫（双击产生重复行）；`Settings.vue:127-141` 把不可见的 `theme` 当隐藏字段回传（与侧边栏主题开关**双写**，导致主题静默回退）；`useScannedResources.ts:36-41`（`catch { /* 失败静默 */ }`）+`Settings.vue:98-105` 清空扫描失败仍无条件提示"已清空"；`Settings.vue:14` 前端正则 `/^\d+(ms|s|m|h)$/` 比后端 `time.ParseDuration` 更严（合法 `1h30m` 被拦） | 均为可静态判定；`theme` 双写与"清空误报成功"影响用户可观察状态 |
| P3-17 | 陈旧 "version 2" 表述残留（核验后为 **10 处 / 10 个测试文件**；全仓 `_test.go` 内共 14 处命中） | 陈旧标签：`config/runtime_test.go:27,66`、`main_test.go:732`、`webui/api/testenv_test.go:216`、`import_runtime_test.go:90`（原引 `:82`）、`import_test.go:11,20,111`、`redact_test.go:111,116`。**已修复**：`export_test.go:461` 已不含该串。**属合法负向控制、不应改**：`main_test.go:907,912`（断言 version 2 导入必须 400）、`alerts_v3_test.go:14,154`（历史说明 + 真实 `{"version":2}` 拒绝夹具） | `import_test.go:111` 仍用 map key `"version 2"` 承载 `{"version":3,...}`，属陈旧标识 |
| P3-18 | 文档漂移（非强制文档） | **Step 0 部分收口：** `Design5.md` 当前配置包已改为 version 3，Build7 已完成状态与 `ProdTestList.md` 范围矛盾已收口；`Build6.md`/`Issue5.md` 历史措辞、`Issue6.md` 基线对账与测试内 version 2 注释等仍是剩余文档清理 | 与 P1-01 直接冲突的部分已修；其余不是 Issue7 Step 1 前置 |
| P3-19 | 邮件 AUTH 失败保留 SMTP 诊断文本（含 535 回显） | `notifier/email.go:219-221` `%w` 包装；`webui/api/test_email.go:78-82` 回给浏览器；`notifier/email_test.go:293-295` **显式断言**保留诊断 | 理论风险：若服务器回显 AUTH 载荷可间接泄露 base64 凭据（**未验证**；标准 SMTP 不会这样做）。属可调试性取舍 |
| P3-20 | 邮件头/正文直发原始 UTF-8（无 RFC 2047 头部编码、无 CTE 声明） | `notifier/email.go:243-244`；`config/validate.go:307-316` 已排除头部注入 | 默认配置即中文主题/正文。多数现代 MTA 可正常投递；**真实表现必须由真实收件箱验证**（未执行） |
| P3-21 | **两子项状态不同，须分别处理：** ① Webhook 响应体未 drain——**部分已修复**；② Push 响应体解析无字节上限——**未修复** | ① `notifier/webhook.go:139` 现经 `io.LimitReader` 读取至 EOF（`93e0e4b`），**≤16 KiB 的 2xx 响应已隐式 drain、连接可复用**；仍不 drain 的只有非 2xx（`:136-138` 在任何读取前返回）与 >16 KiB（`:143-145` 只读 16385 字节）。② `internal/health/push.go:263`（原引 `:247`）`json.NewDecoder(resp.Body).Decode(&payload)` **无 `io.LimitReader`**，`internal/health/` 内 `LimitReader/MaxBytesReader/ReadAll` 零命中，仅有时间上界（`:94` client timeout 与 `:221` 每请求 context deadline，10s） | ① 残余影响收窄为"非 2xx 与超限响应可能多一次握手"；② Push 内存峰值仍不受字节数约束。**不得把 ① 的修复外推为 P3-21 整体完成**。本轮已为子项①补一条此前缺失的判别性用例：`TestWebhookResponseSlotHeldUntilClose`（`notifier/webhook_response_test.go`）断言「名额保持到响应体读取与关闭都结束」——闸门阻塞读取期间 `InFlight()==1`、放行并返回后 `InFlight()==0` 且响应体已关闭。**判别力已验证**：把 `defer release()` 改为读取前提前释放后该用例精确失败（`响应体读取期间在途名额应为 1，实际 0`），恢复后通过；`-race -count=20` 通过。子项②（Push 无字节上限）本轮**未修复**、未补测 |
| P3-22 | Dry Run 对每条规则各发一次 `GetRules` 并 sleep 一个厂商限速间隔（审计快照） | `syncer/syncer.go:604-640` 内层 for rule 里 `p.GetRules()` + `time.Sleep(rateLimitInterval(...))`，注释写"与 syncAll 一致"（实际 `syncAll` 每 provider 只 sleep 一次，`:805-817`） | ✅ 已由 Issue7 Step 4 修复：Dry Run 按目标取一次完整快照并在内存中规划，目标间保留限速；原 20 条规则约 100 秒/20 次 Describe 的风险仅作历史证据 |
| P3-23 | **核心问题真实存在，当前未修复；审计条目的两条附加依据需要降级或删除。** `true → true` 分支无条件 `ticker.Reset(latest.Config.Interval)`，因此不影响调度的主题、告警或其他配置保存也会重新计时；保存频率持续高于 interval 时，周期同步可能被无限推迟，并进一步触发运行健康的“距最近完成时间超过 `interval + health_timeout`”判据。`false → false` 对已停止 ticker 调用 `Reset` **不是 Go API 误用**；这是 Go 支持的重新激活方式，但暂停期间没有必要做此操作。按项目 `go.mod` 的 Go 1.25 合同，`Stop`/`Reset` 后的同步 ticker channel 语义也不支持直接断言“旧 tick 必然残留”，因此 stale-tick 子结论目前证据不足。状态：**部分成立，当前不能关闭**。 | **真实缺陷证据：** `syncer/syncer.go:295-297` 的 `true → true` 无条件 Reset；`ApplyState` 每次状态发布都会通知 Run（`:133-147`），普通目标、规则、settings、alerts、pause/resume 写入口经协调器 commit 后最终调用 `Syncer.ApplyState`（`webui/api/coordinator.go:71-87`、`webui/api/deps.go:130-161`）。当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`（**历史基线，非当前 HEAD；当前见头部「最近核验基线」小节**）、`main == origin/main`；工作树原有未提交修改仅为本报告，源码/测试未由本轮修改，本轮未运行构建、测试或格式化。`go.mod:3` 为 Go 1.25.0；停止后可 Reset 见 [Go time.Ticker 文档](https://pkg.go.dev/time#Ticker.Stop)，Go 1.23 起同步 timer/ticker channel 的 stale value 语义见 [Go 1.23 release notes](https://go.dev/doc/go1.23)。现有 `syncer/state_test.go:168-191` 只证明 interval 改变后会按新 interval 触发，不能证明同 interval 更新不会重新计时。 | **后续最小设计（推荐选项 A）：** 只改 `syncer/syncer.go` 的局部调度状态，创建 ticker 时保存 `tickerInterval := state.Config.Interval`；`false → true` 始终按最新 interval Reset、更新 `tickerInterval` 并保留恢复后的立即同步；`true → true` 仅当 `latest.Config.Interval != tickerInterval` 时 Reset 并更新记录；`true → false` Stop；`false → false` 不操作 ticker，暂停期间的最新 interval 由恢复时使用。不要加入 stale-tick drain、`time.Timer` 重写或时钟抽象。测试重点为 `syncer/state_test.go`：同 interval 的连续 ApplyState 不重新计时；高频保存非调度配置时周期同步仍发生；interval 实际变化仍重新计时；暂停期间修改 interval 后恢复使用最新值；保留 P3-24 的 pause→resume 通知合并控制。受影响范围限定为 `syncer/syncer.go` 与调度测试；不得修改 Provider、RuntimeManager、ConfigCoordinator、OperationalHealth、ApplyState 通知合并机制或外部链路。**选项 B**：额外承诺 `GODEBUG=asynctimerchan=1` 旧 timer channel 兼容，需要定义支持范围、补兼容测试并重新设计 Stop/Reset 后处理，超出本项最小修复范围，暂不推荐。 | **判别性验收与停止条件：** 静态确认 `true → true` 不再无条件 Reset、`false → false` 不再 Reset、`false → true` 仍 Reset，且没有新增 stale-tick drain；保留 P3-24 独立未处理。定向用例至少覆盖：①同 interval 更新后下一轮按原 ticker 剩余时间到期，而非从保存时重新等待完整 interval；②保存频率高于 interval 时仍能发生 ticker 驱动同步；③interval 改变时按新值重新计时；④暂停期间改 interval、恢复后立即同步且后续使用最新值；⑤旧语义下相应测试能判别失败。修复授权后再执行 `go test ./syncer -race -count=1`、`go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 及项目要求的多轮门禁；本轮均未执行，不能预先写成通过。若修改生产范围超出 ticker 局部状态、引入未裁决的旧 timer 兼容承诺、把 P3-24 合并处理、误删恢复立即轮、比较初始而非当前实际 interval，或把单次绿色外推为稳定绿色，应立即停止并回到方案审查。**外部边界：** 本项仅依赖本地 Go 1.25、Syncer、ticker 和测试 Provider；不依赖真实腾讯云/阿里云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR。本地通过只能证明当前 Go 合同下的调度行为，不能外推真实外部链路已验收。 |
| P3-24 | **真实存在，当前未修复；继续保留在独立队列 I-06。** 本轮仅补充审计记录，未修改源码或测试，未构建、未运行测试。轮内 `pause → resume` 的控制通知可被容量为 1 的 `controlCh` 合并，导致 Run 只观察到 `true → true`，恢复不触发按合同要求的立即同步一轮。 | **当前基线与缺陷证据：** HEAD 为 `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树既有修改仅为本报告。`syncer/syncer.go:133-147` 的 `ApplyState` 发布新状态后向容量为 1 的 `controlCh` 非阻塞投递，已有通知时合并；`:217-221` 的 Run 使用本地循环相位 `enabled` 形成 `wasEnabled`；`:270-305` 只在 `false → true` 分支立即调用 `syncAll()`，`:295-297` 的 `true → true` 只 Reset ticker。可达交错为：当前相位为 `true` 时，在 Run 消费前连续 `ApplyState(false)`、`ApplyState(true)`；RuntimeManager 最终只读到 `true`，于是得到 `true → true`，恢复轮被静默跳过。暂停/恢复入口分别为 `webui/api/sync.go:35-52`、`:55-72`，均经 `webui/api/deps.go:122-161` 的协调器发布；协调器只串行化单次提交，不能保证 Run 在两次提交之间消费第一条通知。Build6 §12.5 要求 `false → true` 立即同步；Issue6 A7 修复的是已发布镜像先推进导致恢复边沿丢失的另一种交错，不覆盖本项。现有 `syncer/state_test.go:147-166` 只测初始 `false → true`，`:168-191` 只测正常 `true → true`，`syncer/syncer_test.go:197-237` 使用固定等待，均不能判别本项。 | **后续设计与待决策选项：** 需要在不恢复完整中间状态队列的前提下，保留最终状态语义并记住被合并的恢复边沿。**A（推荐）**：在 `Syncer` 内以 `s.mu` 保护 `resumeGeneration` 与 `handledResumeGeneration`；`ApplyState` 在线性化发布 `next` 时仅对真实 `false → true` 递增 generation，Run 消费控制通知时同锁取得最新 `RuntimeState`、读取并标记已观察 generation。最终 `Enabled=false` 时绝不启动同步；最终为 `true` 且存在未处理恢复 generation 时补发恰好一轮立即同步；普通 `true → true` 仍只按既有 ticker 规则处理，保留 `controlCh` 容量 1 合并语义。**B**：把控制通道改为携带每次状态或边沿的显式事件并逐条排队；语义直观但可能执行已过期的中间状态、扩大队列/生命周期范围，当前不推荐。**C**：仅增加 `pendingResume bool`；改动最小但在多个连续恢复边沿、消费与发布并发时更难证明不丢失或重复，除非补足线性化合同，当前不推荐。上述为后续设计，不是本轮实施授权；需用户在 A/B/C 中裁决后再改代码。 | **影响文件、判别性验收、风险与停止条件：** 生产影响应限定为 `syncer/syncer.go` 的状态字段、`ApplyState`、Run 控制消费与必要中文注释；测试影响为 `syncer/state_test.go`，必要时新建 `syncer/control_transition_test.go`。原则上不改 `RuntimeManager`、`ConfigCoordinator`、`webui/api/sync.go`、Provider、DNS、告警、OperationalHealth、Issue7 目标级状态机或外部 API。必须补确定性用例：①当前轮被 Provider 阻塞时连续 `true → false → true`，放行后断言首轮之外恰好再执行一轮立即同步；②连续 `true → false → true → false` 后最终暂停，断言不启动第二轮；③普通 `true → true` 不触发恢复轮；④正常单次 `false → true` 仍只触发一轮；⑤既有 queued trigger/paused ticker 回归继续成立。用 Provider 阻塞与 release channel 控制时序，不以固定 `Sleep` 猜竞态。主要风险是恢复 generation 被提前清除、最终暂停仍误启动、ticker/trigger 与恢复边沿重复跑两轮，或为保留中间状态扩大为无界队列；这些任一情况均应停止并回到方案审查。该项只依赖本地 Syncer、容量为 1 的控制 channel、可控 Provider 与确定性屏障，不依赖真实云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR。取得用户裁决、判别性回归测试、`go test ./syncer -race -count=1`、全量多轮 race、`go vet ./...`、`go build ./...`、gofmt/diff-check 及文档闭环前，保持 P3-24/I-06 未修复；不得把单次本地绿色或 P3-23 的修复外推为本项关闭。 |
| P3-25（同步路径） | ECS 同步 `NextToken` 分页缺"token 未推进即退出"守卫（审计快照） | 审计快照 `provider/ali_ecs.go:96-102` 仅判空不判重复（**当前实现为 `:107-113`**：未推进与历史重复 token 均返回 `ErrSnapshotIncomplete`） | ✅ 已由 Issue7 Step 1 修复：重复/未推进 token 返回 `snapshot_incomplete`，目标失败且本 attempt 零删除；不再列入待修复 |
| P3-25（资源扫描路径） | ECS 扫描缺 token 推进与环路保护 | scanAliECS 历史 token 集合；异常返回 nil 资源与 ErrSnapshotIncomplete | ✅ 已本地修复，尚未提交；I-19 独立回归与缓存整链证据 |
| **P3-26（新增）** | ECS 扫描结构异常被当成功并覆盖缓存 | 原缺失集合 break 返回半截列表；首屏缺失也可清空旧缓存 | ✅ 已本地修复，尚未提交；decodeECSScanPage + 实际 API/SQLite 回归，兼容性边界见 P3-26 |

> 说明：第 9 节的决策表使用 #1..#7 编号（你实际决策的 7 项）；本 P3 表使用 P3-nn 编号，两套编号相互独立。P3-08 已合并原先拆分的两类 pidfile 失效（TOCTOU 漏判 / PID 复用误判），实施时由 flock 一次解决。

**P3-09 当前实施补记（2026-10-02，方案 A：SQLite 写事务提前预留）：**

- **问题与再次核对**：实际规则新增/修改先 `ValidateRuleTargetsTx` 读 targets，目标删除先 `ReferencingRuleIDsTx` 读 rules；独立日志/扫描写入不受配置协调器锁约束。旧 deferred 事务读完后，即使另一连接只提交 sync_logs，也会使 WAL 快照升级失败并返回 517。固定驱动 `modernc.org/sqlite v1.54.0/tx.go` 仅在 `!opts.ReadOnly` 时使用 beginMode；Issue6 A4 与旧源码注释拒绝 `_txlock` 的理由不成立，用户已选择 A 替代该历史理由。官方机制见 [SQLite 隔离说明](https://sqlite.org/isolation.html)。历史“重试即成功”不是保证；本问题失败会回滚且不发布候选，不损坏数据。
- **生产改动**：仅 `config/store.go` 的 DSN 由 `?_pragma=busy_timeout(5000)` 改为 `?_pragma=busy_timeout(5000)&_txlock=immediate`，并修正 BeginTx/sqliteDSN 注释。非 ReadOnly 的 Begin/BeginTx（含配置、迁移、扫描事务）统一提前预留写锁；导出与启动快照仍 deferred。WAL 仍只在打开后设置一次，原路径转义和每连接 5000ms 保持；不修改 schema、依赖、连接池、事务回调/发布、API/前端、Provider/DNS/健康或添加重试。
- **正式测试**：DSN 改用结构化 query 检查，恰含单一 busy_timeout PRAGMA 与独立 immediate 参数。新增 `TestStoreWriteTransactionReservesLock` 与 `TestStoreWithTransactionReservesLock` 用不同真实连接及零等待外部写入证明预留写锁、同事务读后写成功、提交后外部可写；`TestStoreBeginTxWaitsForWriter`/`TestStoreBeginTxBusyTimeoutAndRecovery` 验证入口等待、约 5 秒后 BUSY(5) 与后续事务恢复；`TestStoreReadOnlySnapshotDuringWrite` 验证持写锁时完整业务快照可读、写提交时读事务仍保持旧快照、新读取看到新值；`TestStoreBackgroundWritesWaitForCommit` 走真实 AddSyncLog 与 ReplaceScannedResources，保证两者提交后完整落库。`TestCoordinatorReadThenWriteReservesLock` 走真实引用检查→AddRuleTx→候选构造→commit→一次发布，断言数据库/运行时规则一致；`TestCoordinatorCanceledLockWait` 取消受阻请求后零提交/零发布，后续合法请求成功，未固定毫秒级取消时限。
- **三类正式负向控制**：仓库外 Go overlay 恢复 HEAD 的旧 store.go，DSN/两种写事务/协调器回归分别按断言变红，明确复现 517 与 apply=0；把 ReadOnly 错改 false，快照并行回归返回 BUSY(5) 变红；把 busy_timeout 改 0，入口超时用例在不足 1ms 返回 BUSY、因等待下限变红。均为运行期行为失败，不以编译失败冒充判别力，正式源码未被负向控制改写。
- **本轮门禁**：定向新回归（排除约 5 秒慢例）及 DSN 检查 `-race -count=20` 通过；入口超时/恢复定向 race 通过（约 5.062s）；受影响 config/API 完整 race 各一轮、`go vet ./...`、`go build ./...` 通过。全量 12 包 `go test -race ./... -count=1 -timeout=10m`、受影响 Go 文件 gofmt 检查与 `git diff --check` 均通过；单次全量绿色不外推长期稳定。证据日志保存在仓库外 `/tmp/fwalizer-p309-research.6VXRZZ/formal-*.log`，研究期候选日志不充当正式门禁。
- **保留边界与状态**：提前预留的写锁持续到 commit/rollback，日志/扫描可能更早等待；候选构造仍限本地对象、不访问网络。占锁超过 5 秒仍可普通 BUSY/API 500，不保证全部锁错误消失或重试成功。当前驱动不承诺锁等待按 context deadline 立即返回；取消零提交/零发布已验证。ReadOnly 不是驱动强制拒写机制，导出只执行读取的既有合同保持。Go `1.26.6 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，不外推长期稳定或外部验收。P3-09 本地子项收口，I-10 其余事项继续独立追踪、整条不勾选。源码/测试/文档尚未提交，未 fetch/push。

**P3-07 当前实施补记（2026-10-02，方案 B：删除旧流程并按现生产语义迁移回归）：**

- **原问题与再次核验**：基线 `c08f2e1`。旧 `retrySync` 的 15 个调用全部来自三份测试文件；`retrySyncDetailed` 只有旧 wrapper 与一个测试调用，`truncateDesc` 只有旧链/测试调用。现生产链从 Run 经 syncAll/runRound 进入 syncTarget，Dry Run 使用 PlanTarget，不执行旧流程。旧实现确实包含先删后加、空 RuleSnapshot 写入与缺少 S1/S2 验证，但这些不是当前生产行为；本项解决维护和测试可信度。
- **方案更正与实施**：审计原建议“机械替换测试调用”升级为研究推荐 B，依当前合同迁移回归并复用既有覆盖。唯一可执行生产源码变更为删除 `retry.go` 的三个不可达函数；`provider/tc_lighthouse.go` 仅更正描述注释。旧算法没有搬入 `_test.go`，没有新增 adapter、框架或依赖。正式范围固定 11 文件，与头部清单一致。
- **测试取舍**：三份旧测试删除 15 项旧入口测试、5 种专属 Provider 夹具、localhost 解析辅助及无消费者的夹具字段/方法。过期的空 DNS 清理、先删后加、仅凭 Provider.Skipped 认定成功和所有 Delete 失败均 failed 等断言不迁移。原本走 syncAll 的 TAG 重试快照测试保留为 `TestSyncRound_TagSnapshotAcrossRetry`；有效意图逐项迁移/复用矩阵见 Issue6 §7.9。
- **新正式回归**：`syncer/target_retry_test.go` 中五个函数共 12 场景。SnapshotFailures 的四类错误断言重新 DNS、S0/S1 次数、写入、最终 outcome 与 1s/2s 退避；WriteAccounting 的四场景刻意拉开请求数与 Written，覆盖部分创建后重试/停止、Provider 跳过但 S1 无覆盖的 failed，以及外部满足但当前 Written=0 的 success；UnsupportedFinalAttempt 只保留最终 attempt 一条 canonical 平台限制；IdempotentCreateRequiresCoverage 证明已存在错误不能替代 S1；DeleteProgressAcrossAttempts 两场景证明部分删除/S2 失败后恢复或 S0 耗尽仍保留确认增删数。
- **共享描述与保留控制**：原两项截断回归归位 `provider/plan_test.go` 的 `TestTruncateDescription_*`，新增四平台 `TestRenderDescription_EmptyComment`，覆盖中文/上限/短描述、CVM/ECS 不截断及 48 rune TAG 完整前缀。`maxRetries` 与现用错误判定函数逐字保留；R7-06 的真实 HTTP 超时/强引用连接回收、R7-07 夹具、GetRules（`webui/api/targets.go` 连接测试调用）、旧 Diff/P0-01、planner 端口收敛、目标安全门、幂等 NotFound/S2 与 TAG/Provider 快照回归继续保留。
- **六类正式负向控制**：使用正式路径的 `go test -overlay`，分别把 Written 换为请求数、覆盖前序 added、覆盖前序 deleted、跨 attempt 追加 unsupported、跳过 S1 覆盖判定、不再重试。对应 WriteAccounting（含 partial_then_retry）、DeleteProgressAcrossAttempts/s2_fail_then_recover、UnsupportedFinalAttempt、IdempotentCreateRequiresCoverage、SnapshotFailures/real_http_timeout 都按预期产生行为断言失败；未以编译失败充当判别力。overlay 只在仓库外生效，`target.go` 正式源码未修改。
- **正式门禁**：`go test ./syncer ./provider -race -count=20 -run '^(TestTargetRetry_|TestTruncateDescription_|TestRenderDescription_)' -timeout=5m` 通过；随后受影响两包完整 race 一轮（syncer 36.007s）、全量 12 包 `go test ./... -race -count=1 -timeout=10m` 全部 ok、`go vet ./...`、`go build ./...`、受影响 Go 文件 gofmt 与 `git diff --check` 通过。六类负向控制全部通过判别力核验。受影响整包/全量/静态门禁与负向控制日志位于仓库外的 `/private/var/folders/1x/ngps27d54l1791wzj_qlqv980000gn/T/fwalizer-p3-07-implementation-rebi22fq` 目录；本轮没有修改依赖或重建前端 dist。
- **独立观察（未定案，不并入 P3-07）**：部分删除 1 条后 S2 返回可重试错误，后两次 S0 同样失败，最终 failed 仍保留 added=1/deleted=1/cleanup_deleted=1；但 `syncTarget` 后续空 attempt 覆盖前次 `cleanup_candidates/deferred`，最终两者为 0。应保留前次可信残留还是表达“未知”尚需独立裁决。新回归对失败路径只断言确认累计数，不把残留 0 固定为正确语义；生产状态机零改动。
- **状态与边界**：P3-07 / I-09 本地收口，源码/测试/本轮文档尚未提交，未 fetch/push。Go `1.26.6 darwin/arm64`；既有 ignored dist 供 embed 使用。未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实腾讯云/阿里云、SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR，不外推长期稳定或外部通过。不可达代码清理不新增 ProdTestList 人工项目，现有未执行/免除边界继续保留。

**P3-06 当前实施补记（2026-10-02，方案 B：CVM 入站计数与完整性判断）：**

- **授权与基线**：引用聊天「研究并修复 P3-06」已完成方案 B 定型和九文件候选准备，本聊天按用户“检查完改动内容后执行修复并更新文档”的明确授权实施。开始时 `main / 82ad2dc`、本地 `origin/main / ac0ee62`、ahead 3、工作树与暂存区干净；未 fetch、提交或推送。P3-05 已提交 `82ad2dc`，旧文档中该批次“尚未提交”属于当时记录。
- **依据与再次更正**：研究和本轮均直接取得 [CVM 安全组规则问题](https://cloud.tencent.com/document/product/213/43699) 与 [使用限制总览](https://cloud.tencent.com/document/product/213/15379) 正文，明确默认入站/出站各 100；工单可提额。单一 `SecurityGroupPolicyLimit` 的泛称未定义双向合计，不能支撑 2026-09-30“合计不是过度保守”的推断；当时无法取得正文的执行事实及当时判断保留为历史，当前结论由此补记替代。入站 80、出站 100、本次新增 1 条现在允许；入站 99+1 允许，100+1 仍拒绝。
- **正式范围**：唯一生产逻辑文件 `provider/tc_cvm.go`，修改 `checkRuleLimit` 的方向、计数可用性、数组下界、错误/WARN 文案，并删除失去用途的 `uint64Val`；创建方法仅配套更正注释。`syncer/retry.go` 只改上限错误示例注释，`isRetryable` 函数及其后源码逐字节保持。测试为 `provider/request_mock_test.go`、`syncer/retry_test.go`、`syncer/target_test.go`，文档为本文、AGENTS、Issue7、ProdTestList，共九文件。未修改 GetSnapshot、通用快照/DTO、planner、target.go、DNS、其他云 Provider、API/schema、前端、依赖或生产超时。
- **计数矩阵**：只需两个入站统计字段都存在，不要求出站统计；完整统计与数组同时返回则取 `max(入站 IPv4 + 入站 IPv6, len(Ingress))`。完整统计可独立使用，显式 0+0 是有效零计数；统计缺失/null/部分字段缺失或 null 时只回退明确数组（`[]` 有效）。无可用统计且数组省略/null、或响应/Response/规则集合缺失时返回包装 `ErrSnapshotIncomplete` 的错误，停止新增。SDK slice 无法区分省略与 null，本次一致按 nil 处理。数组包含手工/其他 TAG/模板等所有入站条目，不按所有权过滤，不扣除待删条目。数组作为下界是本地保守策略；[官方查询示例](https://cloud.tencent.com/document/product/215/15804) 中模板条目非空而统计为零仅支持这项保护，不证明真实模板统计机制已验证。
- **阈值与同步边界**：预计新增后 90 不 WARN，91～100 WARN 且允许，超过 100 整批拒绝，失败 Written/Skipped 均为 0。无新增不作任何云请求。配额 Describe 不带过滤、只保护数量，其 Version 不替代创建所用 S0；删除继续用同一 S1 的 PolicyIndex+Version。满额 IP 轮换无法在本地保护内先增时保持 failed、保留旧权限，不先删腾位。云端配额错误维持不重试，Version mismatch 仍从 DNS/S0/规划开始整目标重试；不扩大生产错误分类。
- **正式回归**：`TestRequest_CVMIngressCapacity` 的 26 个计数场景经真实腾讯 SDK→本地 HTTP mock，覆盖双栈、出站满额、恰好 100/整批 101、数组回退、模板下界、缺失/null/部分统计及缺失 Response/集合；允许路径验证配额返回版本 999 不替代 S0 版本 7、只写 Ingress，拒绝路径验证零创建。`TestRequest_CVMCapacityWarnings` 的 4 个边界验证准确 WARN 字段；`TestRequest_CVMCapacityCloudErrors` 的 3 个场景保留 Describe 权限错误、Create 配额错误与版本竞争，失败零计数；`TestRequest_CVMNoAddNoNetwork` 固定零新增零请求。`TestTargetRound_CVMCapacityReject` 使用探针 Provider 经正式 `syncAll→syncTarget` 验证本地上限/计数缺失/云端配额错误单 attempt、failed、零增删且旧规则保留；`TestTargetRound_CVMCreateVersionMismatch` 验证重新解析、重取 S0、重规划后成功。前者为 SDK 请求证据，后者为正式目标流程与模拟 Provider 的组合证据，均不等于真实云。
- **正式负向控制**：基于已落地的正式测试使用仓库外 Go overlay，依次恢复 HEAD 旧实现、去掉数组下界、空集合放行、配额版本替代 S0、100 即拒绝、90 即 WARN；六类全部退出码 1，均由预期测试断言变红而非编译失败。仓库文件未被回退；修复版正式新回归 race 20 轮通过。材料存于 `/var/folders/1x/ngps27d54l1791wzj_qlqv980000gn/T/fwalizer-p306-implementation-nlw_srcm/negative-controls.json` 及同目录日志；仓库外准备/研究证据未冒充本轮正式证据。
- **本轮门禁**：新增 SDK/目标链回归与既有立即停止控制 `-race -count=20 -timeout=20m` 通过（provider 3.516s、syncer 2.011s）；受影响两包完整 `go test ./provider ./syncer -race -count=1 -timeout=20m` 通过（3.528s / 46.544s）。全量 12 包 `go test ./... -race -count=1 -timeout=20m` 全部 ok（syncer 46.980s、internal/health 46.161s）；`go vet ./...`、`go build ./...`、五个受影响 Go 文件 `gofmt -l`（无输出）与 `git diff --check` 通过。全量仅一轮，20 轮仅定向用例，不外推全仓长期稳定绿色。
- **保留边界与状态**：Go `1.26.6 darwin/arm64`，使用既有 ignored 前端 dist；未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。真实零规则若同时省略数组与统计将被保守拒绝新增；真实模板统计与账号提额情况仍待 PT-I7-03（继续未执行）。本工具固定入站 100 本地保护，不动态适配提额；P3-06 本地子项收口、I-10 其他子项未修复，整条不勾选。源码/测试/本轮文档尚未提交。

**P3-05 实施补记（2026-10-02，本次方案 B：统一普通 JSON 响应禁缓存；其后已提交 `82ad2dc`）：**

- **授权、基线与命名**：用户确认 B 并授权正式修复与文档回写；开始时 `main / ebf8f19`、本地 `origin/main / ac0ee62`、ahead 2，工作树干净。本次 B 明确指在 `writeJSON` 统一设置 `no-store`，与上表历史“B：接受现状”含义不同；用户本次决策替代历史仅改 settings 的范围及其“不得改共同出口”的停止条件。历史研究行只作追溯。
- **实际改动范围**：唯一生产文件为 `webui/api/deps.go`，在 `WriteHeader` 前无条件 `Set("Cache-Control", "no-store")`，补中文注释。配套新增 `webui/api/cachepolicy_test.go`（264 行）与 `webui/server_cachepolicy_test.go`（43 行），回写本文与 AGENTS，共五文件。保留既有 alerts/operational/export 显式缓存头；状态码、DTO、Content-Type、JSON 编码及写出失败日志保持。未修改前端 Fetch/内存缓存、settings handler、SQLite/schema、配置包、协调器、Provider/DNS、健康计算、通知、超时、依赖、路由或 SSE 生命周期。
- **调用链与边界**：31 个业务路由中 28 个正常响应走 `writeJSON`；`writeError` 及请求/事务/内部错误包装共用出口，含业务错误及 SSE/导出早期 JSON 错误。两类 SSE 成功流继续 `text/event-stream + no-cache`，配置导出成功仍直接输出附件并独立 `no-store`；静态资源、静态 `/api/health`、mux 自动 404/405 不经过共同出口。不是“全部 HTTP 响应都禁缓存”，绕过 helper 的未来响应仍须单独设计。
- **正式回归**：`TestJSONCachePolicyCommittedHeaders` 检查 `Recorder.Result()` 已提交头，201/413 均覆盖预先设置的 `public, max-age=60`，精确要求仅一个 `no-store` 并保留载荷。`TestJSONCachePolicySettingsHTTP` 使用真实本地 HTTP，覆盖成功/数据库失败 × GET/HEAD，验证四个凭据 sentinel 原样回显、11 个字段、失败响应无 sentinel、Content-Type 与 HEAD 零正文。`TestJSONCachePolicyAPIMatrix` 覆盖八类 GET、201 创建及实际 400/404/409/413/500/503、导出附件和 alerts/export 数据库错误。`TestJSONCachePolicyBoundaries`/`TestJSONCachePolicyServerBoundaries` 验证真实 SSE 成功、早期 JSON 错误、mux 错误、静态 health 与 SPA 边界。既有写出失败、严格解码、脱敏、事务/导出/SSE 生命周期由完整受影响包回归继续覆盖。
- **正式负向控制**：以仓库中的正式测试为准，仓库外 Go overlay 分别恢复 HEAD 旧生产实现、把缓存头移到 `WriteHeader` 后、只对 HTTP 200 设置；三组均退出码 1，并精确因 `Cache-Control` 断言变红（失败条目含子测试分别 26/26/16）。静态边界组通过。未回退或破坏仓库生产文件，定向修复版 race 20 轮与受影响整包 race 已通过；准备阶段候选证据不代替正式门禁。
- **本轮门禁**：正式新增回归 `go test -race ./webui/api ./webui -run '^TestJSONCachePolicy' -count=20 -timeout=2m` 通过（4.054s / 1.697s）；两包完整 `go test -race ./webui/api ./webui -count=1 -timeout=3m` 通过（14.340s / 8.059s）；`go vet ./...`、`go build ./...` 通过。全量 12 包 `go test ./... -race -count=1 -timeout=20m` 全部 ok（syncer 48.399s、internal/health 43.495s）；受影响三个 Go 文件 `gofmt -l` 无输出、`git diff --check` 通过；最终文件范围核对仅约定五文件，唯一生产改动为共同 JSON 出口的头设置与注释。
- **适用代价与外部边界**：地域等普通静态 JSON 也采用 `no-store`，是用户确认的一致默认策略；可能增加重复 HTTP 请求，不清空已有 Vue 内存或历史缓存，不移除凭据回显，不替代认证/脱敏。环境为 Go `1.26.6 darwin/arm64`，使用既有 ignored 前端 dist 嵌入；未执行前端构建、产品真实二进制/浏览器、Docker/compose、Go 1.25/Linux、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。本项本地 HTTP 与判别性测试足以核验缓存头；20 轮仅覆盖新回归，全量单轮不外推长期稳定绿色或外部验收。以上为当时门禁记录；该批次源码/测试/文档其后已提交为 `82ad2dc`，本次未 fetch/push。P3-12/P3-14/P3-10/P3-13 继续未修复，I-07 整体不勾选。

**P3-03 当前实施补记（2026-09-30）：**

- **授权与范围**：用户确认深入研究后的补强方案并要求开始修复；基线 `7acf303`，工作树开始时干净、main ahead 4。该批次生产修改仅 `internal/health/push.go`，测试仅扩展已有 `internal/health/push_test.go`；文档更新本文与 `Build7.md`。未修改 Store、配置 API、RuntimeManager、协调器、健康、告警、前端或云端操作；**已提交为 `c5cc79d`**、未推送。
- **根因修复**：仅对非空非法 URL 的 `sendOnce=false` 分支保留 `active=false`/空 `lastURL`，增加有下限的 timer；到期直接 `continue` 重读已发布配置。Wake 停止 timer 并立即重读，Stop 停止 timer 并退出。非正 interval 使用默认 60s，正值不足 20s 按 20s，其他值保持原值。正常发送路径不增加重试或排队，不改变 HTTP 10s 上限。
- **判别性覆盖**：`TestInvalidURLRetryInterval` 固定负值/零/极短/下限/默认/长间隔；`TestPushInvalidURLRetriesOnBoundedInterval` 使用真实 20s timer，证明 1ns 脏配置不会紧循环且再次产生安全校验日志，不进入 transport/健康计算、不泄漏 URL/token；`TestPushInvalidURLTimerRereadsConfig` 证明不调用 Wake 仍能在原 timer 到期后读取新配置，并立即向本地 mock 首发一次；`TestPushInvalidURLWakeAndStop`、`TestPushInvalidURLDisableAndReenable` 验证长等待中的 Wake、Stop、关闭/恢复；`TestPushNormalFailuresKeepPeriodicAttempts` 覆盖网络、HTTP、坏 JSON、ok=false 的原周期尝试且不立即重试；`TestPushInvalidURLShapes` 覆盖解析/scheme/host 错误且零 transport 调用；`TestPushDirtySQLiteConfigReachesRuntime` 用临时 SQLite 证明 `enabled=true + ftp URL + 1ns` 可到达运行配置。
- **负向控制**：仓库外 Go overlay 将失败分支换回旧 Wake/Stop 等待（保留纯间隔 helper 以供测试编译），`TestPushInvalidURLRetriesOnBoundedInterval` 在 25s 后因没有第二次校验精确失败；另一 overlay 仅移除 20s 下限，同一用例因两次校验相隔约 11.8µs 精确失败。两个失败均为预期负向控制，没有改回仓库文件。修复版定向 race 通过，证明测试可判别原缺陷与新增高频风险。
- **本轮门禁**：新增相关用例定向 `-race -count=1` 通过；六个快速新增用例（排除两个真实 20s 等待用例）`-race -count=20` 通过；`go test ./... -race -count=1 -timeout=5m` 全量 12 包连续三轮通过；`go vet ./...`、`go build ./...`、前端 `npm run build`、gofmt 与 `git diff --check` 通过。两个真实等待用例随定向测试及三轮全量各执行一次。执行工具链为本机 Go 1.26.4，未另行运行 Go 1.25。
- **证据边界**：I-02 两项本地修复已收口，P2-04 已在基线提交为 `7acf303`，**P3-03 已提交为 `c5cc79d`**。真实 Uptime Kuma HTTP Monitor/Push DOWN 与恢复通知、真实云、SMTP/Webhook、浏览器、Docker 和当前 revision 远端 CI/GHCR 本轮均未执行。关闭或空 URL 的纯等待保持原样；timer 只重读 RuntimeState，不自动读 SQLite；持续非法地址仍需用户修正；定时 WARN 有周期下限，但没有新增对频繁 Wake 的全局日志限流。

**P3-04 实施补记（2026-10-02，方案 B）：**

- **原问题与更正：** 每次订阅全量回放、页面无事件身份判断导致重复；旧 Subscribe 先注册再锁外回放还允许实时日志排在历史前面，被前端截断淘汰。无 `onerror` 不是重复根因；完整 1000 条正序回放也不必然最终丢掉最新日志，不能沿用原审计的无条件表述。
- **实现合同：** 广播器用 `crypto/rand.Text()` 标识实例、`uint64` 递增序号标识日志；同一日志历史/实时 ID 一致，正文保持 TextHandler 格式。序号分配/入环/广播同锁，订阅同锁先历史入队再注册，历史最多 1000、通道 1256，网络写入不持锁。缓存 `[L,H]` 的同实例游标 `L-1 <= C <= H` 只补 `Seq>C`，`C=H` 无回放；缺游标、格式非法/超前、实例不同、`C<L-1` 分别发 initial/invalid_cursor/instance_changed/history_expired reset 并回放现缓存。
- **reset 与前端：** reset 含非空原因 data、完整帧结尾空行与当前实例 `L-1` 基准 ID（空流为 0）；完整 reset 后第一条历史前再断线可续传。前端同时清空实时窗口/旧游标并保存基准，BigInt 精确比较、最多 1000 行、仅存最后游标；同文不同 ID 保留。onopen 不清空日志/提示，onerror 只展示原生 readyState，不创建连接；序号跳跃显示不连续，不自动补发。挂载连接不等待历史 GET；卸载移除 listener/回调/关闭连接，晚到历史成功/失败均不发布。
- **判别性回归：** `TestLogStreamCursorMatrix` 覆盖首次/L-1/L/H/过期/重启/超前/非法/负数/前导零/溢出；`TestLogStreamNoReplayOnLatest`、`TestLogStreamPartialReplayReconnect` 经真实 Register/HTTP/SSE 验证只补遗漏；`TestLogStreamResetDisconnectAndEmpty` 覆盖 reset 后立即断线与空缓存；`TestLogStreamConcurrentReplayOrdering` 检查并发边界 300 条严格递增完整。旧写失败测试拆成 reset 首帧与 reset 后日志帧失败，均验证退出/零订阅/不重复错误头；共享 SSE deadline、能力检测、shutdown 旧门禁保留。前端 6 个真实组件脚本回归覆盖去重、同文不同 ID、窗口、重启/reset/大整数/非法帧、更长实例标识、状态/跳号/卸载和晚到历史请求。
- **正式负向控制：** 仓库外 Go overlay 只撤销游标回放过滤，`TestLogStreamNoReplayOnLatest` 因重连收到旧序号而变红；前端仓库外副本使用 HEAD 的旧 Logs.vue，同一重复 ID 用例期望 2 行、实际 3 行变红。两者退出码 1 是预期负向控制，不是正式修复门禁失败；未修改正式仓库文件。
- **本轮门禁：** 正式 `go test ./webui/api -race -run 'TestLogBroadcaster_|TestLogStream|TestHandleLogStream_|TestWriteSSE_' -count=20 -timeout=5m` 通过；全量 12 包 `go test ./... -race -count=1 -timeout=10m` 全部 ok；`go vet ./...`、`go build ./...`、受影响 Go 文件 gofmt 检查与 diff-check 通过。前端 `test:logs`（6）、`test:alerts`（5）、`test:delete`（20）及 `npm run build`（vue-tsc + Vite）通过；未安装/升级依赖。
- **状态与边界：** 实施前基线 `88154cd`、工作树干净；本轮源码/测试/文档尚未提交。生产仅日志 SSE 与 Logs 页面，不改共享 writeSSE、同步事件流、数据库历史/清空 API、Schema、Provider/DNS 或通知。仍保留慢订阅者满载丢弃；超过 1000 条缓存及进程重启前内存日志不保证恢复。真实浏览器（含原生 Last-Event-ID 自动重连、reset 后立即断线及路由切换）未执行，登记 [ProdTestList.md](./ProdTestList.md) PT-AUDIT-01；Go 1.25/Linux、Docker/compose、真实云/外部通知/远端 CI 未执行，不外推长期稳定绿色或外部验收，I-10 其他 finding 不随之关闭。

**P3-02 实施补记（2026-10-02，方案 B）：**

- **原问题与更正：** 实施前 `resolveTargetRules` 在调用 Resolver 后才读取 IsOpen，只影响日志与计数，不改变解析控制流。多个目标与三个 attempt 可在一轮触发阈值；同轮先失败后成功与先成功后失败给出不同 breaker 状态；解析缓存按 Lower+TrimSpace 去重而计数按原值，身份不一致。原表中“每轮本就只探测一次、AGENTS 字面满足”不适用于 Issue7 目标级重试现状，撤销该判断与接受弱语义的旧建议。用户确认 B 并授权实施。
- **实现合同：** 正式 runRound 创建独立 dnsRound，轮初捕获已熔断状态并显式传至全部目标。一个调用负责半开探测，等待不持锁；失败原始错误仅本轮复用、下一轮重试，成功立即清空计数且不共享成功 IP。正常域名、成功等待者及后续 attempt 新解析；恢复后不重新开启本轮失败缓存。全轮有成功非空解析就不加失败计数，全轮尝试过且均失败才轮末加一次，未解析不变；阈值表示连续无成功解析轮数。原始空结果不解除熔断，单条规则 IPv6 过滤导致的空结果仍交由 planner 判定。
- **身份与快照：** DomainKey 固定 Lower+TrimSpace，配置裁剪/计数/解析去重统一身份，保留展示原值，不扩张尾点/IDNA。P3-01 成功淘汰、紧凑复制、删除裁剪、新旧独立和导入 Reset 保留。轮末只写本轮旧 breaker，不将候选发布后晚到计数合并到新实例；这是已有快照边界，也意味着配置发布时尚未提交的旧轮失败计数不会进入新状态。
- **目标与通知边界：** 不提前跳过目标、不使用旧 IP、不修改 isRetryable、生产 DNS/SDK 超时、Provider、schema/API/前端或事件粒度。其他域名仍可新增；DNS 失败目标保持 failed，删除继续受完整期望与 DNS 安全门约束。某处成功清空 breaker 不掩盖其他失败目标/运行健康；每目标每 host 每轮仍最多一个 DNS 事件。Dry Run 独立解析，不参与探测名额、计数或事件。
- **回归：** TestDNSRound_TargetFlow 覆盖五目标各三 attempt 的五轮阈值、第六轮一次探测、事件粒度、两种成功/失败顺序、跨云探测与并发 Dry Run、混合域名可新增且零删除、恢复后版本竞争重新读取变化 IP、统一身份/复制隔离/Reset、idle 不改变状态。TestDNSRound_ConcurrentProbe 用 32 个并发调用检查失败一次探测与原始超时保留、成功独立 IP、等待者释放。TestDNSRound_FailureExpiresAndLatePublishIsIsolated 覆盖失败不跨轮、下一轮恢复、旧轮末写入隔离；TestDNSRound_EmptyResultDoesNotRecover 覆盖空解析不能清零/零写入；TestDNSRound_FilteredIPv6DoesNotCountFailure 验证上游成功但规则过滤 IPv6 后目标仍 failed、breaker 清空且不发 DNS 失败事件；TestCircuitBreaker_DomainIdentity 精确检查大小写/空白别名共享计数、裁剪与成功清理。既有三个目标事件回归只显式传入并结束轮次对象；P3-01 测试更新大小写身份夹具与精确 map 预期，不削弱裁剪或隔离检查。
- **真实本地网络：** TestDNSRound_RealUDP 使用生产 Resolver 与只接收不响应的 loopback UDP 上游，两个目标三次 attempt、150ms 整体超时。候选研究中旧实现实际 12 个 UDP 包、约 910ms，候选为 A/AAAA 合计 2 包、约 152～153ms；正式测试要求 2 包且两个目标均 failed，耗时仅记录不作阈值断言。重试/厂商间隔通过既有测试接缝免除实际等待，结果不外推生产整轮时延。
- **正式负向控制：** 仓库外 overlay 只移除目标解析中的 round.resolve，TestDNSRound_RealUDP 精确因 12 个 UDP 包（预期 2）变红，记录约 909ms；另一 overlay 让后来的失败清除本轮已成功标记，mixed_order 子用例精确因先成功后失败导致计数错误变红。未改变仓库文件；两项退出码 1 均为预期负向控制，不是正式实现失败。研究阶段另以旧实现验证了阈值/身份等用例的判别力，详见此前研究结果，正式批次不冒充重跑全部旧实现对照。
- **本轮门禁：** 正式 breaker/轮次/目标流程定向 `go test ./dns ./syncer -run 'TestDNSRound|TestCircuitBreaker|TestBreakerPolicy|TestBreakerPreserve|TestTargetRound_' -race -count=20 -timeout=10m` 通过；全量 12 包 `go test ./... -race -count=1 -timeout=10m` 共执行三轮全部 ok（最后一轮包含新增空结果/IPv6 过滤/别名边界测试）；`go vet ./...`、`go build ./...`、受影响 Go 文件 `gofmt -l`（无输出）与 `git diff --check` 均通过。定向重复证据与多轮全量证据不外推全仓长期稳定绿色。
- **状态与证据边界：** 本机 Go 1.26.6 darwin/arm64；源码/测试/本轮文档尚未提交。未执行 Go 1.25/Linux、前端、浏览器、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR。半开失败后即使本轮中途恢复也需等下一轮或手动同步再探测；不新增冷却时间、TTL/LRU、长期 IP 缓存、持久化或通知限流。不外推全仓长期稳定绿色或外部验收，I-08 其他子项不随之关闭。

**P3-01 实施补记（2026-09-30，细化后的 A，历史批次）：**

> 该批次其后已提交为 `ac0ee62`；以下保留当时记录。原值身份、不改变半开探测的历史条款由后续 P3-02 方案 B 替代，成功淘汰与生命周期裁剪继续成立。

- **状态与原问题：** 用户确认研究后授权实施，基线 `901d642`，开始时工作树干净。P3-01 已本地修复，源码/测试/本轮文档尚未提交。历史基线 `34aa9b8` 至本轮实施前，RecordSuccess 写入零值、Clone 全量复制历史 map，成功域名及删除/改名后的正数计数永久残留，随曾访问的不同域名数累积并增加普通配置事务的复制成本；删除后重新添加同名域名可能继承旧失败进度。

- **已实施方案：** RecordSuccess 保留解除日志并删除 key，下一次失败从 1 开始。原全量 Clone 替换为 CloneForDomains(domains []string)：持有旧 breaker 锁，遍历新配置域名原值，仅复制正数计数到全新 map，不按历史条目数预分配。重复域名只保留一项，零条目和已移除域名不进入新状态。BuildRuntimeState 从深拷贝后的 published.DomainRules 提取域名，普通发布保留这些配置域名的进度并应用新阈值；暂停/暂时零目标不清空仍配置的域名。空规则发布后为空，删除后重加从零开始；首次启动和完整导入仍新建空 breaker。

- **快照与存储边界：** 不原地裁剪或共享 map；旧轮次继续写旧 breaker，不能恢复新状态中已删除的域名。候选复制后旧轮次发生的计数更新不合并到新实例，沿用既有快照行为。新发布状态只保存配置域名正数计数，运行中的条目数不超过该快照配置中的不同域名数；不设置绝对配置规模上限。delete 不保证底层立即缩容或 RSS 立即下降；发布时重建紧凑 map，旧快照释放后其存储可由 GC 回收。域名集合取全部配置规则，不等同本轮适用规则，不做大小写规范化。不增加 TTL/LRU/后台协程/持久化，不修改半开探测、IsOpen、解析器、Provider、schema/API 或健康告警。

- **原选项评价更正：** A 同时提供成功即时清理和配置生命周期裁剪，已实施。B 只在复制时丢弃零值，仍保留历史失败域名。C 若每次发布按配置域名裁剪并重建 map，已可阻止历史无界累积，但零值条目留到下次发布；原“C 无法解决无界增长”评价过强，推荐 A 的理由为状态更精简、清理更及时。D 接受现状未采用。

- **判别性回归：**
  1. TestCircuitBreaker_SuccessRemovesEntry 明确断言成功后 key 不存在、再次失败为 1，并验证 10,000 个纯成功域名零条目；在旧生产实现上变红，修复后通过。
  2. TestCircuitBreaker_CloneForDomains 精确检查 map，覆盖正数保留、删除/零值裁剪、重复/新增域名、大小写原值、nil/空集合、双向写入和阈值隔离。
  3. TestCircuitBreaker_CloneForDomainsConcurrent 并发复制、计数读写、IsOpen 与阈值更新，检查锁边界和复制内容约束。
  4. TestBreakerPreservePrunesConfiguredDomains 经真实 BuildRuntimeState 验证暂停/零目标保留、删除裁剪、旧轮次晚到写入隔离、空规则与删除后重加；在旧实现上变红，修复后通过。
  5. 既有 Preserve/Reset 和阈值单测补齐 a.com 配置规则。首次全量 race 暴露 TestConfigImportResetsDNSBreakerOrdinaryChangePreserves 的同类夹具缺失：只向 breaker 写 probe.test、未配置该域名却要求保留。已补齐 SQLite 和初始状态规则，并使完整导入仍包含同一域名，确保 Reset 断言不因域名裁剪而误通过。该变更仅限既有 API 测试夹具，没有扩大生产边界。

- **本轮门禁：** breaker/运行时策略定向 `go test ./dns ./syncer -run 'TestCircuitBreaker|TestBreakerPolicy|TestBreakerPreserve' -race -count=20` 已通过；API 导入策略定向 `go test ./webui/api -run '^TestConfigImportResetsDNSBreakerOrdinaryChangePreserves$' -race -count=20` 通过；补齐夹具后重新执行全量 `go test ./... -race -count=1 -timeout=20m`，12 包全部 ok；`go vet ./...`、`go build ./...`、受影响 Go 文件 gofmt 检查与 `git diff --check` 通过。首次全量 race 的 API 夹具失败保留为过程证据，不写成通过；定向重复证据不外推为全仓长期稳定绿色。

- **证据边界：** Go `1.26.4 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、浏览器、Docker/compose、真实云、SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。本项不依赖这些外部链路，本地通过不外推其已验收或全仓长期稳定绿色。I-08 仅本项收口，其他子项保持各自状态。

**P3-10 当前实施补记（2026-10-03，细化后的方案 A）：**

> 本补记为前序实施批次记录，其后已提交 `99f3fe2`；本段的未提交/权限待办状态不代表现时状态，P3-12 后续结果见其当前实施补记。

- **授权与当前核对：** 用户确认前轮研究推荐并授权本轮正式修复及同步文档；基线 `main / f6b3757`、本地 `origin/main / c08f2e1`、ahead 2，开始时工作树与暂存区干净。再次确认 I-01 已取消 pidfile 删除且 Close 错误已 WARN；剩余四类、五个位置仍存在。本轮不恢复锁文件删除，也不改权限或 MultiHandler。源码/测试/文档尚未提交，未 fetch/push。
- **数据库失败收尾：** `OpenStore` 保留 DSN/sql.Open，包内 `openStoreDB(*sql.DB)` 在失败返回时统一 defer Close；只有关闭也失败才 `errors.Join(primary, wrappedClose)`，主错误在前且 `errors.Is`/`errors.As` 保留，成功初始化保持 DB 可读写。沿用真实 `database/sql` 驱动接口注入错误，不替换全局工厂，不引入新数据库接口；WAL、迁移回滚、`_txlock=immediate`、ReadOnly、busy_timeout、连接池及 schema 保持。
- **日志渲染与历史说明订正：** `renderLine` 返回 `(string,error)`，检查 TextHandler.Handle，广播器在锁/序号/ring/订阅投递前返回错误，不递归记录。标准库 [bytes.Buffer.Write](https://pkg.go.dev/bytes#Buffer.Write) 保证 error 为 nil，取代历史“通常不失败/实际少失败”的表述；当前固定 buffer 路径没有返回 error 的生产复现，本改动是符合 AGENTS 的规范性补齐。另据 [slog.Handler](https://pkg.go.dev/log/slog#Handler)，Logger 丢弃 Handler 返回错误，不能将向上传播表述成应用自动告警，P3-13 仍独立追踪。
- **SSE 采用 A 并细化日志安全：** 序列化失败固定 WARN，只含事件 `type` 和 `error_type`（`fmt.Sprintf("%T", err)`）；不输出 Data、不调用序列化错误的 Error()，因为自定义 MarshalJSON/MarshalText 错误可含敏感内容（正式两类负向控制证明）。跳过该事件、继续连接，后续正常帧可收到；坏事件没有帧，不写成已发送。帧格式、既有写 deadline、shutdown/context 和退订保持；不新增自定义错误事件、回放、发布端预序列化、重试或队列。多个 SSE 客户端可能各记录诊断，满 channel 丢弃边界保持，不承诺无损流。
- **静态 health：** Write 失败只记录 Debug，不补写 500、不重试；正常静态 200、`{"status":"ok"}`、Content-Type 与无 Cache-Control 的既有边界保持，不改 operational health 或 Docker 探针。默认 info 不展示 Debug，避免正常客户端断开噪声。
- **正式新增回归：** `config/store_open_error_test.go` 的 `TestOpenStoreFailurePreservesCloseError` 覆盖 WAL/Begin/Schema 三阶段 × Close 成功/失败六组合，验证主错误与关闭错误、错误类型/顺序、关闭一次、Schema 失败回滚和失败 DB 不可用；`TestOpenStoreSuccessKeepsDBOpen` 用真实临时 SQLite 验证初始化后业务读写。`webui/api/sync_marshal_test.go` 的 `TestSyncEventsMarshalFailureContinues` 用真实 EventBus.Publish + handler 覆盖 function/channel/NaN/cycle/自定义 MarshalJSON/map-key MarshalText/非法时间七类坏输入，随后正常帧结构化解码通过、安全 WARN 恰一次、坏事件零帧、连接保留、取消后恰好退订一次。`webui/server_health_write_test.go` 的 `TestStaticHealthWriteFailureDebug` 覆盖立即失败 writer、一次 Write/零补写状态码、恰一 Debug 及正常响应/头/零日志控制。
- **正式故障注入与负向控制：** 只在仓库外 overlay 将渲染 writer 替换为返回 `io.ErrClosedPipe` 的夹具；正式实现 `TestRenderFailureNoPublish` race 20 轮通过，证明原错误返回、零序号/ring/投递更新、无递归日志，不给生产增加测试 hook。十类负向控制分别为：Store 丢弃 Close、关闭错误覆盖主错误、误关成功 DB；SSE 静默跳过、原样记录错误、断连接；health 忽略 Write、将 Debug 升为 Warn；renderLine 丢弃错误、广播器丢弃渲染错误。全部按对应正式行为断言变红，退出码 1 均是预期控制；检查无 build failure，不以编译失败充当判别力。正式测试基于修正后的 struct map-key Marshaler 夹具，前轮 string key 未触发 MarshalText 的研究夹具错误不冒充通过。
- **本轮门禁：** `go test ./config ./webui/api ./webui -run 'TestOpenStoreFailurePreservesCloseError|TestOpenStoreSuccessKeepsDBOpen|TestSyncEventsMarshalFailureContinues|TestStaticHealthWriteFailureDebug|TestLogBroadcaster_(Format|Replay|RingOverflow)' -race -count=20 -timeout=5m` 通过；受影响三包完整 `-race -count=1 -timeout=5m` 通过（config 14.085s / api 15.691s / webui 8.188s）。全量 12 包 `go test ./... -race -count=1 -timeout=20m` 全部 ok；其中根包 TestMain 通过 `go build -o` 构建真实产品二进制，既有进程级启动/退出/SSE/导入导出/本地 mock Push 回归随全量执行通过，不将其描述为本轮未执行真实二进制；`go vet ./...`、`go build ./...`、受影响 Go 文件 `gofmt -l`（无输出）及 `git diff --check` 均通过。单次全量绿色不外推全仓长期稳定。完整外部注入与负向控制记录保存于本轮临时实施目录；研究候选测试不冒充正式门禁。
- **状态与证据边界：** 生产固定四文件，新增三份测试，文档仅本文/AGENTS，共九文件。Go `1.26.6 darwin/arm64`；未执行 Go 1.25/Linux、前端构建、P3-10 专门进程级故障验收/浏览器、Docker/compose、真实云/SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR。P3-10 依据本地错误注入与自动化验收收口，不外推全仓长期稳定或真实外部通过；I-07 的 P3-12/P3-14/P3-13 继续未完成。

**P3-10 独立追踪与后续设计（以下为 I-01 前的历史研究）：**

> **2026-10-03 当前状态更正：** 以下原始缺陷链、方案与判别性验收设计保留为历史记录；用户已选择细化 A 并授权实施，当前代码、验证与边界以上方补记为准。旧 PID-only/删除锁文件、将错误原文直接写日志及“buffer 通常不失败”均不是本轮合同。

> 2026-09-30 I-01 补记：下述第①项的 `os.Remove` 已随 flock 改造移除，cleanup 的 `Close` 错误已记录 WARN。后续 P3-10 仅处理剩余②～⑤，不得恢复 PID-only 判活或删除锁文件；本项整体仍未完成。

- **状态与精确证据：** P3-10 问题真实存在，当前仍未修复，继续归入独立队列 I-07；不能因相关路径通常不报错、已有成功路径测试，或 P3-11 已由 Issue7 Step 4 修复而关闭本项。当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`（**历史基线，非当前 HEAD；当前见头部「最近核验基线」小节**），`main == origin/main`；工作树唯一 tracked 修改是本审计报告，本轮未编辑代码或测试，未构建、未运行测试、未格式化。六处缺口如下：
  1. `config/pidfile.go:33` 的 cleanup 闭包直接调用 `os.Remove(path)`，丢弃删除失败；陈旧 pidfile 可能因权限、路径类型或文件系统错误而无法清理，但当前没有日志。
  2. `config/store.go:118`、`:124` 在 WAL 设置失败或建表失败的收尾路径调用 `db.Close()`，关闭错误被丢弃；主失败通常仍能返回，但关闭失败不可观测。
  3. `webui/api/logstream.go:97` 以 `_ = h.Handle(...)` 丢弃 `slog.TextHandler` 写入 `bytes.Buffer` 的错误；当前标准 buffer 通常不失败，不等于可忽略返回值。
  4. `webui/api/sync.go:127-130` 的 `json.Marshal(ev)` 失败后直接 `continue`，同步 SSE 事件被静默跳过；`Event.Data` 为 `map[string]any`，理论上可出现函数、channel 等 JSON 不支持值。
  5. `webui/server.go:298` 静态 `/api/health` 的 `w.Write(...)` 返回值被忽略，客户端断开等写失败无法观测。

- **已有修复/不存在核对：** 本轮没有发现上述五类缺口中任何一类已被当前源码收口；P3-11 的 `StoreLogWriter` 返回错误修复不覆盖 P3-10。现有 pidfile 测试只覆盖正常清理，Store 测试未证明失败收尾时 `Close` 错误处理，日志流测试未注入 `Handle` 错误，SSE 测试未覆盖 `Marshal` 失败后的语义，健康端点测试未覆盖 `ResponseWriter.Write` 失败记录。因此结论是“真实存在且未修复”，不是“已修复”或“当前不存在”。

- **推荐的最小后续设计与用户决策：** 以下仅为后续授权后的设计，不是本轮实施。① pidfile 保持 `WritePidFile` 接口和现有 PID-only 互斥语义不变；cleanup 对 `os.ErrNotExist` 按幂等成功处理，其余 `os.Remove` 错误记录不含敏感信息的 WARN，至少包含路径和错误。② `OpenStore` 保留 WAL/建表主错误，并用 `errors.Join` 合并 `db.Close` 错误，使调用方仍可通过 `errors.Is` 识别主错误和关闭错误；不引入新的数据库抽象。③ 让日志渲染函数返回 `(string, error)`，由 `LogBroadcaster.Handle` 向上传播渲染错误；不要在同一 Handler 内再次调用 `slog`，避免日志递归。④ 同步 SSE 的序列化失败有两个选项：**A（推荐）**记录不含 `Data` 的结构化错误（事件类型和序列化错误足够定位），跳过该坏事件并继续保持连接，维持现有 SSE 协议；**B**记录错误后立即结束该 SSE 连接，由客户端重连，但一个坏事件会中断其后的正常事件。当前只记录 A 为推荐，不擅自替用户裁决。⑤ `/api/health` 检查 `w.Write` 返回值并记录 Debug；客户端主动断开属于常见探针情形，不建议每次用 WARN 制造噪声。除 SSE A/B 外，其余为补齐硬性 error 处理的机械性最小修复，不改变业务语义。

- **受影响文件与明确排除：** 生产文件限定为 `config/pidfile.go`、`config/store.go`、`webui/api/logstream.go`、`webui/api/sync.go`、`webui/server.go`。建议扩展或新增 pidfile 测试、`config/store_error_test.go`、`webui/api/logstream_test.go`、`webui/api/sse_test.go`、`webui/server_test.go`；测试接缝必须服务于错误注入，不得把生产实现重构成重型抽象。明确不修改 `WritePidFile` 的 PID 判活/TOCTOU 设计（P3-08/I-01）、权限收敛（P3-12）、`MultiHandler` 多路错误聚合（P3-13）、通用 `writeJSON`、健康状态模型、事件结构、前端 SSE 协议、Provider、同步目标级状态机或任何真实外部链路。

- **判别性验收：** L0 静态确认六处不再丢弃返回值，且未扩大到上述排除项。L1 至少覆盖：①正常 pidfile cleanup 不产生错误日志，将 pidfile 替换为非空目录后 cleanup 能记录失败，已不存在文件按幂等成功；②用小型 close-error 测试接缝证明 `OpenStore` 返回错误同时满足 `errors.Is(err, primaryErr)` 与 `errors.Is(err, closeErr)`，并保留 WAL/迁移失败控制；③用立即失败 writer 证明日志 `Handle` 错误可向上传播且不会递归记录；④发布 `Data` 含函数或 channel 的 SSE 事件，断言记录事件类型和序列化错误但不记录完整数据，随后发布正常事件仍可收到，以判定采用 A；若用户选择 B，则断言该连接结束且客户端重连语义另行记录；⑤用返回固定错误的 `ResponseWriter` 证明 `/api/health` 不 panic、不重复写状态码并记录写出失败，同时保留 200/`{"status":"ok"}` 控制用例。L2 获得实施授权后执行受影响包定向测试，再按项目门禁执行多轮 `go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 与 diff 检查；本轮均未执行，不得预先写成通过或把单次绿色外推为稳定绿色。L3 可选地用本地真实二进制补充健康探针断开和 SSE 坏事件后的连接行为，但不替代 L1/L2。

- **风险、外部边界与停止条件：** 主要风险是错误处理改动遮蔽原始 Store 错误、日志错误处理形成递归、或把 SSE 的坏事件误报为已成功发送。A 的推荐语义仍然会跳过不可序列化事件，只是不再静默；不得把该事件写成“已发送”。本项真实性和修复验收只依赖本地静态核验、受控错误注入、受影响包测试和本地进程；不依赖真实腾讯云/阿里云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR，本地通过也不能改变这些外部验收项的状态。若实现改变 `WritePidFile` 的锁/判活语义、把 `db.Close` 错误覆盖主错误、改变 SSE 帧或事件结构、增加自定义错误事件协议、把 `/api/health` 改为 operational 语义、把客户端断开全部提升为 WARN，或把 P3-08/P3-12/P3-13 混入本项，应立即停止并回到方案审查；在用户对 SSE A/B 作出裁决、判别性测试及受影响门禁完成前，保持 P3-10/I-07 未修复。

**P3-12 当前实施补记（2026-10-03，细化后的方案 A）：**

- **授权与基线：** 用户在本聊天确认研究推荐并授权正式实施及文档更新；实施前 `main / 99f3fe2`、本地 `origin/main / c08f2e1`、ahead 3，工作树与暂存区干净。前序 P3-10 已提交。本轮未 fetch/push，也未提交当前改动；研究临时候选不冒充本轮正式门禁。
- **目录与默认路径：** `run` 以 `0700` 创建最终数据目录并对已有目录显式 `Chmod`，失败返回退出码 1，尚未取得锁或打开 SQLite/HTTP。只修改最终目录，不递归处理父目录或其他用户文件。`LoadDeploymentConfig` 先处理显式数据目录，只有需要默认值才调用返回 `(string,error)` 的 `DefaultDataDir`；HOME 不可用时返回部署错误，取消静默 `.` 回退。真实二进制验证无 HOME 的未设置/空白目录失败时 CWD 保持 `0755`、无 DB/锁、无监听；显式专用目录仍正常启动。
- **锁权限与互斥：** `WritePidFile` 保持 `O_CREATE|O_RDWR`、新建请求 `0600` 和同 FD 上的 flock；成功持锁后、截断与写入 PID 前执行 `f.Chmod(0600)`。失败关闭同一 FD 并返回错误；竞争者不改权限/内容，迁移不替换 inode，退出继续保留锁文件。不改 `pidfile_unix.go` 或恢复旧 PID 判活。
- **主库与辅助文件：** 在 `sql.Open`/实际 SQLite 初始化前，以不截断的 `O_CREATE|O_RDWR` 预创建主库并 `f.Chmod(0600)`，显式处理关闭错误，主错误在前；已有 WAL/SHM 在打开前收紧，初始化后再次收紧。只忽略 `ENOENT`，其他权限/路径错误不能伪装成不存在；缺失不预创建、不清空、不删除。固定 modernc SQLite `v1.54.0` 的 Unix VFS 从主库权限创建新辅助文件，已用真实重建证明，不依赖“继承目录模式”的错误推断。`openStoreDB` 接受路径以在其既有 defer 关闭边界内执行初始化后检查，保持 P3-10 主错误与 Close 错误聚合；未改 WAL、schema、`busy_timeout(5000)`、`_txlock=immediate`、连接池/业务事务或依赖。
- **正式配置回归：** 新增 `config/permissions_test.go`，四个 umask（0000/0022/0077/0777）各在隔离子进程执行：主库路径含中文/空格/#/?/%、两条实际连接、零 idle 后辅助文件消失与重建、旧 0644/0666 主库配置保留、非正常子进程退出留下真实非空 WAL 后读取 committed sentinel 并迁移 DB/WAL/SHM、旧宽权限锁的实际内核屏障/内容保留/同 inode/释放后迁移。不是用空辅助文件代替崩溃恢复。标准 driver 接缝在 Commit 后制造 ENOTDIR，证明初始化后的权限失败关闭恰一次、nil Store、无额外 rollback、DB 不可用，主错误类型及关闭错误保留；缺失辅助文件与其他路径错误有独立控制。`deployment_test.go` 补无 HOME 的默认失败/显式成功，并处理新返回签名；`store_open_error_test.go` 仅跟随私有初始化函数路径参数，原 P3-10 六组合断言保留。
- **正式产品进程回归：** 新增 `permissions_process_test.go`，使用 argv 传递固定 umask 到产品子进程，不修改父测试的全局 umask；12 组真实启动场景覆盖新目录、旧0755/0777目录及宽权限 DB/锁，精确判断目录0700、DB/WAL/SHM/pid0600，父目录0755与 user-file0644不变，API 返回旧配置 sentinel，SIGTERM退出0、锁释放且文件保留。三组无 HOME 场景覆盖默认/空白失败与显式目录成功。测试数据与响应不写出真实凭据。
- **正式负向控制：** 仓库外 overlay 不污染工作树。①恢复旧主库打开/初始化实现：精确模式断言变红；②移除初始化后辅助文件检查：Commit后的故障未关闭被捕捉；③移除锁权限迁移：旧模式仍宽而变红；④把 Chmod 移到 flock 前：竞争者擅自修改持锁文件模式而变红；⑤删除已有目录 Chmod：真实产品保留0755而变红（GOFLAGS overlay 同时传给 TestMain 内的产品构建）；⑥恢复默认路径 `.` 回退：缺少HOME断言变红。六类均编译成功、按目标行为失败。⑦只删除打开前辅助文件检查的正式 Linux 产品变体：root-owned WAL 容器夹具得到退出码0而非要求的1，启动失败断言变红；当前正式源码同夹具明确退出1。没有用编译失败或研究候选代替判别证据。
- **正式 Docker/卷证据：** Go `1.25.0` 交叉编译的 `CGO_ENABLED=0 linux/amd64` 当前产品二进制，装入既有 `fwalizer:issue7` 运行镜像（不是旧镜像的产品二进制）。UID1000/appuser 验证目录、锁、DB、WAL、SHM五种root-owned权限失败均退出1、有对应阶段诊断、无HTTP监听；目录/锁失败不建库，DB内容不变，辅助文件失败保留原内容、仅留下无业务内容的预创建空DB，不继续SQLite初始化。新/已有宽权限目录均health200、模式700/600、退出0。三个独立命名卷验证新卷、旧宽权限有效DB、root-owned有效DB；root-owned先拒绝，停止并修复测试卷属主后重启，健康200且旧配置sentinel保留。所有实验容器与命名卷已清理；应用不自动chown。amd64二进制在linux/aarch64 Docker VM兼容执行，不外推原生amd64主机。
- **正式门禁：** macOS `go1.25.0 darwin/arm64`：新配置回归及相关 `TestPidFile`/P3-10收尾 `-race -count=20` 通过（256.903s），新产品进程回归初版 `-race -count=20` 通过（23.817s）；全量门禁后仅把进程夹具改为唯一 Wait 与有界清理，最终进程新回归再跑20轮通过（20.709s），macOS/Linux根包完整race补测通过；全量12包 `go test ./... -race -count=1 -timeout=20m`、`go vet ./...`、`go build ./...` 通过。Linux容器 `go1.25.14 linux/arm64`：全量12包同一race命令、vet/build通过。受影响Go文件gofmt检查及最终diff-check通过。早期Go1.26.6定向首轮为辅助证据；本轮没有仅凭研究候选或单次全量绿色宣称长期稳定。
- **范围与状态边界：** 四个既有生产文件、四份测试文件、本文/AGENTS/README，共11文件，无新增生产框架或测试全局开关。P3-12本地收口，P3-13/P3-14未处理，I-07整体不勾选。源码/测试/文档尚未提交；未执行原生linux/amd64机器、完整发布镜像重建/compose、前端构建、浏览器、真实云/SMTP/Webhook/Uptime Kuma或远端CI/GHCR。精确模式保护不承诺root/同UID/ACL/NFS等特殊边界；运行中人为改宽权限不在启动迁移保证内。

**P3-12 独立追踪与后续设计（修复前历史研究，旧核验事实保留）：**

> 2026-10-03：以下历史未完成判断、旧三文件候选与待裁决选项已由上方细化 A 实施补记替代当前状态；原证据保留，不用于恢复旧锁生命周期。权限失败即退出及默认路径失败处理均已获得本次明确授权。


> 2026-09-30 I-01 补记：新建锁文件已请求 `0600`，已有锁文件/目录/DB 权限迁移仍未实施。旧 PID-only 源码和测试描述均为历史快照，不能用于恢复旧判活或删除语义；P3-12 仍未完成。

- **精确证据与影响：** `run.go:41` 的 `os.MkdirAll(deploy.DataDir, 0755)` 对新目录只提出宽权限请求，且不会收紧已存在的 `0755/0777` 目录；`config/pidfile.go:29` 的 `os.WriteFile(path, data, 0644)` 同样不会改变既有 pidfile 权限。`config/store.go:111-128` 直接打开 SQLite，未预创建并设为 `0600`，也未处理已有数据库；WAL 模式下还必须核实 `config.db-wal` 与 `config.db-shm` 辅助文件的最终权限。数据库中的 `settings`、`alert_email.password` 等字段保存云 Access Key/Secret、SMTP 用户名/密码、Webhook URL 与 Uptime Kuma Push URL，故该问题不是纯形式规范，而是同机多用户的敏感信息暴露风险。当前没有源码级 `Chmod`、`Umask`、`0600` 或 `0700` 机制。

- **当前验证边界：** 本次静态核验基线为 `HEAD == origin/main == 34aa9b859c38904e701a19d672dcc4f65435b31c`；工作树唯一未提交修改为本审计报告，源码没有本轮改动，本轮没有构建、测试或格式化。现有 `main_test.go:317-339` 只证明数据库存在，`:232-244` 只证明正常退出清理 pidfile，`:994-1059` 只证明按当前 PID 判活的顺序启动拒绝；它们均不能证明新建/已有目录、DB、WAL/SHM 或 pidfile 的精确权限。因此 P3-12 仍不能关闭，I-07 仍不能勾选完成。

- **推荐的最小实施设计（仅供后续授权）：**
  1. `run.go`：使用 `os.MkdirAll(deploy.DataDir, 0700)` 后，无论目录新建还是已存在都显式执行 `os.Chmod(deploy.DataDir, 0700)`；任一权限收敛失败即启动失败，不以 WARN 后继续打开数据库或监听端口。不要递归修改数据目录中的其他用户文件。
  2. `config/store.go`：在 `sql.Open` 前以 `O_CREATE|O_RDWR`、`0600` 预创建数据库并对已有 DB 显式 `Chmod(path, 0600)`；打开/初始化前收紧已存在的 `config.db-wal`、`config.db-shm`，初始化后再次检查已出现的辅助文件，不能把 modernc SQLite 对权限的实现推断当作验收证据。
  3. `config/pidfile.go`：新建 pidfile 请求 `0600`，对已有 pidfile 显式 `Chmod(path, 0600)`，保持 I-01 已实施的同一文件描述符 flock 互斥与文件保留语义，不得恢复旧 PID 判活。新建文件 `0600` 已完成，后续只补齐权限迁移，但 flock、PID 复用、TOCTOU 和并发启动不并入本项。

- **仍需用户确认的实现选项（不在本轮擅自裁决）：**
  - **A（推荐）**：目录、DB、WAL/SHM、pidfile 的 `Chmod` 失败均 fail-closed，启动失败并保留可诊断错误；这符合敏感明文配置的保护目标，也避免把“权限修复失败”伪装成成功启动。
  - **B**：`Chmod` 失败只记 WARN 并继续启动；兼容受限文件系统或 root-owned Docker 旧卷，但会允许不满足 `0700/0600` 的不安全状态继续运行，不建议采用。
  - **C**：把权限迁移与 I-01 的 flock 一并实施；可减少一次生命周期变更，但会扩大 I-01 范围并混淆 P3-12 与互斥语义，当前不建议。
  - 已固定的目标权限 `0700/0600` 不属于待重新裁决项；上述选项只涉及失败语义与 I-01 的边界。

- **受影响文件与明确排除：** 预期生产改动限定为 `run.go`、`config/store.go`、`config/pidfile.go`；建议新增 `config/permissions_test.go`（或同等独立权限测试文件），并视 Docker 旧卷说明需要更新 `README.md`。不修改 `config/pidfile_unix.go` 已实施的 flock 互斥、I-01 的文件保留设计、SQLite schema/业务事务、Provider、同步/告警/HTTP 语义或其他 P3 条目；本轮实际只编辑本报告。

- **判别性验收：**
  1. L0 静态检查：不再以 `0755/0644` 作为目标权限，存在对已存在目录/文件的显式 `Chmod`，并确认未递归改写数据目录。
  2. L1 `config` 权限单测：新建及预置 `0755/0777` 数据目录最终为 `0700`；新建及预置 `0644/0666` DB 最终为 `0600`；预置宽权限 `config.db-wal`/`config.db-shm` 后重新打开仍为 `0600`；新建及预置宽权限、内容无效的 pidfile 最终为 `0600`；在不同 `umask` 下均使用 `info.Mode().Perm()` 判定精确位，不只检查可读写。
  3. L2 真实二进制：覆盖新建数据目录、已有宽权限目录/DB/pidfile、权限收敛失败；失败时不得继续打开数据库或监听端口。
  4. L3 Docker：分别验证非 root `appuser` 使用新建卷和已有宽权限卷；若旧卷由 root 所有且 `appuser` 无权收敛，必须得到可诊断的 fail-closed 结果，并保留 README 的 `chown` 恢复边界。Docker 新卷及迁移后的文件最终分别为 `0700/0600`。
  5. L4 在 Linux 与 macOS 分别执行受影响包 race、全量 race、vet、build、gofmt/diff-check；在这些证据齐全前不能标记 P3-12/I-07 完成。

- **风险、外部边界与停止条件：** 该方案只保护同机其他非特权用户，不能阻止 root 读取，也不把 ACL、NFS 或其他特殊网络文件系统外推为本地文件系统结论；不递归 chmod 也意味着数据目录内用户自放置文件不在本项保证内。SQLite WAL/SHM 的实际创建权限、Docker 持久卷已有文件的属主与 `appuser` 可修改性必须由实测确认。P3-12 不依赖真实云 API、DNS、SMTP、Webhook、Uptime Kuma、浏览器或远端 CI/GHCR；这些外部链路不得被本项本地通过替代。若实施扩大到 flock/PID 判活、改变 DB schema/事务或启动失败语义，继续运行时、忽略 `Chmod` 失败、把一次绿色测试外推为跨平台/旧卷通过，或无法稳定证明 WAL/SHM 权限，应立即停止并回到方案审查；在上述判别性测试、跨平台/Docker 证据和文档闭环完成前，保持“未完成/待独立处理”。

---

## 3. 内存与资源专项结论

| 类别 | 结论 | 依据 |
|---|---|---|
| **goroutine** | **有界，无泄漏** | 常驻仅 5 个（`s.Run`、`supervisor.Run`、`pusher.Run`、`srv.Wait`、`serveErrCh`）；`syncer.go:803` 每云厂商一个（≤4）且有 `wg.Wait()` 收束；`bus.go:113` **每事件每订阅者一个短命 goroutine**（无背压，故障期瞬时数百个但迅速退出，非泄漏）；收尾顺序 `pusher.Stop → supervisor.Stop → s.Stop → s.Wait` 完整 |
| **ticker/timer** | **有界，均正确 Stop**；P3-23 的核心问题是同步 ticker 在 `true → true` 状态更新中被无条件 Reset，另有暂停态无必要 Reset；这不是 Stop/Reset API 误用结论 | `syncer/syncer.go:199-200` ticker `defer Stop`（原引 `:193` 为快照行号）；`internal/health/supervisor.go:86-87` `defer ticker.Stop`；`internal/health/push.go` 现有**两个** timer——`:134`（非法 URL 重校验，Stop 于 `:139/:142`）与 `:152`（正常周期，Stop 于 `:157/:159`），`<-timer.C` 分支已触发无需 Stop；`run.go:62/244` 的 `time.After` 为短命启动/收尾等待。Go 1.25 默认语义下 stale tick 残留尚未被本项目证据证明 |
| **channel/订阅** | **有界，无泄漏** | `triggerCh`/`controlCh` cap=1 且 `ApplyState` 用 `select/default` 合并（合并导致 P3-24）；`EventBus.chanSubs` 取消时**同锁内 delete** 且**永不 close**（`bus.go:78-80` 有 send-on-closed 的 panic 论证）；`LogBroadcaster.subs` 取消时 close+delete；`AlertManager.Apply` 先按**旧集合**退订再按新集合订阅（顺序由 `webui/api/alertset.go:103-132` 源码可证）→ 但"重复 Apply 后订阅者不增长"**原无判别性测试**：`webui/api/alertset_test.go:161-173` 的 `TestAlertManagerApplyIsIdempotent` 只断言 `current.email/webhook != nil`，**不统计订阅者数量**；全仓亦无断言 `bus.subscribers` 不增长的用例。该缺口已由本轮新增的 `TestAlertManagerRepeatedApplyDoesNotDuplicateSubscriptions`（`webui/api/alertset_subscription_test.go`）以行为断言补齐。**判别力已验证**：以本地 HTTP mock 统计真实投递次数，临时移除 `Apply` 的「按旧集合退订」后该用例精确失败（`一次 Publish(dns:failed) 应恰好投递 1 次，实际 2 次`），恢复生产代码后通过；`-race -count=20` 通过。**边界**：本用例只覆盖「重复 Apply 与切换集合不累积订阅」，不代表 `subscribers` 表在任意并发时序下无泄漏 |
| **HTTP body/连接** | **关闭完整**，两处可优化 | 全部 outbound body 均已关闭：`notifier/webhook.go:130-134`（defer 闭包内 Close，`93e0e4b` 起关闭错误记固定 `response_close` WARN）、`internal/health/push.go:252`；**生产 `io.ReadAll` 共 2 处且均有界**：`webui/api/decode.go:78` 由 `http.MaxBytesReader` 界定（1 MiB/10 MiB），`notifier/webhook.go:139` 由 `io.LimitReader` 界定（16 KiB+1）。未 drain 见 P3-21 |
| **SQLite rows/事务** | **完全干净** | 6 处 `rows` 全部 `defer Close()`；`ensureColumnTx` 三条错误分支显式 Close；所有事务 `committed` 标志 + defer Rollback（仅忽略 `sql.ErrTxDone`）；`config/store_error_test.go:23` 证明 panic 也回滚；commit 失败**不 apply 不发布** |
| **SSE** | **有界且退出闭合** | 两条流均 `defer unsubscribe()`，均 select `ShutdownCh`；`probeSSE` 在写响应头**之前**；每次写出独立 5s deadline（`webui/api/sse.go:10`）；订阅 channel 容量固定 `logRingSize+256`；**不存在"重连新开而不关旧"的累积**（`EventSource` 单实例） |
| **日志与集合** | **历史审计：两处例外；扫描重复/环路已补保护** | `sync_logs` 裁剪至 1000（`config/store.go:984`）；`GetSyncLogs(100)`；`LogBroadcaster.ring` 固定 `[1000]string`；前端 `logLines` 上限 1000；`scanned_resources` 按 cloud_type+region **覆盖式**。历史例外：熔断器域名 map（P3-01 已本地修复）；ECS 资源扫描重复/环路（P3-25）本轮已修复，但不断产生全新 token 的服务仍无整扫描总时限；同步路径守卫已修复 |
| **配置快照** | **有界且不可变**（回收路径证据不足） | 每次配置变更创建一个 `RuntimeState`，旧状态与旧 `ClientPool` 被丢弃；`rc.DeepCopy()` + `DeepCopyRules` 确保发布后不可变；旧 SDK client **未显式 Close**（本身不是泄漏），但其空闲连接是否由 transport `IdleConnTimeout` 回收**证据不足**——全仓除本报告外 `IdleConnTimeout` 零命中，`provider/`/`notifier/`/`internal/health/` 亦无显式 `http.Transport` 配置，回收依赖 SDK/stdlib 默认值，未经核实 |
| **前端响应式状态** | **有界** | `logLines` 1000 封顶；dry-run 结果每次覆盖不追加；扫描缓存按 cloud_type 覆盖；无 `localStorage`/`sessionStorage`/`cookie`/`console.*` |
| **Docker/进程资源** | **历史资源验收保留；P1-02 已本地修复** | 镜像非 root（`uid=1000(appuser)`）、`/app/data` 属主正确、`wget` 存在（BusyBox `/usr/bin/wget`）、`HEALTHCHECK` 指向静态 `/api/health`（30s/3s/10s/3）、容器 `healthy`、`docker stop` 0.125s 且 `ExitCode=0`、无 OOM。**后续 I-01 已取得 Linux/amd64 残留 PID 与 SIGKILL 同卷重启证据，见 P1-02** |

**结论**：本项目的资源管理**明显优于**同规模项目。DNS 熔断器历史域名累积（P3-01）已本地修复；仍需关注 ECS 资源扫描全新 token 持续推进时的总扫描时限；P3-25 重复/环路已在扫描路径修复，同步路径的同类问题亦已修复。

---

## 4. 冗余、死代码和兼容残留

### ✅ 确认可删除（已双重核实：静态引用 + 测试引用 + 路由/embed/构建 + 文档）

| 目标 | 位置 | 引用检索结果 | 删除影响 |
|---|---|---|---|
| `Config` + `LoadConfig` + `ToConfig` | `config/config.go:151`、`config/store.go:1025`、`config/runtime.go:116` | 生产 **0**；仅 5 个测试文件。自述"过渡兼容，最终由 RuntimeConfig 取代" | 需同步改 5 个测试改用 `LoadBusinessSnapshot().ToRuntimeConfig()`。**最高价值清理项** |
| `EventRuleChanged` | `notifier/bus.go:17` | **全仓 1 处（仅定义）**，零生产者零订阅者；AGENTS.md:210 只枚举三种事件类型，未提它 | 无影响，不与 AGENTS 冲突 |
| `ResolvedIPs` | `provider/provider.go:78` | 全仓 2 处（注释 + 定义），零使用 | 无影响 |
| `SyncEvent` | `webui/frontend/src/types.ts:117` | 全仓 1 处（仅定义） | 无影响 |
| `clearAllCache` | `webui/frontend/src/composables/useScannedResources.ts:44/62` | 全仓 2 处（定义 + 导出），零调用 | 无影响（其注释声称"「清空所有数据」后调用"，实际 `resetAll` 只做 `location.reload()`） |

### 🟡 高可信删除候选（建议清理，需一并改测试）

| 目标 | 位置 | 引用结果 |
|---|---|---|
| `retrySync` / `retrySyncDetailed` / `truncateDesc` | 原 `syncer/retry.go`（历史位置） | **2026-10-02 P3-07 已按 B 删除**；实施前现生产入口不可达，retrySync 的 15 个调用均来自测试；有效回归已迁移，见 Issue6 §7.9 |
| `PusherDeps.bus` + 相应断言 | `internal/health/push.go:60/91`、`push_test.go:443-462` | 只赋值不读；该断言**永真**（该包任何路径都不 Publish）→ 删字段或改为真正可失败的守卫 |
| `useDryRun.error` / `useSettings` 多余导出 | `webui/frontend/src/composables/useDryRun.ts:32`、`useSettings.ts:70` | 写而不读 / 导出未解构 |
| 仅测试使用的导出 | `Store.SetSetting`（`store.go:363`）、`SaveAlertEmail`（`:777`）/`SaveAlertWebhook`（`:826`，非 Tx 版）、`GetSettingsTx`（`:962`）、`TargetExistsTx`/`RuleExistsTx`（`:619-627`）、`SetBeforeRoundHook`（`syncer.go:466`）、`AlertManager.Current`（`alertset.go:148`）、`Server.ServeStarted`（`server.go:135`）、`Pusher.InFlight`（`push.go:180`）、`InFlightLimiter.InFlight`（`inflight.go:48`）、`ErrNoSnapshotLoader`（`coordinator.go:57`，全仓无 `errors.Is` 比较） | 生产 0（或仅声明处） | 建议改非导出而非删除——其中 `stop_gate_test.go` 依赖 `SetBeforeRoundHook` 做 A20 stop 门控回归，**有判别价值** |

### ⛔ 暂不能删除

| 目标 | 原因 |
|---|---|
| `syncer/ratelimit.go`、`NormalizeResourceID`、`SaveAlertEmailTx`、`SaveAlertWebhookTx` | **有生产调用**（`syncer.go:813`、`config/validate.go:84`、`config/store.go:1297/1300`） |
| `Syncer.Pause()` / `Resume()` | `webui/api/deps.go:20-25` 明确记录：**API 侧接口成员已删除，`syncer` 侧实现必须保留**（Build6 要求 + 包内测试使用）。若确定不再需要，需先修改文档 |
| `MultiHandler` / `NewMultiHandler` 降级为非导出 | 会撞 AGENTS.md:208「`MultiHandler` 统一定义在 `app/logutil.go`」→ **需先修改强要求** |
| `webui/api/redact_test.go`（无对应 `redact.go`） | 这不是死代码：它是针对现有写入器的 sentinel 泄漏断言测试，**属正向控制**，保留 |
| `docker-compose.yml.example:42` 的 `pgrep` 字样、`webui/server.go:40` 的 `serveOnce` 字样 | 均仅出现在**注释**中（声明不使用/描述修复前行为），属预期残留 |

### 🔁 重复实现

| 项 | 位置 |
|---|---|
| 默认值多处真值源：`"587"` / `"dingtalk"` | `config.DefaultAlertPort`/`DefaultWebhookChannel`、DDL `config/store.go:175/187`、`webui/api/bundle_v3.go:353/363` **硬编码字面量**（同文件 `alerts.go:82/91` 却用了常量） |
| `map[string]bool` 做 presence 检查 | `webui/api/bundle_v3.go:617-626`、`:786-795` → **Go map 迭代随机**，同一次非法请求的错误文案跨进程不稳定；同文件 `normalizeBundleSettings` 已用显式顺序 if 链（正确写法） |
| 前端 `theme` 双写 | `Settings.vue:127-141` 与 `useSettings.setTheme` 各写一次 |
| 三处独立健康/状态派生 | ✅ **已由 P2-08 修复**：`Dashboard.vue:52-70` 现只消费后端 `last_round.outcome`（`failed`/`partial`/`cleanup_deferred`/`success`/`idle`），全文件无"停滞/stalled"派生 |
| 前端重复实现后端校验 | `Settings.vue:14` 的 `intervalPattern` 比 `config/validate.go:172-179` 更窄 |

### 🛡 过度防御（无收益复杂度）

| 项 | 判定 |
|---|---|
| `mustDuration`/`mustThreshold` 解析失败**静默返回 0**（`config/runtime.go:137-152`），而注释称"非法值不可能到达这里" | 若真为 0，`syncer.go:193` `time.NewTicker(0)` **会 panic**（Go 公开契约）。生产两条路径都经 `normalizeSettings`，**当前不可达**；属"把不该发生转成必然崩溃输入"的负收益兜底。建议改为 fail-fast 或使用文档化默认值 |
| `internal/health/health.go:153/185` `slices.Compact` | 只去**相邻**重复，当前追加序列不可能重复 → 不可达的冗余防御，且**并非通用去重**（未来新增原因来源会静默失效） |
| `internal/health/health.go:194-198` 有 `Policy == nil` 检查，而 `Evaluate`（`:130-132`）直接调用无保护 | 防御强度不一致 |
| `internal/health/push.go:190-196` 先 `buildPushURL` 校验后 `_ = target`，`:220` 再完整重算 | 无用计算（保留首次调用的**校验**语义，消除赋值/重算） |
| `webui/server.go:303-306` 吞掉 `fs.Sub` 错误并按需禁用静态处理器 | 实践中不可达（embed 保证成功）；若可失败则整个 WebUI 变空，**静默降级**掩盖真实故障 |
| `internal/health/push_test.go:16-18,497-499` 的 `var _ =` | 唯一作用是让三个无用 import 通过编译，属死测试脚手架 |

### 📝 只需文档清理

Step 0 已修正 `Design5.md` 当前 version 3 口径、Build7 状态与 `ProdTestList.md` 范围冲突。剩余仅文档清理仍包括：`Build6.md:3`/`Issue5.md:5` 历史措辞、`Issue6.md:5,7,597` 基线对账、约 12 处测试内 "version 2" 注释、`webui/frontend/src/constants.ts:1/21`、`notifier/email.go:57`/`webhook.go:41` 与 `internal/health/push.go:103-104`。

---

## 5. 设计与运行逻辑评估

| 子系统 | 稳定设计 | 脆弱点 | 不必要复杂度 | 推荐简化方向 |
|---|---|---|---|---|
| **startup/shutdown** | 启动顺序确定性化（`go s.Run()` → 有界等 `Started()` → 再启 `supervisor`/`pusher`）；信号在 HTTP 绑定**之前**注册；收尾顺序 `HTTP shutdown ‖ pusher→supervisor→syncer` 正确；`store.Close()` 在 `s.Wait()` 之后 | P1-02 已由 I-01 本地修复；`Server.Start` 失败后 `started` 保持 true 且 `waitDone` 永不关闭（重试被拒、`Wait()` 永久阻塞，**当前接线不可达**）；`supervisor.Stop`/`pusher.Stop` 在 `Run` 从未启动时永久阻塞（Syncer 有 `runGuard`，这两个没有） | — | P1-02 flock 已完成；与 Syncer 对齐给 supervisor/pusher 加 `started` 守卫 |
| **配置事务与运行时发布** | **本项目最强的一环**：协调器 `锁 → 单事务 → 事务内快照 → 事务内构造候选 → commit → 无失败发布`；commit 后不读库不访问网络；`commit` 失败不 apply；`RuntimeState` 深拷贝 + 单锁替换；已证明**事务内无任何网络 I/O**（四个 SDK 工厂只做本地构造，无 IMDS/元数据/token 获取） | P2-04 已本地修复：ApplyState 先于 Wake | — | 本轮判别性测试证明两条分支唤醒时新快照可见 |
| **同步调度** | 单一控制通道 + 4 处 `beginRound()` 硬门控（stop 门控与 enabled 门控**并列不合并**）；`Stop` 为吸收态且 `doneCh` 单所有者；`idle/failed/partial/success` 判定清晰 | P3-23 的无条件 Reset；P3-24 通知合并 | — | 仅 interval 实际变化时 Reset；`false → true` 保留恢复立即轮；按 Go 1.25 默认合同不加入 stale-tick drain，旧兼容模式另行裁决 |
| **DNS/Provider** | 仅增量 API；严格 TAG 所有权与 canonical FunctionalKey（comment 不参与身份）；普通配置发布裁剪 breaker、导入 Reset；正式 `syncTarget` 每 attempt 重新 S0/规划/Add/S1/验证/条件清理，确认进度跨 attempt 累计 | P1-01 本地核验已收口但真实云未验收；`isRetryable` 保留腾讯 SDK 无 Unwrap 所需字符串兜底；失败重试后的残留计数独立观察见 P3-07 补记 | P3-07 三个不可达旧函数已删除；P3-09 已按 A 订正 `_txlock` 注释并实现写事务提前预留（见补记） | I-09 本地收口；GetRules 有连接测试生产消费者，旧 Diff/P0-01 回归保留，不扩大清理 |
| **告警** | 默认全关；`渠道开关 + 触发开关`同时开启才订阅；邮件与 Webhook **共用同一固定渲染器**（顺序稳定、不遍历 map）；4 在途 + 满载丢弃最新 + 安全 WARN；限流器跨热重载连续；`test-email` 8 字段契约两侧严格一致且不写库；P3-11 写库错误已能上抛 | P3-03 非空非法 URL 周期校验已本地修复（P2-04 时序亦已修复）；P2-05 已补齐三渠道响应校验与有界读取，真实 Webhook 未验收；P3-15 丢弃日志逐条 WARN；P3-19/P3-20/P3-21 邮件与响应体细节 | — | P2-05 已本地实施；聚合丢弃日志；P3-03 已按独立授权补齐有下限的定时校验，真实 Uptime Kuma 仍待验收 |
| **OperationalHealth** | **唯一计算源被三个消费者真实共用**（`supervisor` / `operational` 端点 / `pusher` 都走同一个 `*health.Checker`）；2s 非阻塞探活（`Store` 结构体**无互斥量**，不持应用锁）；`StartupGrace=10s` 三分支正确；`failed/partial` 直到被 `success/idle` 覆盖；原因稳定去重排序；30s 边沿监督器 | 判定输入来自三次独立 `Snapshot()`（`run.go:127-142`），注释自述"一致快照"但可能混用新旧 policy/interval → 30s 内一次瞬时误判，自愈 | `slices.Compact` 冗余 | 一次取 `*RuntimeState` 后派生 policy/interval |
| **HTTP/SSE** | 严格解码齐全（未知字段/尾随/多顶层值/10 MiB/1 MiB/413）；路径 ID `strconv.Atoi` 且 >0；请求 DTO 不含 DB `id`；导出 GET 已删（实测 405）；两类 SSE 监听服务器级 `ShutdownCh` 且每次写出有 5s deadline；P3-05 已实施普通 JSON 成功/错误统一 `no-store` | P3-14 400/503 语义；`GET /api/alerts` 4 次非事务读存在撕裂窗口（PUT 单事务写，读侧可能"新 policy + 旧 email"，前端整体回传即把旧值写回） | `fs.Sub` 静默降级 | 普通 JSON 禁缓存已由 P3-05 收口；GET alerts 改只读事务取快照 |
| **前端** | 8 字段测试邮件载荷两侧严格一致（历史上真实 bug 点，现有注释+类型双重防护）；无 `console.*`/存储/cookie 泄漏；密码与 Webhook/Push URL 用 `type="password"`；导出用 `fetch`+Blob 且 `revokeObjectURL`；EventSource 单实例且卸载关闭；无 `addEventListener` 泄漏 | P3-04 已本地修复（浏览器待验）；P3-16 一批 UI/状态偏离 | 前端重复实现后端校验 | P2-06/P2-07 已本地收口；其余逐项按 P3 收敛；**不建议**引入 Pinia/Vitest 等重型栈 |
| **Docker/CI** | 非 root + `CGO_ENABLED=0` 静态编译 + `HEALTHCHECK` 用静态 `/api/health`（实测全部符合）；前端在 builder 阶段构建并 `COPY` 进 Go 阶段（顺序正确）；容器实测 `healthy`、`ExitCode=0` | **清洁检出裸 Go 命令 100% 失败**（P1 级构建阻断）；P1-02 已本地修复；`go get -u` 使构建不可复现（AGENTS §十 **有意设计**） | Makefile 与 CI 命令集不统一 | 给 `test`/`vet` 加 `frontend` 前置；`go get -u` 策略受强要求约束，**不建议**擅自改为锁版本 |

### 附：构建阻断（原 P1，已并入批次 6 但严重度仍在）

`webui/embed.go:5` 的 `//go:embed frontend/dist` 依赖被 `.gitignore:5` 忽略的目录；`Makefile:6` 的 `build` 有 `frontend` 前置，但 `Makefile:9-13` 的 `test`/`vet` **没有**。在 `git archive HEAD` 抽取的清洁副本上实测：

| 命令 | 结果 |
|---|---|
| `go build ./...` / `go vet ./...` / `go test ./...` | **EXIT=1 / 1 / 1** `pattern frontend/dist: no matching files found`（root 与 `webui` 包 `[setup failed]`） |
| `make vet` | **EXIT=2** |
| 手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` | EXIT=0 |

**修复**：给 `test`/`vet` 加 `frontend` 前置依赖（Make 在同一次调用内对同一前置去重，`make all` 仍只跑一次 `npm ci`）。**不要**提交占位 `webui/frontend/dist/index.html`——它会被 embed 并由 `webui/server.go:305` 的 FileServer 当真实页面提供。

---

## 6. 测试质量与缺口

**总体（审计快照口径）**：62 文件 / 20,457 行（约为生产代码 1.9 倍，属快照 `11918fb` 值；**当前为 69 文件 / 24,896 行 vs 生产 12,606 行 ≈ 1.98 倍**）。密度高，且**确实有判别性**——Issue6 逐条记录了"红灯 → 修复 → 绿灯"。以下均为**测试源码覆盖**，本轮未执行任何门禁。

**判别性良好的核心合同**（无需改动）：事务与发布（`coordinator_test.go`、`import_failure_test.go`）、迁移与 Schema（`store_v3_test.go`、`store_dsn_test.go`）、生命周期（`stop_gate_test.go`、`started_test.go`、`webui/server_test.go`）、契约严格性（`decode_test.go`、`alerts_v3_test.go`、`export_test.go`、`redact_test.go`）、告警与健康（`inflight_test.go`、`supervisor_test.go`、`push_test.go`、`health_test.go`）、四平台 TCP+UDP/ICMP/IPv6 矩阵、CVM 100 上限。

**弱断言 / 误导**：

- `policy_test_helpers_test.go:31-35` `waitForNoSMTPData` —— **名为"断言无报文"，实为纯 `time.Sleep`，无任何断言**（4 个调用点各自补了 `rec.Data() != ""`，侥幸未失效）。**必须修正**
- `push_test.go:443-462` —— 断言 `pub.count()==0`，但该包任何路径都不 Publish → **永真、无判别力**（成因是 `PusherDeps.bus` 只赋值不读）
- `webui/api` 对 `AlertManager` 注入限流器这一步**零断言**（`grep InFlightLimiter webui/api/*_test.go` 零命中）→ 生产"每渠道 ≤4"接线只靠 notifier 包内**手工模拟**覆盖

**7 个永远无法失败的测试**（逐个读正文确认）：`dns/resolver_test.go:24-34`（`if err == nil { t.Log }`）、`:36-58`（唯一 `t.Fatal` 被 skip 保护）、`notifier/bus_test.go:108-118`、`syncer/state_test.go:411-436`、`syncer/syncer_test.go:646-670`（注释自述只靠 race detector → **不带 `-race` 时是空测试**）、`webui/api/alertset_test.go:175-179,215-217`。另 `dns/resolver_test.go:60-71` 只断言非 nil，**从未验证注释声称的"自动补 :53"**。

**固定数值契约没有字面量锚点** → 改错强要求数值仍全绿：`InFlightLimit=4`、`DefaultStartupGrace`、`DefaultHealthTimeout=10m`、`DefaultPushInterval=60s`、`MinPushInterval=20s` 的测试都用常量自身计算期望。对照良好范例：`push_test.go:243` 用字面量 `250`、`webui/server_test.go` 的 `TestServerTimeoutContract` 用字面量 `5s/120s`。

**缺失的关键路径**：

1. **前端 → 后端的载荷契约**：8 字段测试邮件与被严格解码的后端 DTO 之间**没有测试固定"前端实际发出的键集合"**（后端只测"多余字段被拒"）。历史上正是此处出过真实缺陷，当前唯一防线是两处注释
2. **构建/部署契约零守护**：`grep -rln "Dockerfile|docker-publish|Makefile|alpine:3.20|CGO_ENABLED" --include='*_test.go' .` → **0 命中**，而 AGENTS §八 用「**不得**」措辞
3. `GET /api/zones` handler —— 历史审计未覆盖路由（`zones_test.go` 只测 `zoneData` map）；2026-10-02 P3-05 的 `TestJSONCachePolicyAPIMatrix` 已补基础路由、200/JSON 与 `no-store` 检查，不等于地域内容全量验收
4. **零 goroutine/ticker/连接泄漏断言**（`grep NumGoroutine|goleak` 唯一命中是注释）
5. `provider/scan.go` 四条扫描路径几乎无测试（`scan_test.go` 仅 16 行）
6. **负面 sleep 断言会在慢 runner 假通过**：`syncer/state_test.go:174-177,204-205`、`syncer/syncer_test.go:130`；syncer 包 25 处 sleep（12 处 >200ms，最长 600ms）集中在最关键的门控用例上。`webui` 包用 `ServeStarted`/channel 做确定性同步，是正面范例
7. `app/logutil_test.go` **完全没有 `MultiHandler` 用例**
8. `-race` 非空跑（`t.Parallel()`=0，但 24 个测试文件在用例内起 goroutine），唯一边角是上述 race-only 用例

**本轮已补齐的两项**（2026-09-30 第二轮，仅测试改动）：§3「重复 Apply 订阅不累积」与 §6 中 webhook「名额保持到 Close 结束」——见 `webui/api/alertset_subscription_test.go` 与 `notifier/webhook_response_test.go` 的 `TestWebhookResponseSlotHeldUntilClose`；两者均已用「临时破坏生产语义 → 精确失败 → 恢复 → 通过」验证判别力。

**推荐新增的最小测试**（**不引入任何测试框架**）：

1. P0-01：Aliyun `GetRules→Diff` 往返收敛断言（已由 `TestDiff_AliyunPortRoundTripConverges` 补齐，Issue7 重构时必保留）
2. P1-01：同目标两条空 comment 规则的双轮断言（第二轮不得出现 delete）
3. P2-01：IPv6+ICMP key 断言（Lighthouse/CVM）
4. P2-02：ECS 150 条删除产生 2 个请求
5. P2-04：探针 waker 断言"wake 时新配置已可见"
6. ✅ P3-05 已补齐：正式缓存策略回归覆盖真实 HTTP settings 成功/失败 GET/HEAD、普通 JSON 状态矩阵及独立响应边界，三组负向控制与 20 轮定向 race 通过，见本项实施补记
7. 前端载荷契约固化在既有 Go 测试中（8 键字面量断言 200 + 带 `enabled` 断言 400）
8. 一个约 40 行的 `build_contract_test.go`（断言 alpine/golang 版本、`CGO_ENABLED=0`、非 root、HEALTHCHECK 指向 `/api/health` 且不含 `/operational`、`linux/amd64`、`.dockerignore` 含 `fwalizer-config`）
9. ~~`config/pidfile` 单测（目前**不存在**该文件）~~ ✅ **已随 I-01 补齐**：`config/pidfile_test.go`（168 行，`7aaa3f2`）覆盖真实内核锁竞争、残留/当前 PID/空/损坏内容、GC 后锁不丢、释放后同文件复用、新建 `0600` 权限、EBADF 保留原因；另有 `main_test.go` 四个进程级用例（`TestProcessPidFileRestart`/`KernelBarrier`/`ConcurrentStart`/`SecondInstanceRejectedByPidFile`）

---

## 7. 正向控制（整改时**不得**误删）

1. **配置协调器的单事务 + 无失败发布骨架** —— 防"接口报错但库已变"的唯一屏障
2. **`RuntimeState` 深拷贝 + 单锁指针替换 + 发布后不可变**（含 `DeepCopyRules`、`Credentials` 无 setter）
3. **只使用增量云 API**（零全量覆盖/重置）+ `[TAG]` 前缀 + `Description` 匹配 —— "绝不误删**非本工具**规则"的全部依据（该边界经审核确认正确）
4. **`Stop` 门控与 `enabled` 门控并列不合并**（4 处 `beginRound()`）+ `Stop` 吸收态 + `doneCh` 单所有者
5. **唯一 `OperationalHealth` 计算源**（三消费者真实共用同一 `*health.Checker`）+ 2s 非阻塞探活 + `StartupGrace=10s` 三分支
6. **每渠道 ≤4 在途限流器跨热重载保持连续**（限流器由 `AlertManager` 持有，不随 notifier 实例替换）
7. **EventBus 取消订阅不关闭 channel**（`bus.go:78-80` 有 send-on-closed 的 panic 论证）+ 快照复制
8. **两类 SSE 监听服务器级 `ShutdownCh` 显式退出** + 每次写出独立 5s deadline
9. **严格解码全套**（未知字段/尾随/多顶层值/413/10 MiB/1 MiB/路径 ID >0/请求 DTO 不含 `id`/`present targets`）
10. **`/api/health` 保持静态存活语义**，`HEALTHCHECK` 不改用 operational 端点
11. **全部有界化**：`sync_logs` 1000、环形日志 1000、前端日志 1000、`scanned_resources` 覆盖式
12. **启动顺序确定性化**（`Syncer.Started()` 有界等待后再启动 supervisor/pusher）
13. **迁移显式化**（`ensureColumnTx` 用 PRAGMA 探测、单事务、失败中止启动、只归零一次）
14. **测试用 `127.0.0.1:0` 全体零固定端口**
15. **`redact_test.go` 的 sentinel 泄漏断言**（覆盖现有写入器，非死代码）
16. **`Syncer.Pause()` / `Resume()` 实现保留**（API 接口成员已移除，实现受 Build6 约束）
17. **`isRetryable` 的字符串兜底有据**（腾讯 SDK 错误类型无 `Unwrap`）：该依据写在 `syncer/retry.go:123-175` 的 **Go 注释**中；全仓 `.md` 内不含 `Unwrap` 说明，**不得**归因为".md 注释"

---

## 8. 专题：规则身份与 TAG 所有权（本地主体与完整复核已收口，外部验收待执行）

> **本章是 P1-01 及其全部关联内容的唯一集中位置。**
>
> **状态（2026-09-30）**：用户已定案“目标级完整期望集 + TAG 所有权 + comment 纯可读 + 先增后验 + 平台化条件删除 + 无法证明安全时保留残留”，Step 1～5 主体已提交为 `28559ed`；Lighthouse 多端口与 SWAS 分页上限补强已提交为 `38bdc19`；R7-01～R7-07 均已完成并进入当前提交历史（R7-05 与文档回写为 `d6d208e`）。核心 planner/TAG/四平台安全主线与本地 R7 核验项已收口，但**真实四云、浏览器回归与当前 revision 远端 CI/GHCR 仍未执行**（PT-I7-01～07），故不能写成外部验收或发布闭环。`maxTagRunes` 仍为 48。

### 8.0 最终方案（取代本节后续的历史候选状态）

- 仅 `description == "[TAG]"` 或以 `"[TAG] "` 开头的规则属于当前所有权命名空间；`[TAG]foo` 不属于。
- comment 可空/重复/修改/截断，不参与身份或删除；不做唯一性校验。
- 功能 key 是 `address-family + canonical CIDR + canonical protocol + canonical port expression + action`，不含 TAG/comment/description/域名/本地 ID/云端 ID。
- 同一目标的全部本地规则先汇总为完整期望集，同功能去重；非 TAG 精确等价规则可满足期望但永不被操作。
- 同步固定为 `S0 → Plan → Add → S1 → coverage verification → 安全门 → 条件 Delete → 必要时 S2`；Add/Describe/验证失败时旧规则全保留。
- Lighthouse/CVM 使用同快照版本保护；SWAS/ECS 只用 S1 稳定 RuleID；ECS 删除每批最多 100。
- 只要所需功能已确认，`cleanup_deferred` 仍是 `success`/healthy；平台能力不支持是 `partial`；DNS/Describe/Add/覆盖验证失败是 `failed`。
- 不新增本地映射表、不强制 comment、不在 description 编码功能身份、不在空期望集时自动清空 TAG。

下方 8.1～8.7 保留为定案前的缺陷机理、候选方案与外部不确定性记录；其中的“待决策/已冻结”不再表示当前状态。

### 8.1 历史决策状态与冻结说明（已被 8.0 取代）

| 议题 | 状态 | 说明 |
|---|---|---|
| **身份形态** | 🔶 **待决策** | 曾定为方案 A（`[TAG] <host>/<PROTOCOL>/<PORTS>[ <comment>]`）；**已撤回**。候选方案见 8.4 |
| **唯一性校验方式** | 🔶 **待决策** | 曾定为按 `(target set, host, protocol, ports)` 校验；**已撤回** |
| **长度超限行为** | 🔶 **待决策** | 曾定为"拒绝保存"；**已撤回** |
| **TAG 上限收紧 48→32** | 🔶 **已撤回** | 该收紧**仅为配合方案 A** 而提出。`config/validate.go:34` 仍为 48；`AGENTS.md:179` 未改 |
| **旧规则迁移** | ✅ **仍然有效** | 用户已确认**无历史兼容负担** → 任何最终方案都**不需要** orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份模型无关，独立成立 |
| **SWAS `Remark` 上限** | ⏳ **待真机验收** | 已登记为 `ProdTestList.md` **PT-B7-09**（详见 8.6） |

**冻结理由**：讨论中发现本缺陷与一个更根本的设计问题耦合——云端描述字段**同时承担三重职责**（见 8.3），且存在多个各有权衡的候选修复方案（见 8.4）。需要一次性想清身份模型，而不是打补丁。

#### 🔧 代码修复上线前的操作规避建议（历史运行阶段建议，非最终设计要求）

本缺陷在修复前**仍在生效**。规避方法很简单：

> **避免在同一目标上出现两条 comment 相同的规则**——尤其是两条都留空 comment（这是默认值，最容易触发）。

- 同 host 多规则（如 TCP 443 + UDP 443）→ 给两条填**不同备注**
- 不同 host 但备注都留空 → 至少给其中一条填备注
- 已存在碰撞配置时，**不会**有告警提示（这也是缺陷的一部分：`failed==0`，运行健康不报）

### 8.2 缺陷机理（完整因果链）

#### 8.2.1 三个事实（均已核对源码）

**事实 1：进入云端的"身份"只有 `desc` 一个字符串。**

```go
// syncer/retry.go:56
desc := truncateDesc(tag.Format(tagStr, rule.Comment), p.CloudType())
```

云端不保存"这条规则属于哪个 host"。工具重启后，唯一能从云端读回来、用于判断"这条规则是不是我的、属于哪条规则"的依据就是规则的描述字段。

**事实 2：`desc` 的取值只由 `tag + comment` 决定，而 `comment` 可空。**

```go
// internal/tag/tag.go:12-14
if comment == "" {
    return fmt.Sprintf("[%s]", tag)      // → "[auto-dns]"
}
return fmt.Sprintf("[%s] %s", tag, comment)
```

`comment` **允许为空**（`config/validate.go:97`「comment 允许为空，按原值保存」），且**无任何唯一性约束**。因此两条都留空 comment 的规则，`desc` **完全相同**。

**事实 3：`Diff` 完全靠"描述字符串精确相等"划定归属，并据此删除。**

```go
// provider/common.go:144-148
for _, r := range existing {
    if r.Description == desc {            // ← 唯一的归属判定
        domainExisting = append(domainExisting, r)
    }
}

// provider/common.go:177-182
for _, r := range domainExisting {
    k := keyOf(r)
    if !desiredKeys[k] {                  // ← 不在本规则期望集内 → 删除
        toDelete = append(toDelete, r)
    }
}
```

`desiredKeys` = 本规则当前**解析出的 IP** × 协议 × 端口。所以"清理陈旧 IP"正是靠这一步实现的。

#### 8.2.2 因果链

```
desc 只由 tag + comment 决定，comment 可空且无唯一性约束
        ↓
同一目标上两条规则可产生相同的 desc
        ↓
规则 A 同步时，规则 B 的云端规则因 desc 相同而进入 domainExisting
        ↓
B 的规则不在 A 的 desiredKeys（IP 不同）→ 被列入 toDelete
        ↓
规则 B 同步时反过来删除 A 的规则 → 互删，逐轮振荡
        ↓
最终稳定态：只剩最后被处理的那条规则的 IP 被放行，其余持续被拆除
且 failed==0 → 运行健康不告警（静默失效）
```

#### 8.2.3 已复现的证据（overlay 探针，仓库零写入）

```
规则A（comment 空，a.example.com）首轮: ToAdd=1
规则B（comment 空，b.example.com）对 A 的既有规则:
    将被删除: cidr=1.1.1.1/32 desc="[auto-dns]"
>>> ToDelete=1   （B 的同步把 A 的规则列入待删除）
FAIL
```

**边界（正确的部分）**：只删除本工具自己的规则，**不触碰非本工具规则**——`OwnedRules` 的 `[TAG]` 前缀过滤是有效的。

#### 8.2.4 两条产生碰撞的路径（最终方案必须同时覆盖）

| 路径 | 条件 | 示例 |
|---|---|---|
| **路径 1：不同域名 + 相同 comment** | 两条规则 comment 相同（**最常见：都留空**），解析到不同 IP | `api.example.com` 与 `www.example.com`，comment 均为空 |
| **路径 2：同一域名 + 多协议/多端口** | 同一 host 的多条规则，comment 相同 | `api.example.com` 的 TCP 443 与 UDP 443（**AGENTS §三 的 TCP+UDP 拆分机制本身就会产生**） |

> ⚠️ **仅给 desc 加 host 只能覆盖路径 1**。路径 2 的规则 host 相同，desc 仍会碰撞。这是方案 A 必须同时包含"协议 + 端口"的原因，也是本项研究不可忽略的一半。

### 8.3 结构性洞察：描述字段的三重职责

| 职责 | 内容 | 消费者 | 长度需求 |
|---|---|---|---|
| **A. 归属识别** | "这条规则是我的" | 工具（`OwnedRules` 按 `[TAG]` 前缀） | 固定（`[TAG] ` 前缀） |
| **B. 个体识别** | "这条规则对应我本地哪条规则" | 工具（`Diff` 按 desc 精确相等） | **可变且无界**（需要唯一） |
| **C. 人类可读** | "这条规则是干什么的" | 用户（云端控制台） | 可变、可截断 |

**结构性矛盾在于职责 B**：它要求"唯一"，而被编码进去的内容（域名）长度**无界**，字段容量却**定长**（Lighthouse 64、SWAS 50）→ 定长容器装不下无界数据 → **与 `[TAG]` 争夺同一段空间**。

这正是"TAG 变长会挤压域名"这一副作用（本报告早期版本曾详述）的根源，**且与用户是否想缩短 TAG 无关**——只要职责 B 仍由描述字段承担，容量矛盾就存在。

**用户的关键判断（2026-09-28）**：

> *"云端不需要展示完整的域名。备注是给人看的，我只需要 `[TAG]` + 用户自定义备注（可空），此处不需要完整域名。"*

该判断直接指向出路：**把职责 B 从描述字段上摘下来**，描述只保留 A + C，长度便只与 TAG 和备注有关，与域名彻底解耦。

**但必须注意连带后果**：职责 B 一旦摘掉，`Diff` 现有的**删除边界**（`r.Description == desc` 筛选）也随之失去依据——要保留"清理陈旧 IP"的能力，就必须为归属判定提供**新依据**。这是所有后续方案的分水岭：

> 描述里**放域名**是为了让归属筛选能区分不同规则；描述里**不放域名**，归属筛选就必须换依据。二者必须选一个。

---

### 8.4 历史候选方案（当前方案已由 8.0 裁决）

#### 8.4.1 四云可用字段核对结果

已逐个核对 `PlatformAPIDocs/`，结论：**"给域名另找一个字段存"无法作为通用方案**。

| 云 | 描述字段上限 | 是否有可用的独立字段 | 是否可读回（Diff 必需） | 结论 |
|---|---|---|---|---|
| Lighthouse | **64**（`TencentLighthouseAPIGuide/添加防火墙规则.md:15`） | ❌ `FirewallRules.N` 结构内无 Tag/Name | — | 不可行 |
| SWAS | **50**（来源存疑，见 8.6） | ✅ 有 `Tag`（Key/Value 各 ≤64），且 `ListFirewallRules.md:91-97` **确实回传 `Tags`** | ✅ | **仅此一云可行** |
| CVM | 未声明上限 | ❌ 查询安全组规则只回传 `PolicyDescription` | — | 不可行 |
| ECS | **512**（`AliyunECSAPIGuide/AuthorizeSecurityGroup.md:128`） | ❌ 参数表中无 Tag | — | 不可行 |

即便 SWAS 可行，其余三云仍只有描述字段可用 → 会导致四个 Provider 各走一套身份逻辑，与 AGENTS §十一 的收敛方向相悖。

#### 8.4.2 候选方案对比

| # | 方案 | 容纳任意域名 | TAG 完全自由 | 保留陈旧 IP 清理 | 跨四云一致 | 主要代价 |
|---|---|---|---|---|---|---|
| **A** | 描述含 `host + 协议 + 端口`，超限拒绝保存 | ❌（SWAS 最紧） | ❌（与域名互斥） | ✅ | ✅ | 容量耦合；TAG 与域名互相挤压；需收紧 TAG 上限；旧规则 orphan |
| **①** | 描述保持 `[TAG] comment`，**新增本地"规则 → 已下发 CIDR"状态表**做归属判定 | ✅ | ✅ | ✅ | ✅ | 引入持久状态；需处理"首次运行无状态"与"状态与云端漂移"的自愈 |
| **②** | 描述保持 `[TAG] comment`；**同 desc 分组，碰撞时不做删除**（只增不删） | ✅ | ✅ | ⚠️ 碰撞时暂停 | ✅ | 碰撞期间陈旧 IP 不被清理（可恢复：改掉重复 comment 即自愈） |
| **③** | 描述保持 `[TAG] comment`；**保存时强制"同一目标内 comment 唯一"** | ✅ | ✅ | ✅ | ✅ | 由于 comment 默认为空，实际会**强制用户为每条规则写备注** |
| **④** | 描述用定长本地标识（如 `[TAG] #<ruleID>` 或短哈希） | ✅ | ✅ | ✅ | ✅ | 云端控制台不可读（只见编号）；哈希方案存在非零碰撞 |
| **⑤** | 折中：`[TAG] #<ruleID> host`，**域名可截断、编号保唯一** | ✅ | ✅ | ✅ | ✅ | 描述格式最复杂；仍需实现截断与编号解析 |

#### 8.4.3 各方案的初步评估（供后续研究，非定论）

- **方案 ②** 的突出优点：改动最小（仅 `syncer/` 包内约 30 行；`provider.Diff` 签名与逻辑不动、store/schema/API/前端全不动、14 处测试调用点不动），且**取舍方向无脑正确**——把"可能误删云端规则（不可恢复）"换成"可能少删陈旧 IP（可恢复）"。其技术核心是在调用方（`syncer` 能看到同目标的全部规则）判定碰撞并清空 `toDelete`，两个调用点（正式同步 `syncer/retry.go:57`、Dry Run `syncer/syncer.go:625`）共用同一 helper。
- **方案 ①** 最彻底，但引入持久状态后必须回答"状态与云端漂移如何自愈"，复杂度显著上升。
- **方案 ③** 与方案 ② 同源，但把负担转给用户，且因 comment 默认为空而带有强制性。
- **方案 ④/⑤** 彻底解耦长度，但牺牲云端可读性——而可读性恰是 TAG 存在的价值之一，需用户判断其重要性。
- **方案 A** 已被撤回，不应作为默认选项。

### 8.5 与旧版 AGENTS.md 状态的关系（历史，不得据此实施）

- **无冲突的部分**：`[TAG]` 前缀识别归属（§三）在任何方案下都必须保留，这是固定成本。
- **潜在冲突的部分**：
  - 若最终采用**方案 A** 并收紧 TAG 上限 → 需修订 **AGENTS.md:179**「TAG … 最多 48 个 Unicode 字符」（属**强要求变更**，必须用户明确授权）。
  - 若最终采用**方案 ③**（强制 comment 唯一）→ 需确认是否与 §十一「普通 API 最小校验边界」的宽松取向冲突。
- **当时状态（2026-09-28，历史）**：方案尚未定，`AGENTS.md` 未修改；2026-09-29 Step 0 已用 8.0 的最终方案取代该状态，`maxTagRunes` 仍为 48。

### 8.6 待消除的外部不确定性

**SWAS 的 `Remark` 上限"50"在仓内 API 文档中查无出处。**

- `syncer/retry.go:200` 的注释写 `Remark ≤ 50 字符（阿里云 SWAS API）`，但 `PlatformAPIDocs/AliyunSWASAPIGuide/` 下所有提及 `Remark` 的文件（`CreateFirewallRules.md:48`、`CreateFirewallRule.md:45`、`ListFirewallRules.md:61`、`EnableFirewallRule.md:40`、`DisableFirewallRule.md:40`、`ModifyFirewallRule.md:43`）**均只描述字段、未给长度限制**。
- 影响：若真实上限 > 50，则当前实现会**过度保守**；若 < 50，则会在云端失败而非在本地被拦住；若按**字节**而非字符计算，中文备注预算再缩 3 倍。
- 该值对**方案 A** 是决定性的（决定 cap 常量），对**方案 ②** 则仅影响 `[TAG] comment` 的总长。
- **已登记为 `ProdTestList.md` PT-B7-09**（真机确认，含字符/字节口径）。**在确认前不得把 50 当作已核实事实。**

### 8.7 历史待决策清单（已由 8.0 裁决）

| # | 待决策项 | 说明 |
|---|---|---|
| 1 | **身份模型**：选 A / ① / ② / ③ / ④ / ⑤ 中的哪一个（或组合） | 决定后续全部实现 |
| 2 | 若选 **A**：desc 需含 host + **协议 + 端口**（否则路径 2 未修复）；长度超限行为（拒绝保存 / 截断 / 哈希降级）；是否收紧 `maxTagRunes` 并修订 AGENTS.md:179 | |
| 3 | 若选 **①**：本地状态表的形态、首次运行无状态的处理、与云端漂移的自愈策略 | |
| 4 | 若选 **②**：碰撞时"只增不删"是否可接受；WARN 的文案与字段 | |
| 5 | 若选 **③**：是否接受"每条规则必须写不同备注"（因 comment 默认为空） | |
| 6 | 若选 **④/⑤**：云端不可读是否可接受；用 ruleID 还是哈希 | |
| 7 | **云端描述字段的实际容量**：Lighthouse 64 已由文档确认；SWAS 50 待 PT-B7-09 真机确认；CVM 上限未声明 | |

### 8.8 决策状态记录

| 议题 | 状态 |
|---|---|
| identity 形态 | ✅ 目标级功能 key/完整期望集；description 只保留 TAG + comment |
| 唯一性校验方式 | ✅ 不引入 comment 唯一性/必填校验 |
| 长度超限行为 | ✅ 只影响可读 comment 截断，不影响功能身份 |
| TAG 上限收紧 48→32 | 🔶 **已撤回**（`config/validate.go:34` 仍为 48；AGENTS.md:179 未改） |
| 旧规则迁移 | ✅ **仍然有效**：用户确认**无历史兼容负担** → 任何最终方案都不需要 orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份模型无关，独立成立 |
| SWAS `Remark` 上限 | ⏳ 已登记 **ProdTestList.md PT-B7-09**，待真机确认；只影响 comment 可读长度，不阻塞 Issue7 Step 1～4 |

---

## 9. 用户决策记录（决策不等于已实施）

> 本表保留作出决策时的原始记录。当前实施状态统一看第 0 节：P2-04 的目标顺序已在 Issue7 Step 0 修订，代码与判别性测试已于 2026-09-30 独立完成并提交为 `7acf303`；P2-03 原“只补 WARN/Dry Run、不计 skipped”的记录已被当前 AGENTS/Issue7 的 `unsupported → partial` 与清理冻结合同取代。保留旧记录是为了追溯，不得据此覆盖当前强要求。

| # | 议题 | 你的决策 | 实施要点 |
|---|---|---|---|
| 1 | P1-01 规则身份 | ✅ **已实施（本地，`28559ed`）** | 采用目标级完整期望集、TAG 唯一操作授权、comment 纯可读、先增后验、平台化条件删除与可接受残留；不收紧 TAG 48、不需要迁移。详见第 8 节与 Issue7 Step 1～5 |
| 2 | P1-02 容器 pidfile | **改用 flock 文件锁替代 PID 判活** | 见 P1-02 章节；顺带消除 P3-08（TOCTOU 漏判 + PID 复用误判） |
| 3 | P2-04 唤醒顺序 | **改代码：`ApplyState` 提到两次 `Wake()` 之前，并同步修订 AGENTS.md:216** | 见 P2-04 章节；`coordinator.go:50-53` 注释同步 |
| 4 | P2-07 删除确认 | **补卡片式二次确认，满足 AGENTS §十一** | 已按定型方案实施两页卡片确认、四阶段状态、列表顺序保护和焦点恢复；本地联合验收通过、尚未提交；**不改强要求文档** |
| 5 | P2-03 静默跳过 | 📚 **审计快照中的旧决策：先只补 WARN 日志 + Dry Run 展示，不计入 skipped** | **已被当前强合同取代**：能力限制进入结构化 `unsupported`，目标结论为 `partial` 并冻结清理；见 P2-03 当前实施补记与 Issue7 Step 1。此行仅用于追溯，不得据此实施 |
| 6 | P3-01 熔断器淘汰 | **细化后的 A 已提交 `ac0ee62`，后由 P3-02 统一域名身份** | 成功删除、普通发布按配置域名复制正数计数并重建紧凑 map（后由 P3-02 统一身份）、新旧快照独立、导入 Reset；C 单独裁剪已可阻止历史累积，但缺少成功即时清理，原评价已订正 |
| 7 | P3-12 文件权限 | **收敛为 0700/0600，已按细化 A 本地实施** | 最终目录显式0700；主库在SQLite首次访问前0600，已有WAL/SHM打开前及初始化后迁移；同FD持锁后锁文件0600；失败即退出；取消默认路径`.`回退。正式证据见当前实施补记 |

---

## 10. 当前实施顺序与历史批次

### 10.1 当前有效主线：Issue7 Step 0～5

**Issue7 P1-01（Step 0～5 主体提交 `28559ed`；F1/F5 补强提交 `38bdc19`；R7-01～R7-07 本地复核项已收口，追踪证据见 Issue7 §12.5）**

1. ✅ Step 1 纯规划器保留已修复 P0-01/P2-01 的绿色回归，按当前合同吸收 P2-03，并修复 **P3-25 同步路径**安全前置；P3-25 资源扫描路径不在该完成结论内。
2. ✅ Step 2 实现目标级 Add → Describe → coverage verification 与安全门，Step 3 起门开即条件删除。
3. ✅ Step 3 按 Lighthouse/CVM/SWAS/ECS 逐平台开启条件清理，同时吸收 P2-02。
4. ✅ Step 4 主体已落地；R7-02 已提交为 `eab4bea`，R7-03 已按裁决 A 修复并提交为 `297ccfe`，Dry Run 展示所有已配置目标且无适用规则目标零云调用；Dashboard/健康主线与 P2-08/P2-09 修复保持成立。
5. ◧ Step 5 本地二进制/Docker 与主体门禁证据已取得，F1/F5 补强已提交，R7-01～R7-07 本地核验项已收口；**PT-I7 四云/浏览器真实验收**仍未完成。

> P0-01 与 P2-01 均是必保留的绿色回归；P2-01 已在唯一 canonical functional key 中修复，不再是失败先行项。P1-02/flock 已在独立 I-01 本地修复，不是 P1-01 的前置。

### 10.2 当前独立问题入口

与 Issue7 正交的 P1-02/P3-08 已本地收口，P2-04 与 P3-03 亦已独立本地收口，P2-05 已按方案 A 修复并提交为 `93e0e4b`，P2-06 已按完善后的方案 A 本地收口（已提交为 `064b794`），P2-07 已按定型方案本地收口（尚未提交），其余 P3 继续按第 0.4 节的 I-01～I-11 排队；P1-01 完整复核及后续研究形成的 R7-01～R7-07 对应 I-12～I-18。该编号只表示 backlog 顺序，不改变 finding 严重级别，也不构成代码实施授权。

### 10.3 历史批次计划（与 Issue7 重叠部分已被取代）

> 下列批次是上一版报告形成时的排序记录，完整保留用于追溯。凡与 Issue7 Step 1～5 重叠的 P2/P3，均以 10.1 和 Issue7 为准；不得按本节另建第二套并行施工顺序。

**历史批次 2 — 其余云端规则正确性（原始排序，当前状态以 0.2/0.4 和 Issue7 为准）**

P2-01（IPv6+ICMP key，已由 Issue7 修复）、P2-02（ECS 删除分批，已由 Issue7 修复）、P2-03（旧“补 WARN + Dry Run”方案已被 `unsupported → partial` 取代）、P2-04（唤醒顺序 + 文档）、P2-05（Webhook errcode）、P2-06（告警页守卫）、P2-07（删除确认，已本地修复）

> 验收：每项一个判别性用例；P2-02 需 150 条删除分批断言

**历史批次 3 — 生命周期、并发与内存（原始排序，当前状态以 0.2/0.4 为准）**

P3-01（熔断器淘汰已本地修复）、P3-23（ticker Reset）、P3-24（通知合并）、P3-25（ECS 同步分页守卫已修复，资源扫描分页守卫本轮已本地修复，见 I-19）、P3-22（DryRun 限速，已由 Issue7 修复）、P3-09（已按 A 本地修复，见实施补记）、P3-12（权限，已按细化A本地修复）

> 验收：配置保存 N 次后熔断器 map 不增长；`-race` 全绿

**历史批次 4 — 状态一致性与错误处理（原始排序，当前状态以 0.2/0.4 为准）**

P3-05（普通 JSON 统一 `no-store`，已本地修复）、P3-10（忽略的 error，已按细化 A 本地修复，见当前补记）、P3-11（`StoreLogWriter` 返回错误，已由 Issue7 修复）、P3-13（MultiHandler 收集全部错误）、P3-14（400→503）、P3-21（drain + 字节上限）

**历史批次 5 — 死代码与重复实现**

§4 全部"确认可删除"与"高可信删除候选"；`bundle_v3.go` 默认值改用常量、presence 改有序切片

**历史批次 6 — 构建、测试与文档闭环**

Makefile `test`/`vet` 加 `frontend` 前置；修 `waitForNoSMTPData`；消灭 7 个永不失败的测试；补字面量锚点；补构建契约测试；补 `pidfile` 单测；清理约 12 处 "version 2" 与 §4 文档漂移

**依赖关系**：P1-01 的产品语义已确认，不再有配置形态决策前置。P3-25 **同步路径**已在自动清理前改为不完整快照硬失败；资源扫描路径现已按独立证据本地修复，见 I-19 / P3-26；真实云仍未验收。P2-04 代码已按 Step 0 修订的 AGENTS 发布顺序独立落地（2026-09-30，提交为 `7acf303`）；其余死代码与同文件清理应在 Issue7 Step 1～4 完成后再重新证明。

---

## 11. 覆盖矩阵摘要

| 模块 | 主审 | 复核 | 文件数 | 入口/调用链 | 验证 | 未验证 |
|---|---|---|---|---|---|---|
| 启动/生命周期（`main.go`,`run.go`,`app/`） | A | 主代理 | 3 生产 | `main → run → runWebUI → 启动/收尾序列` | 容器 SIGTERM 实测 `ExitCode=0`；双实例 pidfile 实测；**历史 Docker 实测曾复现 P1-02** | I-01 已覆盖残留 PID、内核锁屏障、并发、SIGKILL 重启；真实部署机 systemd 仍未执行 |
| 配置/DB/运行时（`config/`） | A | 主代理 | 7 生产 | `OpenStore → initTables → LoadBusinessSnapshot → BuildRuntimeState` | `config` 包 `-race` 绿；驱动源码核对 | 真实旧库迁移路径 |
| 同步/DNS/重试（`syncer/`,`dns/`,`internal/`） | **B** | **主代理 overlay 探针复现 P0-01/P1-01/P2-01** | 7 生产 | `Run → beginRound → syncAll → runRound → syncDomain → retrySyncDetailed` | `syncer`/`provider`/`dns` 包测试绿 + 3 个判别性探针 | 真实云写入 |
| Provider（4 云） | **B** | 主代理（逐 API 调用点验证） | 8 生产 | `Provider 接口 → 四实现 → SDK 调用` | **确认零全量覆盖 API**；CVM 配额对照官方文档 | 真实云 API |
| 通知/健康（`notifier/`,`internal/health/`） | C | 主代理（推翻 1 项误报） | 7 生产 | `Publish → OnEvent → SMTP/HTTP`；三消费者共用 Checker | `-race` 绿；stdlib `handler.go` 源码核对 | 真实 SMTP/Webhook/Uptime Kuma |
| HTTP/API/SSE（`webui/`,`webui/api/`） | D | 主代理（推翻 1 项误读） | 20 生产 | `Register(31 路由) → handler → Coordinator`（原写 28 属自始低估：`webui/api/deps.go` 有 31 条 `mux.HandleFunc`，加 `webui/server.go:296` 的 `GET /api/health` 共 32） | `webui`/`webui/api` `-race` 绿；路由表逐一核对 | 浏览器交互 |
| 前端（`webui/frontend/src/`） | E | 主代理（复核 5 项 P2） | 18 源 | `view → api.ts → Go handler` 逐字段 | `npm run build`/`vue-tsc` 经 lead 门禁通过 | 浏览器人工 |
| 全局/部署/测试 | F | 主代理 | 6 构建文件 + 62 测试 | Makefile/Dockerfile/CI 三套命令对比 | 全部门禁实跑 | 远端 CI/GHCR |

**文件覆盖（审计快照口径）**：53/53 生产 Go 文件（root 2、app 1、config 7、dns 2、internal/health 3、internal/portconv 1、internal/tag 1、notifier 4、provider 8、syncer 4、webui 2、webui/api 18）**全部读过并归属至少一个主审**；高风险文件均有独立交叉复核。**当前为 55 个生产 Go 文件**：provider 8→9（新增 `provider/plan.go`）、syncer 4→5（新增 `syncer/target.go`），其余不变。

**流程偏差如实记录**：`e38de1fb`（同步/DNS/Provider）与 `00a69dd7`（全局死代码/部署/测试）两路分派**超出合理时限未按时返回**；我已发出限时收敛请求，并**亲自完成**这两个范围的审核与验证（P0-01/P1-01/P2-01 由我自写探针复现）。因此覆盖无缺口，但这两路的"第二双眼睛"来自主代理而非独立子代理——建议后续针对 `syncer/syncer.go`（该快照 971 行，现 952 行）与 `webui/api/bundle_v3.go`（887 行）另做一次独立复核。

---

## 12. 命令与证据

### 历史审计与既有实施门禁（**历史执行记录**；无原始日志入库，本轮未重跑，不构成当前通过证据）

| 命令 | 结果 |
|---|---|
| `go build ./...` | PASS（EXIT=0） |
| `go vet ./...` | PASS（EXIT=0） |
| `go test ./... -race -count=1` | **PASS — 12/12 包 ok**（root 15.9s、app 2.3s、config 8.2s、dns 2.0s、health 4.8s、portconv 2.8s、tag 2.5s、notifier 4.4s、provider 5.0s、syncer 43.6s、webui 6.1s、webui/api 16.1s） |
| `gofmt -l .`（排除 node_modules） | PASS（无输出） |
| `git diff --check` | PASS（EXIT=0） |
| `go mod verify` | PASS `all modules verified` |
| `go mod tidy -diff` | PASS（无差异） |
| `npm audit --omit=dev --audit-level=high` | PASS `found 0 vulnerabilities` |
| `npm audit --audit-level=high` | PASS `found 0 vulnerabilities` |
| `docker compose -f docker-compose.yml.example config --quiet` | PASS（EXIT=0） |
| `docker build -f build/Dockerfile -t fwalizer:audit .` | PASS |
| 容器 `User` / `id` | PASS `appuser` / `uid=1000(appuser)` |
| 容器 `wget` | PASS `/usr/bin/wget`（BusyBox） |
| `HEALTHCHECK` 目标 | PASS `wget … /api/health`，30s/3s/10s/3 |
| 容器健康状态 | PASS `healthy` |
| `GET /api/health` | PASS `{"status":"ok"}` |
| `GET /api/health/operational` | PASS `{"status":"ok","reasons":[]}`（200） |
| 容器内嵌 SPA `/` | PASS 返回 index.html |
| `docker stop` + 退出码 | PASS 0.125s / `ExitCode=0` / `OOMKilled=false` |

### 判别性验证（为证实/证伪具体结论而运行）

| 实验 | 结果 |
|---|---|
| **overlay 探针：Aliyun 端口 key 往返** | **P0-01 复现**：`ToAdd=1 ToDelete=1`（期望 0/0） |
| **overlay 探针：空 comment desc 碰撞** | **P1-01 复现**：B 域名把 A 的 `1.1.1.1/32` 列入 `ToDelete` |
| **overlay 探针：IPv6+ICMP key** | **P2-01 复现**：Lighthouse `ICMPv6` 与 CVM `ICMPV6` 均 1/1 |
| **持久卷 + 预置陈旧 pidfile `1` 启动容器** | **历史 P1-02 复现**：`exited exit=1`，日志「FWAlizer 已在运行 (PID: 1)」。**更正**：首次用 `--volumes-from` 未能复现（卷未复用），后改持久命名卷稳定复现；I-01 修复后已用持久命名卷验证不再阻止启动，见 P1-02 |
| `git archive HEAD` 抽取清洁副本 → `go build/vet/test ./...`、`make vet` | **EXIT=1/1/1/2** `pattern frontend/dist: no matching files found`（构建阻断） |
| 同上 + 手动 `mkdir webui/frontend/dist` → `go build ./...` | EXIT=0（定位根因） |
| 顺序启动两个实例（同一 `FWALIZER_DATA_DIR`） | 历史结果：第二个被拒并提示 PID；只能证明顺序场景，不证明并发安全 |
| **并发**启动两个实例 | 历史一次实验为一个退出、一个运行，**未复现** TOCTOU 双实例；不能据此宣称当前实现并发安全，I-01 必须用“测试先持有内核锁”的判别性控制补强 |
| 遍历 provider 全部 SDK 调用点 vs 禁用 API 名 | 仅 Create/Delete/Authorize/Revoke/Describe，**零** `ModifyFirewallRules`/`ModifySecurityGroupPolicy`/reset |
| Go stdlib `log/slog/handler.go`：`TextHandler.Handle` 是否按 level 过滤 | **不过滤**（过滤只在 `commonHandler.enabled`）→ 驳回相应误报 |
| `modernc.org/sqlite@v1.54.0/tx.go:23` | `if !opts.ReadOnly && c.beginMode != ""` → 证实 `_txlock` 注释理由不成立、mitigation 安全（P3-09） |
| `查询用户安全组配额.md:62` / `安全组添加规则.md:18` | **历史依据的局限**：泛称 `SecurityGroupPolicyLimit: 100` 未定义双向合计；“一次请求单个方向”为请求形态，不能证明配额方向。2026-10-02 已用下列官方 CVM 正文更正 P3-06 |
| [CVM 安全组规则问题](https://cloud.tencent.com/document/product/213/43699) / [使用限制总览](https://cloud.tencent.com/document/product/213/15379) | 2026-10-02 研究与本轮直接读取均确认默认入站、出站各 100 条；可工单提额，本工具仍固定入站 100 的本地保护 |
| [查询安全组规则示例](https://cloud.tencent.com/document/product/215/15804) | 模板条目非空而地址族统计为零，支持以数组条目数作本地计数下界；不代表真实模板统计已云端验证 |
| `grep 'delete(' dns/circuitbreaker.go` | **历史审计零命中** → 当时 P3-01 永不淘汰；现已使用 delete 与配置域名过滤复制 |
| `grep io.ReadAll/LimitReader/Body.Close/NewTicker/go func` | 各 1/2/2/4/6 处，逐处核对均闭合 |
| fd 探针（5000 次注册，`/dev/fd` 计数） | `fd_before=5 fd_after=5` → 无 fd 增长 |
| 前端 8 字段载荷 vs Go DTO 逐字段比对 | **一致**（`webui/api/test_email.go:24-33` ↔ `Alerts.vue:66-75`） |
| `grep NModal` 各 view + 删除按钮绑定 | 历史审计快照中 Targets/Rules 删除**直接绑定 DELETE** → 证实原 P2-07；现已改为仅打开确认，当前证据见实施补记 |

---

## 13. 证据边界与未验证项（**不得外推为通过**）

下表同时包含“未执行”“历史证据不可外推”“已执行但范围有限”和“不适用”。每行状态必须按原文理解；只有明确写为当前 revision、当前环境已完成的证据，才能支持相应范围内的结论。

| 边界 | 状态 |
|---|---|
| **真实腾讯云 / 阿里云 API** | **未执行**。P0-01 与 P2-01 的 key 不对称在 **provider 层与"云端回传形态无关地"**被证明；当前实现的真实云端回传、四平台删除安全、CVM 真实零规则响应形态、模板统计与提额情况、ECS 是否真的拒绝 >100 个 RuleId、SWAS Remark 50 上限均**未实测** |
| **官方文档直取（P3-06 专项）** | **2026-10-02 已取得 CVM 官方正文**：默认入站/出站各 100，2026-09-30 仅凭泛称 API 字段推导双向合计的结论已更正。**保留历史执行事实**：2026-09-30 当时叙述页因环境 DNS 不可达未能直取，只取得 API 字段定义；该事实不再支持当前“每方向 100 无官方依据”的判断。真实账号仍待 PT-I7-03，未调用云 API |
| **真实 SMTP 服务器接受** | **未执行**。仅使用假 SMTP |
| **真实收件箱投递**（含中文主题的 MTA 编码表现，P3-20） | **未执行**。用户已于 2026-09-27 决定跳过并自行处理 |
| **真实 Webhook**（P2-05 已有本地 mock/响应测试证据，**非真实渠道投递**） | **未执行**。用户已决定跳过；`93e0e4b` 不替代人工接收验收 |
| **真实 Uptime Kuma HTTP Monitor** | **未执行** |
| **真实 Uptime Kuma Push DOWN/恢复通知**（含 P2-04 的实际触发概率） | **未执行**。P2-04 已取得本地判别性回归与 mock 首发证据，P3-03 已取得真实 timer、本地 mock 与多轮 race 证据；均不代表真实外部通知通过 |
| **浏览器人工检查**（PT-B7-07、PT-I7-06；布局、按钮尺寸、Dry Run 卡片、重复 key 的实际 patch 行为、侧边栏高亮、SSE 重连表现） | **未执行**。"本环境无浏览器工具"是**当时批次的历史环境陈述**，只解释该批次为何未做，**不约束后续轮次的工具能力**，也不得据此推断浏览器验收已完成；前端结论均为源码级 + 构建/框架源码取证 |
| **Docker 容器运行** | 历史已执行（healthy / uid 1000 / ExitCode 0 / SIGKILL 后重启曾复现 P1-02）；I-01 已在 Linux/amd64 容器执行残留 PID、共享卷互斥、SIGKILL 后替换重启与正常 stop；真实负载下“完成当前轮次再退出”仍未验证 |
| **远端 CI / GHCR** | **未执行**。既有 `v2.0.0`（run `36300428681`）结果属**更早 revision，不能证明当前改动** |
| 真实 DNS 上游 | **未执行**。`dns/resolver_test.go` 两用例由 `testing.Short()` 跳过（`:24-26`、`:37-39`）；只有 `TestResolve_PublicDomain` 在解析出错时 `t.Skipf`（`:42-44`），`TestResolve_NonExistent` 在网络不可用时并不跳过（亦无失败断言） |
| SQLite 真实 BUSY/慢盘争用下的 2s 探活上限端到端保证 | **未执行**（仅核对驱动有 `interruptOnDone`、`Store` 无互斥量） |
| 真实旧库迁移路径 / 生产 SQLite | **未执行** |
| `GOOS=windows` 运行验收 | **不适用**（用户已决定移除支持，仅以构建失败为证据） |
| 后端 `syncer/syncer.go` 与 `webui/api/bundle_v3.go` 的**第二个独立子代理**复核 | **未完成**（原分派超时，由主代理亲自覆盖替代） |

**结论**：`go test` 通过只证明对应测试覆盖的行为，**不得**外推为真实云、真实 SMTP、真实 Webhook、真实 Uptime Kuma、浏览器或远端 CI 已通过。

---

## 14. 审计快照的 Git 完整性（历史记录与当前复核）

> 本节前四列保留原始审计快照，不应被误读为当前 HEAD。**当前基线见头部「最近核验基线（2026-09-30 第二轮）」小节**：`main` / `b84531b10a9dbbf1321b4eb131f8391a6eea0c4a`，`origin/main` = `d6d208e`（**ahead 7**），工作树干净。下述"当前复核（2026-09-30）"行是**该批次**记录，其"`main == origin/main`"当时为真、现已过期。

| 项 | 审核前 | 审核后 |
|---|---|---|
| `git rev-parse HEAD` | `11918fb945fe9dfe2a86ead5bc833b14dd156a68` | **同一值（历史快照）** |
| `git status --short --branch` | `## main...origin/main`（干净） | 干净；**历史快照中本报告文件曾是新增未跟踪文件** |
| tracked 文件改动 | 0 | **0** |
| `git diff --stat` / `--cached --stat` | 空 | **空** |
| ignored 产物 | `fwalizer`、`webui/frontend/dist/`、`webui/frontend/node_modules/` | **完全相同的 3 项**（均系审核前已存在，未新增） |
| 审核产生的临时产物 | — | **全部清理**：overlay 探针目录、清洁检出副本、pid/fd 探针目录、`fwalizer:audit` 镜像、测试容器与命名卷均已删除 |

**历史说明（保留）：** 原审计完成时 `fwalizer-audit-final1.md` 是用户要求写入根目录的新增未跟踪文件，`.gitignore` 未匹配它；后续文档提交已将本报告纳入版本控制，因此该说明不代表当前状态，也不应据此修改 `.gitignore`。

| 历史复核批次（2026-09-30） | `d6d208edd86187e1795318ed555e8e96c5219df9` | **该批次**当时 `main == origin/main`、复核开始时工作树干净；该批次只更新本文与 `Issue7.md`，未修改代码，未运行构建、测试或格式化。**其后已有 7 个提交**（含 `7aaa3f2`/`7acf303`/`c5cc79d`/`93e0e4b` 改代码与测试、`b84531b` 改文档），故不代表当前状态 |

**I-01 后续实施（2026-09-30）：** 基线 `fd298ef`，开始时工作树干净、main ahead 1；该批次修改 6 个文件（含新增 `config/pidfile_test.go`），**已提交为 `7aaa3f2`**、未推送；未修改 Issue7、业务逻辑或依赖，临时探针/容器/卷已清理。具体门禁见 P1-02。

**历史审计确认：原始代码审核未修改、未创建、未删除任何受版本控制的仓库文件，未执行任何 state-changing git 命令，未触碰真实云/SMTP/Webhook/Uptime Kuma，未升级依赖，未安装任何全局工具。** 本次复核只更新两份文档；未修改代码、未重跑历史探针或门禁，也未触碰任何外部系统。

---

## 附录 A：本报告相对上一版的差异

| 变更 | 内容 |
|---|---|
| **分级更正** | 由"P0=0 / P1=2 / P2=6 / P3=33"更正为 **P0=1 / P1=2 / P2=9 / P3=25** |
| 新增 P0-01 | 阿里云端口比较 key 不对称（已探针复现） |
| 新增 P1-01 | 空 comment 导致 desc 碰撞互删（已探针复现） |
| 新增 P2 项 | IPv6+ICMP key、ECS 删除不分批、静默丢弃云端能力限制、Dry Run key 重复、仪表盘 idle 误报（后两项由上一版的泛 P2 归位） |
| 新增 P3 项 | `IsOpen` 无控制流效果、DryRun 过度限速、ticker Reset 语义、通知合并、ECS 分页守卫、邮件报文细节 |
| 剔除误报 | `TextHandler` 二次过滤、发布顺序"与文档相反"、4 项误删候选、SPA fallback |
| 更正表述 | P0-01 影响由"每轮全量替换/净规则丢失"更正为"同一条规则被删+建，**非净丢失**；净丢失需与 IP 变更叠加" |
| 方法学更正 | P1-02 首次测试（`--volumes-from`）未能复现，改用持久命名卷后确认；已在正文如实记录 |
| 纳入决策 | 用户 7 项决策写入第 9 节与相应 finding |
| P1-01 状态继续更新 | 2026-09-28 的“待决策/已冻结”已于 2026-09-29 被第 8 节最终方案取代；Step 0～5 主体提交为 `28559ed`，F1/F5 补强提交为 `38bdc19`，R7-01～R7-04/R7-06/R7-07 已提交为既有修复链，R7-05 与文档回写已提交为 `d6d208e`；`maxTagRunes` 仍为 48，SWAS `Remark` 上限仍登记为 PT-B7-09 |
| 历史复核状态（2026-09-30） | 该批次基线为 `main == origin/main == d6d208e`、复核开始时工作树干净；R7-01～R7-07 的提交祖先关系、生产符号与判别性测试已静态复核保留，该批次未重跑门禁；P3-11/P3-22 已修复；P3-25 拆分为同步路径已修复、资源扫描路径未修复；外部/人工验收登记为 PT-B7 9 项 + PT-I7 7 项。**当前基线为 `main` / `b84531b`、`origin/main` / `d6d208e`、ahead 7**（见头部「最近核验基线」小节）；第二轮核验另新增 P3-26（资源扫描分页中途空响应）并订正 P3-06 结论 |

| 第二轮核验与更正（2026-09-30） | 只读真实性核验后按用户批准方案回写：新增「最近核验基线」小节；当时订正 P3-06（撤销"每方向 100"与"只统计入站"；此历史判断后已由 2026-10-02 官方正文与 B 实施替代）、P3-07（死代码集合扩为 `retrySync`+`retrySyncDetailed`+`truncateDesc`，生产链改指 `syncTarget`）、P3-16（`RunTest.vue` 44px 已修复）、P3-17（10 处标签，`export_test.go:461` 已修复）、P3-21（Webhook drain 部分已修复 / Push 无字节上限未修复）、P2-08 与 P2-09 补状态横幅、§4 Dashboard 行改标已修复、§6 pidfile 单测改标已补齐、§3 的 `io.ReadAll` 计数与 `IdleConnTimeout` 证据边界、§11 路由数 28→31、`AGENTS.md` 行号引用订正；新增 **P3-26** 独立 finding；另新增两个判别性测试（见 §6 与 §3 对应行）。所有"通过"仍属历史执行记录，本轮门禁结果见文末回写 |


## 附录 B：规则身份专题（已移至正文第 8 节）

> 本附录的内容已**整体移入第 8 节「规则身份与 TAG 所有权」**，以避免同一议题在文档内出现两处。
>
> **当前摘要**：P1-01 已探针复现，并已定案将“个体识别”从 description/comment 上移除，改为目标级完整期望集与 canonical functional key；TAG 仍为唯一操作授权，`maxTagRunes` 仍为 48。实施状态见 Issue7。
>
> 完整内容请看 **第 8 节**：8.0 为最终方案；8.1～8.7 保留定案前的机理、候选与不确定性历史；8.8 记录当前决策状态。
