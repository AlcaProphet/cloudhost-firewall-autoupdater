# Issue6.md — FWAlizer 后续问题清单（待处理）

> **定位：** 记录 Build6 Step 5 代码审查报告中的后续问题，供稍后逐项处理。本文是问题记录，不代表修复方案已经获批、代码已经修改或外部链路已经验收。`Issue5.md` 现有追踪与 Build6 Step 状态不因本文件改变。
>
> **核验基线：** 2026-09-24，HEAD `559453221ec8984e48a2e16d260f74940ff9f5bb`。核验时工作区已有未提交改动：`.github/workflows/docker-publish.yml`、`AGENTS.md`、`Build6.md`、`Design5.md`、`Issue5.md`、`ProdTestList.md`、`webui/frontend/package.json`、`webui/frontend/package-lock.json`；本次仅新建本文，不触碰上述改动。
>
> **证据边界：** 以下结论以当前仓库及本机现有 Go 标准库、依赖源码的只读检查为主。原报告声称的 `/tmp` 隔离复现与测试结果未在本次重跑；未访问真实云 API、SMTP、Webhook 或用户生产数据库。应将“代码路径可证实”“原报告复现”“生产事故已发生”分开。原报告附件在 §3.1 的 `ErrNoSnapshotLoader` 表格行中途截断，缺失部分未纳入本文。

## 一、优先处理顺序

1. **可能使核心同步停滞或运行时真值分裂：** A1、A2、A5、A7、A4。
2. **可能产生错误配置效果、错误成功信号或破坏性请求：** A9、A10、A11、A12；A6 的剩余调度边界一并处理。
3. **可观测性与契约一致性：** A3、A8、A13、A17、A18。
4. **较低优先级的生命周期与错误处理：** A14、A15、A16；A19 仅待确定支持范围。

上述仅为处理建议，不改变 Build6 已记录的 Step 5、Step 6 验收状态。每项均保持**待处理**，除 A6 中已提交的手动触发守卫外，不把审查结论写成修复完成。

## 二、问题明细

### A1｜高｜阿里云 SDK 请求缺少应用层超时

- **证据：** `provider/ali_swas.go:33-38`、`provider/ali_ecs.go:32-37`、`provider/scan.go:133-138,181-186` 的 `openapi.Config` 均未设置 `ReadTimeout`、`ConnectTimeout`。本机依赖 `darabonba-openapi/v2@v2.2.4` 将缺省值传下去，`tea@v1.5.2` 计算出的 HTTP 客户端与响应头超时为零；`syncer/syncer.go:457` 等待云厂商任务结束，`run.go:185` 无界等待 Syncer。
- **影响：** 云端连接建立后迟迟不返回时，同步轮次和退出收尾可能长期停滞；同类请求还可能挂住连接测试、扫描和 Dry Run。此为代码层可达风险，未在真实云端复现“永久卡死”。
- **处理方向：** 为四处构造点配置有限连接与请求超时；核对 SDK 的时间单位和上下游重试边界，再用可阻塞的本地 HTTP 服务验证超时与停止行为。
- **状态：** 待处理。

### A2｜高｜异步告警无并发上界，SMTP 无 deadline

- **证据：** `notifier/bus.go:108-115` 每个事件、每个接口订阅者启动一个 goroutine；`notifier/email.go:42-49` 使用无应用层 deadline 的 `smtp.SendMail`；DNS 失败在 `syncer/syncer.go:488-492` 逐域名发布。Webhook 客户端另有 10 秒超时。
- **影响：** SMTP 连接半开且 DNS 故障持续时，邮件协程和连接可累积，最终资源耗尽是可信推论，尚非生产实测结论。
- **处理方向：** 为完整 SMTP 会话设置截止时间，并限定异步通知的在途数量或队列容量；明确满载时的丢弃与 WARN 语义。以本地半开 SMTP 服务验证能按时退出且并发有界。
- **状态：** 待处理。

### A3｜中｜HTTP 健康检查不能表示同步健康

- **证据：** `webui/server.go:240-243` 的 `/api/health` 固定返回 `{"status":"ok"}`；Dockerfile/Compose 只请求该端点。`syncer/syncer.go:317-336` 已有 `running`、`enabled`、`last_sync`，前端 Dashboard 展示最近同步时间，但没有自动的过期判定或整轮成功率。
- **边界：** 当前 AGENTS/README 将 Docker HEALTHCHECK 定位为 **HTTP 服务可用性**检查，因此不能把静态响应直接定为违反现行契约。同步停滞未被监控是独立的可观测性缺口。
- **待决策：** 保持 HTTP health 语义，另提供同步健康指标/告警；若拟让 health 因同步过期返回 503，应先确定暂停、首次启动、无目标、部分失败等状态的语义及 Docker 重启影响。
- **状态：** 待决策、待处理。

