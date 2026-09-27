# Issue6.md — FWAlizer 后续问题与修复合同

> **文档定位：** 本文是 Build6 完成后的当前问题记录与后续修复合同。它只描述问题、已确认的产品语义、建议实施边界和验收要求；除明确标记为“已修复”的条目外，不代表代码已经修改或外部链路已经验收。
>
> **当前基线：** 2026-09-27，分支 `main`，全文规范化提交 HEAD `0d9e2d678f86520a5e688d4ad1b57a6483968d18`（其父为整理前 HEAD `1392ab9b675d0cf79a04f5f4a0f2f5cd966dec36`），相对 `origin/main` ahead 2；`git diff 1fafb172307652912a621eaebf4bf942cbd2c33f..HEAD` 只包含 `Issue6.md`，源码零变化，因此下列源码结论与最终合同仍适用于当前实现。本轮裁决并入后，工作树唯一的未提交改动就是本文（不涉及任何源码、测试、依赖或配置）。
>
> **授权边界：** 2026-09-27 已完成全量只读复核与用户裁决；本次只把裁决结果并入本文，不修改设计、源码、测试、依赖、配置或其他文档，也不开始任何修复。后续仍须经用户一次性授权后，才可按 §2.2 顺序实施。

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
| 确认存在，待处理 | A1、A2、A4、A9、A11、A12、A13、A14、A15、A16、A17、A20 |
| 产品决策已定，待实施 | A3、A18、A19 |
| 已修复，保留回归 | A5、A6 原问题、A7、A8、A10 |
| 待用户决策 | 无 |

上表状态经 2026-09-27 全量只读复核再次确认（复核范围与证据见 §六.1），未发生状态翻转：A1、A2、A4、A9、A11、A12、A13、A14、A15、A16、A17、A20 仍确认存在；A3、A18、A19 仍是产品决策已定、待实施；A5、A6 原问题、A7、A8、A10 仍只保留防回归合同。

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
| 8 | 批次 8 | 低风险清理：`webui/api` 的 `Syncer` 接口成员 `Pause`/`Resume`/`Runtime`（均无生产调用）与零引用死代码 | 不捆绑新功能；开始前重新证明无生产调用；不得删除实现、辅助构建、验收代码或测试夹具（§六.4 F9） |

上一批验收前不得自动进入下一批。批次号沿用已确认合同，因此表中顺序有意为 `1 → 3 → 2 → 4 → 5 → 6 → 7 → 8`；批次编号未变，只有批次 4/6 的内容边界按 §六.4 F3 的用户裁决调整并在两处条目内各自留证。

## 三、待处理问题与最终修复合同

以下条目按实施顺序排列，不按编号排列。

### 批次 1：有界云请求与可靠重试

#### A1｜高｜阿里云 SDK 请求缺少应用层超时

- **状态与判定：** 确认存在，置信度高。正式 SWAS/ECS Provider 和两条扫描构造路径均未配置有限 deadline。
- **当前证据：** `provider/ali_swas.go:33-37`、`provider/ali_ecs.go:32-36`、`provider/scan.go:133-137`（`scanAliSWAS`）、`provider/scan.go:181-185`（`scanAliECS`）创建的 `openapi.Config` 只设置凭据与 Endpoint。当前依赖在零值下使连接、响应头和 HTTP client 超时均为 0：`darabonba-openapi/v2` 的 `ReadTimeout/ConnectTimeout`（`client.go:34-35`、`:127-128`）经 `tea/dara` 生效为 `http.Client.Timeout=Connect+Read`（`core.go:354`）、`Transport.ResponseHeaderTimeout=Read`（`core.go:535`）、`net.Dialer.Timeout=Connect`（`core.go:736-742`），三者全 0 即完全无界；`tea/dara/retry.go:274-281` 在未设 `RetryOptions` 时只尝试一次。SWAS v3 SDK 没有 `WithContext` 变体（`ListFirewallRules`/`CreateFirewallRules`/`DeleteFirewallRules`/`ListInstances` 均无 ctx 版本），ECS v7 虽有但生产未使用。腾讯 SDK 默认单次请求 60s（`profile/http_profile.go:45`），不属于本项。
- **调用链与影响：** 正式同步（`run.go:131` → `syncer.go:457` → `syncDomain` → `retrySync`）、Dry Run（`syncer.go:404`）、连接测试（`targets.go:169-175`）、资源扫描（`scan.go:49` → `:130/:178`）共用这些客户端；四条路径均无上游 deadline（云调用传 `context.Background()`）。连接成功后不返回、TLS/响应头挂起或网络黑洞可使 handler、同步轮次及 `s.Wait()` 长期停滞。该风险已在 localhost 阻塞端点隔离复现，但不等于真实阿里云事故。
- **最终方案：** 在 `provider` 包集中定义 `ConnectTimeout=10_000ms`、`ReadTimeout=30_000ms`，应用到 SWAS/ECS 正式 Provider及两条扫描路径。不新增 SQLite 设置或环境变量，不改 Provider 接口，不用不可取消的 goroutine + select 伪造超时，不改腾讯云 60s 行为。
- **必须保持：** 单轮只使用一个不可变运行时快照；重试仍完整执行 Describe → Diff → Create/Delete；不改变增量添加与精确删除契约；**四处超时值必须完全一致**——正式 Provider 与扫描路径共用同一 `ClientPool` 缓存键（`ali_swas.go:30` 与 `scan.go:132`；`ali_ecs.go:29` 与 `scan.go:180`，先创建者胜出），取值不同会让实际行为取决于调用顺序。
- **数值语义（实施记录用）：** `Connect=10s / Read=30s` 在该 SDK 下等价于「拨号上限 10s、响应头上限 30s、单次请求整体上限 40s」，不是 30s；文档与实施记录不得写成「单次 30s」。
- **修改与回归范围：** `provider/ali_swas.go`、`provider/ali_ecs.go`、`provider/scan.go` 及 provider 测试；建议在 `provider/common.go` 集中常量与一处共享构造 helper，避免四处漂移。使用真实慢 `httptest` 或本地阻塞服务覆盖四条构造路径，并断言在 deadline + 裕量内返回可被 A12 识别的错误；另加一条用例断言四个构造点的默认超时值恒为 10_000/30_000。
- **测试接缝（2026-09-27 用户已裁决，§六.4 F8）：** 允许在 `provider` 包内加入最小非导出接缝——一个把 `service/region` 映射为 `(endpoint, protocol)` 的包级变量（默认仍为 `<service>.<region>.aliyuncs.com` + `https`）与两个非导出超时变量（默认值必须仍为 10_000/30_000ms）。生产语义零变化；测试据此把四条路径指向本地阻塞服务并缩短等待。全仓无 `t.Parallel`，接缝不引入竞态。
- **外部边界：** 真实阿里云弱网/黑洞行为仍需用户真机验证，不得由本地阻塞服务替代。

