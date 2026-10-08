# Issue8.md — 未闭环问题与待裁决事项台账

> 本文档由 2026-10-08 的**全量只读复核**（详见 [fwalizer-audit-final1.md](./fwalizer-audit-final1.md) 与本轮补充复核）产出，用途是**汇总仍未修复、未完成审阅、待裁决、未派发专项、文档订正与外部验收边界**，不建立第二套实施合同。
>
> - 强要求仍以 [AGENTS.md](./AGENTS.md) 为唯一来源；设计方向见 [Design5.md](./Design5.md)；P1-01 合同见 [Issue7.md](./Issue7.md)；历史问题见 [Issue5.md](./Issue5.md)、[Issue6.md](./Issue6.md)；人工验收清单见 [ProdTestList.md](./ProdTestList.md)；业务待办见 [TODOLIST.md](./TODOLIST.md)。
> - 本文档中的"仍存在""未完成"均以**当前磁盘**为准；不得据历史批次措辞反推现状。
> - 编号 `I8-xx` 是本文档内部编号，**不是**审计报告的 P0/P1/P2/P3/I/R7 编号；`NEW-*` 为复核过程新增发现。

---

## 0. 基线与本轮复核范围

| 项 | 值 |
|---|---|
| 分支 / HEAD | `main` / `fd6ed7e`（会话期间被并行会话推进两次：`b658492` P3-25 补强 → `fd6ed7e` 本台账 I8-03/I8-04 订正提交） |
| 本地 `origin/main` | `938bb8b`；ahead/behind `0/2`（**未 fetch**，不代表真实远端） |
| **审阅基线**（本轮全部复核所用） | `938bb8b` + 5 个未提交文件：`AGENTS.md`、`fwalizer-audit-final1.md`、`provider/scan_ecs_test.go`、`provider/snapshot_test.go`、`syncer/ecs_pagination_test.go`（P3-25 批次，生产 Go 代码零改动） |
| **当前工作树** | 仅 `M Issue8.md`（本文档在本会话中的继续追加）；其余均已提交 |
| 并发事件说明 | 本会话进行中，**并行的另一会话提交了 `b658492`**，把本轮授权的文档订正（含新建 `Issue8.md` 与三份 Go 测试）一并纳入；本会话此前的文档改动经逐项核实已全部进入该提交（§0.2 / P3 清单 / §14 / 附录 A / P3-06 / P2-07 / AGENTS / TODOLIST / Issue7 / ProdTestList 的订正均在盘） |
| 复核方式 | 仓库外副本执行测试/overlay/探针；仓库内只读（除本轮授权的文档回写）；**未调用真实云/DNS 上游/SMTP/Webhook/Uptime Kuma** |
| 已确认修复（不在本台账） | P0-01、P1-01/02、P2-01～P2-05、P2-07～P2-09、P3-01～P3-09、P3-11、P3-13～P3-17、P3-19～P3-26、F1、F5、R7-01/02/03/05/06/07、I-01/02/04/05/06/08/09/11.1/12～I-19 等 |

---

## 1. 仍存在的缺陷

### I8-01 【高 · P0】`coverage_ready` 在 unsupported-only 计划下误报（= TODO-001）

- **问题**：当某目标的**全部**期望功能都被标记为平台不支持（`unsupported_*`）时，`coverage_ready` 仍可为 `true`，可能被下游读作"已覆盖/可清理"。
- **当前证据**：`provider/plan.go:655` `CoverageReady = len(plan.Desired) > 0 && !in.AddStateUnknown && len(plan.ToAdd) == 0`；`implementable` 变量仅 `:627/:631` 自增后**再无使用**（`grep -n implementable provider/plan.go` 仅 3 处命中）→ 全 Desired 不可实施时 `ToAdd` 为空 ⇒ 可达 `true`。
- **测试现状**：仅覆盖空 Desired（`provider/plan_test.go:359` 附近）与 Lighthouse 多端口（`:526-561`），**无 unsupported-only 判别用例**。
- **权威出处**：`TODOLIST.md:30`（TODO-001，标为最高优先级）；审计 `fwalizer-audit-final1.md:1851`、`:1853`。
- **影响**：Dry Run / 覆盖判断可能对"平台根本做不了"的目标误报为已覆盖。
- **状态**：**仍未修复，且本轮无任何组认领**。

### I8-02 【中 · C 级】跨 attempt 的残留计数被空 attempt 抹平（= TODO-008 / 审计 `:990` 独立观察）

- **问题**：目标首次 attempt 已产生可信残留（部分删除/幂等 NotFound），后续 attempt 在 Resolve/S0/Add/S1 早退时，`cleanup_candidates` / `cleanup_deferred` 被**覆盖为 0**，而 `cleanup_deleted` 仍保留累加值。
- **当前证据**：`syncer/target.go:102-107` 中 `added/deleted/cleanupDeleted` 为 `+=`，而 `:105 cleanupCandidates =`、`:107 cleanupDeferred =` 为**覆盖赋值**；`runTargetAttempt` 每次进入以 0 初始化（`:143-148`），早退路径 `:154-157`（S0 失败）、`:173-183`（Add 失败）、`:186-189`（S1 失败）返回零值 res。
- **复现**：两个小组各自独立复现 —— `outcome=failed added=1 deleted=1 cleanup_candidates=0 cleanup_deleted=1 cleanup_deferred=0`，而云端**仍存 1 条陈旧规则**；对照"仅 1 个 attempt"场景正确保留 `2/1/1`。
- **下游放大**：`webui/api/logwriter.go:99-104` 的可读详情以 `candidates > 0` 为门槛 ⇒ `candidates=0` 时整行"清理候选…"详情被**完全抑制**；`deleted(1) > candidates(0)` 自相矛盾。
- **重要限定**：`git show b19d271 -- syncer/target.go` 显示该两行是**未改动的上下文** ⇒ **非 R7-04 引入，修复前即存在**；合同**未规定**"后续 attempt 失败时残留数如何聚合"。
- **状态**：**仍存在，确定性可复现**；语义待裁决（见 I8-30）。

### I8-03 【中】清洁检出构建阻断（= §5 附"构建阻断"）

