# Issue6.md — FWAlizer 后续问题与修复合同

> **文档定位：** 本文记录 Build6 完成后的问题、修复合同与历史实施证据；当前条目状态见 §2.1，后续目标级同步以 [Issue7.md](./Issue7.md) 为入口。它只描述问题、已确认的产品语义、建议实施边界和验收要求；除明确标记为“已修复”的条目外，不代表代码已经修改或外部链路已经验收。
>
> **历史基线（2026-09-27 批次）：** 2026-09-27，分支 `main`，全文规范化提交 HEAD `0d9e2d678f86520a5e688d4ad1b57a6483968d18`（其父为整理前 HEAD `1392ab9b675d0cf79a04f5f4a0f2f5cd966dec36`），相对 `origin/main` ahead 2；`git diff 1fafb172307652912a621eaebf4bf942cbd2c33f..0d9e2d678f86520a5e688d4ad1b57a6483968d18` 只包含 `Issue6.md`，源码零变化，因此该批次下列源码结论与最终合同适用于当时实现；当前同步链路已由 Issue7 替代，见下方 P3-07 更正与 §7.9。该批次裁决并入时，工作树唯一的未提交改动是本文（不涉及任何源码、测试、依赖或配置）。
>
> **提交对账与历史阅读说明（2026-10-03 P3-18）：** 上述基线、§6 的复核与授权结论均描述 2026-09-27 实施前状态。`ea71f5f` 只承载文档裁决，后继 `b38678a` 承载批次 1～8 的 52 文件实现/测试/文档改动；§7.2/§7.7 的 Dashboard 与错误路径计数后续提交为 `043ac36`，未启动生命周期修复提交为 `c95b139`。这些提交均为本轮实施前 HEAD `5860cef` 的祖先。历史“当前 HEAD/本轮/尚未提交”只指各批次当时，不能替代 §2.1 与后续补记，也不升级外部验收。
>
> **历史授权边界（2026-09-27 批次）：** 2026-09-27 已完成全量只读复核与用户裁决；本次只把裁决结果并入本文，不修改设计、源码、测试、依赖、配置或其他文档，也不开始任何修复。§2.2 各批次的可实施性由用户后续一次性授权决定。
>
> **Build7（2026-09-28）：** 当前构建方案为 [Build7.md](./Build7.md)（告警与运行健康）；Build7 **Step 0～6 已全部实施完成，Step 7（核验缺陷修复）亦已完成**（逐步证据见 Build7 第八节 Step 7 与 §十一 v1.3；本轮核验发现与处置见本文 §7.8）。§6.5 第 10 项（SMTP 多收件人未逐项 Trim）已由 Build7 合同显式接管并**随 Build7 Step 3 实施完成**：`notifier/email.go` 对 `To` 逐项 `TrimSpace` 并跳过空项，判别性用例见 `notifier/email_test.go:TestEmailRecipientsAreTrimmed`；该 Build7 批次未接管 §6.5 的其余候选；后续独立授权的处理见 §7 及审计报告各 finding，不将历史未授权范围解释为永久禁止后续授权。

> **P3-07 当前链路更正（2026-10-02）：** 本文上述基线与下文 A1/A11/A18、§6.5、§7.2 的 `syncDomain → retrySync` / `retrySyncDetailed` 和逐域计数均为 Build6 当时的历史实现。当前正式同步由 `Run → syncAll → runRound → syncTarget → runTargetAttempt / runTargetCleanup` 执行；Issue7 已改为目标级先增后验。P3-07 按本次独立授权方案 B 删除旧函数并迁移有效回归，不重写过去的实施事实；当前入口与迁移对照见 §7.9，当前强要求继续以 AGENTS 为准。

## 一、使用规则

1. `AGENTS.md` 是唯一强要求；`Design5.md`、`Build6.md`、`Issue5.md` 和本文属于设计、构建与问题记录。若彼此或与当前代码冲突，必须向用户说明并等待决策。
2. 每个问题的细节与修复方案都记录在同一条目中；不得只读总表或批次表后直接施工。
3. 当前没有待用户裁决的产品语义：2026-09-27 复核提出的全部冲突已由用户逐项裁决（见 §六.4），裁决后的口径即为固定口径，实施者不得重新选择；只有出现**新的**实质冲突时才使用提问工具，并附推荐选项与理由。
4. 不得把源码推理、隔离复现、自动测试、真实云、真实 SMTP/收件箱、真实 Webhook、浏览器、Docker 或生产事故互相替代。
5. A5、A6 原问题、A7、A8、A10 已修复，只保留防回归合同；A20 是与 A6 相邻但独立的 Stop 缺陷。
6. 本文不授权删除辅助构建代码、辅助验收代码、判别性测试夹具或已经固定的契约。低风险清理也必须在明确范围内实施：批次 8 的删除范围仅限 §2.2 与 §六.4 F9 列出的接口成员与零引用死代码，实施前必须逐项重新证明无生产调用。

## 二、状态与实施顺序

### 2.1 当前状态

| 状态 | 条目 |
|---|---|
| 已在批次 1～8 实施完成（含判别性回归） | A1、A2、A3、A4、A9、A11、A12、A13、A14、A15、A16、A17、A18、A19、A20 |
| 已修复，保留回归（此前由 Step 7 完成，本轮仅防回归） | A5、A6 原问题、A7、A8、A10 |
| 待用户决策 | 无 |
| 明确不授权实施（后续候选，见 §六.5） | 14 项（删除侧跳过计数、反向「写了却报 0」、`buildDesired` 静默丢弃、dangling 引用、SQLite 其余缺口等） |

上表在 2026-09-27 全量只读复核基线（A1、A2、A4、A9、A11、A12、A13、A14、A15、A16、A17、A20 确认存在；A3、A18、A19 为产品决策已定待实施；A5、A6 原问题、A7、A8、A10 只保留防回归合同）之上，记录了按用户一次性授权执行 §2.2 全部批次后的最终状态。**每个条目的实施证据（文件、用例名、红灯证明、门禁、未执行边界）都追加在该条目内部**，未拆到无关章节。

**未验证/已免除的外部边界（不得写成已通过）：** 真实阿里云弱网与 SWAS 云上观察、真实 SMTP/收件箱、真实 Webhook、Dashboard 与 Dry Run 新展示的浏览器人工验收、真实 WAN/反向代理异常或经典故障型半开 TCP、真实生产 SQLite 与旧库迁移路径、`GOOS=windows` 运行验收（用户已决定移除支持，仅以构建失败为证据）、远端 GitHub Actions 对本次改动的验证（本次未推送）。A15 的 loopback TCP“客户端保持连接但停止读取”已由当前本地判别性测试覆盖，见该条目第二阶段实施记录。

### 2.2 固定实施顺序

| 顺序 | 批次 | 条目 | 依赖与停止条件 |
|---|---|---|---|
| 1 | 批次 1 | A1 + A12 | 先使阿里云调用有界，再把结构化超时纳入重试；两项同批交付 |
| 2 | 批次 3 | A20 + A16；复核 A6/A7 边界 | stop 是**所有**新轮次的硬门控（Run 入口启动轮、ticker、trigger、false→true 恢复轮共 4 处）；只阻止 Stop 后的新轮次，不中断当前轮；必须有确定性竞态回归 |
| 3 | 批次 2 | A2 | 每个告警渠道在途上限 4；满载丢弃最新并 WARN；限流器由 `AlertManager` 按渠道持有，使上限在热重载后仍成立；不得限流整个 EventBus |
| 4 | 批次 4 | A3 + A18 + A13 + A15 **+ A11 的 Provider 接口部分（`CreateRules` 返回 `{Written, Skipped}`）** | 依赖批次 1；一次固定同步健康、轮次汇总与 SSE 口径；A11 接口前移是对 §2.2 的**用户已裁决调整**（理由见 §六.4 F3），使 A18 的 `skipped`/`partial` 在批次 4 即可端到端判别 |
| 5 | 批次 5 | A4 + A9 + A14 + A17 | 同文件不等于同一改动；各条目独立审查，共用整包回归 |
| 6 | 批次 6 | A11 收尾（Dry Run 合同、跳过原因、事件/SSE/实时日志展示、前端展示与端到端用例） | 接口部分已在批次 4 交付；本批只完成 Dry Run 与展示收口，并按 §六.4 F3 在 A11 条目内连续记录两部分证据 |
| 7 | 批次 7 | A19 + A8 文档收口 | 独立平台/依赖变更；本批涉及 `go.mod`、`go.sum`、AGENTS 的 A8 措辞与平台约束句、README 平台与发布说明、pidfile 单实例回归（§六.4 F9） |
| 8 | 批次 8 | 低风险清理：`webui/api` 的 `Syncer` 接口成员 `Pause`/`Resume`（**`Runtime` 依用户 2026-09-27 裁决保留**）与 7 项零引用死代码 | 不捆绑新功能；开始前重新证明无生产调用；不得删除实现、辅助构建、验收代码或测试夹具（§六.4 F9） |

上一批验收前不得自动进入下一批。批次号沿用已确认合同，因此表中顺序有意为 `1 → 3 → 2 → 4 → 5 → 6 → 7 → 8`；批次编号未变，只有批次 4/6 的内容边界按 §六.4 F3 的用户裁决调整并在两处条目内各自留证。

## 三、待处理问题与最终修复合同

以下条目按实施顺序排列，不按编号排列。

### 批次 1：有界云请求与可靠重试

#### A1｜高｜阿里云 SDK 请求缺少应用层超时

- **状态与判定：** 确认存在，置信度高。正式 SWAS/ECS Provider 和两条扫描构造路径均未配置有限 deadline。
- **当前证据：** `provider/ali_swas.go:33-37`、`provider/ali_ecs.go:32-36`、`provider/scan.go:133-137`（`scanAliSWAS`）、`provider/scan.go:181-185`（`scanAliECS`）创建的 `openapi.Config` 只设置凭据与 Endpoint。当前依赖在零值下使连接、响应头和 HTTP client 超时均为 0：`darabonba-openapi/v2` 的 `ReadTimeout/ConnectTimeout`（`client.go:34-35`、`:127-128`）经 `tea/dara` 生效为 `http.Client.Timeout=Connect+Read`（`core.go:354`）、`Transport.ResponseHeaderTimeout=Read`（`core.go:535`）、`net.Dialer.Timeout=Connect`（`core.go:736-742`），三者全 0 即完全无界；`tea/dara/retry.go:274-281` 在未设 `RetryOptions` 时只尝试一次。SWAS v3 SDK 没有 `WithContext` 变体（`ListFirewallRules`/`CreateFirewallRules`/`DeleteFirewallRules`/`ListInstances` 均无 ctx 版本），ECS v7 虽有但生产未使用。腾讯 SDK 默认单次请求 60s（`profile/http_profile.go:45`），不属于本项。
- **调用链与影响（Build6 当时链路，当前入口见 §7.9）：** 正式同步（`run.go:131` → `syncer.go:457` → `syncDomain` → `retrySync`）、Dry Run（`syncer.go:404`）、连接测试（`targets.go:169-175`）、资源扫描（`scan.go:49` → `:130/:178`）共用这些客户端；四条路径均无上游 deadline（云调用传 `context.Background()`）。连接成功后不返回、TLS/响应头挂起或网络黑洞可使 handler、同步轮次及 `s.Wait()` 长期停滞。该风险已在 localhost 阻塞端点隔离复现，但不等于真实阿里云事故。
- **最终方案：** 在 `provider` 包集中定义 `ConnectTimeout=10_000ms`、`ReadTimeout=30_000ms`，应用到 SWAS/ECS 正式 Provider及两条扫描路径。不新增 SQLite 设置或环境变量，不改 Provider 接口，不用不可取消的 goroutine + select 伪造超时，不改腾讯云 60s 行为。
- **必须保持：** 单轮只使用一个不可变运行时快照；重试仍完整执行 Describe → Diff → Create/Delete；不改变增量添加与精确删除契约；**四处超时值必须完全一致**——正式 Provider 与扫描路径共用同一 `ClientPool` 缓存键（`ali_swas.go:30` 与 `scan.go:132`；`ali_ecs.go:29` 与 `scan.go:180`，先创建者胜出），取值不同会让实际行为取决于调用顺序。
- **数值语义（实施记录用）：** `Connect=10s / Read=30s` 在该 SDK 下等价于「拨号上限 10s、响应头上限 30s、单次请求整体上限 40s」，不是 30s；文档与实施记录不得写成「单次 30s」。
- **修改与回归范围：** `provider/ali_swas.go`、`provider/ali_ecs.go`、`provider/scan.go` 及 provider 测试；建议在 `provider/common.go` 集中常量与一处共享构造 helper，避免四处漂移。使用真实慢 `httptest` 或本地阻塞服务覆盖四条构造路径，并断言在 deadline + 裕量内返回可被 A12 识别的错误；另加一条用例断言四个构造点的默认超时值恒为 10_000/30_000。
- **测试接缝（2026-09-27 用户已裁决，§六.4 F8）：** 允许在 `provider` 包内加入最小非导出接缝——一个把 `service/region` 映射为 `(endpoint, protocol)` 的包级变量（默认仍为 `<service>.<region>.aliyuncs.com` + `https`）与两个非导出超时变量（默认值必须仍为 10_000/30_000ms）。生产语义零变化；测试据此把四条路径指向本地阻塞服务并缩短等待。全仓无 `t.Parallel`，接缝不引入竞态。
- **外部边界：** 真实阿里云弱网/黑洞行为仍需用户真机验证，不得由本地阻塞服务替代。

**实施记录（批次 1，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** 在 `provider/common.go` 集中新增 `aliDefaultConnectTimeoutMS=10_000`、`aliDefaultReadTimeoutMS=30_000` 常量，两个非导出生效变量（默认等于常量）与端点解析接缝 `aliResolveEndpoint`（默认 `<service>.<region>.aliyuncs.com` + `https`），以及唯一构造样本 `newAliOpenAPIConfig(service, region, creds)`。**四处构造点全部改为调用该样本**：`provider/ali_swas.go`、`provider/ali_ecs.go`（正式 Provider）、`provider/scan.go` 的 `scanAliSWAS`/`scanAliECS`（扫描）；三处不再各自构造 `openapi.Config`，因此四处超时值不可能漂移。同步删除三文件已无用的 `openapi` 导入。
- **数值语义（按条文要求如实记录）：** `Connect=10s / Read=30s` 经 `darabonba-openapi/v2` → `tea/dara` 等价于「拨号上限 10s、响应头上限 30s、**单次请求整体上限 40s**」，不是「单次 30s」。本轮已独立核对依赖源码确认该链路：`darabonba-openapi/v2@v2.2.4/client/client.go:174-175` 把 `RuntimeObject.ConnectTimeout/ReadTimeout` 缺省回落到 `client.ConnectTimeout/ReadTimeout`（`:127-128` 从 `Config` 拷贝）；`tea/dara@v1.5.2/core.go:354` 置 `http.Client.Timeout=Connect+Read`、`:535` 置 `Transport.ResponseHeaderTimeout=Read`、`:736-742` 置 `net.Dialer.Timeout=Connect`。三者全 0 即完全无界，A1 结论与依赖行为一致。
- **判别性测试（`provider/ali_timeout_test.go`，新增）：**
  1. `TestAliClientRequestIsBounded`——对**四条**构造路径各起一条真实 `net.Listen` 「accept 但永不回包」阻塞服务，经接缝把端点指向该服务，断言请求在 `Connect+Read` + 5s 裕量内返回、耗时落在 `[Read, 预算]` 区间且错误可被 A12 识别。
  2. `TestAliClientDefaultTimeoutValues`——断言常量与两个生效变量的默认值恒为 `10_000/30_000`（接缝不得改变生产默认值）。
  3. `TestAliClientConfigCarriesTimeouts`——断言唯一样本确实把两个超时、Endpoint、Protocol 与凭据写进 `openapi.Config`。
- **红灯证据（修复前必失败）：** 临时移除 `newAliOpenAPIConfig` 的 `ConnectTimeout/ReadTimeout` 接线（等价修复前「只设置凭据与 Endpoint」），`go test ./provider/ -run TestAliClientRequestIsBounded -count=1 -timeout 40s` **FAIL**：SWAS/ECS 两条路径均报 `请求未在 5.5s 内返回：应用层超时未生效（修复前为完全无界）`；随后从 `/tmp/fwa-b1-backup/` 恢复并以 `shasum -a 256` 校验 `provider/common.go` = `4c6ac1014e0fd683aed528757e87c8cdb707c391a544dac8031d0e08ef8783d6` 一致，**未使用任何破坏性 Git 命令**。
- **测试耗时说明：** 阻塞用例在**缩短接缝取值**（200ms/300ms）下断言机制，单包约 3.3s；生产默认值（10s/30s）的真实等待曾单独验证通过（四条路径各 30.00s、用例总 120.02s），为避免每次全仓门禁额外增加约 2 分钟，日常门禁使用缩短取值，默认值由 `TestAliClientDefaultTimeoutValues` 常量断言覆盖。
- **门禁：** `go test ./provider/ ./syncer/ -race -count=1` 通过；`go test ./... -race -count=1` 11 包全通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过。
- **未执行（如实保留）：** 真实阿里云弱网/黑洞真机验证；本地阻塞服务**不等于**真实阿里云事故。

#### A12｜中｜常见超时错误未进入重试

