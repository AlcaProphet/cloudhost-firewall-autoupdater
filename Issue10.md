# Issue10.md — Build8 Step 11～15 复核问题台账

> **建立日期：2026-11-14，用户授权。** 本文件承接「核查 Build8 步骤 11-15 构建」会话发现的 7 项缺陷及证据/文档偏差，在当前更新后的 `beta / e94c8fc` 上重新核验，并按复核结果更新问题列表。复核阶段只读；2026-11-14 后续单提交已处理 §4 的 R08～R13，实施补记见 §7。
>
> **复核基线：** `e94c8fca355b46912865a3b748fb5affad75c5c8`（`beta`），父提交 `ee20e08`；本地 `origin/beta` 为 `ee20e08`，ahead 1；复核开始时工作树与暂存区干净。Build8 Step 11～15 的实施提交为 `b3ccb6b`，首轮核验修复为 `ee20e08`，二次核验修复为 `e94c8fc`。
>
> **结论摘要：** 原 7 项确认缺陷在当前代码中均未复现，且已有对应回归；原 Step 11/12/15 的证据缺口多数已补齐。复核时仍存在 1 项低严重度健康原因文案问题（I10-R08）、1 项文档提交状态漂移（I10-R09）和 4 项测试/证据缺口（I10-R10～R13）；后续单提交已修复/补齐（见 §7）。未发现新的高危或中危生产缺陷。
>
> **边界：** 未执行真实腾讯云 TEO/确认/生效、真实 DNS、产品浏览器人工回归、Linux/Docker/compose、`make all`、依赖审计、当前 revision 远端 CI/GHCR 与发布。本地替身、临时 SQLite 与单轮全绿不外推为外部验收或长期稳定。

## 1. 复核基线与方法

- **代码基线：** 当前 `HEAD` 为 `e94c8fc`（三次核验修复，23 个文件），其父提交为 `ee20e08`（首轮核验修复）。`beta` 相对 `origin/beta` ahead 1；复核开始前 `git status --porcelain` 无输出。
- **复核范围：** 原会话确认的 7 项缺陷、原会话列出的 Step 11/12/15 回归缺口、Step 13/14 前端证据缺口，以及 AGENTS/Build8 的提交状态与测试口径。
- **方法：**
  1. 逐一对照原缺陷的触发路径与当前生产代码，确认修复是否改变了原语义；
  2. 运行原缺陷对应的定向回归和全量门禁；
  3. 在 `/tmp` 的独立 `git worktree`（同一 `e94c8fc`）中加入临时探针，复现「配置发布后未观察预检」「仅 EdgeOne DryRun results」「单源 EdgeOne 数据不足原因」三个关键场景；探针不进入仓库，复核后删除 worktree；
  4. 对仍存在的证据缺口，以「代码路径 + 已有测试」判断是生产行为问题还是仅缺判别性回归。

**相关文件：** [syncer/syncer.go](syncer/syncer.go) · [syncer/edgeone_round.go](syncer/edgeone_round.go) · [syncer/dryrun_edgeone.go](syncer/dryrun_edgeone.go) · [syncer/edgeone_confirm.go](syncer/edgeone_confirm.go) · [internal/health/health.go](internal/health/health.go) · [Dashboard.vue](webui/frontend/src/views/Dashboard.vue) · [DryRunResults.vue](webui/frontend/src/components/DryRunResults.vue) · [step11_transition_test.go](syncer/step11_transition_test.go) · [step12_dryrun_test.go](syncer/step12_dryrun_test.go) · [step15_source_test.go](syncer/step15_source_test.go) · [step14-ui.test.mjs](webui/frontend/tests/step14-ui.test.mjs) · [edgeone-ui.test.mjs](webui/frontend/tests/edgeone-ui.test.mjs) · [Build8.md](Build8.md) · [AGENTS.md](AGENTS.md) · [Design6.md](Design6.md)

## 2. 复核总表

### 2.1 原确认缺陷