- **问题**：`webui/embed.go:5` 的 `//go:embed frontend/dist` 依赖被 `.gitignore:5` 忽略的目录，而 `Makefile:9-13` 的 `test`/`vet` **无 `frontend` 前置**（`build` 有）。
- **实测（清洁检出 = `git archive HEAD`）**：`go build ./...` / `go vet ./...` / `go test ./...` = **EXIT 1/1/1**（`webui/embed.go:5:12: pattern frontend/dist: no matching files found`）；根包与 `webui` 包 `[setup failed]`；`make vet` = EXIT 2；`make all` 在第一个前置 `vet` 就中止（**EXIT=2，`frontend` 目标从未被触达**）。
- **对照（当前工作树）**：存在 ignored `webui/frontend/dist` → `go build ./...` EXIT 0。
- **⚠️ 根因不是"目录存在"**：手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` **仍 EXIT 1**（`contains no embeddable files`）；**必须目录内有 ≥1 个可 embed 文件**才 EXIT 0。审计 §5 附表原写的"手动建目录后 EXIT=0"**已证伪，不得据此修**。
- **附带新发现**：清洁检出中 **`go list ./...` 本身就失败**（非 0 且无输出）⇒ 任何 `go test $(go list ./...)` 脚本会**静默退化成"只跑当前目录"**；须用 `go list -e ./...`。
- **包级结果口径**：实测 **2 包 `[setup failed]`（root 与 `webui`）+ 9 包真跑全 ok + `dns` 包未跑**（授权禁止真实外网解析，仅验证到编译层）⇒ **不得主张"清洁检出可通过"，也不得主张清洁检出里 12 包全绿**。
- **影响**：**所有"本地门禁绿色"都以一个未入库文件为前提**；Docker/CI 因各自先构建前端而不受影响。
- **禁止项**：**不得**提交占位 `webui/frontend/dist/index.html`——一旦提交，`webui/server.go:307` 会把占位页当真实 SPA 提供，且 build/vet/test 全变绿，**静默掩盖真实故障**（当前风险低：已忽略 + 未跟踪 + 任何 ref 从未提交 + `git add -A` 暂存不到，仅 `git add -f` 可绕过）。
- **状态**：**仍存在**；修法（给 `test`/`vet` 加前置）属待办，见 I8-36。

### I8-04 【中】门禁非密闭：默认测试命令**会执行真实外网 DNS 查询**

- **问题**：`dns/resolver_test.go:10/29/40` 使用 `8.8.8.8:53` 且都真调 `Resolve`；`TestResolve_NonExistent`(`:25-27`) 与 `TestResolve_PublicDomain`(`:37-39`) **有 `testing.Short()` 守卫**，但**默认门禁命令（`Makefile:10` 的 `go test ./... -race -v`、CI `docker-publish.yml:68`）都不传 `-short`** ⇒ 守卫失效、真实外网查询被执行。唯一无守卫的 `TestResolve_Localhost`(`:9-22`) 经回环探针证明**根本不外发查询**（上游换死地址仍 PASS，走 hosts 文件）。
- **实测**：把上游改指 `127.0.0.1:15353` 并挂 UDP 监听后——`-short` → **0 个报文**；默认语义 → **6 个 UDP 报文**（`host.invalid`×2、`dns.google`×4），dns 包耗时 **15.1s**（5s PASS + 10s SKIP）。全程未触碰 `8.8.8.8`。
- **影响**：① 无外网/出口受限环境门禁必然变红，且失败点与被测产品无关 ② 历史"全量 12 包 race 绿"**包含真实外网 I/O**，不能表述为纯本地密闭门禁 ③ 清洁检出下 `dns` 包的运行结果**未验证**（只到编译层）。
- **订正说明**：审计原表述"无 `testing.Short()` 守卫"**不准确**（2 个用例有守卫，另 1 个不外发）；准确表述是"**默认门禁不过滤 `-short`，导致守卫失效、真实外网查询被执行**"。

### I8-05 【中低】Push 心跳等待被任意配置保存整段重置

- **问题**：`internal/health/push.go:152-165` 的 wake 分支 `timer.Stop()` 后回到循环顶部，URL 未变时重新 `NewTimer(interval)` **整段重新计时**；而任意页面保存都会经 `webui/api/deps.go:168 Push.Wake()` 触发。
- **实测**：interval=400ms 时，对照组 1.2s 内 4 次请求；实验组每 100ms Wake 一次 → 1.2s 内**仅 1 次**。
- **影响**：连续保存可无限推迟心跳 → Uptime Kuma 侧可能误判 DOWN。Syncer 的同类问题已由 P3-23/P3-24 按"同间隔保留剩余等待"修复，Push 未同步该合同。
- **测试现状**：**无任何测试覆盖该语义**。
- **状态**：**仍存在**（是否对齐 Syncer 合同待裁决，见 I8-32）。

### I8-06 【中低】正常发送路径不应用 20s 间隔下限

- **问题**：`internal/health/push.go:152-155` 只要 `interval > 0` 就原样使用；`MinPushInterval` 仅在非法 URL 重校验分支（`:169-178`）与 API/导入校验边界（`config/validate.go:368-376`）生效，`config/store.go:966-993` 只要求时长为正。
- **实测**：合法 URL + `interval=1ns`（仅能由直改 SQLite 等绕过 API/导入的脏配置产生，已有 `TestPushDirtySQLiteConfigReachesRuntime` 证明此类配置可达运行快照）→ **300ms 内向回环 mock 发出 1211 次请求**。
- **状态**：**既有行为**（非 P3-03/P3-21 回归）；与 P3-03 为非法 URL 分支补下限的做法不对称。

### I8-07 【中低】配置包 version 3 的 `null` 覆盖：**null 专属用例 4 处，无任何守护的字段 30 个**（2026-10-08 由独立复核 G-12b 逐字段实测改写）

- **null 专属用例（4 处，不是 1 处）**：`version:null`（`webui/api/import_test.go:113`）、**`targets:null`（`:118`）**、`target_export_ids:null`（`:131`）、`monitoring:null`（`webui/api/alerts_v3_test.go:211`）。
- **有守护但无 null 专属用例的字段**：`rules`、`settings`、`alerts`、`alerts.policy`、`monitoring.uptime_kuma_push`、`metadata`、`metadata.exported_at` —— 它们有"**缺失形态**"用例走**同一守卫分支**，实测**删守卫会变红**。
- **真正无任何用例（含缺失形态）守护的字段：30 个**（含 `alerts.email`、`alerts.webhook`、`settings.credentials{,.tencent,.aliyun}` 等）。删除守卫后仓库**全绿**；判别力探针 **46/46** 有效。
- **⚠️ 修正说明**：本条此前写作"10 类无用例"，其中 **6 类实际有（缺失形态）守护**。**若按旧表述补测，会重复补 6 类、漏掉 24 个**；正确清单见 `/tmp/fwalizer-issue8-evidence/G12b/null-matrix.md §2`。
- **附加**：`presenceSlice` 是当前唯一"自定义 `UnmarshalJSON` 内自建严格 decoder"的类型，其**内部尾随检查**与 `MarshalJSON` 的 nil→`[]` 兜底分支均**无守护且不可达**。
- **状态**：主契约本身成立（下条），**本题只登记覆盖缺口**。

### I8-08 【低】SWAS 资源扫描对结构缺失静默截断并覆盖缓存

- **问题**：`provider/scan.go:153-156 scanAliSWAS` 对 `body`/`Instances` 缺失直接 `break`，返回累计结果与 `nil` error → `webui/api/scan.go:66` **覆盖写缓存**。
- **实测（API 级，真实 SDK→handler→临时 SQLite）**：首页 `{}` → `success=true count=0`，**旧缓存被清空**；首页 100 条满页 + 次页 `{}` → `success=true count=100`，**旧缓存被截断列表替换**。
- **范围说明**：P3-26 只覆盖 ECS 扫描路径；审计与 AGENTS 均未声明 SWAS 扫描已修。

### I8-09 【低】Lighthouse / CVM 扫描对空响应直接解引用

- **问题**：`provider/scan.go:72-78`（`scanTCLighthouse`）与 `:113-119`（`scanTCCVM`）直接解引用 `resp.Response`。
- **实测**：对响应体 `{}` 实测 `invalid memory address or nil pointer dereference`（panic）；全仓生产代码 `recover()` 命中 0 处 → 由 net/http 每连接兜底，进程不崩但该请求异常中断。
- **对照**：同步 Provider 已守卫（`provider/tc_lighthouse.go:88`、`tc_cvm.go:82`）。

### I8-10 【低】Lighthouse `GetSnapshot` 无页数上限 / 进度守卫

- **问题**：`provider/tc_lighthouse.go:67-124` 只有 Offset 递增与"本页不足一页"判据；服务端持续返回满页时循环无界（仅受网络与 SDK 超时约束）。
- **范围说明**：P3-25 与"分页总预算"候选均只覆盖 ECS 两条路径；Lighthouse 未被任何 finding 覆盖，合同亦未要求。

### I8-11 【低】SWAS 分页：跨页更小/为 0 的 `TotalCount` 会提前 `proven`

- **问题**：`provider/ali_swas.go:95-97` 每页覆盖 `totalCount`，`:113` 用它判 `proven`；若后续页返回更小或 0 的 `TotalCount`，会以**截断快照成功返回**。
- **实测**：TotalCount 3→1 时返回 2 条；TotalCount 0 且有 1 条时返回 1 条；两者 `err=nil`。
- **依赖**：云端自相矛盾输入，真实云未复现；与 F5"不能证明完整却成功返回"同类。

### I8-12 【低】Dry Run 失败后渲染绿色「无待变更规则」成功空态

- **问题**：`webui/frontend/src/composables/useDryRun.ts:17-18` 每次 `run()` 先清空 `results`/`warnings`，`:23` **仅成功分支**设置 `lastRunAt`；失败后 `hasRun` 仍为 true → `DryRunResults.vue:129-131` 在空结果时渲染 `type="success"` 的「无待变更规则」。
- **触发**：先成功执行一次 Dry Run，再使其失败（409 并发冲突 / 500 / 网络错误）。
- **状态**：本轮新发现（审计报告 0 命中），两个小组各自用运行时探针复现；仅 toast 报错会消失。
- **相关**：零已配置目标/零结果也走同一文案（未区分，见 I8-13）。

### I8-13 【低】Dry Run 零目标/零结果未与"无待变更规则"区分

- **问题**：`DryRunResults.vue:129-131` 对"未配置目标"与"已跑但无待变更"使用同一成功文案；R7-03 的语义是"所有已配置目标各返回一项"，零目标即零结果。

### I8-14 【低】旧描述身份实现仍随包发布且仍复现原缺陷语义

- **问题**：`provider/common.go` 保留第二套 key 与旧描述身份路径：`Diff`（`:139-192`，`:152` 以 `Description` 相等判归属）、`buildDesired`（`:207-251`，`:224-228` 静默 `continue` 跳过 IPv6/ICMPv6）、`OwnedRules`（`:68-80`）、`normalizePortForCompare/keyOf/keyOfAction`（`:96-131`）。
- **实测**：该路径仍会① 空 comment 碰撞时把同目标另一条规则列入 `ToDelete`（`ToAdd=1 ToDelete=1`）② Lighthouse `ICMPv6` 与期望 `ICMP` 不收敛（`1/1`）。
- **当前风险**：无生产消费者（`grep '\bDiff('` 唯一调用者全在 `_test.go`），但**无门禁阻止重新接线**；与 `provider/plan.go:20` 自述"全仓只有这一个 canonical key 层"矛盾。
- **附加**：`PlanTarget` 层的 TCP+UDP 拆分（`plan.go:471-474`）**无任何仓库测试**（grep `TCP+UDP` 只命中旧 `Diff` 路径与 config 校验）。

### I8-15 【低】注释/死赋值与实现不符

| 项 | 位置 | 问题 |
|---|---|---|
| a | `webui/api/export.go:88-95` | 注释仍是 P2-04 修复**前**的发布/唤醒顺序（`7acf303` 未包含该文件） |
| b | `provider/plan.go:742` | 注释称 CVM"候选间索引重复 → deferred"，实际校验在 `provider/tc_cvm.go:213-215` 且返回**硬错误** |
| c | `internal/health/push.go:210-216` | 首次 `buildPushURL` 返回值被 `_ = target` 丢弃（死赋值，仅作占位校验） |
| d | `provider/plan.go:153-154,655` | `TargetPlanInput.AddStateUnknown` 全仓**从未被置 true**，合同条款无执行点（当前不可达） |

### I8-16 【低】测试夹具与断言残留

| 项 | 位置 | 问题 |
|---|---|---|
| a | `notifier/inflight_test.go:89-98` | 仍丢弃 accepted `net.Conn`（与 R7-06/R7-07 同模式）；因断言放宽（`:120-132` 任意错误均可）**不 flaky**，但丧失"应用层 deadline 生效 vs 连接被重置"的判别力 |
| b | `internal/health/push.go:48` + `push_test.go:459` | **订正（G-10 实测）**：该断言**不是"永真/无判别力"**——在 Push 失败路径注入一次 `p.bus.Publish(...)` 后 **5/5 FAIL**（`push_test.go:460: 实际 2`）⇒ 断言**可判别**。真正的问题是**死字段**：生产 `p.bus` 恒为 nil、`push.go` 从不读它、`run.go:155-164` 也不传 `Bus`。应归入 §4 冗余项（并建议补正向控制，见 I8-42⑤） |
| c | `syncer/stop_gate_test.go:200` | 断言 `p.calls.Load() != 2`，但失败文案写"want 1"（文案陈旧） |
| d | `notifier/email.go:224-227` / `:229` | 丢弃 `c.Close()` 错误（未计入 11 出口）；忽略 `Extension("STARTTLS")` 错误（既有行为） |
| e | **`webui/api/alertset_test.go:216 TestApplyCandidateWithoutRuntimeIsNoop` 名实不符** | 该用例的 `Candidate{}` 其 **`State` 为 nil**，`applyCandidate` 在首个守卫（`webui/api/deps.go:129-131`）即返回，**从未走到用例名声称的"无 Syncer/Runtime"分支**。变异"只在 else 分支 panic" → 原用例 **PASS**，副本探针（真实 State + `&Deps{}`）**FAIL** |

### I8-17b 【低 · 真实产品缺陷】`NewResolver("::1")` 的 IPv6 字面量上游不可用
- **问题**：`dns` 包对不含端口的地址自动补 `:53`。对 **IPv6 字面量**（如 `::1`）会补成 `"::1:53"` → `net.SplitHostPort` 报 **`too many colons in address`** ⇒ **IPv6 字面量 DNS 上游不可用**（域名形式 `[::1]:53` 应可用）。
- **证据**：G-10 副本探针实测基线 `::1:53 → too many colons`；删除自动补 `:53` 的逻辑后 `TestNewResolver_PortAppend` **PASS** 而副本探针 **FAIL**（`missing port in address`）⇒ **注释声称的补端口行为零守护**。
- **影响**：`AGENTS.md` §四允许自定义 DNS 服务器；若用户填 IPv6 字面量（无方括号/端口），配置可保存但解析必然失败，且失败表现为地址解析错误而非明确提示。
- **状态**：**成立，本轮新发现**；归属 `dns` 包，建议同时补一条"IPv6 字面量 + 端口补全"的判别性用例。

### I8-17 【信息】口径需限定（非缺陷）

| 项 | 说明 |
|---|---|
| a | `provider/ali_ecs.go:108/111` 同步路径错误文本**内嵌原始 NextToken**（扫描路径 `provider/scan.go:209` 刻意不含）→ "错误不含原始 token"**仅对扫描路径成立** |

---

## 1b. 独立复核新发现（G-12a · `syncer/syncer.go`，2026-10-08）

> 该文件是审计自认"第二双眼睛未完成"（§11 行1633 / §13 行1713）的两个高危文件之一。本轮独立复核基线：`syncer.go` 1007 行、sha256 `5be02dc5…`，与 HEAD `fd6ed7e` 逐文件一致。**主体结论 Q1/Q2/Q3/Q4/Q6 = A**（Run 状态机 6 类相位无丢轮/重复轮；**起轮路径恰好 4 处** `syncer.go:246/287/306/340` 且每处前均有 `beginRound`、**无第五条**；非测试代码仅 2 处显式丢弃 error（`:230`、`:706`）均有意且注释在案；Dry Run 与正式同步**共用同一 planner**（4 个 `PlanTarget` 调用点输入一致、无旁路）；轮内零 `Snapshot` 重读），**Q5 = B**。以下为报告未登记的新发现。

### I8-17c 【中 · B】未进入规划阶段的 attempt 会**抹掉已确认的 `unsupported` 明细**（与 I8-02 同机制、不同字段）

- **问题**：与 `cleanup_candidates/deferred` 同一处覆盖赋值族——`syncer/target.go:104-107` 覆盖 + `:154-157`（S0 早退）⇒ 最终 `failed` 目标事件的 `unsupported=[] / skipped=0`，**已确认的平台不支持明细被抹掉**。
- **报告覆盖情况**：审计行 `994` 只登记了同机制的 `cleanup_candidates/deferred`（且标为待裁决）；**`unsupported` 未登记**。
- **争议点**：`syncer/target_retry_test.go:120` 的注释已定案"只保留最终 attempt"，而审计行 `994` 对同一赋值族按"保留前次"待裁决 ⇒ **同一路径存在两套期望**，需你裁决（见 I8-30a）。
- **判别力**：独立复核构造 F2 形状的修复后**既有正式用例全套仍绿**，探针转绿、其余对照仍红。
- **状态**：**成立**（确定性复现 + 红→绿对照）。

### I8-17d 【低 · B】`setSyncEnabled`（`Pause`/`Resume`）读-改-写未持 `s.mu` → 丢失更新

- **问题**：`setSyncEnabled` 的读-改-写未持 `s.mu`，窗口内发起的 `Resume` **被覆盖（8/8 确定性）**；更严重的是窗口内由协调器发布的**已提交配置（TAG）被陈旧快照回滚（8/8）**。
- **可达性**：**生产当前不可达**——`webui/api` 侧的 `Syncer` 接口成员已删除，无生产调用方。
- **报告覆盖情况**：审计 §4 只要求"必须保留"`Pause()/Resume()`（Build6 约束），**未评估其并发语义**；`grep setSyncEnabled` 在报告中 **0 命中**。
- **状态**：**登记为"已知残留风险（当前无生产调用方）"**；处置见 I8-41a。

### I8-17e 【信息 · E 不可达】`dns_round.go:38-40` 对不在本轮快照的域名 nil 解引用

- **问题**：该处对不在本轮快照中的域名会发生 nil 解引用 panic；**当前调用链不可达**，属**缺失前置断言**。
- **判别力**：静态不可达判定（E），未构造可变现路径。

### I8-17f 【低 · B 效率】`runRound` 在每个目标后（含同云分组最后一个）无谓 `sleep(rateLimitInterval)`

- **问题**：分组内最后一个目标之后仍等待一个完整间隔 ⇒ 单目标分组每轮多等 1 个间隔（Lighthouse/SWAS **5s**、CVM/ECS **200ms**），该等待**计入 `duration_ms`** 并**推迟 ticker Reset**。
- **对照**：方向与审计 P3-22 对 Dry Run"取消末尾等待"的处理**相反**，正式同步侧仍有残留。
- **状态**：**成立**（红→绿对照通过）。

### I8-17g 【低 · 测试可信度】`TestStateAppliedHookFiresOncePerApplyState` 偶发红灯根因（补充既有登记）

- **根因**：夹具观察竞争——hook 内 `Add(1)` 与 Store 之间**无同步** + 轮询式观察。**审计行 1807 已记录同一根因**；本轮补充：确定性复刻探针、channel 同步重写后 **50/50 绿**、隔离 `-count=200` **0 失败** vs 重载 `-count=20` **恰 1 次红** ⇒ **"单项 20 轮通过"不构成稳定证据**（也与 I8-25 的口径一致）。

---

## 2. 未完成审阅 / 证据不足

### I8-18 `GET /api/alerts` 4 次非事务读的撕裂窗口 —— **已确定性复现，判定 C（建议修复）**

- **问题**：`handleGetAlerts`（`webui/api/alerts.go:54`）顺序执行 **4 次独立读**：`:55 GetAlertPolicy`（`config/store.go:907`）→ `:60 GetAlertEmail`（`:797`）→ `:65 GetAlertWebhook`（`:849`）→ `:70 GetUptimeKumaPush`（`:970`）。四个 `Get*` 都把 **`*sql.DB`（连接池）** 传给 `load*`（`:786/:838/:895/:959`），**无 `BeginTx`/`BeginReadOnlyTx`** ⇒ WAL 下每条语句自成隐式读事务，**不在同一快照**。
- **确定性复现**：在第 3 读（webhook）与第 4 读（push）之间同步提交一次完整四部分写事务 → 响应为 `policy=old, email=old, webhook=old, push=new`（两读分属两个已提交版本），**一次 200 响应内部自相矛盾**；`-count=5` 稳定复现。
- **写侧对照**：`PUT /api/alerts` **是单事务**（`alerts.go:276-278` → `coordinator.go:73/91`：锁 → `BeginTx` → `ReplaceBusinessAlertsTx`（`store.go:1334`）→ 同事务读快照 → `Commit` → commit 后无失败发布）。判别性验证：给 `uptime_kuma_push` 挂 `BEFORE INSERT RAISE(ABORT)` → PUT 返回 500 且**四部分全部回滚**。
- **前端会把它固化**：`Alerts.vue:117-131` 用 GET 响应**整体覆盖 4 个 ref**，`:143-152` **无条件序列化全部 4 个对象**（无 diff/合并）；校验器 `isAlertsData`（`:55-70`）**只校验对象内部字段名/类型、不校验跨对象版本一致性** ⇒ 撕裂响应必然通过并置 `loadState='ready'`，用户改一个开关再保存即把**混合快照整体写回**。`loadState`/`saving` 守卫都拦不住。
- **测试守护**：**零**。命中 `GET /api/alerts` 的 6 处测试全为单线程同步取值断言（`alerts_v3_test.go:51/339`、`alertset_policy_test.go:115`、`settings_alerts_test.go:133`、`redact_test.go:107`、`cachepolicy_test.go:199`）；唯一涉及原子性的是 `TestPutAlertsRollbackOnPushWriteFailure`（`alerts_v3_test.go:346-381`），断言的是**写侧**回滚。**判别力实测**：未修复实现 + 确定性注入下，整包 `webui/api` 既有测试**全绿**，只有新写的一致性用例变红。
- **实际可达性**：无并发写入时**不可发生**（实测恒同版本）；`sync_enabled=false` 是**无关变量**（四种写者——PUT/import/reset/*——与同步引擎独立，同步引擎也不写这四张表）。并发夹具（4 writer × 25 PUT + 3 reader，带 `-race`）下未修复实现 **544/546/495 次 GET 中分别 116/131/105 次四部分不一致（约 21%～24%）**；「同一只读事务取快照」变体**连续 5 轮 0 次**。
- **修复成本极低**：改用 `Store.BeginReadOnlyTx` + 「事务内读四对象」取同一快照（与 version 3 导出同模型），**契约零变更**——实测 200 / 字段集合（policy 4、email 9 含 password、webhook 3、push 3）/ `Cache-Control: no-store` / `Content-Type` / 默认值补齐 / 四对象非 `null` / 500 错误出口**全部不变**，且仓库既有 12 项告警测试与整包 `webui/api` 在该变体下全绿。实现注意点：`BeginReadOnlyTx` 不进入协调器锁、不与 PUT 写事务互阻（已实测）；`_txlock=immediate` 只作用于非 ReadOnly 事务，快照在事务内第一次读建立，故四部分必须都在该事务内读；API 层需新的「事务内读四对象」入口，避免把 `*sql.Tx` 泄漏到 `webui/api`。
- **最坏后果**：静默回退某个**未被触碰**的渠道配置（例如刚更新的 Webhook URL / Push URL+interval 被退回旧值），界面与日志**无任何提示**。
- **状态**：**成立，可复现**（评级 C：非数据完整性/非提权，库内任一时刻仍自洽）。修复与否见 **I8-34**；若决定不修，应在 `alerts.go` 与 `AGENTS.md` 显式登记"读侧非原子 + 前端整体回传"的已知边界与"不支持多写者并发编辑告警配置"前提；**无论修不修都建议补一条判别性回归**。
- **剩余不确定性**：未在真实部署/浏览器复现（全程 `/tmp` 副本 + `httptest`）；~21% 只是并发夹具量级参考；未排查其他"多读拼装响应"的 handler；未直接观测每次读的物理连接（但"读之间快照不延续"已由"无跨读长事务 + 注入提交被第 4 读看到"实证）。

### I8-19 P2-06 的判别力证据 —— **✅ 已闭环：判别力成立**

- **组件测试（当前实现）**：`npm run test:alerts` **5/5 pass，EXIT 0，0.72s**。
- **负向控制（旧实现 `Alerts.vue` 回退 `064b794^`，SHA256 `dcc88649…`）**：在仓库外副本执行 `node --test --test-timeout=15000 tests/alerts-load.test.mjs` → **EXIT 1，0 pass / 4 fail / 1 cancelled(timeout)，15.35s 内出结果**。其中**两条纯行为红**（不夸大）：①「不完整载荷之后不得发起 PUT」`2 !== 1`——GET 返回 `null`/`[]`/缺组/类型错误时旧实现**照样发 PUT**，把默认或残缺表单整体写回，**正是 P2-06 的原始危害**；②「重复保存只有一个在途 PUT」`2 !== 1`（无 `saving` 守卫）。另两条红属符号缺失型（`isAlertsData`/`loadState` 不存在），已单独分级。
- **上次" >120s 未结束"的根因已定位**：不带逐用例上限时（`node --test tests/alerts-load.test.mjs`）在 180s 硬墙被杀、EXIT 142、**输出 0 字节**；原因是**用例 1 永久挂在 `tests/alerts-load.test.mjs:72` 的 `await state.save()`**——旧 `save()` 无状态守卫，在 loading 态发出 PUT 并 await 一个永不 settle 的 mock promise，而 `node:test` 默认逐用例超时无限且顶层用例串行 ⇒ **用例 2～5 根本不开始**。**因此"旧实现不变红"从来不是判别力问题，而是夹具挂起。**
- **逐行复核（HEAD 版 `Alerts.vue`）三项待确认全部成立**：`loadState` `:52`；`isAlertsData` `:55-70`（`:57/:62` 拒 `null`/非对象/数组，`:59-60` 逐字段 `typeof`，不误拒空串/false）；`load()` `:117-131`（`:118` 每次重锁 loading、`:121` 校验失败即抛、`:122-125` 整组替换、`:126` ready、`:127-129` error）；`save()` `:136-139` 加载态守卫 + `:140` 在途守卫 + `:146-151` **显式四对象载荷（无 spread）** + 模板 `:293` `disabled`。**结论**：加载失败后保存入口双重禁用；PUT 载荷恰为四对象（字段数 4/9/3/3，与后端 `alertsRequest` 指针集合一一对应）；`theme` 等页外字段不可能进入载荷（逐键构造 + `AlertPolicyConfig.HealthTimeout/HealthTimeoutText` 与 `UptimeKumaPushConfig.Interval/IntervalText` 均带 `json:"-"`，GET 走专用响应结构 + `decodeJSONStrict` 兜底）。
- **连带发现（见 I8-102）**：`package.json:7` 的 `test:alerts` **未设逐用例超时**，任何"loading 态发 PUT"回归会让门禁**永久挂起**而不是变红。

### I8-20 P3-12 的 WAL/SHM 时序判别性实验 —— **✅ 已闭环：判别性成立**

- **探针设计**（仓库外副本，显式 `FWALIZER_DATA_DIR`，走 `LoadDeploymentConfig→OpenStore` 即 `run.go:86-87` 同一路径）：夹具为真实 db + 非空 WAL(20632B) + shm，且标记 `settings.wal_probe` **只在 WAL 已提交帧里**；把 `-wal`/`-shm` 预置 **0400**；观测 OpenStore 成败 + 标记是否仍可见 + **重开后写入** + 终态权限；另有 0600 夹具自检排除夹具缺陷。
- **结果**：sanity PASS；**当前实现 EXIT 0 PASS**（0400→0600、标记可见、**写入成功**、终态全 0600）；**变体 A（删除"打开前"的 `chmodStoreAuxFiles`，只留初始化后那次）EXIT 1 FAIL**：`开始 Schema 迁移事务失败: attempt to write a readonly database (8)`，终态 wal/shm 仍 0400；**变体 B（移到 `sql.Open` 之后、首条 SQL 之前）EXIT 0 PASS** ⇒ 证明 `sql.Open` 不做文件访问，**真正的首次访问边界是 `store.go:163` 的 PRAGMA**。
- **⚠️ 技术订正**：顺序错误时的失败**不是 EACCES**——SQLite unix VFS 在 `O_RDWR` 失败后**回退只读打开**，故失败推迟到**首个写事务**，表现为 `SQLITE_READONLY`（`open_err_eacces=false` / `open_err_readonly=true`）。**含义**：只查"`OpenStore` 是否报错"**不够**，必须加"**打开后写入**"观测点，否则会把"只读打开成功"误判为绿。
- **既有证据为何无判别力**：`permissions_test.go:142-167` 用的是 **0644/0666（属主可写）**，正反两种顺序都会绿——只证明"迁移有效"，**不证明"迁移在首次访问之前"**。
- **容器复跑**：Docker 可用，用临时命名卷（用后 `docker volume rm`，实验前后 `docker volume ls` 均为空，**未触碰任何既有卷**）、镜像 `fwalizer:i01-linux-amd64`、强制 `--user 1000:1000`（确认 `uid=1000(appuser)`）——当前实现 PASS、变体 A FAIL，与宿主逐字段一致。
- **边界**：0400 判别对 root/`CAP_DAC_OVERRIDE` 无效（故容器必须非 root）；未测 0000、未分离只读 WAL/只读 SHM；容器为 amd64 在 arm64 宿主兼容层执行，**非原生 amd64**。

### I8-21 P3-04"锁内先历史入队再注册"微属性

- **状态**：**仅结构性证据**。把"注册在前、入队在后"放在**同一把锁内**行为完全等价（测试仍绿）；忠实旧缺陷的 overlay 触发 `send on closed channel`（**夹具伪影**，非行为红灯）。

### I8-22 P3-10 出口④（日志渲染错误）的判别证据

- **状态**：**E（证据不足）**。生产实现按合同正确（`webui/api/logstream.go:121-134`，err 非 nil 在加锁前 return），但：
  - 审计 `fwalizer-audit-final1.md:1081` 声称的正式判别性测试 `TestRenderFailureNoPublish` **全仓 grep 零命中**（不在当前工作树）；
  - 仓库内**无任何测试能让 `renderLine` 失败**（其内部硬编码 `bytes.Buffer`、无 writer 接缝，探针实证 err 恒 nil）；
  - 把"忽略 renderLine 错误"作为 overlay 后，相关测试**仍全绿**。

### I8-23 A20 的修复前红灯负向控制

- **状态**：**未执行**（需 `b38678a` 之前的 `syncer/syncer.go` 构造 overlay）。现有证据为静态调用点核验（4 处 `beginRound` 门控）+ 现行 `race ×5` 6 项全绿。

### I8-24 P1-01 的独立 overlay 判别力

- **状态**：**E（自评）**。独立复核组只做了 20 个自建探针（19 绿 / 1 红为其自身断言错误），**无独立红→绿 overlay**。

### I8-25 全量门禁的完整复现

- **状态**：**部分**。构建专项因安全边界**整包排除 `dns`**，只跑 11 包 race；各组普遍只做 `-count=1` 或定向 `-count=20`；审计声称的"全仓 12 包 race 连续 3 轮"**本轮未复跑**。
- **口径**：单次绿色不外推长期稳定；`dns` 包不得登记为通过。

### I8-26 其他未判定项

| 项 | 状态 |
|---|---|
| `git cat-file -t 3ee1ae87…` 间歇解析失败 | 未解释（文件存在 1170B，同期 `for-each-ref` 可解析为 tree）；需连续采样 + `git fsck --full` |
| 仓库外副本软链与敏感文件检查 | 未执行（`/tmp/fwa-verify.*` 等未检查是否存在指向仓库的软链、是否被写入真实数据库/凭据） |
| 网络层取证 | 无法判断是否发生过真实外部连接（未采集 established 快照、未抓包） |
| `.git/objects` 基线后新增 8 个松散对象 | 内容与基线前既有 Codex 检查点 tree 逐一相同，未改引用/索引/HEAD/工作树；**写入方无法归因** → "仓库目录内零文件新增"不成立 |

---

## 3. 待裁决事项

| 编号 | 争点 | 候选方案 | 影响面 | 不裁决的后果 |
|---|---|---|---|---|
| **I8-30** | **TODO-008 / I8-02**：failed 路径下 `cleanup_candidates`/`cleanup_deferred` 如何表达 | ①**保留前次可信残留**（与 `added/deleted` 累加语义对称）②**表达为未知**（如 `-1`/`null`/显式"未知"字段） | `syncer/target.go:105/107/319-323`、`webui/api/logwriter.go:99-104`、`RoundSummary`、目标/整轮事件、SQLite 详情、PT-I7-07 观察项 | 真实残留对操作者不可见；`deleted > candidates` 长期自相矛盾；PT-I7-07 无判定基准 |
| **I8-31** | **TODO-003**：CVM `DescribeSecurityGroupPolicies` 调用失败的错误分类 | ①包装为 `ErrSnapshotIncomplete` ②写入契约（明确归"普通/可重试错误"） | `provider/tc_cvm.go:248-250`（当前未包）vs `:254/:262`（已包） | 同类错误两种分类，调用方无法一致判定；配额路径的失败语义不确定 |
| **I8-32** | **TODO-001 / I8-01** 的修法 | ①unsupported-only 时 `coverage_ready=false` ②重新定义该字段语义（区分"已覆盖"与"不可实施"） | `provider/plan.go:655`、Dry Run DTO、前端 `coverage_ready` 消费、`AGENTS.md:285` | P0 编号长期悬空，误报风险保留 |
| **I8-33** | **I8-05**：Push `Wake` 是否对齐 Syncer 的"同间隔保留剩余等待" | ①对齐（改名/改语义：仅 URL/启用状态变化才重新计时）②显式接受现状并在 `AGENTS.md`/Build7 写明代价 | `internal/health/push.go:152-165`、`webui/api/deps.go:168`、`AGENTS.md:236` | 用户持续保存时心跳被推迟，Kuma 可能误判 DOWN |
| **I8-34** | **I8-18**：`GET /api/alerts` 撕裂窗口是否修复 | ①改"只读事务取同一快照" ②接受现状（内部使用、单客户端）并写入文档 | `webui/api/alerts.go`、`webui/api/store` 读路径、`Alerts.vue` 回传 | 读一致性缺口保留；若前端整体回传即会用旧值覆盖新值 |
| **I8-35** | **I8-08 / I8-09**：SWAS 扫描缺失集合与 Lighthouse/CVM 扫描空响应的处置 | ①统一为桶 `ErrSnapshotIncomplete` 保守失败（与 P3-26 一致）②仅补 nil 守卫防 panic ③显式接受现状 | `provider/scan.go:72-78/113-119/153-156`、`webui/api/scan.go:49-69` | 扫描可能以"成功"覆盖缓存为不完整结果；或扫描请求 panic 中断 |
| **I8-36** | **I8-03**：构建阻断的修法 | ①给 `Makefile` 的 `test`/`vet` 加 `frontend` 前置 ②在 CI/文档中明确"裸 Go 命令需先构建前端"③其他 | `Makefile:9-13`、`webui/embed.go:5`、`.gitignore:5`、AGENTS §八 | 清洁检出无法跑 Go 门禁；"本地绿色"不可复现 |
| **I8-37** | **I8-04**：`dns` 包真实外网依赖 | ①改为本地 DNS mock ②统一加 `-short` 并为两种模式定合同 ③接受现状 | `dns/resolver_test.go`、`Makefile:10`、CI workflow | 门禁在无外网环境必然变红；"全量绿色"含外网 I/O |
| **I8-38** | 分页操作总预算（研究候选，未实施） | ①页间检查总预算 ②按剩余预算限制每页 ③隔离操作级 HTTP 客户端 | `provider/ali_ecs.go`、`provider/scan.go`、SDK `RuntimeOptions`、`getDaraClient` 全局 `sync.Map` | 持续返回全新 token 的服务无总时限（`AGENTS.md:189/194` §七**既无总预算条款也未排除**，故实施无需先改强要求，但须新增条款并定义预算耗尽语义＝不完整失败＋零半截＋S0/S1 零删除＋扫描保留旧缓存）。**本轮已取得**：三候选对比表（①复杂度低、对共享 ClientPool 无影响、推荐为首选最小实现；②**机制上必然**扩大 SDK 全局池键空间——`core.go:71` 池无 Delete/TTL/容量、tag 含 Read/Connect 值——且 `core.go:349-357` 每请求覆写共享 `httpClient.Timeout` 存在同 tag 并发覆写窗口，**不建议直接实施**；③需自证隔离与回收，仅在软预算不可接受时启动）；**零值陷阱**：`RuntimeOptions` 传 0 ⇒ `Timeout=0`＝无超时（比现状更差）；单页超时＝**每请求**（`dara/core.go:533-535/588-594`、`:354`），与操作总时限确是两回事；**有界 mock 实测**：1000 页全新互异 token → 同步 1001 请求/152ms、扫描 1001 请求/140ms，仅因 mock 第 1001 次固定 400 才停止；参考实现①在每页 150ms 场景下 20 页/3.031s 按预算停止（**证明"预算耗尽后零下一页"可判别**） |
| ~~**I8-39**~~ | ~~`MultiHandler` 若降级为非导出~~ | **✅ 前提已消失，不再需要裁决**（2026-10-08 G-9 复核）：项目自写类型已在 P3-13（`0818197`）删除，现为标准库 `slog.NewMultiHandler`（`app/logutil.go:41`），**不存在"降级为非导出"这个动作**；且 `AGENTS.md:282` 已改写为"日志多路复用使用标准库 `slog.NewMultiHandler`……"，原"需先改强要求"的前置条件已以满足强要求的方式消解 | — | 无需行动 |
| **I8-41** | `Syncer.Pause()/Resume()` 的保留边界 | ①保持实现（Build6 约束）②其他 | `syncer/syncer.go`、`webui/api/deps.go` | 删除会破坏 Build6 约束 |
| **I8-41a** | **`Syncer.Pause()/Resume()` 的并发语义**（由独立复核 G-12a 的 NF-1 提出，与 I8-41 是**两个不同问题**：I8-41 问"是否保留"，本条问"保留后如何保证正确"） | ①**保持导出但按 F1 形状加锁**（读-改-写持 `s.mu`）②**改非导出**（消除跨包误用面；包内测试仍可用）③**登记为"已知残留风险（当前无生产调用方）"并在 `AGENTS.md` 注明** | `syncer/syncer.go setSyncEnabled`（读-改-写未持 `s.mu`）、`webui/api/deps.go` | 已实测：窗口内 `Resume` 被覆盖 **8/8**、窗口内协调器发布的**已提交 TAG 配置被陈旧快照回滚 8/8**。生产当前不可达（接口已删），但**未来任何重新接线都会带入该竞态**；审计全程零覆盖（`grep setSyncEnabled` 0 命中） |
| **I8-30a** | **同一赋值族的两套期望**：审计行 `994` 对 `cleanup_candidates/deferred`（及同机制的 `unsupported`，见 I8-17c）标"待裁决：保留前次可信残留 vs 表达未知"，而 `syncer/target_retry_test.go:120` 的注释**已定案"只保留最终 attempt"** | ①明确"**未规划 attempt 不得覆盖已确认明细**"（独立复核 F2 形状：探针转绿、**既有正式用例全套仍绿**）②维持"只保留最终 attempt"并**同步修订审计行 994 的待裁决标注** ③按字段区分（`cleanup_*` 保留、`unsupported` 只留最终） | `syncer/target.go:104-107`、`:154-157`、`syncer/target_retry_test.go:120`、审计行 `994` | 两套期望并存会导致实现与测试互相矛盾：按①改则需改测试注释，按②改则 I8-30 的裁决空间消失 |
| **I8-42** | **§4 清理类裁决**（G-9 四分判定，逐项独立裁决） | ①**零成本改非导出**（消费者全在同包，共 8 个符号）：`GetSettingsTx`、`TargetExistsTx`、`RuleExistsTx`、`AlertManager.Current`（返回未导出类型 `alertSet`，导出性无价值，**性价比最高**）、`Server.ServeStarted`、`Pusher.InFlight`、`InFlightLimiter.InFlight`、`ErrNoSnapshotLoader` ②**可安全清理（5 项）**：`Config`+`LoadConfig`+`ToConfig`（**必须合并为一个原子改动**，需改 4 个测试文件：`syncer/syncer_test.go`、`config/store_tx_test.go`、`config/store_corrupt_test.go`、`webui/api/settings_policy_test.go`；`RuntimeConfig` 前 11 字段与 `Config` 逐一对应，机械替换即可）＋ `EventRuleChanged`、`ResolvedIPs`、`SyncEvent`(TS)、`clearAllCache`（后 4 项零测试改动）③**前端 2 项只能从返回对象移除、不能删函数**：`useDryRun.error`、`useSettings` 的 `settings`/`load`/`refreshCredentialState`（`load` 被 `applyTheme` 调用、`refreshCredentialState` 被 `load`/`refresh` 调用、`settings` 被四处内部使用）④**暂缓**：`Store.SetSetting`/`SaveAlertEmail`/`SaveAlertWebhook` 改非导出波及 **14 个跨包测试文件**（`webui/api` 7 个 + `main_test.go` + `permissions_process_test.go`），**报告低估了代价** ⑤**补正向控制而非删字段**：`PusherDeps.Bus`（在测试内主动 `pub.Publish` 一次并断言 `count==1`） | `config/config.go`、`notifier/bus.go`、`provider/provider.go`、`webui/frontend/src/types.ts`、`useScannedResources.ts`、`useDryRun.ts`、`useSettings.ts`、`config/store.go`、`internal/health/push.go` | 均为低风险清理；**一旦实施必须走 `vue-tsc`（前端唯一门禁）与全仓编译**，且不得削弱 §7 正向控制 |
| **I8-43** | **`bundle_v3.go` presence map 导致的错误文案漂移**（比报告更严重） | ①改为与同文件 `normalizeBundleSettings` 一致的**顺序 `if` 链**（**强烈建议修**：实测同一次非法请求 **300 次在单进程内**即观察到 3 种（policy，3 字段缺失）与 8 种（email，8 字段缺失）文案；**修复零测试破坏**——`grep '字段缺失' --include=*_test.go` 4 处命中全为子测试名/注释，断言只校验状态码）②接受现状并文档化 | `webui/api/bundle_v3.go` | 同一非法请求的错误文案不可复现，影响排障与用户沟通；不修则每次请求文案可能不同 |
| **I8-44** | **§4 其余四项小裁决** | ①`mustDuration`/`mustThreshold` 静默 0：**首选改 `panic`（显式不可达断言）**，次选文档化默认值 ②`health.go:195` 的 `Policy == nil` 检查与 `Evaluate` 不一致：**删该检查**与 `Evaluate` 对齐（生产 `run.go:138-143` 恒传 Policy，且 `Evaluate` 先于 `TriggerEnabled` 调用，nil 时会在 `supervisor.go:128` 先 panic） ③`buildPushURL` 重复计算：抽 `validatePushURL` 并保留"先校验后占在途名额"顺序（优先级最低） ④**`fs.Sub` 吞错：至少补一行 `slog.Error`**（零风险，只增可观测性；且"embed 保证成功"**仅当 dist 已按预期构建**时成立） | `config/runtime.go`、`internal/health/health.go`、`internal/health/push.go`、`webui/server.go` | ①③④为可观测性/一致性改进；②删检查需确认与 `Evaluate` 的调用顺序不被破坏 |
| **I8-45** | **前端类改动的验收标准** | 必须写明为"**`npm run build`（= `vue-tsc && vite build`）通过**"：`webui/frontend/tests/` 的 4 个测试用 **mock** 顶替 `useSettings`（`p316-state.test.mjs:36` 的 mock 只列被用成员），故前端清理类改动**既不会破坏这些测试、也不会被它们检出** | `webui/frontend/tests/`、`package.json` | 若误把"前端测试通过"当作验收，清理类改动可能留下类型错误 |
| **I8-46** | **version 3 是否收紧两个绕过维度**（G-12b I8-111） | ①**收紧**：显式检测重复键与大小写/转义变体，非精确 `"version":3` 一律 400（需自建 token 级解码或预扫描，复杂度中等）②**接受现状并文档化**：在 `AGENTS.md` §9.1 与 `bundle_v3.go` 注明"`encoding/json` 语义下 last-wins 与大小写不敏感匹配是已知边界" | `webui/api/bundle_v3.go`、`webui/api/decode.go`、`AGENTS.md` §9.1 | 重复键变体（`{"version":2,…,"version":3}`）会让"拒绝 version 2"的合同在**字面上被绕过**；若不收紧又不文档化，后续复核会反复把它当新缺陷 |
| **I8-47** | **`webui/api.Deps.Store == nil` 时 import/export 直接 panic**（G-12b NF-3） | ①按 P3-14 的六处口径统一为 **503**（`Store` 缺失即"依赖未接线"）②接受现状（当前接线不可达） | `webui/api/bundle_v3.go`（import）、`webui/api/export.go`、`webui/api/deps.go` | 与 P3-14 已修的六处 nil→503 **口径不一致**：同样"依赖未接线"，六处返回 503、这两处 panic（当前不可达，但重新接线即暴露） |
| **I8-48** | **`normalizePort("")` 返回错误导致的默认值语义**（G-9 补充，与 I8-44④ 同族） | ①统一到 `config.Default*` 常量（DDL 改为引用常量，消除 I8-113 的漂移）②保留字面量并在测试中断言"新建库与升级库默认值一致" | `config/store.go:223/235/313`、`webui/api/bundle_v3.go:353/363`、`config/validate.go` | 当前**新建库与升级库可能得到不同默认渠道**且测试零告警（I8-113）；不裁决则该漂移面长期存在 |

---

## 4. 未派发 / 待启动的专项（只读任务，指令已备）

| 编号 | 专项 | 内容摘要 | 依赖 |
|---|---|---|---|
| I8-50 | **独立第二双眼睛 · `syncer/syncer.go`** | 审计自认 §11 未完成项：状态机 select 组合、起轮路径是否恰 4 处、吞错点、Dry Run 是否共用 Planner、锁序、快照每轮一次 | 无 |
| I8-51 | **独立第二双眼睛 · `webui/api/bundle_v3.go`** | 同上：version 3 字段集/版本拒绝/对象 null/单事务零写入/decodeJSONStrict 绕过 | 无 |
| I8-52 | **§4 死代码与过度防御四类核销** | ✅可删 5 项、🟡高可信候选（含 11 个仅测试导出）、🔁重复实现 5 项、🛡过度防御 6 项；**硬约束**：`SetBeforeRoundHook` 不得判可删、`Pause/Resume` 不得删、`redact_test.go` 属正向控制、`MultiHandler` 需先改强要求 | 三源引用计数 |
| I8-53 | **§6 测试质量三份核销** | 7 个"永不失败"测试（含一个**已位移的 race-only 用例**，须按"注释自述只靠 race detector"重检）、`waitForNoSMTPData` 纯 sleep、`pub.count()==0` 永真、`InFlightLimiter` 零断言、5 个数值常量无字面量锚点、构建/部署契约零守护、零泄漏断言、`provider/scan.go` 覆盖、前端 8 字段载荷契约 | 无 |
| I8-54 | **§3 内存与资源专项** | 常驻 goroutine/每云厂商上限、3 ticker + 2 timer 的 Stop 覆盖、生产 `io.ReadAll` 现共几处、**`IdleConnTimeout` 全仓零命中（证据不足，不得写成泄漏）**、SSE/订阅/rows/上界 | 无 |
| I8-55 | **§5 生命周期阻塞** | `Server.Start` 失败后 `started` 恒 true / `waitDone` 永不关闭（当前接线不可达）；`supervisor.Stop`/`pusher.Stop` 无 `runStarted` 守卫 → 未启动时永久阻塞 | 有界超时断言 + 接线可达性论证 |
| I8-56 | **§5 附 崩溃面** | `config/runtime.go:137/146 mustDuration/mustThreshold` 静默返回 0 → `time.NewTicker(0)` **会 panic**（当前不可达）；`webui/server.go:305 fs.Sub` 错误被吞 → **静默降级**禁用静态处理器 | `defer recover()` 探针 |
| I8-57 | **§7 正向控制 17 条核销** | 逐条给"实现位置/是否有测试守护/是否被建议触碰"；关键是第③条（只用增量 API）作为"绝不误删非本工具规则"的全部依据 | 产出 17×14 交叉矩阵 |
| I8-58 | **§13 证据边界 + §14 Git 完整性核销** | 17 行逐行当前状态（含因 I8-50/51 而变化的行）；16 项 PT 逐项可追溯性 | 无 |
| I8-59 | **§10.3 历史批次 6 核销** | Makefile 前置、修 `waitForNoSMTPData`、消灭 7 个永不失败测试、补字面量锚点、补构建契约测试、补 pidfile 单测、清 version 2、§4 文档漂移 | 与 I8-53/I8-36 合并 |
| I8-60 | **分页总预算可行性研究** | 三候选对比（复杂度/对共享 ClientPool 影响/对同步请求污染风险/可判别性）+ SDK 源码依据；**不得构造真实无限循环** | 与 I8-38 对应 |

---

## 5. 文档订正项（本轮已执行或待执行）

> 本轮已按用户授权订正下列状态陈述；历史批次的"当时未提交"作为历史事实保留，但**现时状态索引必须与 Git 事实一致**。

| 编号 | 文件:行 | 订正内容 | 状态 |
|---|---|---|---|
| I8-70 | 审计 §0.2 `P3-24` 行 | "尚未提交" → 已提交 `938bb8b` | ✅ 已执行 |
| I8-71 | 审计 P3 清单 `P3-24` 行 | 同上 | ✅ 已执行 |
| I8-72 | 审计 §0.4 `I-06` 行 | "本轮尚未提交" → 已提交 `938bb8b`（P3-23 `9dd5d34`） | ✅ 已执行 |
| I8-73 | 审计 P3 清单 `P3-18` 行 | "尚未提交" → 已提交 `f0997cd` | ✅ 已执行 |
| I8-74 | 审计 P2-07 四处（`L848`/`L865`/`L1561`/`L1584`） | "尚未提交" → 已提交 `ec82d10`（保留历史证据语境） | ✅ 已执行 |
| I8-75 | 审计 §14 `L1717` | "当前基线 = `b84531b`/`d6d208e`/ahead 7/工作树干净" → 现时基线 `938bb8b` + 5 个未提交文件 | ✅ 已执行 |
| I8-76 | 审计 附录 A `L1752` | 同上 | ✅ 已执行 |
| I8-77 | 审计 P3 清单 `P3-06` 行 | 同格双结论 → 标注后半个"🔵 结论已订正"已被 2026-10-02 官方正文推翻，以"只统计入站"为当前合同 | ✅ 已执行 |
| I8-78 | AGENTS.md P3-24 段标题与段末 | 补"历史批次，后已提交 `938bb8b`"；段末"尚未提交"订正 | ✅ 已执行 |
| I8-79 | TODOLIST.md `L315`/`L331` 与统计数字 | P3-18 提交事实；tracked/ignored/`fwalizer` 现状（274 / 2 / 不存在） | ✅ 已执行 |
| I8-80 | Issue7.md §12.6/§12.7/§12.8 三处 | "尚未提交" → `88154cd`/`ba82292`/`333451d` | ✅ 已执行 |
| I8-81 | 审计 P3-24 补记引用他机路径 | `L1932` 的 `/Users/kylechen/.../p324/` 标注不可复现 | ✅ 已执行 |
| I8-82 | 审计 P3-17 范围（两处：补记 `:89` 与文末补记 `:1843`）与 AGENTS.md:65 | "九文件" → 实际**十文件**（含 `5860cef` 新增的 `TODOLIST.md`）；`fwalizer-audit-final1.md:1843` 同时仍写"尚未提交"，已一并订正为已提交 `5860cef` | ✅ 已执行（2026-10-08 复核补漏） |
| I8-83 | TODOLIST TODO-001/003/008 状态 | 与本台账 I8-01/I8-31/I8-30 对齐（仍未完成/待裁决） | ✅ 已执行 |
| **I8-84** | **`ProdTestList.md:9`** | 原写"完整复核仍有 **R7-01～R7-06 待修复/裁决**"→**已过期**（R7-01～R7-07 全部已修复提交：`b80b1b0`/`eab4bea`/`297ccfe`/`b19d271`/`d6d208e`）。该文件是外部验收唯一权威清单，过期会导致执行者误判 | ✅ 已执行（2026-10-08，由素材索引子代理发现） |
| **I8-87** | §3 P1/P3/P5/P8 计数口径 | ① 常驻 goroutine **仍为 5，但成员清单需更正**：`srv.Wait` 与 `serveErrCh` 是同一 goroutine（`run.go:206`），漏计 `webui/server.go:215` 的 `Serve`；每云厂商并发上限 4（`config/config.go:31-34` 四 CloudType + `syncer.go:920-955` 分组 + `wg.Wait`），组内目标串行 ② **"三个 ticker"不成立**：生产只有 2 个 `time.NewTicker`（`syncer.go:233`、`supervisor.go:86`，均 `defer Stop`）＋1 条自续期 `AfterFunc`（`inflight.go:106/125`，空窗口自终止）＋2 个 push timer（`push.go:138/156`，3/3 分支 Stop 全覆盖）＋2 个有界 `time.After` ③ `io.ReadAll` 现为 **3 处**（`webhook.go:139` 16KiB+1、`push.go:267` 16KiB+1、`decode.go:78` 1MiB+1），**旧统计"2 处"已过期** ④ `LogBroadcaster.ring` 是 `[1000]logEntry`，**非** `[1000]string` | 📝 待执行（口径订正，写入时按此改正） |
| **I8-88** | §3 P9 `IdleConnTimeout` | **原"证据不足"应升级为可核实结论**：仓库零命中成立，但 SDK 默认值可查明——腾讯 `common@v1.3.142/client.go:588,599` 克隆 `DefaultTransport` 并设 `IdleConnTimeout=30s`；阿里 `tea@v1.5.2/dara/core.go:533-534` 为 `new(http.Transport)` 零值，`IdleConnTimeout` 仅在 `RuntimeOptions.IdleTimeout>0` 时设置而本仓库从不设 → **阿里侧无按龄回收**，但受 `DefaultMaxIdleConnsPerHost=2` 约束，且 transport 由 tea 包级 `clientPool` 按 domain 缓存（热重载不重复建）。**不得写成"连接泄漏"** | 📝 待执行（口径订正） |
| **I8-89** | §4 崩溃面精确化（G-9d） | `mustDuration`/`mustThreshold` 静默返回 0 成立；`recover` 探针证明 `time.NewTicker(0)` panic 文案为 `non-positive interval for NewTicker`，生产无 `recover`。**新增发现：`mustDuration("-5m")` 返回 `-5m`**（`time.ParseDuration` 接受负号，函数无正数校验），**同样 fatal**；且只有 `Interval=0` 会 panic（`DNSTimeout=0` 为立即过期/fail-closed；`DNSFailThreshold=0` 使 `IsOpen` 恒真）。两条生产路径（启动加载、协调器候选构造）**都经 `normalizeSettings`**（`config/store.go:1155`）→ 0/负值**当前不可达**；`fs.Sub` 失败路径同理不可达（`//go:embed` 在缺目录/空目录时**编译期**失败） | 📝 待执行（记录新发现；**不得表述为"启动会崩溃"或"页面会空白"**） |
| **I8-90** | 审计 **§7 第 17 条**（`isRetryable` 字符串兜底的依据位置） | **两处事实订正**：① `syncer/retry.go` 现全文仅 **104 行**，原引 `:123-175` **已失效**——依据实际在 **`:11-30`**（关键 `:23`）；② "全仓 `.md` 内不含 `Unwrap` 说明" **被证伪**：`Issue6.md:584` 等以 `.md` 记录了同一裁决。规范结论"不得归因为 `.md` 注释"仍成立，**但事实从句必须改写**为"依据在 `syncer/retry.go:11-30` 的 Go 注释（`.md` 亦有同一裁决记录，故不得表述为『.md 中无说明』）" | 📝 待执行（订正审计 §7 第 17 条） |
| **I8-91** | 审计 **§13 行数口径** + **§0.5 逐项可追溯性** | ① §13 表格 17 个 `\|` 行 = 表头 + 分隔 + **数据行 15**，另结论段 1 行 ⇒ 引用时必须写明口径（"17 行"≠ 15 条目）② **§0.5 与 §13 无法逐项追溯 16 项 PT**：§13 只点名 3 项（PT-I7-03、PT-B7-07、PT-I7-06），§0.5 只有区间写法 ⇒ 逐项权威清单在 `ProdTestList.md` 与 [Issue8.md](./Issue8.md) §6；**PT-B7-04/-06/-08、PT-I7-02/-04 审计从未单独点名**；PT-AUDIT-01 不属 16 项但同属未执行边界 | 📝 待执行（补 §0.5 逐项行或明确指向 Issue8/ProdTestList） |
| **I8-92** | 基线推进（原"计数漂移"已消解） | 会话期间并行会话两次推进 HEAD：`938bb8b`（复核基线）→ `b658492`（含本轮授权的文档订正与新建本台账）→ `fd6ed7e`（本台账 I8-03/I8-04 订正）。**当前工作树仅 `M Issue8.md`**（本会话的继续追加），`origin/main` 仍 `938bb8b`（**ahead 2，未推送**）。后续引用请以"HEAD `fd6ed7e`"为当前基线，不要再用"5 个未提交文件"描述现状 | ✅ 已更新 |
| **I8-93** | 审计 **§4 第 3 张表（🔁重复实现）** 成立率 | **2/5 成立，3 项已被后续批次收敛**：① 前端 `theme` 双写 **已失效**（`Settings.vue` 的 `theme` 命中全是 `themeVars` 读取、零写入；报告锚点 `:127-141` 现为 `resetAll`；P3-16 已改为"只提交十个可见字段、不回传 theme"）② 前端重复实现后端校验 **已失效**（`intervalPattern` 已不存在；全前端校验原语仅剩 `Settings.vue:202` 文件名净化与 `Logs.vue:76` 时间戳兜底，`Settings.vue:164` 注释明确"时长语法与正数校验交由后端"）③ 其余 3 项成立（默认值多真值源、presence map、三处健康派生）。**另发现**：`resetAll`（`Settings.vue:131-145`）只做 `POST /api/config/reset` + 800ms 后 `location.reload()`，**从不调用 `clearAllCache`**（无 localStorage/cookie 持久化，故 800ms 窗口无功能影响） | 📝 待执行（§4 表 3 标注 3 项已收敛；补 `clearAllCache` 注释矛盾的确证） |
| **I8-94** | 审计 **§4 锚点漂移全表** | 31 个可核验锚点中 **19 个已漂移**（`config/store.go` 统一 **+48 行**、`internal/health/push.go` **+20 行**）；另有三处**计数/行号错误**：清单 1 的"仅 5 个测试文件"实为 **4 个**（`syncer/syncer_test.go`、`config/store_tx_test.go`、`config/store_corrupt_test.go`、`webui/api/settings_policy_test.go`；`config/runtime_test.go` 与 `store_v3_test.go` 只测 `ToRuntimeConfig`，`\bConfig\b` 在 `config/*_test.go` 零命中）；"11 个仅测试导出"实为 **12 个符号**；`push_test.go` 的 `var _` 锚点为 `:500-502`（报告写 `:16-18,497-499`，其中 `:16-18` 实为 import 块），且"三个无用 import"有误——`config.` 另有 12 处使用（`:500` 纯死代码），`notifier.`/`syncer.` **各仅 1 处即该行**（属 import 保持器） | 📝 待执行（按漂移表更新锚点；**下轮改动前必须先更新锚点，否则会误判"符号不存在"**） |
| **I8-95** | 审计 §5 附表 **H5 行被证伪**（会诱导错误修法） | 报告写"手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` → EXIT=0"，**实测 EXIT=1**：`... cannot embed directory frontend/dist: contains no embeddable files`。**根因是"目录内 ≥1 个可 embed 文件"，不是"目录存在"**；再写 `dist/index.html` 后才 EXIT=0。**建议直接删除或改写该行**，不得保留"EXIT=0"（否则会诱导出"建空目录"甚至"提交占位 index.html"的错误修法） | 📝 待执行（订正 §5 附表 H5 行） |
| **I8-96** | `dns` 包外网依赖的**表述订正**（报告 3 项主张中 2 项不准确） | ① `dns/resolver_test.go:10/29/40` 确实用 `8.8.8.8:53` 且都真调 `Resolve` ✅ ② **"无 `testing.Short()` 守卫"不成立**：`TestResolve_NonExistent`(`:25-27`) 与 `TestResolve_PublicDomain`(`:37-39`) **都有守卫**；唯一无守卫的 `TestResolve_Localhost`(`:9-22`) 经回环探针证明**根本不外发查询**（上游换死地址仍 PASS，走 hosts）③ **但默认门禁确实会执行外网查询**：`make test`（`Makefile:10` 不传 `-short`）与 CI（`docker-publish.yml:68`）都不过滤 ⇒ 守卫失效。**回环探针实测**（上游改指 `127.0.0.1:15353` + UDP 监听）：`-short` → **0 包**；默认语义 → **6 个 UDP 报文**（`host.invalid`×2、`dns.google`×4），dns 包耗时 **15.1s**（5s PASS + 10s SKIP）；全程**未触碰 8.8.8.8**。⇒ I8-04 应改写为"**默认门禁会执行真实外网 DNS 查询**"而非"无守卫" | 📝 待执行（订正 I8-04 与审计对应行的表述） |
| **I8-97** | §6 "7 个永不失败的测试" → **实为 5 个，2 项需重新定性** | 真正"永不失败"的是 **5 个**：#1`dns/resolver_test.go:24-34`、#2`:36-58`、#3`notifier/bus_test.go:108-120`、#6`webui/api/alertset_test.go:176-182`、#7`:216-222`（每项均以独立探针作对照，注入破坏后仍 PASS）。**#4 `syncer/state_test.go:421-448`（报告 411-436，已位移）应改定性为"零断言的 panic/活性守卫"**：去掉 `stopOnce` → 二次 close → **FAIL(panic)**；不再置 `stopped`（A20 门控失效）→ PASS，且兄弟用例 `TestNoNewRoundAfterStop` 也 PASS（**零行为断言，行为破坏双双漏过**）。**#5 `syncer/syncer_test.go:597-627`（自述注释 `:596`）"不带 `-race` 时永不失败"成立，但默认门禁带 `-race`**（`Makefile:10` / CI `:68`），故门禁中**有**判别力——报告该句易被误读为"应删除"。另：`#7` 用例名说 `WithoutRuntime`，但 `Candidate{}` 在 `deps.go:129` 的 **State** 守卫就返回，**并未走到 Runtime 分支** | 📝 待执行（§6 改为 5 个 + 2 项重新定性） |
| **I8-98** | §6 字面量锚点 → **实为 3/5** | `DefaultHealthTimeout=10m`、`DefaultPushInterval=60s`、`MinPushInterval=20s` **已锚定**（`config/store_v3_test.go:430`）；**`InFlightLimit=4` 与 `DefaultStartupGrace` 仍无字面量锚点**（`grep "InFlightLimit == 4"` 0 命中；`health_test.go` 无 `10 * time.Second`） | 📝 待执行（§6 改为 3/5 并点名两项） |
| **I8-99** | 清洁检出下 **`go list ./...` 本身失败**（新发现） | 清洁检出中 `go list ./...` 返回非 0 且无输出 ⇒ 任何 `go test $(go list ./...)` 脚本会**退化成"只跑当前目录"**（静默漏检）；须用 `go list -e ./...`。另实测 `make all` 在第一个前置 `vet` 就中止（**EXIT=2**，`frontend` 目标从未被触达）；overlay 加前置后 `make -n` 显示前置生效且 `make all` 中 `npm ci` 只出现一次（去重成立） | 📝 待执行（补入 I8-03 的影响面） |
| **I8-100** | 清洁检出的**包级结果口径** | 实测：**2 包 `[setup failed]`（root 与 `webui`）+ 9 包真跑全 ok + `dns` 包未跑**（因授权禁止真实外网解析，仅验证到编译层）。⇒ **不得主张"清洁检出可通过"，也不得主张清洁检出里 12 包全绿**；`dns` 在清洁检出中的运行结果**未验证** | 📝 待执行（作为 I8-03/I8-25 的限定写入） |
| **I8-101** | §6 `waitForNoSMTPData` 现状与调用点数 | 函数体**仍是裸 `time.Sleep`**，但**误导注释已改为如实披露**；调用点是 **3 个**（`alertset_policy_test.go:92/150/180`，报告写 4 个）。**判别力成立**：破坏 `policySubscriptions` 触发开关后，用例按 `alertset_policy_test.go:182` **变红** ⇒ 真实判别力在调用点而非该 helper | 📝 待执行（§6 更新为 3 个调用点 + 判别力结论） |
| **I8-102** | `test:alerts` **未设逐用例超时** ⇒ 回归会"永久挂起"而非"变红" | `webui/frontend/package.json:7` 为 `node --test tests/alerts-load.test.mjs`（**无 `--test-timeout`**），而 `node:test` 默认逐用例超时**无限**且顶层用例**串行**。实测：旧实现不带上限时被 180s 硬墙杀掉、**输出 0 字节**（用例 1 永久挂在 `tests/alerts-load.test.mjs:72` 的 `await state.save()`，用例 2～5 根本不开始）。**建议（未实施）**：改为 `node --test --test-timeout=30000 …`，否则任何"loading 态发 PUT"回归都会让门禁**挂死**而不是明确失败 | 📝 待执行（属门禁健壮性改进） |
| **I8-103** | 🔵 **I8-18 的修复面比预期更小**（正面证据） | 仓库**已有**覆盖四个告警对象的**一致快照读路径**：`config/store.go:1125-1169 LoadBusinessSnapshotTx`（含 email/webhook/policy/push）+ `config/runtime.go:27-35 BusinessSnapshot`，**`webui/api/export.go:28,43` 正在使用它**。⇒ `GET /api/alerts` 只需改走该既有路径（或等价的 `BeginReadOnlyTx` + 事务内读四对象），**无需新建读模型**；这也印证 I8-18 F6 的"契约零变更"结论 | 📝 待执行（作为 I8-34 的修复方案依据） |
| **I8-104** | §6 "**7 个永远无法失败的测试**" 分类不精确 | **应拆为 A/B/C 三类，共 11 项**：**A 真·不可失败 1 项**（`dns/resolver_test.go:24 TestResolve_NonExistent`——`Resolve` 恒返回硬编码 `1.2.3.4` + nil 时**仍 PASS**）｜**B 仅 panic 可失败（正文 0 断言）5 项**（`notifier/bus_test.go:108`、`syncer/state_test.go:421`、`alertset_test.go:176`、`alertset_test.go:216`、`policy_test_helpers_test.go:33`）｜**C 失败路径被 skip 吞掉 1 项**（`dns/resolver_test.go:36`——`Resolve` 恒失败时输出 **`--- SKIP` 而非 FAIL**）｜**D 空测试 1 项**（`syncer/syncer_test.go:597`）｜**E 审计误报 2 项**：`alertset_test.go:183` **有失败路径**（丢弃全部副作用 → FAIL ×3 断言 `:203/:206/:209`），故**不属该族**；`push_test.go:459` 的 `pub.count()==0` **非永真**（见 I8-16b） | 📝 待执行（§6 按 A/B/C/D/E 重写） |
| **I8-105** | §6 行号与计数订正 | ① `syncer/state_test.go:411-436` → 现 **`:421-447`** ② `syncer/syncer_test.go:646-670` → 现 **注释 `:595-596` + 函数 `:597`（体 597-621）** ③ `waitForNoSMTPData` 调用点 **3 处**（`alertset_policy_test.go:92/150/180`）④ `push_test.go` 那条应移到 **§4 冗余项（死字段）** ⑤ **`webui/api` 对 `InFlightLimiter`"零断言"不成立**——该 grep 现 **3 行命中、退出码 0**（`alertset_drop_log_test.go:40/114`、`alertset_subscription_test.go:28`），且 `:42/:116` 直接断言 `l.Acquire()` 布尔、`:115` 用 `InFlightLimit` 填满生产限流器、`alertset_subscription_test.go:88-121` 用本地 HTTP 服务器**实际收到请求数**断言投递次数 ⑥ `TestApplyCandidateWithoutRuntimeIsNoop` 升级为"**名实不符**"（见 I8-16e） | 📝 待执行（§6 六处回写） |
| **I8-106** | §6 两项**已修复**（不得照抄审计快照） | ① **前端 8 字段载荷契约已修复**：`webui/frontend/tests/alerts-load.test.mjs:178-180` 已用 `Object.keys(JSON.parse(body)).sort()` 深比较固定 8 键；**变异实验**——`Alerts.vue` 载荷多带 `enabled`（精确重现 Build7 Step 7 历史缺陷）→ 该用例 **FAIL（9 键 vs 8 键）**、其余 4 项 PASS。残余：后端 `TestTestEmailValidationErrors` 只覆盖 **5/8** 字段 required（缺 `username`/`password`），且前后端**无共享夹具/无"前端产出→后端解码"联动** ② **`app/logutil_test.go` 的 MultiHandler 缺口已修复**（P3-13 已删自写实现；现有 4 个直接用例 `TestP313DispatchAndErrors:102`/`TestP313RecordIsolation:179`/`TestP313ConcurrentCalls:213`/`TestP313StdlibWiring:234` + 1 个间接用例；**实跑 `go test ./app/` → ok 0.333s**） | 📝 待执行（§6 两项标"已修复"） |
| **I8-107** | §6 字面量锚点 → **5 个常量里只有 1 个成立** | 变异实验逐一判定：**`DefaultStartupGrace` 是唯一无锚点**（`10s→12s` 后 `./internal/health/` **全绿 42.4s**）——**它是 Build7 Step 7 的合同数值，风险最高**；`InFlightLimit=4` **有间接锚点**（`4→5` → api `TestP315ManagerReloadAggregation` FAIL，`alertset_drop_log_test.go:41` 用**字面量 4**）；`DefaultHealthTimeout=10m` **有锚点**（`store_v3_test.go:430` 字面量 + health FAIL ×2）；`DefaultPushInterval=60s` **有锚点**（同一条字面量）；`MinPushInterval=20s` **有两处锚点**（`:430` 的 `20s` + `push_test.go:556` 的 `19s` 边界）。良好范例仍在（`push_test.go:246` 的 `250`——审计写 `:243` 需订正；`server_test.go:370/:373` 的 `5s/120s`）。另：`provider/scan.go` 腾讯两路径（`scanTCLighthouse`/`scanTCCVM`）**各 0 测试引用**；**goroutine/fd/ticker 泄漏断言为 0**（唯一命中是 `notifier/inflight_test.go:210` 的**否定式注释**；探针实证：注入泄漏 goroutine 后 `pre=2 post=3` **FAIL**，而仓库既有 `TestStopIdempotent` 同变异 **PASS**）；**负面 sleep 假通过**清单为 `state_test.go:156/162/184/214/222/237`、`state_applied_hook_test.go:38/65/86`、`syncer_test.go:166/209/255`（syncer 包 33 处 `time.Sleep`、**11 处 >200ms**、最长 600ms）；§6 规模口径亦已过时（**现 94 个 Go 测试文件 / 31,373 行**，起 goroutine 文件 **36** 个；`t.Parallel()=0` 仍准确） | 📝 待执行（§6 改为 1/5 + 三项真实缺口） |
| **I8-108** | 文档漂移 4 处（独立复核 G-12a 发现） | ①审计 §11 行 `1628` 的调用链**仍写已删除的 `syncDomain/retrySyncDetailed`** ②审计 §4 行 `1204` 的 `syncer.go:813` **行号失效**（真实为 `:716`/`:951` 一带） ③P2-03 行 `694` 引用的"syncer 内 ECS+ICMP WARN"**已不存在**（现为 `provider/plan.go` 的 `IssueUnsupportedICMPv6`） ④P2-08/P2-09 的**前提行号过期** | 📝 待执行（四处按现行符号订正） |
| **I8-109** | 审计 §13 行 `1713` 的"**`syncer/syncer.go` 第二个独立复核未完成**" | **应更新为已完成**：G-12a 已交付（`syncer.go` 1007 行 / sha256 `5be02dc5…`，主体 Q1–Q4、Q6 = A，Q5 = B），证据目录 `/tmp/fwalizer-issue8-evidence/G12a/`（`findings.md`/`crosscheck.md`/`new-findings.md`/19 个 overlay/raw）。**`webui/api/bundle_v3.go` 的第二双眼睛（G-12b）亦已完成**（见 I8-110） | ✅ 两个半句均已具备条件（待回写） |
| **I8-110** | §11/§13 的"`webui/api/bundle_v3.go` 第二个独立复核未完成" | **应更新为已完成**（G-12b，两阶段隔离取证）：`Q8 字段集 = A`（§9.1 规定的 **57 条路径逐字段一致**，无遗漏无多余；导出 DTO 全为 value 型 ⇒ 对象不可能为 null；3 个切片全用 `make(...)`（`bundle_v3.go:319/329/332`）⇒ 空库输出 `[]`）；`Q12 单事务零写入 = A`（四个 stage 注入 `RAISE(ABORT)` 全部完整回滚、零发布、500 不回显原文；3 个 overlay 同时使仓库与探针变红）；`Q13 export_id 映射 = A`（重复 `export_id` → 400 不静默去重；`idMap` 按切片顺序 + `LastInsertId` 写入、**不遍历 map**）；`Q14 严格解码 = A`（**唯一入口**、29 载荷实测：6 个层级未知字段全拒、尾随/多顶层/尾随垃圾/空 body 全拒、`[null]` 给出元素级字段路径、**超限 413 优先于语法错误**、无绕过路径） | ✅ 已具备条件（待回写 §11/§13） |
| **I8-111** | **version 3 存在两个绕过维度**（G-12b 新发现；取值层面 Q9 = A，**19/19 非法取值形态全部 400 且零写入零发布**） | ①**重复键 last-wins**：`{"version":2,…,"version":3}`、`{"version":1,…,"version":3}`、`{"version":null,…,"version":3}` **全部 200 并整库替换**；`{"targets":null,…,"targets":[]}`、`{"monitoring":null,…,"monitoring":{…}}` 亦被接受（共 **5 个 200 载荷**）②**键名大小写不敏感 + 转义**：`{"VERSION":3}`、`{"Version":3}`、`{"\u0076ersion":3}` 均 **200**。裁决见 **I8-46** | 📌 待裁决（**是否收紧取决于你对"严格契约"的解释**：`AGENTS.md` §9.1 要求"只接受 version 3、其他版本一律 400"，但 Go 的 `encoding/json` 默认 last-wins 与大小写不敏感匹配属语言行为，收紧需显式检测重复键与大小写 | 
| **I8-112** | **对象型 `null` 的真实覆盖分布**（**推翻我此前 I8-07 的表述**） | 逐字段 57 项实验结论：**null 专属用例仅 4 处**（`version:null` `import_test.go:113`、**`targets:null` `:118`**、`target_export_ids:null` `:131`、`monitoring:null` `alerts_v3_test.go:211`）——**不是 1 处**；`rules/settings/alerts/alerts.policy/uptime_kuma_push/metadata/metadata.exported_at` 虽无 null 专属用例，但**有"缺失形态"用例走同一守卫分支**，删守卫**会变红**（即**有守护**）；真正**无任何用例（含缺失形态）守护的字段是 30 个**（含 `alerts.email`、`alerts.webhook`、`settings.credentials{,.tencent,.aliyun}`），**不是 10 类**。⇒ **若按原 I8-07 补测，会重复补 6 类、漏掉 24 个**；正确清单见 `/tmp/fwalizer-issue8-evidence/G12b/null-matrix.md §2` | 📝 待执行（按实测改写 I8-07 与审计 `:1214`） |
| **I8-113** | **默认 Webhook 渠道的"新建库 vs 升级库"静默漂移**（G-12b 最有价值的结论） | `config.DefaultWebhookChannel` 单点改为 `"slack"` 后：**新建库得 `dingtalk`、升级库（老表缺列走 ALTER）得 `slack`**，而**仓库测试全绿零告警**——因为 **DDL 用字面量**（`config/store.go:235`）而 **ALTER 路径用常量**（`:313`）。已实测。另：`bundle_v3.go:353/363` 的 `"587"`/`"dingtalk"` 是**不可达死代码**（列恒非空），改它无可观测差异——审计 `:1214` 把两者并列会掩盖"谁真正决定用户可见值"，且该行行号已漂移（`store.go:175/187` → **`:223`(port)/`:235`(channel)**）。裁决见 **I8-44④** | 📌 待裁决（属真值源统一问题，且**新增了"新建库/升级库结果不同"这一用户可见后果**） |
| I8-85 | 审计 §13 行数口径 | 实测为 **15 行**（含表头；14 条目），任务提示中的"17 行"为旧记法 | 📝 待执行（口径澄清） |
| I8-86 | 审计 §2 的 `P2-09` 等 finding 与 §8.2 中仍写 `syncDomain → retrySyncDetailed` 陈旧符号链 | §11 覆盖矩阵 `L1624` 等位置保留被删除符号名，按 §0.2 口径属"引用时快照"，建议统一加符号级警示 | 📝 待执行 |

