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
| 分支 / HEAD | `main` / `938bb8bac0a41ea285597ba454b34e727003d7ba` |
| 本地 `origin/main` | 同值；ahead/behind `0/0`；stash 空（**未 fetch**，不代表真实远端） |
| 复核基线 | HEAD + 5 个未提交文件：`AGENTS.md`、`fwalizer-audit-final1.md`、`provider/scan_ecs_test.go`、`provider/snapshot_test.go`、`syncer/ecs_pagination_test.go`（P3-25 补强批次，生产 Go 代码零改动）。**注**：该计数是**复核开始时**的快照；本文档写入后工作树另有本轮授权的文档改动（见 I8-92）。 |
| 复核方式 | 仓库外副本执行测试/overlay/探针；仓库内只读；**未调用真实云/DNS 上游/SMTP/Webhook/Uptime Kuma** |
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
- **实测（清洁检出）**：`go build ./...` / `go vet ./...` / `go test ./...` = **EXIT 1/1/1**（`webui/embed.go:5:12: pattern frontend/dist: no matching files found`）；根包与 `webui` 包 `[setup failed]`；`make vet` = EXIT 2。
- **对照（当前工作树）**：存在 ignored `webui/frontend/dist` → 全部 EXIT 0。
- **影响**：**所有"本地门禁绿色"都以一个未入库文件为前提**；Docker/CI 因各自先构建前端而不受影响。
- **禁止项**：**不得**提交占位 `webui/frontend/dist/index.html`（会被 embed 当真实页面提供）。
- **状态**：**仍存在**；修法（给 `test`/`vet` 加前置）属待办。

### I8-04 【中】门禁非密闭：默认测试命令访问真实 DNS 上游

- **问题**：`dns/resolver_test.go` 使用 `NewResolver("8.8.8.8:53", …)`，其中 `TestResolve_Localhost` **无 `testing.Short()` 守卫**且解析失败直接 `t.Fatalf`；另两个用例虽有守卫，但默认门禁命令（`Makefile:10`、CI `docker-publish.yml:68`、历史 `go test ./... -race`）**不带 `-short`**。
- **影响**：① 无外网/出口受限环境门禁必然变红，且失败点与被测产品无关；② 历史"全量 12 包 race 绿"**包含真实外网 I/O**，不能表述为纯本地密闭门禁。
- **证据等级**：静态证据充分；CI 实际执行日志未取得（属推论）。
- **状态**：**仍存在**。

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

### I8-07 【中低】配置包 version 3 的对象型 `null` 覆盖仅点状

- **问题**：仓库测试对**对象型 `null`** 的拒绝只有 `webui/api/alerts_v3_test.go:211` 覆盖 `monitoring: null`；以下 **10 类无对应用例**：`rules:null`、`settings:null`、`alerts:null`、`alerts.policy:null`、`alerts.email:null`、`alerts.webhook:null`、`settings.credentials:null`、`settings.credentials.tencent:null`、`settings.credentials.aliyun:null`、`monitoring.uptime_kuma_push:null`。
- **判别力**：负向 overlay 实证——放宽 `monitoring` 之外的 null 对象校验时，仓库测试**不会变红**。
- **附加**：`presenceSlice` 是当前唯一"自定义 `UnmarshalJSON` 内自建严格 decoder"的类型，无静态守卫防止新模式复制时漏设。
- **状态**：主契约本身成立（49 例矩阵通过），**本题只登记覆盖缺口**。

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
| b | `internal/health/push.go:48` + `push_test.go` | `PusherDeps.bus` 只赋值不读 → `pub.count() == 0` 断言**永真** |
| c | `syncer/stop_gate_test.go:200` | 断言 `p.calls.Load() != 2`，但失败文案写"want 1"（文案陈旧） |
| d | `notifier/email.go:224-227` / `:229` | 丢弃 `c.Close()` 错误（未计入 11 出口）；忽略 `Extension("STARTTLS")` 错误（既有行为） |

