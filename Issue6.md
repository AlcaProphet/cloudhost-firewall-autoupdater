# Issue6.md — FWAlizer 后续问题与修复合同

> **文档定位：** 本文是 Build6 完成后的当前问题记录与后续修复合同。它只描述问题、已确认的产品语义、建议实施边界和验收要求；除明确标记为“已修复”的条目外，不代表代码已经修改或外部链路已经验收。
>
> **当前基线：** 2026-09-27，分支 `main`，整理前 HEAD `1392ab9b675d0cf79a04f5f4a0f2f5cd966dec36`，相对 `origin/main` ahead 1，工作树干净。HEAD 相对上一轮源码核验基线 `1fafb172307652912a621eaebf4bf942cbd2c33f` 只修改了本文，因此下列源码结论与最终合同仍适用于当前实现。
>
> **授权边界：** 本次只规范化本文，不修改设计、源码、测试、依赖、配置或其他文档，也不开始任何修复。后续必须先完成全量文档和当前代码复核，经用户最终审核并一次性授权后，才可按本文批次实施。

## 一、使用规则

1. `AGENTS.md` 是唯一强要求；`Design5.md`、`Build6.md`、`Issue5.md` 和本文属于设计、构建与问题记录。若彼此或与当前代码冲突，必须向用户说明并等待决策。
2. 每个问题的细节与修复方案都记录在同一条目中；不得只读总表或批次表后直接施工。
3. 当前没有待用户裁决的产品语义。已固定的决定不得由实施者重新选择；发现新冲突时才使用提问工具，并附推荐选项与理由。
4. 不得把源码推理、隔离复现、自动测试、真实云、真实 SMTP/收件箱、真实 Webhook、浏览器、Docker 或生产事故互相替代。
5. A5、A6 原问题、A7、A8、A10 已修复，只保留防回归合同；A20 是与 A6 相邻但独立的 Stop 缺陷。
6. 本文不授权删除辅助构建代码、辅助验收代码、判别性测试夹具或已经固定的契约。低风险清理也必须在明确范围内实施。

## 二、状态与实施顺序

### 2.1 当前状态

| 状态 | 条目 |
|---|---|
| 确认存在，待处理 | A1、A2、A4、A9、A11、A12、A13、A14、A15、A16、A17、A20 |
| 产品决策已定，待实施 | A3、A18、A19 |
| 已修复，保留回归 | A5、A6 原问题、A7、A8、A10 |
| 待用户决策 | 无 |

### 2.2 固定实施顺序

| 顺序 | 批次 | 条目 | 依赖与停止条件 |
|---|---|---|---|
| 1 | 批次 1 | A1 + A12 | 先使阿里云调用有界，再把结构化超时纳入重试；两项同批交付 |
| 2 | 批次 3 | A20 + A16；复核 A6/A7 边界 | 只阻止 Stop 后的新轮次，不中断当前轮；必须有确定性竞态回归 |
| 3 | 批次 2 | A2 | 每个告警渠道在途上限 4；满载丢弃最新并 WARN |
| 4 | 批次 4 | A3 + A18 + A13 + A15 | 依赖批次 1；一次固定同步健康、轮次汇总与 SSE 口径 |
| 5 | 批次 5 | A4 + A9 + A14 + A17 | 同文件不等于同一改动；各条目独立审查，共用整包回归 |
| 6 | 批次 6 | A11 | 在批次 4 的汇总字段定型后实施，并同步 Dry Run 语义 |
| 7 | 批次 7 | A19 + A8 文档收口 | 独立平台/依赖变更；仅本批涉及 `go.mod`、`go.sum` 和 AGENTS A8 措辞 |
| 8 | 批次 8 | A5 残留接口等低风险清理 | 不捆绑新功能；开始前重新证明无生产调用 |

上一批验收前不得自动进入下一批。批次号沿用已确认合同，因此表中顺序有意为 `1 → 3 → 2 → 4 → 5 → 6 → 7 → 8`。

## 三、待处理问题与最终修复合同

以下条目按实施顺序排列，不按编号排列。

### 批次 1：有界云请求与可靠重试

#### A1｜高｜阿里云 SDK 请求缺少应用层超时