#### A12｜中｜常见超时错误未进入重试

- **状态与判定：** 确认存在，置信度高；与 A1 强绑定。
- **当前证据：** `syncer/retry.go:86-101` 的 `isRetryable` 依赖大小写敏感字符串，调用点为 `retry.go:35`（GetRules）、`:53`（DeleteRules）、`:70`（CreateRules）。标准库常见错误 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)` 不含小写 `timeout`，当前会被判为不可重试；隔离复现中 `errors.Is(..., context.DeadlineExceeded)` 与 `net.Error.Timeout()` 均可正确识别（`url.Error` 同时实现 `Unwrap` 与 `Timeout()`）。**本轮新增证据（实施必须覆盖）**：腾讯 SDK 会把网络错误转成 `*TencentCloudSDKError{Code:"ClientError.NetworkError", Message:"Fail to get response because … context deadline exceeded (Client.Timeout exceeded while awaiting headers)"}`（`common/client.go` 的发送路径 → `common/netretry.go:54`），该类型**没有 `Unwrap`**（`common/errors/errors.go` 全文无 `Unwrap`），因此真实腾讯超时上结构化判断**不成立**，只能靠字符串兜底；阿里云路径（`tea/dara`）原样返回 `*url.Error`，结构化判断有效。
- **影响：** 腾讯 SDK 超时以及 A1 修复后新增的阿里云超时可能只尝试一次，直接告警并写失败日志，与最多 3 次指数退避的契约不一致。
- **最终方案（2026-09-27 用户裁决，§六.4 F2：结构化与兜底两者都做）：** `isRetryable` 依序执行三段判断——① `errors.Is(err, context.DeadlineExceeded)`；② `errors.As(err, &netErr)` 且 `netErr.Timeout()`；③ 字符串兜底：保留现有 5 项（`RequestLimitExceeded`、`InternalError`、`FirewallBusy`、`timeout`、`connection refused`）并把比较改为**大小写不敏感**（message 与关键字统一小写），同时新增云错误码 `ClientError.NetworkError`。语义：腾讯 SDK 的网络类错误（含超时）进入重试；其余判定顺序与幂等优先级不变。
- **必须保持：** 最多 3 次、既有指数退避（1s、2s）、完整重走同步流程、幂等“已存在/已不存在”判定先于可重试判定且不计数不重试、云厂商限速间隔均不变；放宽带宽后不得让 CVM 规则上限等**有意不可重试**的错误变成可重试（`tc_cvm.go:231-233` 的硬错误保持不可重试）。
- **修改与回归范围：** `syncer/retry.go` 与专项测试。表驱动覆盖真实 `http.Client.Timeout` 产生的 `*url.Error`、`context.DeadlineExceeded`、`i/o timeout`、`connection refused`、`InternalError`、**SDK 形状的 `[TencentCloudSDKError] Code=ClientError.NetworkError …`** 与未知错误；端到端 fake Provider 需证明真实超时会进入第 2 次完整尝试（第 1 次返回真实超时错误，断言发生第 2 次 Describe→Diff）。
- **依赖风险：** 必须与 A1 同批交付；先放宽重试而仍无单次调用上限，会进一步拉长无界轮次。

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

#### A16｜低｜`Start`、`Wait`、`Run` 重复调用契约不完整

- **状态与判定：** 确认存在；生产当前只调用一次，因此不是已发生故障。
- **当前证据：** `webui.Server.Start`（`server.go:133-176`）先在 `:135` `net.Listen`、`:159-162` 覆盖 `s.httpServer/s.listener`，**之后**才 `serveOnce.Do`；第二次调用会先占用/降级到随机端口并覆盖字段，第二个 listener 无人 Serve 且**永远不会被关闭**（`http.Server.Shutdown/Close` 只关闭 `Serve` 期间登记的 listener），同时 `Shutdown` 只作用于未服务的第二个 `http.Server` → **真正在服务的第一个 Serve 永不停止，`run.go` 的 `srv.Wait()` 永不返回**，并产生一条伪 `EADDRINUSE` WARN。`Server.Wait`（`:182-184`）从只在 `:167` 投递一次的容量 1 channel 读取，第二次/并发第二次会永久阻塞。`Syncer.Run`（`syncer.go:114-115`）第二次执行会重复关闭 `doneCh` 而 panic，**首个 Run 已退出后再调用同样 panic**（`Syncer.Wait` 读 closed channel，本身可多次安全等待，不属本项缺陷）。`Stop`/`Shutdown` 已幂等，不需重做。
- **最终方案（2026-09-27 用户裁决，§六.4 F7）：**
  1. `Server.Start` 在 `net.Listen` **之前**用锁保护的 started 判定拒绝第二次调用，返回明确错误（`ErrAlreadyStarted`，返回值 `(0, err)`），保持首个 listener 与 `httpServer` 不变，不新建、不降级、不产生伪 WARN；**Shutdown 之后再调用 `Start` 同样拒绝**。
  2. `Server.Wait` 保存唯一 Serve 结果，以关闭完成 channel 广播，使多次/并发 Wait 返回同一结果；归一化仍在 Serve 返回处完成。
  3. `Syncer.Run` 用 mutex 或 atomic CAS 拒绝**一切**第二次调用（含首个已退出后），WARN 并立即返回；**不得使用会让第二个调用等待首个 Run 结束的 `sync.Once.Do`**。
- **必须保持：** 仅 `EADDRINUSE` 才随机端口；Shutdown 先关闭 SSE shutdown channel，再执行 HTTP Shutdown，超时才 Close；Stop/Shutdown 继续幂等；归一化 `http.ErrServerClosed`/`net.ErrClosed` 的时机不变；不新增常驻 goroutine（`TestShutdownNoGoroutineLeak` 会扫描 `webui.(*Server).` 栈）。
- **修改与回归范围：** `webui/server.go`、`syncer/syncer.go` 及对应测试。覆盖 `Start` 两次不泄漏且保持首地址（白盒比较 listener 指针）、并发/重复 `Wait` 同结果、`Run` 两次不 panic 且有界返回。以下既有用例必须继续通过：`TestWaitBeforeStartBlocksUntilServeExits`、`TestServeRuntimeErrorSurfacedToCaller`、`TestShutdownNormalizesServeResult`、`TestShutdownIdempotent`、`TestShutdownBeforeStart`、`TestShutdownTwiceBeforeStart`、`TestShutdownAfterServeExitedRepeated`、`TestShutdownNoGoroutineLeak`。

### 批次 2：告警发送有界化

#### A2｜高｜SMTP 无 deadline，异步告警无并发上界

- **状态与判定：** 确认存在，置信度高。SMTP 无 deadline 与并发无上限是代码事实；生产资源耗尽仍只是可信推论。
- **当前证据：** `notifier/bus.go:109-115` 为每个接口订阅者启动一个 goroutine（无上限、无丢弃、无跟踪）；`notifier/email.go:49` 使用 `smtp.SendMail`——标准库内部 `smtp.Dial` 即 `net.Dial("tcp", addr)` 无 timeout，greeting（`ReadResponse(220)`）、STARTTLS、AUTH、MAIL、RCPT、DATA、QUIT 全链路无 deadline；Webhook client 已有 10s 超时（`notifier/webhook.go:26`）。全仓通知链路零并发上界（`syncer.go:448` 的 `WaitGroup` 只等轮次内云调用）。同步轮次不等待告警，进程退出也不等待在途告警（`run.go:182` 只等 `s.Wait()`）。localhost 静默 SMTP 与阻塞订阅者已分别复现长时间不返回及 goroutine 线性增长。
- **最终方案（限流归属按 2026-09-27 用户裁决，§六.4 F5）：** SMTP 改为 `net.Dialer{Timeout:10s}` → `conn.SetDeadline(now+30s)` → `smtp.NewClient`，完整保留 greeting、STARTTLS、AUTH、MAIL、RCPT、DATA、QUIT；deadline 覆盖初始 greeting 与 QUIT。邮件与 Webhook **各自**最多 4 个在途任务；满载丢弃最新通知，每次记录不含密码、URL 或正文敏感值的 WARN。限流器**由 `AlertManager` 按渠道持有并注入 notifier 实例**，使“每渠道在途 ≤4”在配置热重载（旧实例在途发送不取消、不等待）后仍然成立；上限位于告警订阅者/调度边界，不能限流整个 EventBus。
- **明确不做：** 不新增持久队列、告警重试、进程退出等待或重型 worker 框架；`Publish` 继续非阻塞，StoreLogWriter 与 SSE 不受影响。
- **测试接缝（2026-09-27 用户裁决，§六.4 F8）：** 允许把 10s/30s 定义为 notifier 包内非导出变量（默认值不得改变），同包测试可缩短以断言机制；Webhook 的 10s 若需用例化同样处理。
- **修改与回归范围：** `notifier/email.go`、`notifier/webhook.go`、新增每渠道 limiter（可置于 `notifier/limit.go`）、`webui/api/alertset.go`（按渠道持有并注入 limiter）与 notifier/alertset 测试。静默 SMTP 必须在 deadline + 裕量内返回并关闭连接；阻塞邮件/Webhook 下断言各自在途不超过 4，溢出丢弃最新且 WARN 可见（“在途”的确定性观测量建议用假服务端已接受的连接数/并发数，不得用 `runtime.NumGoroutine`）。以下既有夹具与用例必须保持绿：`notifier/bus_test.go` 全部（尤其 `SlowCallbackDoesNotHoldLock`、`SubscriberErrorIsolation`、`FullBufferDoesNotBlock`、`CancelDoesNotCloseChannel`）、`webui/api/alertset_test.go` 全部（含 `ApplyBoundaryInFlightAndNewSubscriptions`）、`main_test.go` 的 `TestProcessSecretsNotLogged`。
- **外部边界：** 真实 SMTP/收件箱与 Webhook 人工验收此前已被用户免除，至今仍没有真实通过结论；本地假服务不得写成真实链路通过。

### 批次 4：同步健康、轮次汇总、SSE 与 Provider 写入计数接口（A11 前移部分）

#### A3｜中｜HTTP 健康检查不表达同步健康

- **状态与判定：** 产品语义已定，待实施；这不是现有 `/api/health` 的契约错误。
- **当前证据：** `/api/health` 是纯静态 handler（`webui/server.go:240-243`），不读 Store/Syncer/运行时；Docker HEALTHCHECK（`build/Dockerfile:31-33`）与 Compose healthcheck（`docker-compose.yml.example:45-54`）只调用该端点；同步状态目前缺少自动的成功/失败/停滞表达（`SyncStatus` 只有 `running/enabled/last_sync`）。
- **最终方案：** `/api/health`、Docker HEALTHCHECK 与容器重启语义保持不变；同步健康仅通过向后兼容扩展 `/api/sync/status` 和 Dashboard 提示表达，字段口径与 A18 共用（`last_success` + `last_round.outcome`）。
- **必须保持：** 暂停、首次启动、无目标或空轮次不能使 HTTP health 返回 503；不设置全局 `ReadTimeout/WriteTimeout`；不新增第二个状态大字或小号操作按钮（沿用 Dashboard 既有的 `NAlert` 与三态展示约定）。
- **口径澄清（本轮补充）：** “停滞”不引入任何时间阈值判定，只由 `last_success` 缺失或落后于 `last_sync` 与 `outcome` 表达，避免暂停期或重启后（两个时间戳均为内存态、重启归 null）产生误报；Dashboard 提示必须走既有的 5s 轮询 `/api/sync/status`，不得依赖 `/api/sync/events`（该 SSE 无回放、缓冲满即丢，`sync:complete` 可能对已连接客户端不可见）。
- **修改与回归范围：** sync 状态、API、Dashboard 和 README。测试同时断言新同步字段存在且 `/api/health` 在暂停/无目标/空轮次/从未成功等状态下仍精确返回 `{"status":"ok"}` 与 200；前端提示需浏览器验收。

#### A18｜中｜缺少 `last_success` 与整轮成功/失败汇总

- **状态与判定：** 产品语义已定，待实施。退出时无超时等待当前轮是 AGENTS 强要求，不作为本项缺陷。
- **当前证据：** 当前轮结束后无条件刷新 `last_sync`（`syncer.go:464-466`），整轮完成事件 `EventSyncComplete.Data` 只有 `duration`（`:469-473`）；`SyncStatus`（`:322-329`）只有 `running/enabled/last_sync`；逐域结果只发事件，无任何整轮聚合；失败只能从逐域日志/告警侧推断。
- **最终统计合同（字段口径按 2026-09-27 用户裁决，§六.4 F4）：** 一个统计单元为“一个 Provider × 一条适用规则”，`total = Σ_p len(filterRulesForTarget(...))`。每轮至少包含 `finished_at/total/ok/changed/failed/skipped/added/deleted/duration/outcome`，其中 `ok` 严格表示**成功且无变更**的单元，`changed` 表示**成功且发生了增删**的单元，恒有不变量 `total == ok + changed + failed + skipped`。DNS/Provider 错误记 failed；明确未实施操作记 skipped。只有 `total>0 && failed==0 && skipped==0` 为 success 并更新 `last_success`；failed>0 为 failed；无失败但有 skipped 为 partial；无 Provider/适用规则为 idle；后三者都不更新 `last_success`。
- **兼容合同：** `last_sync` 仍表示最近一轮完成；暂停期不制造轮次或刷新时间；`EventSyncComplete.Data` 保留 duration 并增加同一汇总；`SyncStatus` 的既有三字段语义与类型不变；本批不增加 SQLite 列。
- **修改与回归范围：** `syncer` 状态/汇总、`GET /api/sync/status`、Dashboard 和事件测试。覆盖全成功、成功含增删（changed）、部分失败、仅 skipped、idle、从未成功、暂停；并行计数必须通过 race。
- **依赖与边界：** 依赖 A1/A12；依赖 A11 的 `CreateRules` 返回值前移到本批（§2.2、§六.4 F3），否则 `skipped`/`partial` 在批次 4 无法端到端判别；Dashboard 需要浏览器人工验收。

#### A13｜低｜普通轮次重复记录“告警已更新”

- **状态与判定：** 确认存在；属于日志噪声和误导。
- **当前证据：** `syncer.go:218` 在每次 select 返回后**无条件**调用 `logAppliedState(latest)`（ticker/trigger/control 三条路径都会走到），`run.go:103` 注入的回调是 `alertManager.LogStatus("已更新")`，而 `alertset.go:113-121` 会按已启用渠道输出 INFO；该日志经 `MultiHandler`（`app/logutil.go`）进入 WebUI 日志流，因此启用渠道时 ticker/trigger 每轮都会输出与配置变化无关的“告警已更新”。
- **最终方案：** 不新增可变 generation。Run 保存上次已消费的不可变 `RuntimeState` 指针（以 Run 起始快照为基线），只在指针变化且 Run 真正消费状态后触发 hook；普通轮次不触发。启动“已启用”（`run.go:102`）保持独立。
- **必须保持：** hook 仍是“Run 已消费状态”的屏障（`sync:start`/导入相关既有用例依赖该语义，且 control 通知可合并、hook 次数允许少于 ApplyState 次数）；日志不得包含 SMTP 密码或 Webhook URL。
- **回归：** 多轮无配置变化时 hook 次数不增长（修复前随轮次增长）；ApplyState 一次后只增加一次，且此刻 `IsEnabled()` 已反映新状态。`main_test.go` 中“PUT 后出现 Webhook 告警已更新”的既有断言必须继续通过。

#### A15｜低｜SSE 忽略写错误，半开连接缺少单次写出边界

- **状态与判定：** 忽略写错误已确认并隔离复现；Flush 半开阻塞尚未真实网络复现。
- **当前证据：** `webui/api/sync.go:126-127` 与 `webui/api/logstream.go:158-159` 在 `fmt.Fprintf` 后直接 `Flush`，两者的返回值都未检查；`http.ResponseController` 全仓零使用；全局 `WriteTimeout` 按长连接契约保持零（`server.go:155-156`，并有 `TestServerTimeoutContract` 断言）。当前的 Flusher 能力检测分支（`sync.go:99`、`logstream.go:136`）在初始 Flush 失败时无法再改写状态码。
- **最终方案：** 使用可检查错误的写出/刷新路径（优先 `http.ResponseController`），写错误立即返回并由既有 `defer unsubscribe()` 取消订阅；初始 Flush 失败时直接返回，**不得再写第二个响应头或 500**。若加入单次写 deadline，必须覆盖初始 Flush、每次写前刷新并在写后清除，并容忍 `http.ErrNotSupported`。
- **必须保持：** 不设置全局 `WriteTimeout`；继续监听 request context 与服务器 shutdown channel；取消订阅不关闭 EventBus channel；`LogBroadcaster` 的订阅关闭语义不变。
- **修改与回归范围：** 两类 SSE handler 及测试。用自写 erroring `http.ResponseWriter`（`Write` 返回 `io.ErrClosedPipe` 等）断言首个失败即返回且订阅已取消（修复前会永久循环）；若做 deadline，再验证初始与每次写出（记录型 writer 断言设置与清零）。
- **外部边界：** 真实半开 TCP/停止读取客户端仍需人工验证；本批不得把该场景写成已验证。

### 批次 5：SQLite、损坏数据与错误处理

#### A4｜中高｜SQLite `busy_timeout` 未覆盖每条物理连接

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

#### A9｜中｜损坏的 `rules.targets` 被静默扩大为全部目标

- **状态与判定：** 确认存在，需外部改库或损坏触发；后果具有安全方向影响。
- **当前证据：** `config/store.go:224-229` 的 `loadRules` 在 `targets != ""` 时解析 JSON，解析失败不报错、静默保留 nil；空 Targets 被解释为全部目标（`syncer.go:558-560`）。`null` 会被 `json.Unmarshal` 成功解成 nil 切片，因此当前与 `""` 一样被当成全部目标；损坏还会绕过引用检查（`ReferencingRuleIDsTx` 同样经 `loadRules`）并在导出/导入中固化为 `[]`（`bundle_v2.go` + `export.go`）。物理 SQL NULL 现在会在 `Scan` 阶段报 `unsupported Scan … into type *string`，该错误**不含规则 ID**。**历史事实（本轮新增证据）：** `33649b1`/`ad288c0` 时期的写路径是 `json.Marshal(r.Targets)`，nil slice 会写出字面量 `null`，直到 `c289744`（Step 4）才改为 `[]`——即存量库中可能存在**合法产生**的 `targets='null'` 行。
- **最终四态合同（2026-09-27 用户裁决，§六.4 F1：保持严格口径）：** 历史空串 `""` 兼容为全部目标；`[]` 是合法全部目标；`null`、对象、标量、非整数数组及解析失败全部返回带规则 ID、但不含原值的内部错误。`null` 不得视为 `[]`。
- **错误边界：** 损坏必须阻断同步、导出、完整快照和目标引用检查；返回安全 500，不静默放行或扩大。四条链（同步快照、导出、引用检查、启动加载）经复核都能把 `loadRules` 错误升到安全 500 或启动中止，无上游吞错。存量库若含 `null` 或损坏值，升级后 fail-closed 是**预期行为**；发布说明必须给出修复提示：`UPDATE rules SET targets='[]' WHERE targets IS NULL OR targets='null';`，或直接重新导入 version 2 配置包（导入先清空 rules 再写入，可修复损坏库）。
- **实现要点：** `Scan` 改 `sql.NullString` 以覆盖物理 NULL；用 `nums == nil` 区分 `null` 与 `[]`（`[]` 解出非 nil 空切片），无需回显原值；错误文本只带 `#id`，HTTP body 仍是既有安全文案。
- **修改与回归范围：** `config/store.go` 与 config/syncer/webui-api 测试。直插所有形态（`''`、`'[]'`、`'null'`、`'{}'`、`'1'`、`'[1,"a"]'`、NULL、`'[1]'`）并验证四条调用链；补 `GET /api/rules` 的 500 安全文案回归（`/api/targets` 已有同类用例）；断言错误文本含规则 ID 且不含原值。