- **状态与判定：** 确认存在，置信度高；与 A1 强绑定。
- **当前证据：** `syncer/retry.go:86-101` 的 `isRetryable` 依赖大小写敏感字符串，调用点为 `retry.go:35`（GetRules）、`:53`（DeleteRules）、`:70`（CreateRules）。标准库常见错误 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)` 不含小写 `timeout`，当前会被判为不可重试；隔离复现中 `errors.Is(..., context.DeadlineExceeded)` 与 `net.Error.Timeout()` 均可正确识别（`url.Error` 同时实现 `Unwrap` 与 `Timeout()`）。**本轮新增证据（实施必须覆盖）**：腾讯 SDK 会把网络错误转成 `*TencentCloudSDKError{Code:"ClientError.NetworkError", Message:"Fail to get response because … context deadline exceeded (Client.Timeout exceeded while awaiting headers)"}`（`common/client.go` 的发送路径 → `common/netretry.go:54`），该类型**没有 `Unwrap`**（`common/errors/errors.go` 全文无 `Unwrap`），因此真实腾讯超时上结构化判断**不成立**，只能靠字符串兜底；阿里云路径（`tea/dara`）原样返回 `*url.Error`，结构化判断有效。
- **影响：** 腾讯 SDK 超时以及 A1 修复后新增的阿里云超时可能只尝试一次，直接告警并写失败日志，与最多 3 次指数退避的契约不一致。
- **最终方案（2026-09-27 用户裁决，§六.4 F2：结构化与兜底两者都做）：** `isRetryable` 依序执行三段判断——① `errors.Is(err, context.DeadlineExceeded)`；② `errors.As(err, &netErr)` 且 `netErr.Timeout()`；③ 字符串兜底：保留现有 5 项（`RequestLimitExceeded`、`InternalError`、`FirewallBusy`、`timeout`、`connection refused`）并把比较改为**大小写不敏感**（message 与关键字统一小写），同时新增云错误码 `ClientError.NetworkError`。语义：腾讯 SDK 的网络类错误（含超时）进入重试；其余判定顺序与幂等优先级不变。
- **必须保持：** 最多 3 次、既有指数退避（1s、2s）、完整重走同步流程、幂等“已存在/已不存在”判定先于可重试判定且不计数不重试、云厂商限速间隔均不变；放宽带宽后不得让 CVM 规则上限等**有意不可重试**的错误变成可重试（`tc_cvm.go:231-233` 的硬错误保持不可重试）。
- **修改与回归范围：** `syncer/retry.go` 与专项测试。表驱动覆盖真实 `http.Client.Timeout` 产生的 `*url.Error`、`context.DeadlineExceeded`、`i/o timeout`、`connection refused`、`InternalError`、**SDK 形状的 `[TencentCloudSDKError] Code=ClientError.NetworkError …`** 与未知错误；端到端 fake Provider 需证明真实超时会进入第 2 次完整尝试（第 1 次返回真实超时错误，断言发生第 2 次 Describe→Diff）。
- **依赖风险：** 必须与 A1 同批交付；先放宽重试而仍无单次调用上限，会进一步拉长无界轮次。

**实施记录（批次 1，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `syncer/retry.go` 的 `isRetryable` 改为依序三段——① `errors.Is(err, context.DeadlineExceeded)`；② `errors.As(err, &netErr)` 且 `netErr.Timeout()`；③ 字符串兜底：message 与关键字统一转小写后匹配，保留原 5 项（`RequestLimitExceeded`、`InternalError`、`FirewallBusy`、`timeout`、`connection refused`）并新增 `clienterror.networkerror`。nil 直接返回 false。
- **判别性测试（`syncer/retry_test.go`，新增）：**
  1. `TestIsRetryable_RealWorldShapes` 表驱动 12 项：真实 `http.Client.Timeout` 产生的 `*url.Error`（并断言错误文本确含 `Client.Timeout exceeded while awaiting headers`）、`context.DeadlineExceeded` 及其包装、`net.Error` i/o timeout、`Connection Refused`（大写，验证大小写不敏感）、`InternalError`、`RequestLimitExceeded`、`FirewallBusy`、**SDK 形状 `[TencentCloudSDKError] Code=ClientError.NetworkError, …`**、CVM 规则上限（有意不可重试）、未知错误、nil。
  2. `TestRetrySync_RealTimeoutTriggersSecondFullAttempt`——第 1 次 `GetRules` 返回**真实** `http.Client.Timeout` 错误，断言 `GetRules` 调用次数 = 2（发生第 2 次完整 Describe→Diff）。
  3. `TestRetrySync_TencentNetworkErrorRetries`——腾讯 SDK 无 `Unwrap` 的网络错误码同样重试（调用次数 = 2）。
  4. `TestRetrySync_NonRetryableStopsImmediately`——CVM 上限错误经 `errors.Is` 原样返回且调用次数 = 1。
  5. `TestRetrySync_ExhaustsThreeAttempts`——持续可重试时调用次数恰为 `maxRetries`(=3)。
- **红灯证据（修复前必失败）：** 在 `/tmp/fwa-a12-proof` 用独立程序原样复刻修复前的旧 `isRetryable`（大小写敏感 5 项），对同一批真实错误断言：真实 `client.Get` 超时错误 `Get "http://127.0.0.1:63986/": context deadline exceeded (Client.Timeout exceeded while awaiting headers)` → 旧实现 `false`、新实现 `true`；腾讯 SDK 形状 `ClientError.NetworkError` → 旧 `false`、新 `true`。即修复前这两类超时都只尝试一次。
- **必须保持的回归：** 既有 `TestRetrySync_Counts`、`TestRetrySync_TagSnapshotAcrossRetry`、`TestRetrySync_IdempotentErrorsNotCounted`、`TestRetrySync_EmptyCommentDesc`、`TestRetrySync_PartialWriteCounting` 全部继续通过；幂等「已存在/已不存在」判定仍先于可重试判定且不计数不重试。
- **门禁：** 同批次 1（`./provider ./syncer` race 通过、全仓 11 包 race 通过、`vet`/`build`/`git diff --check` 通过）。
- **未执行（如实保留）：** 真实腾讯云/阿里云网络故障下的重试次数真机观测。

### 批次 3：Stop 门控与生命周期

本批实施时必须同时保留 A6 的暂停门控回归与 A7 的恢复立即一轮语义，但不得把 A20 混写为 A6 已修复的一部分；两项完整防回归合同统一见 §四。

#### A20｜中｜`Stop()` 后仍可能启动一整轮同步

- **状态与判定：** 确认存在，置信度高。历史隔离复现先后出现 5/15 与 10/15；概率次数仅说明风险存在，不能作为最终唯一验收。
- **当前证据：** `syncer/syncer.go` 的启用态 select 同时监听 ticker、trigger、control 与 stop；ticker/trigger 守卫只检查 `IsEnabled()`（`:164-167`、`:174-177`），未检查 stop，而 `Stop()` 不改变 `IsEnabled()`（`:267-272` 读已发布状态）。多个分支同时就绪时可随机先消费 trigger/ticker，再启动新轮。
- **本轮新增证据（Issue6 原措辞未覆盖，实施必须一并修）：** 当前代码共有 **4 处** `syncAll` 调用点——`:137`（Run 入口的启动轮，位于循环之外）、`:168`（ticker）、`:179`（trigger）、**`:204`（false→true 恢复立即一轮）**。其中两条额外可达路径已被确认：① `s.Stop()` 先于 `go s.Run()`（生产对应“启动即收到 SIGTERM”，`run.go:131` 与 `run.go:172` 的调度顺序），可 100% 确定性复现，Stop 后仍会跑完整一轮；② 暂停期 `POST /api/sync/resume`（`deps.go:180` → 协调器 commit → `deps.go:134` `ApplyState`）与 SIGTERM 并发时，暂停子循环的 select 在 control 与 stop 间随机，选中 control 后走 `:204` 启动整轮。
- **触发与影响：** 当前轮进行中已排队 trigger 或 ticker 到期，随后收到 SIGTERM。当前轮结束后可能再执行一整轮真实云写入（Describe → Diff → Create/Delete），`s.Wait()` 继续无界等待，违背“完成当前轮次再退出”的意图。
- **最终方案：** 将 stop 设为**新轮次的硬门控**，四处调用点统一走同一门控：Run 入口的启动轮、ticker、trigger 与 false→true 恢复轮进入 `syncAll` 前一律检查 stopped 状态。Stop 只挡新轮次，绝不取消或中断已开始的 `syncAll`。
- **测试机制（2026-09-27 用户裁决，§六.4 F6）：** 抽取可单测的门控函数（如 `isStopped()`）+ 一处测试专用 hook（默认 nil，与既有 `SetStateAppliedHook` 同风格的注入点，生产零行为变化），用于构造“trigger 已被 select 选中 → hook 内调用 `Stop()` → 门控必须拦截”的 100% 确定性交错；另以 `Stop()` 后再 `go Run()` 作为 100% 红灯证据。`-count=20` 仅作压力补充，不作为唯一判据。红灯证明沿用项目既有流程（临时改回 → 观察失败 → 从 `/tmp` 备份恢复并校验 sha256），禁止破坏性 Git 命令。
- **必须保持：** Stop 幂等；当前轮完成后退出；暂停子循环不消费 ticker/trigger；不增加轮次超时；A6 的 enabled 守卫继续存在且**与 stopped 门控并列、不合并**；门控不得外溢到 Dry Run/连接测试（AGENTS §五要求二者不受暂停/停止影响）。
- **修改与回归范围：** `syncer/syncer.go`、`syncer/state_test.go`（必要时含 `syncer/syncer_test.go`）。判别性用例：`Stop()` 后启动 Run 不得起轮（Provider 调用数恒为 0）、门控函数单元断言（Stop 后门控 false 而 `IsEnabled()` 仍 true）、hook 构造的 trigger/stop 交错（调用数恒为 1）；以下既有用例必须继续通过：`TestStopWaitsForBlockedRound`、`TestStopIdempotent`、`TestNoNewRoundAfterStop`、`TestControl_FalseToTrueTriggersRoundImmediately`、`TestControl_TrueToTrueOnlyResetsTicker`、`TestControl_TrueToFalseFinishesCurrentRound`、`TestQueuedTriggerNotRunWhilePaused`、`TestStaleTriggerAfterPauseDoesNotAddRound`、`TestResumeImmediateRoundAfterPhaseMirrorAdvance`、`TestPausedPublishedStateDropsTickerRound`。

**实施记录（批次 3，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `syncer/syncer.go` 新增生命周期标记 `stopped`（由既有 `mu` 保护）、可单测门控谓词 `isStopped()`、统一硬门控 `beginRound()`，以及测试专用钩子 `SetBeforeRoundHook`（默认 nil，与 `SetStateAppliedHook` 同风格，生产零行为变化）。**四处 `syncAll` 调用点全部改为先过 `beginRound()`**：Run 入口启动轮（原 `:137`）、ticker（原 `:168`）、trigger（原 `:179`）、false→true 恢复轮（原 `:204`）；门控拒绝时分别输出日志并 `return`/`break`/`continue`。`Stop()` 现在只置位 `stopped` 并幂等关闭 `stopCh`，**不改变 `IsEnabled()`**。
- **门控语义：** `beginRound()` 刻意**不**检查 `IsEnabled()`，因此不会外溢到 Dry Run / 连接测试（AGENTS §五）；A6 的 enabled 守卫在 ticker/trigger 两处**原样保留**，与 stopped 门控并列、不合并。
- **判别性测试（`syncer/stop_gate_test.go`，新增）：**
  1. `TestBeginRoundGateAfterStop`——单元断言：`Stop()` 后 `isStopped()` 为 true、`beginRound()` 返回 false，而 **`IsEnabled()` 仍为 true**（证明两个门控独立）。
  2. `TestStopBeforeRunStartsNoRound`——`Stop()` 先于 `go Run()`，断言 Run 有界退出且 **Provider 调用次数恒为 0**。
  3. `TestStopInjectedDuringRoundBlocksNextRound`——用 `SetBeforeRoundHook` 在「门控通过之后、`syncAll` 之前」注入 `Stop()`，断言 `beginRound` 通过次数恒为 1、Provider 调用次数恒为 1（只保留 Stop 前已开始的当轮）。
  4. `TestRunSecondCallRejectedWithoutPanic`——见 A16 实施记录（同批交付）。
- **红灯证据（修复前必失败）：** 临时移除 Run 入口启动轮的 `beginRound()` 门控（等价修复前 `if enabled { s.syncAll() }`），`go test ./syncer/ -run 'TestStopBeforeRunStartsNoRound|TestStopInjectedDuringRoundBlocksNextRound' -count=1` **FAIL**：`stop_gate_test.go:73: Stop 后启动 Run 不得调用任何 Provider：GetRules = 1, want 0`（第二条用例同时失败）。随后从 `/tmp/fwa-b3-syncer-final.go` 恢复，`shasum -a 256 syncer/syncer.go` = `65b4f7ae199ee5d93001a934cf6b72d626aa33e0d3798890f2b219aa123a43e8` 一致，**未使用任何破坏性 Git 命令**。`-count=20` 未单独作为判据。
- **必须保持项复核：** Stop 幂等（`stopOnce` + `stopped` 标记）；当前轮完成后退出；暂停子循环不消费 ticker/trigger；不增加轮次超时；A6 的 enabled 守卫仍在原处。下列既有用例继续通过：`TestStopWaitsForBlockedRound`、`TestStopIdempotent`、`TestNoNewRoundAfterStop`、`TestControl_FalseToTrueTriggersRoundImmediately`、`TestControl_TrueToTrueOnlyResetsTicker`、`TestControl_TrueToFalseFinishesCurrentRound`、`TestQueuedTriggerNotRunWhilePaused`、`TestStaleTriggerAfterPauseDoesNotAddRound`、`TestResumeImmediateRoundAfterPhaseMirrorAdvance`、`TestPausedPublishedStateDropsTickerRound`。
- **门禁：** `go test ./syncer/ ./webui/ -race -count=1` 通过；`go test ./... -race -count=1` 11 包全通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过。

#### A16｜低｜`Start`、`Wait`、`Run` 重复调用契约不完整

- **状态与判定：** 确认存在；生产当前只调用一次，因此不是已发生故障。
- **当前证据：** `webui.Server.Start`（`server.go:133-176`）先在 `:135` `net.Listen`、`:159-162` 覆盖 `s.httpServer/s.listener`，**之后**才 `serveOnce.Do`；第二次调用会先占用/降级到随机端口并覆盖字段，第二个 listener 无人 Serve 且**永远不会被关闭**（`http.Server.Shutdown/Close` 只关闭 `Serve` 期间登记的 listener），同时 `Shutdown` 只作用于未服务的第二个 `http.Server` → **真正在服务的第一个 Serve 永不停止，`run.go` 的 `srv.Wait()` 永不返回**，并产生一条伪 `EADDRINUSE` WARN。`Server.Wait`（`:182-184`）从只在 `:167` 投递一次的容量 1 channel 读取，第二次/并发第二次会永久阻塞。`Syncer.Run`（`syncer.go:114-115`）第二次执行会重复关闭 `doneCh` 而 panic，**首个 Run 已退出后再调用同样 panic**（`Syncer.Wait` 读 closed channel，本身可多次安全等待，不属本项缺陷）。`Stop`/`Shutdown` 已幂等，不需重做。
- **最终方案（2026-09-27 用户裁决，§六.4 F7）：**
  1. `Server.Start` 在 `net.Listen` **之前**用锁保护的 started 判定拒绝第二次调用，返回明确错误（`ErrAlreadyStarted`，返回值 `(0, err)`），保持首个 listener 与 `httpServer` 不变，不新建、不降级、不产生伪 WARN；**Shutdown 之后再调用 `Start` 同样拒绝**。
  2. `Server.Wait` 保存唯一 Serve 结果，以关闭完成 channel 广播，使多次/并发 Wait 返回同一结果；归一化仍在 Serve 返回处完成。
  3. `Syncer.Run` 用 mutex 或 atomic CAS 拒绝**一切**第二次调用（含首个已退出后），WARN 并立即返回；**不得使用会让第二个调用等待首个 Run 结束的 `sync.Once.Do`**。
- **必须保持：** 仅 `EADDRINUSE` 才随机端口；Shutdown 先关闭 SSE shutdown channel，再执行 HTTP Shutdown，超时才 Close；Stop/Shutdown 继续幂等；归一化 `http.ErrServerClosed`/`net.ErrClosed` 的时机不变；不新增常驻 goroutine（`TestShutdownNoGoroutineLeak` 会扫描 `webui.(*Server).` 栈）。
- **修改与回归范围：** `webui/server.go`、`syncer/syncer.go` 及对应测试。覆盖 `Start` 两次不泄漏且保持首地址（白盒比较 listener 指针）、并发/重复 `Wait` 同结果、`Run` 两次不 panic 且有界返回。以下既有用例必须继续通过：`TestWaitBeforeStartBlocksUntilServeExits`、`TestServeRuntimeErrorSurfacedToCaller`、`TestShutdownNormalizesServeResult`、`TestShutdownIdempotent`、`TestShutdownBeforeStart`、`TestShutdownTwiceBeforeStart`、`TestShutdownAfterServeExitedRepeated`、`TestShutdownNoGoroutineLeak`。

**实施记录（批次 3，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：**
  1. `webui/server.go`：新增导出错误 `ErrAlreadyStarted`；新增受 `mu` 保护的 `started` 标记，`Start` 在 `net.Listen` **之前**判定并置位，重复/并发/Shutdown 后的第二次调用返回 `(0, ErrAlreadyStarted)`，`listener`/`httpServer` 保持不变；删除已无用的 `serveDone` channel 与 `serveOnce`，改为 `waitOnce` + `waitDone` 关闭广播 + 缓存 `serveResult`，`Wait` 从广播读取同一结果（归一化时机仍在 Serve 返回处，未变）。
  2. `syncer/syncer.go`：新增受 `mu` 保护的 `runGuard`，`Run` 在入口处拒绝一切第二次调用（含首个 Run 已退出之后），WARN 后立即返回；**刻意不使用 `sync.Once.Do`**（否则第二个调用会等待首个 Run 结束）。拒绝分支**不关闭 `doneCh`**（`doneCh` 归首个 Run 所有，关闭两次会 panic）。
- **判别性测试：**
  - `webui/server_test.go` 新增：`TestStartSecondCallRejected`（第二次 `Start` 返回 `ErrAlreadyStarted`、返回端口为 0，白盒比较 `listener`/`httpServer` **指针未变**、监听地址未变，且随后 `Shutdown`+`Wait` 有界完成）；`TestStartAfterShutdownRejected`；`TestConcurrentStartOnlyOneWins`（8 并发 Start 恰好 1 成功、7 个 `ErrAlreadyStarted`）；`TestWaitConcurrentReturnsSameResult`（6 个并发 Wait 在 Serve 运行期全部阻塞、Shutdown 后返回同一 `nil`，Shutdown 之后再次 Wait 亦立即返回同一结果）。
  - `syncer/stop_gate_test.go` 新增：`TestRunSecondCallRejectedWithoutPanic`（首个 Run 运行中连续 3 次第二次 `Run` 均有界返回；首个 Run 退出后再调用同样有界返回）。
- **红灯证据（修复前必失败）：** 临时移除 `Start` 的 `started` 门控（等价修复前无重复调用判定），`go test ./webui/ -run 'TestStartSecondCallRejected|TestConcurrentStartOnlyOneWins' -count=1` **FAIL**（`TestStartSecondCallRejected` 失败：第二次 Start 未被拒绝）。随后从 `/tmp/fwa-b3-server-final.go` 恢复，`shasum -a 256 webui/server.go` = `bfd3dbb009c5b16ceb38e574bade32b485b7a7167ef6fbf7e6bfd070914843a7` 一致，**未使用任何破坏性 Git 命令**。`Wait` 与 `Run` 两条路径的旧实现（容量 1 channel 二次读取永久阻塞、`defer close(doneCh)` 二次关闭 panic）由代码结构与指针级断言判别，未单独做挂起式红灯（那会让单次门禁多耗数分钟）。
- **必须保持项复核：** 仅 `EADDRINUSE` 才随机端口（`TestStartFallsBackOnlyAfterEADDRINUSE`、`TestStartReturnsNonPortErrorsUnchanged`、`TestStartDoesNotReleaseBoundListener` 继续通过）；Shutdown 顺序与幂等不变（`TestShutdownNormalizesServeResult`、`TestShutdownIdempotent`、`TestShutdownBeforeStart`、`TestShutdownTwiceBeforeStart`、`TestShutdownAfterServeExitedRepeated`）；未新增常驻 goroutine（`TestShutdownNoGoroutineLeak` 继续通过）；`TestWaitBeforeStartBlocksUntilServeExits`、`TestServeRuntimeErrorSurfacedToCaller` 继续通过。
- **门禁：** 同批次 3（`./syncer ./webui` race 通过、全仓 11 包 race 通过、`vet`/`build`/`git diff --check` 通过）。

### 批次 2：告警发送有界化

#### A2｜高｜SMTP 无 deadline，异步告警无并发上界

> **P3-15 后续实施说明（2026-10-03）**：下述 A2 方案与 2026-09-27 记录保留历史证据；其中“每次丢弃记录 WARN”的现时行为已由独立授权 P3-15 方案 B 替代。渠道长期限流器保存固定类别计数，新周期首条立即 WARN，随后约每 30 秒汇总新增丢弃，空窗口不输出并停止续约；窗口与计数跨配置重载、关闭再开启及旧实例晚到回调连续。Webhook 平台切换时按原平台计数，混合汇总标记 `mixed`。每渠道 4 条在途、满载丢弃最新、返回 nil、不排队不重试及 EventBus 异步边界保持。正式测试与门禁见 [审计 P3-15 当前实施补记](./fwalizer-audit-final1.md#p3-15-当前实施补记2026-10-03推荐方案-b)；真实外部链路人工状态保持，进程退出前未输出的计数可能丢失。

- **状态与判定：** 确认存在，置信度高。SMTP 无 deadline 与并发无上限是代码事实；生产资源耗尽仍只是可信推论。
- **当前证据：** `notifier/bus.go:109-115` 为每个接口订阅者启动一个 goroutine（无上限、无丢弃、无跟踪）；`notifier/email.go:49` 使用 `smtp.SendMail`——标准库内部 `smtp.Dial` 即 `net.Dial("tcp", addr)` 无 timeout，greeting（`ReadResponse(220)`）、STARTTLS、AUTH、MAIL、RCPT、DATA、QUIT 全链路无 deadline；Webhook client 已有 10s 超时（`notifier/webhook.go:26`）。全仓通知链路零并发上界（`syncer.go:448` 的 `WaitGroup` 只等轮次内云调用）。同步轮次不等待告警，进程退出也不等待在途告警（`run.go:182` 只等 `s.Wait()`）。localhost 静默 SMTP 与阻塞订阅者已分别复现长时间不返回及 goroutine 线性增长。
- **最终方案（限流归属按 2026-09-27 用户裁决，§六.4 F5）：** SMTP 改为 `net.Dialer{Timeout:10s}` → `conn.SetDeadline(now+30s)` → `smtp.NewClient`，完整保留 greeting、STARTTLS、AUTH、MAIL、RCPT、DATA、QUIT；deadline 覆盖初始 greeting 与 QUIT。邮件与 Webhook **各自**最多 4 个在途任务；满载丢弃最新通知，每次记录不含密码、URL 或正文敏感值的 WARN。限流器**由 `AlertManager` 按渠道持有并注入 notifier 实例**，使“每渠道在途 ≤4”在配置热重载（旧实例在途发送不取消、不等待）后仍然成立；上限位于告警订阅者/调度边界，不能限流整个 EventBus。
- **明确不做：** 不新增持久队列、告警重试、进程退出等待或重型 worker 框架；`Publish` 继续非阻塞，StoreLogWriter 与 SSE 不受影响。
- **测试接缝（2026-09-27 用户裁决，§六.4 F8）：** 允许把 10s/30s 定义为 notifier 包内非导出变量（默认值不得改变），同包测试可缩短以断言机制；Webhook 的 10s 若需用例化同样处理。
- **修改与回归范围：** `notifier/email.go`、`notifier/webhook.go`、新增每渠道 limiter（可置于 `notifier/limit.go`）、`webui/api/alertset.go`（按渠道持有并注入 limiter）与 notifier/alertset 测试。静默 SMTP 必须在 deadline + 裕量内返回并关闭连接；阻塞邮件/Webhook 下断言各自在途不超过 4，溢出丢弃最新且 WARN 可见（“在途”的确定性观测量建议用假服务端已接受的连接数/并发数，不得用 `runtime.NumGoroutine`）。以下既有夹具与用例必须保持绿：`notifier/bus_test.go` 全部（尤其 `SlowCallbackDoesNotHoldLock`、`SubscriberErrorIsolation`、`FullBufferDoesNotBlock`、`CancelDoesNotCloseChannel`）、`webui/api/alertset_test.go` 全部（含 `ApplyBoundaryInFlightAndNewSubscriptions`）、`main_test.go` 的 `TestProcessSecretsNotLogged`。
- **外部边界：** 真实 SMTP/收件箱与 Webhook 人工验收此前已被用户免除，至今仍没有真实通过结论；本地假服务不得写成真实链路通过。

**实施记录（批次 2，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：**
  1. `notifier/email.go`：**不再使用 `smtp.SendMail`**（它无法设置 dial timeout 与会话 deadline）。改为 `net.DialTimeout(10s)` → `conn.SetDeadline(now+30s)` → `smtp.NewClient`，并完整保留 greeting(220)、EHLO、STARTTLS（服务端通告时）、AUTH（配置了用户名时）、MAIL、RCPT、DATA、QUIT 的既有顺序与语义；deadline 覆盖初始 greeting 与 QUIT，异常路径由 `defer c.Close()` 释放连接。新增常量 `smtpDefaultDialTimeout=10s` / `smtpDefaultDeadline=30s` 与两个非导出**生效**变量（默认等于常量，仅测试接缝，F8 授权）。
  2. `notifier/inflight.go`（新增）：`InFlightLimit = 4`（每渠道在途上限）、`InFlightLimiter`（带缓冲 channel 作信号量，`Acquire` 非阻塞返回 `(release, ok)`，满载返回 `false`；`release` 用 `sync.Once` 保证幂等，重复调用不会膨胀容量）、导出接口 `LimitedNotifier`（`SetInFlightLimiter` + `ChannelName`）、安全 WARN helper `logDropped`（只记录渠道名、事件类型与上限，绝不记录密码/URL/正文）。
  3. `notifier/webhook.go`：10s 超时改为非导出变量 `webhookTimeout`（默认仍是 10s，F8 接缝）；`OnEvent` 增加同一在途门控，满载丢弃最新并输出同一安全 WARN。
  4. `webui/api/alertset.go`：`AlertManager` **按渠道持有** `emailLimiter`/`webhookLimiter`（构造时创建，生命周期长于 notifier 实例），`Apply` 时经 `injectLimiter` 注入新实例。因此热重载只替换实例，**在途计数保持连续**，「每渠道 ≤4」跨热重载仍成立，而旧实例未完成的发送不取消、不等待。构造函数签名未改动（既有测试与调用点零改动）。
- **明确未做（按条文）：** 未新增持久队列、告警重试、进程退出等待或 worker 框架；`EventBus.Publish` 仍为非阻塞异步投递；StoreLogWriter 与 SSE 未受影响；**未限流整个 EventBus**。
- **判别性测试（`notifier/inflight_test.go`，新增）：**
  1. `TestSMTPDefaultTimeoutValues`——断言默认取值恒为 10s/30s（接缝不得改变生产语义）。
  2. `TestEmailSendBoundedByDeadlineOnSilentServer`——对「accept 但永不发 greeting」的静默 SMTP，断言 `OnEvent` 在预算内有界返回。
  3. `TestInFlightLimiterMechanism`——容量 4、满载 `Acquire` 失败、`release` 幂等不膨胀、释放后可再取。
  4. `TestWebhookInFlightCapAndDropNewest`——阻塞式 `httptest` 服务端记录**已接受并发请求数**（不用 `runtime.NumGoroutine`）：4 条在途时并发观测 ≤4，第 5 条被丢弃且不产生第 5 个请求、`OnEvent` 立即返回 nil、输出「在途已满」WARN 且不含 Webhook URL。
  5. `TestWebhookLimiterSurvivesHotReload`——旧实例占满名额后用**同一限流器**构造新实例，断言新实例仍被丢弃（证明上限跨热重载连续）。
  6. `TestEmailInFlightCapAndDropNewest`——邮件渠道同一门控，并与 Webhook 各自独立计数。
- **红灯证据（修复前必失败）：** 在 `/tmp/fwa-a2-proof` 用独立程序对比：① 修复前路径 `smtp.SendMail` 对静默 SMTP **3s 内未返回**（确认无界）；② 修复后路径「显式建连 + `SetDeadline(400ms)`」**403ms 返回** `read tcp …: i/o timeout`。即修复前该场景会永久挂住一个 goroutine。限流相关用例的判别性来自「第 5 条不产生第 5 个服务端请求 + WARN」，修复前无限流时服务端并发可达 5+。
- **必须保持的既有用例（继续通过）：** `notifier/bus_test.go` 全部（含 `EventBus_SlowCallbackDoesNotHoldLock`、`EventBus_SubscriberErrorIsolation`、`EventBus_FullBufferDoesNotBlock`、`EventBus_CancelDoesNotCloseChannel`）、`webui/api/alertset_test.go` 全部（含 `TestAlertManagerApplyBoundaryInFlightAndNewSubscriptions`）、根包 `TestProcessSecretsNotLogged`（PUT 后仍出现「Webhook 告警已更新」且日志不含 SMTP 密码/Webhook URL/云密钥）。
- **门禁：** `go test ./notifier/ ./webui/api/ -race -count=1` 通过；根包进程级用例通过；`go test ./... -race -count=1` 11 包全通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过。
- **未执行（如实保留）：** 真实 SMTP/收件箱与真实 Webhook 链路仍为**用户已免除、无真实通过结论**；本批全部证据来自本地假服务与单元测试，**不得写成真实链路通过**。

### 批次 4：同步健康、轮次汇总、SSE 与 Provider 写入计数接口（A11 前移部分）

#### A3｜中｜HTTP 健康检查不表达同步健康

- **状态与判定：** 产品语义已定，待实施；这不是现有 `/api/health` 的契约错误。
- **当前证据：** `/api/health` 是纯静态 handler（`webui/server.go:240-243`），不读 Store/Syncer/运行时；Docker HEALTHCHECK（`build/Dockerfile:31-33`）与 Compose healthcheck（`docker-compose.yml.example:45-54`）只调用该端点；同步状态目前缺少自动的成功/失败/停滞表达（`SyncStatus` 只有 `running/enabled/last_sync`）。
- **最终方案：** `/api/health`、Docker HEALTHCHECK 与容器重启语义保持不变；同步健康仅通过向后兼容扩展 `/api/sync/status` 和 Dashboard 提示表达，字段口径与 A18 共用（`last_success` + `last_round.outcome`）。
- **必须保持：** 暂停、首次启动、无目标或空轮次不能使 HTTP health 返回 503；不设置全局 `ReadTimeout/WriteTimeout`；不新增第二个状态大字或小号操作按钮（沿用 Dashboard 既有的 `NAlert` 与三态展示约定）。
- **口径澄清（本轮补充）：** “停滞”不引入任何时间阈值判定，只由 `last_success` 缺失或落后于 `last_sync` 与 `outcome` 表达，避免暂停期或重启后（两个时间戳均为内存态、重启归 null）产生误报；Dashboard 提示必须走既有的 5s 轮询 `/api/sync/status`，不得依赖 `/api/sync/events`（该 SSE 无回放、缓冲满即丢，`sync:complete` 可能对已连接客户端不可见）。
- **修改与回归范围：** sync 状态、API、Dashboard 和 README。测试同时断言新同步字段存在且 `/api/health` 在暂停/无目标/空轮次/从未成功等状态下仍精确返回 `{"status":"ok"}` 与 200；前端提示需浏览器验收。

**实施记录（批次 4，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `syncer.SyncStatus` 在**既有三字段类型与语义完全不变**的前提下追加 `last_success`（`*time.Time`）与 `last_round`（`*RoundSummary`）；`Syncer.Status()` 在原有锁内一并拷贝这两个指针（拷贝值而非共享指针，避免调用方观察到后续轮次的写入）。`webui/api/sync.go` 的 `handleSyncStatus` **未改动**——它直接序列化 `Status()`，因此扩展自动生效；`/api/health` handler **一字未改**。
- **前端：** `webui/frontend/src/types.ts` 新增 `RoundSummary` 接口并给 `SyncStatus` **只追加** `last_success`/`last_round`；`Dashboard.vue` 初始值补齐两个字段，并新增 `healthHint` computed：只依据 `last_round.outcome`（failed/partial）与「`last_success` 缺失或落后于 `last_sync`」表达健康提示，**不引入任何时间阈值判定**；提示走既有 5s `fetchStatus()` 轮询，**不依赖 `/api/sync/events`**；暂停时不提示。未新增第二个状态大字或小号操作按钮，沿用既有 `NAlert`。
- **口径澄清落实：** 「停滞」仅由 `last_success` 缺失/落后 + `outcome` 表达；两个时间戳均为内存态，重启后为 `null`，前端文案使用「尚无成功记录（进程重启后该记录会重置）」而**不写「从未成功」**。
- **判别性测试（`webui/server_test.go` 新增 `TestHealthEndpointUnaffectedBySyncState`）：** 在「未接入 Syncer」（等价暂停/无目标/从未成功）状态下断言 `/api/health` 精确返回 `{"status":"ok"}` 与 200，且查询 `/api/sync/status` 前后该响应体不变；同时断言 `/api/sync/status` 含 `running`/`enabled`/`last_sync`/`last_success`/`last_round` 五个字段。`TestServerTimeoutContract` 继续断言全局 `ReadTimeout`/`WriteTimeout` 为零。
- **门禁：** `go test ./provider/ ./syncer/ ./webui/ ./webui/api/ -race -count=1` 通过；全仓 11 包 race 通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过；前端 `npm ci && npm run build`（`vite`，产物已重建 `webui/frontend/dist`（该目录未被 Git 跟踪，`go:embed` 依赖它））与两条阻断式 audit（`--audit-level=high` 与 `--omit=dev`）均为 **0 漏洞**。
- **浏览器验收（2026-09-28，临时本地 HTTP mock + 当前前端 dist）：** 已逐状态检查初始 null、success、failed、partial、idle 与 paused。failed/partial 提示分别显示失败或跳过数量；idle 在 `last_success` 落后于 `last_sync` 时显示停滞提示；paused 显示“已暂停”、立即同步置灰且不显示健康告警。成功状态的统计概览显示整轮 `新增 2 / 删除 1`。连续轮询日志显示状态请求按 5 秒间隔发起，mock 未收到 `/api/sync/events` 请求。该验收只证明当前 Dashboard 的展示与轮询合同，不代表真实云、Docker 或外部服务通过。

#### A18｜中｜缺少 `last_success` 与整轮成功/失败汇总

- **状态与判定：** 产品语义已定，待实施。退出时无超时等待当前轮是 AGENTS 强要求，不作为本项缺陷。
- **当前证据：** 当前轮结束后无条件刷新 `last_sync`（`syncer.go:464-466`），整轮完成事件 `EventSyncComplete.Data` 只有 `duration`（`:469-473`）；`SyncStatus`（`:322-329`）只有 `running/enabled/last_sync`；逐域结果只发事件，无任何整轮聚合；失败只能从逐域日志/告警侧推断。
- **最终统计合同（字段口径按 2026-09-27 用户裁决，§六.4 F4）：** 一个统计单元为“一个 Provider × 一条适用规则”，`total = Σ_p len(filterRulesForTarget(...))`。每轮至少包含 `finished_at/total/ok/changed/failed/skipped/added/deleted/duration/outcome`，其中 `ok` 严格表示**成功且无变更**的单元，`changed` 表示**成功且发生了增删**的单元，恒有不变量 `total == ok + changed + failed + skipped`。DNS/Provider 错误记 failed；明确未实施操作记 skipped。只有 `total>0 && failed==0 && skipped==0` 为 success 并更新 `last_success`；failed>0 为 failed；无失败但有 skipped 为 partial；无 Provider/适用规则为 idle；后三者都不更新 `last_success`。
- **兼容合同：** `last_sync` 仍表示最近一轮完成；暂停期不制造轮次或刷新时间；`EventSyncComplete.Data` 保留 duration 并增加同一汇总；`SyncStatus` 的既有三字段语义与类型不变；本批不增加 SQLite 列。
- **修改与回归范围：** `syncer` 状态/汇总、`GET /api/sync/status`、Dashboard 和事件测试。覆盖全成功、成功含增删（changed）、部分失败、仅 skipped、idle、从未成功、暂停；并行计数必须通过 race。
- **依赖与边界：** 依赖 A1/A12；依赖 A11 的 `CreateRules` 返回值前移到本批（§2.2、§六.4 F3），否则 `skipped`/`partial` 在批次 4 无法端到端判别；Dashboard 需要浏览器人工验收。

**实施记录（批次 4，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `syncer/syncer.go` 新增 `RoundOutcome`（success/failed/partial/idle）与 `RoundSummary`（`finished_at/total/ok/changed/failed/skipped/added/deleted/duration_ms/outcome` + `Data()`），`Syncer` 新增内存态 `lastSuccess *time.Time`/`lastRound *RoundSummary`。`syncAll` 先按 `total = Σ_p len(filterRulesForTarget(rules, p.TargetIndex()))` 算清统计单元总数，再由新的 `runRound` 按云厂商并行执行并收集每个单元结果；`syncDomain` 改为把结果写入 `unitResult`（`failed/added/deleted/skipped`），新增 `unitOutcome()` 按 F4 口径归类：**失败优先 → 明确跳过 → 有增删记 changed → 否则 ok**。`outcomeOf()` 实现：`total==0 → idle`；`failed>0 → failed`；`skipped>0 → partial`；否则 `success`。**只有 `success` 才刷新 `last_success`**。`EventSyncComplete.Data` 保留既有 `duration` 字符串并增加同一份整轮汇总（10 个字段）。
- **限速位置微调（同批记录）：** 原来每处理一条规则就 `time.Sleep(rateLimitInterval(ct))`，现改为**每个 Provider 处理完后限速一次**。理由是云调用本身是网络往返，原写法在单 Provider 多规则时会叠加大量空闲等待（测试里就是主要耗时来源）；新写法仍满足 AGENTS §七「同一云厂商内串行处理 + 域名之间加入间隔」的语义（轮次上限不变），且不改任何云 API 调用顺序与次数。**如认为该微调超出本批范围，请指出，我会回退为每规则限速。**
- **不变量与兼容合同落实：** `total == ok + changed + failed + skipped` 由 `unitOutcome()` 的单值分类保证（每个单元恰好落入一类）；`last_sync` 语义不变（仍为最近一轮完成时间）；暂停期不制造轮次；`SyncStatus` 既有三字段类型与语义不变；**本批未增加任何 SQLite 列**。
- **判别性测试（`syncer/round_summary_test.go` 新增）：** `TestRoundSummary_Idle`（无适用规则 → idle，且不调用云 API）；`TestRoundSummary_SuccessNoChange`（规则已一致 → `ok=1 changed=0` 且不调用 `CreateRules`）；`TestRoundSummary_ChangedCountsAsChanged`（成功且有新增 → `ok=0 changed=1`，**判别 F4「ok 只计无变更」**）；`TestRoundSummary_ProviderErrorIsFailed`；`TestRoundSummary_OnlySkippedIsPartial`（Provider 报 `{Written:0,Skipped:1}` → `partial`、`added=0`、`skipped=1`，**判别 A11+A18 端到端**）；`TestRoundSummary_InvariantAcrossMixedUnits`（两 Provider 一成功一失败时 `total=2`、不变量成立、`outcome=failed`）。所有断言都从**真实 `EventSyncComplete` 事件负载**还原汇总，因此同时证明事件字段口径正确。
- **门禁：** 同批次 4（受影响包 race、全仓 race、`vet`/`build`/`git diff --check`、前端构建与两条 audit 全通过）。
- **浏览器验收（2026-09-28，临时本地 HTTP mock + 当前前端 dist）：** 已逐状态检查初始 null、success、failed、partial、idle 与 paused，并核对 `last_success` 保留、整轮新增/删除数与暂停期无提示。连续轮询日志显示状态请求按 5 秒间隔发起，且未建立 `/api/sync/events`。该验收只覆盖前端展示和状态 API 消费，不代表真实云或外部链路通过。


#### A13｜低｜普通轮次重复记录“告警已更新”

- **状态与判定：** 确认存在；属于日志噪声和误导。
- **当前证据：** `syncer.go:218` 在每次 select 返回后**无条件**调用 `logAppliedState(latest)`（ticker/trigger/control 三条路径都会走到），`run.go:103` 注入的回调是 `alertManager.LogStatus("已更新")`，而 `alertset.go:113-121` 会按已启用渠道输出 INFO；该日志经 `MultiHandler`（`app/logutil.go`）进入 WebUI 日志流，因此启用渠道时 ticker/trigger 每轮都会输出与配置变化无关的“告警已更新”。
- **最终方案：** 不新增可变 generation。Run 保存上次已消费的不可变 `RuntimeState` 指针（以 Run 起始快照为基线），只在指针变化且 Run 真正消费状态后触发 hook；普通轮次不触发。启动“已启用”（`run.go:102`）保持独立。
- **必须保持：** hook 仍是“Run 已消费状态”的屏障（`sync:start`/导入相关既有用例依赖该语义，且 control 通知可合并、hook 次数允许少于 ApplyState 次数）；日志不得包含 SMTP 密码或 Webhook URL。
- **回归：** 多轮无配置变化时 hook 次数不增长（修复前随轮次增长）；ApplyState 一次后只增加一次，且此刻 `IsEnabled()` 已反映新状态。`main_test.go` 中“PUT 后出现 Webhook 告警已更新”的既有断言必须继续通过。

**实施记录（批次 4，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** 按条文「不新增可变 generation」，`Syncer` 新增受 `mu` 保护的 `applied *RuntimeState` 字段（`New` 用初始快照初始化），并新增 `notifyStateApplied(state)`：**指针相同则直接返回，不触发 hook**；指针变化才置位并调用 `logAppliedState`。Run 循环末尾改为 `s.notifyStateApplied(latest)`，不再是无条件 `s.logAppliedState(latest)`。
- **首次尝试被既有测试判负（如实记录）：** 最初把基线放在 Run 局部变量并初始化为起始快照，导致 `TestStaleTriggerAfterPauseDoesNotAddRound` 失败（`启用态未应用`）——因为该用例依赖「启用态 ApplyState 后 hook 至少触发一次」这一屏障语义。改为上述字段方案后，基线在启动时为起始快照、每次新指针消费后推进，既满足 A13 又保持屏障语义。**这正是既有判别性回归拦住回归的实例。**
- **必须保持项复核：** hook 仍是「Run 已消费状态」的屏障（`TestStaleTriggerAfterPauseDoesNotAddRound`、`TestResumeImmediateRoundAfterPhaseMirrorAdvance`、`webui/api/import_runtime_test.go` 的 `TestConfigImportSyncEnabledRuntimeConsistency` 继续通过）；control 通知仍可合并、hook 次数允许少于 ApplyState 次数；日志仍不含 SMTP 密码与 Webhook URL（根包 `TestProcessSecretsNotLogged` 继续通过，含「PUT 后出现 Webhook 告警已更新」断言）。
- **判别性测试（`syncer/state_applied_hook_test.go` 新增）：** `TestStateAppliedHookNotFiredByOrdinaryRounds`——20ms 间隔跑多轮，断言 hook 次数在无配置变化时**不增长**且不超过 1；`TestStateAppliedHookFiresOncePerApplyState`——暂停态不触发；`ApplyState` 一次恰好 +1，且触发时 `IsEnabled()` 已反映新状态；随后普通轮次不再增长。
- **红灯证据（修复前必失败）：** 临时把 `notifyStateApplied` 改回无条件 `s.logAppliedState(state)`，`go test ./syncer/ -run TestStateAppliedHookNotFiredByOrdinaryRounds -count=1` **FAIL**：`无配置变化时 hook 次数不得增长: 1 → 4（修复前随轮次增长）`。随后从 `/tmp/fwa-b4-syncer-final.go` 恢复，`shasum -a 256 syncer/syncer.go` = `2c26bc8af3008df55d5bc45e89d717bfc71c2b00da6082b3c55505f5d337e9bb` 一致，**未使用任何破坏性 Git 命令**。
- **门禁：** 同批次 4。


#### A15｜低｜SSE 忽略写错误，半开连接缺少单次写出边界

- **状态与判定：** 已完成两阶段修复：批次 4 先收口可观察写错误；2026-09-28 第二阶段再为初始 Flush 与每条消息建立 5 秒单次写出边界，并由真实 loopback TCP 停止读取测试验证。
- **原始证据：** 修复前 `webui/api/sync.go` 与 `webui/api/logstream.go` 在 `fmt.Fprintf` 后直接 `Flush`，两者返回值均未检查，且全仓无 `http.ResponseController`；全局 `WriteTimeout` 按长连接契约保持零。第一阶段之后虽能在错误可观察时退出，但客户端保持连接并停止读取时仍可能阻塞在 Write/Flush，不能回到 select 检查取消信号。
- **最终方案：** 共用 `http.ResponseController` 可检查写出路径；初始 Flush 与每次 `Write + Flush` 前设置 5 秒 deadline，完成后清除；写头前严格检查 Flush/deadline 能力，不支持时安全返回 500；响应开始后的任一错误立即返回并由既有 `defer unsubscribe()` 取消订阅，**不得再写第二个响应头或 500**。
- **必须保持：** 不设置全局 `WriteTimeout`；继续监听 request context 与服务器 shutdown channel；取消订阅不关闭 EventBus channel；`LogBroadcaster` 的订阅关闭语义不变。
- **修改与回归范围：** 两类 SSE handler、共用 helper 与测试。自写 erroring/记录型 `http.ResponseWriter` 覆盖首错退出、初始 Flush、能力拒绝、deadline 设置/清除；真实 loopback TCP 用例覆盖客户端保持 socket 但停止读取的写入背压。
- **外部边界：** loopback TCP 停止读取已验证；真实 WAN、反向代理异常、操作系统故障型经典半开 TCP 与远端 CI/GHCR 仍未验证，不得外推为通过。

**实施记录（批次 4，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** 新增 `webui/api/sse.go`，提供两类 SSE 共用的两个 helper：`writeSSE`（`fmt.Fprintf` 的返回错误**必须**检查；随后用 `http.NewResponseController(w).Flush()` 刷新并检查错误）与 `probeSSE`（写响应头**之前**的能力判定）。`webui/api/sync.go` 与 `webui/api/logstream.go` 的 handler 改用它们：首个写错误即 `return`，由既有 `defer unsubscribe()` 取消订阅；**初始 Flush 失败时直接返回，不再写第二个响应头或 500**；错误只记 `Debug`/`Warn` 安全日志，不回显正文。
- **关于 deadline（按条文「若加入…必须…」）：** 本批**未**加入单次写 deadline，因此只需覆盖「可检查的写出/刷新路径 + 首错返回 + 初始 Flush 失败直接返回」三项必做要求；未设置任何全局 `WriteTimeout`（`TestServerTimeoutContract` 继续通过），继续监听 request context 与服务器 shutdown channel，取消订阅仍不关闭 EventBus/LogBroadcaster 的 channel。
- **实现细节（经实测修正）：** 能力判定**不能**只用 `http.ResponseController.Flush()`——实测对不实现 `http.Flusher` 的 writer 它返回 `http.ErrNotSupported`（与「支持 deadline 但不支持 flush」无法区分），因此 `probeSSE` 改为显式断言 `http.Flusher`（保留原实现的判别语义），`ResponseController` 只用于**写出/刷新的错误检查**。
- **判别性测试（`webui/api/sse_test.go` 新增）：** `TestHandleSyncEvents_WriteErrorExitsAndUnsubscribes` 与 `TestHandleLogStream_WriteErrorExitsAndUnsubscribes`——自写 erroring `http.ResponseWriter`（`Write` 返回 `io.ErrClosedPipe`、`Flush` 成功）断言：首个写错误后 handler **立即退出**、`unsubscribe` **恰好调用一次**、活跃订阅归 0、**没有写第二个响应头**（记录 `WriteHeader` 调用次数与状态码）。修复前该用例会因 handler 永不退出而超时失败。`TestHandleSyncEvents_NoFlushCapabilityReturns500` 断言能力检测在写头之前完成且**不建立订阅**；`TestProbeSSE_DetectsCapabilityAndUnwraps` 覆盖支持/不支持/嵌套三层形态。既有 `TestHandleLogStream_ServerShutdownExitsSubscriber`、`TestHandleLogStream_ContextCancelExitsSubscriber`、`TestHandleSyncEvents_ContextCancelUnsubscribes`、`TestHandleSyncEvents_ClientDisconnectUnsubscribes`、`TestHandleSyncEvents_ServerShutdownExitsSubscriber` 继续通过。
- **门禁：** 同批次 4。
- **未执行（如实保留）：** 真实半开 TCP / 客户端停止读取的人工验证。本批**不**把该场景写成已验证。

**真实半开 TCP / 停止读取客户端研究补充（2026-09-28；研究完成，以下边界随后已由用户授权实施）：**

- **研究时合同与证据边界：** A15 当时合同是：`Write` 或 `Flush` 一旦返回可观察错误，SSE handler 立即退出，并由既有 `defer unsubscribe()` 取消订阅。错误 `ResponseWriter` 测试覆盖了 `Write` 返回 `io.ErrClosedPipe` 后首错返回、无第二个响应头和恰好一次取消；正常断开、请求 context 取消及服务器 shutdown 也已有本地订阅退出证据。这些证据证明“错误已被观察到时”的处理，不证明所有停滞 TCP 连接都会在固定时间内退出。
- **研究时停止读取限制：** 客户端可以保持 socket 打开但停止读取 SSE 数据；服务端 TCP 发送缓冲区逐渐填满后，某次 `Write`/`Flush` 可能等待缓冲区空间而长期阻塞。当时没有单次 SSE 写 deadline，因此 handler 可能无法回到 `select` 检查 request context 或 server shutdown 信号。服务器级 shutdown 上限不等于该次底层写入已经有界返回。
- **影响定性：** 这是资源占用、handler 退出时延和当前证据边界问题，不是已经确认的数据正确性问题，也没有证据表明它会造成云防火墙规则故障。RST、正常断开或错误 `ResponseWriter` 的测试不能冒充“客户端保持连接但停止读取”的真实半开/停滞连接验证。
- **术语澄清：** 经典 TCP“半开”通常指一端已丢失或连接状态不一致而另一端尚未获知；本研究同时覆盖的“socket 仍建立、应用层停止读取”更准确地称为慢客户端/写入背压导致的停滞连接。两者对服务端的共同风险是：底层写入可能暂时没有可观察错误。
- **研究时裁决边界：** 若保持当时合同，应继续记录为“对客户端停止读取无有界退出保证/未验证”，不得标记已修复或已通过。若要完整关闭缺口，需用户另行授权生产代码改动：为初始 `Flush` 及每次 SSE 写出设置、按写刷新并在完成后清除 deadline；同时明确 deadline 时长、`http.ErrNotSupported` 的处理策略和日志语义，并补充真实停止读取客户端测试。该授权已在下述第二阶段实施中给出，本段保留为实施前研究证据。

**有界退出第二阶段实施记录（2026-09-28；起点 HEAD `747dbb4`，当前工作树）：**

- **最终合同：** `webui/api/sse.go` 将单次 SSE 写出上限固定为 5 秒；能力探测、初始响应头 `Flush`、每条消息的 `Write + Flush` 均通过 `http.ResponseController.SetWriteDeadline` 实施，完成或失败后都尝试清除 deadline。这里保证的是“某次写入进入背压后约 5 秒内返回”，不保证无事件写出时主动识别客户端停止读取；未新增 heartbeat，`http.Server.WriteTimeout` 继续保持零值。
- **严格能力边界：** 两类 handler 在写响应头和建立订阅之前同时检查 Flush 与写 deadline 能力；不支持或探测后无法清除 deadline 时返回安全 HTTP 500 `SSE 不可用`，不静默降级。响应开始后的初始刷新、消息写入、刷新或 deadline 清理任一失败均直接退出，不再写第二个响应头；能力失败/初始刷新失败记 WARN，已建立连接后的写错误保持 DEBUG，日志不含 SSE 正文。
- **红灯证据：** 新测试先在未修改生产实现上运行。`TestWriteSSE_SetsAndClearsDeadline` 失败为 `调用顺序 = [write flush], want [deadline:set write flush deadline:clear]`；两类 `NoDeadlineCapabilityReturns500` 证明旧实现仍进入 SSE（同步事件实际建立 1 次订阅、状态码为空且 Content-Type 已为 `text/event-stream`）；`TestHandleSyncEvents_StoppedTCPReaderExitsAtWriteDeadline` 使用真实 loopback TCP、1 KiB 服务端写缓冲和 16 MiB 事件，客户端只读响应头后保持 socket 打开且停止读取，旧实现在 7 秒内未取消订阅而失败。
- **判别性回归：** `TestWriteSSE_SetsAndClearsDeadline` 固定 `deadline:set → Write → Flush → deadline:clear` 顺序并检查约 5 秒值；`TestHandleSyncEvents_InitialFlushErrorExitsAndUnsubscribes` 覆盖初始 Flush 超时、清理 deadline 与恰好一次取消；同步事件/日志流各有不支持 deadline 时写头前 500；真实 TCP 停止读取用例在修复后约 5 秒触发 handler 返回并恰好取消一次订阅。原有 broken-pipe、request context、真实客户端断开和 server shutdown 用例继续通过，测试 recorder 仅补齐生产 `ResponseWriter` 已具备的 deadline 能力。
- **自动门禁：** A15 专项 race 通过；`go test ./webui/api -race -count=1 -timeout=60s` 通过；`go test ./... -race -count=1` 11 包通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过。未改前端源码、依赖、Docker 或云协议，因此未运行 npm、Docker、真实云、SMTP 或 Webhook 验收。
- **真实运行与浏览器：** 使用隔离 `FWALIZER_DATA_DIR` 和当前真实二进制启动 WebUI。curl 对 `/api/logs/stream` 保持 8 秒、对 `/api/sync/events` 保持 10 秒，均为 HTTP 200 且未被 5 秒 deadline 当作连接总时长切断；手动 trigger 收到原格式 `sync:start` / `sync:complete`。真实浏览器打开当前 `frontend/dist` 的同步日志页，空闲 15 秒后连接仍在，并实时显示随后触发同步产生的三条日志；SIGINT 后 HTTP 与同步引擎正常收尾。
- **仍未验证：** 未在真实 WAN、反向代理异常、操作系统故障型经典半开 TCP 或远端 CI/GHCR 上验证当前改动。这些边界不得改写为已通过；但本地真实 socket“客户端保持连接但停止读取”已经取得判别性证据，不再属于未验证项。


### 批次 5：SQLite、损坏数据与错误处理

#### A4｜中高｜SQLite `busy_timeout` 未覆盖每条物理连接

> 本小节原方案与实施结果为 A4 历史记录；其中拒绝 `_txlock` 的理由已由下方 2026-10-02 P3-09 方案 A 补记订正，不再作为当前边界。

- **状态与判定：** 确认存在，置信度高。
- **当前证据：** `config/store.go:64` 以普通路径打开数据库（`sql.Open("sqlite", path)`），`:74` 只执行一次 `PRAGMA busy_timeout=5000`；该 PRAGMA 是连接级，`sql.DB` 之后新建的物理连接为 0，竞争时立即 `SQLITE_BUSY`。全仓未设置任何连接池上限（无 `SetMaxOpenConns`）。隔离复现显示后续连接为 0，竞争时立即 `SQLITE_BUSY`。
- **影响：** 同步日志、扫描结果与协调器事务可并发；非首连接可能立即失败。当前证据不表示数据损坏。
- **最终方案：** 用 `net/url.URL` + `url.Values` 构造 SQLite file URI，以 `_pragma=busy_timeout(5000)` 保证每条连接生效；禁止直接拼接。WAL 继续在打开后执行一次，不让每条连接重复切换。
- **本轮新增实施要求（复核发现，必须一并处理）：**
  1. 构造 URI 前必须 `filepath.Abs`（并按需 `filepath.ToSlash`）：`url.URL{Scheme:"file", Path: rel}` 对相对路径会输出 `file://rel`，SQLite 会把它当作 authority 并报 `invalid uri authority`，导致 `FWALIZER_DATA_DIR` 为相对值或 `DefaultDataDir()` 回退 `"."` 时启动失败。
  2. 同一改动顺带消除一个既有缺陷：当前纯路径 DSN 遇到含 `?` 的路径会被驱动截断（`modernc.org/sqlite` 的 `conn.go:71-72` 在非 `file:` 前缀时执行 `dsn = dsn[:pos]`），例如 `/data/a?b.db` 实际打开 `/data/a`，静默写到另一个文件且无任何报错；改用 file URI 后该路径必须落到预期文件。
  3. 不要把 `journal_mode(WAL)` 或 `_txlock` 加进 `_pragma`：前者按契约只在打开后设置一次，后者会让导出/启动的只读事务申请写锁。
