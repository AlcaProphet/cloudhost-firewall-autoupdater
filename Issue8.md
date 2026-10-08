# Issue8.md — 现存缺陷台账（2026-10-08 复核整理版）

> **本文档只登记经证据确认的真实缺陷、必须裁决的修法、测试证据缺口与外部验收边界。**
> 复核过程中的误判、已被后续证据推翻的旧表述、纯行号/措辞订正、已完成或已失效的专项任务，**一律不再进入正文**：撤销清单见 [附录 A](#附录-a已撤销的误判与不再登记的条目)，对审计报告与 TODOLIST 的待回写事实订正见 [附录 B](#附录-b审计报告与-todolist-待回写的订正)。
>
> - 强要求仍以 [AGENTS.md](./AGENTS.md) 为唯一来源；P1-01 合同见 [Issue7.md](./Issue7.md)；人工验收清单见 [ProdTestList.md](./ProdTestList.md)；业务待办见 [TODOLIST.md](./TODOLIST.md)。
> - **编号沿用原 `I8-xx`** 以便与提交历史互认；**编号不连续是刻意的**——空缺编号即已撤销或已闭环项，一律见附录 A。
> - 证据等级：**① 源码级**（可直接复核到行）｜**② 运行期复现**（仓库外副本探针/overlay）｜**③ 静态不可达**（当前调用链不可达，仅有潜在风险）。
> - 本文档中的"仍存在"以**当前磁盘**为准；同一问题只在正文出现一次，交叉引用关系在各条目标注。

---

## 0. 基线与证据等级

| 项 | 值 |
|---|---|
| 分支 / HEAD | `main` / `da644b5`（本台账整理提交） |
| 本地 `origin/main` | `938bb8b`；ahead/behind `0/3`（**未 fetch**，不代表真实远端） |
| 本轮复核基线 | `938bb8b` + 5 个未提交文件（P3-25 批次，生产 Go 代码零改动）；后续三个提交均为文档/测试，**被复核的生产文件逐字节未变** |
| 证据采集方式 | 仓库外副本执行测试/overlay/探针；**未调用真实云/DNS 上游/SMTP/Webhook/Uptime Kuma** |
| 已确认修复、不再登记 | P0-01；P1-01/P1-02；P2-01～P2-09；P3-01～P3-09、P3-11、P3-13～P3-17、P3-19～P3-20、P3-22～P3-26；F1、F5；R7-01/02/03/05/06/07；I-01～I-19（其中 I-07 的 P3-10 出口④ 仅结构性证据见 §3 I8-22，I-08 的 P3-21 Webhook 子项按合同转为历史事实）。P3-18 的文档漂移残留已在订正中处理；P3-10 出口④、P3-21 Webhook 之外的全部子项均已本地收口 |

---

## 1. 真实缺陷

### 1.1 数据与逻辑

#### I8-01【中低·逻辑错误】`coverage_ready` 在 unsupported-only 计划下误报　`①`　（= TODO-001）

- **现象**：某目标**全部**期望功能都被标记为平台不支持时，`coverage_ready` 仍为 `true`，被下游读作"已覆盖"。
- **证据**：`provider/plan.go:627` 定义 `implementable, covered := 0, 0`；`:631` `implementable++`；`:638/:647` `covered++`；**两个变量此后均未被读取**。`:655` 实际判定为 `len(plan.Desired) > 0 && !in.AddStateUnknown && len(plan.ToAdd) == 0` ⇒ 全部不可实施时 `ToAdd` 为空，可达 `true`。
- **严重度订正（重要）**：**不产生任何删除后果**——清理安全门在 `provider/plan.go:698-701` 对本轮**每一个** `plan.Unsupported` 代码追加"存在平台无法实施的期望规则，保留全部清理候选"，删除集合独立被冻结。消费者也只有两处：`provider/plan.go:709`（即上述已被冻结的门）与 `syncer/syncer.go:738`（Dry Run DTO 字段）。
- **用户可见性**：前端仅在 `webui/frontend/src/types.ts:148` 声明 `coverage_ready: boolean`，**无任何组件读取该字段**。⇒ 实际后果是 **API 输出字段语义错误**，不是"可能误删"。
- **结论**：逻辑错误**确定成立**；但 `TODOLIST.md:30` 标注的 **P0/高 与后果不匹配**（该条目自述亦写"核心安全门已存在"），建议降级为"中低"并保留修法裁决（见 §2 Q-01）。

#### I8-02【中·可观测性】失败 attempt 抹平已确认的清理/不支持明细　`②`　（= TODO-008；呼应 Issue7 §12.7 与审计 `:990`）

- **现象**：目标首次 attempt 已产生可信残留（部分删除/幂等 NotFound）后，后续 attempt 在 Resolve/S0/Add/S1 任一点早退，`cleanup_candidates`/`cleanup_deferred`/`unsupported` 被**覆盖为 0/空**，而 `cleanup_deleted` 仍保留累加值。
- **证据**：`syncer/target.go:102-107` 中 `added`/`deleted`/`cleanupDeleted` 为 `+=`，而 `:104` `unsupported`、`:105` `cleanupCandidates`、`:107` `cleanupDeferred` 为**覆盖赋值**；`runTargetAttempt` 每次进入以零值初始化（`:143`），早退路径 `:156`（S0 失败）、`:179`（Add 失败）、`:188`（S1 失败）直接返回该零值结果。
- **复现**：`outcome=failed added=1 deleted=1 cleanup_candidates=0 cleanup_deleted=1 cleanup_deferred=0`，而云端**仍存 1 条陈旧规则**；对照"仅 1 个 attempt"场景正确保留 `2/1/1`。
- **下游放大**：`webui/api/logwriter.go:99-104` 的详情行以 `candidates > 0` 为门槛 ⇒ `candidates=0` 时整行"清理候选…"被完全抑制，只剩 `deleted=1`，出现 `deleted(1) > candidates(0)` 的自相矛盾输出。
- **合同缺口（需裁决，见 §2 Q-02）**：`syncer/target.go:168-169` 的注释明确要求"S0 已确定的平台限制…若后续 Add 失败，最终 failed 事件仍需保留这些 unsupported 明细（Issue7 §5.3、§7.2）"；而 `syncer/target_retry_test.go:120` 又声明"只保留最终 attempt 的平台限制明细，不重复累加"。⇒ **同一赋值族存在两套期望**，且"后续 attempt 在**规划前**失败"这一情形两处都未覆盖。
- **结论**：行为与输出矛盾**确定成立**；**非 R7-04 引入**（`git show b19d271 -- syncer/target.go` 显示这两行属未改动的上下文），修复前即存在。争点只是**表达方式**（保留前次可信残留 vs 显式表达"未知"），不是"是否存在"。

#### I8-18【中·读一致性】`GET /api/alerts` 由 4 次非事务读拼装，可返回自相矛盾快照　`②`

- **现象**：`webui/api/alerts.go:55/60/65/70` 顺序执行 4 次**独立**读（`config/store.go:894/785/837/958`），全部直接落在 `*sql.DB` 上，**无 `BeginTx`/`BeginReadOnlyTx`** ⇒ WAL 下每条语句自成隐式读事务，不共享快照。
- **确定性复现**：在第 3 读与第 4 读之间提交一次完整写事务 → 响应为 `policy=old, email=old, webhook=old, push=new`，一次 200 响应内部自相矛盾；`-count=5` 稳定。并发夹具（4 writer × 25 PUT + 3 reader，带 `-race`）下未修复实现 **544/546/495 次 GET 中 116/131/105 次四部分不一致（约 21%～24%）**；"同一只读事务取快照"变体连续 5 轮 **0 次**。
- **写侧对照**：`PUT /api/alerts` **是单事务**（`alerts.go:276-278` → 协调器 `BeginTx` → `ReplaceBusinessAlertsTx` → 同事务读快照 → commit → commit 后发布）。注入 `BEFORE INSERT RAISE(ABORT)` → PUT 返回 500 且**四部分全部回滚**。
- **前端会把它固化**：`Alerts.vue:117-131` 用 GET 响应**整体覆盖 4 个 ref**，`:143-152` **无条件回传全部 4 个对象**；校验器 `isAlertsData:55-70` 只校验对象内部字段名/类型，**不校验跨对象版本一致性** ⇒ 撕裂响应必然通过；用户改一个开关保存即把混合快照整体写回。`loadState`/`saving` 守卫都拦不住。
- **测试守护为零**：命中 `GET /api/alerts` 的 6 处测试全为单线程同步取值断言；唯一涉及原子性的是**写侧**回滚用例。判别力实测：未修复实现 + 确定性注入下，整包 `webui/api` 既有测试**全绿**。
- **修复面比预期小（正面证据）**：仓库**已有**一致快照读路径——`config/store.go:1125 LoadBusinessSnapshotTx`（含 policy/email/webhook/push）+ `config/store.go:41 BeginReadOnlyTx`，且 `webui/api/export.go` 的 `handleConfigExport` **正在使用**（`BeginReadOnlyTx` → `LoadBusinessSnapshotTx`）。⇒ 只需让 GET 走该既有路径（或等价的"只读事务内读四对象"），**无需新建读模型**；实测 200/字段集合/`no-store`/`Content-Type`/默认值补齐/非 null/500 出口**全部不变**，既有 12 项告警测试与整包全绿。
- **最坏后果**：静默回退某个**未被触碰**的渠道配置（如刚更新的 Webhook URL），界面与日志无任何提示。
- **可达性限定**：无并发写入时**不可发生**；`sync_enabled=false` 是无关变量（四种写者与同步引擎独立，同步引擎不写这四张表）。
- **修不修见 §2 Q-03**；无论修不修都建议补一条判别性回归。

#### I8-111【中低·输入契约】version 3 校验存在两个字面绕过维度　`②`

- **现象**：①**重复键 last-wins**：`{"version":2,…,"version":3}`、`{"version":1,…,"version":3}`、`{"version":null,…,"version":3}` **全部 200 并整库替换**；`{"targets":null,…,"targets":[]}`、`{"monitoring":null,…,"monitoring":{…}}` 同样被接受（共 5 个 200 载荷）。②**键名大小写不敏感 + 转义**：`{"VERSION":3}`、`{"Version":3}`、`{"\u0076ersion":3}` 均 **200**。
- **对照（正常面成立）**：取值层面 **19/19 非法形态全部 400 且零写入零发布**；57 条字段路径逐字段与 §9.1 一致；单事务四阶段注入 `RAISE(ABORT)` 全部完整回滚。
- **性质**：`encoding/json` 的 last-wins 与大小写不敏感匹配属语言行为；但 `AGENTS.md` §9.1 要求"只接受 version 3、其他版本一律 400"，故该要求在当前实现下**可被字面绕过**。
- **裁决见 §2 Q-08**（收紧需自建 token 级解码或预扫描；不接受则须在 §9.1 显式登记边界）。

#### I8-113【中低·真值源】默认 Webhook 渠道的"新建库 vs 升级库"取值来源分裂　`②`

- **现象**：新建库走 DDL 的**字面量** `'dingtalk'`（`config/store.go:235`），升级库走 ALTER 的**常量拼接** `DefaultWebhookChannel`（`config/store.go:313`）。当前 `config/config.go:24` 的值恰为 `"dingtalk"`，故**当前无实际分歧**；但把该常量单点改为 `"slack"` 后，**新建库得 `dingtalk`、升级库得 `slack`**，而**仓库测试全绿零告警**（已实测）。
- **附带**：`webui/api/bundle_v3.go:353/363` 的 `"587"`/`"dingtalk"` 是**不可达死代码**（对应列恒非空），改它无可观测差异——引用时不得与 DDL/ALTER 并列，否则会掩盖"谁真正决定用户可见值"。
- **裁决见 §2 Q-09**（统一到 `config.Default*` 常量，或补"新建库与升级库默认值一致"的断言）。

#### I8-12【低·前端状态机】Dry Run 失败后渲染绿色「无待变更规则」成功空态　`②`　（原 I8-12 + I8-13）

- **现象**：`webui/frontend/src/composables/useDryRun.ts:17-18` 每次 `run()` 先清空 `results`/`warnings`，`:23` **仅成功分支**设置 `lastRunAt`；`DryRunResults.vue:129-131` 在 `results.length===0 && warnings.length===0` 时无条件渲染 `type="success"` 的「无待变更规则」。
- **触发**：先成功执行一次 Dry Run，再使其失败（409 并发冲突 / 500 / 网络错误）⇒ 页面显示"无待变更规则"，而实际是执行失败（仅 toast 报错即消失）。
- **同源未区分**：**零已配置目标 / 零结果**也走同一文案，未与"已跑且确无待变更"区分（R7-03 语义为"所有已配置目标各返回一项"，零目标即零结果）。
- **状态**：本轮新发现（审计 0 命中），两个小组各自用运行时探针复现。

### 1.2 构建与门禁

#### I8-03【中·可复现性】清洁检出下 Go 侧门禁全部失败　`②`

- **现象**：`webui/embed.go:5` 的 `//go:embed frontend/dist` 依赖被 `.gitignore:5` 忽略的目录，而 `Makefile:9-13` 的 `test`/`vet` **无 `frontend` 前置**（`build` 有）。
- **实测（清洁检出 = `git archive HEAD`）**：`go build ./...` / `go vet ./...` / `go test ./...` = **EXIT 1/1/1**（`pattern frontend/dist: no matching files found`），根包与 `webui` 包 `[setup failed]`；`make vet` EXIT 2；`make all` 在第一个前置 `vet` 即中止（**EXIT=2，`frontend` 目标从未被触达**）。包级结果：**2 包 setup failed + 9 包真跑全 ok + `dns` 包未跑**（授权禁止真实外网解析，仅验证到编译层）⇒ 既**不得主张清洁检出可通过**，也**不得主张清洁检出 12 包全绿**。
- **对照**：当前工作树存在 ignored `webui/frontend/dist` → `go build ./...` EXIT 0。
- **⚠️ 根因不是"目录存在"**：`mkdir -p webui/frontend/dist` 后 `go build ./...` **仍 EXIT 1**（`cannot embed directory frontend/dist: contains no embeddable files`）；必须目录内有 **≥1 个可 embed 文件**。审计 §5 附表 H5 行原写的"建空目录后 EXIT=0"**已证伪，不得据此修**（见附录 B）。
- **附带**：清洁检出中 `go list ./...` 本身失败（非 0 且无输出）⇒ 任何 `go test $(go list ./...)` 脚本会**静默退化成"只跑当前目录"**；须用 `go list -e ./...`。
- **禁止项**：**不得**提交占位 `webui/frontend/dist/index.html`——`webui/server.go:307` 会把占位页当真实 SPA 提供，且 build/vet/test 全变绿，**静默掩盖真实故障**（当前仅 `git add -f` 可绕过忽略规则）。
- **修法见 §2 Q-05**。

#### I8-04【中·门禁密闭性】默认测试命令会执行真实外网 DNS 查询　`②`

- **准确表述（订正后）**：`dns/resolver_test.go:10/29/40` 使用 `8.8.8.8:53` 且都真调 `Resolve`；其中 `TestResolve_NonExistent`(`:25-27`) 与 `TestResolve_PublicDomain`(`:37-39`) **有 `testing.Short()` 守卫**，唯一无守卫的 `TestResolve_Localhost`(`:9-22`) 经回环探针证明**根本不外发查询**（上游换死地址仍 PASS，走 hosts）。**真正的缺陷是默认门禁不传 `-short`**——`Makefile:10` 与 CI `docker-publish.yml:68` 都不过滤，**守卫因此失效**。（审计原表述"无 `testing.Short()` 守卫"不准确，见附录 B。）
- **实测**：上游改指 `127.0.0.1:15353` + UDP 监听——`-short` → **0 个报文**；默认语义 → **6 个 UDP 报文**（`host.invalid`×2、`dns.google`×4），dns 包耗时 **15.1s**。全程未触碰 `8.8.8.8`。
- **影响**：① 无外网/出口受限环境门禁必然变红，失败点与被测产品无关；② 历史"全量 12 包 race 绿"**包含真实外网 I/O**，不能表述为纯本地密闭门禁；③ 清洁检出下 `dns` 包运行结果**未验证**。
- **修法见 §2 Q-06**。

#### I8-102【低·门禁健壮性】`test:alerts` 未设逐用例超时，回归会"永久挂起"而非"变红"　`②`

- **现象**：`webui/frontend/package.json:7` 为 `node --test tests/alerts-load.test.mjs`，**无 `--test-timeout`**；`node:test` 默认逐用例超时无限且顶层用例串行。
- **实测**：旧实现（`064b794^`）不带上限时被 180s 硬墙杀掉、EXIT 142、**输出 0 字节**——用例 1 永久挂在 `tests/alerts-load.test.mjs:72` 的 `await state.save()`，用例 2～5 根本不开始。带上限（`--test-timeout=15000`）后同一实现 **EXIT 1、0 pass / 4 fail / 1 cancelled**。
- **影响**：任何"loading 态发 PUT"类回归都会让门禁**挂死**而不是明确失败。
- **建议**：改为 `node --test --test-timeout=30000 …`（未实施）。

### 1.3 运行时健壮性

#### I8-09【低·崩溃面】Lighthouse / CVM 资源扫描对空响应直接解引用　`②`

- **现象**：`provider/scan.go:72`（`scanTCLighthouse`）与 `:113`（`scanTCCVM`）在未判 `resp`/`resp.Response` 的情况下直接取 `resp.Response.InstanceSet` / `.SecurityGroupSet`。
- **实测**：响应体 `{}` → `invalid memory address or nil pointer dereference`（panic）；全仓生产代码 `recover()` 命中 0 处 ⇒ 由 net/http 每连接兜底，**进程不崩**但该请求异常中断。
- **对照**：同步 Provider 同类路径**已守卫**（`provider/tc_lighthouse.go:88`、`provider/tc_cvm.go:82`）。属**同类保护不对称**。
- **处置见 §2 Q-04**。

#### I8-08【低·数据正确性】SWAS 资源扫描对结构缺失静默截断并覆盖缓存　`②`

- **现象**：`provider/scan.go:153-156`（`scanAliSWAS`）对 `body`/`Instances` 缺失直接 `break`，返回**累计结果 + `nil` error** → `webui/api/scan.go:66` 覆盖写缓存。
- **实测（真实 SDK → handler → 临时 SQLite）**：首页 `{}` → `success=true count=0`，**旧缓存被清空**；首页 100 条满页 + 次页 `{}` → `success=true count=100`，**旧缓存被截断列表替换**。
- **范围**：P3-26 只覆盖 ECS 扫描路径；审计与 `AGENTS.md` 均未声明 SWAS 扫描已修。与 `provider/scan.go:217-239`（ECS 路径返回 `ErrSnapshotIncomplete`）**策略相反**。
- **处置见 §2 Q-04**。

#### I8-10【低·无界循环】Lighthouse `GetSnapshot` 无页数上限 / 进度守卫　`①`

- **现象**：`provider/tc_lighthouse.go:67-124` 仅有 Offset 递增与"本页不足一页即结束"两个判据，**无页数上限、无 token 进度校验**；服务端持续返回满页时循环无界（仅受网络与 SDK 超时约束）。
- **范围**：P3-25 与"分页总预算"候选均只覆盖 ECS 两条路径；Lighthouse 未被任何 finding 覆盖，`AGENTS.md` 亦未要求。属**同类保护不对称**（ECS 有历史 token 集合 + 上限）。

#### I8-11【低·完整性判定】SWAS 分页：跨页更小/为 0 的 `TotalCount` 会提前 `proven`　`②`

- **现象**：`provider/ali_swas.go:95-97` 每页覆盖 `totalCount`，`:113` 用它判 `proven`；若后续页返回更小或 0 的 `TotalCount`，会以**截断快照成功返回**。
- **实测**：TotalCount 3→1 时返回 2 条；TotalCount 0 且有 1 条时返回 1 条；两者 `err=nil`。
- **依赖**：云端自相矛盾输入，真实云未复现；与 F5"不能证明完整却成功返回"同类。属"证明完整性"的判据可被单页小值降级。

#### I8-17b【低·功能不可用】`NewResolver("::1")` 等 IPv6 字面量上游必然失败　`②`

- **现象**：`dns/resolver.go:33-35` 对不含端口的地址直接补 `":53"`；对 IPv6 字面量 `::1` 得到 `"::1:53"` → `net.SplitHostPort` 报 **`too many colons in address`** ⇒ **IPv6 字面量 DNS 上游不可用**（`[::1]:53` 形式可用）。
- **证据**：副本探针实测 `::1:53 → too many colons`；删除自动补端口逻辑后 `TestNewResolver_PortAppend`（`dns/resolver_test.go:60-71`）**仍 PASS**（它只断言返回非 nil），而探针 **FAIL** ⇒ 注释声称的补端口行为**零守护**。
- **影响**：`AGENTS.md` §四允许自定义 DNS 服务器；用户填 IPv6 字面量时配置可保存，但解析必然失败，且报的是地址解析错误而非明确提示。
- **建议**：改用 `net.JoinHostPort`（自动处理 IPv6 方括号），并补一条"IPv6 字面量 + 端口补全"的判别性用例。

#### I8-05【中低·健康可见性】Push 心跳等待被任意配置保存整段重置　`②`

- **现象**：`internal/health/push.go:152-165` 的 wake 分支只做 `timer.Stop()` 后回循环顶部，URL 未变时重新 `NewTimer(interval)` **整段重新计时**；而任意页面保存都会经 `webui/api/deps.go:168` 触发 `Push.Wake()`。
- **实测**：interval=400ms 时，对照组 1.2s 内 4 次请求；实验组每 100ms Wake 一次 → 1.2s 内**仅 1 次**。
- **影响**：连续保存可无限推迟心跳 → Uptime Kuma 侧可能误判 DOWN。Syncer 的同类问题已由 P3-23/P3-24 按"同间隔保留剩余等待"修复，**Push 未同步该合同**，且**无任何测试覆盖该语义**。
- **裁决见 §2 Q-07**。

#### I8-06【中低·配置边界】正常发送路径不应用 20s 间隔下限　`②`

- **现象**：`internal/health/push.go:151-155` 只要 `interval > 0` 就原样使用；`MinPushInterval` 仅在非法 URL 重校验分支（`:169-178`）与 API/导入校验边界生效，`config/store.go:966-993` 只要求时长为正。
- **实测**：合法 URL + `interval=1ns`（仅能由直改 SQLite 绕过 API/导入产生，已有 `TestPushDirtySQLiteConfigReachesRuntime` 证明此类配置可达运行快照）→ **300ms 内向回环 mock 发出 1211 次请求**。
- **性质**：**既有行为**（非 P3-03/P3-21 回归）；与 P3-03 为非法 URL 分支补下限的做法**不对称**。可与 Q-07 合并处置。

#### I8-17f【低·效率】`runRound` 在同云分组**最后一个目标之后**仍等待一个完整厂商间隔　`②`

- **现象**：`syncer/syncer.go:951` 的 `s.sleep(rateLimitInterval(ct))` 位于逐目标循环末尾，对分组内每个目标（含最后一个）都执行 ⇒ 单目标分组每轮多等 1 个间隔（Lighthouse/SWAS **5s**、CVM/ECS **200ms**）。
- **影响**：该等待**计入 `duration_ms`** 并**推迟 ticker Reset**。方向与 P3-22 对 Dry Run"取消末尾等待"的处理**相反**，正式同步侧仍有残留。
- **建议**：仅在"后面还有目标"时限速（最小改动、无契约变更）。

#### I8-43【低·可复现性】`bundle_v3.go` 的 presence map 使同一非法请求的错误文案漂移　`②`

- **现象**：`webui/api/bundle_v3.go` 的字段缺失检查用 `map[string]bool` 迭代，**同一非法请求、同一进程内 300 次即观察到 3 种（policy，3 字段缺失）与 8 种（email，8 字段缺失）不同文案**。
- **影响**：错误文案不可复现，影响排障、日志比对与用户沟通。
- **建议（强烈·零破坏）**：改为与**同文件既有正确写法** `normalizeBundleSettings`（`bundle_v3.go:678-…` 的顺序 `if` 链）一致；实测 `grep '字段缺失' --include=*_test.go` 的 4 处命中全为子测试名/注释，断言只校验状态码 ⇒ **修复零测试破坏**。

### 1.4 残留、一致性与可观测性

#### I8-14【低·潜在回归】旧描述身份实现仍随包发布，且仍复现原缺陷语义　`②`

- **残留物**：`provider/common.go` 保留第二套 key 与旧描述身份路径——`OwnedRules:68`、`Diff:139`（`:152` 以 `Description` 相等判归属）、`buildDesired:207`（`:224-228` 静默 `continue` 跳过 IPv6/ICMPv6）、`normalizePortForCompare/keyOf/keyOfAction:96-131`。
- **实测**：该路径仍会 ① 空 comment 碰撞时把同目标另一条规则列入 `ToDelete`（`ToAdd=1 ToDelete=1`）② Lighthouse `ICMPv6` 与期望 `ICMP` 不收敛（`1/1`）。
- **当前风险**：**无生产消费者**（`Diff`/`buildDesired`/`OwnedRules` 的调用者全部在 `provider/common_test.go`），但**无门禁阻止重新接线**；且与 `provider/plan.go:20` 自述"全仓只有这一个 canonical key 层"矛盾。
- **建议**：删除或加"仅供历史回归"的显式标注 + 门禁（属低风险清理，非必须）。
- **附带缺口**：`PlanTarget` 层的 TCP+UDP 拆分（`provider/plan.go:471-474`）**无任何仓库测试**（`grep 'TCP+UDP'` 只命中旧 `Diff` 路径与 config 校验）。

#### I8-15【低·维护性】注释/死赋值与实现不符　`①`

| 项 | 位置 | 问题 |
|---|---|---|
| a | `webui/api/export.go:88-95` | 注释仍是 P2-04 修复**前**的顺序（"→ 发布 RuntimeState → 返回成功"写在唤醒之后），与现行"先发布后唤醒"相反 |
| b | `provider/plan.go:742` | 注释称 CVM"候选间索引重复 → deferred"，实际在 `provider/tc_cvm.go:213-215` 返回**硬错误**（"拒绝删除"） |
| c | `internal/health/push.go:210-216` | 首次 `buildPushURL` 的返回值被 `_ = target` 丢弃（仅作占位校验，死赋值） |
| d | `provider/plan.go:153-154,655` | `TargetPlanInput.AddStateUnknown` 全仓**从未被置 true**，合同子句无执行点 |

#### I8-16【低·测试夹具】测试夹具与断言残留　`①`

| 项 | 位置 | 问题 |
|---|---|---|
| a | `notifier/inflight_test.go:89-98` | 仍丢弃 accepted `net.Conn`（与 R7-06/R7-07 同模式）；因断言放宽（`:120-132` 任意错误均可）**不 flaky**，但丧失"应用层 deadline 生效 vs 连接被重置"的判别力 |
| b | `internal/health/push.go:48` + `push_test.go:459` | **订正**：`pub.count()==0` **不是永真断言**（注入一次 `Publish` 后 5/5 FAIL）⇒ 断言可判别。真问题是**死字段**：生产 `p.bus` 恒为 nil、`push.go` 从不读它、`run.go:155-164` 也不传 `Bus`。建议补正向控制而非删断言 |
| c | `syncer/stop_gate_test.go:200` | 断言 `p.calls.Load() != 2`，失败文案却写"want 1"（文案陈旧） |
| d | `notifier/email.go:224-227` / `:229` | 丢弃 `c.Close()` 错误（未计入 P3-19 的 11 个出口）；忽略 `Extension("STARTTLS")` 错误（既有行为） |
| e | `webui/api/alertset_test.go:216 TestApplyCandidateWithoutRuntimeIsNoop` | **名实不符**：`d := &Deps{}; d.applyCandidate(Candidate{})`，而 `Candidate{}` 的 `State` 为 nil ⇒ 在 `webui/api/deps.go:129-131` 的**首个守卫**即返回，**从未走到用例名声称的"无 Syncer/Runtime"分支**。变异"只在 else 分支 panic" → 原用例 PASS、真实探针 FAIL |

### 1.5 静态不可达的一致性缺口（当前无生产后果）

| 编号 | 位置 | 问题 | 不可达原因 |
|---|---|---|---|
| I8-17d | `syncer/syncer.go:378-390 setSyncEnabled` | 读-改-写**未持 `s.mu`**（对比 `ApplyState` 持锁）⇒ 窗口内 `Resume` 被覆盖 **8/8**、窗口内协调器发布的**已提交 TAG 配置被陈旧快照回滚 8/8** | `Pause()/Resume()`（`:365/:370`）**无生产调用方**（`webui/api` 接口成员已删，仅注释引用）。**未来任何重新接线都会带入该竞态**；审计全程零覆盖。处置见 §2 Q-10 |
| I8-17e | `syncer/dns_round.go:38-40` | `h := r.hosts[...]` 后直接 `h.open`，域名不在本轮快照时 nil 解引用 | 调用方只传本轮配置中的适用规则（代码注释即以此为前提）；属**缺失前置断言** |
| I8-47 | `webui/api/bundle_v3.go`（import）、`webui/api/export.go` | `Deps.Store == nil` 时直接 panic | 与 P3-14 已修的六处 nil→**503** 口径不一致（同样"依赖未接线"）；当前接线不可达 |
| I8-44-1 | `config/runtime.go:137-143 mustDuration`（`:146-152 mustThreshold` 同族） | 解析失败**静默返回 0**；且 `mustDuration("-5m")` 返回 **`-5m`**（`time.ParseDuration` 接受负号，函数无正数校验）⇒ 若真为 0/负值，`time.NewTicker` 会 fatal | 两条生产路径（启动加载、协调器候选构造）都经 `normalizeSettings`（`config/store.go:1155`） |
| I8-44-2 | `internal/health/health.go:195-197` | `TriggerEnabled` 有 `Policy == nil → false` 检查，而 `Evaluate` 无 ⇒ 语义不一致 | 生产 `run.go:138-143` 恒传 Policy |
| I8-44-3 | `webui/server.go:305-308` | `fs.Sub` 失败被静默吞掉，静态处理器被**无日志**禁用 | `//go:embed` 在缺目录/空目录时**编译期**失败；但在"dist 存在却不含预期内容"时仍可能静默降级。建议至少补一行 `slog.Error`（零风险，只增可观测性） |

---

## 2. 待裁决事项

> 均为"改动前需要你定方向"的争点；只读研究结论已附在候选方案内。纯代码整洁事项本身**不登记为缺陷**，只保留一项"是否实施清理"的开关式裁决（Q-13）。

| 编号 | 争点 | 候选方案 | 影响面 |
|---|---|---|---|
| **Q-01** | **I8-01** `coverage_ready` 的修法 | ①用已算出的 `implementable/covered`（`covered == implementable` 才置 true）——最小改动，语义即"全部可实施期望都已覆盖" ②重新定义字段语义，把"不可实施"单列为 `partial` 依据 | `provider/plan.go:627-655`、Dry Run DTO、`AGENTS.md` 相关条款 |
| **Q-02** | **I8-02** failed 路径下 `cleanup_candidates`/`cleanup_deferred`/`unsupported` 如何表达（= 原 I8-30 与原 I8-30a 两套期望的合并裁决） | ①**保留前次可信残留**（与 `added/deleted` 累加语义对称；独立复核的修复形状经验证：探针转绿且**既有正式用例全套仍绿**，但需同步修订 `target_retry_test.go:120` 的注释）②**维持"只保留最终 attempt"**（则须修订 `syncer/target.go:168-169` 的注释与审计待裁决标注）③按字段区分（`cleanup_*` 保留、`unsupported` 只留最终） | `syncer/target.go:102-107/156/179/188`、`:168-169`、`:223-224`、`syncer/target_retry_test.go:120`、`webui/api/logwriter.go:99-104`、`RoundSummary`/事件/SQLite 详情、PT-I7-07 观察基准 |
| **Q-03** | **I8-18** `GET /api/alerts` 撕裂窗口是否修复 | ①改走**既有** `BeginReadOnlyTx` + `LoadBusinessSnapshotTx`（`export.go` 正在用），契约零变更、既有测试全绿 ②接受现状（内部使用、单客户端）并在 `alerts.go` 与 `AGENTS.md` 显式登记"读侧非原子 + 前端整体回传"边界与"不支持多写者并发编辑"前提 | `webui/api/alerts.go`、`config/store.go`、`Alerts.vue` |
| **Q-04** | **I8-08 / I8-09** SWAS 扫描缺失集合 与 Lighthouse/CVM 扫描空响应的处置 | ①统一为保守失败 `ErrSnapshotIncomplete`（与 P3-26 的 ECS 策略一致，不覆盖缓存）②仅补 nil 守卫防 panic ③显式接受现状并文档化 | `provider/scan.go:72/113/153-156`、`webui/api/scan.go:49-69` |
| **Q-05** | **I8-03** 构建阻断的修法 | ①给 `Makefile` 的 `test`/`vet` 加 `frontend` 前置（`build` 已有，overlay 验证前置生效且 `npm ci` 只跑一次）②在 CI/文档中明确"裸 Go 命令需先构建前端" ③其他 | `Makefile:9-13`、`webui/embed.go:5`、`.gitignore:5`、`AGENTS.md` §八 |
| **Q-06** | **I8-04** `dns` 包真实外网依赖 | ①改为本地 DNS mock（最彻底，门禁密闭）②统一为默认加 `-short` 并为两种模式定合同 ③接受现状并文档化 | `dns/resolver_test.go`、`Makefile:10`、CI workflow |
| **Q-07** | **I8-05 + I8-06** Push 心跳的"重置语义"与"下限语义" | ①对齐 Syncer：仅 URL/启用状态变化才重新计时，同间隔保留剩余等待；并让正常路径也应用 `MinPushInterval` ②显式接受现状并在 `AGENTS.md`/Build7 写明代价（连续保存可推迟心跳） | `internal/health/push.go:138-178`、`webui/api/deps.go:168`、`AGENTS.md` §三/§五 |
| **Q-08** | **I8-111** version 3 是否收紧两个绕过维度 | ①**收紧**：显式检测重复键与大小写/转义变体，非精确 `"version":3` 一律 400（需 token 级解码或预扫描，复杂度中等）②**接受现状并文档化**：在 `AGENTS.md` §9.1 与 `bundle_v3.go` 注明"last-wins 与大小写不敏感是 `encoding/json` 已知边界" | `webui/api/bundle_v3.go`、`webui/api/decode.go`、`AGENTS.md` §9.1 |
| **Q-09** | **I8-113 / I8-48** 默认值真值源 | ①统一到 `config.Default*` 常量（DDL 改为引用常量，消除分裂）②保留字面量但在测试中断言"新建库与升级库默认值一致" | `config/store.go:235/313`、`webui/api/bundle_v3.go:353/363`、`config/validate.go` |
| **Q-10** | **I8-17d / I8-41** `Syncer.Pause()/Resume()` 的去留与并发语义（"是否保留"与"保留后如何正确"是两个问题） | ①保持导出但读-改-写持 `s.mu` ②改非导出（消除跨包误用面，包内测试仍可用）③登记为"已知残留风险（当前无生产调用方）"并在 `AGENTS.md` 注明 | `syncer/syncer.go:365-390`、`webui/api/deps.go` |
| **Q-11** | **I8-31 / TODO-003** CVM `DescribeSecurityGroupPolicies` 调用失败的错误分类 | ①包装为 `ErrSnapshotIncomplete`（与 `:254/:262` 的结构/计数缺失一致）②写入契约，明确归"普通/可重试错误" | `provider/tc_cvm.go:248-250` vs `:254/:262` |
| **Q-12** | **I8-38** 分页操作总预算（**未实施的研究候选**，`AGENTS.md` §七既无总预算条款也未排除，实施前须新增条款） | ①**页间检查总预算**（复杂度低、对共享 `ClientPool` 无影响，推荐首选）②按剩余预算限制每页（**机制上必然**扩大 SDK 全局池键空间，`dara/core.go:71` 池无 Delete/TTL/容量，且 `:349-357` 每请求覆写共享 `httpClient.Timeout` 存在同 tag 并发覆写窗口 ⇒ 不建议直接实施）③隔离操作级 HTTP 客户端（需自证隔离与回收） | `provider/ali_ecs.go`、`provider/scan.go`、SDK `RuntimeOptions`、`getDaraClient` 全局 `sync.Map` |
| **Q-13** | **I8-42** §4 清理类改动**是否实施**（本身不是缺陷，仅为"可安全清理清单"） | 八符号可零成本改非导出；5 项可安全清理（其中 `Config`+`LoadConfig`+`ToConfig` **必须合并为一个原子改动**）；前端 2 项只能从返回对象移除；`Store.SetSetting` 类波及 **14 个跨包测试文件**，建议暂缓 | `config/config.go`、`notifier/bus.go`、`provider/provider.go`、前端 `types.ts`/composables |

---

## 3. 测试与门禁证据缺口（非产品缺陷，但影响结论可信度）

| 项 | 状态与结论 |
|---|---|
| **I8-22** P3-10 出口④（日志渲染错误）判别证据 | **E（证据不足）**。生产实现按合同正确（`webui/api/logstream.go:121-134`，err 非 nil 在加锁前 return），**但仓库内无任何测试能让 `renderLine` 失败**（内部硬编码 `bytes.Buffer`、无 writer 接缝，探针证 err 恒 nil）；"忽略 renderLine 错误"overlay 后相关测试**仍全绿**。审计 `:1081` 声称的 `TestRenderFailureNoPublish` **全仓 grep 零命中**（见附录 B）。⇒ 该出口属"规范性补齐"，不得声称有判别性验证 |
| **I8-23** A20 的修复前红灯负向控制 | **未执行**（需 `b38678a` 之前的 `syncer/syncer.go` 构造 overlay）。现有证据为静态调用点核验（4 处 `beginRound` 门控）+ 现行 `race ×5` 6 项全绿 |
| **I8-24** P1-01 的独立 overlay 判别力 | **E（自评）**。独立复核只做了 20 个自建探针（19 绿 / 1 红为其自身断言错误），**无独立红→绿 overlay** |
| **I8-25** 全量门禁的完整复现 | **部分**。构建专项因安全边界**整包排除 `dns`**，只跑 11 包 race；各组普遍只做 `-count=1` 或定向 `-count=20`；审计声称的"全仓 12 包 race 连续 3 轮"**本轮未复跑**。⇒ 单次绿色不外推长期稳定；`dns` 包不得登记为通过 |
| **I8-17g** `TestStateAppliedHookFiresOncePerApplyState` 偶发红灯根因 | 夹具观察竞争：hook 内 `Add(1)` 与 Store 之间**无同步** + 轮询式观察。确定性复刻探针在 channel 同步重写后 **50/50 绿**；隔离 `-count=200` **0 失败** vs 重载 `-count=20` **恰 1 次红** ⇒ **"单项 20 轮通过"不构成稳定证据**（与 I8-25 同口径） |
| `provider/scan.go` 腾讯两条扫描路径 | `scanTCLighthouse`/`scanTCCVM` **各 0 测试引用**（I8-09 的 panic 面因此长期无守护） |
| goroutine / fd / ticker 泄漏断言 | **全仓 0 处**（唯一命中是 `notifier/inflight_test.go:210` 的**否定式注释**）。探针实证：注入泄漏 goroutine 后 `pre=2 post=3` FAIL，而仓库既有 `TestStopIdempotent` 同变异 **PASS** |
| `DefaultStartupGrace` 字面量锚点 | **唯一无锚点的合同数值**（`10s→12s` 后 `./internal/health/` 全绿 42.4s）——它是 Build7 Step 7 的合同值，风险最高。其余四个常量均有（间接或直接）锚点：`InFlightLimit=4`（`alertset_drop_log_test.go:41` 字面量）、`DefaultHealthTimeout=10m`/`DefaultPushInterval=60s`/`MinPushInterval=20s`（`config/store_v3_test.go:430`） |
| 负面 sleep 假通过 | 清单：`syncer/state_test.go:156/162/184/214/222/237`、`state_applied_hook_test.go:38/65/86`、`syncer_test.go:166/209/255`（syncer 包 33 处 `time.Sleep`、**11 处 >200ms**、最长 600ms） |
| 前端类改动的验收标准 | 必须写明为 **`npm run build`（= `vue-tsc && vite build`）通过**：`webui/frontend/tests/` 的 4 个测试用 **mock** 顶替 `useSettings`（`p316-state.test.mjs:36` 的 mock 只列被用成员），故前端清理类改动**既不会破坏这些测试、也不会被它们检出** |

---

## 4. 外部验收边界（全部未通过）

**16 项主清单**（`ProdTestList.md`）：PT-B7-01～09、PT-I7-01～07。

| 状态 | 项 |
|---|---|
| **人工免除（非通过）** | PT-B7-02（真实自动邮件）、PT-B7-03（真实钉钉/飞书/Slack） |
| **未执行** | 其余 14 项 + PT-AUDIT-01（P3-04 浏览器）+ 真实家庭 DDNS 连通性与真实 DNS 上游轮次节奏 |

**不得表述为通过**。`PT-B7-04/-06/-08`、`PT-I7-02/-04` 在审计中从未被单独点名，已逐项在册。

---

## 5. 处理优先级建议

1. **Q-02 先行**——它同时决定 I8-02 的修法与 `syncer/target.go:168-169`、`target_retry_test.go:120` 谁改，是唯一"两个小组确定性复现 + 两套期望并存"的争点。
2. **Q-01**——`coverage_ready` 逻辑错误确定成立、修法极小（用已算出的 `covered == implementable`）；同时应把 `TODOLIST.md:30` 的 P0 降级（见附录 B）。
3. **Q-03**——alerts 撕裂已确定性复现且修复面已知（走既有 `LoadBusinessSnapshotTx`），建议直接修 + 补判别性回归。
4. **Q-05 → Q-06**——决定"本地绿色"能否在清洁检出复现、能否在无外网环境运行；注意 H5 已被证伪且占位 `index.html` 属禁止项。
5. **Q-09 / Q-08**——两个"当前无告警但用户可见后果"的静默漂移（默认渠道来源分裂、version 3 字面绕过）。
6. **I8-43 / I8-17f / I8-17b**——三处零风险或极低风险的确切修复（错误文案确定性、末尾等待、IPv6 字面量）。
7. **Q-04 / Q-07 / Q-11 / Q-12**——策略性裁决，可按外部验收结果再定。
8. **§3 的测试缺口**——优先补：`provider/scan.go` 腾讯两路径、`DefaultStartupGrace` 锚点、一处 goroutine 泄漏断言、`test:alerts` 的 `--test-timeout`。
9. **Q-10 / I8-14 / I8-47 / I8-44**——当前不可达，可与后续改动合并处理。

---

## 附录 A：已撤销的误判与不再登记的条目

> 这些条目曾在复核过程中被登记为"问题"，但**已被后续证据推翻、或本就属非缺陷**。保留在此以防再次被当作新发现引入。

| 原编号 | 曾登记的表述 | **实际情况（以此为准）** |
|---|---|---|
| I8-07（旧版） | "version 3 的对象型 `null` 只有 1 处用例、10 类无用例" | **推翻**：null 专属用例 **4 处**（`version:null` `import_test.go:113`、`targets:null` `:118`、`target_export_ids:null` `:131`、`monitoring:null` `alerts_v3_test.go:211`）；`rules/settings/alerts/alerts.policy/uptime_kuma_push/metadata/metadata.exported_at` **有"缺失形态"用例走同一守卫分支，删守卫会变红**；真正无任何用例守护的是 **30 个字段**。⇒ 按旧表述补测会**重复补 6 类、漏掉 24 个** |
| I8-16b（旧版） | "`pub.count()==0` 是永真断言、无判别力" | **推翻**：注入一次 `Publish` 后 **5/5 FAIL**，断言可判别。真问题是**死字段**（`PusherDeps.Bus` 生产恒 nil、从不读），已改登记为 I8-16b |
| I8-04（旧版） | "dns 测试**无** `testing.Short()` 守卫" | **推翻**：2 个用例**有**守卫，第 3 个（`TestResolve_Localhost`）**根本不外发查询**。真问题是**默认门禁不传 `-short`**（已改写为 I8-04） |
| 审计 §6 | "7 个永远无法失败的测试" | **推翻**：应拆为 **A/B/C/D/E 共 11 项**，其中**真·不可失败仅 1 项**（`dns/resolver_test.go:24`，`Resolve` 恒返回硬编码 `1.2.3.4` 仍 PASS）；B 类 5 项"仅 panic 可失败"；C 类 1 项"失败被 `t.Skip` 吞掉"（DNS 全挂可长期显示 SKIP 而非 FAIL）；D 类 1 项空测试（但默认门禁带 `-race`，**门禁中有判别力**，不得表述为"应删除"）；**E 类 2 项属审计误报**——`alertset_test.go:183` **有失败路径**，`push_test.go:459` 见上条 |
| I8-98 | "字面量锚点 3/5" | **被 I8-107 的变异实验取代**：实为 **1/5**，唯一无锚点的是 `DefaultStartupGrace`（其余四个均有锚点，`InFlightLimit` 为间接锚点） |
| I8-39 | "`MultiHandler` 若降级为非导出需先改强要求 → 待裁决" | **前提已消失**：自写类型已在 P3-13（`0818197`）删除，现为标准库 `slog.NewMultiHandler`（`app/logutil.go:41`），`AGENTS.md` 已同步改写。**不存在该动作，无需裁决** |
| I8-19 | "P2-06 判别力未取回，旧实现不变红" | **已闭环：判别力成立**。上次" >120s 取不回结果"的根因是**夹具挂起**（用例 1 永久挂在 `await state.save()`）+ `test:alerts` 无逐用例超时；加上限后旧实现 EXIT 1、**两条纯行为红**（不完整载荷后仍发 PUT、重复保存两个在途 PUT） |
| I8-20 | "P3-12 WAL/SHM 时序判别未执行" | **已闭环：判别性成立**。变体 A（删除打开前 chmod）FAIL、变体 B（移到 `sql.Open` 之后）PASS ⇒ 真正边界是 `config/store.go:163` 的 PRAGMA。**技术订正**：失败模式是 `SQLITE_READONLY`（unix VFS 在 `O_RDWR` 失败后回退只读打开），**不是 EACCES** ⇒ 只查"OpenStore 是否报错"不够，必须加"打开后写入"观测点 |
| I8-106 | "前端 8 字段载荷契约缺测试 / `app/logutil_test.go` 缺 MultiHandler 用例" | **两项均已修复**：`alerts-load.test.mjs:178-180` 已用 `Object.keys(...).sort()` 深比较固定 8 键（变异多带 `enabled` → FAIL 9 vs 8）；P3-13 已删自写 MultiHandler 并有 4 个直接用例（`go test ./app/` ok） |
| I8-30a | "同一赋值族两套期望 → 单独裁决" | **合并进 Q-02**（同一问题不重复登记） |
| I8-13 | "Dry Run 零目标/零结果未区分" | **合并进 I8-12**（同一文案、同一组件） |
| I8-26 / §7 | `.git/objects` 新增 8 个松散对象、`git cat-file` 间歇解析失败、副本软链未查、无网络层取证 | **属复核过程记录，不是产品问题**，移出本台账（结论表述为"在可比对范围内未发现差异"）。`/tmp` 证据目录的软链与敏感文件检查、网络层取证仍为未执行项，但**不影响任何产品结论** |
| §4 全节 | "未派发 / 待启动的专项" 10 项（I8-50～I8-60） | **全部已执行闭环**：两份"第二双眼睛"（`syncer/syncer.go` 主体 Q1–Q4/Q6=A、Q5=B；`bundle_v3.go` Q8/Q9取值/Q12/Q13/Q14=A）、§4 四张表、§6 三份、§3 资源专项、生命周期与崩溃面、§13/§14 边界、17 条正向控制、历史批次 6 核销、分页总预算研究。**该节已整体移除**，结论并入正文与附录 B |
| §4 部分 | "`MultiHandler` 降级"、"`health.go Policy==nil` 需删"、`buildPushURL` 重复计算、死代码/仅测试导出清单 | **非缺陷**：前两项见 I8-44-2 与 I8-39；`buildPushURL` 重复计算与"死代码/未使用导出"属代码整洁，**不登记为缺陷**（是否清理见 Q-13） |
| §5 部分 | 44 条"文档订正项"中已完成或纯行号类的部分 | **已执行部分不再登记**；仍具决策价值的事实性订正压缩进附录 B |

---

## 附录 B：审计报告与 TODOLIST 待回写的订正

> 这些是**证据/文档层面的事实错误**（不是产品缺陷），会误导后续复核，建议回写。按主题合并，不再逐行罗列。

| # | 位置 | 订正内容 |
|---|---|---|
| B1 | 审计 §5 附表 **H5 行** | "手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` → EXIT=0" **已证伪**：实测 EXIT=1（`contains no embeddable files`）。根因是"目录内 ≥1 个可 embed 文件"。**建议删除或改写该行**，否则会诱导"建空目录"甚至"提交占位 index.html"的错误修法（见 I8-03） |
| B2 | 审计 §6「7 个永远无法失败的测试」 | 改为 **A/B/C/D/E 共 11 项**（真·不可失败 1 项），并删除 2 项误报（`alertset_test.go:183`、`push_test.go:459`）；`dns/resolver_test.go:36` 的失败路径被 `t.Skip` 吞掉应单独点名；D 类空测试需注明"默认门禁带 `-race`，门禁中有判别力"。同时订正行号与计数：`syncer/state_test.go:411-436` → **`:421-447`**；`syncer/syncer_test.go:646-670` → **注释 `:595-596` + 函数 `:597`**；`waitForNoSMTPData` 调用点 **3 处**（`alertset_policy_test.go:92/150/180`，非 4 处）；§6 规模口径过期（现 **94 个 Go 测试文件 / 31,373 行**，起 goroutine 文件 36 个） |
| B3 | 审计 §6 字面量锚点 | 改为 **1/5**，并点名 `DefaultStartupGrace` 为唯一无锚点的合同数值；`push_test.go` 的 `250` 锚点在 `:246`（审计写 `:243`） |
| B4 | 审计 §6「`webui/api` 对 `InFlightLimiter` 零断言」 | **不成立**：grep 现 3 行命中、退出码 0（`alertset_drop_log_test.go:40/114`、`alertset_subscription_test.go:28`），且 `:42/:116` 直接断言 `l.Acquire()` 布尔、`:88-121` 用本地 HTTP 服务器实收请求数断言投递次数 |
| B5 | 审计 §7 第 17 条（`isRetryable` 字符串兜底的依据位置） | 两处事实订正：① `syncer/retry.go` 现全文 **104 行**，原引 `:123-175` **已失效**，依据实际在 **`:11-30`**（关键 `:23`）② "全仓 `.md` 内不含 `Unwrap` 说明"**被证伪**（`Issue6.md:584` 等以 `.md` 记录了同一裁决）。规范结论"不得归因为 `.md` 注释"仍成立，但须改写为"依据在 Go 注释；`.md` 亦有同一记录" |
| B6 | 审计 §13 行数口径 + §0.5 可追溯性 | §13 表格 17 个 `\|` 行 = 表头 + 分隔 + **15 数据行**（另有结论段 1 行），引用时必须写明口径；§0.5 与 §13 **无法逐项追溯 16 项 PT**（§13 只点名 3 项）⇒ 应指向 `ProdTestList.md` 与本台账 §4 |
| B7 | 审计 §13「`syncer/syncer.go` 第二个独立复核未完成」与 §11/§13「`bundle_v3.go` 第二双眼睛未完成」 | **两处均已完成**，应更新状态并附结论（见附录 A 的 §4 全节行） |
| B8 | 审计 §4 第 3 张表（🔁重复实现） | **2/5 成立，3 项已被后续批次收敛**（前端 `theme` 双写已失效、前端重复实现后端校验已失效）；另`resetAll` 从不调用 `clearAllCache`（无持久化，800ms 窗口无功能影响） |
| B9 | 审计 §4 锚点与计数 | 31 个可核验锚点中 **19 个已漂移**（`config/store.go` 统一 +48 行、`internal/health/push.go` +20 行）；"仅 5 个测试文件"实为 **4 个**；"11 个仅测试导出"实为 **12 个符号**；`push_test.go` 的 `var _` 锚点为 `:500-502`（且"三个无用 import"表述有误）。**下轮改动前必须先更新锚点**，否则会误判"符号不存在" |
| B10 | 审计 §11/§4/P2-03/P2-08/P2-09 的符号与行号 | 4 处漂移：§11 `:1628` 调用链仍写已删除的 `syncDomain/retrySyncDetailed`（现为 `Run → syncAll → runRound → syncTarget → runTargetAttempt/runTargetCleanup`）；§4 `:1204` 的 `syncer.go:813` 失效（真实为 `:716/:951` 一带）；P2-03 行 `:694` 的"syncer 内 ECS+ICMP WARN"已不存在（现为 `provider/plan.go` 的 `IssueUnsupportedICMPv6`）；P2-08/P2-09 前提行号过期 |
| B11 | 审计 §2/§8.2 与 §11 的陈旧符号链 | `syncDomain → retrySyncDetailed` 已由 `ba82292` 删除（全仓 0 命中），按 §0.2 口径属"引用时快照"，建议统一加符号级警示 |
| B12 | **`TODOLIST.md:30`（TODO-001）** | **优先级 P0/高 与后果不匹配**（该条目自述已写"核心安全门已存在"）。按 I8-01 的核实：清理门在 `provider/plan.go:698-701` 独立冻结，前端无消费者 ⇒ 实际后果是 API 输出字段语义错误，建议降级为"中低"并在描述中写明"无删除后果" |
| B13 | 审计 §0.2 与 P3 清单的**口径** | 已订正部分（P3-24/P3-18/P2-07 的"尚未提交"、§14 与附录 A 的过期基线、P3-06 同格双结论、P3-17 九→十文件、P3-24 他机证据路径）**已随 `b658492`/`fd6ed7e` 入库，不再列出**；采集时行号会漂移，后续引用请用「符号/章节名 + 行号」双锚点 |

---

> **基线说明**：本台账整理时的 HEAD 为 `da644b5`（`origin/main` 仍 `938bb8b`，ahead 3、未推送）。正文全部结论针对**生产工作树内容**，复核期间被复核的生产文件在两个 revision 间逐字节相同，故结论对当前 HEAD 同样成立。**生产源码、测试、API/schema、依赖、部署配置全程零改动。**