### I8-17 【信息】口径需限定（非缺陷）

| 项 | 说明 |
|---|---|
| a | `provider/ali_ecs.go:108/111` 同步路径错误文本**内嵌原始 NextToken**（扫描路径 `provider/scan.go:209` 刻意不含）→ "错误不含原始 token"**仅对扫描路径成立** |

---

## 2. 未完成审阅 / 证据不足

### I8-18 `GET /api/alerts` 4 次非事务读的撕裂窗口

- **声明出处**：审计 `fwalizer-audit-final1.md:1243`「`GET /api/alerts` 4 次非事务读存在撕裂窗口（PUT 单事务写，读侧可能"新 policy + 旧 email"，前端整体回传即把旧值写回）」；参照 `AGENTS.md:229`、`:292`。
- **状态**：**本轮未完成审阅** —— 主审组明确未读取 `webui/api/alerts.go` 的读取次数/事务边界，也未读 `webui/frontend/src/views/Alerts.vue` 的回传载荷构造（两文件均存在）。
- **最小核验点**：① `handleGetAlerts` 是否 4 次独立 `Store.Get*` 且无事务；② `handlePutAlerts` 是否要求四对象全量覆盖；③ `Alerts.vue` 是否把 GET 响应整体作为 PUT 载荷；④ 用真实 HTTP 并发 PUT/GET 注入撕裂。

### I8-19 P2-06 的判别力证据

- **状态**：**B（部分）** —— 组件测试 `npm run test:alerts` **5/5 pass** 成立；但旧实现 overlay（`Alerts.vue` 回退 `064b794^`）运行 **>120s 未结束**、转入后台 job `bash-842`，**未取回结果**；`Alerts.vue` 亦未经行级复核。
- **缺口**：缺少"旧实现变红"的判别性证据。

### I8-20 P3-12 的 WAL/SHM 时序判别性实验