> **采集时行号会漂移**（本轮已实测：审计 `:1717`/`:1752`、Issue7/TODOLIST 各数行在我订正后位移）。本表行号是**2026-10-08 订正时的快照**；后续引用请用「符号/章节名 + 行号」双锚点。

---

## 6. 外部验收边界（全部未通过）

**16 项主清单**（`ProdTestList.md`）：PT-B7-01、PT-B7-02、PT-B7-03、PT-B7-04、PT-B7-05、PT-B7-06、PT-B7-07、PT-B7-08、PT-B7-09、PT-I7-01、PT-I7-02、PT-I7-03、PT-I7-04、PT-I7-05、PT-I7-06、PT-I7-07。

| 状态 | 项 |
|---|---|
| **人工免除（非通过）** | PT-B7-02（真实自动邮件）、PT-B7-03（真实钉钉/飞书/Slack） |
| **未执行** | 其余 14 项 + PT-AUDIT-01（P3-04 浏览器）+ 真实家庭 DDNS 连通性与真实 DNS 上游轮次节奏 |

**不得表述为通过**；`PT-B7-04/-06/-08`、`PT-I7-02/-04` 在审计中从未被单独点名，本轮已逐项在册。

---

## 7. 只读边界说明（本台账数据的采集前提）

- 本轮复核的测试/overlay/探针全部在仓库外副本执行；仓库内仅只读命令；**未调用真实云/DNS 上游/SMTP/Webhook/Uptime Kuma**。
- 只读完整性核对（两路独立）：274 个 tracked 文件 sha256 与基线逐行一致、文件集合无增删、未跟踪文件未变、Git 状态与基线逐项一致、基线自身 6/6 校验通过、无残留测试进程。
- **保留前提**：① 两点采样非连续监控，无法排除"写入后还原"；② `webui/frontend/dist`、`node_modules` 仅核存在性与 mtime，未逐文件哈希；③ 未 `fetch`，本地 `origin/main` 不代表真实远端；④ 复核时工作树含 5 个未提交文件，"未修改"指相对基线未再变化，**不表示工作树干净**；⑤ `.git/objects` 基线后新增 8 个松散对象（内容与既有对象相同、未改引用/索引/HEAD/工作树，写入方无法归因），故"仓库目录内零文件新增"不成立；⑥ 未覆盖 `.git` 内部清单、属主/ACL、仓库外副本的软链与敏感文件检查、网络层取证。
- **本文档写入后基线已变更**：Issue8.md 为新增文件，另有若干文档状态订正，均属用户授权范围；后续如需再次核对只读完整性，应以新的基线重新采集。

