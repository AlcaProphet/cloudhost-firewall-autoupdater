# FWAlizer 全量代码审核报告（最终版）

| 项 | 内容 |
|---|---|
| 仓库 | `/Users/kylechen/Desktop/Repo/cloudhost-firewall-autoupdater` |
| 审计快照分支 / HEAD | `main` / `11918fb945fe9dfe2a86ead5bc833b14dd156a68` |
| 审计快照开始前工作树 | **干净**（0 tracked 改动、0 非忽略未跟踪文件）；这是历史审计快照，不表示当前工作树状态 |
| 当前复核基线（2026-09-30） | `main` / `34aa9b859c38904e701a19d672dcc4f65435b31c`；`main == origin/main`；复核开始时工作树干净（已有 `fwalizer`、`webui/frontend/dist/`、`webui/frontend/node_modules/` 仅为 ignored 产物） |
| 审核性质 | 代码审核为**只读**：未修改、未创建、未删除任何**受版本控制**的仓库文件 |
| 审核方式 | 8 路并行子代理分模块审核 + 主代理亲自覆盖超时范围 + 判别性探针独立复现 + 交叉复核裁决 |
| 审核范围 | 233 个 tracked 文件；**53/53 生产 Go 文件**（10,890 行）；62 个测试文件（20,457 行）；18 个前端源文件（2,212 行）；6 个构建/部署/CI 文件；8 份合同文档 |
| 报告版本 | final1（已剔除全部被驳回/误报项，并纳入用户 7 项决策） |

> **阅读规则（2026-09-29）**：本报告同时保留“审计快照事实”“后续状态补记”和“定案前历史分析”。当前强约束以 [AGENTS.md](./AGENTS.md) 为准；P1-01 当前设计与串行实施入口以 [Issue7.md](./Issue7.md) 为准；[Design5.md](./Design5.md) 记录当前设计方向。本报告用于保存审计证据与整理剩余问题，不建立第二套实施合同。源码行号属于审计快照，后续定位应同时使用 finding ID、符号名和测试名。

---

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

### 0.2 当前问题状态索引

| 范围 | 当前状态 | 当前入口 |
|---|---|---|
| P0-01 | ✅ 已由 `108e528` 修复；原证据与回归必须保留 | Issue7 Step 1 重构 functional key 时保持绿色回归 |
| P1-01 | 🟠 本地主体与 R7-01～R7-07 完整复核项均已实施（R7-01～R7-04/R7-06/R7-07 已提交，R7-05 当前工作树未提交）；真实云/浏览器/远端 CI 仍未执行 | [Issue7.md](./Issue7.md) §12.3 实施证据、§12.4 已提交补强、§12.5 R7 修复与本地门禁 |
| P1-02 | 🔵 真实存在；已决定采用 `flock`，当前仍未实施 | 独立高优先级队列 I-01；同时收口 P3-08；本轮仅更新审计记录 |
| P2-01 | ✅ 已实施（canonical family/协议归一化，IPv6 ICMP 与云端协议别名收敛） | Issue7 Step 1；保留原审计红灯作为正向控制 |
| P2-03 | ✅ 已实施（能力矩阵产出 `unsupported_*`，目标 `partial` 且冻结清理） | Issue7 Step 1；旧“仅 WARN/Dry Run、不计 skipped”决策仅作历史记录 |
| P2-02 | ✅ 已实施（ECS 删除每批 ≤100，150 → 100+50，部分成功如实计数） | Issue7 Step 3 |
| P2-08、P2-09 | ✅ 已实施（Dashboard 只消费后端 outcome；Dry Run 以 `target_id` 为 key） | Issue7 Step 4 |
| P2-04～P2-07 | 🔵 未修复，且不构成 P1-01 前置 | 独立问题队列 |
| P3-25（同步路径） | ✅ 已实施（重复/未推进 token 立即 `snapshot_incomplete`，本 attempt 零删除） | Issue7 Step 1 |
| P3-25（资源扫描路径） | 🔵 未修复（`provider/scan.go` 仍只判断空 token，未判断重复/未推进 token） | 独立低风险问题；不得把同步路径修复外推到扫描路径 |
| P3-11、P3-22 与 Dry Run 相关 P3-16 | ✅ 已实施（目标级日志上抛写库错误；Dry Run 每目标一次快照；`RunTest.vue` 44px） | Issue7 Step 4 |
| 其余 P3 | 🔵/⏳ 未修复、文档清理或待真实验证 | 按 0.4 的独立队列处理 |

### 0.3 当前唯一串行主线：Issue7

- [x] **Step 0｜文档合同**：已完成；只代表定性与实施合同完成，不代表代码修复。
- [x] **Step 1｜纯规划器与失败先行用例**：已完成（严格 TAG 所有权、唯一 `FunctionalKey`、能力矩阵、目标级纯 planner、快照 revision、ECS token 保护）。
- [x] **Step 2｜目标级先增后验**：已完成（`S0 → Add(S0 版本) → S1 → 覆盖验证`；Add/验证失败时删除调用恒为 0）。
- [x] **Step 3｜四平台条件清理**：已完成（Lighthouse→CVM→SWAS→ECS 串行；安全门 + S1 定位 + S2 强制 + 残留计数）。
- [x] **Step 4｜Dry Run、事件、日志、Dashboard 与健康口径**：主体已实施；R7-02 已提交为 `eab4bea`，R7-03 已提交为 `297ccfe`，R7-04/R7-06/R7-07 已提交为 `b19d271`，R7-05 已按测试-only 边界在当前工作树修复。真实外部验收仍按 Issue7/ProdTestList 独立保留。
- [x] **Step 5｜完整门禁与真实云验收**：本地部分已完成（`go test -race`/vet/build/gofmt/前端/compose/docker build/真实二进制/容器验收与文档闭环）；独立核验补强后复跑门禁为 12/12 包 ok，但 `go test ./... -race -count=1` 存在一个与本项无关的既有 flaky 用例（`TestIsRetryable_RealWorldShapes`，见 Issue7 §12.4 F2），不得把单次绿色外推为稳定绿色；**真实云与浏览器仍待人工执行**（PT-I7-01～07）。

**主线停止条件**：必须逐 Step 实施和验收；任一 Step 未满足 [Issue7.md](./Issue7.md) 的停止条件时，不进入下一 Step。不得把 mock、单测、本地进程或 Docker 结果外推为真实云验收。

### 0.4 独立问题队列（不与 Issue7 主线编号混用）

以下顺序是整理后的 backlog，不表示已授权实施；每次仍应只处理一个问题并保留判别性测试。

- [ ] **I-01｜P1-02 + P3-08**：以 `flock` 替换 PID 判活，覆盖残留 pidfile、PID 复用与并发启动；当前方案已定但源码、测试、README 与当前门禁均未更新，不得标记完成。
- [ ] **I-02｜P2-04 + P3-03**：先发布 `RuntimeState`，再唤醒 Health/Push；同时收口首次 Push 失败后的重试/唤醒语义。AGENTS 目标顺序已在 Step 0 修订，当前剩余是代码与测试。
- [ ] **I-03｜P2-06**：告警页增加 loaded 守卫，防止加载失败后用默认值覆盖真实敏感配置。
- [ ] **I-04｜P2-07**：目标与规则删除增加卡片式二次确认，满足 AGENTS 强要求。
- [ ] **I-05｜P2-05**：Webhook 按渠道解析业务错误码；真实 Webhook 仍需单独验收。
- [ ] **I-06｜P3-23、P3-24**：串行修复 ticker Reset 与 pause/resume 通知合并，不和 Issue7 状态机重构混做。
- [ ] **I-07｜P3-12、P3-05、P3-14、P3-10、P3-13**：文件权限与 HTTP/error 一致性；P3-11 已由 Issue7 Step 4 修复，不再列入此队列。
- [ ] **I-08｜P3-01、P3-21、P3-15**：资源与连接健壮性；P3-02 保留为已知弱语义，不在 Issue7 中扩张为隔离/降频重构。
- [ ] **I-09｜P3-07**：Issue7 Step 1～4 完成后重新证明生产零引用，再清理旧同步包装与关联测试。
- [ ] **I-10｜其余独立 P3**：P3-04、P3-06、P3-09、P3-16 非 Dry Run 子项、P3-19、P3-20，按各 finding 的前置与真实环境边界逐项处理。
- [ ] **I-11｜P3-17、P3-18**：仅做文档/注释闭环；不得与业务语义修改混在同一批次。
- [x] **I-12｜Issue7 R7-01（P1）**：已修复并提交为 `b80b1b0`；可重试清理失败耗尽后保持 S1 已覆盖的 success + cleanup_deferred 强语义与每 attempt 重读快照。
- [x] **I-13｜Issue7 R7-02（P2）**：已修复并提交为 `eab4bea`；补全目标事件 `cleanup_deleted`/`duration_ms`/canonical `unsupported`，并以真实 publisher/EventBus/SQLite 整链证明清理 `2/1/1` 落库一致。
- [x] **I-14｜Issue7 R7-03（P2）**：已按用户裁决 A 修复并提交为 `297ccfe`；Dry Run 覆盖所有已配置目标，无适用规则目标只返回未调度空骨架且零 DNS/云 API/planner/限速，正式同步统计口径不变。
- [ ] **I-15｜Issue7 R7-04（P3）**：🔵 **问题真实存在，当前未修复；本轮仅完成审计记录更新，未修改源码、测试或运行门禁。** 这是清理计数与可观测性缺陷，不改变删除安全门、目标 `outcome` 或 OperationalHealth，但会使目标事件、`RoundSummary`、Dashboard 与 `sync_logs` 一致地记录错误的 `cleanup_deferred`。

  **当前证据与缺陷链：** 当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；本轮研究确认工作树原有修改仅涉及本报告，源码与测试没有本轮改动，也未运行构建、测试或格式化。`syncer/target.go` 先以 S1 的 `plan1.CleanupCandidates` 记录候选，再由 `len(plan1.CleanupCandidates) - cleanupResolved` 间接计算 deferred；`runTargetCleanup` 返回的 `resolved` 来自 `DeleteResult.Resolved`，而错误型幂等 NotFound 可得到零值 `DeleteResult{Deleted: 0, Resolved: 0}`。该分支虽会执行 S2，但 `verifyCleanupResult` 当前只检查 `plan2.ToAdd`，丢弃 `plan2.CleanupCandidates`，调用方仍按旧 S1 候选减 `Resolved` 计算。因此“1 条候选被并发者先删除、Delete 返回 NotFound、S2 已无候选”会错误记录 `cleanup_candidates=1 / cleanup_deleted=0 / cleanup_deferred=1`，合同要求为 `1 / 0 / 0`。现有正常删除、普通删除失败、R7-01 重试耗尽、部分删除及 S2 覆盖失败控制已存在，但缺少当前目标级 NotFound→S2 最终候选计数的判别性覆盖。

  **固定语义与最小修复设计：** `cleanup_candidates` 保留 S1 planner 看到的候选数；`cleanup_deleted` 只计云端明确确认的实际删除数，NotFound 不计入；成功取得并通过验证的 S2 后，`cleanup_deferred` 直接取 `len(plan2.CleanupCandidates)`，包括仍存在但不可删除的 Owned 候选；未执行或未成功取得可信 S2 时，使用 S1 与 Provider 已确认进度的 fallback，不把不完整 planner 结果当最终状态。建议删除私有 `cleanupResolved` 间接推导链，使 `runTargetCleanup` 返回明确的 `deleted` 与 `deferred`（例如私有 `cleanupResult`），并让 `verifyCleanupResult` 返回 S2 最终候选数；`runTargetAttempt` 直接消费该结果，同时保持 NotFound 的 `deleted/cleanup_deleted=0`、S2 Describe/完整性/覆盖验证失败为 `failed`。正常 Delete 成功、PartialDeleteError 或幂等 NotFound 均须按既有安全语义执行必要 S2；不得跳过 S2。

  **受影响文件与符号：** 生产主线集中在 `syncer/target.go` 的 `targetResult`、`syncTarget`、`runTargetAttempt`、`runTargetCleanup`、`verifyCleanupResult`；`syncer/syncer.go` 的整轮汇总和 `webui/api/logwriter.go` 的写库字段读取原则上无需改动，只应继续消费修正后的 `targetResult`。判别性测试涉及 `syncer/target_test.go` 的目标级夹具、`syncer/cleanup_test.go` 的清理场景，必要时扩展 `syncer/round_summary_test.go` 与 `webui/api/logwriter_test.go` 证明事件、整轮汇总和 SQLite 生产链传播一致。不得借本项修改 `provider.DeleteResult` 语义、四平台 Provider 删除实现、`isIdempotentDelete`、`provider.PlanTarget`、OperationalHealth、API/数据库 Schema，或 R7-06/R7-07 的测试夹具与生产超时参数。

  **判别性验收：** 修复前以下当前目标级用例应能暴露缺陷，修复后必须通过：① 单候选 Delete 返回错误型 NotFound、S2 无候选，断言 `1/0/0`、`success`、S2 已执行；② 两候选 NotFound 后 S2 只剩一条，断言 `2/0/1`，证明不是把 NotFound 全部清零；③ 两候选均仍存在，断言 `2/0/2`；④ NotFound 后 S2 Describe 失败或覆盖验证失败，断言 `failed`、发布同步错误，且不把不完整 S2 当最终计数，按既定 fallback 保留未证实残留；⑤ 正常删除、普通删除失败、可重试删除耗尽及 ECS 部分删除的既有正向控制继续成立。还应至少补一条生产链断言，证明目标事件、`EventSyncComplete`/`RoundSummary` 与 `sync_logs` 对 NotFound 后 S2 收敛均记录 `cleanup_candidates=1`、`cleanup_deleted=0`、`cleanup_deferred=0`。本地验收建议先执行 `go test ./syncer ./webui/api -race -count=1`，再按 Issue7 §12.5.4 顺序进行全量 race、vet、build、前端与 diff-check；这些命令本轮未执行，不能预先写成通过。

  **风险、外部依赖与边界：** 主要风险是把“已确认不再存在”误计为实际删除，或把 S2 的原因条目数/可删除数误当残留规则数；必须分别保持 `Deleted`、`Resolved` 与最终 `CleanupCandidates` 的语义。S2 可能发现 S1 之后新出现的 Owned 残留，因此按当前合同允许 `cleanup_deferred` 大于 `cleanup_candidates`，不新增上限约束。该缺陷可由本地 Syncer/Provider 夹具、事件/汇总与 SQLite 生产链完全证明，不依赖真实云账号；真实腾讯云/阿里云的 NotFound 错误形态、四平台分类、并发删除后的 S2 回读，以及 Lighthouse/CVM 版本保护仍是独立的 PT-I7 外部验收，当前均未执行，本地通过不得外推为真实云通过。

  **用户决策记录：** 当前 AGENTS.md、Issue7 §5.4、§5.5、§12.5.1 对本项没有直接冲突，不需要用户裁决。唯一边界选项是：A（推荐）按现合同允许 S2 发现的新 Owned 残留使 `cleanup_deferred > cleanup_candidates`，因为两字段分别来自 S1 与成功 S2；B 新增 `deferred <= candidates` 约束，但会掩盖 S2 的新残留并改变既定字段语义。推荐 A，影响是实现与验收必须断言“最终 S2 候选数”而不能用 S1 候选数作上限；若用户要求 B，应在实施前停止并重新修订 Issue7 §12.5.1、计数合同和测试矩阵。

  **停止条件：** 未完成上述判别性测试和定向门禁前，保持 I-15 未修复；不得把一次绿色运行写成稳定绿色。若修复通过修改 Provider 的 `DeleteResult.Resolved` 定义、扩大 `isIdempotentDelete`、跳过 S2、把 NotFound 计入 `cleanup_deleted`、把 S2 不完整结果当最终计数、改变 `success + cleanup_deferred`/OperationalHealth 语义，或牵连 R7-06/R7-07、真实云验收范围，应立即停止并重新审查合同。
- [x] **I-16｜Issue7 R7-05（P3）**：✅ **已于 2026-09-30 按测试-only 边界本地修复，尚未提交。** 失效的数组非 `null` 字符串门禁已替换为结构化 JSON 合同检查；实际 Dry Run 输出未发现生产 `null`，故未修改生产 DTO、planner、API 或前端。

  **修复前证据：** 基线 `syncer/dryrun_test.go:209-233` 的 `TestDryRun_ArraysNeverNull` 字段列表已经包含引号（如 `"results"`），但断言再次拼接 `"`，实际搜索 `""results"":null`，无法匹配合法 JSON 的 `"results":null`。因此即使响应退化为 `{"results":null}`，旧测试也会通过；它同时不检查字段存在、JSON 类型或目标结果内部数组。

  **生产侧正向证据与准确结论：** 当前源码有充分的非空初始化：`syncer/syncer.go:602-617` 的 `emptyDryRunResult` 初始化目标级全部数组，`:630-631` 初始化 `DryRunResponse.Results`/`Warnings`，`:646-653` 对无适用规则目标返回该骨架；`syncer/target.go:455-467` 的 `ruleHosts` 使用 `make`；`provider/plan.go:434-447` 初始化规划结果数组。修复前这些静态初始化与内存级 nil 检查不能证明最终 JSON 形状；本轮新增结构化序列化测试后，三种真实输出的数组非 `null` 自动化证据已成立，且未发现生产缺陷。

  **实际修复：** 仅修改 `syncer/dryrun_test.go`。`requireJSONArray` 与 `validateDryRunArrayJSON` 使用 `map[string]json.RawMessage` 检查顶层 `results`、`warnings` 及每个目标的 `domains`、`desired`、`satisfied_by_owned`、`satisfied_by_external`、`to_add`、`cleanup_candidates`、`cleanup_deferred`、`dns_errors`、`unsupported`、`conflicts`：字段必须存在、非 `null` 且为 JSON array。正向覆盖有适用规则目标、R7-03 无适用规则骨架、零目标/零结果；负向对两个顶层字段和十个目标数组字段逐一注入 `null`、缺失、对象类型，并拒绝 `null` 目标项。结构化测试未发现生产响应为 `null`。

  **受影响文件与明确排除：** 实际只修改 `syncer/dryrun_test.go` 的 imports、`TestDryRun_ArraysNeverNull`、测试 helper、成功形状与负向控制。未修改 `syncer/syncer.go`、`provider/plan.go`、WebUI API、前端 Dry Run 页面、R7-04 清理计数逻辑、R7-06/R7-07 测试夹具或生产超时参数。

  **判别性验收结果：** 三种真实 `DryRun()` JSON 成功形状与逐字段负向控制均通过；`go test ./syncer -race -count=1` 通过。R7-06/R7-07 的两个 GOGC 压力门禁通过；`go test ./syncer -race -count=20 -timeout=20m` 以 892.213s 通过（首次未加 `-timeout` 时因 Go 默认 10m 总超时中止，不记为通过），`go test ./provider -race -count=20 -timeout=20m` 以 42.256s 通过；全量 12 包 race 连续 3 次、vet、build、前端 build 与 diff-check 均通过。

  **风险、外部依赖与范围边界：** 主要风险是继续把无判别力的测试当作数组序列化合同，导致未来 `null`、缺失或错误类型回归无法被门禁发现；反向样本必须保留，且必须使用 `json.RawMessage` 保留结构差异。R7-05 本身可由本地源码与 JSON 测试完全证明，不依赖真实腾讯云/阿里云、浏览器、SMTP、Webhook、Uptime Kuma 或远端 CI/GHCR；但这不改变 P1-01 的整体边界，PT-I7-01～07 及真实外部验收仍未执行，本项本地证据不得外推为真实云或浏览器通过。

  **用户决策记录：** 用户授权按推荐选项 A 实施：仅修复 `syncer/dryrun_test.go`，使用 `json.RawMessage` 做两层结构化数组检查并加入真实 `null`/缺失/非数组负向控制，保持生产 DTO 与序列化逻辑不变。实际测试未发现生产 `null`，因此未进入选项 B 的生产修复边界。

  **停止条件核对：** 已完成结构化成功形状、逐字段负向控制、定向 race 与多轮稳定性门禁；未修改生产 DTO、planner、API、前端、序列化逻辑或其他 R7 项，也未把测试修复写成生产缺陷修复。I-16 可按本地测试证据关闭，但外部验收边界不变。