### A4｜中高｜SQLite `busy_timeout` 未覆盖连接池的每条连接

- **证据：** `config/store.go:64-76` 以普通文件路径打开 SQLite，仅执行一次 `PRAGMA busy_timeout=5000`；未用 `_pragma` DSN，也未限制最大打开连接数。该 PRAGMA 是连接级设置。`config/store.go:654-671` 的日志写入可与配置协调器事务并发。
- **影响：** 新连接可能没有等待时限，并发写锁竞争可直接返回 `SQLITE_BUSY`；原报告的 `/tmp` 三连接实验称已复现，本次未重跑。
- **处理方向：** 选择每连接 DSN PRAGMA 或单连接池策略，并检查读事务、SSE/扫描与写事务是否存在等待关系；用多连接写锁测试验证实际等待时限。
- **状态：** 待处理。

### A5｜中高｜pause/resume 在协调器外再次改写运行时开关

- **证据：** `webui/api/sync.go:46-54,65-73` 先调用 `ConfigCoordinator.Mutate`，返回后再调用 `Syncer.Pause/Resume`；`webui/api/coordinator.go:77-78` 的锁此时已释放；`syncer/syncer.go:235-246` 再以运行时快照做读改写。
- **影响：** 并发 pause/resume 或导入交错时，迟到的第二次运行时写入可覆盖后来提交的真值，使 SQLite `sync_enabled` 与运行时状态分裂。原报告在隔离副本中称已确定性复现；当前代码路径仍保留。
- **处理方向：** 让协调器的提交后发布成为唯一运行时写入口；回归测试使用真实 Store/Syncer，控制交错并比较数据库与运行时最终值。
- **状态：** 待处理。

### A6｜中｜暂停后排队触发的旧缺陷已部分修复，ticker 边界与测试仍待收束

- **已完成事实：** 报告所依据的旧 HEAD `c35eb9d` 的手动 trigger 分支使用无效的 `if !enabled` 守卫；当前 HEAD `5594532` 已提交 `if !s.IsEnabled()`，`syncer/syncer.go:165-175` 对已提交暂停状态重新检查。
- **剩余问题：** `syncer/syncer.go:163-164` 的 ticker 分支未作同样检查；`syncer/state_test.go:107-115` 的测试清理使用无界 `Stop(); Wait()`，若失败后 fake Provider 的新增轮次仍被阻塞，测试可能挂住。现有暂停测试没有确定性覆盖“暂停已提交但 Run 尚未更新本地相位”的交错。
- **处理方向：** 给 ticker 消费前加已发布状态守卫；新增该交错的确定性回归测试，并保证失败后的测试资源能有界释放。ticker 额外轮次目前为代码窗口推断，原报告 12 次探针未复现。
- **状态：** 手动 trigger 守卫已提交；其余待处理。

### A7｜中｜恢复同步时可能漏掉立即一轮

- **证据：** `syncer/syncer.go:93-100` 的 `ApplyState` 在发布时更新 `enabled` 镜像；Run 在 `:150` 以该镜像取得 `wasEnabled`，却在 `:190-203` 以本地 `enabled` 及最新快照判定过渡。镜像可能先于 Run 消费控制通知推进，造成 `false→true` 被判断为 `true→true`。
- **影响：** 恢复后可能只重置 ticker，不立即同步，直到下一个 interval 才运行。原报告在隔离副本中称已用 hook 复现；当前代码未改变相关判定。
- **处理方向：** 以前一次 Run 本地已处理状态作为过渡前值，检查通知合并场景，并补确定性交错测试；与 A6 一起验证调度。
- **状态：** 待处理。

### A8｜中｜完整导入没有重置 DNS 熔断计数

- **证据：** `webui/api/deps.go:86-101` 的所有候选构造固定传 `syncer.BreakerPreserve`；`BreakerReset` 只在 `run.go:84` 的启动路径使用。`syncer/state_test.go` 分别测试两种策略，但没有证明导入走 Reset。Build6 当前记录“完整导入重置计数”，实现并非如此。
- **边界：** AGENTS 使用“完整导入允许新建并清空”的表述，是否必须重置应明确为一项设计决定；不能仅凭 `BreakerReset` 单元测试声称导入语义已落实。
- **处理方向：** 决定导入保留或重置计数；若选择重置，传入明确的策略并增加从导入端点验证熔断状态的测试。
- **状态：** 待决策、待处理。

### A9｜中低｜损坏的 `rules.targets` 可被解释为“适用于全部目标”