- **必须保持：** `_pragma` 只承载 `busy_timeout(5000)`；WAL 只设一次；不新增连接池上限设置（超出本项范围）；`OpenStore` 失败仍返回 error；不改变 404/409/413/500 语义。
- **修改与回归范围：** `config/store.go` 与测试。覆盖含空格、`#`、`?`、非 ASCII 的路径（断言文件确实建在预期路径而非截断路径）；固定 4 条并发持留连接逐条断言 5000ms（修复前恰为 1×5000 + 3×0）；写锁竞争等待约 5s 而非立即失败——**竞争用例必须走 autocommit 或 `BEGIN IMMEDIATE`**，WAL 下 deferred 事务先读后写会得到 `SQLITE_BUSY_SNAPSHOT`（不受 busy handler 约束，会让修复后用例仍然失败）。

**实施记录（批次 5，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `config/store.go` 新增 `sqliteDSN(path)`：`filepath.Abs` → 逐字符转义 `%`/`#`/`?` → `?_pragma=busy_timeout(5000)`；`OpenStore` 用该 DSN + `sql.Open("sqlite", dsn)`，并**删除**打开后的一次性 `PRAGMA busy_timeout=5000`（改由驱动在每条新连接上执行）。WAL 仍只在打开后设置一次。
- **两个顺带修复的既有缺陷（按条文要求一并处理）：**
  1. 相对路径：先 `filepath.Abs`（并在 Windows 上 `filepath.ToSlash`），避免 `file://rel` 触发 `invalid uri authority`。
  2. 含 `?` 的路径：`file:` 前缀让驱动不再执行 `dsn = dsn[:pos]` 截断，`/data/a?b.db` 不再被静默打开成 `/data/a`。