- [ ] **I-17｜Issue7 R7-06（门禁可靠性）**：🔵 **当前仍未修复，问题真实存在于测试夹具而非生产重试逻辑。** 精确受影响文件/符号为 `syncer/retry_test.go` 的 `realHTTPTimeoutError`，及其调用者 `TestIsRetryable_RealWorldShapes`、`TestRetrySync_RealTimeoutTriggersSecondFullAttempt`、`TestRetrySync_ExhaustsThreeAttempts`：`Accept()` 成功返回的 `net.Conn` 被直接丢弃，失去强引用后可能由 Go 的 `netFD` finalizer 提前关闭，客户端于是得到 `connection reset by peer`/EOF，而不是等待真实 `http.Client.Timeout` 的 awaiting-headers 超时。最小修复仅稳定本地测试服务器生命周期：保存全部 accepted connections，cleanup 依次关闭 listener、等待 Accept goroutine 退出，再逐个关闭并清空保存的连接；必要时使用 mutex + `acceptDone`/`WaitGroup` 保证并发访问与回收有界。判别性门禁为 `GOGC=1 go test ./syncer -run '^TestIsRetryable_RealWorldShapes$' -count=50`，并保留真实 `*url.Error`、`Client.Timeout exceeded while awaiting headers` 与 `isRetryable=true` 断言；随后再做 `syncer` 定向 race/稳定性门禁。验收仅覆盖本地测试夹具、真实标准库 HTTP 超时形状与生产重试分类的门禁可靠性；无真实云、浏览器、SMTP/Webhook/Uptime Kuma 或远端 CI/GHCR 外部依赖，也不得以这些层次替代本项本地证据。停止条件：若修改生产 `isRetryable`、把 `connection reset by peer` 加入可重试集合、降低/放宽真实超时断言、修改生产 10s/30s 超时，或以单次绿色宣称稳定通过，立即停止；本项修复完成后仍须保持 I-17 未与 R7-07 混成单一证据。
- [ ] **I-18｜Issue7 R7-07（门禁可靠性）**：🔵 **当前仍未修复，问题真实存在于测试夹具而非生产阿里云超时配置或调用链。** 精确受影响文件/符号为 `provider/ali_timeout_test.go` 的 `aliBlockingServer`，以及其驱动的 `TestAliClientRequestIsBounded`：`Accept()` 成功返回的 `net.Conn` 当前被直接丢弃，失去强引用后可能由 Go 的 `netFD` finalizer 在毫秒级提前关闭，四条构造路径因此可能得到 EOF/`connection reset by peer`，而不是稳定阻塞到应用层 timeout。研究与修复范围仅限本地 TCP 测试夹具：保存全部 accepted `net.Conn`，cleanup 按“关闭 listener → 等待 Accept goroutine 退出 → 逐一关闭并清空已保存连接”的顺序回收；必要时用 mutex + `acceptDone`/`WaitGroup` 保证并发访问与回收有界。

  **2026-09-30 实施补记（优先于 I-15/I-17/I-18 的上述研究快照）：** I-17/R7-06、I-18/R7-07 与 I-15/R7-04 已提交为 `b19d271`：accepted connections 均持有至 cleanup 并有界回收；私有 `cleanupResult` 直接传递实际删除与最终残留，可信 S2 planner 成为 `cleanup_deferred` 唯一最终来源。I-16/R7-05 已在当前工作树按测试-only 边界修复，结构化数组合同与逐字段负向控制均成立，未修改生产代码。两个 GOGC 压力门禁、两包 `-race -count=20`、全量 race 连续 3 次、vet、build、前端 build、diff-check 均本地通过；R7-05 与文档改动尚未提交。真实云、浏览器与远端 CI/GHCR 仍未执行。

  必须保留四条阿里云构造路径、150ms 下限、`assertAliTimeout` 应用层 timeout 断言，以及生产 `newAliOpenAPIConfig` 的 10s `ConnectTimeout` / 30s `ReadTimeout` 正向默认值控制；不得修改生产超时、SDK、请求调用链或超时分类。验收仅为本地自动门禁，外部依赖为零；当前状态仍为待修复，完成前不得写成通过。风险是夹具继续把连接生命周期错误误报为已满足有界超时，导致门禁对 EOF/reset 假通过或间歇失败。停止条件：若修改生产超时/`isRetryable` 或其他生产分类逻辑、降低或放宽 150ms/应用层 timeout 断言、删减四条路径或以单次绿色宣称稳定通过，立即停止。本项与 R7-06（`syncer/retry_test.go` 的 `realHTTPTimeoutError`）同属 accepted connection 生命周期根因，但必须保持独立文件、符号、断言与证据，不能合并成单一结论。
- [ ] **I-19｜P3-25（资源扫描路径）**：🔵 **问题真实存在，当前仍未修复；已修复的只是同步路径，不能外推到资源扫描路径。** 当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`、`main == origin/main`；本轮只更新本报告，源码与测试未改动，未运行构建、测试或格式化。`provider/scan.go:172-214` 的 `scanAliECS` 以 `nextToken` 驱动 `DescribeSecurityGroups` 分页，只在 `body.NextToken == nil` 或空字符串时结束，非空 token 直接继续请求；当前没有 `seenTokens`、当前 token 未推进检测、页数上限或 `snapshot_incomplete` 失败路径。因此当第 1 页返回 `T`、第 2 页仍返回 `T` 时，会持续发送相同 token，形成无界扫描循环。

  **调用链与影响证据：** `POST /api/scan-resources` 经 `webui/api/deps.go:193-196` 注册、`webui/api/scan.go:48-54` 调用 `provider.ScanResources`，只有扫描正常返回后才在 `webui/api/scan.go:56-69` 调用 `ReplaceScannedResources`（`config/store.go:534-550`）。所以重复/未推进 token 场景下，请求可能长期不返回，扫描刷新不会完成，也不会执行缓存覆盖写入；已有旧 `scanned_resources` 缓存会继续保留，不会因该循环被半截结果覆盖。该问题影响资源扫描可用性与请求有界性，不是防火墙同步删除路径；当前也没有真实阿里云异常分页响应的验收证据，不能写成真实云已验证。

  **同步路径对照：** `provider/ali_ecs.go:63-118` 的 `AliECS.GetSnapshot` 已有 `seenTokens`、当前 token 未推进与历史 token 重复检测，异常返回 `ErrSnapshotIncomplete` 且不返回半截快照；Issue7 与本报告已将该同步修复和本资源扫描 finding 分开登记。`provider/scan_test.go` 当前仅覆盖未知云类型错误，`webui/api/scan_test.go` 仅覆盖缓存查询/删除等路径，现有测试没有 ECS 扫描分页 token 的判别性覆盖。

  **最小后续设计（未实施）：** 仅修改 `provider/scan.go` 的 `scanAliECS` 与 `provider/scan_test.go`；不改同步路径、HTTP handler、SQLite schema、前端或 SDK。按 opaque 字符串原样比较和传递分页 token：维护 `seenTokens`，在接受下一页 token 前拒绝它与本次请求 token 相同，或已在历史中出现；异常统一返回 `nil, error`，错误以既有 `ErrSnapshotIncomplete` 为可判别哨兵并补充“ECS 资源扫描”上下文，确保累计的部分资源不会向调用方或缓存写入。当前不建议新增页数硬上限：同步路径没有该合同限制，重复/未推进/环路保护已足以终止本 finding，硬上限可能误伤合法的大规模扫描。

  **判别性验收：** 修复后至少增加三类 provider 层用例：①两页正常推进，断言请求次数为 2、第二次携带 `T1`、返回两页完整资源；②第 1 页返回 `T`、第 2 页再次返回 `T`，断言最多请求 2 次、`errors.Is(err, ErrSnapshotIncomplete)` 为真且返回资源为空；③历史环路 `A → B → A`，断言最多请求 3 次后失败且不返回半截资源。重复 token 用例必须设置有界 mock 响应，不能靠超时证明修复。生产调用链的静态结论是扫描错误会在 `ReplaceScannedResources` 前短路，因此最低充分门禁是 provider 层“错误 + 零资源”证据；若未来确需 API 集成证据，应额外预置旧缓存并证明异常扫描返回 `success:false` 且旧缓存不变，不得为此新增生产注入接口。

  **相邻风险与范围边界：** `provider/scan.go:197-200` 对 `body == nil`、`SecurityGroups == nil` 或 `SecurityGroup == nil` 直接结束；若此前已累计资源，这可能把部分结果当作成功结果，但空安全组列表也可能是合法“无资源”响应，当前没有足够阿里云 API 合同证据决定其语义。本轮不把空响应处理并入 I-19，也不宣称“所有异常响应都不会写缓存”；如要扩大到该范围，必须先补正式 API 语义证据并重新确认合同。I-19 的外部边界仅为本地分页控制流、provider 判别性测试及必要的缓存短路证明；真实云通常无法主动制造重复 token，正常真实 ECS 多页扫描只能证明正向兼容，不能替代异常分支证据。远端 CI/GHCR、浏览器、SMTP、Webhook、Uptime Kuma 与四云防火墙写入/删除验收均不是本项直接证据，且当前仍未执行。

  **用户决策记录：** 当前 AGENTS.md、Issue7 §12.5 与现有源码证据没有冲突，不需要用户裁决。若后续范围需要选择：A（推荐）仅加入重复/未推进/历史环路保护并复用 `ErrSnapshotIncomplete`，保持 API、缓存 schema 和 SDK 不变，风险最小且与同步路径语义一致；B 同时把空响应/空列表纳入失败并扩充 API 集成测试，但必须先确认阿里云正式响应语义，影响是扩大生产行为与验收范围；C 增加固定页数上限，能提供额外硬上界但可能拒绝合法大规模扫描，当前不推荐。未收到新的范围授权前按 A 保持为后续设计，不视为已实施。

  **停止条件：** 在 `scanAliECS` 实际加入 token 保护、三类判别性用例通过并取得受影响包门禁前，保持 I-19 未修复；不得把同步路径的绿色测试、正常真实云扫描或单次本地绿色运行写成该 finding 已关闭或稳定通过。若修改同步路径、HTTP/API 契约、缓存 schema、前端、SDK，加入未经合同支持的页数上限，返回半截资源，混入空响应语义，或把真实云正常分页当作重复 token 异常分支证据，应立即停止并重新审查范围。

**I-15～I-19 串行状态：** I-17+I-18、I-15、I-16 已按顺序完成，统一压力/race/vet/build/前端/diff-check 门禁亦已完成；下一独立项为 I-19 的资源扫描分页。不同 finding 仍须逐项串行，R7-06/R7-07 仅因同根因在同一批次连续处理；本次 R7-05 授权不包含 I-19。

### 0.5 真实外部与人工验收

- [ ] **PT-B7-01～09（9 项）**：仍未执行；真实 SMTP、收件箱、Webhook、Uptime Kuma、浏览器、当前 revision 的远端 CI/GHCR、SWAS Remark 上限均不得写成已通过。PT-B7-02/03 沿用人工免除决定，但仍无真实通过结论。
- [ ] **PT-I7-01～07（7 项）**：Issue7 Step 1～4 主体与 R7-01～R7-07 本地核验项已实施，真实清单仍**未执行**；四云写入/删除安全、异常分页零删除、目标级 Dry Run 与浏览器回归均需真实或人工证据（PT-I7-07 为残留/收敛观察项）。本地通过不能替代真实验收。
- **外部/人工验收登记合计：16 项（PT-B7 9 + PT-I7 7），均不可表述为当前通过。**
- [ ] P3-06 的 CVM 配额方向、P3-19/P3-20 的真实 SMTP/MTA 表现继续保留为外部不确定性。

---

## 1. 审核结论摘要

| 项 | 结果 |
|---|---|
| **P0** | **1** |
| **P1** | **2** |
| **P2** | **9** |
| **P3** | **25** |
| **当前状态补记（2026-09-30）** | P0-01 已于 `108e528` 修复并加回归；R7-01～R7-07 本地核验项已完成（R7-05 尚未提交）；P3-11/P3-22 已修复；P3-25 仅同步路径已修复，资源扫描路径仍未修复。上述 P0/P1/P2/P3 数量仍是审计当时的发现统计，不等于当前未修复数 |
| 会实际破坏云端防火墙规则的问题 | **有，已实测复现**（P0-01、P1-01、P2-01） |
| 🟠 **P1-01 本地核验项已实施** | 2026-09-29 定为“目标级完整期望集 + TAG 所有权 + comment 纯可读 + 先增后验 + 平台化条件删除 + 可接受残留”；Step 0～5 主体提交 `28559ed`，F1/F5 补强提交 `38bdc19`，R7-01～R7-04/R7-06/R7-07 已提交，R7-05 已在当前工作树修复；**真实云/浏览器/远端 CI 仍未执行** |
| ⏳ **未执行的外部/人工验收** | **16 项登记边界：PT-B7-01～09（9 项）+ PT-I7-01～07（7 项）**；其中 PT-B7-02/03 为人工免除但仍无真实通过结论 |
| Goroutine / 连接 / 订阅泄漏 | **未发现** |
| 无界内存 | 仅 DNS 熔断器域名 map（P3-01；增长受"曾用域名数"约束） |
| 核心同步静默停止 | **未发现** |
| 明确凭据泄漏 | **未发现**（唯一残余是 P3-12 同机文件权限与 P3-19 的 SMTP 诊断文本回显） |
| 整体质量判断 | 架构与并发设计**优秀**（事务、快照、凭据、生命周期、SSE 五条主线干净，正向控制密度很高）；缺陷集中在**规则身份与端口比较层**（会造成持续删改云端规则）与**清理收尾** |

### 一句话结论

审计当时确认两条会每轮重复删改生产防火墙规则的路径：P0-01 已于 `108e528` 修复；P1-01 Step 1～5 主体已提交为 `28559ed`，F1/F5 补强已提交为 `38bdc19`，R7-01～R7-04/R7-06/R7-07 已提交，R7-05 已在当前工作树修复；本地核验项已收口，但真实云/浏览器/远端 CI 仍未执行，不能写成外部验收或发布闭环；P3-25 只完成同步路径，资源扫描路径仍未修复。当前入口见 [Issue7.md](./Issue7.md) §12.3～§12.5。

> ⚠️ **可立即执行与待决的区分**：
> - **P0-01（阿里云端口 key 不对称）已修复**：提交 `108e528`，回归 `TestDiff_AliyunPortRoundTripConverges`；Issue7 Step 1 的新规划器已保持该绿色回归。
> - **P1-01 本地核验项已实施**：严格 TAG 命名空间、canonical `FunctionalKey`、目标级 planner、先增后验状态机与四平台条件清理主线已落地；核验补强提交为 `38bdc19`，R7-01～R7-04/R7-06/R7-07 已提交，R7-05 已在当前工作树修复。真实四云/浏览器/远端 CI 仍待人工执行。
> - **P1-02（容器 pidfile 崩溃循环）仍为独立未修复项**：源码仍使用 PID 判活，方案已定为 `flock`；本轮只更新本报告，不赋予代码实施授权，也没有取得当前 Docker/进程门禁证据。

### 本次审核中被驳回的候选（**请勿据其动手**）

| 候选 | 裁决与依据 |
|---|---|
| "`slog.TextHandler.Handle` 按 level 二次过滤 → WebUI 日志丢行" | **驳回**。Go 源码 `log/slog/handler.go` 中 `TextHandler.Handle` **不含任何 level 判断**（过滤只在 `commonHandler.enabled`）；且 `MultiHandler.Handle`（`app/logutil.go:30`）已按 `h.Enabled(ctx, r.Level)` 逐子 handler 正确门控。不存在双重过滤 |
| "发布顺序与 AGENTS.md 相反" | **驳回（但引出真实缺陷）**。这是审计快照中的历史结论：当时 AGENTS.md:199 原文为"…监督器唤醒 → Uptime Kuma Push 唤醒 → `RuntimeState`"，与当时代码**顺序一致**；该分派误读了文档。真正的缺陷不是"不一致"，而是该顺序本身会引发 P2-04。2026-09-29 Step 0 已把当前强合同修订为先发布 `RuntimeState` 再唤醒，代码仍待独立实施 |
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
| **TAG 上限收紧（48→32）** | 🔶 **已撤回** | 该收紧**仅为配合方案 A** 而提出。`AGENTS.md:162` 未修改，`maxTagRunes` 仍为 **48**（`config/validate.go:34`） |

**📚 历史授权边界（2026-09-29 Step 0 时）：** 当时尚未获得后续代码实施授权，故不得进入 Step 1。该句不代表当前状态；后续授权已在本地完成 Step 1～5 与 R7-01～R7-07 本地核验项。TAG 上限 48 与 Lighthouse/SWAS 描述容量仍会挤压 comment 的可读长度，但不再影响功能身份或 P1-01 已实施的主线。

**跨方案共通的前提**（无论最终选哪个方案都成立）：

- `hasPrefix`/`Parse` 以 `[TAG]` 前缀识别归属的机制**不能取消**（AGENTS §三 要求），因此 `[TAG] ` 前缀的容量开销是固定成本。
- 云端描述字段的上限：Lighthouse **64**（`TencentLighthouseAPIGuide/添加防火墙规则.md:15`）、SWAS **50**（来源存疑，见下）、ECS **512**（`AliyunECSAPIGuide/AuthorizeSecurityGroup.md:128`）、CVM 未声明上限。
- ⚠️ **不确定性**：SWAS 的 `Remark ≤ 50` 仅见于代码注释（`syncer/retry.go:200`），**仓内 API 文档查无出处**（已在 6 个提及 `Remark` 的文件中核查），需真实云确认（含字符/字节口径）。已登记为 **ProdTestList.md PT-B7-09**。
- 🔶 **与 TAG 上限的关系**：现行 `maxTagRunes = 48` 与上述描述字段上限之间**已经**存在紧张关系（TAG 用满 48 时 `[TAG] ` 占 51 runes，仅剩约 13 runes）。这属于同一字段的容量约束，研究本项时应一并考虑；但**本项修复不以收紧 TAG 为前提**，此前提出的 48→32 收紧**已随方案 A 一并撤回**。

---

### P1-02｜容器陈旧 pidfile 导致无限崩溃循环（Docker 部署专有）

**当前状态：🔵 真实存在，方案已定，源码尚未修复。** 当前 HEAD 为 `34aa9b859c38904e701a19d672dcc4f65435b31c`；本轮只核验并更新本报告，未修改源码/测试、未构建、未运行测试或格式化。历史 Docker 复现仍然有效地支持问题判断，但不能写成本轮重新复验通过。

#### 当前失效链与影响

启动调用链为 `main.go:5 → run.go:28 run → run.go:35 LoadDeploymentConfig → run.go:41 MkdirAll → run.go:46 runWebUI → run.go:70 GetPidFilePath → run.go:71 WritePidFile`。

- `config/pidfile.go:16-34` 先 `ReadFile` 解析 PID，再调用 `processExists`，最后用 `os.WriteFile` 覆盖当前 PID；检查与占用不是原子操作。
- `config/pidfile_unix.go:10-15` 用 `os.FindProcess` + `proc.Signal(syscall.Signal(0))` 判定“进程存在”，无法证明该进程就是本应用。
- `run.go:71-76` 启动时写入并在正常退出时 `defer cleanup()`；SIGKILL/OOM-kill 不执行清理。
- `build/Dockerfile:27-34` 让应用直接成为容器 PID 1，`docker-compose.yml.example:37-39` 用持久卷保存 `/app/data`，`:56-57` 使用 `restart: unless-stopped`。

因此存在三类相互独立的失效：

1. 容器被 SIGKILL/OOM-kill 后残留 `"1\n"`，新容器同样是 PID 1，`Signal(0)` 对自身成功，错误拒绝启动并形成重启循环。
2. 旧实例崩溃后，残留 PID 被无关进程复用时，当前实现无法识别身份，会永久拒绝启动，直到人工删除文件。
3. 两个进程可同时通过“读取/判活”检查，再分别覆盖写入，存在 TOCTOU 双实例窗口；一次并发实验未命中窗口不能证明并发安全。

#### 历史实测证据与可信边界

审计过程曾用持久命名卷复现：预置 `/app/data/fwalizer.pid` 内容 `1` 后启动 `fwalizer:audit`，结果为 `state=exited exit=1`，日志为「FWAlizer 已在运行 (PID: 1)，请先停止现有实例」。首次 `--volumes-from` 实验未真正复用卷，后来改用持久命名卷才稳定复现；该方法学更正应保留。

这属于此前审计过程的历史实测，不是本轮重新运行结果。当前源码、Dockerfile、Compose 持久卷与重启配置仍与该复现链一致，因此静态证据支持问题仍在；但在实施完成前不得宣称已修复，也不得把历史结果升级为当前 Docker 通过。

#### 已确定的最小修复设计（仅记录，不在本阶段实施）

用户已决定采用 `flock` 替换 PID 判活。推荐保持 `WritePidFile(path) (cleanup func(), err error)` 调用边界，具体为：

1. `config/pidfile.go` 用 `os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)` 打开锁文件；成功后在同一打开的文件描述符上取得非阻塞独占锁。
2. 获取锁后截断并写入当前 PID；PID 只用于错误提示和诊断，不再参与实例判定。锁文件路径保留，cleanup 只关闭文件描述符释放锁。
3. 锁竞争失败时，`EWOULDBLOCK`/`EAGAIN` 统一表示已有实例持锁；读取文件内容只为生成提示，空、损坏或无法读取时仍应拒绝启动，不能因诊断 PID 不可解析而绕过真实锁。权限、路径等其他错误原样返回。
4. `config/pidfile_unix.go` 提供精确 `linux || darwin` 的平台 helper，删除不再需要的 `processExists`；`run.go:76` 现有 `defer cleanup()` 原则上可保持不变。cleanup 的 `Close()` 错误必须按 AGENTS.md 处理，最小方案是在无返回值 cleanup 中安全记录 WARN，不为本项扩大调用接口。

#### 实现选项、推荐与影响

| 选项 | 推荐 | 影响 |
|---|---|---|
| `syscall.Flock`；或引入 `golang.org/x/sys/unix` | **推荐 `syscall.Flock`** | 标准库即可完成，不新增直接依赖；若改用 `x/sys/unix`，需改变依赖面但锁语义目标不变 |
| cleanup 只 `Close()` 并保留锁文件；或关闭后 `Remove` | **推荐保留文件，只 `Close()`** | 避免“释放旧 inode 后删除路径”重新打开竞态；文件可残留但不再代表运行实例 |
| 将文件权限迁移/收紧并入本项；或留给 P3-12 | **留给 P3-12** | 本项只解决实例互斥；既有 `0644` 文件是否显式 `Chmod` 不应无授权扩大到 I-01 |
| 保留 PID 判活作为 fallback；或完全以锁为准 | **完全以锁为准** | fallback 会重新引入 PID 复用误判；PID 仅保留为诊断文本 |

上述推荐不改变用户已决定的 `flock` 方向；若实施前要改动这些边界，必须先补充决策及其影响，不得自行扩展范围。该设计不违反 AGENTS.md：要求是通过 pidfile 防多实例，并未规定必须用 PID 判活。

#### 受影响文件

- 生产代码：`config/pidfile.go`、`config/pidfile_unix.go`；`run.go` 原则上不改，只有 cleanup 错误处理或签名改变时才联动。
- 测试：新增 `config/pidfile_test.go` 覆盖锁竞争、空/损坏诊断、权限与释放；调整 `main_test.go:994-1059`，不再要求正常退出后文件删除，增加真实顺序、并发、SIGTERM、SIGKILL 后重启覆盖。
- 用户文档：`README.md:132`、`:499` 将“检测已有实例/进程”改为“通过 OS 文件锁防止同一数据目录多实例，文件 PID 仅供诊断”。
- 验收材料：本报告在代码与门禁证据取得后才能把 P1-02/I-01 改为已修复；`Issue7.md` 不纳入本项，因其已明确将 pidfile/flock 排除在 P1-01 主线之外。

#### 判别性验收合同（按层级执行）

1. 静态：源码不再有 `processExists`、`Signal(0)` 或无锁覆盖写入；平台 tag 仍精确为 `linux || darwin`。
2. config：预置 `"1\n"`、当前测试 PID、空内容、损坏内容均不能绕过真实锁；测试进程先持有同一文件 `LOCK_EX` 时，`WritePidFile` 必须立即失败；新文件权限为 `0600`。
3. 进程：两个真实二进制并发使用同一数据目录时严格只有一个获得锁；第一个进程 SIGTERM 或 SIGKILL 后，第二个均能启动；锁文件可以残留但不能阻止后续实例。
4. Docker：持久命名卷预置 PID `1` 后容器能启动；运行容器被 `docker kill` 后复用同一卷仍能启动并达到既定健康状态。
5. 工程门禁：受影响包 `-race`、全仓 `-race`、`go vet ./...`、`go build ./...`、`gofmt -l`、Linux/macOS 构建与 `git diff --check` 均通过。

并发验收不得只依赖固定 `Sleep` 或“偶尔命中”旧 TOCTOU 窗口；先由测试持有内核锁再调用 `WritePidFile` 是最强判别控制。旧实现会错误覆盖并成功，新实现必须拒绝。

#### 风险、外部边界与停止条件

- `flock` 适用于当前 Linux/macOS 本地文件系统、Docker named volume 与普通本地部署；NFS 或部分网络文件系统语义可能不可靠，应作为部署边界提示，不在本项引入分布式锁。
- 新实现无法识别仍在运行但使用旧 PID-only 版本的进程；升级时需先停止旧实例，不能以重新引入 PID 判活作为兼容层。
- 本项不依赖真实云、DNS、SMTP、Webhook、Uptime Kuma 或浏览器；远端 CI/GHCR 可作常规发布门禁，但不是证明锁机制正确的必要条件。
- 在实现、定向测试、真实进程测试和 Docker 复验未全部取得证据前，停止将 P1-02/I-01 标记为已修复；任一平台 tag、锁失败语义、SIGKILL 后重启或并发互斥不满足时，停止推进并回到方案/实现核查。
- 本阶段停止条件已满足且必须保持：只更新本报告，不编辑代码或其他文档，不构建、不测试、不格式化；因此本轮不改变 P1-02 的未修复状态。

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

- **证据**：`provider/ali_ecs.go:163-190` 一次性把全部 `RuleID` 传给 `RevokeSecurityGroup`；而 **create 是分批的**（`:111-126` 使用 `batchRules(rules, 100)`）。`PlatformAPIDocs/AliyunECSAPIGuide/RevokeSecurityGroup.md` 明确 `SecurityGroupRuleId` **数组长度 0~100**。
- **触发条件**：单个 (provider, 规则) 单元待删规则 >100——例如域名 IP 集大幅收缩（TCP+UDP 拆分后 50+ 个 IP 即达 100），或修改端口列表（60 个端口 × TCP+UDP = 120 条）。
- **实际影响**：删除请求被云端拒绝 → 该单元每轮 `failed` → 整轮 failed → **运行健康持续 unhealthy 并反复告警**；同时**旧规则（旧 IP 的放行）永久残留在安全组里，安全面持续扩大**，需人工清理。
- **推荐整改**：`DeleteRules` 复用 `batchRules(rules, 100)` 逐批提交，并把已确认批次累加进 `PartialDeleteError.Deleted`。
- **判别性测试**：mock 增加"`SecurityGroupRuleId` 数量 >100 则返回 400"的约束，用 150 条断言产生 2 个请求。
- **是否与 AGENTS.md 冲突**：否（§十一 要求遵守 `PlatformAPIDocs/` 的参数限制）

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

> **当前状态（2026-09-30）**：🔵 **问题真实存在，当前未修复；本轮仅更新本审计条目，未修改源码、测试或其他文档，也未运行构建、测试或格式化。** 本轮基线为 `main` / `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树中已有的未提交修改仅涉及本报告。