- **状态与判定：** 确认存在，置信度高。正式 SWAS/ECS Provider 和两条扫描构造路径均未配置有限 deadline。
- **当前证据：** `provider/ali_swas.go`、`provider/ali_ecs.go`、`provider/scan.go` 创建的 `openapi.Config` 只设置凭据与 Endpoint。当前依赖在零值下使连接、响应头和 HTTP client 超时均为 0；SWAS 请求无 `context.Context` 接口。腾讯 SDK 默认单次请求 60s，不属于本项。
- **调用链与影响：** 正式同步、连接测试、资源扫描和 Dry Run 共用这些客户端。连接成功后不返回、TLS/响应头挂起或网络黑洞可使 handler、同步轮次及 `s.Wait()` 长期停滞。该风险已在 localhost 阻塞端点隔离复现，但不等于真实阿里云事故。
- **最终方案：** 在 `provider` 包集中定义 `ConnectTimeout=10_000ms`、`ReadTimeout=30_000ms`，应用到 SWAS/ECS 正式 Provider及两条扫描路径。不新增 SQLite 设置或环境变量，不改 Provider 接口，不用不可取消的 goroutine + select 伪造超时，不改腾讯云 60s 行为。
- **必须保持：** 单轮只使用一个不可变运行时快照；重试仍完整执行 Describe → Diff → Create/Delete；不改变增量添加与精确删除契约。
- **修改与回归范围：** `provider/ali_swas.go`、`provider/ali_ecs.go`、`provider/scan.go` 及 provider 测试。使用真实慢 `httptest` 或本地阻塞服务覆盖四条构造路径，并断言在 deadline + 裕量内返回可被 A12 识别的错误。
- **外部边界：** 真实阿里云弱网/黑洞行为仍需用户真机验证，不得由本地阻塞服务替代。

#### A12｜中｜常见超时错误未进入重试