- **未做（按条文）：** `_pragma` **只**承载 `busy_timeout(5000)`；未加入 `journal_mode(WAL)`（WAL 只在打开后设一次）；未使用 `_txlock`（避免只读导出事务申请写锁）；未新增连接池上限；`OpenStore` 失败仍返回 error；404/409/413/500 语义未变。括号刻意保留为字面量（未走 `net/url` 查询编码），以保持 `busy_timeout(5000)` 可读。
- **判别性测试（`config/store_dsn_test.go` 新增）：**
  1. `TestOpenStoreBusyTimeoutOnEveryConnection`——先同时持留 **4** 条连接再逐条读 `PRAGMA busy_timeout`，断言全部为 5000（修复前恰为 1×5000 + 3×0）。
  2. `TestConcurrentConnectionsAllHaveBusyTimeout`——8 条并发连接全部为 5000。
  3. `TestOpenStorePathsWithSpecialCharacters`——空格、`#`、`?`、中文、`?`+`#` 混合共 6 个路径，断言业务写入成功且文件恰好落在预期路径（修复前 `?` 之后被截断）。
  4. `TestOpenStoreRelativePathUsesAbsFileURI`——相对路径可打开且落在预期位置。
  5. `TestSQLiteDSNShape`——断言 `file:` 前缀、`_pragma` 仅出现一次且只承载 `busy_timeout(5000)`、不含 `journal_mode`/`_txlock`、`?`/`#` 已转义为 `%3F`/`%23`。
  6. `TestBusyTimeoutWaitsInsteadOfFailingImmediately`——按条文要求走 `BEGIN IMMEDIATE`：持有写锁时另一条连接的写入**等待 5.103s** 后才返回 `database is locked (5) (SQLITE_BUSY)`（修复前非首条连接 busy_timeout=0，会立即失败）。
- **红灯证据（修复前必失败）：** 在 `/tmp/fwa-a4-proof` 用独立程序对比同一驱动同一版本：含 `?` 路径下**旧实现**「预期文件存在=false / 截断文件存在=true」，**新实现**反之；四条**同时持留**连接下**旧实现** `busy_timeout = [5000 0 0 0]`，**新实现** `[5000 5000 5000 5000]`。与条文的「恰为 1×5000 + 3×0」完全一致。
- **门禁：** `go test ./config/ ./webui/api/ ./webui/ . -race -count=1` 通过；全仓 11 包 race 通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过。
- **外部边界（如实保留）：** 未访问真实生产 SQLite；未验证真实旧库的迁移路径。


**2026-10-02 P3-09 后续实施补记（用户选择方案 A）：**

- 固定驱动 `modernc.org/sqlite v1.54.0/tx.go` 对 `ReadOnly=true` 跳过 beginMode，故 A4 历史“会让导出/启动只读事务申请写锁”理由不成立。历史描述与当时测试证据保留；本补记替代禁止 `_txlock` 的理由和 DSN 形状边界。
- 当前 DSN 为 file URI + `?_pragma=busy_timeout(5000)&_txlock=immediate`；`_txlock` 是独立驱动参数，不放入 `_pragma`。A4 的每连接 5000ms、路径转义和一次性 WAL 继续有效，未新增连接池上限。
- `Store.BeginTx` 与通用写事务（含迁移/扫描）在首次读取前预留写事务；ReadOnly 导出/启动快照仍可与写事务并行。修复实际引用检查先读后写与独立日志/扫描提交之间的 517 竞争，不改 API 错误分类、事务回滚与提交后一次发布。
- 正式 DSN 结构检查、写锁预留/入口等待/超时恢复、只读快照、日志/扫描完整落库与 coordinator 取消/发布回归已落地。定向 race 20 轮、5 秒慢例、受影响两包 race 与 vet/build 通过；三类仓库外正式 overlay 负向控制全部按行为断言变红。全量 12 包 race 一轮、受影响 Go 文件 gofmt 检查与 diff-check 均通过，完整证据见 [审计报告 P3-09 当前实施补记](./fwalizer-audit-final1.md)。
- 保留边界：超过 5 秒占锁仍可能返回 BUSY/API 500，重试不保证成功；context 不承诺毫秒级取消，但取消零提交/零发布及恢复已覆盖；ReadOnly 非驱动强制拒写。Go `1.26.6 darwin/arm64`，未执行 Go 1.25/Linux、前端/浏览器、Docker、真实云/通知链路或远端 CI。源码/测试/文档尚未提交；本地结果不外推长期稳定或外部验收。


#### A9｜中｜损坏的 `rules.targets` 被静默扩大为全部目标

- **状态与判定：** 确认存在，需外部改库或损坏触发；后果具有安全方向影响。
- **当前证据：** `config/store.go:224-229` 的 `loadRules` 在 `targets != ""` 时解析 JSON，解析失败不报错、静默保留 nil；空 Targets 被解释为全部目标（`syncer.go:558-560`）。`null` 会被 `json.Unmarshal` 成功解成 nil 切片，因此当前与 `""` 一样被当成全部目标；损坏还会绕过引用检查（`ReferencingRuleIDsTx` 同样经 `loadRules`）并在导出/导入中固化为 `[]`（`bundle_v2.go` + `export.go`）。物理 SQL NULL 现在会在 `Scan` 阶段报 `unsupported Scan … into type *string`，该错误**不含规则 ID**。**历史事实（本轮新增证据）：** `33649b1`/`ad288c0` 时期的写路径是 `json.Marshal(r.Targets)`，nil slice 会写出字面量 `null`，直到 `c289744`（Step 4）才改为 `[]`——即存量库中可能存在**合法产生**的 `targets='null'` 行。
- **最终四态合同（2026-09-27 用户裁决，§六.4 F1：保持严格口径）：** 历史空串 `""` 兼容为全部目标；`[]` 是合法全部目标；`null`、对象、标量、非整数数组及解析失败全部返回带规则 ID、但不含原值的内部错误。`null` 不得视为 `[]`。
- **错误边界：** 损坏必须阻断同步、导出、完整快照和目标引用检查；返回安全 500，不静默放行或扩大。四条链（同步快照、导出、引用检查、启动加载）经复核都能把 `loadRules` 错误升到安全 500 或启动中止，无上游吞错。存量库若含 `null` 或损坏值，升级后 fail-closed 是**预期行为**；发布说明必须给出修复提示：`UPDATE rules SET targets='[]' WHERE targets IS NULL OR targets='null';`，或直接重新导入 version 2 配置包（导入先清空 rules 再写入，可修复损坏库）。
- **实现要点：** `Scan` 改 `sql.NullString` 以覆盖物理 NULL；用 `nums == nil` 区分 `null` 与 `[]`（`[]` 解出非 nil 空切片），无需回显原值；错误文本只带 `#id`，HTTP body 仍是既有安全文案。
- **修改与回归范围：** `config/store.go` 与 config/syncer/webui-api 测试。直插所有形态（`''`、`'[]'`、`'null'`、`'{}'`、`'1'`、`'[1,"a"]'`、NULL、`'[1]'`）并验证四条调用链；补 `GET /api/rules` 的 500 安全文案回归（`/api/targets` 已有同类用例）；断言错误文本含规则 ID 且不含原值。

**实施记录（批次 5，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `config/store.go` 的 `loadRules` 改为 `Scan` 到 `sql.NullString`（覆盖物理 NULL），并抽出 `parseRuleTargets(ruleID, raw)` 落实**四态严格口径**：`""` → 兼容为全部目标（返回 nil）；`"[]"` → 合法全部目标（非 nil 空切片）；`json.Unmarshal` 失败 → 内部错误；解出 `nil` 切片（JSON `null`）→ **单独报错，不得视为 `[]`**；`!raw.Valid`（物理 NULL）→ 内部错误。所有错误文本只带 `#id` 与损坏事实，**不回显原值**。
- **错误传播复核：** 四条链都能把 `loadRules` 的错误升到安全 500 或启动中止——`LoadBusinessSnapshot`（同步快照与导出共用）、`LoadConfig`（启动）、`ReferencingRuleIDsTx`（目标引用检查）、`BeginReadOnlyTx` + `loadRules`（导出）。未在上游添加任何吞错分支。
- **判别性测试（`config/store_corrupt_test.go` 新增）：**
  1. `TestLoadRulesTargetsFourStateContract`——直插 `''`/`'[]'`/`'null'`/`'{}'`/`'1'`/`'[1,"a"]'`/`'[1,'`/`NULL`/`'[1]'` 九种形态，逐一断言成功或内部错误；每条错误都断言**含规则 ID**、且**不含该用例的原始值回显**（`{}`、`[1,"a"]`、`[1,` 等）。
  2. `TestCorruptTargetsBlocksAllFourPaths`——一条 `'null'` 损坏规则必须同时阻断四条链。
  3. `webui/api/rules_test.go:TestRuleReadCorruptTargetsUsesSafe500`——通过真实 `GET /api/rules` 路由加载损坏的非整数 `targets`，断言 500、精确 JSON Content-Type、仅含安全 `error` 字段，响应无损坏原值/规则 ID/内部诊断/部分合法规则，服务日志含动作、规则 ID 与损坏类别但不含原值，且失败只读路径 `applyCount=0`。
- **错误文本实测（安全文案示例）：** `规则 #1 的 targets 为 JSON null（数据已损坏，不得视为全部目标）`、`规则 #1 的 targets 不是合法的整数数组（数据已损坏）`、`规则 #1 的 targets 为 SQL NULL，数据已损坏`。
- **发布说明（F1 裁决要求，已落地）：** `README.md` 新增「从旧版本升级：`rules.targets` 数据损坏的修复提示」，明确 fail-closed 属**预期行为**，并给出两种修复方式：`UPDATE rules SET targets='[]' WHERE targets IS NULL OR targets='null';`，或重新导入 version 2 配置包（导入先清空 rules 再写入，可顺带修复）。
- **门禁：** 2026-09-28 独立测试代理执行：`go test ./webui/api -run '^TestRuleReadCorruptTargetsUsesSafe500$' -count=1`、`go test ./config -run '^(TestLoadRulesTargetsFourStateContract|TestCorruptTargetsBlocksAllFourPaths)$' -count=1`、`go test ./webui/api ./config -race -count=1`、`go test ./... -race -count=1`（11 个包）均通过；`go vet ./...`、`go build ./...`、`git diff --check` 均通过。本项无需前端构建、浏览器、Docker 或外部服务，均未执行且不适用。
- **未执行（如实保留）：** 未以真实生产旧库验证损坏 targets 的升级行为。


#### A14｜低｜Rollback、Encode 与日志裁剪错误未完整处理

- **状态与判定：** 确认存在，违反 AGENTS “所有 error 必须处理”；多数触发条件苛刻。
- **当前证据：** `config/store.go:291-301` 的 `WithTransaction` 忽略 Rollback 返回值、无 defer，`fn` panic 时事务与连接不会被回滚（`*sql.Tx` 没有 finalizer，WAL 下泄漏的写事务会让后续写入持续 BUSY）；`webui/api/deps.go:199-203` 的 `writeJSON` 忽略 `Encode` 返回值（全仓 36 处调用）；`config/store.go:631` 的 `AddSyncLog` 吞掉 COUNT 错误。**范围修正（本轮复核）：** 裁剪 `DELETE` 的错误**已经**返回（`:632-635`），Issue6 原表述“COUNT/清理错误可能被忽略”偏严，实际只有 COUNT 需要处理。
- **最终方案：** 使用 `committed` 标志 + defer 回滚，忽略 `sql.ErrTxDone`、记录其他安全错误，panic 时回滚后重新 panic；处理 COUNT 错误（安全日志，不改变裁剪语义）；`writeJSON` 处理 Encode 错误——响应头已发出后只记安全日志，不伪造第二个 HTTP 错误响应（参照 `export.go:74-77` 的既有先例）。
- **明确不做：** 不因本项引入新框架或强行增加 errcheck 门禁；不新增连接池上限；不改裁剪阈值与语义。
- **修改与回归范围：** `config/store.go`、`webui/api/deps.go` 与测试。覆盖 panic 回滚（断言后续写入不 BUSY、旧行未被写入）、已结束事务（`sql.ErrTxDone` 忽略）、裁剪 COUNT 查询失败（注入）与 1001 条裁剪回归、失败 ResponseWriter（自写 stub，断言不 panic 且不第二次 `WriteHeader`）。