- **强合同与当前实现**：当前 [AGENTS.md](./AGENTS.md) 的 §十一要求 commit 后按“日志级别 → 告警集合 → `RuntimeState` → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒”发布，并明确“唤醒时新运行时必须已可见”。但 `webui/api/deps.go` 的 `Deps.applyCandidate` 当前实际顺序仍是：日志级别 → 告警集合 → `Health.Wake()` → `Push.Wake()` → `Syncer.ApplyState()` / `Runtime.Apply()`；`ConfigCoordinator` 在 commit 后直接调用该 apply，没有额外屏障，`Syncer.ApplyState` 最终才替换 `RuntimeManager` 指针。
- **缺陷链**：`internal/health/push.go` 的 Pusher 循环每轮首先读取 `p.config()`（生产接线中来自同一个 `RuntimeManager` 的 `Snapshot()`）。如果配置由关闭改为启用时，Pusher 在 `ApplyState` 前被 `Push.Wake()` 唤醒，它可能读到旧的关闭配置；“未启用或 URL 为空”分支随后只等待 `wake/stop`，没有 interval 定时器。本次保存产生的 wake 已被旧配置路径消费后，新的启用状态可能要等下一次配置保存或重启才触发首条心跳。
- **触发条件与影响**：只需在运行期间把 `uptime_kuma_push` 从关闭保存为启用，且 Pusher 恰好落在 `Wake → ApplyState` 窗口即可触发。影响是新配置不会按本次保存及时发出首个 Push；若 Uptime Kuma 侧已有监控状态，可能继续看到旧状态或误判 DOWN。该结论来自源码时序分析，不是“真实 Uptime Kuma 已复现”；真实 Push 的 DOWN/恢复通知仍未执行。已在途的旧配置 HTTP 请求不属于本项必须取消的对象。

**后续设计与最小修复**

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

- **A（推荐，独立处理）**：本项只修复 RuntimeState 先发布再 Wake；P3-03 另行裁决非法 URL/请求构造失败是否需要 interval 重试。当前正常 HTTP 状态错误、网络错误、超时和非 `ok` JSON 已按 interval 继续尝试，不能把 P3-03 泛化成“所有首次 Push 失败都静默”。优点是补丁和验收单一、不会改变生产超时或健康语义；代价是非法配置仍可能等待下一次配置唤醒，需在独立议题中明确接受或修复。
- **B**：与本项同时为 `sendOnce` 返回 false 的非法 URL/请求构造失败增加周期性重试。影响是需要另行决定重试间隔、WARN 频率、是否在 operational health/UI 中暴露配置错误，以及如何避免脏数据库造成日志噪声；范围超出 P2-04 的最小时序修复，当前不推荐直接并入。

**风险、外部依赖与停止条件**

- 风险较低但需保持边界：RuntimeState 提前发布是无失败内存操作；已经开始的旧 Health 检查或旧 Push HTTP 请求可以继续完成，本项不增加完成屏障，也不要求配置 API 等待网络请求。
- 该问题可由共享 `RuntimeManager`、Wake 探针、webui/api 定向测试和本地 mock Push 完全证明，不依赖真实云账号或真实 Uptime Kuma。真实 Uptime Kuma HTTP Monitor、Push 的 DOWN/恢复通知、真实云 API、浏览器和远端 CI 仍是独立外部边界，当前均无通过结论。
- 在判别性用例证明 Wake 时已经看到新配置、两条 apply 分支均通过、注释无旧顺序残留前，保持 P2-04/I-02 未修复；不得以仅检查 Wake 次数、最终状态或单次绿色运行替代时序断言。
- 若修复引入等待 Push/Health 完成、数据库或网络操作，修改 `Pusher`/`Supervisor` 内部同步、改变 shutdown 语义、把 P3-03 重试语义混入，或无法证明生产接线共享同一 RuntimeManager，应停止并重新审查范围。

**决策与合同关系**

- 用户已有决策是采用“`ApplyState` 提到两次 `Wake()` 之前”的最小代码修复；这是后续实现方向，不等于本轮已授权修改源码。
- 当前实现与 AGENTS.md §十一的发布顺序冲突；Issue7 已将 P2-04 登记为 P1-01 越界的独立问题。本条更新不把历史审计记录或该决策扩展为代码实施授权，也不顺带关闭 P3-03。

---

### P2-05｜Webhook "成功"仅看 HTTP 状态码，忽略响应体业务错误码 → 告警静默失效

- **状态**：🔵 **问题真实存在，当前未修复，仍属于独立问题队列 I-05；本轮仅更新审计记录，未修改源码、测试或其他文档，未运行构建、测试或格式化。** 它与 Issue7/P1-01 主线正交，不能因 P1-01 Step 0～5 主体完成而关闭，也不能把已有 Uptime Kuma Push 的严格 `{"ok":true}` 判断外推到普通 Webhook。
- **当前基线与工作树证据**：当前为 `main` / `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；本轮核验时工作树唯一已跟踪修改是本审计文档，源码与测试没有本轮修改。该状态是当前核验结论，不沿用历史快照中的 HEAD 或“工作树干净”措辞。
- **生产缺陷链**：`EventBus.Publish` 异步调用 `WebhookNotifier.OnEvent`；`notifier/webhook.go:120-129` 发起 POST 后只关闭响应体，`notifier/webhook.go:126-128` 只判断 `resp.StatusCode >= 300`，从不读取或解析 `resp.Body`。因此所有 2xx 响应都返回 `nil`，即使响应体明确表示业务失败。EventBus 只有在 `OnEvent` 返回错误时才记录“事件处理失败”WARN，所以该路径既被当成发送成功，也不会产生告警日志。
- **现有测试为何不能证明成功语义**：`notifier/webhook_content_test.go:19-29` 的假 transport 固定返回 `HTTP 200 + {}`，当前只验证正文、格式和确定性，没有 `200 + {"errcode":310000,...}` 的失败控制；`notifier/webhook_security_test.go:75-119` 等现有测试也没有覆盖 `errcode`、`errmsg` 或响应体业务状态，全仓未发现对应的判别性解析测试。故“测试通过”不能证明当前 Webhook 已正确接受消息。
- **外部协议依据与准确边界**：
  - 钉钉常见成功语义为 JSON `errcode == 0`；关键词、签名或 IP 白名单校验失败、Token 不存在、机器人停用等错误可以表现为 HTTP 200 但 `errcode != 0`，例如 `310000`、`300001`、`400102`。参考[阿里云钉钉通知错误码文档](https://www.alibabacloud.com/help/en/sls/error-codes)及[关于 HTTP 200 与 `errcode=310000` 的说明](https://www.alibabacloud.com/help/zh/cms/cloudmonitor-1-0/support/the-dingtalk-robot-that-sets-the-alarm-contact-reported-an-error-the-signature-sent-by-the-robot-does-not-match)。
  - Slack Incoming Webhook 的官方口径是成功通常返回 HTTP 200 且正文为纯文本 `ok`；其他正文，即使仍为 200，也不能按成功处理。HTTP 400/403/404/410/500 等则属于 HTTP 层失败。参考[Slack Incoming Webhooks](https://api.slack.com/messaging/webhooks)和[Slack 错误状态说明](https://api.slack.com/changelog/2016-05-17-changes-to-errors-for-incoming-webhooks)。
  - 飞书资料同时出现 `code/msg` 业务错误形态和 `StatusCode/StatusMessage` 成功形态；公开官方开发者社区示例展示了非零 `code`（如 `19001`、`19024`）等错误，但该页面不能单独作为当前接口唯一规范。实施前必须以当前飞书机器人文档或真实端点响应确认最终兼容范围，不能简单把钉钉的 `errcode` 规则套到飞书。
- **实际影响**：钉钉等端点若返回 HTTP 200 业务失败，`OnEvent` 返回 nil，日志没有失败 WARN，用户会误以为告警已发送而实际未送达。这是告警静默失效，不是普通 HTTP 网络错误；现有 Webhook 错误安全测试只能约束传输错误时的敏感信息，不覆盖业务失败响应。
- **最小后续设计（尚未实施）**：只修改 Webhook 响应处理和其判别性测试，不改变告警订阅、EventBus 异步模型、每渠道在途限流、配置契约、Webhook 重试策略或 Uptime Kuma Push。`OnEvent` 尾部应按“非 2xx → 有界读取响应体 → 按 channel 校验业务成功 → 明确成功才返回 nil”的顺序处理；建议增加包内私有 `validateWebhookResponse(channel string, status int, body []byte) error` 或等价 helper。响应体读取必须有小的上限，避免第三方返回异常大正文导致无界内存占用。
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
- **用户决策选项（本轮不擅自裁决）**：

  | 选项 | 飞书响应兼容范围 | 影响 |
  |---|---|---|
  | **A（推荐）** | 同时接受已确认的 `code == 0` 与 `StatusCode == 0`；明确非零失败，未知/空/非法响应失败 | 兼容当前资料中两种已见形态，并采用失败闭合；测试矩阵较完整，实施前仍需以当前飞书文档或真实响应确认字段合同 |
  | B | 只接受当前实际使用的一种明确成功结构 | 逻辑更窄、误接受面更小，但可能拒绝项目现有端点的另一种合法响应；需用户确认当前实际形态 |
  | C | 所有 2xx 或任意 JSON 视为成功 | 保留现状的静默丢告警风险，无法满足 P2-05 修复目标，不推荐 |

  钉钉的 `errcode == 0` 与 Slack 的正文 `ok` 检查是独立于该选择的固定修复方向；用户未选择飞书范围前，不应实施飞书兼容分支，也不应据此自行扩大或收窄产品合同。
- **风险、相邻边界与停止条件**：主要风险是把渠道协议错误抽象为一个通用字段，或为了兼容而把任意 2xx 当成功，继续造成告警静默丢失；未知响应必须失败闭合，响应体读取必须有界。若实施时修改 EventBus 异步/限流/重试、告警配置 API、Uptime Kuma Push、生产 HTTP 超时，或把原始 URL、token、请求正文、响应正文/`errmsg`/`msg` 写入错误和 WARN，应立即停止并重新审查范围。未补齐三渠道判别性响应测试、未完成安全错误断言以及受影响包门禁前，保持 I-05/P2-05 未修复；不得以单次绿色运行或真实端点的 HTTP 200 代替业务层成功证据。
- **是否与 AGENTS.md 冲突**：当前最小修复方向不冲突。AGENTS.md §9.1 已明确 Uptime Kuma Push 的 `{"ok":true}` 成功口径；P2-05 只是为普通 Webhook 渠道补齐各自协议的业务成功判断，不改变既有配置、异步投递或外部验收边界。

---

### P2-06｜告警页加载失败后仍可保存，用初始默认值覆盖全部真实告警配置并提示"保存成功"

- **状态**：🔵 **问题真实存在，当前未修复，仍属于独立问题队列 I-03；本轮只更新审计记录，没有修改源码、测试或配置契约。** 不能因后端告警 API、事务或校验测试通过而关闭本项，也不能把它并入 Issue7 Step 1～5 的完成结论。
- **当前基线与证据边界**：当前 `HEAD=34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树已有修改仅为本审计报告。本轮核验未构建、未测试、未格式化。源码证据为 `webui/frontend/src/views/Alerts.vue:19-48` 四组表单在组件初始化时直接采用合法默认值：所有渠道和触发开关关闭，SMTP 主机/用户名/密码/地址为空，Webhook/Push URL 为空，主题/正文为默认文本，策略超时为 `10m`、Push 间隔为 `60s`。`Alerts.vue:97-107` 的 `load()` 失败只显示错误消息，没有失败状态，也没有阻止保存；`Alerts.vue:111-130` 的 `save()` 没有 `loaded` 检查；`Alerts.vue:257` 的保存按钮只绑定 `saving`，加载失败时仍可用。`webui/frontend/src/api.ts:18` 会把非 2xx、网络异常和超时抛给 `load()`，所以失败路径可实际触发。
- **完整触发链**：

  ```text
  GET /api/alerts 失败
  → 真实配置没有进入表单，表单继续保留初始默认值
  → 用户仍可点击“保存配置”
  → save() 发送完整四对象 PUT /api/alerts
  → 后端覆盖写入 policy/email/webhook/uptime_kuma_push
  → 页面提示“保存成功”
  ```

- **后端为何挡不住**：`webui/api/alerts.go:224-284` 的 `PUT /api/alerts` 是四个完整对象的覆盖式保存；`ReplaceBusinessAlertsTx` 在同一事务内覆盖四张告警单行表。`config/validate.go:277`、`:323`、`:362` 规定渠道 `enabled=false` 时允许空 SMTP 主机或空 URL，默认端口、主题/正文、`10m`、`60s` 也均合法，因此加载失败后提交的默认四对象能够通过严格解析与校验，并实际覆盖 SMTP 参数/密码、Webhook URL、Push URL/token、触发开关及策略。
- **实际影响**：一次瞬时读取失败后，用户若继续保存，会把真实敏感配置和行为配置静默替换为默认值，并收到误导性的“保存成功”提示。受影响的不只是展示字段，还包括 SMTP 密码、Webhook URL、Uptime Kuma Push URL（含 token）、三个触发开关、`health_timeout` 及 Push 间隔。该问题属于前端状态机与覆盖式接口组合造成的破坏性写入，不是后端 DTO 校验缺失。
- **后续最小设计（推荐方案 A）**：仅修改 `webui/frontend/src/views/Alerts.vue`，不改变后端完整对象保存契约。

  1. 增加初始为 `false` 的 `loaded` 状态。
  2. `load()` 开始时先设 `loaded=false`，避免未来重载失败时继续提交旧表单；只有四个对象 `policy`、`email`、`webhook`、`uptime_kuma_push` 均完整取得并整体写入表单后，才一次性设为 `true`。
  3. 对 HTTP 200 但缺少任一对象或对象不完整的响应按加载失败处理，保持 `loaded=false`；可增加页面内告警，明确说明“告警配置未成功加载，保存已禁用，请刷新页面重试”。
  4. `save()` 首行增加 `if (!loaded.value) { message.error('告警配置尚未成功加载，无法保存'); return }`，按钮同时使用 `:disabled="saving || !loaded"`。按钮禁用不是唯一防线，函数入口仍必须拒绝保存。
  5. “测试发送邮件”继续允许使用当前表单值，不因 `loaded=false` 一并禁用；它按既有合同不写 SQLite、不执行 PUT、不应用告警集合。