- **状态与判定：** 确认存在，置信度高；与 A1 强绑定。
- **当前证据：** `syncer/retry.go` 的 `isRetryable` 依赖大小写敏感字符串。标准库常见错误 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)` 不含小写 `timeout`，当前会被判为不可重试；隔离复现中 `errors.Is(..., context.DeadlineExceeded)` 与 `net.Error.Timeout()` 均可正确识别。
- **影响：** 腾讯 SDK 超时以及 A1 修复后新增的阿里云超时可能只尝试一次，直接告警并写失败日志，与最多 3 次指数退避的契约不一致。
- **最终方案：** `isRetryable` 先检查 `errors.Is(err, context.DeadlineExceeded)`，再通过 `errors.As(err, net.Error)` 与 `Timeout()` 判断，最后保留现有云错误码字符串兜底。
- **必须保持：** 最多 3 次、既有指数退避、完整重走同步流程、幂等“已存在/已不存在”不计数且不重试、云厂商限速间隔均不变。
- **修改与回归范围：** `syncer/retry.go` 与专项测试。表驱动覆盖真实 `Client.Timeout`、`context.DeadlineExceeded`、`i/o timeout`、`connection refused`、`InternalError` 和未知错误；端到端 fake Provider 需证明真实超时会进入第 2 次完整尝试。
- **依赖风险：** 必须与 A1 同批交付；先放宽重试而仍无单次调用上限，会进一步拉长无界轮次。

### 批次 3：Stop 门控与生命周期

本批实施时必须同时保留 A6 的暂停门控回归与 A7 的恢复立即一轮语义，但不得把 A20 混写为 A6 已修复的一部分；两项完整防回归合同统一见 §四。

#### A20｜中｜`Stop()` 后仍可能启动一整轮同步

- **状态与判定：** 确认存在，置信度高。历史隔离复现先后出现 5/15 与 10/15；概率次数仅说明风险存在，不能作为最终唯一验收。
- **当前证据：** `syncer/syncer.go` 的启用态 select 同时监听 ticker、trigger、control 与 stop；ticker/trigger 守卫只检查 `IsEnabled()`，未检查 stop。多个分支同时就绪时可随机先消费 trigger/ticker，再启动新轮。
- **触发与影响：** 当前轮进行中已排队 trigger 或 ticker 到期，随后收到 SIGTERM。当前轮结束后可能再执行一整轮真实云写入，`s.Wait()` 继续无界等待，违背“完成当前轮次再退出”的意图。
- **最终方案：** 将 stop 设为新轮次的硬门控：Run 循环入口以及 ticker/trigger 进入 `syncAll` 前统一检查 stopped 状态。Stop 只挡新轮次，绝不取消或中断已开始的 `syncAll`。
- **必须保持：** Stop 幂等；当前轮完成后退出；暂停子循环不消费 ticker/trigger；不增加轮次超时；A6 的 enabled 守卫继续存在。
- **修改与回归范围：** `syncer/syncer.go`、`syncer/state_test.go`。必须使用 hook、屏障或抽取的门控函数构造确定性交错，断言 Stop 后 Provider 调用次数恒定；`-count=20` 仅作压力补充。

#### A16｜低｜`Start`、`Wait`、`Run` 重复调用契约不完整

- **状态与判定：** 确认存在；生产当前只调用一次，因此不是已发生故障。
- **当前证据：** `webui.Server.Start` 会在 `serveOnce.Do` 前再次创建 listener，第二次 listener 无人 Serve；`Wait` 从只投递一次的 channel 读取，第二次会阻塞；`Syncer.Run` 第二次执行会重复关闭 `doneCh` 并 panic。`Stop` 与 `Shutdown` 已幂等，不需重做。
- **最终方案：** `Server.Start` 在创建 listener 前拒绝第二次调用并保持首个 listener；`Server.Wait` 保存唯一 Serve 结果，以关闭完成 channel 广播，使多次/并发 Wait 返回同一结果；`Syncer.Run` 用 mutex 或 atomic CAS 拒绝第二个活动调用并 WARN，不使用会让第二个调用等待首个 Run 结束的 `sync.Once.Do`。
- **必须保持：** 仅 `EADDRINUSE` 才随机端口；Shutdown 先关闭 SSE shutdown channel，再执行 HTTP Shutdown，超时才 Close；Stop/Shutdown 继续幂等。
- **修改与回归范围：** `webui/server.go`、`syncer/syncer.go` 及对应测试。覆盖 `Start` 两次不泄漏且保持首地址、并发/重复 Wait 同结果、Run 两次不 panic。

### 批次 2：告警发送有界化

#### A2｜高｜SMTP 无 deadline，异步告警无并发上界

- **状态与判定：** 确认存在，置信度高。SMTP 无 deadline 与并发无上限是代码事实；生产资源耗尽仍只是可信推论。
- **当前证据：** `notifier/bus.go` 为每个接口订阅者启动 goroutine；`notifier/email.go` 使用无 deadline 的 `smtp.SendMail`；Webhook client 已有 10s 超时。同步轮次不等待告警，进程退出也不等待在途告警。localhost 静默 SMTP 与阻塞订阅者已分别复现长时间不返回及 goroutine 线性增长。
- **最终方案：** SMTP 改为 `net.Dialer{Timeout:10s}` → `conn.SetDeadline(now+30s)` → `smtp.NewClient`，完整保留 greeting、STARTTLS、AUTH、MAIL、RCPT、DATA、QUIT；deadline 覆盖初始 greeting 与 QUIT。邮件与 Webhook 各自最多 4 个在途任务；满载丢弃最新通知，每次记录不含密码、URL 或正文敏感值的 WARN。上限位于告警订阅者/调度边界，不能限流整个 EventBus。
- **明确不做：** 不新增持久队列、告警重试、进程退出等待或重型 worker 框架；`Publish` 继续非阻塞，StoreLogWriter 与 SSE 不受影响。
- **修改与回归范围：** `notifier/email.go`、告警调度相关代码与 notifier 测试。静默 SMTP 必须在 deadline + 裕量内返回并关闭连接；阻塞邮件/Webhook 下断言各自在途不超过 4，溢出丢弃最新且 WARN 可见。
- **外部边界：** 真实 SMTP/收件箱与 Webhook 人工验收此前已被用户免除，至今仍没有真实通过结论；本地假服务不得写成真实链路通过。

### 批次 4：同步健康、轮次汇总与 SSE

#### A3｜中｜HTTP 健康检查不表达同步健康

- **状态与判定：** 产品语义已定，待实施；这不是现有 `/api/health` 的契约错误。
- **当前证据：** `/api/health` 固定表示 HTTP 可用，Docker/Compose 只调用该端点；同步状态目前缺少自动的成功/失败/停滞表达。
- **最终方案：** `/api/health`、Docker HEALTHCHECK 与容器重启语义保持不变；同步健康仅通过向后兼容扩展 `/api/sync/status` 和 Dashboard 提示表达，字段口径与 A18 共用。
- **必须保持：** 暂停、首次启动、无目标或空轮次不能使 HTTP health 返回 503；不设置全局 `ReadTimeout/WriteTimeout`。
- **修改与回归范围：** sync 状态、API、Dashboard 和 README。测试同时断言新同步字段存在且 `/api/health` 不变；前端提示需浏览器验收。

#### A18｜中｜缺少 `last_success` 与整轮成功/失败汇总

- **状态与判定：** 产品语义已定，待实施。退出时无超时等待当前轮是 AGENTS 强要求，不作为本项缺陷。
- **当前证据：** 当前轮结束后无条件刷新 `last_sync`，整轮完成事件主要只有 duration；失败只能从逐域日志/告警侧推断。
- **最终统计合同：** 一个统计单元为“一个 Provider × 一条适用规则”。每轮至少包含 `finished_at/total/ok/failed/skipped/added/deleted/duration/outcome`。DNS/Provider 错误记 failed；成功且无变更记 ok；明确未实施操作记 skipped。只有 `total>0 && failed==0 && skipped==0` 为 success 并更新 `last_success`；failed>0 为 failed；无失败但有 skipped 为 partial；无 Provider/适用规则为 idle；后三者都不更新 `last_success`。
- **兼容合同：** `last_sync` 仍表示最近一轮完成；暂停期不制造轮次或刷新时间；`EventSyncComplete.Data` 保留 duration 并增加同一汇总。本批不增加 SQLite 列。
- **修改与回归范围：** `syncer` 状态/汇总、`GET /api/sync/status`、Dashboard 和事件测试。覆盖全成功、部分失败、仅 skipped、idle、从未成功、暂停；并行计数必须通过 race。
- **依赖与边界：** 依赖 A1/A12；Dashboard 需要浏览器人工验收。

#### A13｜低｜普通轮次重复记录“告警已更新”

- **状态与判定：** 确认存在；属于日志噪声和误导。
- **当前证据：** Run 每轮末尾无条件触发 state-applied hook，启用渠道时 ticker/trigger 也输出与配置变化无关的 INFO，并进入 WebUI 日志流。
- **最终方案：** 不新增可变 generation。Run 保存上次已消费的不可变 `RuntimeState` 指针，只在指针变化且 Run 真正消费状态后触发 hook；普通轮次不触发。启动“已启用”保持独立。
- **必须保持：** hook 仍是“Run 已消费状态”的屏障；日志不得包含 SMTP 密码或 Webhook URL。
- **回归：** 多轮无配置变化时 hook 次数不增长；ApplyState 一次后只增加一次。

#### A15｜低｜SSE 忽略写错误，半开连接缺少单次写出边界

- **状态与判定：** 忽略写错误已确认并隔离复现；Flush 半开阻塞尚未真实网络复现。
- **当前证据：** 两类 SSE 在 `fmt.Fprintf` 后直接 Flush，未处理写返回值；全局 `WriteTimeout` 按长连接契约保持零。
- **最终方案：** 使用可检查错误的写出/刷新路径，写错误立即返回并 unsubscribe。若加入单次写 deadline，必须覆盖初始 Flush、每次写前刷新并在写后清除；优先使用 `http.ResponseController`。
- **必须保持：** 不设置全局 `WriteTimeout`；继续监听 request context 与服务器 shutdown channel；取消订阅不关闭 EventBus channel。
- **修改与回归范围：** 两类 SSE handler 及测试。错误 ResponseWriter 下首个失败即退出；若做 deadline，再验证初始与每次写出。
- **外部边界：** 真实半开 TCP/停止读取客户端仍需人工验证。

### 批次 5：SQLite、损坏数据与错误处理

#### A4｜中高｜SQLite `busy_timeout` 未覆盖每条物理连接

- **状态与判定：** 确认存在，置信度高。
- **当前证据：** `config/store.go` 以普通路径打开数据库，只执行一次 `PRAGMA busy_timeout=5000`；该 PRAGMA 是连接级。隔离复现显示后续连接为 0，竞争时立即 `SQLITE_BUSY`。
- **影响：** 同步日志、扫描结果与协调器事务可并发；非首连接可能立即失败。当前证据不表示数据损坏。
- **最终方案：** 用 `net/url.URL` + `url.Values` 构造 SQLite file URI，以 `_pragma=busy_timeout(5000)` 保证每条连接生效；禁止直接拼接。WAL 继续在打开后执行一次，不让每条连接重复切换。
- **修改与回归范围：** `config/store.go` 与测试。覆盖含空格、`#`、`?`、非 ASCII 的路径；固定 4 条连接逐条断言 5000ms；写锁竞争等待约 5s 而非立即失败。