---

## 8. 优先级建议

1. **I8-01（TODO-001，P0）** — 仍未修复且无认领（`coverage_ready` 在 unsupported-only 时误报）。
2. **I8-34 / I8-18（alerts 读侧撕裂）** — **已确定性复现**（并发夹具下约 21%～24% 的 GET 响应四部分不一致），修复只需改走**既有**的 `LoadBusinessSnapshotTx`（I8-103）、契约零变更；若不修须显式登记已知边界。
3. **I8-30a（同一赋值族两套期望）→ 决定 I8-02 / I8-17c 的修法** — 影响 `cleanup_*` 与 `unsupported` 两类明细的可观测性。
4. **I8-03（构建阻断）** — 决定"本地绿色"能否在清洁检出复现；**注意 H5 已被证伪**（I8-95），且占位 `index.html` 属禁止项。
5. **I8-113 / I8-48（默认渠道"新建库 vs 升级库"漂移）** — 用户可见后果 + 当前测试零告警。
6. **I8-46（version 3 的两个绕过维度）** — 重复键与大小写变体会让"拒绝 version 2"在字面上被绕过。
7. **I8-107 三项真实测试缺口** — `DefaultStartupGrace` 无字面量锚点（Build7 Step 7 合同数值）、goroutine/泄漏零断言、`provider/scan.go` 腾讯两路径零测试。
8. **I8-41a（`Pause`/`Resume` 并发语义）** 与 **I8-17d** — 当前无生产调用方，但重新接线即带入竞态。
9. **I8-17f（轮末无谓 sleep）**、**I8-05（Push Wake 重置心跳）**、**I8-102（`test:alerts` 无逐用例超时）** — 效率与门禁健壮性。
10. **I8-42 / I8-44 / I8-47（清理与口径裁决）** — 低风险，可批量；前端类以 `vue-tsc` 为唯一门禁（I8-45）。
11. **文档订正 I8-85～I8-113** — 口径/锚点/状态订正，不涉生产代码；其中 I8-109/I8-110 已具备"第二双眼睛已完成"的回写条件。