- **受影响文件与明确不扩大的范围**：生产改动和测试证据集中在 `webui/frontend/src/views/Alerts.vue`，必要时只增加与该组件状态机直接相关的轻量验证。`webui/api/alerts.go`、`config/validate.go`、SQLite schema/事务、`types.ts`、测试邮件 API、配置导入导出均不应修改。不得把 `/api/alerts` 改成部分更新、缺字段不覆盖或静默保留旧值；Build7 已固定该接口为四个完整对象的覆盖式原子保存。不得引入 Pinia、Vitest、Vue Test Utils、Playwright 或 Cypress 等重型前端测试栈来解决单一守卫问题。
- **判别性验收（修复前应能暴露，修复后必须通过）**：

  1. **失败加载后禁止覆盖（核心用例）**：先保存含明显标记的 SMTP 密码、Webhook URL、Push URL 和至少一个启用触发开关的配置；让 `GET /api/alerts` 返回 500、网络错误或超时；进入告警页，断言保存按钮 disabled，点击不产生 `PUT /api/alerts`，页面不显示“保存成功”，数据库中的敏感配置保持不变。
  2. **成功加载后仍可保存**：`GET /api/alerts` 返回四个完整对象；断言加载完成后保存按钮可用；修改普通字段并保存，断言发送完整四对象 PUT 并成功，原有密码和 URL 未被无意清空。
  3. **重载失败重新锁定**：若存在重载入口，先成功加载，再让第二次加载失败；断言 `loaded` 回到 `false`、保存重新禁用，旧表单残留值不得再次提交。
  4. **200 但结构不完整**：返回缺少 `email`、`webhook` 或 `uptime_kuma_push` 的 200 响应；按加载失败处理，保持 `loaded=false` 且保存禁用。
  5. **测试邮件边界**：告警配置加载失败时，测试发送按钮仍按既有合同使用当前表单值；不产生 PUT、不写 SQLite、不改变告警订阅。该反向用例防止把本项扩大为所有操作都必须等待 GET 成功。

- **验收层级与外部边界**：源码检查需证明 `loaded` 初始为 false、仅完整成功载荷后为 true，`load()` 失败会保持 false，按钮和 `save()` 均受保护；实现后执行现有前端 `npm run build`。必须补充浏览器/API 联合证据，覆盖失败加载后的保存阻断与成功加载后的正常保存，并保留本地数据库配置不变的证据。后端 API 单测只能证明服务端契约，不能单独证明 Vue 状态机安全。真实云、真实 SMTP、真实 Webhook、真实 Uptime Kuma 和远端 CI 不属于本项前置；Build7 的 PT-B7-07 浏览器人工回归仍是独立未执行项，本项证据应作为其补充，不能提前把整项写成通过。
- **风险与相邻边界**：最小修复只阻止非 2xx、网络异常、超时和明显的 200 响应结构不完整时提交默认值，不能解决 `GET /api/alerts` 当前四次独立读取造成的并发 PUT 撕裂快照问题；后者应另立 GET 原子快照/一致性事项。实现时不得把测试邮件、告警订阅、后端事务或数据库 schema 一并改动。风险控制重点是避免按钮视觉禁用但程序入口仍可保存，以及避免只检查 HTTP 200 而接受不完整载荷。
- **用户决策选项**：

  | 选项 | 方案 | 影响 |
  |---|---|---|
  | **A（推荐）** | `loaded` 守卫 + 加载失败提示 + 保存按钮禁用 + `save()` 入口拒绝；成功后端载荷完整性检查；测试邮件保持独立可用 | 最小改动即可阻断破坏性覆盖，保持 Build7 `/api/alerts` 完整覆盖契约；需补浏览器/API 联合验收 |
  | B | 仅禁用保存按钮，不在 `save()` 入口增加守卫 | 改动略少，但键盘触发、程序调用或未来组件变化仍可能绕过 UI，不能形成可靠安全边界；不推荐 |
  | C | 修改后端 PUT 为部分更新或缺字段不覆盖 | 会改变已固定的四对象原子覆盖契约，扩大事务/API/导入导出影响面；不建议作为 P2-06 修复 |

- **停止条件**：未取得失败加载后“无 PUT、无成功提示、敏感配置不变”以及成功加载后正常保存的判别性证据前，保持 I-03/P2-06 未修复；仅源码静态检查或后端 API 测试不得关闭本项。若实现改动后端完整对象契约、禁用独立测试邮件、引入重型前端测试栈，或未能证明 200 不完整载荷会保持未加载状态，应停止并回到方案审查。**是否与 AGENTS.md 冲突**：当前最小修复方案不冲突；它落实 AGENTS 的可靠性与敏感配置保护边界，同时不改变 AGENTS/Build7 已固定的 `/api/alerts` 覆盖式保存语义。

---

### P2-07｜目标与规则的行内删除**零二次确认**（违反 AGENTS §11）

- **当前状态**：🔵 **问题真实存在，当前未修复，仍属于独立队列 I-04**。本轮只更新审计记录，未修改源码、测试或强要求文档，未构建、测试或格式化；已有未提交改动仍仅涉及本报告。不能标记为已修复，也不能把后端已有的引用保护当作前端二次确认证据。
- **当前证据**：`webui/frontend/src/views/Targets.vue:121` 的 `deleteTarget()` 直接调用 `DELETE /api/targets/{id}`，`:165` 的表格按钮直接绑定 `onClick: () => deleteTarget(row)`；`webui/frontend/src/views/Rules.vue:86` 的 `deleteRule()` 直接调用 `DELETE /api/rules/{id}`，`:127` 的按钮直接绑定 `onClick: () => deleteRule(row)`。两个页面虽有新增/编辑 `NModal`，但没有删除确认状态、待删除对象、取消路径或提交中守卫。相对地，`webui/frontend/src/views/Logs.vue:123` 与 `Settings.vue:450` 已提供 `NModal preset="card"` 确认模式，可作为局部实现参考。AGENTS 强制条款当前位于 `AGENTS.md:210`：所有二次确认使用卡片式 `NModal`，危险确认按钮使用 `type="error"`。
- **后端边界与已存在保护**：`webui/api/deps.go:184` 已注册目标和规则两个 DELETE 路由；`webui/api/targets.go:104` 在目标仍被规则引用时返回 409，`webui/api/rules.go:122` 已有正常删除事务并对不存在 ID 返回 404。上述后端行为已存在且不应改变：目标的 409 是数据完整性保护，不是误触确认；规则正常删除路径没有 409 兜底。当前没有证据表明后端删除接口本身不存在或需要修复。
- **实际影响**：用户一次误触即可删除本地 SQLite 中的目标或规则配置；规则删除没有后端冲突兜底，目标删除只有被规则引用时才会被 409 拦截。目标/规则列表中的删除按钮与编辑按钮相邻且为小号操作，误删可能改变后续同步期望，进而影响生产防火墙可达性。该问题是 WebUI 误触保护缺失，不是 API 授权边界；直接调用 DELETE API 的客户端仍按既有后端契约执行。
- **后续最小设计（推荐方案 A）**：仅在 `Targets.vue` 与 `Rules.vue` 各自增加本地确认状态，不引入共享组件、Pinia、路由、后端确认令牌或新的测试框架。
  1. 增加 `showDeleteConfirm`、对应类型的 `pendingDelete` 和 `deleteSubmitting` 状态。
  2. 表格删除按钮只调用 `openDeleteConfirm(row)`，不再直接调用 DELETE；弹窗使用 `NModal preset="card"`，目标弹窗展示云产品/资源 ID/地域，规则弹窗展示域名/协议/端口等摘要，不显示完整凭据或其他敏感信息。
  3. 确认处理函数是唯一 DELETE 调用点：读取待删除行后先设置提交中并同步关闭弹窗，再发送请求；若已提交或待删除对象为空则直接返回。`deleteSubmitting` 防止确认按钮快速双击产生重复 DELETE。
  4. 取消、关闭按钮和遮罩关闭只清理/关闭确认状态，不发送请求；成功后保持现有成功提示与列表刷新；失败后显示错误并复位状态，使用户可以重新操作。危险确认按钮必须使用 `type="error"`，页面级操作按钮继续遵守既有大号尺寸规范。
  5. 后端 API、引用保护、同步器、配置协调器、数据库 Schema 与 Provider 均不改。
- **受影响文件与明确排除**：预计仅修改 `webui/frontend/src/views/Targets.vue`、`webui/frontend/src/views/Rules.vue`。`webui/api/targets.go`、`webui/api/rules.go`、`webui/api/deps.go`、SQLite schema、ConfigCoordinator、Syncer、云 Provider 与强要求文档均不应因本项修改。当前已有的目标引用 409、规则 404/成功语义必须保留。
- **判别性验收（实施后必须取得）**：
  1. **取消路径**：目标和规则分别打开确认框后点击取消、关闭或遮罩；浏览器网络面板确认没有对应 DELETE，目标/规则仍存在。
  2. **确认只发一次**：确认删除并快速连续点击确认，网络面板只出现一条 DELETE；成功提示只出现一次且列表刷新一次。目标与规则分别覆盖。
  3. **目标引用保护**：目标仍被规则引用时确认删除，DELETE 仍返回 409，页面显示失败，目标和规则都保留；前端不得先删规则、解除引用或扩大规则适用目标。
  4. **正常删除**：未被引用的目标与已创建规则分别确认删除，返回 2xx 且从列表消失；取消时数据不变。
  5. **失败后可重新操作**：对已被其他操作删除的目标确认删除，验证 404 能正常显示，弹窗关闭且 loading 复位；随后对另一行仍可重新打开和确认。
  6. 实施后至少执行前端 `npm run build`，再做本地浏览器/API 联合验收；仅源码检查或后端单测不能证明“取消不发请求”与“确认只发一次”。
- **风险、外部依赖与边界**：本项不依赖真实腾讯云、阿里云、SMTP、Webhook 或 Uptime Kuma，删除的是本地 SQLite 配置数据；但需要浏览器交互证据。主要风险是只做视觉禁用而没有确认处理函数守卫、重复点击造成重复 DELETE，或为“方便删除”绕过目标引用 409。确认框不提供后端安全授权，也不改变 API 直接调用语义；真实云/真实通知链路和远端 CI 不属于本项前置。当前没有实施后的通过证据，不能把 P2-07 写成已修复。
- **用户决策选项**：

  | 选项 | 方案 | 影响 |
  |---|---|---|
  | **A（推荐）** | 两个页面各增加本地 `NModal preset="card"` 删除确认；表格按钮只打开弹窗；确认处理函数唯一发送 DELETE，并用 `deleteSubmitting` 防重复提交；后端语义不变 | 最小改动满足 AGENTS 强要求，并覆盖取消、重复点击和失败复位；需要前端构建与浏览器/API 联合验收 |
  | B | 只增加卡片式弹窗，不增加提交中守卫或状态机边界 | 能阻断普通误触，但快速双击可能产生重复 DELETE，验收证据不足；不推荐 |
  | C | 修改后端增加确认令牌、改变 DELETE 或自动解除目标引用 | 会扩大 API、协调器和数据完整性范围，不能替代前端误触保护；不建议 |

  当前建议采用 A，但本条只记录推荐，不擅自替用户完成该决策或实施代码。
- **是否与 AGENTS.md 冲突**：是；按 A 增加卡片式确认、危险按钮 `type="error"`，即可满足该强要求，不需要修改 AGENTS.md。
- **停止条件**：在取消路径无 DELETE、确认路径单次 DELETE、目标 409 引用保护、目标/规则正常删除、失败后可再次操作，以及前端构建和浏览器/API 联合证据全部取得前，保持 I-04/P2-07 未修复。若改动后端删除契约、自动解除引用、改变规则适用范围、引入重型前端测试栈，或没有浏览器证据就宣称通过，应立即停止并回到方案审查。

---

### P2-08｜仪表盘把 `idle` 轮次误报为"最近一轮未完整成功"

- **证据**：`webui/frontend/src/views/Dashboard.vue:48-70`——`failed`/`partial` 提前 return，其余落到 `:63-65` 的 `lastSync !== lastSuccess` 比较。后端 `syncer/syncer.go:751-755` **每轮**都更新 `lastSync`，**仅 `RoundSuccess`** 更新 `lastSuccess`；`outcomeOf`（`:776-788`）中 `Total==0 → idle`（≠success）。
- **决定性证据**：`webui/sync_status_test.go:161` 的 `assertLastSuccessPreserved` **断言 idle 后 `last_success` 被保留**——证明"`last_sync` 前进而 `last_success` 不动"是**有意的后端契约**，不是缺陷。因此 `:63-65` 分支**唯一可达输入就是 `idle`**，净效果即误报。
- **违反语义**：AGENTS.md:151「`idle` 与空目标/空规则视为正常」
- **实际影响**：仪表盘显示黄色警告"最近一轮同步未取得完整成功"，而 `/api/health/operational` 同时返回 **200** → 前端把健康显示为异常，引导用户排查不存在的问题。
- **推荐整改**：`healthHint` 对 `outcome === 'idle'` 显式返回 null。
- **判别性测试**：跑出一轮 success → 删除全部规则 → 等下一轮 → 出现该警告，同时 `curl /api/health/operational` 返回 200
- **是否与 AGENTS.md 冲突**：否（属补齐 §9.1 语义）

---

### P2-09｜模拟测试结果以 `domain` 作 `v-for` key，同域名多规则时 key 重复且两张表无法区分

- **证据**：`webui/frontend/src/components/DryRunResults.vue:106` `v-for="item in items" :key="item.domain"`；后端 `syncer/syncer.go:604-638` **每条规则**产出一个 `DryRunResult{Provider: p.Name(), Domain: rule.Host}`；`rules` 表无 host 唯一约束（`config/store.go:147-155`），`NormalizeRule` 不做 host 去重。
- **触发条件**：同一域名 ≥2 条规则且适用于同一目标（如 `api.example.com` 的 TCP 443 + UDP 443——这是 TCP+UDP 拆分的自然结果）。
- **实际影响**：同父节点下 key 重复（Vue 要求 key 唯一，dev 报警、生产可能 patch 到错误节点复用旧行）；且两张表用同一个 `<h4>{{item.domain}}</h4>`，**用户无法判断哪张对应哪条规则**——而该页是开启同步前**唯一的变更预览门禁**。
- **推荐整改**：key 改为 `${item.domain}-${index}`，标题加入 protocol/ports（如 `api.example.com · TCP/443`）。
- **是否与 AGENTS.md 冲突**：否

---

### P3 清单（25 项，确认但低风险）