#### A9｜中｜损坏的 `rules.targets` 被静默扩大为全部目标

- **状态与判定：** 确认存在，需外部改库或损坏触发；后果具有安全方向影响。
- **当前证据：** `loadRules` 解析失败不报错，空 Targets 被解释为全部目标；损坏还会绕过引用检查并在导出/导入中固化为 `[]`。
- **最终四态合同：** 历史空串 `""` 兼容为全部目标；`[]` 是合法全部目标；`null`、对象、标量、非整数数组及解析失败全部返回带规则 ID、但不含原值的内部错误。`null` 不得视为 `[]`。
- **错误边界：** 损坏必须阻断同步、导出、完整快照和目标引用检查；返回安全 500，不静默放行或扩大。若存量库已损坏，升级后 fail-closed 是预期行为，发布说明需给修复提示。
- **修改与回归范围：** `config/store.go` 与 config/syncer 测试。直插所有形态并验证四条调用链。

#### A14｜低｜Rollback、Encode 与日志裁剪错误未完整处理

- **状态与判定：** 确认存在，违反 AGENTS “所有 error 必须处理”；多数触发条件苛刻。
- **当前证据：** `WithTransaction` 忽略 Rollback 且 panic 时无 defer；`writeJSON` 忽略 Encode/Write；`AddSyncLog` 的 COUNT/清理错误可能被忽略。
- **最终方案：** 使用 committed 标志 + defer 回滚，忽略 `sql.ErrTxDone`、记录其他安全错误，panic 时回滚后重新 panic；处理 COUNT/清理错误；响应头已发后只记安全日志，不伪造第二个 HTTP 错误响应。
- **明确不做：** 不因本项引入新框架或强行增加 errcheck 门禁。
- **修改与回归范围：** `config/store.go`、`webui/api/deps.go` 与测试。覆盖 panic 回滚、已结束事务、裁剪查询失败和失败 ResponseWriter。