> **基线与边界**：复核基线为 `938bb8b` + 5 个未提交文件；会话期间并行会话把 HEAD 推进到 **`fd6ed7e`**（`origin/main` 仍 `938bb8b`，**ahead 2，未推送**）。独立复核已逐一 SHA256 确认被复核的生产文件在两个 revision 间**逐字节相同**，故全部结论对当前 HEAD 同样成立。**生产源码、测试、API/schema、依赖、部署配置全程零改动。**

---

## 9. 本轮执行总结（2026-10-08）

### 9.1 已完成的复核与专项

| 批次 | 内容 | 结论 |
|---|---|---|
| **G-12a** | `syncer/syncer.go` 独立第二双眼睛（审计自认缺口） | Q1–Q4/Q6 = **A**；Q5 = **B**；**5 项新发现**（NF-1～NF-6） |
| **G-12b** | `webui/api/bundle_v3.go` 独立第二双眼睛（审计自认缺口） | Q8/Q9(取值)/Q12/Q13/Q14 = **A**；Q9 两个绕过维度 = **D**；Q10 文案漂移 = C；Q11 = **D**；**10 项新发现** |
| **G-6 / G-17③** | `GET /api/alerts` 撕裂窗口（两路独立） | **A：确定性复现**（≈21%～24%）；并证明修复只需走既有路径 |
| **G-17①** | P2-06 判别力（补做上次超时实验） | **A：判别力成立**；上次"取不回结果"＝夹具挂起 + 无逐用例超时 |
| **G-17②** | P3-12 WAL/SHM 时序判别实验 | **A：判别性成立**；技术订正＝失败模式为 `SQLITE_READONLY` 而非 EACCES |
| **G-8 / G-13** | 清洁检出构建阻断 + 历史批次 6 六项核销 | 阻断**仍存在**；**H5 行被证伪**；R1❌ R2🟡 R3🟡5/7 R4🟡3/5 R5❌ R6✅ R7✅ R8❌ |
| **G-9** | §4 死代码/重复实现/过度防御四张表 | 表1 **5/5**、表2 全部成立（**12 个符号**）、表3 **2/5**（3 项已收敛）、表4 **6/6**；**HC-4 前提消失**（`MultiHandler`） |
| **G-10a/b/c** | §6 测试质量三份 | 不可失败测试应拆 **A/B/C/D/E 共 11 项**（真·不可失败仅 1）；**2 项已修复**；字面量锚点 **1/5**；新发现 IPv6 字面量缺陷 |
| **G-11 / G-7 / G-9d** | 内存资源 + 生命周期 + 崩溃面 | §3 五处计数订正；**`IdleConnTimeout` 从"证据不足"升级为可核实**（腾讯 30s / 阿里无按龄回收，**非泄漏**）；生命周期阻塞**生产不可达**；`mustDuration("-5m")` 新发现 |
| **G-14 / G-15 / G-16** | 分页总预算 + §13/§14 边界 + 17 条正向控制 | 总预算**确认无**（三候选对比，推荐①）；§13 口径 **15 数据行**；§7 第 17 条**两处事实订正** |
| **Issue8 素材索引** | 未闭环项/待裁决/文档订正三张清单 + 章节骨架 | 约 128 行登记；**7 对同源登记须合并**；抓到本会话一处**声称不实**（I8-82）与一处全新过期项（I8-84） |