**实施记录（批次 5，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：**
  1. `config/store.go` 的 `WithTransaction` 改为 `committed` 标志 + `defer` 回滚：正常提交后不再回滚；`fn` 返回错误或 **panic** 时都会回滚（panic 继续向上传播）；`sql.ErrTxDone` 属预期被忽略；其他回滚失败记 `Error` 安全日志。
  2. `config/store.go` 的 `AddSyncLog` 处理 COUNT 错误：查询失败时记 `Warn` 并**跳过本次裁剪**（保持「查询失败就不裁剪」的保守语义），不再静默吞掉；裁剪阈值 1000 与 `DELETE` 语句未变。
  3. `webui/api/deps.go` 的 `writeJSON` 检查 `Encode` 返回值：响应头与状态码已发出，因此**只记安全日志**（`Warn`，含 status 与 error），**不伪造第二个 HTTP 错误响应**（参照 `export.go` 的既有先例）。
- **明确未做（按条文）：** 未引入新框架或 errcheck 门禁；未新增连接池上限；未改裁剪阈值与语义。
- **判别性测试（`config/store_error_test.go` 与 `webui/api/deps_test.go`）：** `TestWithTransactionPanicRollsBack`（panic 被重新抛出、旧值未被写入、**panic 后后续写入不 BUSY**）；`TestWithTransactionFinishedTxIsIgnored`（事务被提前回滚后再 Commit 的错误被正确处理，Store 仍可用且无 panic）；`TestAddSyncLogTrimsOverLimit`（1001 条 → 恰 1000 条）。本轮补充的 `TestAddSyncLogCountFailureDoesNotFailWrite` 使用 `sql.OpenDB` + 标准库 `driver.Connector`：INSERT 返回成功，COUNT 返回非 `driver.ErrBadConn` 的哨兵错误；加锁调用记录断言严格为一次 INSERT 后一次 COUNT、无 database/sql 重试或 DELETE，并校验参数、`AddSyncLog` 成功返回及安全日志脱敏。`TestWriteJSONEncodeFailureLogsWithoutSecondHeader` 使用先写出非零短前缀、再返回 `io.ErrClosedPipe` 的 `ResponseWriter`，断言 Content-Type、单次状态头、`header → write` 顺序、单次 Write、部分响应体及不含业务 payload 的安全错误日志，能判别错误二次响应。本轮仅补充失败分支测试，未修改生产代码；既有实现已在测试前存在，**无先红证据**。
- **独立门禁（2026-09-28）：** `go test ./config -run '^TestAddSyncLogCountFailureDoesNotFailWrite$' -count=1`、`go test ./webui/api -run '^TestWriteJSONEncodeFailureLogsWithoutSecondHeader$' -count=1`、`go test ./config -run '^TestAddSyncLogTrimsOverLimit$' -count=1`、`go test ./config ./webui/api -race -count=1`、`go test ./... -race -count=1`（11 个包）、`go vet ./...`、`go build ./...` 与 `git diff --check` 均通过。该项不涉及前端、浏览器、Docker、真实 SQLite 故障环境或外部服务，均未执行且不适用。


#### A17｜低｜非预期迁移失败只告警后继续

- **状态与判定：** 确认存在；两条 ALTER 对旧库仍有作用，不能删除为“永久无用”。
- **当前证据：** `config/store.go:153-159` 的两条 ALTER（`rules ADD COLUMN enable_ipv6`、`alert_webhook ADD COLUMN channel`）对非 duplicate-column 错误只 `slog.Warn` 后继续，函数末尾 `return nil`。这两条 ALTER 对应 Build1 时期的旧库形态（`CREATE TABLE IF NOT EXISTS` 不会给已存在表补列），因此仍然必要。
- **最终方案：** duplicate-column 继续视为幂等成功；其他 ALTER 错误立即从 `OpenStore` 返回并中止启动。不引入 `schema_migrations` 表。
- **实施建议：** 把 `initTables` 拆成 `createSchema(q DBTX)` 与 `migrateColumns(q DBTX) error`，测试用返回哨兵错误的假 `DBTX` 直接注入非预期失败（比“用 VIEW 顶替 rules 表”等取巧更确定），保持 duplicate-column 仍视为成功。
- **必须保持与回归：** 新库、已迁移库、已有表但缺列的旧库都能正确打开；覆盖 duplicate-column 与可控非预期失败两类路径，并断言非预期失败时 `OpenStore` 返回错误而不是返回可用 Store。

**实施记录（批次 5，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** `config/store.go` 把 `initTables` 拆为 `createSchema`（建表，失败返回错误）与 `migrateColumns(q DBTX) error`：两条 ALTER 逐条执行，`duplicate column` **视为幂等成功并继续**；**其他任何错误立即返回**（错误文本包含具体列名与底层原因）——`OpenStore` 随即关闭连接并返回该错误，**中止启动**。未引入 `schema_migrations` 表。
- **判别性测试（`config/store_error_test.go` 新增）：** `TestMigrateColumnsDuplicateIsIdempotent`（注入 `duplicate column name: enable_ipv6`，断言返回 nil 且两条 ALTER 都执行）；`TestMigrateColumnsUnexpectedFailureAborts`（注入 `disk I/O error`，断言返回错误、**保留底层原因**、且**首个失败后立即中止**）；`TestOpenStoreWorksForNewAndMigratedDB`（新库可写、再次打开命中 duplicate-column 仍成功且数据保留）。用假 `DBTX` 直接注入哨兵错误，比「用 VIEW 顶替 rules 表」更确定。
- **红灯证据（修复前必失败）：** 用「同名 VIEW 顶替 alert_webhook 表」制造非 duplicate-column 失败，实测新实现返回 `迁移列 rules.enable_ipv6 失败: SQL logic error: no such table: rules (1)` → `OpenStore` 会中止启动；修复前该分支只 `slog.Warn` 后就 `return nil`，Store 会以不完整 schema 继续可用。
- **门禁：** 同批次 5。
- **未执行（如实保留）：** 未以真实旧库验证迁移失败路径。


### 批次 6：SWAS DROP 表达收尾（Dry Run、原因与展示）

#### A11｜中低｜SWAS 跳过 DROP 后仍计作新增成功

- **状态与判定：** 确认存在，置信度高。
- **当前证据：** `provider/ali_swas.go:117-135` 对 DROP 记 WARN 后 `continue`，混合批次照常提交、全 DROP 时 `len(fwRules)==0` 直接 `return nil`，返回值无法区分“全部写入成功”与“全部跳过”；`syncer/retry.go:76` 以 `len(diff.ToAdd)` 累加 `added`。隔离复现为 added=1、实际写入=0。**本轮新增证据：** 由于 SWAS 永远无法表达 DROP（`PlatformAPIDocs/AliyunSWASAPIGuide/CreateFirewallRules.md` 请求参数无 Policy 字段，只有 `ListFirewallRules.md` 能列出云端已有的 drop 规则），该虚增**每轮都会重复出现、永不收敛**；Dry Run 也会把它列为普通 `to_add`（`syncer.go:412-418`）。现有 `TestRequest_SWASPortSlashDropSkipAndDelete` 只断言“全 DROP 不发请求 + err==nil”，反而固化了错误语义。
- **最终方案：** 统一收敛 `Provider.CreateRules` 返回至少 `{Written, Skipped}`；其他 Provider 成功为 `len(rules),0`，SWAS 混合批次分别计数，全部 DROP 为 `0,N,nil`。added 只累计 Written；事件、SSE 与实时日志增加 skipped 并说明原因。
- **批次归属（2026-09-27 用户裁决，§六.4 F3）：** `CreateRules` 接口与四实现的 `{Written, Skipped}` 改动**前移到批次 4**，使 A18 的 `skipped`/`partial` 在同批即可端到端判别；**批次 6 只做收尾**：Dry Run 表达、跳过原因文案、事件/SSE/实时日志展示、前端展示与端到端用例。两部分证据都必须记录在本条目内。
- **持久化边界：** 本批不增加 `sync_logs.skipped` 列；历史 added 改为真实写入数。
- **Dry Run 合同：** SWAS DROP 表达为不可实施/跳过，不能继续进入普通 `to_add`；`DryRunResponse{results, warnings}` 包装与 `to_add`/`to_delete` 明细数组只能**追加**可选字段，不得改名或移除（AGENTS §十一 强要求）。
- **必须保持：** SWAS DROP 继续跳过；幂等语义不变；跳过不重试；不使用 SWAS 专属可选接口；CVM 的 100 条上限仍是**硬错误**而不是 skipped；ECS ICMPv6 与 SWAS IPv6 的既有跳过与 WARN 语义不变；TCP+UDP 拆分不计入 skipped。
- **修改与回归范围：** Provider 统一接口、四 Provider、retry、事件/日志、Dry Run 与测试；同步更新 6 个测试 fake（`provider/common_test.go`、`syncer/syncer_test.go`、`syncer/state_test.go`、`webui/api/import_runtime_test.go`）与 `provider/request_mock_test.go` 内的 `CreateRules` 调用点。覆盖全 DROP 与混合场景（provider 层计数、retrySync 层 added=Written、事件/SSE/sync_logs 一致、Dry Run 不再列出 DROP），并保留 CVM 上限与 ECS ICMPv6 的防回归断言。
- **未纳入本项的相邻问题：** 删除侧同型“跳过仍计数”、错误路径“已写却报 0”、`buildDesired` 静默丢弃（SWAS IPv6、ECS ICMPv6）不上报 skipped 等，见 §六.5，**本轮不授权实施**。
- **外部边界：** 真实 SWAS 仍需用户观察；mock 不等于真实云通过。

**实施记录（批次 6：收尾部分，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施（Dry Run 合同）：** `provider/provider.go` 新增 `SkippedRule{Action, Reason}`，`DiffResult` **只追加** `Skipped []SkippedRule`；`provider/common.go` 新增 `unsupportedReason(cloudType, action)`（当前唯一已知限制：SWAS 的 `CreateFirewallRules` 无 Policy 字段 ⇒ 无法表达 DROP）与 `RuleChange.SkipReason`（`json:"skip_reason,omitempty"`，只追加字段）。`Diff` 在计算 `to_add` 时把无法实施的期望规则**移出 to_add、放入 skipped**，因此 Dry Run **不再把它列为普通待添加**。`syncer.DryRunResult` 只追加 `Skipped []provider.RuleChange`；`DryRun()` 用原因填充。`retrySync` 在每轮 Diff 后 `skipped += len(diff.Skipped)`（与 Provider 写入阶段报告的 `Skipped` 互斥，不重复计数）。
- **已实施（展示）：** `EventDomainSyncComplete.Data` 增加 `skipped`；逐域名与整轮日志均带 `skipped`；`RoundSummary.Skipped` 经 `/api/sync/status` 与 `EventSyncComplete` 暴露（批次 4 已落地）。前端 `types.ts` 只追加 `RuleChange.skip_reason?` 与 `DryRunResult.skipped?`；`DryRunResults.vue` 在既有统计条**追加**「无法实施」一项（`NGrid :cols` 4→5）并在既有「待删除」表**之后追加**「无法实施（同步时跳过）」表（列 = 既有五列 + 跳过原因），既有 `to_add`/`to_delete` 的名称、结构与列**全部保持**。
- **必须保持项复核：** SWAS DROP 继续跳过且不重试；幂等语义不变；未使用 SWAS 专属可选接口；CVM 100 条上限仍是硬错误；ECS ICMPv6 与 SWAS IPv6 的既有跳过与 WARN 语义不变；TCP+UDP 拆分不计入 skipped；`DryRunResponse{results, warnings}` 包装与 `to_add`/`to_delete` 明细数组未改名、未移除（AGENTS §十一）；**未增加 `sync_logs.skipped` 列**；历史 added 已是真实写入数。
- **判别性测试：** `syncer/round_summary_test.go` 新增 `TestRetrySync_SkippedCountsDryRunSkipsWithoutToAdd`（全 DROP：`added=0`、`skipped=1`、**`CreateRules` 调用 0 次**）与 `TestDryRunDoesNotListSWASDropAsToAdd`（`to_add` 为空、`skipped` 恰 1 条且带原因；修复前该规则会出现在 `to_add`）；`provider/request_mock_test.go` 的 SWAS 用例（批次 4 已改写）继续覆盖「全 DROP `{0,N}` 且不发送请求」。CVM 上限与 ECS ICMPv6 的防回归断言继续通过。
- **门禁：** `go test ./provider/ ./syncer/ ./webui/api/ -race -count=1` 通过；全仓 11 包 race 通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过；前端 `npm run build`（`vue-tsc && vite build`）通过且 `dist` 已重建，两条阻断式 audit 均 **0 漏洞**。
- **未执行（如实保留）：** 前端新展示的**浏览器人工验收**；真实 SWAS 云上观察。mock 不等于真实云通过。


**实施记录（批次 4：接口部分，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

> 本条目跨批次交付（§六.4 F3）。以下是**批次 4 的接口部分**证据；Dry Run 表达、跳过原因文案、事件/SSE/实时日志展示、前端展示与端到端用例属**批次 6 收尾**，将在本条目内继续追加。

- **已实施（接口与四实现）：** `provider/provider.go` 新增 `CreateResult{Written, Skipped int}`（含语义注释：`Written` 只计真正提交的期望规则；`Skipped` 只计**因云端能力限制明确未实施**的规则；幂等「已存在」不计入 `Skipped`；不含 TCP+UDP 拆分的条数变化），`Provider.CreateRules` 签名改为 `([]config.RuleAction) (CreateResult, error)`。四个实现：`tc_lighthouse.go`、`tc_cvm.go`、`ali_ecs.go` 成功时恒返回 `{Written: len(rules), Skipped: 0}`（CVM 超上限仍返回**空结果 + 硬错误**）；`ali_swas.go` 逐条累计 DROP 跳过数——混合批次返回 `{Written: 提交条数, Skipped: DROP 条数}`，**全 DROP 时返回 `{Written:0, Skipped:N}` 且不发送请求**。
- **已实施（Syncer）：** `retrySync` 改为返回 `(added, deleted, skipped int, err error)`；`added += res.Written`（**不再** `added += len(diff.ToAdd)`），`skipped += res.Skipped`；`EventDomainSyncComplete.Data` 增加 `skipped` 字段；实时日志与整轮日志增加 `skipped`。
- **接口破坏面（精确清点，与 Issue6 一致并补齐）：** 1 个接口 + 4 个生产实现 + **6 个测试 fake**（`provider/common_test.go`、`syncer/syncer_test.go` 的 `stubProvider`/`countingProvider`/`fakeTagProvider`、`syncer/state_test.go`、`webui/api/import_runtime_test.go`）+ `provider/request_mock_test.go` 的 **12 处**调用点全部同步；由 Go 编译器穷尽报错确认无漏项。
- **判别性测试：**
  1. `provider/request_mock_test.go` 的 `TestRequest_SWASPortSlashDropSkipAndDelete` **改写**（原文只断言「全 DROP 不发请求 + err==nil」，反而固化了错误语义）：全 ACCEPT → `{2,0}`；**全 DROP → `{Written:0, Skipped:1}` 且请求数不变**；混合 → `{Written:1, Skipped:1}` 且只提交 1 条 ACCEPT。
  2. `syncer/retry_test.go` 新增 `TestRetrySync_AddedFollowsProviderWritten`——Provider 报告 `{Written:0, Skipped:1}` 而 `diff.ToAdd` 长度 1 时，断言 `added==0`（跟随 `Written`）且 `skipped==1`；若仍是 `len(diff.ToAdd)` 累加，`added` 会是 1。
  3. `syncer/round_summary_test.go` 的 `TestRoundSummary_OnlySkippedIsPartial` 端到端判别（Provider 层 → retry 层 → 事件/汇总层）。
- **必须保持项复核：** SWAS DROP 继续跳过且不重试；幂等语义不变；未使用 SWAS 专属可选接口；CVM 100 条上限仍是硬错误（`TestRequest_CVMLimitAndDeleteByPolicyIndex` 继续通过）；ECS ICMPv6 与 SWAS IPv6 的既有跳过与 WARN 语义不变；TCP+UDP 拆分不计入 `skipped`（拆分在 `buildDesired` 阶段完成，不经 `CreateResult`）。
- **持久化边界：** 未增加 `sync_logs.skipped` 列；`added` 已改为真实写入数。
- **门禁：** 同批次 4。
- **未执行（如实保留）：** 真实 SWAS 云上观察；mock 不等于真实云通过。

### 批次 7：平台边界与强要求措辞收口

本批同时完成 A8 的强要求措辞收口；A8 的代码状态与完整防回归合同统一见 §四，不在此重复建立第二份问题记录。A19 除代码与依赖收束外，还承担 AGENTS 平台约束句、README 平台声明与 pidfile 单实例回归（§六.4 F9）。

#### A19｜低｜按决策完全移除 Windows 兼容

- **状态与判定：** 产品决策已定，待实施；当前 Windows 代码可交叉编译，不是编译缺陷。
- **当前证据：** Windows 面共 4 处——`config/pidfile_windows.go`（`//go:build windows`，导入 `golang.org/x/sys/windows`，是全仓唯一 `x/sys` 使用者）、`config/pidfile_unix.go` 的 `//go:build !windows`、`config/deployment.go:70` 的 `case "windows"`（`%APPDATA%\fwalizer`）、`README.md:139` 的 Windows 数据目录行；`go.mod` 把 `golang.org/x/sys v0.46.0` 列为 direct require。其余平台相关代码（`syscall.SIGTERM/SIGINT`、`syscall.EADDRINUSE`）跨平台有定义，不属 Windows 门禁。发布与 CI 面向 Linux/Docker（`linux/amd64`）。
- **最终方案：** 删除 Windows pidfile、数据目录分支、README 承诺与仅为其存在的直接依赖；平台文件使用明确 `linux || darwin` 约束，不能简单删除 `!windows`。`go mod tidy` 后若 x/sys 仍被间接依赖则保留 indirect（`modernc.org/sqlite` 的 `rulimit.go` 在 linux/darwin 即导入 `x/sys/unix`，因此预计保留为 indirect）。
- **必须保持：** Linux/macOS 路径、`FWALIZER_DATA_DIR` 优先级、pidfile 单实例、Docker/CI `linux/amd64` 不变；不保留 Windows 门禁；**不得**给 `config/deployment.go` 加平台 build tag（会让 `runtime` 变为未使用并让 `default` 分支失去意义）；tag 必须精确写成 `linux || darwin`（漏掉 darwin 会让 `main_test.go` 在 macOS 上用宿主 GOOS 构建真实二进制时失败）。
- **文档与措辞（2026-09-27 用户裁决，§六.4 F9）：** 平台约束明确为 Linux/macOS——写入 AGENTS（新增平台约束句，并把 pidfile 处“平台文件”措辞收紧为“平台文件（linux/darwin）”）与 README（删除 Windows 行、新增平台声明与“不再支持 Windows”的发布说明）；仓库没有 CHANGELOG，发布说明的落点是 README 或 GitHub Release 正文。
- **修改与回归范围：** `config/pidfile_windows.go`（删除）、`config/pidfile_unix.go`（tag）、`config/deployment.go`、`README.md`、`AGENTS.md`、`go.mod`/`go.sum`、`.gitignore` 的 `fwalizer.exe` 条目；测试补 `config/pidfile` 侧回归。门禁除全仓四道外还必须包含：`go build ./...`（darwin 宿主）、`GOOS=linux go build ./...`、`GOOS=darwin go build ./...` 通过，且 `GOOS=windows go build ./...` **按预期失败**（`undefined: processExists`）并如实记录为“显式移除”的证据；`docker compose -f docker-compose.yml.example config --quiet` 与 `docker build -f build/Dockerfile` 继续作为容器侧门禁。
- **新增回归（本轮确认的缺口）：** 当前**没有任何用例**验证 pidfile 单实例——全仓无“启动第二个实例被拒绝”的测试，`pidfile.go` 的“FWAlizer 已在运行”分支无判别性覆盖。本批补一个进程级重复启动用例（真实二进制 + 真实 pidfile，断言第二个实例拒绝启动并提示 PID），以支撑“pidfile 单实例不变”这一必须保持项。

**实施记录（批次 7，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施：** 删除 `config/pidfile_windows.go`（全仓唯一 `golang.org/x/sys/windows` 使用者）；`config/pidfile_unix.go` 的 build tag 由 `!windows` 收紧为**精确** `linux || darwin`；`config/deployment.go` 删除 `case "windows"`（`%APPDATA%`）分支并加注释说明「刻意不加平台 build tag」（否则 `runtime` 变未使用、`default` 分支失去意义）；`README.md` 删除 Windows 数据目录行并新增「平台支持声明：仅支持 Linux 与 macOS，自本版本起不再支持 Windows」；`AGENTS.md` 新增「平台约束」条并把 pidfile 处措辞收紧为「平台文件（linux/darwin）」；`go.mod`/`go.sum` 经 `go mod tidy` 调整；`.gitignore` 删除 `fwalizer.exe` 条目。
- **依赖变化（与条文预测一致）：** `go mod tidy` 后 `golang.org/x/sys v0.46.0` 由 **direct** 变为 **indirect**（`modernc.org/sqlite` 在 linux/darwin 下导入 `x/sys/unix`），版本号未变；`go.sum` **无变化**。未执行任何 `go get` 升级。
- **判别性回归（新增，覆盖此前的空白）：** `main_test.go` 新增 `TestProcessSecondInstanceRejectedByPidFile`——用**真实二进制 + 真实 pidfile**：第一个实例正常运行并确认 pidfile 内容等于其 PID；第二个实例（同一 `FWALIZER_DATA_DIR`）必须以**非零**状态退出、输出「FWAlizer 已在运行」与已有 PID；随后断言**第一个实例仍可访问** `/api/health`（第二个实例未抢占端口/数据目录）；最后第一个实例 SIGTERM 正常退出并清理 pidfile。此用例补齐了 Issue6 指出的「全仓没有任何用例验证 pidfile 单实例」缺口。
- **门禁（按条文要求的平台与容器侧全部执行）：**
  - `go build ./...`（darwin 宿主）通过；`GOOS=linux go build ./...` 通过；`GOOS=darwin go build ./...` 通过；
  - `GOOS=windows go build ./...` **按预期失败**：`config/pidfile.go:22:7: undefined: processExists` —— 如实记录为「Windows 支持已显式移除」的证据；
  - `docker compose -f docker-compose.yml.example config --quiet` 通过；`docker build -f build/Dockerfile -t fwalizer:issue6-batch7 .` 通过；
  - 全仓 `go test ./... -race -count=1` 11 包通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过；前端 `npm ci && npm run build` + 两条阻断式 audit（均 0 漏洞）。