#### A17｜低｜非预期迁移失败只告警后继续

- **状态与判定：** 确认存在；两条 ALTER 对旧库仍有作用，不能删除为“永久无用”。
- **最终方案：** duplicate-column 继续视为幂等成功；其他 ALTER 错误立即从 `OpenStore` 返回并中止启动。不引入 `schema_migrations` 表。
- **必须保持与回归：** 新库、已迁移库、已有表但缺列的旧库都能正确打开；覆盖 duplicate-column 与可控非预期失败。

### 批次 6：SWAS DROP 真实计数

#### A11｜中低｜SWAS 跳过 DROP 后仍计作新增成功

- **状态与判定：** 确认存在，置信度高。
- **当前证据：** SWAS 对 DROP WARN 后跳过，全部跳过仍返回 nil；`retrySync` 以 `len(diff.ToAdd)` 累加 added。隔离复现为 added=1、实际写入=0；Dry Run 也会列为普通 `to_add`。
- **最终方案：** 统一收敛 `Provider.CreateRules` 返回至少 `{Written, Skipped}`；其他 Provider 成功为 `len(rules),0`，SWAS 混合批次分别计数，全部 DROP 为 `0,N,nil`。added 只累计 Written；事件、SSE 与实时日志增加 skipped 并说明原因。
- **持久化边界：** 本批不增加 `sync_logs.skipped` 列；历史 added 改为真实写入数。未来持久化 skipped 必须另做 Schema 设计。
- **Dry Run 合同：** SWAS DROP 表达为不可实施/跳过，不能继续进入普通 `to_add`。
- **必须保持：** SWAS DROP 继续跳过；幂等语义不变；跳过不重试；不使用 SWAS 专属可选接口。
- **修改与回归范围：** Provider 统一接口、四 Provider、retry、事件/日志、Dry Run 与测试。覆盖全 DROP 与混合场景，验证各层一致。
- **外部边界：** 真实 SWAS 仍需用户观察；mock 不等于真实云通过。

### 批次 7：平台边界与强要求措辞收口

本批同时完成 A8 的强要求措辞收口；A8 的代码状态与完整防回归合同统一见 §四，不在此重复建立第二份问题记录。

#### A19｜低｜按决策完全移除 Windows 兼容

- **状态与判定：** 产品决策已定，待实施；当前 Windows 代码可交叉编译，不是编译缺陷。
- **当前证据：** Windows pidfile、数据目录和 README 仍表达兼容；x/sys/windows 仅由该平台实现直接使用。发布与 CI 面向 Linux/Docker。
- **最终方案：** 删除 Windows pidfile、数据目录分支、README 承诺与仅为其存在的直接依赖；平台文件使用明确 `linux || darwin` 约束，不能简单删除 `!windows`。`go mod tidy` 后若 x/sys 仍被间接依赖则保留 indirect。
- **必须保持：** Linux/macOS 路径、`FWALIZER_DATA_DIR` 优先级、pidfile 单实例、Docker/CI `linux/amd64` 不变；不保留 Windows 门禁。
- **修改与回归范围：** config 平台文件、deployment、README、`go.mod/go.sum` 与 Linux/macOS 构建检查。发布说明需明确不再支持 Windows。