#### A14｜低｜Rollback、Encode 与日志裁剪错误未完整处理

- **状态与判定：** 确认存在，违反 AGENTS “所有 error 必须处理”；多数触发条件苛刻。
- **当前证据：** `config/store.go:291-301` 的 `WithTransaction` 忽略 Rollback 返回值、无 defer，`fn` panic 时事务与连接不会被回滚（`*sql.Tx` 没有 finalizer，WAL 下泄漏的写事务会让后续写入持续 BUSY）；`webui/api/deps.go:199-203` 的 `writeJSON` 忽略 `Encode` 返回值（全仓 36 处调用）；`config/store.go:631` 的 `AddSyncLog` 吞掉 COUNT 错误。**范围修正（本轮复核）：** 裁剪 `DELETE` 的错误**已经**返回（`:632-635`），Issue6 原表述“COUNT/清理错误可能被忽略”偏严，实际只有 COUNT 需要处理。
- **最终方案：** 使用 `committed` 标志 + defer 回滚，忽略 `sql.ErrTxDone`、记录其他安全错误，panic 时回滚后重新 panic；处理 COUNT 错误（安全日志，不改变裁剪语义）；`writeJSON` 处理 Encode 错误——响应头已发出后只记安全日志，不伪造第二个 HTTP 错误响应（参照 `export.go:74-77` 的既有先例）。
- **明确不做：** 不因本项引入新框架或强行增加 errcheck 门禁；不新增连接池上限；不改裁剪阈值与语义。
- **修改与回归范围：** `config/store.go`、`webui/api/deps.go` 与测试。覆盖 panic 回滚（断言后续写入不 BUSY、旧行未被写入）、已结束事务（`sql.ErrTxDone` 忽略）、裁剪 COUNT 查询失败（注入）与 1001 条裁剪回归、失败 ResponseWriter（自写 stub，断言不 panic 且不第二次 `WriteHeader`）。