| ID | 结论 | 关键证据 | 备注 |
|---|---|---|---|
| P3-01 | **🔵 问题真实存在，当前未修复**：DNS 熔断器 `failCount` 无淘汰机制，普通运行时发布还会全量 `Clone()` | `dns/circuitbreaker.go:11` 使用 `map[string]int`；`RecordSuccess`（`:59-67`）只写 `0` 不删除；`RecordFailure` 为新域名创建条目；`Clone`（`:37-50`）全量复制；`syncer/state.go:74-82` 的 `BreakerPreserve` 在普通配置发布时保留该 map；`syncer/target.go:362-381` 的解析路径确实持续写入 breaker。当前无 `delete`、裁剪、淘汰或过期机制 | 增长量等于进程生命周期内曾参与解析的不同域名数；成功解析留下永久 `0` 条目，删除/改名域名的正数失败计数也永久残留。属于低风险的无界内存增长，不是立即的功能故障。当前完整证据、后续设计、影响文件、判别性验收、外部边界与停止条件见下方 **P3-01 独立追踪与后续设计**；既有用户决策细化为 **A（推荐）**，尚未授权实施 |
| P3-02 | 熔断器 `IsOpen` **不改变任何控制流**，仅影响日志分支 | `syncer/syncer.go:861-881`：解析照做、轮次照跑；全仓 `IsOpen` 唯一读取点 `:865` | "熔断"无隔离/降频效果；多 provider 共享同一域名时阈值按单元累加、由任一成功清零，"连续失败轮数"语义被扭曲。AGENTS §四字面满足（每轮本就只探测一次）。建议接受现状并在注释/文档写明语义 |
| P3-03 | **问题真实存在，当前未修复；范围已收窄为非法 URL/`buildPushURL` 失败后的重试语义**。`internal/health/push.go:105-140` 在启用后首次发送或 URL 变化后的首次发送中，若 `sendOnce` 返回 `false`，保持 `active=false`、清空 `lastURL`，随后只等待 `wake` 或 `stop`，没有按 `cfg.Interval` 重新尝试；因此非法 URL 可使 Push 长期沉默，直到配置再次唤醒。该问题可由手工 SQLite、历史数据或内部构造绕过正常 API 校验进入运行时；`config/store.go:918-945` 读取 `url` 时不重新校验，虽会校验/规范化 interval。与 P2-04/I-02 的 RuntimeState→Wake 发布时序正交，不应借本条扩张时序、健康或告警修复范围 | 精确证据：`internal/health/push.go:186-195` 的首次 `buildPushURL` 失败返回 `false`；`:220-224` 的发送前第二次 `buildPushURL` 失败同样返回 `false`，两条路径都落入 `:130-139` 的纯 `wake/stop` 等待。对照 `:226-230`，`http.NewRequestWithContext` 构造失败返回 `true`，和网络错误 `:231-235`、HTTP 非 2xx `:238-241`、非法 JSON/`ok=false` `:243-254` 一样回到外层 interval 路径；故不能泛化为“所有首次 Push 失败都会无限静默”。受影响文件限定为 `internal/health/push.go:105-140,186-231` 与 `config/store.go:918-945`；实现/测试阶段如需新增文件必须先重新确认范围 | **推荐最小设计（选项 A）**：仅在 `sendOnce=false` 的非法 URL分支增加按 `cfg.Interval`（无效时回退 `config.DefaultPushInterval`）的 timer，并与 `wake/stop` 竞争等待；保持 `active=false`，按现有安全类别 WARN 限频，不记录 URL/token。修正 URL 后 `Wake()` 仍须立即触发重试；不修改健康判定、告警订阅、HTTP 超时、正常请求失败的 interval 语义、Push 完成等待或 shutdown 语义。判别性验收至少包括：1）通过直接 SQLite/内部构造注入启用的非法 URL，确认首次失败后不会忙循环，等待一个配置 interval 后再次尝试，`active` 仍为 false；2）在 interval 等待期间修正 URL 并调用 `Wake()`，确认不必等完整 interval 即发送成功；3）`stop` 可立即结束该等待且 `Stop()` 有界；4）用网络错误/HTTP 错误控制组确认原有 interval 重试仍在，且不会被改成 wake-only；5）日志仅含稳定类别、无完整 URL/token，WARN 频率与既有安全口径一致 | **风险与外部依赖**：风险低，改变仅是非法配置从“唤醒驱动重试”变为“interval 兜底重试”；最坏是脏数据持续按 interval 产生有限 WARN，不应形成紧循环或网络洪水。无需真实云、SMTP、Webhook、浏览器或远端 CI；本地 fake client/transport、受控 SQLite 与时序断言足以证明本条。真实 Uptime Kuma Push 的 DOWN/恢复通知仍是独立未验收边界，不得因本条通过而写成真实通过。用户决策：A（推荐）实施上述最小 timer/wake/stop 等待，补充判别性测试；B 接受现状，仅依赖正常 API 校验与后续配置 `Wake()`，则须明确接受手工 SQLite/历史脏数据可能长期沉默；不推荐扩大为把非法 URL纳入 operational health/告警，或统一重构所有 Push 失败策略。**停止条件**：在 A 的判别性用例、`go test`/race 与 diff-check 未证明前保持 P3-03 未修复；若实现改变健康/告警口径、生产 HTTP 超时、正常请求重试、`active` 语义、完成等待或 shutdown，记录为越界并停止；若出现忙循环、URL/token 泄漏、Wake 不再立即生效或无法保持无在途等待，也停止回到方案审查。 |
| P3-04 | **SSE 断线重连重复回放最多 1000 行**，挤掉真实新日志 | `webui/frontend/src/views/Logs.vue:45-50` 无去重、无 `onerror`；`webui/api/logstream.go:52-88` 每次订阅都回放环形缓冲；`webui/api/sse.go:22` 无 `id:` 字段 | 后端重启/网络切换/休眠后浏览器自动重连即触发。修法：加 `id:` 序号+前端丢弃已见，或回放用独立 event 名 |
| P3-05 | **问题真实存在，当前未修复；继续保留在独立队列 I-07。** 已修复/不存在结论均不成立。本轮仅更新审计记录，未修改源码、测试或依赖，未运行构建、测试或格式化 | **当前基线**：HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树原有修改仅涉及本报告。`webui/api/deps.go:213-220` 注册 `GET /api/settings`；`webui/api/settings.go:15-20` 定义并在 `:33-50` 复制 `tc_access_id`、`tc_access_key`、`ali_access_id`、`ali_access_key` 到响应，再经 `writeJSON` 返回；`webui/api/deps.go:229-240` 的 `writeJSON` 只设置 `Content-Type`，不设置 `Cache-Control`。全仓生产代码中 `no-store` 仅见于 operational health、alerts、config export 等其他端点，未发现为 settings 补头的全局 middleware。现有 `webui/api/settings_alerts_test.go:13-52` 验证键集合/默认值/凭据回显，`redact_test.go:98-110` 验证既有脱敏契约，但均未判定缓存头。Issue7 已将本项留在 I-07，且 2026-09-29 用户裁决确认本轮越界不修复 | **后续设计与决策项（尚未授权实施）**：**A（推荐）**：仅在 `webui/api/settings.go` 的 `handleGetSettings` 入口设置 `w.Header().Set("Cache-Control", "no-store")`，使成功和数据库读取失败响应都保持禁缓存语义；新增/扩展本地 HTTP 测试精确断言该值。保持四个凭据字段的既有回显契约，不修改通用 `writeJSON`，避免扩大所有 JSON API 的缓存策略；不改前端、存储、配置导入导出或认证边界。**B**：接受现状，依赖浏览器/中间件配置或后续统一 HTTP 缓存策略；不建议，因为当前没有全局补头证据，敏感设置响应仍可能被缓存。是否授权单独处理 I-07/P3-05 需用户决策，本记录不擅自裁决。**影响文件**：实施时必改 `webui/api/settings.go`；建议改 `webui/api/settings_alerts_test.go`（或新增同包测试）。明确不改 `webui/api/deps.go` 的通用 `writeJSON`/路由、`Settings.vue`、数据库 schema、Provider、DNS、告警和配置包逻辑 | **判别性验收**：L0 静态确认 `no-store` 只加在 settings handler，未改变通用 JSON 响应策略、凭据字段或导入导出行为；L1 本地 HTTP 用例同时断言 `GET /api/settings` 为 200、JSON `Content-Type` 不变、四个凭据 sentinel 仍按既有契约返回，且 `Cache-Control` **恰为** `no-store`，并保留失败响应也带该头的控制；L2 运行受影响包测试，随后按项目门禁执行 race、vet、build 与 `git diff --check`；L3 可选地用真实本地二进制 `curl -i /api/settings` 核对最终响应头。当前所有实施/门禁命令均未执行，不得提前写成通过 | **风险、外部边界与停止条件**：风险低，改变仅影响浏览器和中间缓存，可能增加设置页重新请求，但不会移除响应中的凭据、清理 Vue 内存/DevTools/服务端日志，也不替代认证或脱敏。无需真实云、DNS、SMTP、Webhook、Uptime Kuma、浏览器或远端 CI/GHCR；本地 handler/HTTP 测试足以证明本项，浏览器 DevTools 只能补充，不能替代判别性测试。不要额外加入 `Pragma`/`Expires`、移除 GET 凭据回显、统一重构所有敏感 API 或引入认证。如果修复扩大到通用 `writeJSON`、凭据产品契约、认证/会话、配置导入导出或其他 I-07 项，停止并重新确认范围；在用户裁决、判别性 HTTP 回归测试及受影响门禁完成前，保持 P3-05/I-07 未修复，不得把本地通过外推为外部链路通过。 |
| P3-06 | CVM 100 条上限按"入站+出站合计"判定，实际配额为**每方向** 100 | `provider/tc_cvm.go:234-241` 把 IngressIPv4+IngressIPv6+EgressIPv4+EgressIPv6 相加与 100 比较；`PlatformAPIDocs/TencentCVMAPIGuide/查询用户安全组配额.md:62` 为 `"SecurityGroupPolicyLimit": 100`；`安全组添加规则.md:18` 明确"一次请求中只能创建单个方向的规则" | 偏保守：出站规则多时会**拒绝合法的入站新增**（硬错误、不可重试）。修法：只统计入站计数。**建议用真实账号确认配额口径后再改**（属人工验收项） |
| P3-07 | **死代码：`retrySync` 零生产调用方** | `syncer/retry.go:26` 仅被 3 个测试文件约 15 处调用；生产走 `retrySyncDetailed`（`syncer/syncer.go:905`） | 被取代后未删除。注意：`Issue6.md` A1 段仍把它写在生产链上，删除时需同步更新该文档 |
| P3-08 | **真实存在，当前未修复**；与 P1-02 合并保留在独立队列 **I-01**。已修复/不存在结论均不成立，不能因顺序启动测试通过而关闭 | 精确链路为 `config/pidfile.go:17` 的 `ReadFile → 解析 PID → processExists → os.WriteFile`；`config/pidfile_unix.go:10` 的 `os.FindProcess + Signal(0)` 只能证明某 PID 存在，不能证明是 FWAlizer。`os.WriteFile` 会覆盖文件，检查与写入之间没有原子互斥、`O_EXCL` 或 `flock`。因此残留 PID `1`、当前 PID 或无关进程复用时会永久拒绝启动；两个进程也可同时通过检查并都启动，后写者覆盖 PID，任一实例退出还可能删除共享文件。已有 `main_test.go:1002` 仅覆盖首实例稳定运行后的顺序拒绝与正常退出，不能证明 PID 复用、SIGKILL 后复用或并发互斥；历史并发实验未命中窗口，不构成安全证据 | **后续设计**：I-01 仅将 PID 判活替换为同一文件描述符上的非阻塞独占 `flock`。`WritePidFile(path) (cleanup func(), err error)` 接口与路径保持不变；先 `os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0600)`，成功加锁后再截断并写入当前 PID。PID 只作诊断，不参与判定；锁竞争时即使内容为空、损坏或 PID 残留也必须拒绝，不能绕过；cleanup 只关闭文件描述符释放锁，不删除锁文件，`Close` 错误按 AGENTS 记录安全 WARN。影响文件限定为 `config/pidfile.go`、`config/pidfile_unix.go`、新增 `config/pidfile_test.go`、调整 `main_test.go`、`README.md` 与本审计条目；`run.go` 原则上不改，`Issue7.md` 不纳入。判别性验收至少覆盖：L0 静态移除 `processExists`、`Signal(0)` 与无锁覆盖写；L1 预置 PID 1、当前 PID、空/损坏内容并分别验证无持锁可启动、真实内核锁持有时必拒且不覆盖；L1 验证 cleanup 后锁文件仍在且可再次取得；L2 真实二进制顺序拒绝、SIGTERM 后复用、SIGKILL 后复用、锁屏障控制的并发启动一成一败；L3 持久命名卷预置 PID 1，`docker kill` 后同卷重启健康；L4 当前 revision 的受影响包 race、全仓 race、vet、build、gofmt、diff-check 及 Linux/macOS 构建。真实云、DNS、SMTP、Webhook、Uptime Kuma、浏览器不属于本项前置 | **风险与外部边界**：`flock` 只对 Linux/macOS 本地文件系统、Docker named volume 与普通本地部署作边界保证，不外推 NFS 或部分网络文件系统；运行中手工删除锁文件会因 inode 变化产生两把锁，文档必须说明不要删除，残留文件也无需清理；旧 PID-only 版本与新 flock 版本混跑不提供可靠兼容，升级前须先停止旧实例。权限范围存在一个实施前决策：A，将已存在锁文件、数据目录和 DB 的权限迁移并入 I-01，范围扩大且与 P3-12 重叠；B，I-01 仅保证新建锁文件 `0600`，已存在文件的 `Chmod`、数据目录 `0700` 和 DB `0600` 留给 P3-12，推荐 B，避免扩大实例互斥任务。未取得 L0～L4 当前 revision 证据前，或任一平台 build tag、锁失败语义、SIGKILL 重启、并发互斥不满足时，停止推进并回到方案/实现核查；不得标记 P3-08/I-01 完成 |
| P3-09 | SQLite 写事务为 deferred，先读后写存在 WAL read→write 升级（`SQLITE_BUSY_SNAPSHOT`，`busy_timeout` 不生效）；且注释理由与驱动实现矛盾 | `config/store.go:32-34` `BeginTx(ctx,nil)`；`:77-78` 注释以"会让只读事务申请写锁"为由拒绝 `_txlock`；**但驱动 `modernc.org/sqlite@v1.54.0/tx.go:23` 为 `if !opts.ReadOnly && c.beginMode != ""`——只读事务根本不加 beginMode，该理由不成立** | 影响仅"偶发 500、重试即成功、不损坏数据"。修法：DSN 加 `_txlock=immediate`（`ReadOnly` 事务不受影响），并同步修正注释与 `config/store_dsn_test.go:142` 的断言 |
| P3-10 | **问题真实存在，当前仍未修复；属于独立队列 I-07，不能标记完成。** 这是五类问题、六处返回值处理缺口，不是“均不可失败”或已被其他修复覆盖 | `config/pidfile.go:33` `os.Remove` 丢弃清理错误；`config/store.go:118/124` 两处失败收尾的 `db.Close()` 未检查；`webui/api/logstream.go:97` `_ = h.Handle(...)`；`webui/api/sync.go:127-130` `json.Marshal` 失败静默跳过事件；`webui/server.go:298` 静态 `/api/health` 的 `w.Write` 忽略 | 与 AGENTS §十一“所有 error 必须处理”冲突。SSE 序列化失败会静默丢事件，是影响最实质的一项；其余缺口通常低概率或当前 writer 实际少失败，但仍须补齐可观测错误处理 |
| P3-11 | `StoreLogWriter.OnEvent` 吞掉写库失败（审计快照） | `webui/api/logwriter.go:83-86` 记 WARN 后 `return nil` → `notifier/bus.go:114` 的"事件处理失败"路径**永不可达** | ✅ 已由 Issue7 Step 4 修复：`AddSyncLog` 错误直接返回给 EventBus；保留原行作为历史红灯，不再列入剩余队列 |
| P3-12 | **真实存在，当前未修复；状态保持“未完成/待独立处理”，对应 I-07。** 数据目录、SQLite DB 及 pidfile 没有应用层权限收敛，不能把既有启动/Docker 证据外推为权限验收通过 | `run.go:41` 使用 `os.MkdirAll(..., 0755)` 且无后续 `Chmod`；`config/pidfile.go:29` 使用 `os.WriteFile(..., 0644)` 且无后续 `Chmod`；`config/store.go:111-128` 直接 `sql.Open`，没有预创建/显式 `Chmod` DB；DB 明文保存云 AK/SK、SMTP 凭据、Webhook URL、Uptime Kuma Push URL。当前没有应用层 `Chmod`、`Umask`、`0600`、`0700` 权限原语 | 同机其他非特权用户可能遍历数据目录或读取明文敏感配置。**既有用户决策仍固定为数据目录 `0700`、数据库和 pidfile/lock 文件 `0600`**；对已存在目录/文件必须显式 `Chmod`，`MkdirAll`/`WriteFile` 的请求模式不能替代迁移收敛。本轮只更新审计记录，未修改源码、测试或其他文档，未构建、未运行测试或格式化 |
| P3-13 | **问题真实存在，当前未修复；继续归入独立队列 I-07。** 本轮仅完成只读核验与本审计条目更新，未修改源码、测试或其他文件，未构建、未运行测试或格式化；不可标记完成。 | **当前基线与精确缺陷链：** HEAD 为 `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树已有唯一 tracked 修改为本报告。`app/logutil.go:28-37` 的 `MultiHandler.Handle` 按顺序处理子 Handler，但 `:31-33` 在第一个启用 Handler 返回错误时立即 `return`，不会继续分发。生产组装顺序由 `run.go:99-100` 与 `app/logutil.go:85-88` 固定为 `stdout TextHandler → WebUI LogBroadcaster`；因此 stdout 写入失败（例如管道断裂）时，WebUI broadcaster、日志环形缓冲与 SSE 订阅者不会收到同一条日志，形成静默丢行。当前 `app/logutil_test.go` 只有日志级别和初始化测试，没有错误注入、继续分发、多错误聚合或 Disabled Handler 语义测试。已修复/不存在核对未发现收口证据；`Issue7.md:584-591` 明确本项是越界的正交问题，AGENTS §十一的 error 处理要求与 MultiHandler 文件归属也不构成已修复证据。另需注意 Go `slog.Logger` 会丢弃底层 Handler 返回的错误，故核心验收必须直接调用 `MultiHandler.Handle`，不能只通过 `slog.Logger` 观察返回值。 | **后续设计与用户决策（尚未授权实施）：** 影响生产代码仅限 `app/logutil.go` 的 `MultiHandler.Handle`，测试限于 `app/logutil_test.go`；`run.go` 的组装顺序、`webui/api/logstream.go`、SSE 协议、前端、日志格式和同步业务均明确不改。**A（推荐）**：继续遍历全部启用 Handler，收集错误；零错误返回 nil，单个错误原样返回，多个错误用标准库 `errors.Join` 返回，以保留单错误身份并让 `errors.Is/errors.As` 识别全部错误；保持现有顺序、`Enabled` 过滤、同步串行、`WithAttrs`/`WithGroup` 行为，不重试、不异步、不在错误路径再次写日志，避免 stdout 失败时递归。**B**：继续调用后续 Handler，但多个错误只返回第一个；实现更小，但丢失后续错误信息和 `errors.Is` 可观测性，不推荐。**C**：继续分发并吞掉/另行记录 Handler 错误；会削弱现有 error 处理契约，且可能形成日志递归，不推荐。当前仅记录 A 为推荐，不擅自替用户裁决或实施。 | **判别性验收、风险、外部边界与停止条件：** L0 静态确认不再在单个 Handler 出错后提前返回，所有启用 Handler 均尝试，多个错误经 `errors.Join` 聚合，未新增递归日志，顺序仍为 stdout 后 WebUI。L1 直接调用 `MultiHandler.Handle` 至少覆盖：前一 Handler 返回 sentinel 错误而后一 Handler 仍被调用并收到原始 Record；两个 Handler 出错且返回值可分别 `errors.Is`；中间 Handler 出错不阻断后续成功 Handler；Disabled Handler 的 `Handle` 不调用且不贡献错误；全成功及全 Disabled 控制组返回 nil。修复授权后再执行 `go test ./app`、`go test ./app -race`，随后按项目门禁执行全量 race、vet、build 与 `git diff --check`；本轮这些命令均未执行，不能预先写成通过。L3 可选用受控 stdout 管道断裂补充进程级证明，但不是必要前置。风险低：`errors.Join` 可能改变多错误 `Error()` 文本；不保留单错误原值会改变兼容性，故 A 原样返回单错误；继续串行调用不解决 Handler 阻塞，也不处理 panic。无需真实腾讯云、阿里云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR，本地 fake `slog.Handler` 已足以证明根因修复；真实 stdout 管道仅是补充边界。若实现改变 Handler 顺序、SSE/前端协议、日志格式、同步业务，加入重试/异步队列，修改 `slog` 递归错误路径，或把本地通过外推为外部链路通过，应立即停止并回到方案审查；在用户决策、判别性测试、受影响门禁和文档闭环完成前，保持 P3-13/I-07 未修复。 |
| P3-14 | **🔵 问题真实存在，当前未修复，继续归入 I-07。** 本轮仅完成只读核验与本审计条目更新，未修改源码、测试或其他文件，未构建、未运行测试或格式化。 | **当前基线**：`main` / HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；本轮前工作树已有修改仅涉及本报告。`webui/api/sync.go:24` 的 `/api/sync/trigger`、`:76` 的 `/api/sync/dryrun` 在 `Syncer == nil` 时返回 HTTP 400；`:42、:61` 的 `/api/sync/pause` 与 `/api/sync/resume` 同样如此；`:94` 的 `/api/sync/events` 在 `EventBus == nil` 时返回 HTTP 400；`webui/api/logstream.go:131` 的 `/api/logs/stream` 在 `LogBroadcaster == nil` 时返回 HTTP 400。这些分支表示服务端依赖未接线或尚未就绪，不是客户端请求格式错误。对照 `webui/api/operational.go:46-52`，`Health == nil` 已按同类不可用语义返回 503，说明仓库已有正确范式。`/api/sync/status` 的 `Syncer == nil` 分支在 `webui/api/sync.go:16` 返回 200 且 `running:false`，并由 `webui/server_test.go:1119-1151` 作为未接入 Syncer 的状态查询场景固定覆盖；本条暂不扩大到该观察接口。现有 `webui/api/sync_test.go`、`webui/api/sse_test.go` 与相关日志流测试没有对上述 nil 依赖精确断言 503。前端 `webui/frontend/src/views/Logs.vue:45-49` 只创建 `EventSource` 并处理消息，没有应用层不可用提示或恢复逻辑；浏览器对该具体非 2xx 响应的重连表现本轮未人工验证。 | **后续最小设计与用户决策（尚未授权实施）**：A（推荐）仅把五个 handler 中六个未就绪依赖分支（trigger、dryrun、pause、resume、events、logs）传给 `writeError` 的状态码从 `http.StatusBadRequest` 改为 `http.StatusServiceUnavailable`，保留现有错误文案、JSON 形状和响应头；补充判别性本地 HTTP 测试。`/api/sync/status` 继续保持 200，Syncer 已接线但暂停的操作仍为 409，Dry Run 冲突仍为 409，SSE 能力不支持仍为 500，正常路径状态码不变。B：把 `/api/sync/status` 的 nil 分支也改为 503；不推荐，会改变已有状态查询兼容语义，应另立状态接口决策。C：同时补前端 `Logs.vue` 的错误提示或 EventSource 重连；可作为独立 UI 议题，不纳入 P3-14 后端最小修复。影响文件限定为 `webui/api/sync.go`、`webui/api/logstream.go` 及 `webui/api/sync_test.go`、`webui/api/sse_test.go`/日志流测试；不改 `writeError`、`webui/api/deps.go`、`run.go`、`operational.go`、前端、Provider、SSE 生命周期或重试策略。是否授权 A 需用户决策，本条不擅自裁决。 | **判别性验收、风险、外部边界与停止条件**：L0 静态确认上述六个 nil 分支均为 503，`/api/sync/status` 仍为 200，其他 409/500/200 分支未改变。L1 使用 `Deps{Syncer:nil}` 分别请求 `POST /api/sync/trigger`、`/api/sync/dryrun`、`/api/sync/pause`、`/api/sync/resume`，使用 `Deps{EventBus:nil}` 请求 `GET /api/sync/events`，使用 `Deps{LogBroadcaster:nil}` 请求 `GET /api/logs/stream`；每项精确断言 503、`application/json; charset=utf-8` 与 `{"error":"..."}` 形状，并证明不访问 Store/协调器、不调用 `probeSSE`、不建立订阅。保留正常 Syncer 触发 202、暂停触发 409、正常 pause/resume 200、正常 SSE 200、SSE 能力缺失 500、nil Syncer 的 status 200 等负向控制。L2 在获得授权后执行受影响包测试，再按项目门禁执行全量 race、vet、build 与 `git diff --check`；当前这些命令均未执行，不能预先写成通过，也不得以单次绿色外推为稳定绿色。L3 可选地用本地真实二进制和 `curl -i`核对最终状态码与 JSON 头部。风险低，主要是让调用方正确识别服务端不可用，前端仍可能因缺少应用层 `onerror`/恢复 UI 而只显示空日志；浏览器人工回归仅补充用户表现，不是后端关闭必要条件。该项不依赖真实腾讯云、阿里云、DNS、SMTP、Webhook、Uptime Kuma、Docker 或远端 CI/GHCR。未完成上述 nil 依赖判别性测试和受影响门禁前保持 P3-14/I-07 未修复；若扩大到 `/api/sync/status`、通用错误封装、运行时接线、前端重连、`Retry-After` 或 SSE 生命周期，或把浏览器未验证行为写成确定结论，应立即停止并重新审查范围。 |
| P3-15 | 被丢弃的告警事件**每条约一条 WARN**，故障期日志洪水 | `notifier/inflight.go:62-67` `logDropped` 对每个被丢弃事件都记 WARN；`notifier/bus.go:113` 每事件每订阅者一个 goroutine | 500 个 DNS 失败事件 → 约 496 条 WARN。量级不大且信息有用，属可聚合项 |
| P3-16 | 前端 UI/状态偏离（AGENTS §11） | `RunTest.vue:32` 缺 `size="large"`（唯一漏网，**属明确漏改**）；`App.vue:10/66-69` 侧边栏高亮不随路由（刷新/深链后停在"仪表盘"）；`Targets.vue:209-212`/`Rules.vue:165` 保存按钮无 in-flight 守卫（双击产生重复行）；`Settings.vue:127-141` 把不可见的 `theme` 当隐藏字段回传（与侧边栏主题开关**双写**，导致主题静默回退）；`useScannedResources.ts:36-41`+`Settings.vue:98-105` 清空扫描失败仍提示"已清空"；`Settings.vue:14` 前端正则比后端更严（合法 `1h30m` 被拦） | 均为可静态判定；`theme` 双写与"清空误报成功"影响用户可观察状态 |
| P3-17 | 陈旧 "version 2" 表述残留约 12 处 | `config/runtime_test.go:27,66`、`webui/api/testenv_test.go:216`、`import_runtime_test.go:82`、`import_test.go:11,20,111`、`export_test.go:461`、`redact_test.go:111,116`、`main_test.go:732,735` | Build7 Step 7 声称已收口 version 2 滞后注释，**测试文件未被覆盖**；`import_test.go:111` 甚至用 map key `"version 2"` 承载 `{"version":3,...}` |
| P3-18 | 文档漂移（非强制文档） | **Step 0 部分收口：** `Design5.md` 当前配置包已改为 version 3，Build7 已完成状态与 `ProdTestList.md` 范围矛盾已收口；`Build6.md`/`Issue5.md` 历史措辞、`Issue6.md` 基线对账与测试内 version 2 注释等仍是剩余文档清理 | 与 P1-01 直接冲突的部分已修；其余不是 Issue7 Step 1 前置 |
| P3-19 | 邮件 AUTH 失败保留 SMTP 诊断文本（含 535 回显） | `notifier/email.go:219-221` `%w` 包装；`webui/api/test_email.go:78-82` 回给浏览器；`notifier/email_test.go:293-295` **显式断言**保留诊断 | 理论风险：若服务器回显 AUTH 载荷可间接泄露 base64 凭据（**未验证**；标准 SMTP 不会这样做）。属可调试性取舍 |
| P3-20 | 邮件头/正文直发原始 UTF-8（无 RFC 2047 头部编码、无 CTE 声明） | `notifier/email.go:243-244`；`config/validate.go:307-316` 已排除头部注入 | 默认配置即中文主题/正文。多数现代 MTA 可正常投递；**真实表现必须由真实收件箱验证**（未执行） |
| P3-21 | Webhook 响应体未 drain（连接不可复用）；Push 响应体解析无字节上限 | `notifier/webhook.go:120-124` 只 Close 未 drain；`internal/health/push.go:247` `json.Decoder` 无 `io.LimitReader`（仅 10s `client.Timeout` 兜底） | 每次告警多一次 TCP/TLS 握手；Push 内存峰值不受字节数约束 |
| P3-22 | Dry Run 对每条规则各发一次 `GetRules` 并 sleep 一个厂商限速间隔（审计快照） | `syncer/syncer.go:604-640` 内层 for rule 里 `p.GetRules()` + `time.Sleep(rateLimitInterval(...))`，注释写"与 syncAll 一致"（实际 `syncAll` 每 provider 只 sleep 一次，`:805-817`） | ✅ 已由 Issue7 Step 4 修复：Dry Run 按目标取一次完整快照并在内存中规划，目标间保留限速；原 20 条规则约 100 秒/20 次 Describe 的风险仅作历史证据 |
| P3-23 | **核心问题真实存在，当前未修复；审计条目的两条附加依据需要降级或删除。** `true → true` 分支无条件 `ticker.Reset(latest.Config.Interval)`，因此不影响调度的主题、告警或其他配置保存也会重新计时；保存频率持续高于 interval 时，周期同步可能被无限推迟，并进一步触发运行健康的“距最近完成时间超过 `interval + health_timeout`”判据。`false → false` 对已停止 ticker 调用 `Reset` **不是 Go API 误用**；这是 Go 支持的重新激活方式，但暂停期间没有必要做此操作。按项目 `go.mod` 的 Go 1.25 合同，`Stop`/`Reset` 后的同步 ticker channel 语义也不支持直接断言“旧 tick 必然残留”，因此 stale-tick 子结论目前证据不足。状态：**部分成立，当前不能关闭**。 | **真实缺陷证据：** `syncer/syncer.go:295-297` 的 `true → true` 无条件 Reset；`ApplyState` 每次状态发布都会通知 Run（`:133-147`），普通目标、规则、settings、alerts、pause/resume 写入口经协调器 commit 后最终调用 `Syncer.ApplyState`（`webui/api/coordinator.go:71-87`、`webui/api/deps.go:130-161`）。当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`、`main == origin/main`；工作树原有未提交修改仅为本报告，源码/测试未由本轮修改，本轮未运行构建、测试或格式化。`go.mod:3` 为 Go 1.25.0；停止后可 Reset 见 [Go time.Ticker 文档](https://pkg.go.dev/time#Ticker.Stop)，Go 1.23 起同步 timer/ticker channel 的 stale value 语义见 [Go 1.23 release notes](https://go.dev/doc/go1.23)。现有 `syncer/state_test.go:168-191` 只证明 interval 改变后会按新 interval 触发，不能证明同 interval 更新不会重新计时。 | **后续最小设计（推荐选项 A）：** 只改 `syncer/syncer.go` 的局部调度状态，创建 ticker 时保存 `tickerInterval := state.Config.Interval`；`false → true` 始终按最新 interval Reset、更新 `tickerInterval` 并保留恢复后的立即同步；`true → true` 仅当 `latest.Config.Interval != tickerInterval` 时 Reset 并更新记录；`true → false` Stop；`false → false` 不操作 ticker，暂停期间的最新 interval 由恢复时使用。不要加入 stale-tick drain、`time.Timer` 重写或时钟抽象。测试重点为 `syncer/state_test.go`：同 interval 的连续 ApplyState 不重新计时；高频保存非调度配置时周期同步仍发生；interval 实际变化仍重新计时；暂停期间修改 interval 后恢复使用最新值；保留 P3-24 的 pause→resume 通知合并控制。受影响范围限定为 `syncer/syncer.go` 与调度测试；不得修改 Provider、RuntimeManager、ConfigCoordinator、OperationalHealth、ApplyState 通知合并机制或外部链路。**选项 B**：额外承诺 `GODEBUG=asynctimerchan=1` 旧 timer channel 兼容，需要定义支持范围、补兼容测试并重新设计 Stop/Reset 后处理，超出本项最小修复范围，暂不推荐。 | **判别性验收与停止条件：** 静态确认 `true → true` 不再无条件 Reset、`false → false` 不再 Reset、`false → true` 仍 Reset，且没有新增 stale-tick drain；保留 P3-24 独立未处理。定向用例至少覆盖：①同 interval 更新后下一轮按原 ticker 剩余时间到期，而非从保存时重新等待完整 interval；②保存频率高于 interval 时仍能发生 ticker 驱动同步；③interval 改变时按新值重新计时；④暂停期间改 interval、恢复后立即同步且后续使用最新值；⑤旧语义下相应测试能判别失败。修复授权后再执行 `go test ./syncer -race -count=1`、`go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 及项目要求的多轮门禁；本轮均未执行，不能预先写成通过。若修改生产范围超出 ticker 局部状态、引入未裁决的旧 timer 兼容承诺、把 P3-24 合并处理、误删恢复立即轮、比较初始而非当前实际 interval，或把单次绿色外推为稳定绿色，应立即停止并回到方案审查。**外部边界：** 本项仅依赖本地 Go 1.25、Syncer、ticker 和测试 Provider；不依赖真实腾讯云/阿里云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR。本地通过只能证明当前 Go 合同下的调度行为，不能外推真实外部链路已验收。 |
| P3-24 | **真实存在，当前未修复；继续保留在独立队列 I-06。** 本轮仅补充审计记录，未修改源码或测试，未构建、未运行测试。轮内 `pause → resume` 的控制通知可被容量为 1 的 `controlCh` 合并，导致 Run 只观察到 `true → true`，恢复不触发按合同要求的立即同步一轮。 | **当前基线与缺陷证据：** HEAD 为 `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树既有修改仅为本报告。`syncer/syncer.go:133-147` 的 `ApplyState` 发布新状态后向容量为 1 的 `controlCh` 非阻塞投递，已有通知时合并；`:217-221` 的 Run 使用本地循环相位 `enabled` 形成 `wasEnabled`；`:270-305` 只在 `false → true` 分支立即调用 `syncAll()`，`:295-297` 的 `true → true` 只 Reset ticker。可达交错为：当前相位为 `true` 时，在 Run 消费前连续 `ApplyState(false)`、`ApplyState(true)`；RuntimeManager 最终只读到 `true`，于是得到 `true → true`，恢复轮被静默跳过。暂停/恢复入口分别为 `webui/api/sync.go:35-52`、`:55-72`，均经 `webui/api/deps.go:122-161` 的协调器发布；协调器只串行化单次提交，不能保证 Run 在两次提交之间消费第一条通知。Build6 §12.5 要求 `false → true` 立即同步；Issue6 A7 修复的是已发布镜像先推进导致恢复边沿丢失的另一种交错，不覆盖本项。现有 `syncer/state_test.go:147-166` 只测初始 `false → true`，`:168-191` 只测正常 `true → true`，`syncer/syncer_test.go:197-237` 使用固定等待，均不能判别本项。 | **后续设计与待决策选项：** 需要在不恢复完整中间状态队列的前提下，保留最终状态语义并记住被合并的恢复边沿。**A（推荐）**：在 `Syncer` 内以 `s.mu` 保护 `resumeGeneration` 与 `handledResumeGeneration`；`ApplyState` 在线性化发布 `next` 时仅对真实 `false → true` 递增 generation，Run 消费控制通知时同锁取得最新 `RuntimeState`、读取并标记已观察 generation。最终 `Enabled=false` 时绝不启动同步；最终为 `true` 且存在未处理恢复 generation 时补发恰好一轮立即同步；普通 `true → true` 仍只按既有 ticker 规则处理，保留 `controlCh` 容量 1 合并语义。**B**：把控制通道改为携带每次状态或边沿的显式事件并逐条排队；语义直观但可能执行已过期的中间状态、扩大队列/生命周期范围，当前不推荐。**C**：仅增加 `pendingResume bool`；改动最小但在多个连续恢复边沿、消费与发布并发时更难证明不丢失或重复，除非补足线性化合同，当前不推荐。上述为后续设计，不是本轮实施授权；需用户在 A/B/C 中裁决后再改代码。 | **影响文件、判别性验收、风险与停止条件：** 生产影响应限定为 `syncer/syncer.go` 的状态字段、`ApplyState`、Run 控制消费与必要中文注释；测试影响为 `syncer/state_test.go`，必要时新建 `syncer/control_transition_test.go`。原则上不改 `RuntimeManager`、`ConfigCoordinator`、`webui/api/sync.go`、Provider、DNS、告警、OperationalHealth、Issue7 目标级状态机或外部 API。必须补确定性用例：①当前轮被 Provider 阻塞时连续 `true → false → true`，放行后断言首轮之外恰好再执行一轮立即同步；②连续 `true → false → true → false` 后最终暂停，断言不启动第二轮；③普通 `true → true` 不触发恢复轮；④正常单次 `false → true` 仍只触发一轮；⑤既有 queued trigger/paused ticker 回归继续成立。用 Provider 阻塞与 release channel 控制时序，不以固定 `Sleep` 猜竞态。主要风险是恢复 generation 被提前清除、最终暂停仍误启动、ticker/trigger 与恢复边沿重复跑两轮，或为保留中间状态扩大为无界队列；这些任一情况均应停止并回到方案审查。该项只依赖本地 Syncer、容量为 1 的控制 channel、可控 Provider 与确定性屏障，不依赖真实云、DNS、SMTP、Webhook、Uptime Kuma、浏览器、Docker 或远端 CI/GHCR。取得用户裁决、判别性回归测试、`go test ./syncer -race -count=1`、全量多轮 race、`go vet ./...`、`go build ./...`、gofmt/diff-check 及文档闭环前，保持 P3-24/I-06 未修复；不得把单次本地绿色或 P3-23 的修复外推为本项关闭。 |
| P3-25（同步路径） | ECS 同步 `NextToken` 分页缺"token 未推进即退出"守卫（审计快照） | `provider/ali_ecs.go:96-102` 仅判空不判重复（审计快照行号） | ✅ 已由 Issue7 Step 1 修复：重复/未推进 token 返回 `snapshot_incomplete`，目标失败且本 attempt 零删除；不再列入待修复 |
| P3-25（资源扫描路径） | ECS `ScanResources` 分页仍缺"token 未推进即失败"守卫 | `provider/scan.go` 的 `scanAliECS` 仍只在 token 为空时结束，重复/未推进 token 会持续追加并循环 | 🔵 **未修复**：需单独给扫描路径加已见 token/推进检查，并验证异常时不写入半截扫描结果；不能把同步路径的修复外推到扫描路径 |

> 说明：第 9 节的决策表使用 #1..#7 编号（你实际决策的 7 项）；本 P3 表使用 P3-nn 编号，两套编号相互独立。P3-08 已合并原先拆分的两类 pidfile 失效（TOCTOU 漏判 / PID 复用误判），实施时由 flock 一次解决。

**P3-01 独立追踪与后续设计：**

- **状态与精确证据：** P3-01 确认真实存在，当前未修复。本轮仅更新审计记录，未修改代码、测试或格式化，也未运行构建与测试；当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`，工作树原有未提交修改仅涉及本报告。`dns/circuitbreaker.go:11` 的 `map[string]int` 保存所有曾被访问过的域名；`RecordSuccess`（`:59-67`）只把计数写成 `0`，没有删除 map 条目；`RecordFailure` 会为新域名创建条目，源码没有 `delete`、淘汰、裁剪或过期机制。`Clone`（`:37-50`）完整复制 map，`syncer/state.go:74-82` 的 `BreakerPreserve` 在普通运行时配置发布时调用该 Clone；只有完整配置导入的 `BreakerReset` 才会新建空 breaker。同步解析路径在 `syncer/target.go:362-381` 对解析成功/失败持续写入 breaker。

- **实际影响与残留类型：** map 的增长量受进程生命周期内曾参与解析的不同域名数约束，但没有源码级上界。成功解析的域名会留下永久 `0` 条目；从规则配置移除或改名的域名，其仍大于 `0` 的失败计数也会永久保留，并在普通 `BreakerPreserve` 发布中继续被复制。短期不改变解析、熔断阈值或同步结果，主要影响是长期运行且反复修改域名配置时的无界内存增长与不必要的状态复制。

- **推荐的最小后续设计（选项 A）：** 同时收敛两类残留。① 将 `RecordSuccess` 改为 `delete(cb.failCount, domain)`；不存在的 key 在 `IsOpen` 中读取零值，因此不改变熔断语义，下一次失败仍从 `1` 开始。② 增加按活动域名过滤的 Clone 入口，例如 `CloneForDomains(domains []string) *CircuitBreaker`；`BuildRuntimeState` 的 `BreakerPreserve` 从 `published.DomainRules` 构造当前配置域名集合，只复制仍在活动集合中的、且失败计数大于 `0` 的条目。完整配置导入继续走现有 `BreakerReset`，创建空 breaker。不得只做其中一项：只丢弃零计数会让已删除域名的正数计数残留，只按活动域名裁剪会让成功后的零计数条目在下一次配置发布前继续堆积。不得借本项引入 TTL、定时清理协程、LRU、SQLite 持久化、半开探测语义变化或域名大小写规范化。

- **影响文件与明确排除：** 生产文件限定为 `dns/circuitbreaker.go` 与 `syncer/state.go`；判别性测试限定为 `dns/circuitbreaker_test.go` 与 `syncer/state_test.go`。必要时只在这两个测试包内补充夹具或辅助断言。不得顺带修改 `dns` breaker 的 `IsOpen` 控制流、`syncer` 的运行时发布协议、配置 schema、Provider、解析器、半开探测、外部云 API、告警/健康语义或其他 P3 项。

- **判别性验收：** 修复授权后至少补齐：
  1. 成功记录后，明确断言 map 中不存在该域名，而不是只断言计数为 `0`。
  2. `CloneForDomains` 保留活动域名的正数失败计数。
  3. `CloneForDomains` 丢弃已移除域名的正数失败计数。
  4. `CloneForDomains` 不复制活动域名的零计数条目。
  5. 普通配置变更仍保留活动域名的熔断进度。
  6. 完整配置导入仍清空全部计数。
  7. 空规则配置发布后 breaker map 为空。
  8. 保留并通过并发读写的 `-race` 控制，证明锁语义未被破坏。

  受影响包用例通过后，按项目门禁执行 `go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 及必要的前端/文档门禁；上述命令本轮均未执行，不能预先写成通过，也不能把单次绿色外推为稳定绿色。

- **风险、外部边界与停止条件：** 主要风险是误删仍活动域名的失败进度、把零值删除错误地解释为状态变化，或把活动域名集合构造错而导致普通配置变更丢失熔断进度。删除成功后的下一次失败必须仍从 `1` 计数；已删除域名不得通过 Clone 复活；完整导入清空计数的既有语义必须保持。本项只依赖本地 DNS/breaker/state 源码与单测、race、vet、build；不依赖真实腾讯云/阿里云、浏览器、SMTP、Webhook、Uptime Kuma、Docker 或远端 CI/GHCR，本地通过也不能外推这些外部链路已验收。

  在用户授权前保持只读。若实现扩大到 TTL/LRU/后台清理、状态持久化、域名规范化、改变半开探测或 `IsOpen` 控制流、修改配置导入语义、改动 Provider/真实外部链路，或无法用判别性测试证明“活动正数计数保留、已删除和零计数条目收敛”，立即停止并回到方案审查；在受影响测试、全量 race/vet/build、diff-check 与文档闭环完成前，保持 **P3-01 未修复**。

- **用户决策选项：**

  | 选项 | 方案 | 影响 |
  |---|---|---|
  | **A（推荐）** | `RecordSuccess` 删除成功域名条目，并在普通 `BreakerPreserve` 中按当前活动域名集合裁剪且只复制正数失败计数；完整导入继续 reset | 同时解决零计数与已删除正数计数两类残留，改动集中、熔断语义不变；需要补齐上述判别性测试和门禁 |
  | B | 只在 Clone 时丢弃计数为 `0` 的条目 | 能清理成功域名的零计数残留，但已删除域名的正数失败计数仍永久保留；不足以关闭 P3-01，不推荐 |
  | C | 只按当前配置域名集合裁剪 Clone | 能清理已删除域名，但成功域名的零计数条目会持续存在至下一次配置发布；仍缺少即时收敛，不推荐 |
  | D | 接受现状，不实施淘汰 | 不改代码，但继续承担长期运行和反复改名/删域名造成的无界 map 增长；与既有“加淘汰策略”决策不一致，不推荐 |

  当前推荐 A；本次仅更新审计记录，不擅自实施代码。

**P3-10 独立追踪与后续设计：**

- **状态与精确证据：** P3-10 问题真实存在，当前仍未修复，继续归入独立队列 I-07；不能因相关路径通常不报错、已有成功路径测试，或 P3-11 已由 Issue7 Step 4 修复而关闭本项。当前基线为 HEAD `34aa9b859c38904e701a19d672dcc4f65435b31c`，`main == origin/main`；工作树唯一 tracked 修改是本审计报告，本轮未编辑代码或测试，未构建、未运行测试、未格式化。六处缺口如下：
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

**P3-12 独立追踪与后续设计：**

- **精确证据与影响：** `run.go:41` 的 `os.MkdirAll(deploy.DataDir, 0755)` 对新目录只提出宽权限请求，且不会收紧已存在的 `0755/0777` 目录；`config/pidfile.go:29` 的 `os.WriteFile(path, data, 0644)` 同样不会改变既有 pidfile 权限。`config/store.go:111-128` 直接打开 SQLite，未预创建并设为 `0600`，也未处理已有数据库；WAL 模式下还必须核实 `config.db-wal` 与 `config.db-shm` 辅助文件的最终权限。数据库中的 `settings`、`alert_email.password` 等字段保存云 Access Key/Secret、SMTP 用户名/密码、Webhook URL 与 Uptime Kuma Push URL，故该问题不是纯形式规范，而是同机多用户的敏感信息暴露风险。当前没有源码级 `Chmod`、`Umask`、`0600` 或 `0700` 机制。

- **当前验证边界：** 本次静态核验基线为 `HEAD == origin/main == 34aa9b859c38904e701a19d672dcc4f65435b31c`；工作树唯一未提交修改为本审计报告，源码没有本轮改动，本轮没有构建、测试或格式化。现有 `main_test.go:317-339` 只证明数据库存在，`:232-244` 只证明正常退出清理 pidfile，`:994-1059` 只证明按当前 PID 判活的顺序启动拒绝；它们均不能证明新建/已有目录、DB、WAL/SHM 或 pidfile 的精确权限。因此 P3-12 仍不能关闭，I-07 仍不能勾选完成。

- **推荐的最小实施设计（仅供后续授权）：**
  1. `run.go`：使用 `os.MkdirAll(deploy.DataDir, 0700)` 后，无论目录新建还是已存在都显式执行 `os.Chmod(deploy.DataDir, 0700)`；任一权限收敛失败即启动失败，不以 WARN 后继续打开数据库或监听端口。不要递归修改数据目录中的其他用户文件。
  2. `config/store.go`：在 `sql.Open` 前以 `O_CREATE|O_RDWR`、`0600` 预创建数据库并对已有 DB 显式 `Chmod(path, 0600)`；打开/初始化前收紧已存在的 `config.db-wal`、`config.db-shm`，初始化后再次检查已出现的辅助文件，不能把 modernc SQLite 对权限的实现推断当作验收证据。
  3. `config/pidfile.go`：新建 pidfile 请求 `0600`，对已有 pidfile 显式 `Chmod(path, 0600)`，保持当前 `ReadFile → processExists → WriteFile` 判活逻辑不变。未来 I-01 的 flock 锁文件复用同一 `0600` 权限原语，但 flock、PID 复用、TOCTOU 和并发启动不并入本项。

- **仍需用户确认的实现选项（不在本轮擅自裁决）：**
  - **A（推荐）**：目录、DB、WAL/SHM、pidfile 的 `Chmod` 失败均 fail-closed，启动失败并保留可诊断错误；这符合敏感明文配置的保护目标，也避免把“权限修复失败”伪装成成功启动。
  - **B**：`Chmod` 失败只记 WARN 并继续启动；兼容受限文件系统或 root-owned Docker 旧卷，但会允许不满足 `0700/0600` 的不安全状态继续运行，不建议采用。
  - **C**：把权限迁移与 I-01 的 flock 一并实施；可减少一次生命周期变更，但会扩大 I-01 范围并混淆 P3-12 与互斥语义，当前不建议。
  - 已固定的目标权限 `0700/0600` 不属于待重新裁决项；上述选项只涉及失败语义与 I-01 的边界。

- **受影响文件与明确排除：** 预期生产改动限定为 `run.go`、`config/store.go`、`config/pidfile.go`；建议新增 `config/permissions_test.go`（或同等独立权限测试文件），并视 Docker 旧卷说明需要更新 `README.md`。不修改 `config/pidfile_unix.go` 的 PID 判活、I-01 的 flock 设计、SQLite schema/业务事务、Provider、同步/告警/HTTP 语义或其他 P3 条目；本轮实际只编辑本报告。

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
| **ticker/timer** | **有界，均正确 Stop**；P3-23 的核心问题是同步 ticker 在 `true → true` 状态更新中被无条件 Reset，另有暂停态无必要 Reset；这不是 Stop/Reset API 误用结论 | `syncer.go:193` ticker `defer Stop`；`supervisor.go:86` `defer ticker.Stop`；`push.go:147` timer 三分支都 `Stop()`；`run.go:62/244` 的 `time.After` 为短命启动/收尾等待。Go 1.25 默认语义下 stale tick 残留尚未被本项目证据证明 |
| **channel/订阅** | **有界，无泄漏** | `triggerCh`/`controlCh` cap=1 且 `ApplyState` 用 `select/default` 合并（合并导致 P3-24）；`EventBus.chanSubs` 取消时**同锁内 delete** 且**永不 close**（`bus.go:78-80` 有 send-on-closed 的 panic 论证）；`LogBroadcaster.subs` 取消时 close+delete；`AlertManager.Apply` 先按**旧集合**退订再按新集合订阅 → `alertset_test.go:159-172` 重复 Apply 5 次验证无增长 |
| **HTTP body/连接** | **关闭完整**，两处可优化 | 全部 outbound 均 `defer Body.Close()`（`webhook.go:124`、`push.go:236`）；`webui/api/decode.go:78` 的唯一 `io.ReadAll` 已被 `http.MaxBytesReader` 界定（1 MiB/10 MiB）；未 drain 见 P3-21 |
| **SQLite rows/事务** | **完全干净** | 6 处 `rows` 全部 `defer Close()`；`ensureColumnTx` 三条错误分支显式 Close；所有事务 `committed` 标志 + defer Rollback（仅忽略 `sql.ErrTxDone`）；`config/store_error_test.go:23` 证明 panic 也回滚；commit 失败**不 apply 不发布** |
| **SSE** | **有界且退出闭合** | 两条流均 `defer unsubscribe()`，均 select `ShutdownCh`；`probeSSE` 在写响应头**之前**；每次写出独立 5s deadline（`webui/api/sse.go:10`）；订阅 channel 容量固定 `logRingSize+256`；**不存在"重连新开而不关旧"的累积**（`EventSource` 单实例） |
| **日志与集合** | **全部有界，两处例外** | `sync_logs` 裁剪至 1000（`config/store.go:984`）；`GetSyncLogs(100)`；`LogBroadcaster.ring` 固定 `[1000]string`；前端 `logLines` 上限 1000；`scanned_resources` 按 cloud_type+region **覆盖式**。例外：熔断器域名 map（P3-01）、ECS **资源扫描路径** `NextToken` 循环（P3-25）；同步路径守卫已修复 |
| **配置快照** | **有界且不可变** | 每次配置变更创建一个 `RuntimeState`，旧状态与旧 `ClientPool` 被丢弃；`rc.DeepCopy()` + `DeepCopyRules` 确保发布后不可变；旧 SDK client 的空闲连接由 transport `IdleConnTimeout` 回收（**未显式 Close**，属可接受） |
| **前端响应式状态** | **有界** | `logLines` 1000 封顶；dry-run 结果每次覆盖不追加；扫描缓存按 cloud_type 覆盖；无 `localStorage`/`sessionStorage`/`cookie`/`console.*` |
| **Docker/进程资源** | **已验证正常，但含 P1-02** | 镜像非 root（`uid=1000(appuser)`）、`/app/data` 属主正确、`wget` 存在（BusyBox `/usr/bin/wget`）、`HEALTHCHECK` 指向静态 `/api/health`（30s/3s/10s/3）、容器 `healthy`、`docker stop` 0.125s 且 `ExitCode=0`、无 OOM。**但存在 P1-02 的崩溃循环** |

**结论**：本项目的资源管理**明显优于**同规模项目。当前仍需关注的无界结构是熔断器域名 map（P3-01，已决定修）与 ECS 资源扫描路径分页循环（P3-25）；同步路径的同类问题已由 `snapshot_incomplete` 守卫修复，其余均已证明有界。

---

## 4. 冗余、死代码和兼容残留

### ✅ 确认可删除（已双重核实：静态引用 + 测试引用 + 路由/embed/构建 + 文档）

| 目标 | 位置 | 引用检索结果 | 删除影响 |
|---|---|---|---|
| `Config` + `LoadConfig` + `ToConfig` | `config/config.go:151`、`config/store.go:1025`、`config/runtime.go:116` | 生产 **0**；仅 5 个测试文件。自述"过渡兼容，最终由 RuntimeConfig 取代" | 需同步改 5 个测试改用 `LoadBusinessSnapshot().ToRuntimeConfig()`。**最高价值清理项** |
| `EventRuleChanged` | `notifier/bus.go:17` | **全仓 1 处（仅定义）**，零生产者零订阅者；AGENTS.md:193 只枚举三种事件类型，未提它 | 无影响，不与 AGENTS 冲突 |
| `ResolvedIPs` | `provider/provider.go:78` | 全仓 2 处（注释 + 定义），零使用 | 无影响 |
| `SyncEvent` | `webui/frontend/src/types.ts:117` | 全仓 1 处（仅定义） | 无影响 |
| `clearAllCache` | `webui/frontend/src/composables/useScannedResources.ts:44/62` | 全仓 2 处（定义 + 导出），零调用 | 无影响（其注释声称"「清空所有数据」后调用"，实际 `resetAll` 只做 `location.reload()`） |

### 🟡 高可信删除候选（建议清理，需一并改测试）

| 目标 | 位置 | 引用结果 |
|---|---|---|
| `retrySync` | `syncer/retry.go:26` | 生产 **0**；约 15 处测试调用。建议机械改测试调用点后删除，并同步更新 `Issue6.md` A1 段 |
| `PusherDeps.bus` + 相应断言 | `internal/health/push.go:60/91`、`push_test.go:443-462` | 只赋值不读；该断言**永真**（该包任何路径都不 Publish）→ 删字段或改为真正可失败的守卫 |
| `useDryRun.error` / `useSettings` 多余导出 | `webui/frontend/src/composables/useDryRun.ts:32`、`useSettings.ts:70` | 写而不读 / 导出未解构 |
| 仅测试使用的导出 | `Store.SetSetting`（`store.go:363`）、`SaveAlertEmail`（`:777`）/`SaveAlertWebhook`（`:826`，非 Tx 版）、`GetSettingsTx`（`:962`）、`TargetExistsTx`/`RuleExistsTx`（`:619-627`）、`SetBeforeRoundHook`（`syncer.go:466`）、`AlertManager.Current`（`alertset.go:148`）、`Server.ServeStarted`（`server.go:135`）、`Pusher.InFlight`（`push.go:180`）、`InFlightLimiter.InFlight`（`inflight.go:48`）、`ErrNoSnapshotLoader`（`coordinator.go:57`，全仓无 `errors.Is` 比较） | 生产 0（或仅声明处） | 建议改非导出而非删除——其中 `stop_gate_test.go` 依赖 `SetBeforeRoundHook` 做 A20 stop 门控回归，**有判别价值** |

### ⛔ 暂不能删除

| 目标 | 原因 |
|---|---|
| `syncer/ratelimit.go`、`NormalizeResourceID`、`SaveAlertEmailTx`、`SaveAlertWebhookTx` | **有生产调用**（`syncer.go:813`、`config/validate.go:84`、`config/store.go:1297/1300`） |
| `Syncer.Pause()` / `Resume()` | `webui/api/deps.go:20-25` 明确记录：**API 侧接口成员已删除，`syncer` 侧实现必须保留**（Build6 要求 + 包内测试使用）。若确定不再需要，需先修改文档 |
| `MultiHandler` / `NewMultiHandler` 降级为非导出 | 会撞 AGENTS.md:191「`MultiHandler` 统一定义在 `app/logutil.go`」→ **需先修改强要求** |
| `webui/api/redact_test.go`（无对应 `redact.go`） | 这不是死代码：它是针对现有写入器的 sentinel 泄漏断言测试，**属正向控制**，保留 |
| `docker-compose.yml.example:42` 的 `pgrep` 字样、`webui/server.go:40` 的 `serveOnce` 字样 | 均仅出现在**注释**中（声明不使用/描述修复前行为），属预期残留 |

### 🔁 重复实现

| 项 | 位置 |
|---|---|
| 默认值多处真值源：`"587"` / `"dingtalk"` | `config.DefaultAlertPort`/`DefaultWebhookChannel`、DDL `config/store.go:175/187`、`webui/api/bundle_v3.go:353/363` **硬编码字面量**（同文件 `alerts.go:82/91` 却用了常量） |
| `map[string]bool` 做 presence 检查 | `webui/api/bundle_v3.go:617-626`、`:786-795` → **Go map 迭代随机**，同一次非法请求的错误文案跨进程不稳定；同文件 `normalizeBundleSettings` 已用显式顺序 if 链（正确写法） |
| 前端 `theme` 双写 | `Settings.vue:127-141` 与 `useSettings.setTheme` 各写一次 |
| 三处独立健康/状态派生 | `Dashboard.vue:48-70` 自行派生"停滞"判定，与后端权威 `internal/health/health.go` 语义冲突（即 P2-08） |
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
| **startup/shutdown** | 启动顺序确定性化（`go s.Run()` → 有界等 `Started()` → 再启 `supervisor`/`pusher`）；信号在 HTTP 绑定**之前**注册；收尾顺序 `HTTP shutdown ‖ pusher→supervisor→syncer` 正确；`store.Close()` 在 `s.Wait()` 之后 | **P1-02 容器崩溃循环**；`Server.Start` 失败后 `started` 保持 true 且 `waitDone` 永不关闭（重试被拒、`Wait()` 永久阻塞，**当前接线不可达**）；`supervisor.Stop`/`pusher.Stop` 在 `Run` 从未启动时永久阻塞（Syncer 有 `runGuard`，这两个没有） | — | 按 P1-02 换 flock；与 Syncer 对齐给 supervisor/pusher 加 `started` 守卫 |
| **配置事务与运行时发布** | **本项目最强的一环**：协调器 `锁 → 单事务 → 事务内快照 → 事务内构造候选 → commit → 无失败发布`；commit 后不读库不访问网络；`commit` 失败不 apply；`RuntimeState` 深拷贝 + 单锁替换；已证明**事务内无任何网络 I/O**（四个 SDK 工厂只做本地构造，无 IMDS/元数据/token 获取） | **P2-04 发布顺序**（Wake 早于 ApplyState） | — | 按 P2-04 决策调整顺序 + 同步修订 AGENTS.md:199 |
| **同步调度** | 单一控制通道 + 4 处 `beginRound()` 硬门控（stop 门控与 enabled 门控**并列不合并**）；`Stop` 为吸收态且 `doneCh` 单所有者；`idle/failed/partial/success` 判定清晰 | P3-23 的无条件 Reset；P3-24 通知合并 | — | 仅 interval 实际变化时 Reset；`false → true` 保留恢复立即轮；按 Go 1.25 默认合同不加入 stale-tick drain，旧兼容模式另行裁决 |
| **DNS/Provider** | 只使用**增量** API（已逐调用点验证，零全量覆盖 API）；TAG 精确匹配 + `Description` 匹配；熔断阈值随状态原子发布（普通变更 `Clone` 保留计数、导入重置）；`retrySyncDetailed` 每次 attempt 重新 `Describe → Diff → Create/Delete`；部分成功用 `PartialDeleteError` 如实累计 | P1-01 本地完整复核已收口但真实云未验收；P3-25 仅资源扫描路径仍未修复；`isRetryable` 依赖**字符串关键字兜底**（腾讯 SDK 错误类型无 `Unwrap`，属有据可查的妥协） | `retrySync` 死包装；`_txlock` 注释理由与驱动实现矛盾 | P0-01/P2-01/P2-02/P2-03 与同步路径 P3-25 已吸收；资源扫描分页继续按 I-19 独立处理，重构后重新证明再删 `retrySync` |
| **告警** | 默认全关；`渠道开关 + 触发开关`同时开启才订阅；邮件与 Webhook **共用同一固定渲染器**（顺序稳定、不遍历 map）；4 在途 + 满载丢弃最新 + 安全 WARN；限流器跨热重载连续；`test-email` 8 字段契约两侧严格一致且不写库；P3-11 写库错误已能上抛 | **P2-04/P3-03 Push 时序与非法配置边界**；P2-05 Webhook 只看状态码；P3-15 丢弃日志逐条 WARN；P3-19/P3-20/P3-21 邮件与响应体细节 | — | 解析业务错误码；聚合丢弃日志；独立确认 P3-03 是否需要对非法 URL 增加定时重试 |
| **OperationalHealth** | **唯一计算源被三个消费者真实共用**（`supervisor` / `operational` 端点 / `pusher` 都走同一个 `*health.Checker`）；2s 非阻塞探活（`Store` 结构体**无互斥量**，不持应用锁）；`StartupGrace=10s` 三分支正确；`failed/partial` 直到被 `success/idle` 覆盖；原因稳定去重排序；30s 边沿监督器 | 判定输入来自三次独立 `Snapshot()`（`run.go:127-142`），注释自述"一致快照"但可能混用新旧 policy/interval → 30s 内一次瞬时误判，自愈 | `slices.Compact` 冗余 | 一次取 `*RuntimeState` 后派生 policy/interval |
| **HTTP/SSE** | 严格解码齐全（未知字段/尾随/多顶层值/10 MiB/1 MiB/413）；路径 ID `strconv.Atoi` 且 >0；请求 DTO 不含 DB `id`；导出 GET 已删（实测 405）；两类 SSE 监听服务器级 `ShutdownCh` 且每次写出有 5s deadline | P3-05 缺 `no-store`；P3-14 400/503 语义；`GET /api/alerts` 4 次非事务读存在撕裂窗口（PUT 单事务写，读侧可能"新 policy + 旧 email"，前端整体回传即把旧值写回） | `fs.Sub` 静默降级 | 补 `no-store`；GET alerts 改只读事务取快照 |
| **前端** | 8 字段测试邮件载荷两侧严格一致（历史上真实 bug 点，现有注释+类型双重防护）；无 `console.*`/存储/cookie 泄漏；密码与 Webhook/Push URL 用 `type="password"`；导出用 `fetch`+Blob 且 `revokeObjectURL`；EventSource 单实例且卸载关闭；无 `addEventListener` 泄漏 | **P2-06 / P2-07 / P2-08 / P2-09**；P3-04 SSE 重放；P3-16 一批 UI/状态偏离 | 前端重复实现后端校验 | 逐项按 P2/P3 收敛；**不建议**引入 Pinia/Vitest 等重型栈 |
| **Docker/CI** | 非 root + `CGO_ENABLED=0` 静态编译 + `HEALTHCHECK` 用静态 `/api/health`（实测全部符合）；前端在 builder 阶段构建并 `COPY` 进 Go 阶段（顺序正确）；容器实测 `healthy`、`ExitCode=0` | **清洁检出裸 Go 命令 100% 失败**（P1 级构建阻断）；P1-02 容器崩溃循环；`go get -u` 使构建不可复现（AGENTS §十 **有意设计**） | Makefile 与 CI 命令集不统一 | 给 `test`/`vet` 加 `frontend` 前置；`go get -u` 策略受强要求约束，**不建议**擅自改为锁版本 |

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

**总体**：62 文件 / 20,457 行（约为生产代码 1.9 倍），密度高，且**确实有判别性**——Issue6 逐条记录了"红灯 → 修复 → 绿灯"。`config/store_error_test.go` 用同名 VIEW 注入迁移失败、`config/store_v3_test.go:371` 判别"迁移只归零一次"、`webui/sync_status_test.go:161` 判别 `last_success` 保留语义、`main_test.go` 的 8 字段邮件载荷用例含**反向断言**（多带 `enabled` 必须 400），这些都是**真正会失败的**测试。

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
3. `GET /api/zones` handler —— 路由字符串**未出现在任何 Go 测试**（`zones_test.go` 只测 `zoneData` map）
4. **零 goroutine/ticker/连接泄漏断言**（`grep NumGoroutine|goleak` 唯一命中是注释）
5. `provider/scan.go` 四条扫描路径几乎无测试（`scan_test.go` 仅 16 行）
6. **负面 sleep 断言会在慢 runner 假通过**：`syncer/state_test.go:174-177,204-205`、`syncer/syncer_test.go:130`；syncer 包 25 处 sleep（12 处 >200ms，最长 600ms）集中在最关键的门控用例上。`webui` 包用 `ServeStarted`/channel 做确定性同步，是正面范例
7. `app/logutil_test.go` **完全没有 `MultiHandler` 用例**
8. `-race` 非空跑（`t.Parallel()`=0，但 24 个测试文件在用例内起 goroutine），唯一边角是上述 race-only 用例

**推荐新增的最小测试**（**不引入任何测试框架**）：

1. P0-01：Aliyun `GetRules→Diff` 往返收敛断言（已由 `TestDiff_AliyunPortRoundTripConverges` 补齐，Issue7 重构时必保留）
2. P1-01：同目标两条空 comment 规则的双轮断言（第二轮不得出现 delete）
3. P2-01：IPv6+ICMP key 断言（Lighthouse/CVM）
4. P2-02：ECS 150 条删除产生 2 个请求
5. P2-04：探针 waker 断言"wake 时新配置已可见"
6. P3-05：`GET /api/settings` 的 `no-store` 断言
7. 前端载荷契约固化在既有 Go 测试中（8 键字面量断言 200 + 带 `enabled` 断言 400）
8. 一个约 40 行的 `build_contract_test.go`（断言 alpine/golang 版本、`CGO_ENABLED=0`、非 root、HEALTHCHECK 指向 `/api/health` 且不含 `/operational`、`linux/amd64`、`.dockerignore` 含 `fwalizer-config`）
9. `config/pidfile` 单测（目前**不存在**该文件）；I-01 实施时需覆盖真实锁竞争、残留/复用 PID、cleanup 释放与 `0600` 权限

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
17. **`DailyRisk` 无关项：`isRetryable` 的字符串兜底有据**（腾讯 SDK 错误类型无 `Unwrap`，`.md` 注释已说明依据）

---

## 8. 专题：规则身份与 TAG 所有权（主体已实施，完整复核仍有待修项）

> **本章是 P1-01 及其全部关联内容的唯一集中位置。**
>
> **状态（2026-09-30）**：用户已定案“目标级完整期望集 + TAG 所有权 + comment 纯可读 + 先增后验 + 平台化条件删除 + 无法证明安全时保留残留”，Step 1～5 主体已提交为 `28559ed`；Lighthouse 多端口与 SWAS 分页上限补强已提交为 `38bdc19`；R7-01～R7-04/R7-06/R7-07 已提交，R7-05 已在当前工作树修复。核心 planner/TAG/四平台安全主线与本地 R7 核验项已收口，但**真实四云、浏览器回归与远端 CI/GHCR 仍未执行**（PT-I7-01～07），故不能写成外部验收或发布闭环。`maxTagRunes` 仍为 48。

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
| **TAG 上限收紧 48→32** | 🔶 **已撤回** | 该收紧**仅为配合方案 A** 而提出。`config/validate.go:34` 仍为 48；`AGENTS.md:162` 未改 |
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
  - 若最终采用**方案 A** 并收紧 TAG 上限 → 需修订 **AGENTS.md:162**「TAG … 最多 48 个 Unicode 字符」（属**强要求变更**，必须用户明确授权）。
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
| 2 | 若选 **A**：desc 需含 host + **协议 + 端口**（否则路径 2 未修复）；长度超限行为（拒绝保存 / 截断 / 哈希降级）；是否收紧 `maxTagRunes` 并修订 AGENTS.md:162 | |
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
| TAG 上限收紧 48→32 | 🔶 **已撤回**（`config/validate.go:34` 仍为 48；AGENTS.md:162 未改） |
| 旧规则迁移 | ✅ **仍然有效**：用户确认**无历史兼容负担** → 任何最终方案都不需要 orphan 清理入口 / 迁移脚本 / 旧格式 WARN。此结论与身份模型无关，独立成立 |
| SWAS `Remark` 上限 | ⏳ 已登记 **ProdTestList.md PT-B7-09**，待真机确认；只影响 comment 可读长度，不阻塞 Issue7 Step 1～4 |

---

## 9. 用户决策记录（决策不等于已实施）

> 本表保留作出决策时的原始记录。当前实施状态统一看第 0 节：P2-04 的 AGENTS 目标顺序已在 Issue7 Step 0 修订，剩余为代码与测试；P2-03 原“只补 WARN/Dry Run、不计 skipped”的记录已被当前 AGENTS/Issue7 的 `unsupported → partial` 与清理冻结合同取代。保留旧记录是为了追溯，不得据此覆盖当前强要求。

| # | 议题 | 你的决策 | 实施要点 |
|---|---|---|---|
| 1 | P1-01 规则身份 | ✅ **已实施（本地，`28559ed`）** | 采用目标级完整期望集、TAG 唯一操作授权、comment 纯可读、先增后验、平台化条件删除与可接受残留；不收紧 TAG 48、不需要迁移。详见第 8 节与 Issue7 Step 1～5 |
| 2 | P1-02 容器 pidfile | **改用 flock 文件锁替代 PID 判活** | 见 P1-02 章节；顺带消除 P3-08（TOCTOU 漏判 + PID 复用误判） |
| 3 | P2-04 唤醒顺序 | **改代码：`ApplyState` 提到两次 `Wake()` 之前，并同步修订 AGENTS.md:199** | 见 P2-04 章节；`coordinator.go:50-53` 注释同步 |
| 4 | P2-07 删除确认 | **补卡片式二次确认，满足 AGENTS §11** | `Targets.vue` / `Rules.vue` 各加一个 `NModal preset="card"`（确认按钮 `type="error"`），复用 `Logs.vue` 模式；**不改强要求文档** |
| 5 | P2-03 静默跳过 | 📚 **审计快照中的旧决策：先只补 WARN 日志 + Dry Run 展示，不计入 skipped** | **已被当前强合同取代**：能力限制进入结构化 `unsupported`，目标结论为 `partial` 并冻结清理；见 P2-03 当前实施补记与 Issue7 Step 1。此行仅用于追溯，不得据此实施 |
| 6 | P3-01 熔断器淘汰 | **A（推荐）：加完整淘汰策略，待授权实施** | `RecordSuccess` 删除成功域名条目；普通 `BreakerPreserve` 按当前活动域名集合裁剪，并只复制正数失败计数；完整导入继续 `BreakerReset`。B“只丢弃零计数”或 C“只按活动域名裁剪”均只能解决一类残留，不足以关闭 P3-01；本轮只记录决策，不改代码 |
| 7 | P3-12 文件权限 | **收敛为 0700/0600** | 数据目录 `os.MkdirAll(dir, 0700)`；建库后 `os.Chmod(dbPath, 0600)`；pidfile/lock 文件 `0600`。注意：**对已存在的目录/文件需显式 Chmod**（`MkdirAll` 不改变既有权限） |

---

## 10. 当前实施顺序与历史批次

### 10.1 当前有效主线：Issue7 Step 0～5

**Issue7 P1-01（Step 0～5 主体提交 `28559ed`；F1/F5 补强提交 `38bdc19`；完整复核待修项见 Issue7 §12.5）**

1. ✅ Step 1 纯规划器保留已修复 P0-01/P2-01 的绿色回归，按当前合同吸收 P2-03，并修复 **P3-25 同步路径**安全前置；P3-25 资源扫描路径不在该完成结论内。
2. ✅ Step 2 实现目标级 Add → Describe → coverage verification 与安全门，Step 3 起门开即条件删除。
3. ✅ Step 3 按 Lighthouse/CVM/SWAS/ECS 逐平台开启条件清理，同时吸收 P2-02。
4. ✅ Step 4 主体已落地；R7-02 已提交为 `eab4bea`，R7-03 已按裁决 A 修复并提交为 `297ccfe`，Dry Run 展示所有已配置目标且无适用规则目标零云调用；Dashboard/健康主线与 P2-08/P2-09 修复保持成立。
5. ◧ Step 5 本地二进制/Docker 与主体门禁证据已取得，F1/F5 补强已提交，R7-01～R7-07 本地核验项已收口；**PT-I7 四云/浏览器真实验收**仍未完成。

> P0-01 与 P2-01 均是必保留的绿色回归；P2-01 已在唯一 canonical functional key 中修复，不再是失败先行项。P1-02/flock 仍是可独立实施的高优先级项，但不是 P1-01 的前置。

### 10.2 当前独立问题入口

与 Issue7 正交的 P1-02、P2-04～P2-07 及其余 P3 继续按第 0.4 节的 I-01～I-11 排队；P1-01 完整复核及后续研究形成的 R7-01～R7-07 对应 I-12～I-18。该编号只表示 backlog 顺序，不改变 finding 严重级别，也不构成代码实施授权。

### 10.3 历史批次计划（与 Issue7 重叠部分已被取代）

> 下列批次是上一版报告形成时的排序记录，完整保留用于追溯。凡与 Issue7 Step 1～5 重叠的 P2/P3，均以 10.1 和 Issue7 为准；不得按本节另建第二套并行施工顺序。

**历史批次 2 — 其余云端规则正确性（原始排序，当前状态以 0.2/0.4 和 Issue7 为准）**

P2-01（IPv6+ICMP key，已由 Issue7 修复）、P2-02（ECS 删除分批，已由 Issue7 修复）、P2-03（旧“补 WARN + Dry Run”方案已被 `unsupported → partial` 取代）、P2-04（唤醒顺序 + 文档）、P2-05（Webhook errcode）、P2-06（告警页守卫）、P2-07（删除确认）

> 验收：每项一个判别性用例；P2-02 需 150 条删除分批断言

**历史批次 3 — 生命周期、并发与内存（原始排序，当前状态以 0.2/0.4 为准）**

P3-01（熔断器淘汰）、P3-23（ticker Reset）、P3-24（通知合并）、P3-25（ECS 同步分页守卫已修复，资源扫描分页守卫仍未修复）、P3-22（DryRun 限速，已由 Issue7 修复）、P3-09（`_txlock`）、P3-12（权限）

> 验收：配置保存 N 次后熔断器 map 不增长；`-race` 全绿

**历史批次 4 — 状态一致性与错误处理（原始排序，当前状态以 0.2/0.4 为准）**

P3-05（`no-store`）、P3-10（忽略的 error）、P3-11（`StoreLogWriter` 返回错误，已由 Issue7 修复）、P3-13（MultiHandler 收集全部错误）、P3-14（400→503）、P3-21（drain + 字节上限）

**历史批次 5 — 死代码与重复实现**

§4 全部"确认可删除"与"高可信删除候选"；`bundle_v3.go` 默认值改用常量、presence 改有序切片

**历史批次 6 — 构建、测试与文档闭环**

Makefile `test`/`vet` 加 `frontend` 前置；修 `waitForNoSMTPData`；消灭 7 个永不失败的测试；补字面量锚点；补构建契约测试；补 `pidfile` 单测；清理约 12 处 "version 2" 与 §4 文档漂移

**依赖关系**：P1-01 的产品语义已确认，不再有配置形态决策前置。P3-25 **同步路径**已在自动清理前改为不完整快照硬失败；剩余的是不参与同步清理的资源扫描路径分页守卫，应独立验收。P2-04 代码需按 Step 0 已修订的 AGENTS 发布顺序独立落地；其余死代码与同文件清理应在 Issue7 Step 1～4 完成后再重新证明。

---

## 11. 覆盖矩阵摘要

| 模块 | 主审 | 复核 | 文件数 | 入口/调用链 | 验证 | 未验证 |
|---|---|---|---|---|---|---|
| 启动/生命周期（`main.go`,`run.go`,`app/`） | A | 主代理 | 3 生产 | `main → run → runWebUI → 启动/收尾序列` | 容器 SIGTERM 实测 `ExitCode=0`；双实例 pidfile 实测；**历史 Docker 实测曾复现 P1-02** | 当前 `flock` 修复后的残留 PID、PID 复用、并发、SIGKILL 重启与真实部署机 systemd |
| 配置/DB/运行时（`config/`） | A | 主代理 | 7 生产 | `OpenStore → initTables → LoadBusinessSnapshot → BuildRuntimeState` | `config` 包 `-race` 绿；驱动源码核对 | 真实旧库迁移路径 |
| 同步/DNS/重试（`syncer/`,`dns/`,`internal/`） | **B** | **主代理 overlay 探针复现 P0-01/P1-01/P2-01** | 7 生产 | `Run → beginRound → syncAll → runRound → syncDomain → retrySyncDetailed` | `syncer`/`provider`/`dns` 包测试绿 + 3 个判别性探针 | 真实云写入 |
| Provider（4 云） | **B** | 主代理（逐 API 调用点验证） | 8 生产 | `Provider 接口 → 四实现 → SDK 调用` | **确认零全量覆盖 API**；CVM 配额对照官方文档 | 真实云 API |
| 通知/健康（`notifier/`,`internal/health/`） | C | 主代理（推翻 1 项误报） | 7 生产 | `Publish → OnEvent → SMTP/HTTP`；三消费者共用 Checker | `-race` 绿；stdlib `handler.go` 源码核对 | 真实 SMTP/Webhook/Uptime Kuma |
| HTTP/API/SSE（`webui/`,`webui/api/`） | D | 主代理（推翻 1 项误读） | 20 生产 | `Register(28 路由) → handler → Coordinator` | `webui`/`webui/api` `-race` 绿；路由表逐一核对 | 浏览器交互 |
| 前端（`webui/frontend/src/`） | E | 主代理（复核 5 项 P2） | 18 源 | `view → api.ts → Go handler` 逐字段 | `npm run build`/`vue-tsc` 经 lead 门禁通过 | 浏览器人工 |
| 全局/部署/测试 | F | 主代理 | 6 构建文件 + 62 测试 | Makefile/Dockerfile/CI 三套命令对比 | 全部门禁实跑 | 远端 CI/GHCR |

**文件覆盖**：53/53 生产 Go 文件（root 2、app 1、config 7、dns 2、internal/health 3、internal/portconv 1、internal/tag 1、notifier 4、provider 8、syncer 4、webui 2、webui/api 18）**全部读过并归属至少一个主审**；高风险文件均有独立交叉复核。

**流程偏差如实记录**：`e38de1fb`（同步/DNS/Provider）与 `00a69dd7`（全局死代码/部署/测试）两路分派**超出合理时限未按时返回**；我已发出限时收敛请求，并**亲自完成**这两个范围的审核与验证（P0-01/P1-01/P2-01 由我自写探针复现）。因此覆盖无缺口，但这两路的"第二双眼睛"来自主代理而非独立子代理——建议后续针对 `syncer/syncer.go`（971 行）与 `webui/api/bundle_v3.go`（887 行）另做一次独立复核。

---

## 12. 命令与证据

### 门禁（全部 PASS）

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
| **持久卷 + 预置陈旧 pidfile `1` 启动容器** | **历史 P1-02 复现**：`exited exit=1`，日志「FWAlizer 已在运行 (PID: 1)」。**更正**：首次用 `--volumes-from` 未能复现（卷未复用），后改持久命名卷稳定复现；本轮未重新运行 |
| `git archive HEAD` 抽取清洁副本 → `go build/vet/test ./...`、`make vet` | **EXIT=1/1/1/2** `pattern frontend/dist: no matching files found`（构建阻断） |
| 同上 + 手动 `mkdir webui/frontend/dist` → `go build ./...` | EXIT=0（定位根因） |
| 顺序启动两个实例（同一 `FWALIZER_DATA_DIR`） | 历史结果：第二个被拒并提示 PID；只能证明顺序场景，不证明并发安全 |
| **并发**启动两个实例 | 历史一次实验为一个退出、一个运行，**未复现** TOCTOU 双实例；不能据此宣称当前实现并发安全，I-01 必须用“测试先持有内核锁”的判别性控制补强 |
| 遍历 provider 全部 SDK 调用点 vs 禁用 API 名 | 仅 Create/Delete/Authorize/Revoke/Describe，**零** `ModifyFirewallRules`/`ModifySecurityGroupPolicy`/reset |
| Go stdlib `log/slog/handler.go`：`TextHandler.Handle` 是否按 level 过滤 | **不过滤**（过滤只在 `commonHandler.enabled`）→ 驳回相应误报 |
| `modernc.org/sqlite@v1.54.0/tx.go:23` | `if !opts.ReadOnly && c.beginMode != ""` → 证实 `_txlock` 注释理由不成立、mitigation 安全（P3-09） |
| `查询用户安全组配额.md:62` / `安全组添加规则.md:18` | `SecurityGroupPolicyLimit: 100` + "一次请求只能创建单个方向" → 支撑 P3-06 |
| `grep 'delete(' dns/circuitbreaker.go` | **零命中** → 证实 P3-01 永不淘汰 |
| `grep io.ReadAll/LimitReader/Body.Close/NewTicker/go func` | 各 1/2/2/4/6 处，逐处核对均闭合 |
| fd 探针（5000 次注册，`/dev/fd` 计数） | `fd_before=5 fd_after=5` → 无 fd 增长 |
| 前端 8 字段载荷 vs Go DTO 逐字段比对 | **一致**（`webui/api/test_email.go:24-33` ↔ `Alerts.vue:66-75`） |
| `grep NModal` 各 view + 删除按钮绑定 | Targets/Rules 删除**直接绑定 DELETE** → 证实 P2-07 |

---

## 13. 证据边界与未验证项（**不得外推为通过**）

下表同时包含“未执行”“历史证据不可外推”“已执行但范围有限”和“不适用”。每行状态必须按原文理解；只有明确写为当前 revision、当前环境已完成的证据，才能支持相应范围内的结论。

| 边界 | 状态 |
|---|---|
| **真实腾讯云 / 阿里云 API** | **未执行**。P0-01 与 P2-01 的 key 不对称在 **provider 层与"云端回传形态无关地"**被证明；当前实现的真实云端回传、四平台删除安全、CVM 配额方向口径、ECS 是否真的拒绝 >100 个 RuleId、SWAS Remark 50 上限均**未实测** |
| **真实 SMTP 服务器接受** | **未执行**。仅使用假 SMTP |
| **真实收件箱投递**（含中文主题的 MTA 编码表现，P3-20） | **未执行**。用户已于 2026-09-27 决定跳过并自行处理 |
| **真实 Webhook**（P2-05 的依据来自官方文档与公开同类报告，**非本项目实测**） | **未执行**。用户已决定跳过 |
| **真实 Uptime Kuma HTTP Monitor** | **未执行** |
| **真实 Uptime Kuma Push DOWN/恢复通知**（含 P2-04 的实际触发概率） | **未执行**。P2-04/P3-03 为代码与时序分析结论 |
| **浏览器人工检查**（PT-B7-07、PT-I7-06；布局、按钮尺寸、Dry Run 卡片、重复 key 的实际 patch 行为、侧边栏高亮、SSE 重连表现） | **未执行**。本环境无浏览器工具；前端结论均为源码级 + 构建/框架源码取证 |
| **Docker 容器运行** | 历史已执行（healthy / uid 1000 / ExitCode 0 / SIGKILL 后重启曾复现 P1-02）；本轮未重新运行，`flock` 修复后的残留 PID、SIGKILL 后重启与真实负载下“完成当前轮次再退出”均未验证 |
| **远端 CI / GHCR** | **未执行**。既有 `v2.0.0`（run `36300428681`）结果属**更早 revision，不能证明当前改动** |
| 真实 DNS 上游（`dns/resolver_test.go` 两用例无网络时 `t.Skip`） | **未执行** |
| SQLite 真实 BUSY/慢盘争用下的 2s 探活上限端到端保证 | **未执行**（仅核对驱动有 `interruptOnDone`、`Store` 无互斥量） |
| 真实旧库迁移路径 / 生产 SQLite | **未执行** |
| `GOOS=windows` 运行验收 | **不适用**（用户已决定移除支持，仅以构建失败为证据） |
| 后端 `syncer/syncer.go` 与 `webui/api/bundle_v3.go` 的**第二个独立子代理**复核 | **未完成**（原分派超时，由主代理亲自覆盖替代） |

**结论**：`go test` 通过只证明对应测试覆盖的行为，**不得**外推为真实云、真实 SMTP、真实 Webhook、真实 Uptime Kuma、浏览器或远端 CI 已通过。

---

## 14. 审计快照的 Git 完整性（历史记录与当前复核）

> 本节前四列保留原始审计快照，不应被误读为当前 HEAD。当前复核基线见最后一行：`main` 与 `origin/main` 同指 `34aa9b8`，且复核开始时工作树干净。

| 项 | 审核前 | 审核后 |
|---|---|---|
| `git rev-parse HEAD` | `11918fb945fe9dfe2a86ead5bc833b14dd156a68` | **同一值（历史快照）** |
| `git status --short --branch` | `## main...origin/main`（干净） | 干净；**历史快照中本报告文件曾是新增未跟踪文件** |
| tracked 文件改动 | 0 | **0** |
| `git diff --stat` / `--cached --stat` | 空 | **空** |
| ignored 产物 | `fwalizer`、`webui/frontend/dist/`、`webui/frontend/node_modules/` | **完全相同的 3 项**（均系审核前已存在，未新增） |
| 审核产生的临时产物 | — | **全部清理**：overlay 探针目录、清洁检出副本、pid/fd 探针目录、`fwalizer:audit` 镜像、测试容器与命名卷均已删除 |

**历史说明（保留）：** 原审计完成时 `fwalizer-audit-final1.md` 是用户要求写入根目录的新增未跟踪文件，`.gitignore` 未匹配它；后续文档提交已将本报告纳入版本控制，因此该说明不代表当前状态，也不应据此修改 `.gitignore`。

| 当前复核（2026-09-30） | `34aa9b859c38904e701a19d672dcc4f65435b31c` | `main == origin/main`；复核开始时工作树干净；本轮只允许更新本报告，不运行构建/测试，不修改代码或其他文档 |

**明确确认：本次代码审核未修改、未创建、未删除任何受版本控制的仓库文件，未执行任何 state-changing git 命令，未触碰真实云/SMTP/Webhook/Uptime Kuma，未升级依赖，未安装任何全局工具。** 探针均经 `go test -overlay` 虚拟映射运行，仓库零写入。

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
| P1-01 状态继续更新 | 2026-09-28 的“待决策/已冻结”已于 2026-09-29 被第 8 节最终方案取代；Step 0～5 主体提交为 `28559ed`，F1/F5 补强提交为 `38bdc19`，R7-01～R7-04/R7-06/R7-07 已提交，R7-05 当前工作树已修复；`maxTagRunes` 仍为 48，SWAS `Remark` 上限仍登记为 PT-B7-09 |
| 当前复核状态（2026-09-30） | 当前 HEAD 为 `b19d271`，`main` 相对 `origin/main` ahead 1；R7-01～R7-07 本地核验项完成，R7-05 与文档改动未提交；P3-11/P3-22 已修复；P3-25 拆分为同步路径已修复、资源扫描路径未修复；外部/人工验收登记为 PT-B7 9 项 + PT-I7 7 项，均未执行/不得写成通过 |

## 附录 B：规则身份专题（已移至正文第 8 节）

> 本附录的内容已**整体移入第 8 节「规则身份与 TAG 所有权」**，以避免同一议题在文档内出现两处。
>
> **当前摘要**：P1-01 已探针复现，并已定案将“个体识别”从 description/comment 上移除，改为目标级完整期望集与 canonical functional key；TAG 仍为唯一操作授权，`maxTagRunes` 仍为 48。实施状态见 Issue7。
>
> 完整内容请看 **第 8 节**：8.0 为最终方案；8.1～8.7 保留定案前的机理、候选与不确定性历史；8.8 记录当前决策状态。