- **证据：** `config/store.go:239-244` 仅在 JSON 解析成功时设置 Targets，解析失败无错误；`syncer/syncer.go:550-565` 把空 Targets 视为所有目标；`config/store.go:449-464` 的目标引用检查复用该加载路径。
- **影响与前提：** 需数据库行被外部修改或损坏，正常 API 写入不会制造非法 JSON；一旦发生，规则可能被扩大下发，目标删除 409 也可能漏判。
- **处理方向：** 对非空但非法的 JSON 返回明确错误，阻止加载/同步/导出继续按“全部目标”解释；覆盖损坏行的测试。
- **状态：** 待处理。

### A10｜中低｜配置 reset 接受 JSON `null`

- **证据：** `webui/api/settings.go:173-181` 将请求解码到 `struct{}`；`encoding/json` 对 `null` 解码到非指针结构体不报错，随后执行 `ResetAllTx`。`webui/api/settings_alerts_test.go:266-281` 未覆盖 `null`。
- **影响：** 非前端调用方误把 `null` 当作空参数时，也会执行全量清空；与只接受单一空对象 `{}` 的明确契约冲突。
- **处理方向：** 显式验证顶层 JSON 对象且非 `null`，补 `null`、数组、标量、未知字段的拒绝测试，确保拒绝后数据及运行时不变。
- **状态：** 待处理。

### A11｜中低｜SWAS 跳过 DROP 后仍被计作“新增成功”

- **证据：** `provider/ali_swas.go:118-135` 遇到 DROP 记录 WARN 并跳过，全部跳过也返回 `nil`；`syncer/retry.go:63-77` 对返回 `nil` 的整个 `diff.ToAdd` 计数。README 已说明 SWAS 不支持 DROP。
- **影响：** 日志/同步事件可持续报告新增成功，但相应规则未在云端建立。
- **处理方向：** 明确跨目标规则对 SWAS DROP 的处理语义，并让实际写入数与“跳过”结果可区分；补全跳过与部分跳过测试。
- **状态：** 待处理，规则保存时是否拒绝需决策。

### A12｜中低｜超时错误文案可能绕过重试