#### A17｜低｜非预期迁移失败只告警后继续

- **状态与判定：** 确认存在；两条 ALTER 对旧库仍有作用，不能删除为“永久无用”。
- **当前证据：** `config/store.go:153-159` 的两条 ALTER（`rules ADD COLUMN enable_ipv6`、`alert_webhook ADD COLUMN channel`）对非 duplicate-column 错误只 `slog.Warn` 后继续，函数末尾 `return nil`。这两条 ALTER 对应 Build1 时期的旧库形态（`CREATE TABLE IF NOT EXISTS` 不会给已存在表补列），因此仍然必要。
- **最终方案：** duplicate-column 继续视为幂等成功；其他 ALTER 错误立即从 `OpenStore` 返回并中止启动。不引入 `schema_migrations` 表。
- **实施建议：** 把 `initTables` 拆成 `createSchema(q DBTX)` 与 `migrateColumns(q DBTX) error`，测试用返回哨兵错误的假 `DBTX` 直接注入非预期失败（比“用 VIEW 顶替 rules 表”等取巧更确定），保持 duplicate-column 仍视为成功。
- **必须保持与回归：** 新库、已迁移库、已有表但缺列的旧库都能正确打开；覆盖 duplicate-column 与可控非预期失败两类路径，并断言非预期失败时 `OpenStore` 返回错误而不是返回可用 Store。

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