### 9.2 本轮完成的授权写入

- **新建 `Issue8.md`**（本台账，已随 `b658492` 入库；后续追加仍在工作树）。
- **文档订正 22 条**（I8-70～I8-113 中已执行部分）：审计 §0.2/P3 清单/§14/附录 A/P3-06 同格双结论/P2-07 四处/P3-24 三处/P3-18/P3-17 九→十文件/P3-24 他机路径；`AGENTS.md` P3-24 标记 + 文档清单登记 Issue8；`TODOLIST.md` 复核订正条目 + 统计数字；`Issue7.md` §12.6/12.7/12.8 提交状态；`ProdTestList.md:9` 的 R7 过期状态。
- **未改动**：任何**产品**源码、API/schema、依赖、Docker/CI 配置。**唯一被改动的非文档文件是三份 Go 测试**（`provider/scan_ecs_test.go`、`provider/snapshot_test.go`、新增 `syncer/ecs_pagination_test.go`，合计 +215/−2）——它们是**本轮开始前既有的 P3-25 补强批次**，由并行会话随 `b658492` 提交，**不是本会话的改动**。

### 9.4 台账口径说明（避免误读）

- **本台账条目数为三层叠加**：§1/§1b/§2 共 **32 个缺陷/审阅条目**、§3 共 **19 项待裁决**、§5 共 **44 条文档订正项**。其中**存在同源登记**（例如 alerts 撕裂在 §1 之外仍被 §3 引用；`PusherDeps.Bus` 在 §1、§3、§5 各出现一次），**引用时须按"同一问题只计一次"合并**，不得据条目数推断问题数。
- **`I8-xx` 是本台账内部编号**，不是审计报告的 P0/P1/P2/P3/I/R7 编号；`NEW-*`/`NF-*` 为复核过程新增发现。
- **七对已知同源登记**（引用时须合并 + 交叉引用）：`TODO-008 ≡ 审计:990 ≡ I8-02`；`PusherDeps.bus` 永真断言（**实为死字段**，见 I8-16b）在四处出现；构建阻断在四处出现；分页总预算 ≡ I8-38 ≡ I8-60；`waitForNoSMTPData` ≡ 批次 6；7 个永不失败测试 ≡ 批次 6（**实为 11 项，见 I8-104**）；`buildPushURL` 死赋值 ≡ §4🛡 ≡ I8-15c。

### 9.3 仍未闭环（转入 §2/§3/§4 跟踪）

| 项 | 状态 |
|---|---|
| `GET /api/alerts` 修复与否 | **待裁决 I8-34**（缺陷已证实，修复面已知） |
| TODO-001 / TODO-003 / TODO-008 | **仍未修复 / 仍开放 / 待裁决**（I8-01 / I8-31 / I8-30） |
| 构建阻断、`dns` 外网门禁 | **仍未修复**（I8-36 / I8-37 待裁决） |
| 分页总预算 | **未实施**（三候选已评估，推荐①） |
| §4 清理、§6 补测、§3 专项 | **均未实施**（清单与建议已备） |
| 16 项外部验收 | **全部未通过**（PT-B7-02/03 为人工免除） |
| 真实云/DNS 上游/SMTP/Webhook/Kuma/浏览器/远端 CI | **全部未验证**，不得表述为通过 |