- **证据：** `syncer/retry.go:86-101` 直接区分大小写搜索 `timeout` 等字符串；例如 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)` 不含小写 `timeout`，当前函数返回 false。
- **影响：** 部分可恢复的超时在首次失败后直接产生错误告警，未进入约定的最多三次重试。
- **处理方向：** 优先检查可识别的 `context.DeadlineExceeded`、`net.Error.Timeout()`，必要时补规范化字符串判断；测试 SDK/标准库实际错误形态。
- **状态：** 待处理。

### A13｜低｜普通 ticker/trigger 轮次重复记录“告警已更新”

- **证据：** `syncer/syncer.go:214` 在每轮调度循环末尾调用状态应用 hook，`run.go:103` 将其连接到 `AlertManager.LogStatus("已更新")`；ticker、trigger、control 都可到达该位置。
- **影响：** 定期同步时产生与配置更新无关的 INFO 日志。
- **处理方向：** 仅在确实消费并应用新的运行时状态时输出；用事件/日志计数测试区分配置变更与普通轮次。
- **状态：** 待处理。

### A14｜低｜部分错误返回值未处理

- **证据：** `config/store.go:325-335` 的 `WithTransaction` 忽略 `Rollback` 错误，panic 时没有 defer 回滚；`webui/api/deps.go:196-200` 忽略 `Encode` 错误。
- **影响：** 违反 AGENTS 的 error 处理要求；回滚失败与响应写入失败不可见。不能据当前证据断言“无资源泄漏后果”。
- **处理方向：** 复用协调器的有条件 defer 回滚模式，记录非 `sql.ErrTxDone` 错误；处理响应编码/写入错误。
- **状态：** 待处理。

### A15｜低｜两类 SSE 忽略写入错误，半开连接缺少写出边界

- **证据：** `webui/api/logstream.go:171-172`、`webui/api/sync.go:127-128` 忽略 `fmt.Fprintf` 的错误并执行 `Flush`；全局 `WriteTimeout` 按 SSE 契约保持零值。
- **影响：** 可检测到的写入失败没有及时退出；半开连接若阻塞于写出/Flush，handler、订阅和缓冲会被占用。实际持续时间依赖网络栈，尚未实测。
- **处理方向：** 先处理写错误；若要约束半开连接，还需评估每次 SSE 写出的 deadline 或其他可取消的写出机制。**只检查 `Fprintf` 返回值不能解决已阻塞在 `Flush` 的情况。**
- **状态：** 待处理。

### A16｜低｜生命周期方法重复调用语义不完整

- **证据：** `webui/server.go:133-168` 的 `Start` 在 `serveOnce.Do` 前每次创建 listener，第二次调用可遗留未服务的 listener；`:182-184` 的 `Wait` 从只接收一次的通道读取，第二次会等待。`syncer/syncer.go:119-120` 的 `Run` 也未防止重复关闭 `doneCh`。
- **边界：** 当前生产接线只调用一次，属于潜在 API 契约问题，不是当前已触发的故障。
- **处理方向：** 确定这些方法是只可调用一次还是应幂等，并用注释/测试或实现固定语义。
- **状态：** 待处理。

### A17｜低｜迁移失败只告警后继续

- **证据：** `config/store.go:153-159` 对非 `duplicate column` 的 `ALTER TABLE` 错误仅 WARN，随后返回成功。
- **影响：** 迁移失败可能延迟到后续读取才表现为启动错误，根因不够直接。
- **边界：** 两条 ALTER 对**已有但缺列的旧数据库**仍有实际作用；仅在新建库或已迁移库中通常命中“列已存在”，不能按原报告称其“永久无用”。
- **处理方向：** 非预期迁移错误直接返回，并保留旧库迁移验证；是否重构迁移逻辑另作评估。
- **状态：** 待处理。

### A18｜低｜无界等待放大外部挂起，整轮完成缺少成功失败汇总

- **证据：** `run.go:184-185` 按强要求无超时等待当前同步轮次；`syncer/syncer.go:459-470` 在轮次结束时无条件刷新 `lastSync`，完成事件只带耗时，不带失败/新增/删除汇总。
- **边界：** “完成当前轮次再退出”是当前明确契约，不能单独把无界 `Wait()` 当作实现错误；A1 的无超时请求会把它放大为严重停滞。`last_sync` 当前更接近“最近执行完毕”，不等于“最近成功”。
- **处理方向：** 优先解决 A1；另讨论是否增加整轮成功率或 `last_success`，避免直接改变现有 `last_sync` 语义。
- **状态：** 待决策、待处理。

### A19｜低｜Windows 支持范围与退出说明待澄清

- **证据：** README 列出 Windows 数据目录；`run.go:117` 监听 SIGTERM/SIGINT，仓库有 `config/pidfile_windows.go`。现有证据不能仅凭 Windows 不投递 SIGTERM 推出“Windows 无法优雅退出”，也不能由目录表推定承诺 Windows 服务管理。
- **处理方向：** 明确 Windows 是受支持运行平台、尽力兼容，还是仅保留路径代码；据此更新说明或补平台验证。不在未决策前删除平台代码及依赖。
- **状态：** 待澄清；暂不列为已证实的运行缺陷。

## 三、文档与清理候选

- **审查快照过时：** 原报告开始时 HEAD 为 `c35eb9d`，A6 的工作区守卫后来提交为当前 HEAD `5594532`。本次核验时 `Build6.md` 等进度文档已有他人未提交改动；这些改动正在修正部分“Step 5 尚未提交”的旧口径。待相关工作完成后，再核对最终提交状态、Step 5/6 的证据边界与远端 CI 结果，勿根据原报告旧快照覆盖当前进度。
- **旧形态与零引用候选：** `Store.LoadConfig` / `BusinessSnapshot.ToConfig` 当前只见测试调用；`Store.DeleteRule`、`UpdateTarget`、`UpdateRule`、`GetAlertEmailConfigTx`、`GetAlertWebhookConfigTx`、`SettingsKeysV2`、`EventRuleChanged`、`SyncDomainResult`、`ResolvedIPs` 等可在独立清理时逐项检查。`presenceSlice.MarshalJSON` 与 `toInt` 的 `float64` 分支也值得复查。仅凭仓库内部无调用不能证明导出 API 没有外部使用者；`ErrNoSnapshotLoader` 分支具有接线错误防御语义，不能简单归为零引用死代码。
- **注释/忽略规则残留：** `config/validate.go`、`config/runtime.go`、`syncer/retry.go` 有过渡期措辞；`.dockerignore` 中 `.env` 和根目录 `Dockerfile` 条目与当前仓库布局不完全一致。均为低优先级清理候选，不影响上述问题排序。

## 四、后续处理记录约定

处理任一条目时，先重新核对当时 HEAD、工作区、现行 AGENTS/Build/Issue 状态与实际调用链；修复后在该条目追加实际改动、针对性回归和证据边界。源码检查、本地自动测试、浏览器、Docker、真实云 API、SMTP、Webhook 和远端 GitHub Actions 结果分别记录，不互相替代。