### 批次 8：低风险清理（无行为变更）

本批范围由 2026-09-27 用户裁决固定（§六.4 F9），只包含“零生产调用”的接口成员与零引用死代码，**不捆绑任何新功能**，且必须在删除前逐项重新证明无生产调用：

| 目标 | 位置 | 复核结论（2026-09-27） |
|---|---|---|
| API `Syncer` 接口成员 `Pause()`、`Resume()` | `webui/api/deps.go` | handler 只经协调器写入（A5 已修复），全仓无 `d.Syncer.Pause()/Resume()` 调用；`syncer` 侧实现必须保留 |
| API `Syncer` 接口成员 `Runtime()` | `webui/api/deps.go` | 生产只用 `Deps.Runtime` 字段（`runtimeSnapshot()`），接口方法唯一调用点在测试且走具体类型 |
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
- 批次 8 按 §六.4 F9 的用户裁决移除 API `Syncer` 接口中的无生产调用成员 `Pause()`、`Resume()`、`Runtime()`；**不得**删除 `syncer` 侧的具体实现方法（Build6 明确要求保留）、辅助构建/验收代码或任何测试夹具。移除接口成员后，`sync_test.go` 中“handler 未二次调用 Pause/Resume”的断言必须同步改写并如实记录：该保证从运行时断言升级为**编译期不可能**，A5 的实质契约继续由数据库值 + 恰好一次 apply 的断言承担。