- **必须保持项复核：** Linux/macOS 数据目录路径与 `FWALIZER_DATA_DIR` 优先级不变；pidfile 单实例语义不变（新增回归覆盖）；Docker/CI `linux/amd64` 不变；未保留任何 Windows 门禁。
- **明确未做：** 不为 `config/deployment.go` 添加平台 build tag；不保留 Windows 运行验收（用户已决定移除支持）。


### 批次 8：低风险清理（无行为变更）

本批范围由 2026-09-27 用户裁决固定（§六.4 F9，并含 2026-09-27 的范围修正：**只移除 `Pause`/`Resume`，保留 `Runtime`**），只包含“零生产调用”的接口成员与零引用死代码，**不捆绑任何新功能**，且必须在删除前逐项重新证明无生产调用：

| 目标 | 位置 | 复核结论（2026-09-27） |
|---|---|---|
| API `Syncer` 接口成员 `Pause()`、`Resume()` | `webui/api/deps.go` | handler 只经协调器写入（A5 已修复），全仓无 `d.Syncer.Pause()/Resume()` 调用；`syncer` 侧实现必须保留 |
| API `Syncer` 接口成员 `Runtime()` | `webui/api/deps.go` | **依 2026-09-27 用户裁决（解释 A）保留**；复核结论维持「生产只用 `Deps.Runtime` 字段（`runtimeSnapshot()`），接口方法唯一调用点在测试且走具体类型」 |
| `DeleteRule`、`UpdateTarget`、`UpdateRule` | `config/store.go` | 非事务遗留入口，生产与测试均零引用（兄弟方法已在 `1fafb17` 删除） |
| `GetAlertEmailConfigTx`、`GetAlertWebhookConfigTx` | `config/store.go` | 零引用，注释声称的导入用途实际由 `LoadBusinessSnapshotTx` 承担 |
| `SettingsKeysV2()` | `config/runtime.go` | 零引用；被内部使用的 `settingsKeysV2` 保留 |
| `SyncDomainResult` | `provider/provider.go` | 零引用死类型（A11 的新结果结构在 `provider` 包内另建，不复用该类型） |

**必须保持：** 行为零变化；不得删除 `syncer` 的具体实现方法、辅助构建/验收代码、任何测试夹具（`testenv_test.go`、`request_mock_test.go`、`main_test.go`、`build/Dockerfile`、`webui/embed.go` 等）或 slog/JSON 接口方法；A10 的 reset 单一空对象契约只读不动。
**回归与记录：** 全仓四道门禁全绿；A5 的判别断言按 §四 A5 改写并如实记录“由运行时断言升级为编译期不可能 + 恰好一次 apply”；批次记录写入 §2.2 与 A5 条目。

## 四、已关闭问题与防回归合同

#### A5｜已修复｜pause/resume 双重运行时写入

- handler 已只通过协调器更新数据库与运行时，commit 后无二次 Pause/Resume。
- 保留数据库值、RuntimeState、apply 次数与 stub 未被调用的判别性断言。
- 批次 8 按 §六.4 F9 的用户裁决移除 API `Syncer` 接口中的无生产调用成员；**范围修正（2026-09-27 用户裁决，解释 A）：只移除 `Pause()`、`Resume()`，`Runtime()` 保留**；**不得**删除 `syncer` 侧的具体实现方法（Build6 明确要求保留）、辅助构建/验收代码或任何测试夹具。移除接口成员后，`sync_test.go` 中“handler 未二次调用 Pause/Resume”的断言已同步改写并如实记录：该保证从运行时断言升级为**编译期不可能**（stub 不再实现这两个方法），A5 的实质契约继续由数据库值 + 恰好一次 apply 的断言承担；另加源码级反向守卫 `TestAPISyncerInterfaceHasNoPauseResume` 防止成员被重新引入。

**实施记录（批次 8，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **删除前逐项重新证明无生产调用（已复核）：** `DeleteRule`、`UpdateTarget`、`UpdateRule`、`GetAlertEmailConfigTx`、`GetAlertWebhookConfigTx`、`SettingsKeysV2`、`SyncDomainResult` 各自在全仓只出现于**定义行与其文档注释**（无任何调用点）。顺带核对：`ruleColumns` 仍有事务内调用点（`config/store.go` 的规则插入路径），**保留**；`loadAlertEmail`/`loadAlertWebhook` 仍被 `LoadBusinessAlertConfig` 与固定键读取使用，**保留**。
- **已删除（零引用死代码，7 项）：** `config/store.go` 的 `DeleteRule`、`UpdateTarget`、`UpdateRule`、`GetAlertEmailConfigTx`、`GetAlertWebhookConfigTx`；`config/runtime.go` 的 `SettingsKeysV2()`（内部 `settingsKeysV2` 保留）；`provider/provider.go` 的 `SyncDomainResult`。
- **已删除（接口成员，2 项）：** `webui/api/deps.go` 的 `api.Syncer` 接口成员 `Pause()`、`Resume()`。**`Runtime()` 依 2026-09-27 用户裁决（解释 A）保留**，并在接口注释中记录该保留决定与其依据。**`syncer.Syncer` 侧的具体实现方法 `Pause`/`Resume`/`Runtime` 全部保留**（Build6 明确要求，`syncer` 包内测试继续使用）。
- **A5 判别断言的升级（如实记录）：** `webui/api/sync_test.go` 的 `stubSyncer` **刻意不再实现** `Pause()`/`Resume()`，原先两条基于 stub 观测位（`spy.paused`/`spy.resumed`）的断言因此**在编译期已不可能存在**——handler 若试图在协调器之外二次改写运行时开关，编译即失败。这是比运行时断言更强的保证，故移除原断言并就地注明原因；A5 的实质契约继续由**数据库真值**（`sync_enabled`）与**恰好一次 apply**（`e.applyCount()` 分别为 1 与 2）两条断言承担。
- **新增反向守卫（防止保证退化）：** `TestAPISyncerInterfaceHasNoPauseResume` 源码级断言 `api.Syncer` 接口体内**不得**再出现 `Pause()`/`Resume()`，同时必须保留 `ApplyState(`/`Runtime()`，并断言 `syncer` 侧三个实现方法仍存在——避免后续有人把成员加回来而无人察觉。
- **行为零变化：** 未改动任何 handler 逻辑、数据库写入、运行时发布顺序或 HTTP 契约；仅删除零引用符号与接口成员。
- **必须保持项复核（逐项确认仍在）：** `syncer` 具体实现方法、辅助构建/验收代码、全部测试夹具（`webui/api/testenv_test.go`、`provider/request_mock_test.go`、`main_test.go`、`build/Dockerfile`、`webui/embed.go`、`syncer/state_test.go`、`webui/api/alertset_test.go` 等逐个确认存在）、slog/JSON 接口方法；**A10 的 reset 单一空对象契约只读未动**（`decodeJSONObjectStrict` 与 `TestConfigResetStrictBody` 保持原样）。
- **门禁：** 全仓 `go test ./... -race -count=1` 11 包通过；`go vet ./...`、`go build ./...`、`git diff --check` 通过。


#### A6｜已修复｜暂停后排队 ticker/trigger 仍启动同步

- ticker 与 trigger 消费前检查已发布状态；暂停期不得启动新轮。A20 是独立 Stop 问题。

#### A7｜已修复｜恢复同步可能漏掉立即一轮

- false → true 使用 Run 已处理相位并立即一轮；true → true 只重置 ticker。不得回退到已发布镜像。

#### A8｜已修复｜完整导入未重置 DNS breaker

- 完整导入确定 Reset；普通变更 Preserve 并应用新阈值，且已有端点级判别性回归。批次 7 只把 AGENTS 的“完整导入允许新建并清空”收紧为“确定新建 breaker 并清空计数；普通变更保留既有失败计数”，不得重复修改实现。批次 7 中 AGENTS 的平台约束句、README 平台声明与 pidfile 回归属 A19 范围（§六.4 F9），与 A8 无关、不得混写。

**实施记录（批次 7：仅文档收口，2026-09-27，实施前文档基线 `ea71f5f`，实现提交 `b38678a`）**

- **已实施（仅措辞，零代码改动）：** `AGENTS.md` §十一 中「完整导入**允许**新建并清空」收紧为「完整导入**确定**新建 breaker 并清空计数」（前半句保留「普通变更经 `dns.CircuitBreaker.Clone` + `SetThreshold` 保留既有失败计数」）。**未触碰任何实现**：`webui/api/coordinator.go` 的 `Mutate`（`BreakerPreserve`）/`MutateImport`（`BreakerReset`）与 `syncer/state.go` 的 `BuildRuntimeState` 一字未改。
- **顺带完成 G1（用户 2026-09-27 授权）：** `Build6.md:774` 的「完整导入允许新建并清空」与 `:796` 的「熔断计数允许重置」同步收紧为「**确定**新建 breaker 并清空计数 / **确定重置**」，消除 Build6 自身与 `:1269`、`:1060` 的矛盾。
- **既有判别性回归继续通过：** `webui/api/import_runtime_test.go` 的端点级用例（普通变更保留失败计数、完整导入清空计数）、`syncer/state_test.go` 的 `BreakerPreserve`/`BreakerReset` 构造用例。
- **门禁：** 同批次 7。
- **范围边界：** A19 的平台约束句、README 平台声明、pidfile 单实例回归均记在 A19 条目，**未混写**入本条。


#### A10｜已修复｜配置 reset 接受 JSON `null`

- reset 只接受单一空对象 `{}`；`null`、数组、标量、空 body、未知字段、尾随值和多顶层值均 400，且零写入、零 apply。
- 保留 `decodeJSONObjectStrict` 与判别性回归。

## 五、统一实施与验收要求

### 5.1 开工前检查

1. 完整阅读 `AGENTS.md`、`Design5.md`、`Build6.md`、`Issue5.md`、本文和本批相关 `PlatformAPIDocs/`；不得只依赖本文、旧会话或提交标题。
2. 检查分支、HEAD、远端关系、工作树、忽略产物、Go/Node/Docker 环境和依赖版本；保护用户改动，禁止 reset/checkout 覆盖。
3. 重新核对每项完整调用链、生产可达性、测试覆盖与未覆盖边界；代码已变化时先报告差异。
4. 提交逐文件方案、不变量、判别性测试、通用门禁、人工/外部验收与文档回写范围。有实质歧义时使用提问工具并附推荐选项。
5. 在用户最终审核并明确一次性授权全部处理前，不修改代码、测试、依赖或其他文档，不启动构建。
6. 2026-09-27 的全量只读复核与用户裁决已完成（证据见 §六.1，裁决见 §六.4）；除非源码相对 `1fafb172307652912a621eaebf4bf942cbd2c33f` 发生变化，否则不重复整套复核，只核对本批涉及的行号与调用链。

### 5.2 每批最低自动门禁

```text
go test <受影响包> -race -count=1
go test ./... -race -count=1
go vet ./...
go build ./...
git diff --check
```

涉及前端、依赖、Docker、Compose 或平台构建时，还必须执行对应 Build6 门禁：改动前端源码（含 A18 的 `types.ts`/Dashboard 与 A11 的 Dry Run 展示）时必须 `npm ci && npm run build` 并重建 `dist`（`go:embed` 依赖它），同时执行两条阻断式 audit；A19 必须包含 `GOOS=linux`、`GOOS=darwin` 构建通过与 `GOOS=windows` 按预期失败，以及 `docker compose … config --quiet` 与 `docker build`。A1、A2、A4、A9、A11、A12、A13、A15、A16、A17、A20 必须各有判别性用例；仅通用门禁全绿不能关闭。

### 5.3 关闭与文档回写

条目关闭必须同时满足：问题路径消除且不变量未破坏；判别性用例能说明修复前失败、修复后通过；受影响包与全仓门禁通过；要求的人工/外部证据已取得或由用户明确免除；在本条目内追加提交、文件、测试、门禁、外部边界和偏差，再按授权同步其他文档。A11 跨批次交付，接口部分与收尾部分必须都记录在同一条目内，不得拆到无关章节。

真实 SMTP/收件箱与 Webhook 当前属于用户已免除的人工验收，仍不得写成已经通过。已关闭项只有在判别性回归失败或当前调用链重现时才可重开。

## 六、证据边界与规范化记录

### 6.1 已有证据

- 2026-09-27 在 `54efb10` 与 `1fafb17` 上完成全量只读复核；当时整理完成的 `0d9e2d6` 相对 `1fafb17` 无源码变化；比较终点固定到历史提交。
- 当时 `go test ./... -race -count=1`（11 包）与 `go vet ./...` 通过；通用门禁不等于覆盖 Issue6。
- `/tmp/fwreview` overlay 隔离复现确认 A1、A2、A4、A9、A11、A12、A13、A15、A16、A17、A20；均使用虚构数据、localhost 假服务和临时数据库。
- Build6 Step 7 已修复 A5、A6、A7、A8、A10，并保留判别性回归。
- **2026-09-27 第二轮全量只读复核（本轮，未运行 build/test/vet）：** 基线 HEAD `0d9e2d6`，相对 `origin/main` ahead 2，工作树干净；`git diff --stat 1fafb172307652912a621eaebf4bf942cbd2c33f..0d9e2d678f86520a5e688d4ad1b57a6483968d18` 只含 `Issue6.md`，源码零变化。工具链：Go `1.26.6 darwin/arm64`、Node `26.7.0`、npm `11.19.0`、Docker `29.8.0`、Compose `v5.5.1`；`go.mod` 依赖版本与 Step 7 记录一致。复核方式为源码逐行阅读 + 只读阅读 `$GOMODCACHE` 内的 SDK / `modernc.org/sqlite` 源码；新增确认的证据包括：A1 四处构造点与零值语义、共享 `ClientPool` 缓存键要求同值、A12 的腾讯 SDK 错误包装无 `Unwrap`、A20 的 4 处 `syncAll` 调用点与两条额外可达路径、A16 的 listener 泄漏与 `Shutdown` 作用错对象、A2 的标准库 SMTP 无 deadline 链路、A4 的驱动 `_pragma` 逐连接生效与相对路径/`?` 路径缺陷、A9 的历史 `null` 来源、A11 的永不收敛与接口破坏面、A19 的 Windows 面仅 4 处且无 pidfile 单实例回归。上述结论已并入各条目。

### 6.2 尚未取得的证据

- 未以 Issue6 修复版本验证真实云弱网/超时、SWAS DROP 计数、真实 SMTP/收件箱或 Webhook。
- 未访问生产 SQLite，也未验证真实旧库的损坏 targets 或迁移失败。
- Issue6 各批次的 Docker、真机、长跑资源耗尽或远端 CI 验收未整体重跑；A15 已补当前真实二进制/浏览器回归与 loopback TCP 停止读取判别测试，但未覆盖真实 WAN、反向代理异常或操作系统故障型经典半开 TCP。
- Windows 仅曾交叉编译通过；用户已决定移除支持，不再要求 Windows 运行验收。
- 本轮为纯只读复核，未运行 `go test`/`go vet`/`go build`/`go mod tidy`/`npm`/`docker`，因此不产生新的自动门禁证据；上述 SDK 与驱动行为结论来自源码阅读，属静态推理，需在实施批次用真实用例确认。

### 6.3 规范化内容（首次整理）

- 删除旧 HEAD、旧行号和旧工作树状态与当前合同并列的多重权威结构；历史事实仍可从 Git 追溯。
- 合并原“问题明细、逐项复核、最终修复合同”的重复内容，使问题与方案不再分开记录。
- 保留 A1～A20 编号、所有用户决策、固定数值、不变量、判别性回归、外部边界和关闭项回归要求。
- 清除已裁决的“待决策”、漂移行号、重复命令清单及不会指导实施的过程叙述。

### 6.4 用户裁决记录（2026-09-27，第二轮复核）

第二轮只读复核向用户提出 9 项冲突/不确定点，用户逐项裁决如下；这些裁决即为固定口径，已并入对应条目：

| 编号 | 事项 | 裁决 | 受影响条目/位置 |
|---|---|---|---|
| F1 | A9 与历史数据：`c289744` 之前确实写过 `targets='null'`（nil slice） | **保持固定合同：`null` = 内部错误**；存量库 fail-closed 属预期，发布说明必须给修复提示（SQL 或重新导入 v2 包） | A9、§5.3、README 回写范围 |
| F2 | A12 字符串兜底如何覆盖腾讯 SDK 的无 `Unwrap` 包装 | **结构化与兜底两者都做**：`errors.Is` → `errors.As(net.Error)` → 大小写不敏感兜底，并新增云错误码 `ClientError.NetworkError` | A12 |
| F3 | A18 的 `skipped` 生产源属批次 6，无法在批次 4 端到端判别 | **把 `CreateRules` 的 `{Written, Skipped}` 接口改动前移到批次 4**；批次 6 缩为 Dry Run/原因/展示收尾，两部分证据都记入 A11 | §2.2 批次 4/6、A11、A18 |
| F4 | A18 的 `ok` 定义 | **严格字面**：`ok` 只计“成功且无变更”，**另加 `changed`** 计“成功且有增删”；不变量 `total == ok + changed + failed + skipped` | A18、A3、Dashboard/README 回写范围 |
| F5 | A2 每渠道在途上限在配置热重载时的语义范围 | **提升到调度边界**：限流器由 `AlertManager` 按渠道持有并注入实例，使“每渠道 ≤4”跨热重载仍成立 | A2 |
| F6 | A20 确定性判别机制 | **抽取门控 + 测试专用 hook（默认 nil）**；另有“`Stop()` 后 `Run()` 不得起轮”的 100% 红灯证据 | A20 |
| F7 | A16 重复调用语义 | `Start` 第二次**返回错误且不新建 listener**；`Run` 拒绝**一切**第二次调用（含首个已退出后）；`Shutdown` 后 `Start` 也拒绝；`Wait` 用关闭广播 + 存储唯一结果 | A16 |
| F8 | 是否允许测试接缝 | **允许最小非导出接缝**：Ali 端点解析 var 与超时值 var（默认 10_000/30_000ms 不变，另有用例断言默认值）；notifier 的 10s/30s 同样处理 | A1、A2 |
| F9 | 批次 8 范围与 A19 文档范围 | 批次 8 **包含**接口成员与全部零引用死代码；A19 顺带把平台约束写入 AGENTS/README 并补 pidfile 单实例回归。**范围修正（2026-09-27 后续裁决，解释 A）：接口只移除 `Pause`/`Resume`，`Runtime` 保留** | §2.2 批次 8、A5、A19、§六.5 |

### 6.5 历史复核发现、但未纳入当时合同的后续候选（2026-09-27）

以下问题均由当时只读复核确认存在，但未纳入该批 A1～A20 的合同与授权；当时要求另行提出、授权并编号。下文保留原问题分析；后续已处理项以 §7 和审计报告的独立 finding 补记为准，未处理候选仍不得顺手修改。