| 编号 | 原缺陷 | 当前状态 | 复核依据 |
|---|---|---|---|
| [I10-R01](#i10-r01) | 双开配置发布/重启后，未观察预检被 health 误报为两侧数据不足 | **已关闭** | `ee20e08` 修复；定向/独立探针均不再产生数据不足原因 |
| [I10-R02](#i10-r02) | 仅 EdgeOne 时 DryRun `results` 混入停用 DNS 规则并产生伪 `dns_empty`；前端误显示「无适用规则」 | **已关闭** | 按开关过滤规则，新增 `edgeone_sites`；results/探针均正确 |
| [I10-R03](#i10-r03) | 两侧均不完整时前端仍提示「单侧继续」 | **已关闭** | Dashboard/DryRunResults 已区分未观察/单侧/两侧；UI 回归通过 |
| [I10-R04](#i10-r04) | `address_preflight.protected_cidrs` 双开含全部当前/待更新前缀，单源为空 | **已关闭** | 双开与单源均只回填过渡保护；精确集合回归通过 |
| [I10-R05](#i10-r05) | Dashboard 把 `/api/edgeone/status` 查询失败显示为「暂无记录」 | **已关闭** | `edgeStatusError` 与模板分支；UI 回归通过 |
| [I10-R06](#i10-r06) | DryRun 站点在数据不完整时隐藏已知版本；unit 与站点 `data_complete` 矛盾 | **已关闭** | 版本独立回显；完整性纳入地址族与快照有效性；回归通过 |
| [I10-R07](#i10-r07) | 来源级 `last_outcome` 沿用整轮结果，单侧失败污染另一侧 | **已关闭** | 按实际参与目标归因；双向回归通过 |

### 2.2 仍存在、新发现或需要补证的事项

| 编号 | 事项 | 复核时状态 | 实施结果 |
|---|---|---|---|
| [I10-R08](#i10-r08) | 仅 EdgeOne 启用且查询不完整时，健康原因仍写「单侧继续」，但实际没有另一侧可继续 | **新发现** | 已修复：单源使用「EdgeOne 数据不足（来源已阻断）」 |
| [I10-R09](#i10-r09) | Build8/AGENTS 仍把 `e94c8fc` 批次写成「本地实施」「改动保留在工作树」，与实际提交不符 | **仍存在** | 已订正提交状态与命名口径 |
| [I10-R10](#i10-r10) | 来源级 `partial` 只有合成/合并用例，没有真实轮次产生 `TargetPartial` 并断言来源结果的回归 | **证据缺口** | 已补真实 SWAS partial 来源回归 |
| [I10-R11](#i10-r11) | EdgeOne 确认成功/未知/未执行的日志没有自动捕获回归 | **证据缺口** | 已补 success/unknown/保存失败日志捕获 |
| [I10-R12](#i10-r12) | Step 14 `disabled` 断言只检查属性存在；Dashboard/EdgeOne 卸载清理无自动回归 | **部分存在** | 已收紧为值断言并补卸载用例 |
| [I10-R13](#i10-r13) | Step 13 深链/历史仅用响应式路由代理，未使用真实 `vue-router` 历史；有隔离浏览器证据但未固化 | **部分存在** | 已补真实 memory router 深链/历史回归 |

R08～R13 均已在 2026-11-14 的单一本地提交中处理，实施细节见 §7；下方 §4 保留复核时的原因、代码位置与方案。

### 2.3 原证据/文档偏差的处置

| 原偏差 | 复核结果 |
|---|---|
| Step 11 缺少释放前缀精确匹配、裁决 C 内存保护、过渡读取失败 DNS 继续、A/B 共用网段的判别性回归 | **已补齐并复核通过**：`TestStep11ReleaseRequiresCanonicalPrefixMatch`、`TestStep11StorageFailureStillUsesMemoryProtectionForCleanup`、`TestStep11TransitionReadFailureKeepsDNSWrites`、`TestStep11SharedPrefixAcrossVersions`。 |
| Step 12 未覆盖查询不完整；仅 EdgeOne 用例只断言 unit | **已补齐**：`TestStep12DryRunIncompleteQuery`、`TestStep12DryRunEdgeOneOnlyResults`、`TestStep12DryRunEdgeOneOnlyNoDNSRules`。 |
| Step 15 未测双源单侧失败归因 | **已补齐**：`TestStep15SourceOutcomeAttributedPerSource`、`TestStep15SourceOutcomeEdgeFailureDoesNotPolluteDNS`。 |
| AGENTS 设置页可见键写成 10 个 | **已订正为 13 个**（新增 `dns_enabled`、`edgeone_tag`、`edgeone_interval`）。 |
| Build8 将 `b3ccb6b` 写成未提交 | **已订正**；但二次核验批次又出现同类漂移，见 I10-R09。 |
| `TestStep11TransitionSQLiteRealStore` 名不副实 | **已重命名**为 `TestStep11ResidualProtectionDoesNotFallBackToPending`；真实 SQLite 重启恢复由 `TestStep11RestartRestoresProtectionFromSQLite` 覆盖。 |
| 提交说明「16 用例」与顶层用例数不符 | 历史提交说明未改写；Build8 已按实际口径记录 Step 11/12/15 顶层用例为 20/12/9，不再使用「16 用例」。 |

## 3. 已关闭事项的复核证据

<a id="i10-r01"></a>

### I10-R01 双开未观察预检被 health 误报

- **原原因：** 任意配置发布时 `applyStateLocked` 清除 `preflightSet`；`preflightStatusLocked` 把「尚未预检」表示成 `data_unavailable` 且 `complete=false`；health 只要看到 `complete=false` 就追加「DNS/EdgeOne 数据不足」，配置保存或启动窗口可因此产生 operational 503、WARN、边沿通知与 Push DOWN。
- **当前代码：** `syncer/syncer.go:980-986` 的双开未观察分支保留 `ObservedAt=nil` 并单独暴露持久化 `KnownConflict`；`internal/health/health.go:197-204` 只在 `ObservedAt != nil` 时把 `complete=false` 解释为数据不足；`webui/frontend/src/views/Dashboard.vue:73-77` 对未观察显示中性提示。配置发布清空预检的路径仍在 `syncer/syncer.go:238-243`，但不再制造伪原因。
- **复核证据：** `TestEvaluateUnobservedPreflightIsNeutral`、`TestEvaluateKnownConflictWithoutObservationIsVisible`、`TestEvaluateObservedSingleSideIncompleteStillReports` 通过。独立探针在双开成功轮后调用 `ApplyState` 发布新状态，`Status().Preflight` 为 `state=data_unavailable`、`DNSComplete=false`、`EdgeOneComplete=false`、`ObservedAt=nil`，与 health 的「未观察」分支一致。
- **结论：** 原缺陷已关闭；未观察到回归。

<a id="i10-r02"></a>

### I10-R02 仅 EdgeOne 时 DryRun results 错乱

- **原原因：** `DryRun` 无条件读取 `state.Config.DomainRules`；DNS 关闭时这些规则未解析，`PlanTarget` 产生伪 `dns_empty`。目标只有 EdgeOne 站点时 `domains` 为空，前端据此显示「无适用规则/正式同步将跳过」。
- **当前代码：** `dryRunTargetRules`（`syncer/syncer.go:1329-1334`）按 `DNSFeatureEnabled` 返回空；`targetGroups` 也只并入启用来源的规则（`syncer/target_group.go:23-70`）。`DryRunResult.EdgeOneSites` 在 `syncer/syncer.go:1057-1060` 定义，并在各 result 构造路径填充；前端 `hasTargetPlan` 以 `domains` 或 `edgeone_sites` 判断适用来源（`webui/frontend/src/components/DryRunResults.vue:27-29`、`:261-271`）。
- **复核证据：** `TestStep12DryRunEdgeOneOnlyResults` 断言 `domains=[]`、`dns_errors=[]`、`edgeone_sites=[zone-a]`、`to_add=1`；`TestStep12DryRunEdgeOneOnlyNoDNSRules` 覆盖没有 DNS 规则的目标。独立探针在同一场景得到相同结果。
- **结论：** 原缺陷已关闭；仅 EdgeOne 的 results 与前端展示不再自相矛盾。

<a id="i10-r03"></a>

### I10-R03 两侧不足仍提示「单侧继续」

- **原原因：** Dashboard/DryRunResults 只判断 `dns_complete`/`edgeone_complete` 中哪一侧为 false，未区分「两侧均不足」；实际 admission 会阻断两侧。
- **当前代码：** `Dashboard.vue:73-82` 与 `DryRunResults.vue:57-62` 均先处理两侧不足，再处理单侧不足；Dashboard 另有未观察分支。
- **复核证据：** `B8S14 仪表盘预检两侧均不足时阻断两侧`、`B8S14 DryRun 双侧不足提示阻断两侧`、`B8S14 仪表盘未观察预检不误报数据不足` 通过；文案不再出现「单侧继续」。
- **结论：** 原缺陷已关闭。

<a id="i10-r04"></a>

### I10-R04 `protected_cidrs` 语义不一致

- **原原因：** 双开 `storePreflight` 传入 `edgePrefixInputs`（含全部当前/待更新前缀），单源路径提前返回不复制；与 DryRun/站点页的「过渡保护」语义不一致。
- **当前代码：** 双开、仅 EdgeOne 均以 `transitionProtectedInputs(edgeData)` 写入 `address_preflight.protected_cidrs`（`syncer/edgeone_round.go:866`、`:889`；转换函数 `syncer/syncer.go:1310-1323`）；单源状态从 `s.preflightProtected` 回填（`syncer/syncer.go:962-969`）。预检参与重叠判定的仍是全部当前/待更新前缀（`edgePrefixInputs`，`edgeone_round.go:438`），两者不再混用。
- **复核证据：** `TestStep11PreflightProtectedCIDRsTransitionOnly` 对仅 EdgeOne/双开分别断言精确集合；`TestBuild8RetainsProtectionWithoutConfirmation`（`syncer/build8_step11_15_test.go:17-42`）断言 DryRun `preflight.protected_cidrs` 与过渡保护一致。
- **结论：** 原缺陷已关闭。

<a id="i10-r05"></a>

### I10-R05 Dashboard 站点状态查询失败被显示为「暂无记录」

- **原原因：** `fetchEdgeStatus` 的 `catch` 为空，失败时保留旧值或 null，模板只按「无 `edgeStatus`」显示「暂无站点运行状态记录」。
- **当前代码：** 新增 `edgeStatusError`（`webui/frontend/src/views/Dashboard.vue:22`），失败路径设置错误（`:176-179`），成功清除（`:174`）；模板先显示错误并在有旧数据时注明「以下为最近一次成功记录」（`:341-345`），「暂无记录」只在无错误时出现（`:345`）。
- **复核证据：** `B8S14 仪表盘站点状态读取失败明确提示，不冒充暂无记录` 通过；`B8S14 仪表盘 EdgeOne 状态乱序响应只接受最新请求` 保留。
- **结论：** 原缺陷已关闭。

<a id="i10-r06"></a>

### I10-R06 DryRun 站点版本/完整性矛盾

- **原原因：** `dryrun_edgeone.go` 只在 `complete` 时填版本；`syncer.go` 的 unit `DataComplete` 只查 unavailable/readErr，不查缺地址族，导致站点不完整但执行单元「完整」。
- **当前代码：** `dryRunSites`（`syncer/dryrun_edgeone.go:251-300`）用独立 `knownVersion` 回显当前/待更新版本；`complete` 同时要求快照有效、过渡可读、地址族满足（`:266-292`）。`edgePreviewComplete`（`syncer/syncer.go:1350-1365`）对 unit 使用同一组条件。
- **复核证据：** `TestStep12DryRunSiteVersionAndCompleteness` 断言缺 IPv6 时 unit/site 均不完整、`CurrentVersion` 仍为 `v1`、原因为 `address_family_missing`；`TestStep12DryRunIncompleteQuery` 覆盖查询失败。
- **结论：** 原缺陷已关闭。

<a id="i10-r07"></a>

### I10-R07 来源级 `last_outcome` 使用整轮结果

- **原原因：** `edgeone_round.go` 的两侧都调用 `lastRoundOutcome()`，双源同轮单侧失败会把另一侧也标记为 failed，health 误报两侧失败。
- **当前代码：** `sourceRoundAccumulator`（`syncer/syncer.go:1581-1600`）按物理目标实际参与来源记录结果，失败 > 部分 > 成功；`runRoundInput` 在轮末写回（`:1680-1701`）；`sourceOutcome`（`syncer/edgeone_round.go:941-985`）先处理准入阻断，再读取本来源的归因结果，未参与来源为 `idle`。
- **复核证据：** `TestStep15SourceOutcomeAttributedPerSource`（仅 DNS-only 目标失败，EdgeOne 仍 success）与 `TestStep15SourceOutcomeEdgeFailureDoesNotPolluteDNS`（反向）通过；`TestStep15MergeSourceOutcomePriority` 验证优先级。
- **结论：** 原缺陷已关闭；共享目标失败时两侧同记失败由代码路径保证。

## 4. 复核时仍存在的问题与后续实施

<a id="i10-r08"></a>

### I10-R08 单源 EdgeOne 数据不足的健康原因文案错误（已修复）

- **现象：** 仅启用 EdgeOne，且已观察的预检 `edgeone_complete=false` 时，`/api/health/operational` 的 `reasons` 为 `["EdgeOne 数据不足（单侧继续）"]`。当前没有 DNS 侧在运行，「单侧继续」不成立；实际上 EdgeOne 写入已被阻断。
- **原因：** `internal/health/health.go:201-203` 的 `if status.EdgeOne.Enabled && !preflight.EdgeOneComplete` 没有 `dual` 门控；稳定原因常量 `ReasonEdgeOneIncomplete` 固定为「单侧继续」（`internal/health/health.go:42`）。双开单侧不足时该文案正确，单源时语义错误。
- **代码位置：** `internal/health/health.go:42`、`:197-204`；单源状态构造见 `syncer/syncer.go:953-980`（单源分支仍会设置 `EdgeOneComplete` 与 `ObservedAt`）。
- **复核证据：** 在 `/tmp` 独立 worktree 的探针中构造 `DNS.Enabled=false`、`EdgeOne.Enabled=true`、`EdgeOneComplete=false`、`ObservedAt` 非空，`Evaluate` 返回 `healthy=false reasons=["EdgeOne 数据不足（单侧继续）"]`。这是本次复核唯一确认的生产语义问题，健康结论本身（unhealthy）正确，错的是可读原因。
- **影响：** 运维提示、日志和告警文案会暗示存在可继续的另一侧；不影响实际写入安全（EdgeOne 仍按 admission 停止）。
- **实施（2026-11-14，用户选择 A）：** `internal/health/health.go` 新增 `ReasonEdgeOneIncompleteSingle = "EdgeOne 数据不足（来源已阻断）"`；双开仍使用 `ReasonEdgeOneIncomplete`，单源使用新原因。`internal/health/health_test.go` 增加“单源使用来源阻断”“双开保留单侧继续”两条对照。AGENTS/Design6 的原因清单同步。定向与整包 `go test ./internal/health ./syncer -race -count=1` 通过。
- **优先级：** 低。已完成，不单独发布。

<a id="i10-r09"></a>

### I10-R09 Build8/AGENTS 的提交状态漂移（已订正）

- **现象：** `Build8.md:62` 仍写「二次核验修复已本地实施」；`Build8.md:788-805` 的「二次核验修复」段仍写「本轮改动保留在工作树」；`AGENTS.md:14` 仍写「本轮从干净的 `beta / ee20e08` 实施二次核验修复」。但当前 `HEAD` 已是 `e94c8fc`，复核开始时工作树/暂存区干净，`origin/beta` 为 `ee20e08`，上述修复已经提交。
- **原因：** `e94c8fc` 的提交说明与 Build8/AGENTS 回写不是同一批次；文档停留在「待提交/工作树」状态，且进度段没有列出 `e94c8fc`。此外提交标题写「三次核验修复」而 Build8 段名写「二次核验修复」，`git show --stat` 实际为 23 个文件而提交说明写「共 30+ 文件」，属于同批次的命名/文件数元数据偏差。
- **代码/文档位置：** `Build8.md:60-62`、`:788-805`；`AGENTS.md:14`。
- **复核证据：** `git log -1 --format='%H %s'` 为 `e94c8fc ...`；`git status --porcelain=v1` 无输出；`git rev-parse origin/beta` 为 `ee20e08`。`Build8.md:802` 的 186 项前端测试与本次实际 `npm --prefix webui/frontend test` 的 186/186 一致，测试数字本身不是问题。
- **影响：** 审计者可能误判二次核验修复尚未落库，或低估当前提交边界；不影响运行行为。
- **实施（2026-11-14，用户选择保留「二次核验修复」）：** `Build8.md:62`、`:788`、`:805` 已写明 `e94c8fc`、23 个文件、本地 ahead 1/未推送，并注明提交标题按实施批次称「三次核验修复」；`AGENTS.md:14`、`Design6.md` 二次核验段已补提交 hash 与命名说明。未改写历史提交说明。本文件 §7 记录本批单提交。
- **优先级：** 低（文档）。已订正，避免 Step 16 前证据链歧义。

<a id="i10-r10"></a>

### I10-R10 来源级 `partial` 缺少真实轮次回归（已补齐）

- **现象：** `SourceOutcomePartial` 目前只有两类证据：`syncer/step15_source_test.go:218-228` 的 `mergeSourceOutcome` 纯函数用例，以及 `internal/health/health_test.go:572-586` 的手工 `SourceStatus{LastOutcome: partial}`。没有一个真实 `runScheduledRound` 产生 `TargetPartial` 并断言对应来源 `LastOutcome=partial` 的用例。
- **原因：** 原会话已指出「证据声称 partial 可见但无 partial 来源用例」；`ee20e08` 补了归因和 health 合成用例，但没有补真实 partial 目标链。生产代码的 `sourceRoundAccumulator.record` 会接收 `res.outcome`，按代码路径应能正确映射，但缺判别性证据。
- **代码位置：** `syncer/syncer.go:1587-1600`（`record`）、`:1680-1701`（轮末写回）；`syncer/step15_source_test.go:218-228`（现有纯函数用例）。
- **影响：** 测试证据缺口；若未来 `syncTargetExec` 的 partial 路径变化，现有测试不一定能发现来源级回归。
- **实施（2026-11-14）：** 新增 `TestStep15SourceOutcomePartialAttributed`，以 SWAS 目标加一条不支持的 `DROP` 规则触发真实 `runScheduledRound` 的 `TargetPartial`，断言 `DNS.LastOutcome=partial`、整轮 `partial/skipped=1`。保留 `mergeSourceOutcome` 纯函数对照。定向与整包 `go test ./syncer -race` 通过。
- **优先级：** 低。已补齐。

<a id="i10-r11"></a>

### I10-R11 EdgeOne 确认日志缺少自动回归（已补齐）

- **现象：** Step 15 证据声称「确认成功/未知/失败写安全日志」，但 `syncer` 测试没有任何 `slog` 捕获；`edgeone_confirm.go` 的日志分支（成功 `:306`、未知 `:318`、云端已最新 `:279`、未发送撤销失败 `:264-270`）只有行为断言，没有日志内容/敏感字段断言。
- **原因：** 现有测试使用替身验证确认请求次数与 SQLite 状态，未注入 logger；全局 `slog` 在并行测试中不易捕获，故一直没有补。
- **代码位置：** `syncer/edgeone_confirm.go:264-318`；测试 `syncer/step10_confirm_test.go`、`syncer/build8_followup_test.go`。
- **影响：** 证据缺口；日志文案或敏感字段回退不会被自动发现。生产日志路径本身未发现泄露（代码只记录 `ZoneId`、版本、RequestId 等固定字段）。
- **实施（2026-11-14）：** 新增 `TestStep10ConfirmationLogs`，串行捕获 success / unknown / accepted-save-failure 三条路径，断言固定文案与 `request_id` 存在，unknown 不回显底层错误原文；测试结束恢复默认 logger。定向 `go test ./syncer -race` 通过。`syncer` 当前无并行测试，后续若引入并行需改为注入 logger。
- **优先级：** 低。已补齐。

<a id="i10-r12"></a>

### I10-R12 Step 14 `disabled` 断言与卸载清理（已补齐）

- **现象：** `webui/frontend/tests/step14-ui.test.mjs:171` 只断言 `buttons.some(n => n.props && 'disabled' in n.props)`，即「存在 disabled 属性」，未断言其值为 `true`；总暂停时按钮是否真正禁用可能漏判。`Dashboard.vue:252-255` 与 `EdgeOne.vue:16-18` 有 `onUnmounted` 清理定时器，但没有卸载后不再请求/不再更新状态的自动回归。
- **原因：** 原会话已指出 `disabled` 断言过宽、Step 14 未测卸载清理；`ee20e08`/`e94c8fc` 补了两侧不足、查询失败、恢复等用例，但没有收紧该断言，也没有卸载用例。
- **代码位置：** `webui/frontend/tests/step14-ui.test.mjs:161-173`；`webui/frontend/src/views/Dashboard.vue:252-255`；`webui/frontend/src/views/EdgeOne.vue:16-18`。
- **影响：** 测试判别力不足；卸载清理目前靠代码审查，缺回归。
- **实施（2026-11-14）：** 将断言改为检查 `disabled` 的值（Vue 模板静态布尔 prop 在此测试夹具中呈现为 `''`，因此接受 `''` 或 `true`，不能只写 `=== true`）；新增 `mount → 挂起请求 → unmount → 晚到响应` 用例，断言轮询定时器清零且晚到响应不更新状态。`step14-ui` 23/23 通过。
- **优先级：** 低。已补齐。

<a id="i10-r13"></a>

### I10-R13 Step 13 真实 router/历史证据（已补齐）

- **现象：** `webui/frontend/tests/edgeone-ui.test.mjs:29-40` 用 `vue.reactive({ path: '/edgeone' })` 模拟路由；`tests/helpers/compiled-component.mjs:24` 把 `vue-router` 替换为 `useRoute`/`useRouter` 桩。没有使用真实 `createRouter` + `createMemoryHistory` 的深链/回退回归。Build8 的二次核验段记录了隔离副本中的真实 Chrome 验证（深链、侧栏高亮、历史导航），但未固化为仓内测试。
- **原因：** 前端组件测试框架为避免真实路由依赖使用代理；真实浏览器证据留在研究阶段，没有转换为自动回归。
- **代码位置：** `webui/frontend/tests/edgeone-ui.test.mjs:29-40`；`webui/frontend/tests/helpers/compiled-component.mjs:24`；文档证据见 `Build8.md:790`。
- **影响：** 自动化证据缺口；不影响当前路由实现本身。
- **实施（2026-11-14，用户选择真实 memory router）：** 测试 helper 增加可选 `vueRouter` 桥接（`afterEach` 同步 `useRoute` 路径），新增测试用真实 `createRouter(createMemoryHistory())` 覆盖 `/edgeone` 深链、`push('/rules')` 与 `back()` 的菜单高亮。`edgeone-ui` 11/11 通过。该测试使用真实导航/历史，但不是完整 `app.use(router)` 挂载，边界在测试注释与 Build8 中记录。
- **优先级：** 低。已补齐。

## 5. 复核与实施验证

复核时命令（`e94c8fc`）如下，`node --test` 在 `webui/frontend` 下执行，环境为 Go 1.27.3 darwin/arm64、Node v26.7.0（本机）。

| 命令 | 结果 |
|---|---|
| `go test ./syncer -run '^(TestStep11\|TestStep12\|TestStep15)' -race -count=1 -timeout=20m` | `ok syncer 3.803s` |
| `go test ./internal/health -run '^(TestEvaluateUnobservedPreflightIsNeutral\|TestEvaluateKnownConflictWithoutObservationIsVisible\|TestEvaluateObservedSingleSideIncompleteStillReports\|TestEvaluateSourceOutcomePartialIsVisible)$' -race -count=1 -timeout=10m` | `ok internal/health 3.432s` |
| `cd webui/frontend && node --test --test-timeout=30000 tests/edgeone-ui.test.mjs tests/step14-ui.test.mjs` | 32/32 pass（10 + 22） |
| `npm --prefix webui/frontend test` | 186/186 pass；fail/cancelled/skipped 均为 0 |
| `go test ./... -race -count=1 -timeout=30m` | 12 个包全部 ok（root/app/config/dns/health/portconv/tag/notifier/provider/syncer/webui/webui/api） |
| `go vet ./...` | 退出码 0，无输出 |

实施后（本批单提交）：

| 命令 | 结果 |
|---|---|
| `go test ./internal/health ./syncer -race -count=1 -timeout=20m` | `ok internal/health 45.929s`、`ok syncer 32.592s` |
| `go test ./... -race -count=1 -timeout=30m` | 12 个包全部 ok（root 19.220s、config 37.522s、health 46.513s、syncer 33.902s、webui/api 45.382s 等） |
| `npm --prefix webui/frontend test` | 188/188 pass；fail/cancelled/skipped 均为 0 |
| `cd webui/frontend && node --test --test-timeout=30000 tests/edgeone-ui.test.mjs tests/step14-ui.test.mjs` | 34/34 pass（11 + 23） |
| `go vet ./...` | 退出码 0，无输出 |
| `gofmt -l`（4 个受影响 Go 文件）与 `git diff --check` | 均无输出 |

**独立探针（复核阶段 `/tmp` worktree，同一 `e94c8fc`，复核后已删除）：**

- `TestReverifyPublishClearsPreflightToUnobserved`：双开成功轮后发布新状态，`Status().Preflight` 为 `data_unavailable`、两侧 `complete=false`、`ObservedAt=nil`；health 不再获得数据不足输入。
- `TestReverifyDryRunEdgeOneOnlyResults`：仅 EdgeOne 且配置了停用 DNS 规则时，`results[0].Domains=[]`、`DNSErrors=[]`、`EdgeOneSites=[zone-a]`、`ToAdd=1`。
- `TestReverifySingleSourceEdgeOneIncompleteReasonText`：仅 EdgeOne、`EdgeOneComplete=false` 时，复核阶段 health 返回 `healthy=false reasons=["EdgeOne 数据不足（单侧继续）"]`，即本文件 I10-R08；实施后已改为来源阻断原因。

**未执行：** `make all`、`npm audit`、真实腾讯云 TEO/确认/生效、真实 DNS、产品浏览器人工回归、Linux/Docker/compose、当前 revision 远端 CI/GHCR、推送/发布。上述本地结果不外推为外部验收或长期稳定。

## 6. 边界、接受项与后续顺序

- **已确认不构成当前缺陷的边界：**
  - 真实待更新窗口、真实确认/生效、真实 DNS 与云快照差异不在本地替身覆盖范围；Step 17 保留。
  - DryRun 的 CVM 过渡期峰值以 S0 `Ingress` 条目为下界；`rules_read` 与 `current_rule_count` 当前同取 S0 规则数，是预览口径。
  - 单源运行保留历史冲突提示，不把历史冲突当作当前双开阻断；这是 Design6 的既定语义。
  - 原提交说明的「16 用例」「30+ 文件」等历史元数据不重写提交，由文档订正口径。
- **实施范围（2026-11-14，单提交）：** 生产仅 `internal/health/health.go` 的原因选择；其余为 `internal/health/health_test.go`、`syncer/step10_confirm_test.go`、`syncer/step15_source_test.go`、`webui/frontend/tests/step14-ui.test.mjs`、`webui/frontend/tests/edgeone-ui.test.mjs`、`webui/frontend/tests/helpers/compiled-component.mjs` 与本文件、Build8/AGENTS/Design6。未改预检、写入、确认、调度、API/schema、配置包或依赖。
- **后续：** 按用户选择完成单一本地提交，不推送；Step 16 本地端到端与 Step 17 真实云/发布仍待执行。R13 的 helper 桥接测试使用真实 `createRouter` + memory history，但不是完整 `app.use(router)` 挂载；R11 的日志捕获依赖 syncer 当前无并行测试，后续引入并行测试时需改为注入 logger。这两点作为已知测试边界记录，不改变生产合同。
- **复查入口：** 本文件是复核与实施台账；旧 Issue9 已归档，不重新打开 I9 编号。若后续再发现问题，在本文件追加新编号与实施补记，不覆盖本次复核事实。

## 7. 实施补记（2026-11-14，单提交）

用户在同一会话确认：R08 采用新增独立原因；R10～R13 全部实施；R13 使用真实 `createRouter` + memory history；R09 保留「二次核验修复」并注明提交标题按实施批次称「三次」；最终单一提交、不推送。

**生产变更（仅 1 处）**

- [internal/health/health.go](internal/health/health.go)：新增 `ReasonEdgeOneIncompleteSingle = "EdgeOne 数据不足（来源已阻断）"`；双开保留 `ReasonEdgeOneIncomplete`，仅 EdgeOne 启用时使用新原因。健康结论仍为 unhealthy，写入准入不变。
- [internal/health/health_test.go](internal/health/health_test.go)：新增单源/双开两条原因对照。

**测试补齐**

- [syncer/step15_source_test.go](syncer/step15_source_test.go)：`TestStep15SourceOutcomePartialAttributed`，SWAS 目标 + 不支持 `DROP` 规则触发真实 `TargetPartial`，断言来源 `partial` 与整轮 `partial/skipped=1`。
- [syncer/step10_confirm_test.go](syncer/step10_confirm_test.go)：`TestStep10ConfirmationLogs`，捕获 success / unknown / accepted-save-failure，断言固定文案、`request_id` 与不泄露底层错误原文。
- [webui/frontend/tests/step14-ui.test.mjs](webui/frontend/tests/step14-ui.test.mjs)：`disabled` 断言改为值检查（兼容编译后的 `''`/`true`）；新增卸载清理与晚到响应隔离用例。
- [webui/frontend/tests/edgeone-ui.test.mjs](webui/frontend/tests/edgeone-ui.test.mjs)、[compiled-component.mjs](webui/frontend/tests/helpers/compiled-component.mjs)：helper 增加可选 `vueRouter` 桥接；新增真实 memory router 深链/`push`/`back` 菜单高亮用例。

**文档**

- [Build8.md](Build8.md)：订正 `e94c8fc` 提交状态、23 个文件、本地 ahead 1/未推送与「二次/三次」命名口径；新增本批复核修复段。
- [AGENTS.md](AGENTS.md)、[Design6.md](Design6.md)：补 `e94c8fc` 状态、新健康原因与单源语义；Design6 新增本批复核修复段。
- 本文件：更新 R08～R13 状态、实施说明与验证结果。

**验证**

- `go test ./internal/health ./syncer -race -count=1 -timeout=20m`：两包通过。
- `go test ./... -race -count=1 -timeout=30m`：12 包全部通过。
- `npm --prefix webui/frontend test`：188/188；`edgeone-ui` + `step14-ui` 定向 34/34。
- `go vet ./...`：退出码 0；受影响 Go 文件 `gofmt -l` 与 `git diff --check` 无输出。

**提交与边界**

- 本批为单一本地提交，不推送；提交 hash 见 `git log -1`。未执行 `make all`、`npm audit`、真实云/DNS、真实浏览器人工回归、Linux/Docker、远端 CI/GHCR 与发布。
- R13 测试使用真实 memory history，但 `useRoute` 由 helper 的 `afterEach` 桥接，不是完整 `app.use(router)` 挂载；R11 的全局 `slog` 捕获依赖 `syncer` 当前无并行测试。两点为测试边界，不改变生产合同。