#### A6｜已修复｜暂停后排队 ticker/trigger 仍启动同步

- ticker 与 trigger 消费前检查已发布状态；暂停期不得启动新轮。A20 是独立 Stop 问题。

#### A7｜已修复｜恢复同步可能漏掉立即一轮

- false → true 使用 Run 已处理相位并立即一轮；true → true 只重置 ticker。不得回退到已发布镜像。

#### A8｜已修复｜完整导入未重置 DNS breaker

- 完整导入确定 Reset；普通变更 Preserve 并应用新阈值，且已有端点级判别性回归。批次 7 只把 AGENTS 的“完整导入允许新建并清空”收紧为“确定新建 breaker 并清空计数；普通变更保留既有失败计数”，不得重复修改实现。批次 7 中 AGENTS 的平台约束句、README 平台声明与 pidfile 回归属 A19 范围（§六.4 F9），与 A8 无关、不得混写。

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

- 2026-09-27 在 `54efb10` 与 `1fafb17` 上完成全量只读复核；当前 HEAD 相对 `1fafb17` 无源码变化。
- 当时 `go test ./... -race -count=1`（11 包）与 `go vet ./...` 通过；通用门禁不等于覆盖 Issue6。
- `/tmp/fwreview` overlay 隔离复现确认 A1、A2、A4、A9、A11、A12、A13、A15、A16、A17、A20；均使用虚构数据、localhost 假服务和临时数据库。
- Build6 Step 7 已修复 A5、A6、A7、A8、A10，并保留判别性回归。
- **2026-09-27 第二轮全量只读复核（本轮，未运行 build/test/vet）：** 基线 HEAD `0d9e2d6`，相对 `origin/main` ahead 2，工作树干净；`git diff --stat 1fafb172307652912a621eaebf4bf942cbd2c33f..HEAD` 只含 `Issue6.md`，源码零变化。工具链：Go `1.26.6 darwin/arm64`、Node `26.7.0`、npm `11.19.0`、Docker `29.8.0`、Compose `v5.5.1`；`go.mod` 依赖版本与 Step 7 记录一致。复核方式为源码逐行阅读 + 只读阅读 `$GOMODCACHE` 内的 SDK / `modernc.org/sqlite` 源码；新增确认的证据包括：A1 四处构造点与零值语义、共享 `ClientPool` 缓存键要求同值、A12 的腾讯 SDK 错误包装无 `Unwrap`、A20 的 4 处 `syncAll` 调用点与两条额外可达路径、A16 的 listener 泄漏与 `Shutdown` 作用错对象、A2 的标准库 SMTP 无 deadline 链路、A4 的驱动 `_pragma` 逐连接生效与相对路径/`?` 路径缺陷、A9 的历史 `null` 来源、A11 的永不收敛与接口破坏面、A19 的 Windows 面仅 4 处且无 pidfile 单实例回归。上述结论已并入各条目。