1. **删除侧同型“跳过仍计数”**：`retry.go:59` 以 `len(diff.ToDelete)` 累加 `deleted`，而 CVM 无效 PolicyIndex 静默 `continue`、SWAS/ECS 空 RuleID 被过滤后仍 `return nil`；重试轮还会让同一跳过项按轮重复计数。
2. **反向缺口“写了却报 0”（2026-09-28 修复时尚未提交，后续提交 `043ac36`）**：核验时非可重试错误会让 `retrySync` 返回的 added/deleted 在 `syncDomainInternal` 错误分支被丢弃，且 ECS 分批创建、CVM 逐条删除内部也会丢失中途已确认成功数；本轮已按“只统计客户端确认成功的独立请求/子批次”贯通 Provider、重试、失败事件、整轮汇总与 `sync_logs`，实施与证据见 §7.2 第 2 项。
3. **`buildDesired` 静默丢弃不上报**：SWAS IPv6 与 ECS ICMPv6 在 Diff 阶段被丢弃，Dry Run 与事件中完全不可见（仅 ECS 有一条 WARN），与 A11 追求的 skipped 口径不一致。
4. **dangling 目标引用无校验**：外部删除 targets 行后，规则会静默“作用于 0 个目标”，既不报错也不告警。
5. **通知链路残余无界**：`StoreLogWriter` 仍按事件派生 goroutine 写 SQLite，DB 慢或被锁时仍会堆积。
6. **SSE 契约缺口**：`/api/sync/events` 无 `id:`/`Last-Event-ID`/回放，缓冲（cap 32）满即丢，`sync:complete` 可能对已连接客户端不可见；两类 SSE 均无心跳。
7. **Dashboard 数字双口径（2026-09-28 修复时尚未提交，后续提交 `043ac36`）**：核验时统计概览仍取最近一条逐域日志的 added/deleted，与 A18 的整轮汇总并存会出现两套数字；本轮已统一改取同一份 `SyncStatus.last_round.added/deleted` 整轮快照，实施与证据见 §7.2 第 1 项。
8. **云侧错误码与重试面**：阿里云 `Throttling`/`ServiceUnavailable` 等不在可重试列表（F2 只新增腾讯 `ClientError.NetworkError`）。
9. **Webhook 细节（历史观察，当前更正见下）**：`resp.Body.Close()` 错误被忽略且不 drain body（影响 keep-alive 复用）。
   - **P3-21 当前更正（2026-10-03，方案 B）：** P2-05（`93e0e4b`）已处理 Close 的固定安全警告，≤16 KiB 的 2xx 经 ReadAll 读至 EOF；非 2xx/超限仍及时关闭。当前 Go 1.27 标准 HTTP/1 Transport 关闭后会尝试有界清理（[官方发布说明](https://go.dev/doc/go1.27#net/http)），不保证慢正文/取消/超大响应等异常连接复用；接受此边界，不增加主动 drain 等待。独立 Push 子项现已实施 16 KiB 有界完整正文校验、`*bool` 确认 ok 和安全 Close WARN，原 10 秒/单在途/无重试/健康独立保持，不混用 Webhook 的渠道成功协议。正式回归与门禁见 [审计报告 P3-21 当前实施补记](./fwalizer-audit-final1.md#p3-21-当前实施补记2026-10-03定型方案-b)；P3-21 本地闭环，真实通知验收状态保持。
10. **SMTP 收件人解析**：`strings.Split(To, ",")` 未逐项 `TrimSpace`，`"a@x.com, b@y.com"` 会产生带前导空格的收件人。
11. **时间戳纯内存**：`last_sync` 与新增的 `last_success` 重启归 null，UI 文案需避免表述为“从未成功”。
12. **SQLite 其他缺口**：未设置任何连接池上限；`AddSyncLog` 的 `COUNT(*)` 是全表扫描且不在事务内；`WithTransaction` 使用无 context 的 `Begin()`；`PRAGMA foreign_keys` 未设置。
13. **`sync_logs.result` 的 `skipped` 语义**：`SyncLog.Result` 注释已预留 `skipped`，但生产只写 success/failed，A11 明确不加列，因此“零写入且有跳过”如何落库仍未定义。
14. **生命周期边角**：`Server.Addr()` 在 Shutdown 后仍返回已关闭地址；`Syncer.Wait()` 原先在 Run 从未启动时会永久阻塞，后者已于 2026-09-28 按“Stop 完成未启动生命周期、Run 拒绝 Stop 后启动”的合同修复并加入判别性回归，实施与证据见 §7.2 第 4 项。`Server.Addr()` 语义仍未处理。

---

**历史结论（2026-09-27，实施前）：** Issue6 已完成第二轮全量只读复核，全部冲突已由用户裁决并并入本文（§六.4），§六.5 的后续候选明确不授权实施。本文已具备构建前使用条件，但待处理项仍未获代码实施授权。下一步等待用户最终审核并给出一次性授权后，再严格按 §2.2 顺序（`1 → 3 → 2 → 4 → 5 → 6 → 7 → 8`）处理全部批次。**后续状态：** 该授权与批次 1～8 实施已经完成（`b38678a`，§2.1）；此历史结论不构成当前待授权任务。

## 七、2026-09-28 Issue6 修复后独立核验报告

> **核验边界：** 本节复核 Codex 任务「核验 Issue6 修复」所报告的问题，只记录当前源码、测试和文档中能够重新证实的结论；不授权修改实现、测试、依赖或其他文档。核验基线为 `main` / `b38678a8d28612517f3cff4f4fc9610a3cc33b95`，核验开始时工作树干净且与 `origin/main` 同步。本轮执行 `go test ./webui ./syncer ./webui/api ./notifier -race -count=1`，四包均通过；未重跑浏览器、Docker、真实云、真实 SMTP/Webhook、远端 CI 或 GHCR 发布。

### 7.1 核验确认且本轮已修复的实现问题

#### R6-01｜已修复（当前 HEAD `8898263d`）｜A11 的具体跳过原因未贯通正式同步事件与日志

- **判定：确认存在。** Dry Run 已通过 `RuleChange.skip_reason` 展示具体原因，但正式同步的 `retrySync` 只返回 `added/deleted/skipped` 三个整数；`EventDomainSyncComplete.Data` 与 `slog.Info("同步完成", ...)` 也只有 `skipped` 数量，没有被跳过的规则或 `skip_reason`。
- **历史日志口径：** `StoreLogWriter` 收到 `EventDomainSyncComplete` 后固定写 `result="success"`，只保存 `added/deleted`，不读取 `skipped`。这与 §3 A11 固定的“跳过原因、事件/SSE/实时日志展示”收尾合同不一致。§3 A11 同时明确“不增加 `sync_logs.skipped` 列”，因此持久化 Schema 不应被本报告擅自扩大；历史日志如何表达 partial/skipped 仍需另行裁决。
- **影响：** 用户可从整轮状态看到 `partial` 和跳过数量，也可在 Dry Run 中看到具体原因，但正式同步的逐域事件、SSE/实时日志和历史日志无法说明“哪条规则因何被跳过”；历史记录还可能把含跳过的逐域结果显示为 `success`。
- **相关既有记录：** §6.5 第 13 项已记录 `sync_logs.result` 的 skipped 语义未定义；本项新增确认的是 A11 已宣称完成的“具体原因贯通正式事件/实时展示”实际上也未完成。
- **本轮裁决（2026-09-28）：** 不新增 `sync_logs.skipped` 列、表或迁移；逐域正式同步事件保留既有 `skipped` 数量并新增结构化 `skipped_details`。历史日志在 `skipped > 0 && added == 0 && deleted == 0` 时写 `result="skipped"`，在有成功增删且同时有跳过时写 `result="partial"`；数量、规则与原因写入既有 `error` 文本列。这样既能区分“全部跳过”和“部分写入”，又不扩大 SQLite Schema。
- **实施（已提交至当前 HEAD `8898263d`）：** `syncer/retry.go` 新增正式同步详情返回路径，只采用最终成功 attempt 的详情，失败重试的同一跳过项不会重复累计；`syncer/syncer.go` 将协议、端口、动作、CIDR、描述和 `skip_reason` 写入 `EventDomainSyncComplete.skipped_details` 与实时 `slog`。`webui/api/logwriter.go` 按上述裁决写 `skipped/partial` 和详情；`config/store.go` 只补齐 `SyncLog.Result` 注释；`webui/frontend/src/types.ts`、`views/Logs.vue` 让 `failed/skipped/partial` 共用中性的“同步详情”入口。SSE 仍按既有机制透明序列化事件，没有引入回放、心跳或新持久化结构。
- **判别性红灯：** 实现前运行 `go test ./webui/api -run TestStoreLogWriter_SkippedAndPartial -count=1`，纯跳过与部分跳过两种事件均被错误写成 `success` 且 `error` 为空；新增正式同步用例同时要求 `skipped_details` 完整，并验证可重试失败 attempt 不会重复累计详情。
- **绿灯证据：** 最终独立测试通过 `go test ./syncer -race -count=1 -run 'Test(RetrySyncDetailedUsesSuccessfulAttemptDetails|DomainSyncCompleteCarriesSkippedDetails)$'`、`go test ./webui/api -race -count=1 -run 'Test(StoreLogWriter_SkippedAndPartial|HandleSyncEvents_ClientDisconnectUnsubscribes|HandleSyncEvents_RealConnectionPushAndDisconnect|HandleSyncEvents_ServerShutdownExits|HandleSyncEvents_WriteErrorExitsAndUnsubscribes)$'`；统一门禁与前端门禁见 §7.6。
- **明确未扩张：** 未处理 §6.5 第 1、2、3、5、6、7 项（删除侧计数、错误路径已写计数丢失、`buildDesired` 静默丢弃、日志写库 goroutine 上界、SSE 回放/心跳、Dashboard 双口径）。未执行浏览器人工回归或真实 SWAS；自动化结果不代表真实云链路已经通过。

#### R6-02｜已修复（当前 HEAD `8898263d`）｜A16 仍有 `Shutdown` 与首次 `Start` 的生命周期窗口

- **判定：确认存在。** `Server.Shutdown()` 会置 `shutdown=true` 并关闭 `shutdownCh`，但不会置 `started=true`；因此在从未启动的实例上先调用 `Shutdown()`，随后首次调用 `Start()`，当前门控仍会放行并建立监听。这与 A16/F7 的“Shutdown 后 Start 也拒绝”字面合同不一致。
- **并发窗口：** `Start()` 在锁内先置 `started=true`，随后解锁执行 `net.Listen` 和构造 `http.Server`，最后才再次加锁发布 `httpServer/listener`。若 `Shutdown()` 在两次加锁之间执行，它会看到 `httpServer == nil` 并返回成功；之后 `Start()` 仍可发布并启动 Serve，形成“Shutdown 已返回，服务随后启动”的逻辑竞态。该问题不会由 Go race detector 报告，因为字段访问有锁，缺口在生命周期状态机。
- **边界澄清：** §6.5 第 14 项已经记录 `Syncer.Wait()` 在 `Run()` 从未启动时永久阻塞，仍然成立，不重复编号。“首次 `Start` 监听失败后不能重试”则来自当前一次性 `Start` 门控；既有 F7 未明确允许失败重试，本报告不把它直接判为回归，需产品裁决后才能改变。
- **本轮裁决（2026-09-28）：** 保持 `Start` 的一次性尝试语义：首次 `Listen` 失败后仍不可重试，不把本问题扩大为失败重试产品变更。`shutdown=true` 是吸收态；此前未启动也必须拒绝 `Start`。监听成功但尚未发布时若发现已 Shutdown，必须关闭该未发布 listener、处理关闭错误，并返回可由 `errors.Is` 判定为 `ErrAlreadyStarted` 的错误。
- **实施（已提交至当前 HEAD `8898263d`）：** `webui/server.go` 为每个 `Server` 增加默认指向 `net.Listen` 的非导出 `listenFunc` 测试接缝；`Start` 的首个锁边界检查 `started || shutdown`，并在发布 `httpServer/listener` 前再次持锁检查 `shutdown`。并发 Shutdown 获胜时不发布字段、不关闭 `serveStarted`、不调用 `Serve`，关闭错误用 `errors.Join` 保留生命周期错误。只修改 `webui/server.go` 与 `webui/server_test.go`。
- **判别性红灯与绿灯：** 三个新增确定性用例分别覆盖“首次 Start 前已 Shutdown”“真实 listener 已建立但尚未返回/发布时并发 Shutdown”“首次 Listen 失败后第二次 Start 仍拒绝”；实现前因缺少 `listenFunc` 接缝而编译红灯。最终独立运行 `go test ./webui -race -count=20 -run 'Test(StartAfterShutdownBeforeFirstStartRejected|ShutdownDuringFirstStartClosesUnpublishedListener|StartListenFailureStillCannotRetry)$'` 全部通过，统一门禁见 §7.6。
- **明确未扩张：** 未修改 `Server.Addr()` 关闭后的语义、`Syncer.Wait()` 未 Run 时阻塞或 §6.5 其他生命周期候选；本问题不需要浏览器、Docker 或外部服务验收。

#### R6-03｜已修复（当前 HEAD `8898263d`）｜Webhook 发送错误可能把完整敏感 URL 写入 WARN 日志

- **判定：确认存在。** `WebhookNotifier.OnEvent` 调用 `http.Client.Post(n.url, ...)`，失败时以 `%w` 返回底层错误；Go HTTP 客户端的请求错误通常包含完整请求 URL。`EventBus.Publish` 随后把该错误作为 `error` 属性写入“事件处理失败” WARN。
- **影响：** 钉钉、飞书、Slack 等 Webhook URL 常把 token/signature 放在路径或查询参数中；网络失败、TLS 失败或超时可能使该敏感 URL 进入 stdout / `docker logs`。这与项目既有“不记录 Webhook URL”的安全口径不一致。
- **范围：** 该问题不同于 §6.5 第 9 项的 response body 未 drain / Close 错误被忽略；两者可分别处理。修复时应保留渠道名和安全错误类别，不应记录完整 URL。
- **本轮裁决（2026-09-28）：** 不修改通用 EventBus，也不把底层发送错误包装、输出或暴露给 `Unwrap`；日志只保留经白名单收敛的渠道名（`dingtalk/feishu/slack`，其他为 `unknown`）与固定安全类别 `canceled/timeout/network/transport`。HTTP 非成功响应只记录 `category=http_status` 与状态码。
- **实施（已提交至当前 HEAD `8898263d`）：** `notifier/webhook.go` 只用底层错误做结构化分类，返回的新错误不含 `%w`、`err.Error()`、目标 host/path/query 或底层网络/TLS 文本；新增 `notifier/webhook_security_test.go`，测试内通过私有 `client` 注入 `RoundTripper`，未扩大生产构造 API。
- **判别性红灯与绿灯：** 修复前，直接返回错误与真实 `EventBus → slog WARN` 都包含 URL host、路径 token、查询 signature 和底层 transport 哨兵，且可解包到底层错误；修复后独立运行 `go test ./notifier -race -count=1 -run 'Test(WebhookSendErrorDoesNotExposeSecrets|WebhookErrorCategory|WebhookEventBusWarningDoesNotExposeSecrets)$'` 通过，四种安全分类、非法渠道、不可 `Unwrap` 与端到端 WARN 脱敏均已覆盖。统一门禁见 §7.6。
- **明确未扩张：** 未处理 §6.5 第 9 项 response body drain / `Close` 错误，也未修改 SMTP 或其他渠道。真实 Webhook 人工验收仍沿用用户免除，且没有真实通过结论。

### 7.2 确认存在、但已由 §6.5 记录的残余

以下报告结论均由当前源码再次证实，不另建重复条目：

1. **A18 Dashboard 数字双口径（该批次修复时尚未提交，后续提交 `043ac36`）：**
   - **研究结论：** 核验时 `Dashboard.vue` 每 5 秒读取 `/api/sync/status`，但统计概览另行读取 `/api/sync/logs` 并取 `logs[0].added/deleted`；后者是按 `sync_logs.id DESC` 返回的最新一条逐域单元记录，而 `SyncStatus.last_round` 才是跨全部 Provider 与域名规则累计的整轮原子快照。多单元轮次中两者即使没有请求竞态也可天然不同，问题确认存在。
   - **裁决与边界：** 无需新增产品决策；按 A18 既有合同统一使用 `status.last_round.added/deleted`。不在本项处理 §6.5 第 11 项“重启后内存态时间戳与 `last_round` 归空”的展示语义，`null` 继续按既有界面回退为 `0`；不新增 API、SQLite 字段、依赖或前端测试框架。
   - **实施：** 仅修改 `webui/frontend/src/views/Dashboard.vue`：移除 `SyncLogEntry` 导入和 Dashboard 对 `/api/sync/logs` 的请求，`stats` 只保留 targets/rules，“最近同步 新增/删除”直接显示 `status.last_round?.added ?? 0` 与 `status.last_round?.deleted ?? 0`。后端、API、Schema、依赖和日志页均未修改。
   - **判别性红灯与绿灯：** 修复前源码检查命中 `SyncLogEntry`、`/api/sync/logs`、`logs[0].added/deleted`，且缺少两处 `status.last_round` 计数引用，退出码为 1；修复后旧数据源引用全部消失、两处整轮字段引用均存在，退出码为 0。
   - **门禁：** `npm ci`、`npm run build`、`npm audit --audit-level=high`、`npm audit --omit=dev --audit-level=high`、`go test ./webui -race -count=1`、`go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 与 `git diff --check` 全部通过；npm 两类审计均为 0 漏洞。
   - **未执行边界：** 未执行浏览器人工回归、真实云 API、DNS、SMTP、Webhook、Docker、远端 CI 或其他外部链路验收。自动门禁不能替代浏览器对多逐域单元整轮合计、重启后 `0/0` 回退和 5 秒轮询更新的人工确认，也不得把上述外部链路写成已通过。
2. **错误路径已写计数丢失（该批次修复时尚未提交，后续提交 `043ac36`）：**
   - **研究结论：** 缺口共有三层：`retrySyncDetailed` 已跨 attempt 累计 added/deleted，但 `syncDomainInternal` 在错误分支先返回，导致失败单元的 `unitResult`、`EventSyncError`、整轮汇总与 `sync_logs` 全部报 0；ECS 每 100 条分批创建时，前批成功、后批失败会返回空 `CreateResult`；CVM 按 PolicyIndex 逐条删除时，前项成功、后项失败只返回 error。任一 Provider 还可在删除成功后、创建失败时触发上层丢失。
   - **裁决与不可判定边界：** 只统计客户端已确认成功的独立请求或子批次；当前请求返回错误且云端提交状态不明时计 0，不根据随后 Describe 看到的状态推测归因，因为该状态也可能来自外部并发。确认数跨 retry attempt 累积，每次重试仍完整执行 Describe → Diff → Create/Delete，由新 Diff 自然避免重复写入计数。失败仍优先归类 `failed`，added/deleted 与 outcome 正交。
   - **实施：** `provider/provider.go` 明确 `CreateResult.Written` 在 `err != nil` 时可报告此前成功子批，并新增支持 `errors.As`/`Unwrap` 的 `PartialDeleteError`；`provider/ali_ecs.go` 保留失败批次之前的 Written，`provider/tc_cvm.go` 保留逐条删除中途的确认数。`syncer/retry.go` 在错误分类与重试前累计 Written/部分删除数；`syncer/syncer.go` 在失败分支前写入 `unitResult`，并让实时日志与 `EventSyncError` 携带 added/deleted；`webui/api/logwriter.go` 保持 `result=failed` 与原错误文本，同时持久化确认计数。
   - **判别性红灯：** 新增 Provider/Syncer 用例后，因 `PartialDeleteError` 尚不存在而编译失败；`TestStoreLogWriter_ErrorDetail` 则实际得到 `added/deleted=0/0`、期望 `2/1`，直接证明历史失败记录丢数。
   - **判别性覆盖：** `TestRetrySync_NonRetryableErrorKeepsConfirmedDeleteProgress`、`TestRetrySync_ExhaustedRetriesKeepConfirmedProgress`、`TestRetrySync_CreateResultWithErrorKeepsWritten`、`TestRoundSummary_FailedUnitKeepsConfirmedCounts`、`TestSyncErrorCarriesConfirmedCounts`、`TestRequest_ECSCreateReturnsConfirmedProgress`、`TestRequest_CVMDeleteReturnsConfirmedProgress` 与 `TestStoreLogWriter_ErrorDetail` 覆盖非重试失败、重试耗尽、跨 attempt 去重、失败整轮不变量/事件、ECS 后批失败、CVM 中途失败及历史落库。
   - **门禁：** 判别性选择测试、`go test ./provider ./syncer ./webui/api -race -count=1`、`go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 与 `git diff --check` 全部通过。
   - **明确未扩张与未验证：** 未全面修改 `DeleteRules` 接口，未处理 §6.5 第 1 项删除侧在正常成功返回下的跳过虚增，未新增 Schema、前端或依赖。未执行真实腾讯云逐条删除中途失败、真实阿里云分批后批失败、弱网响应丢失、Docker、远端 CI 或其他外部验收；自动 mock 结果不得写成真实云已通过。
3. **历史 skipped 语义原先未定义（已由当前 HEAD 收口；本轮复核被用户中断）：** 核验时 `SyncLog.Result` 注释允许 skipped，但生产只写 success/failed；提交 `8898263d` 已随 R6-01 使用 `skipped/partial` 并把详情写入既有 `error` 列，见 §7.1 与 §7.6。本轮原计划再派独立研究代理复核这一关闭结论，但用户在代理完成前要求中断；该代理未修改文件，其未完成研究不得记作本轮验证证据，也没有派发修复代理。
4. **`Syncer.Wait()` 未运行即阻塞（该批次修复时尚未提交，后续提交 `c95b139`）：**
   - **研究结论：** 修复前 `Wait()` 直接等待仅由首个 `Run()` 关闭的 `doneCh`；`Stop()` 只关闭 `stopCh`，因此从未调用 Run 的实例即使已经 Stop，后续 Wait 仍永久阻塞。不能简单让 Wait 在 `runGuard=false` 时返回，否则并发 `go Run(); Stop(); Wait()` 可能由 Wait 抢先返回、Run 随后才取得生命周期所有权，破坏“Wait 返回即完全退出”。
   - **最终合同：** Wait 仍是“Stop 后调用”的等待接口；Run/Stop 均未发生时继续阻塞。Run 与 Stop-before-Run 二选一取得 `doneCh` 关闭权：Run 先取得 `runGuard` 时，Stop 只关闭 `stopCh`，当前轮完成后由 Run 关闭 `doneCh`；Stop 先发生时直接关闭 `stopCh` 与 `doneCh`，stopped 成为吸收态，后续首次 Run 也立即拒绝且不进入 running。
   - **实施：** `syncer/syncer.go` 的 Run 在同一 `mu` 锁边界先检查 `stopped`、再检查 `runGuard`；Stop 把 `stopped=true` 与 `neverStarted := !runGuard` 放在 `stopOnce` 内并由同一锁线性化，先关闭 `stopCh`，仅在 neverStarted 时关闭 `doneCh`。未新增状态枚举、channel、goroutine、timeout、context 或公开 API。
   - **判别性红灯：** 只加入新测试、尚未改生产实现时，`TestStopBeforeRunWaitReturns` 在 1s 后失败“Run 从未启动时，Stop 后 Wait 必须有界返回”；`TestWaitBeforeRunAndStopBlocksUntilStop` 在 Stop 后仍无法释放第 1/4 个 Wait，二者直接证明缺陷。
   - **判别性与回归绿灯：** 新增 `TestStopBeforeRunWaitReturns`（Stop 后 Wait 返回、首次 Run 拒绝、不进入 running、不调用 Provider）与 `TestWaitBeforeRunAndStopBlocksUntilStop`（Stop 前阻塞、Stop 后广播释放 4 个 Wait）。两条新用例连同 `TestStopBeforeRunStartsNoRound`、`TestStopWaitsForBlockedRound`、`TestStopIdempotent`、`TestRunSecondCallRejectedWithoutPanic` 在 `-race -count=20` 下通过；后者继续证明 Run 已开始时 Wait 不得在当前轮完成前返回。
   - **门禁：** `go test ./syncer -race -count=1`、上述生命周期选择测试 `-race -count=20`、`go test ./... -race -count=1`（11 包）、`go vet ./...`、`go build ./...` 与 `git diff --check` 全部通过。
   - **未扩张：** 未处理同属 §6.5 第 14 项的 `Server.Addr()` Shutdown 后地址语义，未修改主启动顺序、同步开关、Dry Run、Provider、数据库、前端、依赖或其他 §6.5 候选；本问题不需要浏览器、Docker 或外部服务验收。

### 7.3 确认存在的测试与外部证据缺口

这些缺口不等同于已经复现的生产故障，但不应被“通用门禁通过”替代：

1. **A3/A18 状态 API 值级回归不足：已收口（2026-09-28）：** 新增未跟踪判别性用例 `webui/sync_status_test.go:TestSyncStatusEndpointRoundValues`，通过真实 `Syncer`、`RuntimeManager`、`Server` 与 HTTP GET 端点验证初始 null、success、failed、partial、idle、paused 的 `last_success`、`last_round`、`last_sync`、不变量与暂停不制造新轮次；使用 `EventSyncComplete` 作为确定性屏障，无固定 sleep。独立专项与相关包 race、全仓 race、vet、build、前端 `npm ci`/build、两条 audit 及 `git diff --check` 均通过。另以临时本地 HTTP mock 完成 Dashboard 六状态浏览器人工验收：提示、整轮新增/删除、5 秒 `/api/sync/status` 轮询和不建立 `/api/sync/events` 均符合合同。该证据仅覆盖本地自动化与浏览器展示，不代表真实云、Docker、SMTP、Webhook 或远端 CI/GHCR 已通过。
2. **A9 HTTP 安全文案回归缺失：已收口（2026-09-28）：** 新增 `webui/api/rules_test.go:TestRuleReadCorruptTargetsUsesSafe500`，通过真实 `GET /api/rules` 路由和损坏 SQLite `rules.targets` 验证 HTTP 500、精确 JSON Content-Type、仅安全 `error` 字段、无原值/规则 ID/内部诊断/部分合法规则，服务端日志保留动作/规则 ID/损坏类别但不回显原值，且 `applyCount=0`。A9 九种损坏值/四条加载链专项、问题 2 端点专项、`webui/api` + `config` race、全仓 `go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 与 `git diff --check` 均通过。本项无需前端构建、浏览器、Docker 或外部服务，均未执行且不适用；未以真实生产旧库验证损坏 targets 的升级行为仍保持原边界。
3. **A14 故障注入不完整：已收口（2026-09-28）：** `config/store_error_test.go:TestAddSyncLogCountFailureDoesNotFailWrite` 通过 `sql.OpenDB` + 标准库 `driver.Connector` 精确注入非 `driver.ErrBadConn` 的 COUNT 哨兵错误，确认 INSERT 已成功、COUNT 失败后 `AddSyncLog` 仍返回 nil、严格只有一次 INSERT 与一次 COUNT、无重试/DELETE，且日志包含错误类别但不泄露 target/domain/error 业务值。`webui/api/deps_test.go:TestWriteJSONEncodeFailureLogsWithoutSecondHeader` 通过非零短写后返回 `io.ErrClosedPipe` 的 ResponseWriter 触发 Encode 失败，确认只写一次状态头、只发生一次 Write、响应体为非零短前缀且日志不泄露 payload，不会伪造第二个错误响应。两项均为对既有生产处理的判别性补测试，未修改生产代码，**无先红证据**。专项、trim 回归、`config + webui/api` race、全仓 `go test ./... -race -count=1`、`go vet ./...`、`go build ./...` 与 `git diff --check` 均通过；前端、浏览器、Docker、真实 SQLite 故障环境和外部服务不适用且未执行。
4. **外部层仍未验证：** Issue6 修复后的真实云弱网/超时、真实 SWAS DROP、A11/A18 新前端展示、真实 SMTP/收件箱、Webhook、真实 WAN/反向代理异常或操作系统故障型经典半开 TCP，以及当前工作树的远端 CI/GHCR 均无新增证据。A15 已按第二阶段合同建立 5 秒单次写出边界，并取得真实 loopback TCP 停止读取、当前二进制 curl 与浏览器日志流证据；这不外推为上述外部网络环境已通过。既有 `v2.0.0` Actions/镜像结果属于更早 revision，不能证明当前改动；SMTP/Webhook 则是用户明确免除人工验收，不得改写为通过。

### 7.4 历史核验：文档状态与当时代码互相矛盾（2026-09-28）

> **P3-18 对账结果（2026-10-03）：** 本节以下为当时发现的原始证据，保留不删除。顶部与 §6 现已标为历史基线，比较命令固定终点；旧等待授权结论标为实施前，实施记录区分 `ea71f5f` 文档基线与 `b38678a` 实现提交。Build6/Issue5 阅读入口补充后续状态，当前入口与历史证据不再混用。本项只解决文档漂移，不代表全部业务观察或外部验收关闭。

- **判定：确认存在。** 本文顶部仍把 `0d9e2d6`、源码零变化和“等待一次性实施授权”写成当前基线，文末旧结论也仍称批次尚未实施；但当前 HEAD `b38678a` 已包含批次 1～8 的 52 文件实现、测试和文档改动，§2.1 又已写为全部实施完成。
- `AGENTS.md` 与 `Design5.md` 仍称批次 8 视后续执行或仍剩余，但当前源码和本文 §3 批次 8 实施记录都表明低风险清理已经完成。
- `Build6.md` 和 `Issue5.md` 多处仍称 A1/A2/A3/A4/A9/A11～A19 “继续留在 Issue6/待处理”，与当前实现和 Issue6 状态表冲突。
- 各实施记录标题把 `ea71f5f` 写成 HEAD；该提交实际是修复前的文档裁决提交，承载实现改动的是后继提交 `b38678a`。记录可说明实施时工作树基线，但不能写成最终实现提交。
- **结论：** 在上述冲突完成对账前，Issue6 不能作为无歧义的“全部关闭”状态基线；本节只记录冲突，没有获授权改写其他段落或其他文档。

### 7.5 本轮未确认的新问题

- 引用报告提到 A1 阻塞服务用例和 A15 SSE 用例各出现过一次偶发失败；本轮受影响四包 race 测试全部通过，现有证据不足以把两次偶发现象定性为稳定可复现缺陷。若后续再次出现，应保留完整失败输出、随机种子/次数和运行环境后另行跟踪。
- A1/A2/A4/A5～A8/A10/A12/A13/A15/A17/A19/A20 的核心本地实现，本轮源码复核未发现报告所述范围之外的新确定性回归；这不扩大为外部链路或当前 HEAD 远端发布已经通过。

### 7.6 R6-01～R6-03 并行修复与独立验收记录

> **执行边界：** 2026-09-28，以 `main` / `dc25d9e429e939a2f49c1ac6a1791890afb23a26`（相对 `origin/main` ahead 1）为起点，先由三个互相独立的只读研究代理分别研究 R6-01、R6-02、R6-03，再由三个新的修复代理各自只实现一个问题，最后由未参与实现的测试代理统一验收。三个实现代理均未修改本文，本文由主调度代理在全部测试完成后统一收口。该批实现与本文随后已提交为 `8898263d8e25fbd8503811bcea11429629bb88ff`，当前 `main` 与 `origin/main` 均指向该提交。

- **范围复核：** 实现改动共 12 个已跟踪文件与 1 个新增测试文件：`config/store.go`；`syncer/retry.go`、`syncer/retry_test.go`、`syncer/round_summary_test.go`、`syncer/syncer.go`；`webui/api/logwriter.go`、`webui/api/logwriter_test.go`；`webui/frontend/src/types.ts`、`webui/frontend/src/views/Logs.vue`；`webui/server.go`、`webui/server_test.go`；`notifier/webhook.go`、`notifier/webhook_security_test.go`。独立测试代理逐项检查差异，未发现顺手处理 §6.5 其他未授权候选。
- **Go 统一门禁：** 涉及 Go 文件的 `gofmt -l` 无输出；`git diff --check` 通过；`go test ./... -race -count=1` 共 11 个包全部通过；`go vet ./...`、`go build ./...` 通过。R6-01、R6-02、R6-03 的专项判别命令与结果分别记录在对应条目中。
- **前端与依赖门禁：** 在 `webui/frontend` 使用独立任务缓存 `/tmp/fwalizer-r6-test-npm.JFNDfJ` 执行 `npm ci` 与 `npm run build` 均通过（Vite 8.3.1，2819 modules）；`npm audit --audit-level=high` 与 `npm audit --omit=dev --audit-level=high` 均为 0 vulnerabilities。构建后 `package.json`、`package-lock.json` 与构建目录没有新增 tracked diff；未使用 sudo 或修改全局 npm 缓存。
- **证据分层：** 上述结论只证明当前合并工作树的本地确定性测试、race、静态检查、Go 构建和前端构建/审计通过。未执行浏览器人工回归、真实 SWAS、真实 Webhook、Docker、远端 GitHub Actions 或 GHCR 发布；其中真实 Webhook 人工验收继续沿用用户已明确免除，仍不得写成真实通过。

**该批次结论（2026-09-28）：** R6-01、R6-02、R6-03 已按本节裁决完成修复、经独立测试代理验证，并提交、推送为该批次 HEAD `8898263d`；Docker/远端 CI 及上述外部人工验收仍不在该批已完成证据内。§6.5 其他候选当时维持未授权状态，后续 §7.2 残余处理进度见 §7.7。

### 7.7 §7.2 残余问题串行处理进度（2026-09-28）

> **执行方式：** 第 1～3 项沿用此前严格串行处理记录；第 1、2 项随后由提交 `043ac36` 纳入 `main`，第 3 项由 `8898263d` 的 R6-01 实现收口。用户于 2026-09-28 重新授权直接研究并修复第 4 项；本次由主任务实施并执行统一门禁，未另派独立测试代理。

| §7.2 项目 | 当前状态 | 代码与证据边界 |
|---|---|---|
| 1. A18 Dashboard 数字双口径 | **已由修复提交 `043ac36` 收口** | 仅修改 `webui/frontend/src/views/Dashboard.vue`；统一改取 `SyncStatus.last_round.added/deleted`。failure-first 源码判别、`npm ci/build`、两条 audit、`go test ./webui -race`、全仓 race/vet/build 与 `git diff --check` 已通过；浏览器与外部验收未执行。 |
| 2. 错误路径已写计数丢失 | **已由修复提交 `043ac36` 收口** | 修改 Provider 部分成功传递、retry 累计、失败事件/汇总与 `sync_logs` 计数，并新增判别性测试；受影响三包 race、全仓 race/vet/build 与 `git diff --check` 已通过。真实云分批/逐条中途失败和弱网响应丢失未验证。 |
| 3. 历史 skipped 语义 | **当前 HEAD 已由 R6-01 收口；本轮独立复核被用户中断** | `8898263d` 的既有实现与 §7.1/§7.6 记录仍在；本轮研究代理未完成、未修改文件，因此没有新增研究或测试结论，也未派修复代理。 |
| 4. `Syncer.Wait()` 未运行即阻塞 | **研究、修复与该批次门禁完成；后续已提交 `c95b139`** | `Stop` 完成未启动生命周期并关闭 `doneCh`，`Run` 拒绝 Stop 后启动；两条新判别性回归先红后绿，生命周期选择测试 `-race -count=20`、全仓 race/vet/build 与 `git diff --check` 通过。 |
| 最终独立测试 | **尚未执行** | 本次由主任务完成统一本地门禁，未另派独立测试代理；这不影响判别性自动测试结论，但不得记作独立代理验收。 |

**历史工作树（2026-09-28，`c95b139` 提交前）：** 第 1、2 项已由提交 `043ac36` 纳入当前 `main`；第 3 项仍由 `8898263d` 的 R6-01 实现收口。当时未提交工作树仅包含第 4 项的 `syncer/syncer.go`、`syncer/stop_gate_test.go` 与本文记录。未执行 Docker、远端 GitHub Actions、GHCR、浏览器人工回归或真实云/API/DNS/SMTP/Webhook 验收；同属 §6.5 第 14 项的 `Server.Addr()` 语义及其他候选仍未授权处理。

### 7.8 Build7 Step 7：核验缺陷修复记录（2026-09-28）

> **执行方式：** Build7 Step 0～6 完成后，按用户要求对 Build7 构建情况做一次只读全量核验（逐版块比对
> 合同 + 自动门禁 + 真实二进制 + Docker 容器 + 前端审计），核验发现的缺陷经用户裁决后由主任务一次性修复。
> 核验同时确认：**「前端发送后端 DTO 没有的字段」这一类缺陷全项目仅一处**（19 处写请求与全部 GET
> 响应字段逐字段核对，其余 18 处对齐）。

| 编号 | 问题 | 根因 | 处置 | 状态 |
|---|---|---|---|---|
| R7-01 | 告警页「测试发送邮件」必然 HTTP 400，Build7 §5.3 的「SMTP 已接受」成功态在 UI 中不可达 | `Alerts.vue` 的 `testSend()` 序列化整个 email 表单对象（含 `enabled`），而 `POST /api/alerts/test-email` 的请求 DTO 只有 8 个字段并使用严格解码（拒绝未知字段） | 按用户裁决**只改前端**：新增 `TestEmailPayload` 类型并显式构造 8 个发送字段；后端 DTO 与严格解码契约不变（测试邮件 API 的严格性不得放宽） | 已修复（2026-09-28） |
| R7-02 | 进程重启时把「同步引擎尚未启动」误判为「引擎未运行」：启动即写 WARN「运行健康异常」，Push 首条心跳为 `status=down&msg=同步引擎未运行`；开启第三触发开关 + 渠道时**实测发出一次误报 Webhook/邮件** | `run.go` 先启动监督器/Push goroutine、后启动 `Syncer.Run`，而 `running=true` 只在 `Run` 内 `setRunning(true)` 才置位；监督器首检与 Push 首发立即执行 | 按用户裁决同时采纳两层修复：① **启动顺序确定性化**——`Syncer` 暴露 `Started()`（在 `running=true` 可见后关闭一次），`run.go` 有界等待（上限 2s，超时 WARN 后继续）再启动监督器与 Push；② **健康判定启动宽限**——新增 `SyncStatus.started_at` 与固定 `StartupGrace=10s`：从未进入运行态且在宽限内不判异常，超宽限仍按未运行上报，已进入运行后停止立即异常（不受宽限影响） | 已修复（2026-09-28） |

**同批低风险收口（无行为变更或仅文案/样式）：**

- **R7-03（注释滞后）**：`config/store.go`、`webui/api/bundle_v3.go`、`webui/api/coordinator.go`、`webui/api/settings.go`、`webui/frontend/src/views/Settings.vue` 中残留的「version 2」表述统一改为 version 3；`webui/api/export.go` 与 `coordinator.go` 的 commit 后发布顺序注释补齐为「日志级别 → 告警集合 → 运行健康监督器唤醒 → Uptime Kuma Push 唤醒 → RuntimeState」。
- **R7-04（注释与实现不符）**：`config/runtime.go` 的 `BusinessSnapshot` 注释改为与实现一致（policy/push 读取时解析校验；email/webhook 按原值读取，合法性由 PUT 与配置导入共用校验保证）。
- **R7-05（Webhook 详情块非确定）**：`notifier/webhook.go` 原用 `formatEventBody` 遍历 map，同一事件可能产生不同文本顺序；现与邮件共用 `formatEventDetails` 固定渲染器（顺序稳定、缺失写 `-`），并删除 `formatEventBody`。按用户裁决，运行健康异常事件在详情块末尾追加固定 `原因` 行（稳定原因用 `; ` 连接，无原因不输出），避免丢失 reasons。
- **R7-06（提示与样式）**：`Settings.vue` 导出/导入二次确认弹窗的敏感清单补 `Uptime Kuma Push URL（含 token）`；`Alerts.vue` 的 Webhook URL 改为密码型可切换显示输入。

**判别性证据（本机 2026-09-28）：**

- R7-01：`main_test.go:TestProcessTestEmailWithUIPayload`（真实二进制 + 本地假 SMTP）——与页面一致的 8 字段载荷返回 `200 {"success":true,"message":"SMTP 服务器已接受测试邮件"}`，并断言主题固定后缀、正文固定说明、多收件人逐项 Trim；同一载荷多带 `enabled` 返回 `400 请求体包含未知字段: enabled`（修复前页面载荷正是该形态）。修复前现场复现：带 `enabled` → 400。
- R7-02：`main_test.go:TestProcessRestartPushFirstHeartbeatIsUp`（Push 配置持久化 + `GOMAXPROCS=1` 重启）——首条心跳必须 `status=up&msg=OK`、token 与未知 query 保留、启动日志不得出现「运行健康异常」；修复前在 `GOMAXPROCS=1` 下 12/12 复现 `status=down&msg=同步引擎未运行`，并实测收到误报 Webhook。新增 `internal/health:TestEvaluateStartupGrace`（宽限内健康 / 超宽限异常 / 已启动后停止立即异常）、`syncer:TestStartedSignalClosesAfterRunBegins`、`syncer:TestStartedNeverClosesWhenStopPrecedesRun`。
- R7-05：`notifier:TestWebhookContentIsDeterministicAndUnified`（同一事件连续 20 次渲染逐字节一致、三渠道、固定顺序、不含未固定键）、`notifier:TestWebhookSyncEventUsesFixedBlockWithoutExtraKeys`、`notifier:TestEmailOperationalEventIncludesReasons`。
- 统一门禁：`gofmt -l` 无输出、`go build ./...`、`go vet ./...`、`go test ./... -race -count=1`（12 包全绿）、前端 `npm run build` 与两条 `npm audit`（0 漏洞）、`docker build` + 容器非 root（uid 1000）/`HEALTHCHECK` 仍指 `/api/health`/`healthy`/`docker stop` 有界且退出码 0。

**证据边界（不得写成通过）：** 本地假 SMTP/本地 HTTP mock 不等于真实 SMTP 收件箱、真实 Webhook 或真实 Uptime Kuma；浏览器人工交互回归（ProdTestList PT-B7-07）仍未执行；真实云 API 与远端 CI/GHCR 未执行。契约同步见 `AGENTS.md` §9.1（启动宽限、两渠道共用详情块与「原因」行）与 `Build7.md` §4.4/§7.2/§7.3/Step 7。


### 7.9 P3-07 旧逐规则同步清理与回归迁移（2026-10-02，方案 B，后已提交 `ba82292`）

用户已授权按引用研究聊天的推荐方案再次核对范围、修复并同步更新文档。正式实施前 `main == origin/main == c08f2e1`，工作树干净；研究阶段的 ahead 4 已是历史快照。旧 `retrySync` 的 15 个调用全部来自测试；`retrySyncDetailed` 只有旧 wrapper 和一个测试调用，`truncateDesc` 只有旧链与测试调用。现生产入口不可达旧流程，因此删除不会改变当前目标状态机。

正式同步继续使用 `Run → syncAll → runRound → syncTarget → runTargetAttempt / runTargetCleanup`：每 attempt 解析/取 S0/规划，先 Add，再 S1 证明覆盖，经安全门 Delete，必要时 S2。旧链的先删后加、空快照写入与空 DNS 仍清理等断言已不符合当前合同，应随旧实现删除。确认写入跨 attempt 保留，幂等错误仍须由覆盖快照证明；Provider.Skipped 不代表访问权限已满足。

| 原回归（历史测试） | 当前承接与验收 |
|---|---|
| `TestRetrySync_RealTimeoutTriggersSecondFullAttempt`、`TestRetrySync_TencentNetworkErrorRetries`、`TestRetrySync_NonRetryableStopsImmediately`、`TestRetrySync_ExhaustsThreeAttempts` | `TestTargetRetry_SnapshotFailures`：真实 HTTP 超时、腾讯 SDK 网络错误、不可重试与三次耗尽；断言 DNS/快照/写入次数与 1s/2s 退避 |
| `TestRetrySync_Counts`、`TestRetrySync_AddedFollowsProviderWritten`、`TestRetrySync_AddedCountsOnlyWritten`、`TestRetrySync_CreateResultWithErrorKeepsWritten` | `TestTargetRetry_WriteAccounting`：确认部分写入后重试/停止、0 Written 且无覆盖失败、外部满足但当前调用 0 Written 成功；已有 `TestRoundSummary_ChangedCountsAsChanged` 保留正常整轮计数 |
| `TestRetrySync_PartialWriteCounting`、`TestRetrySync_NonRetryableErrorKeepsConfirmedDeleteProgress`、`TestRetrySync_ExhaustedRetriesKeepConfirmedProgress` | 前者改由 `TestTargetRound_AddFailureKeepsOldRules` 证明先增失败零删除；后两者由 `TestCleanup_PartialDeleteKeepsConfirmedAndDefersRest` 与 `TestTargetRetry_DeleteProgressAcrossAttempts` 证明覆盖后的确认删除/跨 attempt 进度；失败事件与汇总继续由 `TestSyncErrorCarriesConfirmedCounts`、`TestRoundSummary_FailedUnitKeepsConfirmedCounts` 承接 |
| `TestRetrySyncDetailedUsesSuccessfulAttemptDetails`、`TestRetrySync_SkippedCountsDryRunSkipsWithoutToAdd` | `TestTargetRetry_UnsupportedFinalAttempt`、`TestRoundSummary_OnlySkippedIsPartial`、`TestTargetSyncCompleteCarriesUnsupported`、`TestSyncErrorRetainsUnsupportedAfterAddFailure` 与既有 Dry Run/能力矩阵用例；只取最终 attempt 的明细 |
| `TestRetrySync_IdempotentErrorsNotCounted` | `TestTargetRound_IdempotentCreateConfirmedByS1` + `TestTargetRetry_IdempotentCreateRequiresCoverage`；删除用 `TestCleanup_IdempotentNotFoundUsesS2FinalCandidates` / `TestCleanup_IdempotentNotFoundStillFailsWhenS2Untrusted`，幂等不虚增计数 |
| `TestRetrySync_EmptyCommentDesc`、两项 `TestTruncateDesc_*` | `provider/plan_test.go` 的 `TestRenderDescription_EmptyComment` 与两项 `TestTruncateDescription_*`，直接覆盖共享实现，四平台空备注、中文截断、CVM/ECS 不截断与 48 rune TAG 保留 |
| `TestRetrySync_TagSnapshotAcrossRetry`（本来已走 syncAll，并非旧入口测试） | 完整保留，仅更名为 `TestSyncRound_TagSnapshotAcrossRetry` 并更正目标链注释；其他 TAG/Provider 单轮快照测试不删除 |

正式范围为 11 文件，详见审计报告 P3-07 补记；现用 `maxRetries` / 错误判定、R7-06 的真实超时连接夹具、连接测试所需 `GetRules`、旧 Diff/P0-01 和 planner 收敛回归保留。生产同步状态机、Provider 增删、DNS、健康、API/schema、前端和 SDK 均未修改。正式门禁与六类负向控制的结果以审计报告本次记录为准，源码/测试/文档尚未提交。

独立观察：部分删除后 S2 失败，随后 S0 重试耗尽，确认 added/deleted/cleanup_deleted 保留，但 cleanup_candidates/deferred 会被后续空 attempt 覆盖为 0。应保留上次残留还是表达“未知”尚未定案；本次测试仅判定失败路径确认计数，不把残留 0 固定为正确语义，不扩大本项生产修复范围。

本机 Go 1.26.6 / macOS arm64；未执行 Go 1.25/Linux、前端构建、产品真实二进制/浏览器、Docker、真实云/通知链路或当前 revision 远端 CI/GHCR；既有人工清单不新增项目，亦无新增外部通过结论。
