# Issue8.md — 现存缺陷台账（2026-10-08 复核整理版）

> **I8-03 后续实施（2026-10-08）**：已按用户裁决 A 补齐 Make 的共享真实前端前置，Q-05 已裁决；范围、正式回归与本地/外部证据边界见文末 I8-03 补记。以下原复核记录保留为历史。

> **2026-10-08 后续实施：I8-01 已按用户裁决 A 本地修复，Q-01 已裁决；本轮实现与验证见文末补记。I8-02 另按用户裁决 C 本地实施，Q-02 已裁决。I8-18 已按用户裁决 A 本地实施，Q-03 已裁决；正式证据见文末补记。I8-111 已在用户明确无兼容性需求后按严格方案 B 本地实施，Q-08 已裁决；正式证据见文末补记。以下只读复核记录保留为历史，I8-113 已按用户定型的空渠道＋单选按钮本地实施，Q-09 已裁决；正式证据见文末补记。I8-12 已按推荐方案 A 本地实施，新增 Q-14 已裁决；本轮证据见文末补记。I8-01/I8-02/I8-18/I8-111/I8-113/I8-12 当前状态以各自补记为准。**
>
> **本文档保留问题追踪编号，明确区分真实待修问题、合理前置条件、维护候选、已撤销判断、证据缺口与外部验收边界。本次只修订文档，生产与测试均未修改。**
> 正文保留本次核验过的原单元并显式标候选/撤销，以便后续跟进；既有历史误判与已完成专项索引见 [附录 A](#附录-a已撤销的误判与不再登记的条目)，对审计报告与 TODOLIST 的待回写事实订正见 [附录 B](#附录-b审计报告与-todolist-待回写的订正)。
>
> - 强要求仍以 [AGENTS.md](./AGENTS.md) 为唯一来源；P1-01 合同见 [Issue7.md](./Issue7.md)；人工验收清单见 [ProdTestList.md](./ProdTestList.md)；业务待办见 [TODOLIST.md](./TODOLIST.md)。
> - **编号沿用原 `I8-xx`** 以便与提交历史互认；**编号不连续是刻意的**——空缺编号即已撤销或已闭环项，一律见附录 A。
> - 证据等级：**① 源码级**（可直接复核到行）｜**② 历史运行期复现**（仓库外副本探针/overlay；本次重验另标）｜**③ 静态不可达**（当前调用链不可达，仅有潜在风险）。
> - 本文档中的"仍存在"以**当前磁盘**为准；原段落中未标“本次”的次数、耗时、百分比、红绿结果均为**历史台账记录**，本次并未全部重演。§1.6 给出本次证据与后续验证，不将源码推理或历史记录冒充本次动态实测。

---

## 0. 基线与证据等级

| 项 | 值 |
|---|---|
| 分支 / HEAD | `main` / `729b7c349594c17529bd9f09aa31780c941ddf77`（本次文档修订开始前；未提交修订不改变 HEAD） |
| 本地 `origin/main` | `938bb8bac0a41ea285597ba454b34e727003d7ba`；ahead/behind `4/0`（**未 fetch**，不代表真实远端） |
| 本轮复核基线 | `938bb8b` + 5 个未提交文件（P3-25 批次，生产 Go 代码零改动）；后续四个提交均为文档/测试，**被复核的生产文件逐字节未变** |
| 证据采集方式 | 历史台账来自仓库外副本测试/overlay/探针；本次由两个独立核验子代理与分诊代理复核，具体证据层见 §1.6；本次文档编辑没有重跑历史全部探针或全量门禁。**未调用真实云/DNS 上游/SMTP/Webhook/Uptime Kuma** |
| 已确认修复、不再登记 | P0-01；P1-01/P1-02；P2-01～P2-09；P3-01～P3-09、P3-11、P3-13～P3-17、P3-19～P3-20、P3-22～P3-26；F1、F5；R7-01/02/03/05/06/07；I-01～I-19（其中 I-07 的 P3-10 出口④ 有历史仓外故障注入记录、缺仓内长期回归，见 §3 I8-22，I-08 的 P3-21 Webhook 子项按合同转为历史事实）。P3-18 的文档漂移残留已在订正中处理；P3-10 出口④、P3-21 Webhook 之外的全部子项均已本地收口 |

---

## 1. 问题与候选项（逐项区分缺陷、维护风险和合理前置条件）

### 1.1 数据与逻辑

#### I8-01【已本地修复·原中低逻辑错误】`coverage_ready` 在 unsupported-only 计划下误报　`①`　（= TODO-001）

- **现象**：某目标**全部**期望功能都被标记为平台不支持时，`coverage_ready` 仍为 `true`，被下游读作"已覆盖"。
- **证据**：`provider/plan.go:627` 定义 `implementable, covered := 0, 0`；`:631` `implementable++`；`:638/:647` `covered++`；**两个变量此后均未被读取**。`:655` 实际判定为 `len(plan.Desired) > 0 && !in.AddStateUnknown && len(plan.ToAdd) == 0` ⇒ 全部不可实施时 `ToAdd` 为空，可达 `true`。
- **严重度订正（重要）**：**不产生任何删除后果**——清理安全门在 `provider/plan.go:698-701` 对本轮**每一个** `plan.Unsupported` 代码追加"存在平台无法实施的期望规则，保留全部清理候选"，删除集合独立被冻结。消费者也只有两处：`provider/plan.go:709`（即上述已被冻结的门）与 `syncer/syncer.go:738`（Dry Run DTO 字段）。
- **用户可见性**：前端仅在 `webui/frontend/src/types.ts:148` 声明 `coverage_ready: boolean`，**无任何组件读取该字段**。⇒ 实际后果是 **API 输出字段语义错误**，不是"可能误删"。
- **历史只读结论（已由文末实施补记替代）**：逻辑错误成立；当时将 TODOLIST 的 TODO-001 从 P0/高订正为中低，生产缺陷尚未修复。现有 `provider/plan.go:137-138` 明确无可实施期望时为 false；最小候选须包含 `implementable > 0`，仅 `covered == implementable` 会让空集仍为 true（见 Q-01）。

#### I8-02【已本地修复·原中可观测性】失败 attempt 抹平已确认的清理/不支持明细　`②`　（= TODO-008；呼应 Issue7 §12.7 与审计 `:990`）

- **历史现象（已由文末 I8-02 实施补记替代）**：目标首次 attempt 已产生可信残留（部分删除/幂等 NotFound）后，后续 attempt 在 S0 失败，或 Add/S1 早退、未建立新的可信清理观察时，`cleanup_candidates`/`cleanup_deferred` 被零值覆盖；`unsupported` 也被本 attempt 结果替换，S0 早退时为空，Add 失败时则可能保留本 attempt 已知平台限制；`cleanup_deleted` 仍保留累加值。
- **证据**：`syncer/target.go:102-107` 中 `added`/`deleted`/`cleanupDeleted` 为 `+=`，而 `:104` `unsupported`、`:105` `cleanupCandidates`、`:107` `cleanupDeferred` 为**覆盖赋值**；`runTargetAttempt` 每次进入以零值初始化（`:143`），早退路径 `:156`（S0 失败）、`:179`（Add 失败）、`:188`（S1 失败）直接返回该零值结果。
- **历史复现**：`outcome=failed added=1 deleted=1 cleanup_candidates=0 cleanup_deleted=1 cleanup_deferred=0`，而云端**仍存 1 条陈旧规则**；对照"仅 1 个 attempt"场景正确保留 `2/1/1`。
- **下游放大**：`webui/api/logwriter.go:99-104` 的详情行以 `candidates > 0` 为门槛 ⇒ `candidates=0` 时整行"清理候选…"被完全抑制，只剩 `deleted=1`，出现 `deleted(1) > candidates(0)` 的自相矛盾输出。
- **历史合同缺口（现已由 Q-02 裁决与 Issue7 §12.9 补齐）**：`syncer/target.go:168-169` 描述同一个 attempt 的 Add 失败仍保留已规划 unsupported；`target_retry_test.go:120` 描述后续 attempt 重新规划后采用最终明细，两者可以兼容，不能据此声称合同冲突。尚缺的是后续 attempt 在取得可信观察之前失败时，如何保留最后可信观察、标明其时间与未知状态。旧观察不得无标记宣称为最新云状态。
- **历史结论**：行为与输出矛盾**确定成立**；**非 R7-04 引入**（`git show b19d271 -- syncer/target.go` 显示这两行属未改动的上下文），修复前即存在。争点只是**表达方式**（保留并标明前次可信残留 vs 显式表达"未知"），不是"是否存在"。

#### I8-18【已本地修复·原中读一致性】`GET /api/alerts` 由 4 次非事务读拼装，可返回自相矛盾快照　`②`

- **当前状态**：2026-10-08 已按用户裁决 A 使用四对象窄只读事务正式修复；范围与正式证据见文末补记。以下保留修复前复核记录。
- **历史现象**：`webui/api/alerts.go:55/60/65/70` 顺序执行 4 次**独立**读（`config/store.go:894/785/837/958`），全部直接落在 `*sql.DB` 上，**无 `BeginTx`/`BeginReadOnlyTx`** ⇒ WAL 下每条语句自成隐式读事务，不共享快照。
- **历史确定性复现**：在第 3 读与第 4 读之间提交一次完整写事务 → 响应为 `policy=old, email=old, webhook=old, push=new`，一次 200 响应内部自相矛盾；`-count=5` 稳定。并发夹具（4 writer × 25 PUT + 3 reader，带 `-race`）下未修复实现 **544/546/495 次 GET 中 116/131/105 次四部分不一致（约 21%～24%）**；"同一只读事务取快照"变体连续 5 轮 **0 次**。
- **写侧对照**：`PUT /api/alerts` **是单事务**（`alerts.go:276-278` → 协调器 `BeginTx` → `ReplaceBusinessAlertsTx` → 同事务读快照 → commit → commit 后发布）。注入 `BEFORE INSERT RAISE(ABORT)` → PUT 返回 500 且**四部分全部回滚**。
- **历史前端影响**：`Alerts.vue:117-131` 用 GET 响应**整体覆盖 4 个 ref**，`:143-152` **无条件回传全部 4 个对象**；校验器 `isAlertsData:55-70` 只校验对象内部字段名/类型，**不校验跨对象版本一致性** ⇒ 撕裂响应必然通过；用户改一个开关保存即把混合快照整体写回。`loadState`/`saving` 守卫都拦不住。
- **历史测试缺口**：命中 `GET /api/alerts` 的 6 处测试全为单线程同步取值断言；唯一涉及原子性的是**写侧**回滚用例。判别力实测：未修复实现 + 确定性注入下，整包 `webui/api` 既有测试**全绿**。
- **历史候选研究（非正式门禁）**：仓库**已有**一致快照读路径——`config/store.go:1125 LoadBusinessSnapshotTx`（含 policy/email/webhook/push）+ `config/store.go:41 BeginReadOnlyTx`，且 `webui/api/export.go` 的 `handleConfigExport` **正在使用**（`BeginReadOnlyTx` → `LoadBusinessSnapshotTx`）。⇒ 历史副本采用完整快照变体取得以下结果，**不是对正式实现零失败面扩大的保证**：完整快照还读取校验无关 targets/rules/settings，正式候选优先窄只读事务读四个对象，见 Q-03。历史实测 200/字段集合/`no-store`/`Content-Type`/默认值补齐/非 null/500 出口**全部不变**，既有 12 项告警测试与整包全绿。
- **历史最坏后果**：静默回退某个**未被触碰**的渠道配置（如刚更新的 Webhook URL），界面与日志无任何提示。
- **历史触发条件**：无并发写入时**不可发生**；`sync_enabled=false` 是无关变量（四种写者与同步引擎独立，同步引擎不写这四张表）。
- **Q-03 已裁决 A 并正式实施**；窄事务、确定性屏障与真实 HTTP 并发回归已入仓，旧表单覆盖属于独立边界。

#### I8-111【已本地修复·原中低输入契约】version 3 解码的重复键、大小写与转义边界　`②`

- **历史现象（修复前）**：①**重复标量覆盖，重复对象还可能合并**：`{"version":2,…,"version":3}`、`{"version":1,…,"version":3}`、`{"version":null,…,"version":3}` **全部 200 并整库替换**；`{"targets":null,…,"targets":[]}`、`{"monitoring":null,…,"monitoring":{…}}` 同样被接受（共 5 个 200 载荷）。②**键名大小写不敏感 + 转义**：`{"VERSION":3}`、`{"Version":3}`、`{"\u0076ersion":3}` 均 **200**。
- **历史对照（正常面成立）**：取值层面 **19/19 非法形态全部 400 且零写入零发布**；57 条字段路径逐字段与 §9.1 一致；单事务四阶段注入 `RAISE(ABORT)` 全部完整回滚。
- **历史性质订正**：最终解码版本仍为 3，不能称为接受 version 1/2 的业务配置包。重复键 last-wins、大小写兼容与 JSON 字符串转义是三个不同维度；`\u0076ersion` 解码后就是 `version`，并非未知字段。现有合同未要求原始键字面编码必须唯一，故这是兼容策略候选，不能直接定为输入漏洞。本次仅同构标准解码器验证，未重跑导入端点或整库替换。
- **Q-08 已裁决严格方案 B 并正式实施**：用户明确当前无兼容性需求、允许破坏性改动。使用 Go 1.27.1 标准库 JSON v2，拒绝重复字段、错误大小写与非法 Unicode；合法转义保持 JSON 等价语义。无需自建 token 扫描器；嵌套数组继承同一解码选项，10 MiB 有界读取与安全错误转换配套落地。正式测试与边界见文末补记。

#### I8-113【已本地修复·原中低真值源】Webhook 空初始值与单选渠道（方向 A 细化）　`②`

- **历史现象（修复前）**：新建库走 DDL 的**字面量** `'dingtalk'`（`config/store.go:235`），升级库走 ALTER 的**常量拼接** `DefaultWebhookChannel`（`config/store.go:313`）。当前 `config/config.go:24` 的值恰为 `"dingtalk"`，故**当前无实际分歧**；但把该常量单点改为 `"slack"` 后，**新建库得 `dingtalk`、升级库得 `slack`**，而当时**仓库测试全绿零告警**（历史探针）。后续 I8-18 缺行默认值回归已能提示常量变值，但仍不能完整守护新建／迁移／导出路径一致性。
- **附带订正**：`webui/api/bundle_v3.go:353/363` 是转换兜底，不能泛称绝对不可达（空库 loader 可能返回零值）。DDL/ALTER 来源分裂当前没有实际默认值差异，应登记维护风险；历史改常量探针不是当前生产分歧。
- **Q-09 已裁决并正式实施**：方向 A 进一步定型为空渠道初始值＋单选按钮；新建、无主行、reset 均未选择，关闭可空、启用必选，GET/导出不补钉钉，发送前拒绝非法渠道。已有配置保留；缺列历史主行仅转换一次，固定历史钉钉值不属于新默认值。version 3 保留，旧版导入新增空渠道包不作兼容要求。见文末正式补记。

#### I8-12【已本地修复·原低前端状态机】Dry Run 失败后渲染绿色「无待变更规则」成功空态　`②`　（原 I8-12 + I8-13）

- **历史现象（修复前）**：`webui/frontend/src/composables/useDryRun.ts:17-18` 每次 `run()` 先清空 `results`/`warnings`，`:23` **仅成功分支**设置 `lastRunAt`；`DryRunResults.vue:129-131` 在 `results.length===0 && warnings.length===0` 时无条件渲染 `type="success"` 的「无待变更规则」。
- **边界订正**：首次执行就失败时 hasRun 仍为 false，显示“尚未执行”；绿色成功空态需要已有成功历史，不能泛称每次失败都如此。
- **触发**：先成功执行一次 Dry Run，再使其失败（409 并发冲突 / 500 / 网络错误）⇒ 页面显示"无待变更规则"，而实际是执行失败（仅 toast 报错即消失）。
- **零目标订正（2026-10-08）**：当前后端零目标返回空结果及“暂无云资源目标”warning，通常不会进入旧绿色分支；运行时未就绪同样返回空结果加 warning。真正边界为“空结果不能推导无变更，也不能一律断言没有配置目标”。R7-03 所有已配置目标各一项及无适用规则骨架合同保持。
- **当前修复**：用户已授权请求阶段方案 A；`idle/running/completed/failed` 控制互斥展示，重试清空旧预览、失败原因持久展示，空结果中性提示并保留 warnings；时间戳只记录最近请求结束，通知不宣称全部目标正常。仓内 17 项行为回归与正式证据见文末；真实浏览器待 PT-AUDIT-03。历史探针不混作本轮证据。

### 1.2 构建与门禁

#### I8-03【已本地修复·原中可复现性】清洁检出下 Makefile 的 test/vet/all 缺前端前置　`②`

- **当前状态**：2026-10-08 按用户裁决 A 为 `test`、`vet` 补齐共享 `.PHONY frontend` 前置，保留真实安装/构建；17 项依赖与失败传播回归已入仓，正式证据见文末。以下现象与命令结果为修复前历史。

- **正常前置与问题范围**：裸 Go 构建前需真实 dist；`README.md:407/410/413` 指示先 make build 再 test/vet，CI workflow :51 先前端、:65/68 后 Go，因此 CI 顺序当前正确。欠完善的是清洁副本单独 make test/vet 和 make all 的入口，不能把正常 embed 前置称全部构建不可用。
- **现象**：`webui/embed.go:5` 的 `//go:embed frontend/dist` 依赖被 `.gitignore:5` 忽略的目录，而 `Makefile:9-13` 的 `test`/`vet` **无 `frontend` 前置**（`build` 有）。
- **历史实测（清洁检出 = `git archive HEAD`）**：`go build ./...` / `go vet ./...` / `go test ./...` = **EXIT 1/1/1**（`pattern frontend/dist: no matching files found`），根包与 `webui` 包 `[setup failed]`；`make vet` EXIT 2；`make all` 在第一个前置 `vet` 即中止（**EXIT=2，`frontend` 目标从未被触达**）。包级结果：**2 包 setup failed + 9 包真跑全 ok + `dns` 包未跑**（授权禁止真实外网解析，仅验证到编译层）⇒ 既**不得主张清洁检出可通过**，也**不得主张清洁检出 12 包全绿**。
- **对照**：当前工作树存在 ignored `webui/frontend/dist` → `go build ./...` EXIT 0。
- **⚠️ 根因不是"目录存在"**：`mkdir -p webui/frontend/dist` 后 `go build ./...` **仍 EXIT 1**（`cannot embed directory frontend/dist: contains no embeddable files`）；必须目录内有 **≥1 个可 embed 文件**。审计 §5 附表 H5 行原写的"建空目录后 EXIT=0"**已证伪，不得据此修**（见附录 B）。
- **附带**：清洁检出中 `go list ./...` 本身失败（非 0 且无输出）⇒ 任何 `go test $(go list ./...)` 脚本会**静默退化成"只跑当前目录"**；须用 `go list -e ./...`。
- **禁止项**：**不得**提交占位 `webui/frontend/dist/index.html`——`webui/server.go:307` 会把占位页当真实 SPA 提供，占位页可以通过编译，但当前 `TestStaticAssetsServedFromEmbed` 会因缺少真实 JS 资源而失败；原“全部测试变绿”判断已订正。不得用占位页绕过真实前端构建（仅 `git add -f` 可绕过忽略规则）。
- **修法见 §2 Q-05**。

#### I8-04【中·门禁密闭性】默认测试命令会执行真实外网 DNS 查询　`②`

- **准确表述（订正后）**：`dns/resolver_test.go:10/29/40` 使用 `8.8.8.8:53` 且都真调 `Resolve`；其中 `TestResolve_NonExistent`(`:25-27`) 与 `TestResolve_PublicDomain`(`:37-39`) **有 `testing.Short()` 守卫**，唯一无守卫的 `TestResolve_Localhost`(`:9-22`) 经回环探针证明**根本不外发查询**（上游换死地址仍 PASS，走 hosts）。**真正的缺陷是默认门禁不传 `-short`**——`Makefile:10` 与 CI `docker-publish.yml:68` 都不过滤，**守卫因此失效**。（审计原表述"无 `testing.Short()` 守卫"不准确，见附录 B。）
- **历史实测**：上游改指 `127.0.0.1:15353` + UDP 监听——`-short` → **0 个报文**；默认语义 → **6 个 UDP 报文**（`host.invalid`×2、`dns.google`×4），dns 包耗时 **15.1s**。全程未触碰 `8.8.8.8`。
- **影响**：① 无外网/出口受限环境可能等待、SKIP 或 PASS，不能写成必然变红：NonExistent 无失败断言，PublicDomain 网络错误走 Skip、地址不符仅 Log；② 历史"全量 12 包 race 绿"**包含真实外网 I/O**，不能表述为纯本地密闭门禁；③ 清洁检出下 `dns` 包运行结果**未验证**。
- **修法见 §2 Q-06**。

#### I8-102【低·门禁健壮性】`test:alerts` 未设逐用例超时，回归会"永久挂起"而非"变红"　`②`

- **现象**：`webui/frontend/package.json:7` 为 `node --test tests/alerts-load.test.mjs`，**无 `--test-timeout`**；`node:test` 默认逐用例超时无限且顶层用例串行。
- **历史实测**：旧实现（`064b794^`）不带上限时被 180s 硬墙杀掉、EXIT 142、**输出 0 字节**——用例 1 永久挂在 `tests/alerts-load.test.mjs:72` 的 `await state.save()`，用例 2～5 根本不开始。带上限（`--test-timeout=15000`）后同一实现 **EXIT 1、0 pass / 4 fail / 1 cancelled**。
- **影响**：历史候选中的“loading 态发 PUT”回归使该用例等待未解 Promise；无用例超时时可能挂起，不能概括所有同类回归必挂死。
- **建议**：改为 `node --test --test-timeout=30000 …`（未实施）。

### 1.3 运行时健壮性

#### I8-09【低·崩溃面】Lighthouse / CVM 资源扫描对空响应直接解引用　`②`

- **现象**：`provider/scan.go:72`（`scanTCLighthouse`）与 `:113`（`scanTCCVM`）在未判 `resp`/`resp.Response` 的情况下直接取 `resp.Response.InstanceSet` / `.SecurityGroupSet`。
- **历史仓外实测**：响应体 `{}` → `invalid memory address or nil pointer dereference`（panic）；全仓生产代码 `recover()` 命中 0 处 ⇒ 由 net/http 每连接兜底，**进程不崩**但该请求异常中断。
- **对照**：同步 Provider 同类路径**已守卫**（`provider/tc_lighthouse.go:88`、`provider/tc_cvm.go:82`）。属**同类保护不对称**。
- **处置见 §2 Q-04**。

#### I8-08【低·数据正确性】SWAS 资源扫描对结构缺失静默截断并覆盖缓存　`②`

- **现象**：`provider/scan.go:153-156`（`scanAliSWAS`）对 `body`/`Instances` 缺失直接 `break`，返回**累计结果 + `nil` error** → `webui/api/scan.go:66` 覆盖写缓存。
- **历史实测（真实 SDK → handler → 临时 SQLite）**：首页 `{}` → `success=true count=0`，**旧缓存被清空**；首页 100 条满页 + 次页 `{}` → `success=true count=100`，**旧缓存被截断列表替换**。
- **范围**：P3-26 只覆盖 ECS 扫描路径；审计与 `AGENTS.md` 均未声明 SWAS 扫描已修。与 `provider/scan.go:217-239`（ECS 路径返回 `ErrSnapshotIncomplete`）**策略相反**。
- **处置见 §2 Q-04**。

#### I8-10【低·无界循环】Lighthouse `GetSnapshot` 无页数上限 / 进度守卫　`①`

- **现象**：`provider/tc_lighthouse.go:67-124` 仅有 Offset 递增与"本页不足一页即结束"两个判据，**无页数上限或整个分页操作的时间预算**（Offset 会递增，不能直接套用 token 校验）；服务端持续返回满页时循环无界（仅受网络与 SDK 超时约束）。
- **范围**：P3-25 与"分页总预算"候选均只覆盖 ECS 两条路径；Lighthouse 未被任何 finding 覆盖，`AGENTS.md` 亦未要求。属**同类保护不对称**（ECS 同步与扫描有历史 token 集合，但均无页数上限；100 页上限属于 SWAS `GetSnapshot`）。

#### I8-11【低·完整性判定】SWAS 分页：跨页更小/为 0 的 `TotalCount` 会提前 `proven`　`②`

- **现象**：`provider/ali_swas.go:95-97` 每页覆盖 `totalCount`，`:113` 用它判 `proven`；若后续页返回更小或 0 的 `TotalCount`，会以**截断快照成功返回**。
- **历史实测**：TotalCount 3→1 时返回 2 条；TotalCount 0 且有 1 条时返回 1 条；两者 `err=nil`。
- **依赖**：云端自相矛盾输入，真实云未复现；与 F5"不能证明完整却成功返回"同类。属"证明完整性"的判据可被单页小值降级。

#### I8-17b【低·功能不可用】`NewResolver("::1")` 等 IPv6 字面量上游必然失败　`②`

- **现象**：`dns/resolver.go:33-35` 对不含端口的地址直接补 `":53"`；对 IPv6 字面量 `::1` 得到 `"::1:53"` → `net.SplitHostPort` 报 **`too many colons in address`** ⇒ **IPv6 字面量 DNS 上游不可用**（`[::1]:53` 形式可用）。
- **证据**：副本探针实测 `::1:53 → too many colons`；删除自动补端口逻辑后 `TestNewResolver_PortAppend`（`dns/resolver_test.go:60-71`）**仍 PASS**（它只断言返回非 nil），而探针 **FAIL** ⇒ 注释声称的补端口行为**零守护**。
- **影响**：`AGENTS.md` §四允许自定义 DNS 服务器；用户填 IPv6 字面量时配置可保存，但解析必然失败，且报的是地址解析错误而非明确提示。
- **建议**：改用 `net.JoinHostPort`（自动处理 IPv6 方括号），并补一条"IPv6 字面量 + 端口补全"的判别性用例。

#### I8-05【中低·健康可见性】Push 心跳等待被任意配置保存整段重置　`②`

- **现象**：`internal/health/push.go:152-165` 的 wake 分支只做 `timer.Stop()` 后回循环顶部，URL 未变时重新 `NewTimer(interval)` **整段重新计时**；而任意页面保存都会经 `webui/api/deps.go:168` 触发 `Push.Wake()`。
- **历史实测**：interval=400ms 时，对照组 1.2s 内 4 次请求；实验组每 100ms Wake 一次 → 1.2s 内**仅 1 次**。
- **影响**：连续保存可无限推迟心跳 → Uptime Kuma 侧可能误判 DOWN。Syncer 的同类问题已由 P3-23/P3-24 按"同间隔保留剩余等待"修复，**Push 未同步该合同**，且本次未发现直接守护此语义的专项回归。
- **裁决见 §2 Q-07**。

#### I8-06【中低·配置边界】正常发送路径不应用 20s 间隔下限　`②`

- **现象**：`internal/health/push.go:151-155` 只要 `interval > 0` 就原样使用；`MinPushInterval` 仅在非法 URL 重校验分支（`:169-178`）与 API/导入校验边界生效，`config/store.go:966-993` 只要求时长为正。
- **历史实测**：合法 URL + `interval=1ns`（仅能由直改 SQLite 绕过 API/导入产生，已有 `TestPushDirtySQLiteConfigReachesRuntime` 证明此类配置可达运行快照）→ **300ms 内向回环 mock 发出 1211 次请求**。
- **性质**：**既有行为**（非 P3-03/P3-21 回归）；与 P3-03 为非法 URL 分支补下限的做法**不对称**。可与 Q-07 合并处置。

#### I8-17f【低·效率】`runRound` 在同云分组**最后一个目标之后**仍等待一个完整厂商间隔　`②`

- **现象**：`syncer/syncer.go:951` 的 `s.sleep(rateLimitInterval(ct))` 位于逐目标循环末尾，对分组内每个目标（含最后一个）都执行 ⇒ 单目标分组每轮多等 1 个间隔（Lighthouse/SWAS **5s**、CVM/ECS **200ms**）。
- **影响**：该等待**计入 `duration_ms`** 并**推迟 ticker Reset**。方向与 P3-22 对 Dry Run"取消末尾等待"的处理**相反**，正式同步侧仍有残留。
- **候选方向**：减少末尾空等，但必须同时保护相邻轮次、手动 trigger 与相同 CloudType 的配额间隔；单纯移除最后一次等待可能缩短跨轮间隔，不能保证无契约变更。先以虚拟时间验证单目标、多目标和紧邻两轮的实际请求间隔。

#### I8-43【低·可复现性】`bundle_v3.go` 的 presence map 使同一非法请求的错误文案漂移　`②`

- **现象**：`webui/api/bundle_v3.go` 的字段缺失检查用 `map[string]bool` 迭代，**同一非法请求、同一进程内 300 次即观察到 3 种（policy，3 字段缺失）与 8 种（email，8 字段缺失）不同文案**。
- **影响**：错误文案不可复现，影响排障、日志比对与用户沟通。
- **建议（低影响候选，仍需验证）**：改为与**同文件既有正确写法** `normalizeBundleSettings`（`bundle_v3.go:678-…` 的顺序 `if` 链）一致；实测 `grep '字段缺失' --include=*_test.go` 的 4 处命中全为子测试名/注释，断言只校验状态码 ⇒ 既有测试未直接固定错误选择顺序；应补多缺失字段时的确定性断言，不能保证所有消费者零影响。

### 1.4 残留、一致性与可观测性

#### I8-14【低·潜在回归】旧描述身份实现仍随包发布，且仍复现原缺陷语义　`②`

- **残留物**：`provider/common.go` 保留第二套 key 与旧描述身份路径——`OwnedRules:68`、`Diff:139`（`:152` 以 `Description` 相等判归属）、`buildDesired:207`（`:224-228` 静默 `continue` 跳过 IPv6/ICMPv6）、`normalizePortForCompare/keyOf/keyOfAction:96-131`。
- **历史实测**：该路径仍会 ① 空 comment 碰撞时把同目标另一条规则列入 `ToDelete`（`ToAdd=1 ToDelete=1`）② Lighthouse `ICMPv6` 与期望 `ICMP` 不收敛（`1/1`）。
- **当前风险**：**无生产消费者**（`Diff`/`buildDesired`/`OwnedRules` 的调用者全部在 `provider/common_test.go`），但**无门禁阻止重新接线**；且与 `provider/plan.go:20` 自述"全仓只有这一个 canonical key 层"矛盾。
- **建议**：删除或加"仅供历史回归"的显式标注 + 门禁（属低风险清理，非必须）。
- **附带缺口**：`PlanTarget` 层的 TCP+UDP 拆分（`provider/plan.go:471-474`）未发现直接专项回归；单凭字面量搜索不能排除间接覆盖，应补 PlanTarget 拆分场景的行为断言。

#### I8-15【低·维护性】注释/死赋值与实现不符　`①`

| 项 | 位置 | 问题 |
|---|---|---|
| a | `webui/api/export.go:88-95` | 注释仍是 P2-04 修复**前**的顺序（"→ 发布 RuntimeState → 返回成功"写在唤醒之后），与现行"先发布后唤醒"相反 |
| b | `provider/plan.go:742` | 注释称 CVM"候选间索引重复 → deferred"，实际在 `provider/tc_cvm.go:213-215` 返回**硬错误**（"拒绝删除"） |
| c | `internal/health/push.go:210-216` | 首次 `buildPushURL` 的返回值被 `_ = target` 丢弃（仅作占位校验，死赋值） |
| d | `provider/plan.go:153-154,655` | `TargetPlanInput.AddStateUnknown` 全仓**从未被置 true**，合同子句无执行点 |

#### I8-16【低·测试与规范候选】测试夹具、文案与清理错误处理（Extension 误判撤销）　`①`

| 项 | 位置 | 问题 |
|---|---|---|
| a | `notifier/inflight_test.go:89-98` | 仍丢弃 accepted `net.Conn`（与 R7-06/R7-07 同模式）；因断言放宽（`:120-132` 任意错误均可）不能据此保证不 flaky；现有宽断言无法区分"应用层 deadline 生效 vs 连接被重置"的判别力 |
| b | `internal/health/push.go:48` + `push_test.go:459` | **订正**：`pub.count()==0` **不是永真断言**（注入一次 `Publish` 后 5/5 FAIL）⇒ 断言可判别。真问题是**死字段**：生产 `p.bus` 恒为 nil、`push.go` 从不读它、`run.go:155-164` 也不传 `Bus`。建议补正向控制而非删断言 |
| c | `syncer/stop_gate_test.go:215-216` | 断言 `p.calls.Load() != 2`，失败文案却写"want 1"（文案陈旧） |
| d | `notifier/email.go:224-227` / `:229` | Close 错误被丢弃属规范性补齐候选；`email.go:215/221` 的 `conn.Close()` 同类。**撤销 Extension 错误子项**：`Extension` 返回 `(bool, string)`，忽略的是扩展参数字符串，不是 error |
| e | `webui/api/alertset_test.go:216 TestApplyCandidateWithoutRuntimeIsNoop` | **名实不符**：`d := &Deps{}; d.applyCandidate(Candidate{})`，而 `Candidate{}` 的 `State` 为 nil ⇒ 在 `webui/api/deps.go:129-131` 的**首个守卫**即返回，**从未走到用例名声称的"无 Syncer/Runtime"分支**。变异"只在 else 分支 panic" → 原用例 PASS、真实探针 FAIL |

### 1.5 前置条件与静态候选（当前无生产后果，含撤销项）

| 编号 | 位置 | 问题 | 不可达原因 |
|---|---|---|---|
| I8-17d | `syncer/syncer.go:378-390 setSyncEnabled` | 读-改-写**未持 `s.mu`**（对比 `ApplyState` 持锁）⇒ 窗口内 `Resume` 被覆盖 **8/8**、窗口内协调器发布的**已提交 TAG 配置被陈旧快照回滚 8/8** | `Pause()/Resume()`（`:365/:370`）**无生产调用方**（`webui/api` 接口成员已删，仅注释引用）。若未来接线并与配置发布并发，可能丢失更新；属于语义原子性问题，不等同 Go 数据 race。处置见 §2 Q-10 |
| I8-17e | `syncer/dns_round.go:38-40` | `h := r.hosts[...]` 后直接 `h.open`，域名不在本轮快照时 nil 解引用 | 调用方只传本轮配置中的适用规则（代码注释即以此为前提）；属**缺失前置断言** |
| I8-47 | `webui/api/export.go:99/117`（import）、`webui/api/coordinator.go:95`（实际 Store 使用）、export handler | `Deps.Store == nil` 时直接 panic | 当前启动接线保证 Store 非 nil。P3-14 的强要求仅列六个 sync/SSE 入口，不能称本项违反其合同；防御性统一低收益 |
| I8-44-1 | `config/runtime.go:137-143 mustDuration`（`:146-152 mustThreshold` 同族） | 解析失败**静默返回 0**；且 `mustDuration("-5m")` 返回 **`-5m`**（`time.ParseDuration` 接受负号，函数无正数校验）⇒ 若非法间隔绕过所有前置校验，`time.NewTicker` 会 panic；当前校验阻断该路径 | 两条生产路径（启动加载、协调器候选构造）都经 `normalizeSettings`（`config/store.go:1155`） |
| I8-44-2 | `internal/health/health.go:195-197` | `TriggerEnabled` 有 `Policy == nil → false` 检查，而 `Evaluate` 无 ⇒ 语义不一致 | 生产 `run.go:138-143` 恒传 Policy |
| I8-44-3 | `webui/server.go:305-308` | **撤销当前产品缺陷判定**：固定合法路径与 embed.FS 下 `fs.Sub` 返回包装 FS、nil；吞错仅理论规范候选 | `fs.Sub` 不检查目录内容完整性。缺可 embed 目录在编译期失败；dist 存在但缺 index 时处理器仍注册并返回 404，不能归因于吞掉 fs.Sub error。保留该单元便于追溯，暂不要求改代码 |

---

### 1.6 本次状态与后续跟进（34 个正文核验单元）

本次采用三个 GPT-6.1 Sol／low 子代理分诊和独立核验；主代理只调度与审阅汇总。下表“源码”指本次当前调用链复核；“历史”指此前仓外证据，本次未全部重跑。所有“待改”仍未修改生产实现。§3 的十行证据事项另列，不与缺陷数相加；Q 和附录均为交叉引用。

| 单元 | 本次结论／可达性 | 最小跟进方向与所需验证 | 本次证据 |
|---|---|---|---|
| I8-01 | 已按 A 本地修复；原缺陷无删除后果 | 非空可实施集合全部覆盖且 Add 状态确定；混合目标保留 true + partial，独立清理门不放宽 | 新增仓内 planner 与 Dry Run/正式目标回归；旧实现行为红灯与当前结果见文末补记 |
| I8-02 | 已按 C 本地修复；Q-02 已裁决 | 追加 nullable 观察与四类汇总；保留整数类型；最新与最近完整规划分存；实际目标/事件/日志/状态/SSE/组件回归 | 仓内 TestI802_* 与 Dashboard 8 项回归，门禁和负向控制见文末补记；外部未执行 |
| I8-18 | 已按 A 本地修复；Q-03 已裁决 | 四对象窄只读快照；事务返回前结束、默认值/安全错误出口不扩展 | 正式 TestI818Snapshot* 两包 race 20 轮、五类负向控制通过；完整门禁与边界见文末补记 |
| I8-111 | 已按严格 B 本地修复；Q-08 已裁决，旧版本业务漏洞判断仍未成立 | 字段名精确匹配、全层级重复/非法 Unicode 拒绝；合法转义等价；大小检查先于解析，非法包零写入零发布 | 正式 TestI8111* 覆盖 57 条字段路径、运行时与完整配置保留、限额及安全错误；门禁/负向控制见文末补记 |
| I8-113 | 已按细化 A 本地修复；Q-09 已裁决 | 空初始值＋单选按钮；历史主行仅补列时转换，旧库 reset 显式未选择；外部验收保持 | 正式初始化/回滚/往返/零写入零发布/发送前拒绝/真实组件渲染及负向控制，见文末 |
| I8-12 | 已按 A 本地修复；Q-14 已裁决 | 四请求阶段；重试清空、持久错误、中性空结果/完成通知；保留目标级明细 | 正式 17 项页面/模板/请求封装行为回归及六类负向控制；证据见文末，真实浏览器待 PT-AUDIT-03 |
| I8-03 | 已按 A 本地修复；裸 Go 缺产物仍为正常前置 | test/vet/build 共享真实 frontend，单次 Make 只构建一次；17 项依赖/故障回归、清洁副本与正式门禁见文末；不提交占位页 | Makefile、build/makefile_test.sh；CI/Docker 原有正确顺序保持 |
| I8-04 | 待改，网络依赖与假绿风险 | 本地 DNS 替身或明确短模式；统计本地报文，失败必须按所测合同 FAIL，默认测试不意外外发 | resolver_test24–56／Make10／CI68；本次不访问真实上游 |
| I8-102 | 待改，低；未解 Promise 可挂起 | 逐用例超时；已知挂起候选有界失败且后续报告可见，正常实现通过 | package script源码；180s/15s对照是历史 |
| I8-09 | 待改，低；异常云结构导致请求中断 | nil/结构守卫并返回可识别错误；SDK→handler 缺 Response 与有效空集合对照，旧缓存不覆盖 | scan72/113源码；历史 SDK panic 不证明进程崩溃 |
| I8-08 | 待改，低；异常 SWAS 页被当成功 | 缺失结构保守失败、有效空集合保留合法语义；首页/中页异常保留缓存，完整/零资源可覆盖 | scan153–156与API66源码；历史临时SQLite链路 |
| I8-10 | 加固候选，低；服务持续满页 | 定整操作预算／页数或进度策略；本地持续满页必须有界失败且无删除，不用 ECS 有上限作依据 | Lighthouse67–124源码；ECS只有历史token守卫 |
| I8-11 | 待改，低；云页统计自相矛盾 | 不允许后页小 TotalCount 降级完整性证明；测3→1、0+非空、正常稳定分页及失败零删除 | SWAS95–113源码；历史异常响应探针，真实云未复现 |
| I8-17b | 待改，低；配置裸 IPv6 上游 | JoinHostPort 前识别已有端口与IPv6；本地IPv4/IPv6/主机名/显式端口矩阵，不访问外部DNS | resolver33–35及 SplitHostPort 地址行为；本次 `go test ./dns -short -run 'TestNewResolver_PortAppend|TestHasPort|TestResolvedIP_CIDR' -count=1` PASS（0.696s），仅构造/格式，非整包或IPv6实际解析通过 |
| I8-05 | 待改，中低；同配置频繁保存 | 保存保留剩余截止时间；虚拟时间多次Wake及url/启用/间隔变化、Stop取消，真实Kuma另验 | push152–165/deps168源码；400ms吞心跳是历史本地mock |
| I8-06 | 防脏库候选，中低；API/导入正常阻断 | 运行端应用最小间隔；临时SQLite合法URL+1ns验证被夹紧且无请求风暴，不改正常20s/60s | 正数加载与发送源码；历史脏SQLite/local mock，不代表普通API可输入1ns |
| I8-17f | 效率候选，低；每组末尾多等 | 优化前证明跨轮/trigger相邻请求限速；单/多目标虚拟时间，duration与停止保持 | runRound末尾sleep源码；不能承诺简单删sleep零契约影响 |
| I8-43 | 待改，低；非法请求同时缺多字段 | 固定校验顺序；同负载多次同错误、400零写入零发布，状态契约保留 | presence map源码；300次文案分布属历史 |
| I8-14 | 维护候选；生产无消费者 | 逐符号确认后删除/标历史，保留有效旧回归与正式planner专项；TCP+UDP补直接行为测试 | common调用链源码；字面搜索不证明全部间接测试缺失 |
| I8-15a | 注释待改，低 | 后续代码注释同步实际先发布后唤醒，行为不变 | export88–95源码；本次不改Go注释 |
| I8-15b | 注释待改，低 | 同步CVM重复索引为硬失败的真实行为，保留拒删测试 | plan742/tc_cvm213–215源码 |
| I8-15c | 整洁候选 | 保留URL校验作用，仅移除死返回值时验证非法URL周期路径 | push210–216源码 |
| I8-15d | 合理防御字段／低收益 | Add失败早退与S1确认已守安全；未来入口若使用未知态，补明确场景，不按未置true认定安全缺口 | 当前目标链源码 |
| I8-16a | 测试改进，低 | 夹具持有 acceptedConn 并有界回收，断言deadline而非任意错误；压力复验 | inflight89–132源码，宽断言不能证明永不flaky |
| I8-16b | 整洁／测试候选 | Bus未读取属残留，Publish零计数断言可失败；加正向控制或清理字段须复核调用者 | push48/run155–164；旧永真断言已撤销 |
| I8-16c | 文案待改，低 | 后续Go测试文案 want2，与断言一致，行为不变 | stop_gate_test215–216 |
| I8-16d | Close规范候选；Extension误报撤销 | 错误只作安全固定类别、不得推翻已接受邮件；断开/成功Quit对照，禁止记录SMTP原文 | email215/221/224–229；Extension第二返回值为string |
| I8-16e | 测试改进，低 | 以非nil Candidate.State 到达无Runtime分支，保留nil State首守卫单独用例；分支变异应失败 | alertset_test216/deps129–131 |
| I8-17d | 静态更新丢失候选；无生产调用 | 未来保留接口时同锁辅助发布，不持s.mu调用自锁ApplyState；屏障证明不丢配置且无死锁 | setSyncEnabled378–389/ApplyState144–153 |
| I8-17e | 当前前置条件合理 | 若将来允许任意域名，才定义缺键错误；当前调用者传本轮已知适用域名 | dns_round38–40调用链 |
| I8-47 | 防御统一低收益；接线保证Store | 若扩大可空契约，导入/导出统一错误需新测试；不称违反既有六入口503要求 | export导入99/117/coordinator95真实nil点 |
| I8-44-1 | 当前normalize前置合理 | 不绕开已校验快照；仅未来独立调用时需要显式错误/断言 | runtime137–152与store1155 |
| I8-44-2 | 当前非nil Policy前置合理 | 将来允许空Policy才定义Evaluate语义；当前无需为理论输入改产品逻辑 | health195–197/run138–143 |
| I8-44-3 | 产品缺陷判断撤销 | 缺index是内容验证问题，与fs.Sub吞error不同；不因理论error加日志便宣称修复404 | server305–308/embed.FS固定合法路径 |

## 2. 待裁决事项

> 未标“已裁决”的项目为"改动前需要你定方向"的争点；只读研究结论已附在候选方案内。纯代码整洁事项本身**不登记为缺陷**，只保留一项"是否实施清理"的开关式裁决（Q-13）。

| 编号 | 争点 | 候选方案 | 影响面 |
|---|---|---|---|
| **Q-01（已裁决）** | **I8-01** `coverage_ready` 的修法 | 用户 2026-10-08 确认 A：`implementable > 0 && covered == implementable && !in.AddStateUnknown`。保留可实施子集覆盖语义；不新增多状态字段。混合目标仍可 true + partial | 唯一生产逻辑改动为 `provider/plan.go`；同步字段合同与回归，详见文末补记 |
| **Q-02（已裁决）** | **I8-02** failed 路径的观察表达 | 用户选择 C + 追加字段保留旧整数类型，并分别保留 latest/last_complete；历史来源、估计、未知明确区分，可信零可替换；已授权正式修复 | 目标事件、RoundSummary、SQLite 详情、Dashboard 与字段合同；无 schema/配置包迁移；实施见文末与 Issue7 §12.9 |
| **Q-03（已裁决）** | **I8-18** GET 告警快照一致性 | 用户 2026-10-08 确认 A：只用一个只读事务读取四对象，复用现有私有 loader，保持 HTTP 默认值/字段/安全错误语义；不复用完整业务快照、不引入编辑版本冲突机制。已授权并本地实施 | 2 个生产、2 个新增测试及 4 个文档，共 8 文件；前端不变，正式证据见文末补记 |
| **Q-04** | **I8-08 / I8-09** SWAS 扫描缺失集合 与 Lighthouse/CVM 扫描空响应的处置 | ①统一为保守失败 `ErrSnapshotIncomplete`（与 P3-26 的 ECS 策略一致，不覆盖缓存）②仅补 nil 守卫防 panic ③显式接受现状并文档化 | `provider/scan.go:72/113/153-156`、`webui/api/scan.go:49-69` |
| **Q-05（已裁决）** | **I8-03** 构建入口契约 | 用户 2026-10-08 选择 A 并授权实施：test/vet/build 共享真实 `.PHONY frontend` 前置；完整核验推荐一次 make all，快速裸 Go 检查显式使用已准备好的 dist；不引入增量缓存 | Makefile 两条前置、一份回归脚本与 README/AGENTS/本文/审计共六文件；正式证据见文末 |
| **Q-06** | **I8-04** `dns` 包真实外网依赖 | ①改为本地 DNS mock（最彻底，门禁密闭）②统一为默认加 `-short` 并为两种模式定合同 ③接受现状并文档化 | `dns/resolver_test.go`、`Makefile:10`、CI workflow |
| **Q-07** | **I8-05 + I8-06** Push 心跳的"重置语义"与"下限语义" | ①同 URL、同启用状态、同间隔的普通保存保留剩余等待；URL/启用/间隔实际变化的重新计时语义另定，Wake 须及时重读新配置，独立 Health 监督器的唤醒保持；正常路径补 MinPushInterval 防脏库 ②显式接受现状并在 `AGENTS.md`/Build7 写明代价（连续保存可推迟心跳） | `internal/health/push.go:138-178`、`webui/api/deps.go:168`、`AGENTS.md` §三/§五 |
| **Q-08（已裁决）** | **I8-111** version 3 严格解析策略 | 用户 2026-10-08 明确无兼容性需求、允许破坏性改动后授权严格 B：全层级拒绝重复字段、只接受规定大小写、拒绝非法 Unicode，合法转义按解码后的字段名识别。已本地实施；强要求同步至 AGENTS §9.1 | 三个生产文件与两份测试，同步 AGENTS/本文/Build7/README/审计；正式证据见文末补记 |
| **Q-09（已裁决细化 A）** | **I8-113 / I8-48** Webhook 未配置真值源 | 用户确认空渠道＋单选按钮、version 3 保留、不要求旧版导入空渠道包；初始化/reset 显式未选择，历史缺列主行仅转换一次；关闭可空、启用必选、非法渠道发送前拒绝。I8-48 仅为该行索引，不扩大到其他告警默认值 | `config.DefaultWebhookChannel`、`defaultWebhookRowSQL`、`NormalizeAlertWebhook`、GET/导出/通知器/Alerts.vue；正式证据见文末 |
| **Q-10** | **I8-17d / I8-41** `Syncer.Pause()/Resume()` 的去留与并发语义（"是否保留"与"保留后如何正确"是两个问题） | ①设计同锁原子更新辅助方法；**不得持 s.mu 直接调用 ApplyState**，后者自己加锁会死锁 ②改非导出仅减少误用面，不修包内丢失更新③登记为"已知残留风险（当前无生产调用方）"并在 `AGENTS.md` 注明 | `syncer/syncer.go:365-390`、`webui/api/deps.go` |
| **Q-11** | **I8-31 / TODO-003** CVM `DescribeSecurityGroupPolicies` 调用失败的错误分类 | 现状区分 SDK 调用失败与成功响应结构不完整是合理分类。保留 SDK 错误链及现有 retry；若要统一包装，须先验证错误分类和重试后果，不默认作为缺陷修复 | `provider/tc_cvm.go:248-250` vs `:254/:262` |
| **Q-12** | **I8-38** 分页操作总预算（**未实施的研究候选**；实现前先定操作预算、失败类型与契约边界） | ①页间检查总预算（只能限制继续起下一页，不硬限制已在途页的耗时）②按剩余预算限制每页（**机制上必然**扩大 SDK 全局池键空间，`dara/core.go:71` 池无 Delete/TTL/容量，且 `:349-357` 每请求覆写共享 `httpClient.Timeout` 存在同 tag 并发覆写窗口 ⇒ 不建议直接实施）③隔离操作级 HTTP 客户端（需自证隔离与回收） | `provider/ali_ecs.go`、`provider/scan.go`、SDK `RuntimeOptions`、`getDaraClient` 全局 `sync.Map` |
| **Q-13** | **I8-42** 清理类改动**是否实施**（本身不是缺陷，仅为"可安全清理清单"） | 原引用的 §4 清单已从本文移除，不能仅凭八/五/二/十四等历史计数承诺安全或零成本。具体符号清单缺失，待逐符号列出调用者、兼容边界、测试影响后再决定；本次未捏造或实施清理 | `config/config.go`、`notifier/bus.go`、`provider/provider.go`、前端 `types.ts`/composables |
| **Q-14（已裁决）** | **I8-12 / I8-13** 请求阶段与历史预览保留策略 | 用户 2026-10-08 授权推荐 A：四请求阶段，重试清空旧结果；失败持久提示，空结果中性展示并保留 warnings；请求完成不代表全部目标正常；时间只显示最近请求结束，不保留历史预览 | 三个前端生产文件、专用回归/命令入口与五份文档；无后端/API/schema/依赖改动；正式证据见文末 |

---

## 3. 测试与门禁证据缺口（非产品缺陷，但影响结论可信度）

| 项 | 状态与结论 |
|---|---|
| **I8-22** P3-10 出口④（日志渲染错误）判别证据 | 生产固定 bytes.Buffer，正常路径 renderLine 错误不可触发；仓内缺长期渲染失败回归。审计 P3-10「正式故障注入与负向控制」（修订前 :1085）明确 TestRenderFailureNoPublish 来自**仓库外 writer overlay**，仓内查无同名不能否认历史记录。本次未取回重跑该历史夹具；应区分历史外部判别记录、现有生产规范补齐、仓内长期守护缺口。后续若补测，验证原错误返回且序号/ring/投递不变，不扩大生产故障面。 |
| **I8-23** A20 的修复前红灯负向控制 | 历史复核记录，本次未重演； **未执行**（需 `b38678a` 之前的 `syncer/syncer.go` 构造 overlay）。现有证据为静态调用点核验（4 处 `beginRound` 门控）+ 现行 `race ×5` 6 项全绿 |
| **I8-24** P1-01 的独立 overlay 判别力 | 历史复核记录，本次未重演； **E（自评）**。独立复核只做了 20 个自建探针（19 绿 / 1 红为其自身断言错误），**无独立红→绿 overlay** |
| **I8-25** 全量门禁的完整复现 | 历史复核记录，本次未重演； **部分**。构建专项因安全边界**整包排除 `dns`**，只跑 11 包 race；各组普遍只做 `-count=1` 或定向 `-count=20`；审计声称的"全仓 12 包 race 连续 3 轮"**本轮未复跑**。⇒ 单次绿色不外推长期稳定；`dns` 包不得登记为通过 |
| **I8-17g** `TestStateAppliedHookFiresOncePerApplyState` 偶发红灯根因 | 本次源码确认 Add(1) 先于 enabledAtHook.Store，读者仅观察计数存在窗口；以下次数为历史复核记录、本次未重演： 夹具观察竞争：hook 内 `Add(1)` 与 Store 之间**无同步** + 轮询式观察。确定性复刻探针在 channel 同步重写后 **50/50 绿**；隔离 `-count=200` **0 失败** vs 重载 `-count=20` **恰 1 次红** ⇒ **"单项 20 轮通过"不构成稳定证据**（与 I8-25 同口径） |
| `provider/scan.go` 腾讯两条扫描路径 | 未发现 scanTCLighthouse/scanTCCVM 的直接函数名测试引用；搜索不能排除间接覆盖。后续补 SDK→handler 的结构异常专项以守护 I8-09 |
| goroutine / fd / ticker 泄漏断言 | **撤销全仓 0 处**：`webui/server_test.go:732 TestShutdownNoGoroutineLeak` 及 :759–776 的重复 Shutdown 检查用 runtime.Stack 观察 Server 残留。已有 HTTP Server 专项，不等于 Syncer/Push/Supervisor/FD/ticker 全面覆盖；后续按组件补确定性退出证据，不以单次 goroutine 数量等同全面泄漏证明。 |
| `DefaultStartupGrace` 字面量锚点 | **所列五个常量中唯一未发现直接数值锚点者**（历史 `10s→12s` 后 `./internal/health/` 全绿 42.4s，本次未复跑）——它是 Build7 Step 7 的合同值，属低成本数值锚点缺口，不能据此称风险最高。其余四个常量均有（间接或直接）锚点：`InFlightLimit=4`（`alertset_drop_log_test.go:41` 字面量）、`DefaultHealthTimeout=10m`/`DefaultPushInterval=60s`/`MinPushInterval=20s`（`config/store_v3_test.go:430`） |
| 固定 sleep 观察窗口 | 有限时间内未发生只证明该观察窗口；不能只凭 sleep 判定已经假通过。旧数量/最长 600ms 口径过期（`scheduling_test.go:421` 含 3s）。优先针对负面断言用 channel/虚拟时间作区分性验证，避免机械替换所有 sleep。 |
| 前端类改动的验收标准 | 部分轻量测试会 mock 依赖（确切锚点 `p316-state.test.mjs:36`、`delete-confirm.test.mjs:51`）；不能声称全部四套都 mock useSettings 或任何清理都检不出。后续必须执行 `npm run build`（vue-tsc && vite build）并保留行为回归，浏览器另记。 |

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

1. **I8-08/09、I8-05**：优先跟进扫描异常覆盖缓存/请求中断、普通保存持续延迟心跳。分别先定异常与有效空集合区分、等待截止时间语义；I8-18 已按 A 本地修复，正式证据见文末补记。
2. **I8-11、I8-17b**：完整性与 IPv6 DNS 需要改进；I8-12 已按 A 本地修复，真实浏览器待 PT-AUDIT-03；I8-02 已按 C 本地修复，真实浏览器/云观察仍待 PT-I7-07，不再作为待裁决事项。
3. **I8-04/102 与 §3**：继续完善无外网门禁和失败能有界变红的证据；I8-03 已按 A 本地修复，清洁 Make 入口证据见文末。CI 原有前端先行顺序正确；I8-03 收口不表示 I8-04 外网 DNS 已解决。
4. **I8-01 已本地修复**；I8-43、I8-17f 继续独立处理错误顺序和末尾等待候选，分别守住无效请求零写入、跨轮限速。
5. **I8-06、Q-11/12/13 与静态候选**：脏库防御、维护清理和整体预算按收益与合同决定。I8-111 已按严格 B 本地修复，Q-08 已裁决。I8-113 已按细化 A 本地修复，Q-09 已裁决，真实浏览器仍待执行。当前合理前置条件或理论路径不作为必须修的产品缺陷；文档修订不表示生产修复。

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

## 附录 B：审计报告与 TODOLIST 订正追踪

> 这些是**证据/文档层面的订正**，不另算产品缺陷。本次已回写审计的建空dist、永真Push断言、限流零断言、常量锚点、全仓零泄漏、旧调用链、后续独立审阅状态等确切误句，并订正 TODO-001。其余完整统计和历史外部实验不冒充本次重算，后续引用仍按符号/章节复核。

| # | 位置 | 订正内容 |
|---|---|---|
| B1 | 审计 §5 附表 **H5 行** | "手动 `mkdir -p webui/frontend/dist` 后 `go build ./...` → EXIT=0" **已证伪**：实测 EXIT=1（`contains no embeddable files`）。根因是"目录内 ≥1 个可 embed 文件"。**本次已改写该行**，否则会诱导"建空目录"甚至"提交占位 index.html"的错误修法（见 I8-03） |
| B2 | 审计 §6「7 个永远无法失败的测试」 | 改为 **A/B/C/D/E 共 11 项**（真·不可失败 1 项），并删除 2 项误报（`alertset_test.go:183`、`push_test.go:459`）；`dns/resolver_test.go:36` 的失败路径被 `t.Skip` 吞掉应单独点名；D 类空测试需注明"默认门禁带 `-race`，门禁中有判别力"。同时订正行号与计数：`syncer/state_test.go:411-436` → **`:421-447`**；`syncer/syncer_test.go:646-670` → **注释 `:595-596` + 函数 `:597`**；`waitForNoSMTPData` 调用点 **3 处**（`alertset_policy_test.go:92/150/180`，非 4 处）；§6 规模口径过期（现 **94 个 Go 测试文件 / 31,373 行**，起 goroutine 文件 36 个） |
| B3 | 审计 §6 字面量锚点 | 改为 **1/5**，并点名 `DefaultStartupGrace` 为唯一无锚点的合同数值；`push_test.go` 的 `250` 锚点在 `:246`（审计写 `:243`） |
| B4 | 审计 §6「`webui/api` 对 `InFlightLimiter` 零断言」 | **不成立**：grep 现 3 行命中、退出码 0（`alertset_drop_log_test.go:40/114`、`alertset_subscription_test.go:28`），且 `:42/:116` 直接断言 `l.Acquire()` 布尔、`:88-121` 用本地 HTTP 服务器实收请求数断言投递次数 |
| B5 | 审计 §7 第 17 条（`isRetryable` 字符串兜底的依据位置） | 两处事实订正：① `syncer/retry.go` 现全文 **104 行**，原引 `:123-175` **已失效**，依据实际在 **`:11-30`**（关键 `:23`）② "全仓 `.md` 内不含 `Unwrap` 说明"**被证伪**（`Issue6.md:584` 等以 `.md` 记录了同一裁决）。规范结论"不得归因为 `.md` 注释"仍成立，但须改写为"依据在 Go 注释；`.md` 亦有同一记录" |
| B6 | 审计 §13 行数口径 + §0.5 可追溯性 | §13 表格 17 个 `\|` 行 = 表头 + 分隔 + **15 数据行**（另有结论段 1 行），引用时必须写明口径；§0.5 与 §13 **无法逐项追溯 16 项 PT**（§13 只点名 3 项）⇒ 应指向 `ProdTestList.md` 与本台账 §4 |
| B7 | 审计 §13「`syncer/syncer.go` 第二个独立复核未完成」与 §11/§13「`bundle_v3.go` 第二双眼睛未完成」 | 历史台账记录两处后续复核已完成；本次也由独立子代理核验。本次已澄清审计的历史未完成状态；历史细节不冒充本次重演（见附录 A 的 §4 全节行） |
| B8 | 审计 §4 第 3 张表（🔁重复实现） | **2/5 成立，3 项已被后续批次收敛**（前端 `theme` 双写已失效、前端重复实现后端校验已失效）；另`resetAll` 从不调用 `clearAllCache`（无持久化，800ms 窗口无功能影响） |
| B9 | 审计 §4 锚点与计数 | 31 个可核验锚点中 **19 个已漂移**（`config/store.go` 统一 +48 行、`internal/health/push.go` +20 行）；"仅 5 个测试文件"实为 **4 个**；"11 个仅测试导出"实为 **12 个符号**；`push_test.go` 的 `var _` 锚点为 `:500-502`（且"三个无用 import"表述有误）。**下轮改动前必须先更新锚点**，否则会误判"符号不存在" |
| B10 | 审计 §11/§4/P2-03/P2-08/P2-09 的符号与行号 | 4 处漂移：§11 `:1628` 调用链仍写已删除的 `syncDomain/retrySyncDetailed`（现为 `Run → syncAll → runRound → syncTarget → runTargetAttempt/runTargetCleanup`）；§4 `:1204` 的 `syncer.go:813` 失效（真实为 `:716/:951` 一带）；P2-03 行 `:694` 的"syncer 内 ECS+ICMP WARN"已不存在（现为 `provider/plan.go` 的 `IssueUnsupportedICMPv6`）；P2-08/P2-09 前提行号过期 |
| B11 | 审计 §2/§8.2 与 §11 的陈旧符号链 | `syncDomain → retrySyncDetailed` 已由 `ba82292` 删除（全仓 0 命中），按 §0.2 口径属历史引用快照；本次已更新当前调用链表并加原审计历史警示，其余历史叙述不改为本次事实 |
| B12 | **`TODOLIST.md:30`（TODO-001）** | **优先级 P0/高 与后果不匹配**（该条目自述已写"核心安全门已存在"）。按 I8-01 的核实：清理门在 `provider/plan.go:698-701` 独立冻结，前端无消费者 ⇒ 实际后果是 API 输出字段语义错误，**本次已降级中低**并明确无删除后果，生产判定未修 |
| B13 | 审计 §0.2 与 P3 清单的**口径** | 已订正部分（P3-24/P3-18/P2-07 的"尚未提交"、§14 与附录 A 的过期基线、P3-06 同格双结论、P3-17 九→十文件、P3-24 他机证据路径）**已随 `b658492`/`fd6ed7e` 入库，不再列出**；采集时行号会漂移，后续引用请用「符号/章节名 + 行号」双锚点 |

---

> **历史整理基线说明（不是本次当前状态）**：前次台账整理时的 HEAD 为 `da644b5`（`origin/main` 仍 `938bb8b`，ahead 3、未推送）。正文全部结论针对**生产工作树内容**，复核期间被复核的生产文件在两个 revision 间逐字节相同，故当时生产定位仍可追溯；本次纠正部分推断，当前基线与证据以 §0、§1.6 为准。**本次仅修改Markdown，生产源码、测试、API/schema、依赖、部署配置零改动。**


## I8-01 方案 A 当前实施补记（2026-10-08）

- **授权与基线**：用户确认采用 A 后授权修复。实施前 `main == 本地 origin/main == 7696482243461f582342aeb78fdc2f467012f674`，工作树与暂存区干净；未 fetch。
- **字段合同**：`coverage_ready` 表示当前完整快照已精确覆盖非空的全部可实施期望，且 Add 提交状态确定；空期望和全量 unsupported 为 false，混合目标可 true + partial。它不表示整体成功或清理授权，不新增多状态字段。
- **实施范围**：唯一生产逻辑修改为 `provider/plan.go` 的覆盖公式：`implementable > 0 && covered == implementable && !in.AddStateUnknown`。复用现有计数，不修改能力矩阵、Desired/ToAdd 构造、canonical key、Provider、DNS、正式目标结果判定、删除定位、API/schema 或前端。同步 AGENTS §三、Issue7 §4.5、本文与 TODOLIST，共七文件。
- **可观察变化**：unsupported-only 的 `coverage_ready` 从 true 变 false，`cleanup_deferred` 额外包含 `coverage_not_ready`；原 unsupported 原因、清理候选和零删除保留。混合目标可实施部分已覆盖时仍为 true，正式结果仍 partial，清理冻结不放宽。
- **判别回归**：`TestPlan_CoverageReadyImplementableSubset` 的十场景覆盖空集、全量 unsupported（无匹配/有匹配）、混合缺失/Owned/External、纯 Owned/External、缺失、Add 状态未知；增强三种平台能力场景（SWAS IPv6/DROP、ECS ICMPv6）的 false/零删除断言。`TestDryRun_CoverageReadyCapabilityBoundary` 直接断言输出，而非与同源 planner 互比；同一夹具继续通过正式 `syncTarget` 验证全量 unsupported 与混合目标均 partial、保留残留、零删除。
- **红→绿证据**：修复前，三个能力场景、全量 unsupported 两个矩阵场景及 Dry Run 全量 unsupported 按行为断言失败；无编译失败或 panic。修复后上述定向回归 `-race -count=20` 两包通过。
- **最终门禁**：定向 `go test -race -count=20 ./provider ./syncer -run 'TestPlan_(CoverageReadyImplementableSubset|UnsupportedMatrixFreezesCleanup)|TestDryRun_CoverageReadyCapabilityBoundary'` 通过；全量 `go test -race -count=1 ./...` 十二包全部 ok；`go vet ./...`、`go build ./...`、受影响 Go 文件 gofmt 检查及最终文档后的 `git diff --check` 均通过。全量门禁含既有 TestMain 构建产品二进制的本地进程回归，不是本项专门产品进程验收；单轮全量不外推长期稳定。
- **边界**：Go 1.27.1 darwin/arm64，使用既有 ignored 前端 dist；本轮未执行前端构建、浏览器、Linux/Docker/compose、真实云/通知链路或远端 CI/GHCR。全量默认 Go 门禁包含既有真实 DNS 查询测试（I8-04）；它们不是本项的 DNS 外部验收，网络测试可能自行 skip，不据包级 ok 宣称上游验证通过。Q-01 已裁决，I8-01 本地修复不外推长期稳定或外部验收；其余 I8/Q 项保持独立。尚未提交或推送。


## I8-02 当前实施补记（2026-10-08，方案 C）

- **授权与基线**：根据引用研究聊天中用户的三项裁决开始正式修复并同步文档。实施前 `main / 256a7b365cfa1759ae6e51dcaa2686422c9dfd00`、本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`、ahead 1，工作树/暂存区干净；未 fetch/push。I8-01 的生产修复已包含在该基线，保持原样。
- **实现**：目标增加 attempts、nullable cleanup_observation/unsupported_observation；有效新观察（包括零）替换，后续早退保留来源并标历史。清理依据区分 s1/s2/delete_progress；最新规划与最近完整规划分存，不完整空列表不抹去完整历史。原增删/cleanup_deleted 累计确认，候选/残留保留整数类型为最近观察的兼容投影。整轮四类互斥汇总只累加 observed 数量，Dashboard 与 SQLite 分开累计操作与候选/残留观察。完整字段和更新合同见 [Issue7 §12.9](./Issue7.md#129-i8-02-观察表达修复2026-10-08用户裁决-c)。
- **回归**：仓内实际目标链覆盖 S2失败后的S0/Add/S1早退与恢复、新零替换、未知/估计零/可信零/NotFound、latest不完整保留last_complete与完整空替换、混合四类目标及JSON；正式 Run→publisher→EventBus→SQLite 验证历史/恢复/估计/未知，状态 API 与真实 HTTP SSE 保留 nullable 和汇总字段；前端真实 Dashboard setup/模板渲染 8 项回归。
- **负向控制**：六类仓外正式源码 overlay（清空历史、拒绝零更新、提升估计、覆盖完整历史、汇总估计作观察、删除事件观察）均按行为断言变红，无编译失败/超时/数据竞争；旧 Dashboard 与忽略零估计提示两类前端候选按行为断言变红。临时证据属于本轮仓外验证，不新增长期错误实现。
- **门禁**：`go test ./syncer ./webui/api -race -run '^TestI802_' -count=20 -timeout=10m` 两包通过；`go test ./... -race -count=3 -timeout=20m` 全仓 12 包通过（单条命令将每项测试重复三次，不称三次独立执行）。`go vet ./...`、`go build ./...`、9 个受影响 Go 文件的 gofmt 检查、`git diff --check` 通过；前端 `npm run test:dashboard` 8 项、`node --test tests/*.test.mjs` 全部 66 项及 `npm run build` 通过，最终 Go 门禁使用本轮构建的真实 ignored dist。全仓门禁包含既有 TestMain 构建正式产品二进制的进程回归，不是 I8-02 专项产品浏览器/云验收。
- **范围与边界**：7 个生产/前端、6 个测试与 AGENTS/Issue7/Issue8/TODOLIST/ProdTestList 共 18 文件。Provider/SDK、DNS策略/超时、重试次数、版本保护、先增后删、清理安全门、outcome/健康、数据库 schema、配置包版本、依赖与通知固定详情保持。旧客户端忽略新增字段仍不能识别未知，历史日志不回填。Go `1.27.1 darwin/arm64`；未执行真实浏览器、Linux/Docker/compose、真实云/SMTP/收件箱/Webhook/Uptime Kuma、原生 amd64/macOS 13 或当前 revision 远端 CI/GHCR；人工未执行/免除状态保持。本地门禁不外推长期稳定或外部通过；默认全仓门禁含 I8-04 的既有网络 DNS 测试，其包级 ok/自行 skip 不是本项外部验收。源码/测试/文档尚未提交或推送。

## I8-18 当前实施补记（2026-10-08，用户裁决 A）

- **授权与恢复点**：用户在本聊天确认窄只读事务方案 A、进一步确认 8 文件范围后授权正式修复与文档同步。`main / 3f50f3821c86d04ee19b67a1d280a28069880194`，本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`，ahead 2；实施前工作树/暂存区干净，未 fetch、提交或 push。
- **正式实现**：生产仅 `config/store.go` 与 `webui/api/alerts.go`。新增值类型 `AlertsSnapshot` 与接收请求 context 的 `LoadAlertsSnapshot`，四个私有 loader 共用 `BeginReadOnlyTx` 返回的 tx；读取及 commit 全成功才返回完整快照，所有失败返回 nil/error，回滚错误检查并记录，已结束事务忽略 `sql.ErrTxDone`。事务在方法返回前结束，不进入配置协调器、不预留写锁、不持有到 HTTP 发送。完整业务快照/导出/启动加载和四个单项 getter 保留。
- **响应与默认值**：GET 原四对象、字段类型、SMTP 密码/Webhook URL/Push URL 回显、Content-Type/no-store 和安全 500 文案保持；HTTP 层只对空字符串补齐邮件 port/subject/body、Webhook channel，空格字符串保持原值。policy/Push 时长继续由已有 loader 解析，非空非法值失败；缺行继续默认全关。无新增 email/Webhook 归一化或校验，targets/rules/settings 不参与 GET。
- **正式仓内回归**：新增 `config/store_alerts_snapshot_test.go` 与 `webui/api/alerts_snapshot_test.go`。config 测试专用驱动包装保留真实 SQLite，在第四读前提交完整新配置，证明本快照四项全旧/下一快照全新；覆盖只读选项、begin/四阶段查询/commit/rollback 注入、取消后连接释放与恢复。提交失败由测试驱动模拟并先清理底层事务，不冒充真实存储故障。API 覆盖四表明确无行、四对象与全部子字段存在/非 null、默认全关、原值读取、无关三表 DROP、七类安全错误出口、取消与恢复；真实 HTTP 两写者各 25 PUT、三读者各 60 GET 检查四项版本一致，发布次数为 51 次 PUT（含初始化）、GET 不增加发布。
- **正式负向控制**：五类仓库外 overlay 使用本轮正式测试：第四读跳出 tx 检出混合值；改为普通写事务约 5 秒后返回 SQLITE_BUSY；忽略 commit 错误检出非 nil 成功快照；复用完整业务快照检出无关 settings/rules/targets 引起 500；旧 GET 四读在真实 HTTP 并发测试中检出混合值。全部按目标行为变红，无编译失败、超时或数据竞争。研究阶段的驱动回调曾直接中止测试导致退出死锁，正式夹具改为返回错误后在驱动调用外断言；研究门禁不混作正式证据。
- **正式门禁**：`go test ./config ./webui/api -run '^TestI818Snapshot' -race -count=20 -timeout=3m` 两包通过；受影响两包完整 `-race -count=1 -timeout=10m`、`go vet ./...` 与 `go build ./...` 通过。`go test ./... -race -count=3 -timeout=20m` 全仓 12 包通过（单条命令将每项测试重复三次，不称三次独立执行）；四个受影响 Go 文件的 gofmt 检查与最终 `git diff --check` 通过。全仓包含既有 TestMain 构建当前正式产品二进制的进程回归，不是 I8-18 专项产品验收。
- **范围与边界**：2 个生产、2 个新增测试及 AGENTS/Issue8/Build7/审计报告 4 个文档，共 8 文件。前端/协调器写事务/schema/配置包版本/依赖/通知和健康循环不变；不增加 ETag、版本冲突检测或部分保存，旧表单先后整体保存仍可能覆盖，保证仅为单次 GET 内部一致。Go `1.27.1 darwin/arm64`，Go 门禁使用本轮开始时既有 ignored 前端 dist（未重建前端）。未执行 I8-18 专项产品进程/浏览器、Linux/Docker/compose、原生 amd64/macOS 13、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR；原人工未执行/免除状态保持。既有全仓门禁含 I8-04 网络 DNS 测试，包级 ok/自行 skip 不作为本项外部通过证据。本地重复回归不外推长期稳定或外部验收。源码/测试/文档尚未提交或推送。

## I8-111 当前实施补记（2026-10-08，严格方案 B）

- **授权与基线**：用户明确当前项目无兼容性需求、允许破坏性改动，随后授权正式修复与文档同步。实施前 `main / 3cc4c3db3da02b7985ddc2700a06bd12af234d98`，本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`，ahead 3，工作树与暂存区干净；前序 I8-18 已提交为 `3cc4c3d`。未 fetch、提交或 push。
- **正式实现**：生产仅 `webui/api/decode.go`、`bundle_v3.go`、`export.go`。导入专用 `decodeBundleV3Strict` 先经 MaxBytesReader 完整有界读取，再要求单一顶层对象并以标准库 JSON v2 解码；默认字段名精确匹配、拒绝重复/非法 Unicode，显式 RejectUnknownMembers。`presenceSlice.UnmarshalJSONFrom` 复用同一 decoder 与选项，删除旧 v1 自定义解码，不保留兼容分支。`decodeBundleV3Error` 仅取错误类别与 JSON Pointer，不回显 JSONValue 或 Error 原文；读取错误沿安全出口处理。
- **输入与状态合同**：同一对象内同值重复、转义后重复、重复对象合并均拒绝；大小写变体是未知字段（即使与合法字段并存也拒绝）。合法转义/代理项对、顺序、空白与合法空数组仍允许，不要求原始字节规范化；不同对象的同名字段正常。超限统一 413，其他非法输入 400 且 no-store；版本仍仅接受 3，完整字段/非 null/领域校验继续在写事务之前完成。普通 API、schema/SDK/依赖/前端/通知/健康/目标状态机均不变。
- **正式仓内回归**：新增 `import_json_test.go` 的七项 TestI8111*，既有 `import_test.go` 仅订正共享解码的滞后注释。57 条合同字段路径逐一覆盖大写、重复、缺失、null（228 个字段变体）；另覆盖对象合并、转义重复、非法 UTF-8/孤立代理项、根类型与尾随输入，八类合法输入、恰好 10 MiB/超出一字节/超限且前缀无效、读取失败、安全字段路径与敏感值不回显（包括标准库整数溢出诊断会携带的原始数字）。每个拒绝请求检查完整业务快照、扫描缓存、同步日志、旧 RuntimeState 指针、发布次数、日志级别与告警集合不变；持协调器锁的有界回归证明非法 JSON 在事务入口前返回。普通 settings API 的原有大小写/覆盖行为另作范围控制。
- **判别证据**：新正式字段回归先在旧实现上运行，错误大小写与重复 version 返回 200 并改变状态，按行为变红；修复后七项定向 `go test ./webui/api -run '^TestI8111' -race -count=20 -timeout=5m` 通过。六类仓外源码 overlay 使用同一正式测试：放开重复、放开大小写、放开非法 Unicode、数组改回独立 v1 解码、原始错误/JSONValue 回显、流式解析先于限额判断，均按断言变红，无编译失败、panic、超时或数据竞争；研究候选证据不替代正式门禁。
- **最终门禁**：`go test ./... -race -count=3 -timeout=20m` 全仓 12 包通过（单条命令将每项测试重复三次，不称三次独立执行）；补充整数溢出不回显原值回归后，`TestI8111SafeErrors` 定向 race 20 轮与最终 `go test ./... -race -count=1 -timeout=20m` 全仓 12 包再次通过。`go vet ./...`、`go build ./...`、五个受影响 Go 文件 gofmt 检查与最终 `git diff --check` 通过。全仓包含既有 TestMain 构建当前正式产品二进制的进程回归，不是 I8-111 专项产品验收。
- **范围与外部边界**：三个生产、两个测试与五份文档，共 10 文件。Go `1.27.1 darwin/arm64`，Go 门禁使用既有 ignored 前端 dist（未重建前端）。未执行 I8-111 专项产品进程/浏览器、前端构建、Linux/Docker/compose、原生 amd64/macOS 13、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR。既有全仓门禁含 I8-04 网络 DNS 测试，包级通过/自行 skip 不作为本项外部通过证据；重复本地回归不外推长期稳定或外部验收。源码/测试/文档尚未提交或推送。

## I8-113 当前实施补记（2026-10-08，方向 A 细化为空渠道＋单选按钮）

- **授权与基线**：已读取引用聊天《研究 I8-113 修复方案》及仓库外准备稿；用户确认空渠道初始值、单选按钮、不要求旧版导入新增空渠道配置包、保留 version 3，并在本聊天授权实施及文档同步。实施前 `main / 8052015c766121b05cdaf8afe2eabd55a45aa8db`、本地 `origin/main / 7696482243461f582342aeb78fdc2f467012f674`、ahead 4，工作树和暂存区干净，未 fetch。本轮源码/测试/文档尚未提交或推送。
- **数据库与读写**：新建/补列 DDL 引用 `DefaultWebhookChannel=""`；初始化和 reset 共用 `defaultWebhookRowSQL` 显式写 channel，旧表默认值不再重新出现，不重建历史表。仅 ensureColumnTx 本次确实新增 channel 列时，将已有 id=1 主行补为固定历史钉钉值；无主行创建空渠道，再次启动保留用户值。既有 Build7 主题/正文补列时的一次性启用归零保持。GET 和导出原样保留空渠道，其他字段默认补齐保留；普通保存/导入共用领域校验：关闭可空，开启必须选择合法渠道并填写有效 HTTP/HTTPS URL，字段仍必填，未知/缺失/null 均拒绝。
- **发送与界面**：取消通知器构造与 payload 的隐式钉钉；不相关事件仍忽略，相关事件非法渠道先于在途名额与 HTTP 返回固定 `channel=unknown category=invalid_channel`，不保留原始渠道/URL/错误链。三渠道 payload、响应校验、16 KiB/10 秒、限流及健康关系保持。Alerts.vue 使用真实 NRadioGroup/NRadio，空初始值、单选在 URL 前，开启未选明确提示且零 PUT；关闭/重新开启保留渠道和 URL。TypeScript channel 为必填 string。
- **兼容与范围**：配置包仍为 version 3，原有合法包继续可导入；旧版可能拒绝新增“关闭且空渠道”包，属已确认边界。原定八个生产文件加用户本聊天追加确认的 `webui/embed.go` 一行嵌入修正，十三份测试、五文档，共 27 文件；不扩展到 SMTP/Push 默认值，不改变表字段结构、JSON 严格解码器、协调器原子发布、事件策略、投递时限/在途上限或 Provider/DNS/目标状态机。
- **正式回归与判别力**：三个新增 `webhook_channel_test.go` 共六项 TestI8113*，覆盖新建、缺列空/有主行、旧默认列空表/已有配置、reset、迁移中途失败回滚新增列及原配置、重启保留；GET/PUT/导出/导入/reset 空值往返及三种原有合法渠道；八类非法配置在 PUT/导入均 400＋no-store，完整业务快照、扫描缓存、同步日志、运行时指针、发布次数、日志级别与告警订阅集合不变；非法渠道三类相关事件、空闲/满载名额、零 HTTP、真实 EventBus 安全 WARN。不再用非法渠道测试 transport 失败，合法飞书 transport 脱敏继续验证；未知渠道丢弃统计在限流器层保留。前端新增三项真实 setup/单选 SSR 回归，空值零选中、三种已有渠道恰选一个、保存引导和关闭保留输入。
- **已取得门禁**：新正式回归先在旧实现按目标行为变红；修复后 `go test ./config ./webui/api ./notifier -race -run TestI8113 -count=20 -timeout=10m` 三包通过；三个受影响包完整 race 一轮通过；告警 8 项、前端全部 69 项回归与 `npm run build` 通过，正式 ignored dist 已更新。`go vet ./...`/`go build ./...` 通过。八类正式 Go 源码反向控制（新 DDL 恢复钉钉、reset 不显式渠道、遗漏历史转换、每次启动重写、开启空渠道放行、空值通知器钉钉兜底、GET/导出补钉钉）与两类前端反向控制（默认预选、移除开启未选保存守卫）均按行为断言变红，无编译/接线失败；完整源码控制与日志保存在仓库外 `/Users/kyle/.codex/outputs/i8-113-implementation-2026-10-08/`。
- **首轮失败与追加修正**：重建前端后 `go test ./... -race -count=3 -timeout=20m` 为 11 包通过、webui 失败，三轮 `TestStaticAssetsServedFromEmbed` 均发现 index.html 引用 `_common-C8PoDiYF.js` 返回 404。修正前 `//go:embed frontend/dist` 排除下划线文件。仓库外一行候选 `all:frontend/dist` 已使该回归 race 20 次通过；它需要在八文件范围外修改 `webui/embed.go`，用户已在本聊天明确确认范围扩展，现已正式改为 `//go:embed all:frontend/dist`，只调整构建资源包含规则；正式 `TestStaticAssetsServedFromEmbed -race -count=20` 通过；仓外 overlay 恢复旧嵌入规则时同一测试精确变红（下划线资源 404），证明一行修正有判别性。修正后全仓三次重复门禁仍在执行，最终结果待回写，首轮失败保留为历史事实。
- **外部边界**：Go `1.27.1 darwin/arm64`。真实浏览器专项登记为 ProdTestList PT-AUDIT-02，尚未执行；真实通知平台继续 PT-B7-03 人工免除、非通过。未执行本项专门产品进程验收、Linux/Docker/compose、原生 amd64/macOS 13、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR。全仓默认测试包含既有网络 DNS 测试，包级通过/自行 skip 不作为本项外部验收证据；全仓门禁含既有 TestMain 产品二进制进程回归，不能外推本项浏览器或真实投递通过。

## I8-12 当前实施补记（2026-10-08，推荐方案 A）

- **授权与基线**：用户在本聊天授权推荐方案并要求同步文档；实施前 `main == 本地 origin/main == c3bb468478f02b32361c85be1090c70743750579`，工作树/暂存区干净，未 fetch。Q-14 固定重试清空，不保留历史预览。
- **正式实现**：`useDryRun.ts` 使用 `idle/running/completed/failed`，loading 从阶段计算；新请求清空旧结果/提示/错误，完成或失败更新 `lastFinishedAt`，异常非 Error/空 message 使用稳定原因。`RunTest.vue` 阻止在途重复入口、传递阶段和错误，完成用中性通知、失败使用同一持久原因；时间显示最近请求结束。`DryRunResults.vue` 只在 completed 渲染本次统计/卡片；等待/失败独立展示，空结果中性提示且保留 warnings；现有无适用规则卡片与目标明细保持。completed 不等于目标全正常，不增加全局无变更成功结论。
- **台账订正**：后端零目标本来返回 warning，不必然进入绿色分支；运行时未就绪也可能是空结果加 warning。本文保留修复前现象并订正零结果解释，不能把空结果推断为零配置或已确认无变更。
- **正式回归**：新增 `tests/dryrun-state.test.mjs`，经真实 `api.ts` 的 fetch 替身、真实 composable/页面 setup 和编译后的页面→结果组件模板验证可见行为。17 项逐用例 3 秒上限，覆盖初始/等待/重复入口，首次及曾成功后的 409/500/503/网络失败、通知消失后错误保持、重试恢复、最近结束时间、零目标/未就绪/无 warning 空结果、无适用规则、HTTP 200 的目标错误/DNS/限制/冲突/清理延后，以及正常待新增明细和零变更卡片。Naive UI 组件用轻量节点替身检查文本、类型与明细，不作为真实浏览器/样式验收。夹具首轮重复点击测试误换了待完成 Promise，出现 3 秒超时；修正夹具为调用同一页面入口后通过，没有修改产品策略迎合测试。
- **正式负向控制**：仓库外隔离源码使用本轮正式测试，恢复旧页面/组件展示、隐藏失败原因、保留旧预览、绿色完成 toast、绿色空结果、失败不更新时间六类均由目标行为断言检出；无编译错误、TypeError 或挂起作为红灯证据。隔离源码/日志保存在 `/tmp/fwalizer-i812-research/controls/`。
- **门禁**：`npm run test:dryrun` 17 项与 `node --test tests/*.test.mjs` 全部 86 项通过；`npm run build`（vue-tsc + vite）通过并更新 ignored dist；`go vet ./...`/`go build ./...` 通过。`go test ./... -race -count=1 -timeout=20m` 全仓 12 包通过，含使用本轮重建前端产物的既有产品进程及静态嵌入回归；单轮通过不外推长期稳定。最终 `git diff --check` 通过。
- **范围与外部边界**：三个前端生产文件、package.json 命令入口、一份专用回归与 AGENTS/Issue8/Issue7/ProdTestList/审计五份文档，共十文件。无后端业务源码/API/schema/配置包/依赖/正式同步/健康改动，不新增超时、自动重试或历史缓存。Go `1.27.1 darwin/arm64`；真实浏览器登记 PT-AUDIT-03，尚未执行。未执行本项专门产品进程验收、Linux/Docker/compose、原生 amd64/macOS 13、真实云/SMTP/收件箱/Webhook/Uptime Kuma 或当前 revision 远端 CI/GHCR；其他人工未执行/免除状态保持。全仓默认 Go 门禁包含既有 I8-04 网络 DNS 测试，不把包级通过/自行 skip 当作外部验收；既有 TestMain 产品进程回归也不替代本项浏览器验收。本地证据不外推长期稳定或外部通过；源码/测试/文档尚未提交或推送。

## I8-03 当前实施补记（2026-10-08，用户裁决 A：共享真实前端前置）

- **授权与基线**：用户在本聊天选择 A，随后明确授权修复并同步文档；实施前 `main / bb8e30103a21e28523745e5eed71be17694451e8`、本地 `origin/main / c3bb468478f02b32361c85be1090c70743750579`、ahead 1，工作树/暂存区干净，未 fetch。本轮修改尚未提交或推送。
- **正式实现与契约**：Makefile 仅将 `test:`、`vet:` 改为依赖 `frontend`，保留已有 `build: frontend`、`.PHONY frontend` 与 `npm ci && npm run build`。前端成功后才能执行消费者；同一次 Make 调用共享一次构建，前端失败则不执行 Go。`make -j4 all` 在前端完成后允许 Go 检查/测试/构建并行，任一失败返回非零，不承诺其他并行任务从未运行或独立 Make 进程互斥。不引入增量缓存、占位 dist、替代 embed、递归 Make 或新 Make 版本要求。
- **正式回归与判别力**：新增 `build/makefile_test.sh`，POSIX shell 在隔离目录复制待测真实 Makefile、使用本地 npm/go 替身；不安装依赖、不创建 dist。17 项覆盖 test/vet/build/all、并行 all、多目标、npm ci/前端 build 失败抑制消费者、三个 Go 阶段失败传播与单次安装/构建计数；同名 frontend 文件验证 `.PHONY` 不被遮蔽。`sh build/makefile_test.sh` 全部通过；旧 Makefile、缺 test 前置、缺 vet 前置、仅 all 加兄弟前置、移除 frontend 的 phony 声明五类仓外负向控制均被同一正式回归按行为断言检出。`sh -n build/makefile_test.sh` 通过。
- **正式工作树真实门禁**：运行 `make -j4 all`，外部临时 go 包装器仅对 test 追加 `-short -timeout=5m`，以隔离 I8-04 的两个外网 DNS 用例；Makefile 原测试命令未改。npm ci 与 vue-tsc/vite 构建各一次，Go build/vet 与全仓 12 包 race 短模式测试一轮通过；现有 TestHTTPRoutesRegression、TestStaticAssetsServedFromEmbed 与既有 TestMain 产品二进制进程回归包含在该门禁内。此结果不是原样默认 make test 的外网验证，也不外推长期稳定绿色。正式 ignored dist 与二进制已重建。
- **无产物副本验证**：按正式工作树 tracked 输入加本轮新增脚本复制到仓外隔离目录，明确不携带 dist/node_modules（不是尚未提交修改之前的 git archive HEAD）。执行真实 `make -j4 vet build` 通过，npm ci 与前端 build 各一次，Go vet/build 成功并生成真实 index.html 与 fwalizer；副本 Makefile 与正式工作树逐字节一致。研究阶段的清洁副本 make all 和占位页负向证据仅作为研究历史保留，不冒充本轮正式门禁。
- **文档闭环与历史订正**：README 推荐一次 make all，删掉完整二进制章节重复 npm build，说明单独调用的重建成本、裸 Go 前置及独立脚本入口；AGENTS 固定构建契约；本文标题/状态表/Q-05/优先级与审计对应当前状态同步。占位页可编译但当前静态资源回归会拒绝缺少 JS 的 dist，原“build/vet/test 全变绿”表述已订正，禁止占位修法保持。审计最近基线与历史指针更新，本次不回写其他缺陷的完成结论。
- **范围与证据**：Makefile、一份新回归脚本与 README/AGENTS/Issue8/审计四份文档，共六文件；Go 业务源码、embed、前端源码、模块/前端依赖、CI/Docker、DNS 测试与 clean 策略不变。证据保存在仓外 `/private/tmp/codex-i803-implementation-xpxif0aj/`（checks.json、regression.log、各负向控制、formal-result.json/formal-all.log、clean-result.json/clean-vet-build.log）；最终六文件范围、暂存区为空及 `git diff --check` 已检查。
- **环境与外部边界**：Go `1.27.1 darwin/arm64` 实际二进制、`GOTOOLCHAIN=local`；Node `26.7.0`、npm `11.19.0`、GNU Make `3.81`。本轮未验证 CI 固定 Node 24.21.0、Linux/Docker/compose、浏览器、原生 amd64/macOS 13、真实云/上游 DNS/SMTP/收件箱/Webhook/Uptime Kuma 或远端 CI/GHCR。I8-04 未修，其他外部未执行/免除状态保持。npm ci 输出 3 个 high 漏洞提示，未执行独立 npm audit、未修改依赖，不将历史 audit=0 写成本轮结果；依赖风险独立核验，不扩大本次修复。