### 6.2 尚未取得的证据

- 未以 Issue6 修复版本验证真实云弱网/超时、SWAS DROP 计数、真实 SMTP/收件箱或 Webhook。
- 未访问生产 SQLite，也未验证真实旧库的损坏 targets 或迁移失败。
- 未执行 Issue6 新功能的浏览器、Docker、真机、半开 TCP、长跑资源耗尽或远端 CI 验收。
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
| F9 | 批次 8 范围与 A19 文档范围 | 批次 8 **包含**接口 `Pause`/`Resume`/`Runtime` 与全部零引用死代码；A19 顺带把平台约束写入 AGENTS/README 并补 pidfile 单实例回归 | §2.2 批次 8、A5、A19、§六.5 |

### 6.5 本轮复核发现、但未纳入合同的后续候选（不授权实施）

以下问题均由本轮只读复核确认存在，但不在 A1～A20 的固定合同与本次授权范围内，**不得顺手修改**；如需处理必须另行提出、另行授权并另行编号：

1. **删除侧同型“跳过仍计数”**：`retry.go:59` 以 `len(diff.ToDelete)` 累加 `deleted`，而 CVM 无效 PolicyIndex 静默 `continue`、SWAS/ECS 空 RuleID 被过滤后仍 `return nil`；重试轮还会让同一跳过项按轮重复计数。
2. **反向缺口“写了却报 0”**：非可重试错误时 `retrySync` 返回的 added/deleted 在 `syncDomainInternal` 的错误分支被丢弃，`sync_logs` 记 `failed/Added=0`，ECS 分批或 CVM 逐条删除中途失败会出现“实际已写但历史记 0”。
3. **`buildDesired` 静默丢弃不上报**：SWAS IPv6 与 ECS ICMPv6 在 Diff 阶段被丢弃，Dry Run 与事件中完全不可见（仅 ECS 有一条 WARN），与 A11 追求的 skipped 口径不一致。
4. **dangling 目标引用无校验**：外部删除 targets 行后，规则会静默“作用于 0 个目标”，既不报错也不告警。
5. **通知链路残余无界**：`StoreLogWriter` 仍按事件派生 goroutine 写 SQLite，DB 慢或被锁时仍会堆积。
6. **SSE 契约缺口**：`/api/sync/events` 无 `id:`/`Last-Event-ID`/回放，缓冲（cap 32）满即丢，`sync:complete` 可能对已连接客户端不可见；两类 SSE 均无心跳。
7. **Dashboard 数字双口径**：统计概览仍取最近一条逐域日志的 added/deleted，与 A18 的整轮汇总并存会出现两套数字。
8. **云侧错误码与重试面**：阿里云 `Throttling`/`ServiceUnavailable` 等不在可重试列表（F2 只新增腾讯 `ClientError.NetworkError`）。
9. **Webhook 细节**：`resp.Body.Close()` 错误被忽略且不 drain body（影响 keep-alive 复用）。
10. **SMTP 收件人解析**：`strings.Split(To, ",")` 未逐项 `TrimSpace`，`"a@x.com, b@y.com"` 会产生带前导空格的收件人。
11. **时间戳纯内存**：`last_sync` 与新增的 `last_success` 重启归 null，UI 文案需避免表述为“从未成功”。
12. **SQLite 其他缺口**：未设置任何连接池上限；`AddSyncLog` 的 `COUNT(*)` 是全表扫描且不在事务内；`WithTransaction` 使用无 context 的 `Begin()`；`PRAGMA foreign_keys` 未设置。
13. **`sync_logs.result` 的 `skipped` 语义**：`SyncLog.Result` 注释已预留 `skipped`，但生产只写 success/failed，A11 明确不加列，因此“零写入且有跳过”如何落库仍未定义。
14. **生命周期边角**：`Server.Addr()` 在 Shutdown 后仍返回已关闭地址；`Syncer.Wait()` 在 Run 从未启动时会永久阻塞（A16 的拒绝路径需保证可观测，避免调用方随后 Wait 挂死）。

---

**当前结论：** Issue6 已完成第二轮全量只读复核，全部冲突已由用户裁决并并入本文（§六.4），§六.5 的后续候选明确不授权实施。本文已具备构建前使用条件，但待处理项仍未获代码实施授权。下一步等待用户最终审核并给出一次性授权后，再严格按 §2.2 顺序（`1 → 3 → 2 → 4 → 5 → 6 → 7 → 8`）处理全部批次。