## 四、已关闭问题与防回归合同

#### A5｜已修复｜pause/resume 双重运行时写入

- handler 已只通过协调器更新数据库与运行时，commit 后无二次 Pause/Resume。
- 保留数据库值、RuntimeState、apply 次数与 stub 未被调用的判别性断言。
- 批次 8 可移除 API Syncer 接口中的无生产调用方法，但须先重查调用链；不得自动删除实现、辅助构建或验收代码。

#### A6｜已修复｜暂停后排队 ticker/trigger 仍启动同步

- ticker 与 trigger 消费前检查已发布状态；暂停期不得启动新轮。A20 是独立 Stop 问题。

#### A7｜已修复｜恢复同步可能漏掉立即一轮

- false → true 使用 Run 已处理相位并立即一轮；true → true 只重置 ticker。不得回退到已发布镜像。

#### A8｜已修复｜完整导入未重置 DNS breaker

- 完整导入确定 Reset；普通变更 Preserve 并应用新阈值，且已有端点级判别性回归。批次 7 只把 AGENTS 的“完整导入允许新建并清空”收紧为“确定新建 breaker 并清空计数；普通变更保留既有失败计数”，不得重复修改实现。

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

### 5.2 每批最低自动门禁

```text
go test <受影响包> -race -count=1
go test ./... -race -count=1
go vet ./...
go build ./...
git diff --check
```

涉及前端、依赖、Docker、Compose 或平台构建时，还必须执行对应 Build6 门禁。A1、A2、A4、A9、A11、A12、A13、A15、A16、A17、A20 必须各有判别性用例；仅通用门禁全绿不能关闭。

### 5.3 关闭与文档回写

条目关闭必须同时满足：问题路径消除且不变量未破坏；判别性用例能说明修复前失败、修复后通过；受影响包与全仓门禁通过；要求的人工/外部证据已取得或由用户明确免除；在本条目内追加提交、文件、测试、门禁、外部边界和偏差，再按授权同步其他文档。

真实 SMTP/收件箱与 Webhook 当前属于用户已免除的人工验收，仍不得写成已经通过。已关闭项只有在判别性回归失败或当前调用链重现时才可重开。

## 六、证据边界与规范化记录

### 6.1 已有证据

- 2026-09-27 在 `54efb10` 与 `1fafb17` 上完成全量只读复核；当前 HEAD 相对 `1fafb17` 无源码变化。
- 当时 `go test ./... -race -count=1`（11 包）与 `go vet ./...` 通过；通用门禁不等于覆盖 Issue6。
- `/tmp/fwreview` overlay 隔离复现确认 A1、A2、A4、A9、A11、A12、A13、A15、A16、A17、A20；均使用虚构数据、localhost 假服务和临时数据库。
- Build6 Step 7 已修复 A5、A6、A7、A8、A10，并保留判别性回归。

### 6.2 尚未取得的证据

- 未以 Issue6 修复版本验证真实云弱网/超时、SWAS DROP 计数、真实 SMTP/收件箱或 Webhook。
- 未访问生产 SQLite，也未验证真实旧库的损坏 targets 或迁移失败。
- 未执行 Issue6 新功能的浏览器、Docker、真机、半开 TCP、长跑资源耗尽或远端 CI 验收。
- Windows 仅曾交叉编译通过；用户已决定移除支持，不再要求 Windows 运行验收。

### 6.3 本次规范化内容

- 删除旧 HEAD、旧行号和旧工作树状态与当前合同并列的多重权威结构；历史事实仍可从 Git 追溯。
- 合并原“问题明细、逐项复核、最终修复合同”的重复内容，使问题与方案不再分开记录。
- 保留 A1～A20 编号、所有用户决策、固定数值、不变量、判别性回归、外部边界和关闭项回归要求。
- 清除已裁决的“待决策”、漂移行号、重复命令清单及不会指导实施的过程叙述。

---

**当前结论：** Issue6 已具备构建前使用条件，但待处理项仍未获代码实施授权。下一步应先完成全量复核与方案汇报，待用户最终审核并一次性授权后，再严格按 §2.2 顺序处理。