- **状态**：**未执行**。已有静态调用顺序（`config/store.go:130` 早于 `:134/:139`）与 0666→0600 迁移成功等**非判别证据**；探针与 overlay（辅助文件 Chmod no-op）已备，未在容器执行。
- **所需条件**：Docker 可用 + 约 2 分钟（临时卷，无需重建镜像）。

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
| **I8-42** | **§4 清理类裁决**（G-9 四分判定，逐项独立裁决） | ①**零成本改非导出**（消费者全在同包，共 8 个符号）：`GetSettingsTx`、`TargetExistsTx`、`RuleExistsTx`、`AlertManager.Current`（返回未导出类型 `alertSet`，导出性无价值，**性价比最高**）、`Server.ServeStarted`、`Pusher.InFlight`、`InFlightLimiter.InFlight`、`ErrNoSnapshotLoader` ②**可安全清理（5 项）**：`Config`+`LoadConfig`+`ToConfig`（**必须合并为一个原子改动**，需改 4 个测试文件：`syncer/syncer_test.go`、`config/store_tx_test.go`、`config/store_corrupt_test.go`、`webui/api/settings_policy_test.go`；`RuntimeConfig` 前 11 字段与 `Config` 逐一对应，机械替换即可）＋ `EventRuleChanged`、`ResolvedIPs`、`SyncEvent`(TS)、`clearAllCache`（后 4 项零测试改动）③**前端 2 项只能从返回对象移除、不能删函数**：`useDryRun.error`、`useSettings` 的 `settings`/`load`/`refreshCredentialState`（`load` 被 `applyTheme` 调用、`refreshCredentialState` 被 `load`/`refresh` 调用、`settings` 被四处内部使用）④**暂缓**：`Store.SetSetting`/`SaveAlertEmail`/`SaveAlertWebhook` 改非导出波及 **14 个跨包测试文件**（`webui/api` 7 个 + `main_test.go` + `permissions_process_test.go`），**报告低估了代价** ⑤**补正向控制而非删字段**：`PusherDeps.Bus`（在测试内主动 `pub.Publish` 一次并断言 `count==1`） | `config/config.go`、`notifier/bus.go`、`provider/provider.go`、`webui/frontend/src/types.ts`、`useScannedResources.ts`、`useDryRun.ts`、`useSettings.ts`、`config/store.go`、`internal/health/push.go` | 均为低风险清理；**一旦实施必须走 `vue-tsc`（前端唯一门禁）与全仓编译**，且不得削弱 §7 正向控制 |
| **I8-43** | **`bundle_v3.go` presence map 导致的错误文案漂移**（比报告更严重） | ①改为与同文件 `normalizeBundleSettings` 一致的**顺序 `if` 链**（**强烈建议修**：实测同一次非法请求 **300 次在单进程内**即观察到 3 种（policy，3 字段缺失）与 8 种（email，8 字段缺失）文案；**修复零测试破坏**——`grep '字段缺失' --include=*_test.go` 4 处命中全为子测试名/注释，断言只校验状态码）②接受现状并文档化 | `webui/api/bundle_v3.go` | 同一非法请求的错误文案不可复现，影响排障与用户沟通；不修则每次请求文案可能不同 |
| **I8-44** | **§4 其余四项小裁决** | ①`mustDuration`/`mustThreshold` 静默 0：**首选改 `panic`（显式不可达断言）**，次选文档化默认值 ②`health.go:195` 的 `Policy == nil` 检查与 `Evaluate` 不一致：**删该检查**与 `Evaluate` 对齐（生产 `run.go:138-143` 恒传 Policy，且 `Evaluate` 先于 `TriggerEnabled` 调用，nil 时会在 `supervisor.go:128` 先 panic） ③`buildPushURL` 重复计算：抽 `validatePushURL` 并保留"先校验后占在途名额"顺序（优先级最低） ④**`fs.Sub` 吞错：至少补一行 `slog.Error`**（零风险，只增可观测性；且"embed 保证成功"**仅当 dist 已按预期构建**时成立） | `config/runtime.go`、`internal/health/health.go`、`internal/health/push.go`、`webui/server.go` | ①③④为可观测性/一致性改进；②删检查需确认与 `Evaluate` 的调用顺序不被破坏 |
| **I8-45** | **前端类改动的验收标准** | 必须写明为"**`npm run build`（= `vue-tsc && vite build`）通过**"：`webui/frontend/tests/` 的 4 个测试用 **mock** 顶替 `useSettings`（`p316-state.test.mjs:36` 的 mock 只列被用成员），故前端清理类改动**既不会破坏这些测试、也不会被它们检出** | `webui/frontend/tests/`、`package.json` | 若误把"前端测试通过"当作验收，清理类改动可能留下类型错误 |

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
| **I8-92** | 基线计数的**漂移**表述 | 台账 §0 与审计 §14 的"**5 个未提交文件**"是**复核开始时**的快照；此刻工作树已有本轮授权的文档改动（AGENTS/审计/Issue7/TODOLIST/ProdTestList + 新增 Issue8），实测未提交项增至 9 项（**生产 Go 代码仍零改动**）。建议改为**不含计数**的表述（如"HEAD + 本轮文档改动"），避免每次写入后即过期 | 📝 待执行（改为不计数表述） |
| **I8-93** | 审计 **§4 第 3 张表（🔁重复实现）** 成立率 | **2/5 成立，3 项已被后续批次收敛**：① 前端 `theme` 双写 **已失效**（`Settings.vue` 的 `theme` 命中全是 `themeVars` 读取、零写入；报告锚点 `:127-141` 现为 `resetAll`；P3-16 已改为"只提交十个可见字段、不回传 theme"）② 前端重复实现后端校验 **已失效**（`intervalPattern` 已不存在；全前端校验原语仅剩 `Settings.vue:202` 文件名净化与 `Logs.vue:76` 时间戳兜底，`Settings.vue:164` 注释明确"时长语法与正数校验交由后端"）③ 其余 3 项成立（默认值多真值源、presence map、三处健康派生）。**另发现**：`resetAll`（`Settings.vue:131-145`）只做 `POST /api/config/reset` + 800ms 后 `location.reload()`，**从不调用 `clearAllCache`**（无 localStorage/cookie 持久化，故 800ms 窗口无功能影响） | 📝 待执行（§4 表 3 标注 3 项已收敛；补 `clearAllCache` 注释矛盾的确证） |
| **I8-94** | 审计 **§4 锚点漂移全表** | 31 个可核验锚点中 **19 个已漂移**（`config/store.go` 统一 **+48 行**、`internal/health/push.go` **+20 行**）；另有三处**计数/行号错误**：清单 1 的"仅 5 个测试文件"实为 **4 个**（`syncer/syncer_test.go`、`config/store_tx_test.go`、`config/store_corrupt_test.go`、`webui/api/settings_policy_test.go`；`config/runtime_test.go` 与 `store_v3_test.go` 只测 `ToRuntimeConfig`，`\bConfig\b` 在 `config/*_test.go` 零命中）；"11 个仅测试导出"实为 **12 个符号**；`push_test.go` 的 `var _` 锚点为 `:500-502`（报告写 `:16-18,497-499`，其中 `:16-18` 实为 import 块），且"三个无用 import"有误——`config.` 另有 12 处使用（`:500` 纯死代码），`notifier.`/`syncer.` **各仅 1 处即该行**（属 import 保持器） | 📝 待执行（按漂移表更新锚点；**下轮改动前必须先更新锚点，否则会误判"符号不存在"**） |
| **I8-95** | 审计 §5 附表 **H5 行被证伪**（会诱导错误修法） | 报告写"手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` → EXIT=0"，**实测 EXIT=1**：`... cannot embed directory frontend/dist: contains no embeddable files`。**根因是"目录内 ≥1 个可 embed 文件"，不是"目录存在"**；再写 `dist/index.html` 后才 EXIT=0。**建议直接删除或改写该行**，不得保留"EXIT=0"（否则会诱导出"建空目录"甚至"提交占位 index.html"的错误修法） | 📝 待执行（订正 §5 附表 H5 行） |
| **I8-96** | `dns` 包外网依赖的**表述订正**（报告 3 项主张中 2 项不准确） | ① `dns/resolver_test.go:10/29/40` 确实用 `8.8.8.8:53` 且都真调 `Resolve` ✅ ② **"无 `testing.Short()` 守卫"不成立**：`TestResolve_NonExistent`(`:25-27`) 与 `TestResolve_PublicDomain`(`:37-39`) **都有守卫**；唯一无守卫的 `TestResolve_Localhost`(`:9-22`) 经回环探针证明**根本不外发查询**（上游换死地址仍 PASS，走 hosts）③ **但默认门禁确实会执行外网查询**：`make test`（`Makefile:10` 不传 `-short`）与 CI（`docker-publish.yml:68`）都不过滤 ⇒ 守卫失效。**回环探针实测**（上游改指 `127.0.0.1:15353` + UDP 监听）：`-short` → **0 包**；默认语义 → **6 个 UDP 报文**（`host.invalid`×2、`dns.google`×4），dns 包耗时 **15.1s**（5s PASS + 10s SKIP）；全程**未触碰 8.8.8.8**。⇒ I8-04 应改写为"**默认门禁会执行真实外网 DNS 查询**"而非"无守卫" | 📝 待执行（订正 I8-04 与审计对应行的表述） |
| **I8-97** | §6 "7 个永不失败的测试" → **实为 5 个，2 项需重新定性** | 真正"永不失败"的是 **5 个**：#1`dns/resolver_test.go:24-34`、#2`:36-58`、#3`notifier/bus_test.go:108-120`、#6`webui/api/alertset_test.go:176-182`、#7`:216-222`（每项均以独立探针作对照，注入破坏后仍 PASS）。**#4 `syncer/state_test.go:421-448`（报告 411-436，已位移）应改定性为"零断言的 panic/活性守卫"**：去掉 `stopOnce` → 二次 close → **FAIL(panic)**；不再置 `stopped`（A20 门控失效）→ PASS，且兄弟用例 `TestNoNewRoundAfterStop` 也 PASS（**零行为断言，行为破坏双双漏过**）。**#5 `syncer/syncer_test.go:597-627`（自述注释 `:596`）"不带 `-race` 时永不失败"成立，但默认门禁带 `-race`**（`Makefile:10` / CI `:68`），故门禁中**有**判别力——报告该句易被误读为"应删除"。另：`#7` 用例名说 `WithoutRuntime`，但 `Candidate{}` 在 `deps.go:129` 的 **State** 守卫就返回，**并未走到 Runtime 分支** | 📝 待执行（§6 改为 5 个 + 2 项重新定性） |
| **I8-98** | §6 字面量锚点 → **实为 3/5** | `DefaultHealthTimeout=10m`、`DefaultPushInterval=60s`、`MinPushInterval=20s` **已锚定**（`config/store_v3_test.go:430`）；**`InFlightLimit=4` 与 `DefaultStartupGrace` 仍无字面量锚点**（`grep "InFlightLimit == 4"` 0 命中；`health_test.go` 无 `10 * time.Second`） | 📝 待执行（§6 改为 3/5 并点名两项） |
| **I8-99** | 清洁检出下 **`go list ./...` 本身失败**（新发现） | 清洁检出中 `go list ./...` 返回非 0 且无输出 ⇒ 任何 `go test $(go list ./...)` 脚本会**退化成"只跑当前目录"**（静默漏检）；须用 `go list -e ./...`。另实测 `make all` 在第一个前置 `vet` 就中止（**EXIT=2**，`frontend` 目标从未被触达）；overlay 加前置后 `make -n` 显示前置生效且 `make all` 中 `npm ci` 只出现一次（去重成立） | 📝 待执行（补入 I8-03 的影响面） |
| **I8-100** | 清洁检出的**包级结果口径** | 实测：**2 包 `[setup failed]`（root 与 `webui`）+ 9 包真跑全 ok + `dns` 包未跑**（因授权禁止真实外网解析，仅验证到编译层）。⇒ **不得主张"清洁检出可通过"，也不得主张清洁检出里 12 包全绿**；`dns` 在清洁检出中的运行结果**未验证** | 📝 待执行（作为 I8-03/I8-25 的限定写入） |
| **I8-101** | §6 `waitForNoSMTPData` 现状与调用点数 | 函数体**仍是裸 `time.Sleep`**，但**误导注释已改为如实披露**；调用点是 **3 个**（`alertset_policy_test.go:92/150/180`，报告写 4 个）。**判别力成立**：破坏 `policySubscriptions` 触发开关后，用例按 `alertset_policy_test.go:182` **变红** ⇒ 真实判别力在调用点而非该 helper | 📝 待执行（§6 更新为 3 个调用点 + 判别力结论） |
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

1. **I8-01（TODO-001，P0）** — 仍未修复且无认领。
2. **I8-30（TODO-008 语义裁决）+ I8-02** — 唯一被两小组确定性复现的 C 级可观测性缺陷。
3. **I8-03（构建阻断）** — 决定"本地绿色"能否在清洁检出复现。
4. **I8-50 / I8-51** — 两个高危文件仍缺独立第二双眼睛（审计自认的方法论缺口）。
5. **I8-04、I8-05、I8-07** — 门禁密闭性、Push 心跳语义、version 3 null 覆盖。
6. 其余按 I8-xx 顺序处理；文档订正已在本轮完成。
