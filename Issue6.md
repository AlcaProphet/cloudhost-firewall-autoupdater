# Issue6.md — FWAlizer 后续问题清单（待处理）

> **定位：** 记录 Build6 Step 5 代码审查报告中的后续问题，供稍后逐项处理。本文是问题记录，不代表修复方案已经获批、代码已经修改或外部链路已经验收。`Issue5.md` 现有追踪与 Build6 Step 状态不因本文件改变。
>
> **核验基线：** 2026-09-24，HEAD `559453221ec8984e48a2e16d260f74940ff9f5bb`。核验时工作区已有未提交改动：`.github/workflows/docker-publish.yml`、`AGENTS.md`、`Build6.md`、`Design5.md`、`Issue5.md`、`ProdTestList.md`、`webui/frontend/package.json`、`webui/frontend/package-lock.json`；本次仅新建本文，不触碰上述改动。
>
> **后续进展（2026-09-27）：** 本文件的核验基线为 `5594532`，属历史快照；A5、A6、A7、A8、A10 已在 Build6 Step 7 内按用户确认的最小边界修复（详见各项「实施记录」），其余条目状态未变。
>
> **2026-09-27 只读复核（新增 §五）：** 复核基线为 HEAD `54efb10e2ae1ad5fae1d6d3f13eef87a412eb7c1`（`main`，相对 `origin/main` **ahead 1**，工作树干净）。A1～A19 已逐项重新核验并给出判定、现行行号证据与修复方案；另新增 **A20（Stop 后仍可能启动一整轮同步，已隔离复现）**。本轮 7 项产品语义决策已由用户确认（见 §5.7）。§二 中 A5～A10 的行号与部分结论属历史快照，现行行号与修正清单见 §5.5。
>
> **证据边界：** 以下结论以当前仓库及本机现有 Go 标准库、依赖源码的只读检查为主。原报告声称的 `/tmp` 隔离复现与测试结果未在本次重跑；未访问真实云 API、SMTP、Webhook 或用户生产数据库。应将“代码路径可证实”“原报告复现”“生产事故已发生”分开。原报告附件在 §3.1 的 `ErrNoSnapshotLoader` 表格行中途截断，缺失部分未纳入本文。

## 一、优先处理顺序

1. **可能使核心同步停滞或运行时真值分裂：** A1、A2、A5、A7、A4。
2. **可能产生错误配置效果、错误成功信号或破坏性请求：** A9、A10、A11、A12；A6 的剩余调度边界一并处理。
3. **可观测性与契约一致性：** A3、A8、A13、A17、A18。
4. **较低优先级的生命周期与错误处理：** A14、A15、A16；A19 仅待确定支持范围。

上述仅为处理建议，不改变 Build6 已记录的 Step 5、Step 6 验收状态。**2026-09-27 更新：** 经用户逐项确认，A5、A6、A7、A8、A10 已在 Build6 Step 7 内做最小修复并附判别性回归（见各项「实施记录」）；其余条目（A1、A2、A3、A4、A9、A11～A19）仍**待处理/待决策**，不得写成已修复。

**2026-09-27 复核更新（详见 §五）：** 在 HEAD `54efb10` 上重新核验的判定为——**确认存在 11 项**（A1、A2、A4、A9、A11、A12、A13、A14、A15、A16、A17）、**已修复 5 项**（A5、A6 原描述、A7、A8、A10）、**设计/产品决策 3 项**（A3、A18、A19，均已由用户裁决）、**部分存在 0 项、未能证实 0 项、原结论不成立 0 项**；另新增 **A20（`Stop` 之后仍可能因排队 trigger 启动一整轮同步，已隔离复现 5/15）**，经用户确认并入 A6/A16/A18 同批修复。优先级建议：A1 保持第 1，A6-Stop/A9/A12/A18 上调，A5/A7/A8/A10 关闭。

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
- **实施记录（2026-09-27，Build6 Step 7，经用户确认）：** `webui/api/sync.go` 删除 commit 之后额外的 `d.Syncer.Pause()` / `d.Syncer.Resume()`，协调器成为唯一运行时写入口；`TestHandleSyncPauseResume` 重写为判别性用例（断言 DB、已发布 `RuntimeState`、apply 次数，且 stub 的 `paused/resumed` 必须为 false），修复前该用例真实失败、修复后通过。`Syncer.Pause/Resume` 方法与其 syncer 内部测试保留，生产 handler 不再调用。
- **状态：** ✅ 已修复（2026-09-27，Build6 Step 7）；证据为源码 + webui/api 专项与整包 `-race` 测试。

### A6｜中｜暂停后排队触发的旧缺陷已部分修复，ticker 边界与测试仍待收束

- **已完成事实：** 报告所依据的旧 HEAD `c35eb9d` 的手动 trigger 分支使用无效的 `if !enabled` 守卫；当前 HEAD `5594532` 已提交 `if !s.IsEnabled()`，`syncer/syncer.go:165-175` 对已提交暂停状态重新检查。
- **剩余问题：** `syncer/syncer.go:163-164` 的 ticker 分支未作同样检查；`syncer/state_test.go:107-115` 的测试清理使用无界 `Stop(); Wait()`，若失败后 fake Provider 的新增轮次仍被阻塞，测试可能挂住。现有暂停测试没有确定性覆盖“暂停已提交但 Run 尚未更新本地相位”的交错。
- **处理方向：** 给 ticker 消费前加已发布状态守卫；新增该交错的确定性回归测试，并保证失败后的测试资源能有界释放。ticker 额外轮次目前为代码窗口推断，原报告 12 次探针未复现。
- **实施记录（2026-09-27，Build6 Step 7，经用户确认）：** ticker 分支补 `if !s.IsEnabled() { slog.Debug(...); break }`，与 trigger 分支共用已发布状态守卫（AGENTS §五）。`TestPausedPublishedStateDropsTickerRound` 用「直接发布已暂停状态、不投递控制通知」精确复现消费前窗口：移除守卫时该用例真实失败（`GetRules = 2, want 1`），恢复守卫后通过。测试清理改为有界 `stopRunBounded`（10s 上限）。原报告所述 ticker 额外轮次在修复前无法用确定性探针复现，本轮改用上述发布窗口模型后已可确定性覆盖。
- **状态：** ✅ 已修复（2026-09-27，Build6 Step 7）；手动 trigger 守卫（更早提交）与 ticker 守卫均已落地并有回归用例。

### A7｜中｜恢复同步时可能漏掉立即一轮

- **证据：** `syncer/syncer.go:93-100` 的 `ApplyState` 在发布时更新 `enabled` 镜像；Run 在 `:150` 以该镜像取得 `wasEnabled`，却在 `:190-203` 以本地 `enabled` 及最新快照判定过渡。镜像可能先于 Run 消费控制通知推进，造成 `false→true` 被判断为 `true→true`。
- **影响：** 恢复后可能只重置 ticker，不立即同步，直到下一个 interval 才运行。原报告在隔离副本中称已用 hook 复现；当前代码未改变相关判定。
- **处理方向：** 以前一次 Run 本地已处理状态作为过渡前值，检查通知合并场景，并补确定性交错测试；与 A6 一起验证调度。
- **实施记录（2026-09-27，Build6 Step 7，经用户确认）：** Run 的过渡前值由已发布镜像 `s.isEnabledMirror()` 改为本循环已处理相位 `enabled`。`TestResumeImmediateRoundAfterPhaseMirrorAdvance` 用 `SetStateAppliedHook` 屏障在「Run 刚完成一次过渡」与「下一次读取过渡前值」之间注入已提交的暂停+恢复：修复前真实失败（`GetRules 调用次数 = 1, want >= 2`），修复后通过。`isEnabledMirror` 仅保留为运行时状态尚未发布时的 `IsEnabled` 回退，注释已同步。
- **状态：** ✅ 已修复（2026-09-27，Build6 Step 7）；含确定性交错回归用例与调度用例 100 轮 race。

### A8｜中｜完整导入没有重置 DNS 熔断计数

- **证据：** `webui/api/deps.go:86-101` 的所有候选构造固定传 `syncer.BreakerPreserve`；`BreakerReset` 只在 `run.go:84` 的启动路径使用。`syncer/state_test.go` 分别测试两种策略，但没有证明导入走 Reset。Build6 当前记录“完整导入重置计数”，实现并非如此。
- **边界：** AGENTS 使用“完整导入允许新建并清空”的表述，是否必须重置应明确为一项设计决定；不能仅凭 `BreakerReset` 单元测试声称导入语义已落实。
- **处理方向：** 决定导入保留或重置计数；若选择重置，传入明确的策略并增加从导入端点验证熔断状态的测试。
- **实施记录（2026-09-27，Build6 Step 7，用户决策：完整导入重置）：** `webui/api/coordinator.go` 的 `buildCandidate` 增加 `syncer.BreakerPolicy` 形参，`Mutate` 固定 `BreakerPreserve`、新增 `MutateImport` 固定 `BreakerReset`；`webui/api/deps.go` 透传策略，`webui/api/export.go` 导入 handler 改调 `MutateImport`；Build6 §12.3 第 7 条与本节语义由「允许」明确为「确定重置」。`TestConfigImportResetsDNSBreakerOrdinaryChangePreserves` 以「阈值 2 + 2 次失败已熔断」为夹具做判别：临时改回 `BreakerPreserve` 时真实失败，恢复后通过。
- **状态：** ✅ 已修复（2026-09-27，Build6 Step 7）；导入重置、普通变更保留，均有端点级断言。

### A9｜中低｜损坏的 `rules.targets` 可被解释为“适用于全部目标”

- **证据：** `config/store.go:239-244` 仅在 JSON 解析成功时设置 Targets，解析失败无错误；`syncer/syncer.go:550-565` 把空 Targets 视为所有目标；`config/store.go:449-464` 的目标引用检查复用该加载路径。
- **影响与前提：** 需数据库行被外部修改或损坏，正常 API 写入不会制造非法 JSON；一旦发生，规则可能被扩大下发，目标删除 409 也可能漏判。
- **处理方向：** 对非空但非法的 JSON 返回明确错误，阻止加载/同步/导出继续按“全部目标”解释；覆盖损坏行的测试。
- **状态：** 待处理。

### A10｜中低｜配置 reset 接受 JSON `null`

- **证据：** `webui/api/settings.go:173-181` 将请求解码到 `struct{}`；`encoding/json` 对 `null` 解码到非指针结构体不报错，随后执行 `ResetAllTx`。`webui/api/settings_alerts_test.go:266-281` 未覆盖 `null`。
- **影响：** 非前端调用方误把 `null` 当作空参数时，也会执行全量清空；与只接受单一空对象 `{}` 的明确契约冲突。
- **处理方向：** 显式验证顶层 JSON 对象且非 `null`，补 `null`、数组、标量、未知字段的拒绝测试，确保拒绝后数据及运行时不变。
- **实施记录（2026-09-27，Build6 Step 7，经用户确认）：** `webui/api/decode.go` 新增 `decodeJSONObjectStrict`（顶层必须是 JSON 对象，显式拒绝 `null`），`handleConfigReset` 改用它，其余严格解码口径不变。`TestConfigResetStrictBody` 拒绝列表扩为 `null`、`  null  `、`[]`、`[{}]`、`1`、`"str"`、`true` 与原有未知字段/空 body，并断言 400 + 数据保留 + apply==0；临时改回 `decodeJSONStrict` 时 `null` 会返回 200 并清空数据（真实失败），恢复后通过。
- **状态：** ✅ 已修复（2026-09-27，Build6 Step 7）；含判别性回归用例。

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

---

## 五、2026-09-27 只读复核报告：A1～A19 逐项判定与修复方案（含新增 A20）

> 本节由 2026-09-27 的只读复核形成，应用户要求写入本文件。复核**未修改任何源码/测试/依赖/配置/其他文档**，唯一写入对象是本文件。当时工作树在写入前是干净的。
>
> **本节性质：** 判定与方案，不是实施记录。任何条目在真正修复前，其「状态」仍是待处理/待决策；不得把本节内容写成「已修复」或「已验收」。

### 5.0 复核基线与方法

**基线（开始时与结束时各检查一次，结果一致）**

| 项 | 复核结果 |
|---|---|
| 分支 | `main`（跟踪 `origin/main`） |
| 完整 HEAD | `54efb10e2ae1ad5fae1d6d3f13eef87a412eb7c1` |
| 与远端关系 | `git rev-list --left-right --count origin/main...HEAD` = `0 1`，即 **ahead 1**（`origin/main` = `3db45fe`） |
| 工作树 | 干净：`git diff --name-only` 空、`git diff --check` 无输出；无未提交改动；仅 3 个既有被忽略产物（`fwalizer`、`webui/frontend/dist/`、`node_modules/`），其中 `fwalizer` mtime 未被本次复核覆盖 |
| 与本文件原始基线的关系 | `5594532..HEAD` 之间共 **8 个提交**（含 Step 6 前端依赖升级、Step 7 补测与验收、Issue6 新建/更新、v2.0.0 tag 结果回写） |
| 与题干旧快照的差异 | 题干所述「HEAD 曾为 `69fb7c88…`、main 曾 ahead 3」在本次复核时**均不成立**，以本表为准 |

**实际阅读范围**：`AGENTS.md` 全文；`Issue6.md`、`Build6.md`（§12.3 第 7 条、§12.5、§12.9、§12.14、Step 7 记录与变更记录）、`Issue5.md`、`Design5.md`、`ProdTestList.md`、`README.md` 相关节；`PlatformAPIDocs/` 中与 SWAS DROP、ECS 安全组相关的 API 要求；`run.go`、`main.go`、`syncer/*`、`notifier/*`、`config/*`、`webui/server.go`、`webui/api/*`、`provider/*`、`dns/*`、`app/logutil.go`、`build/Dockerfile`、`docker-compose.yml.example`、`.github/workflows/docker-publish.yml`、`.dockerignore`、`Makefile`，以及全部 37 个 `_test.go` 的用例清单与关键用例实现。

**依赖行为核对（本机 module cache 只读）**：`darabonba-openapi/v2@v2.2.4`、`tea@v1.5.2`、`tea-utils/v2@v2.0.9`、`swas-open-20200601/v3@v3.0.0`、`ecs-20140526/v7@v7.9.2`、`tencentcloud-sdk-go/common@v1.3.142`、`modernc.org/sqlite@v1.54.0`；标准库 `net/smtp`、`net/http`、`net/textproto`、`net/url`、`net`、`database/sql`。本机工具链为 **go1.26.6**（`go.mod` 声明 `go 1.25.0`），GOROOT = `/opt/homebrew/Cellar/go/1.26.6/libexec`。

**证据分层（沿用 §四约定，不得互相替代）**：源码路径可达 ≠ 静态推理 ≠ 已有自动测试 ≠ 本轮隔离复现 ≠ 真实云 API ≠ 真实 SMTP/收件箱 ≠ 真实 Webhook ≠ 浏览器/真机 ≠ 生产事故。本节所有「动态复现」均为 **`/tmp/fwreview/` 下的 `go test -overlay` 隔离复现**（虚构数据 + localhost 假服务 + `t.TempDir()` 临时数据库），**未**访问真实云、SMTP、Webhook、生产数据库，也**未**做长跑资源耗尽实验。

### 5.1 判定总表

| 编号 | 判定 | 优先级复核 | 置信度 | 现行核心证据（文件:行） | 需代码修复 | 需用户决策 | 批次 |
|---|---|---|---|---|---|---|---|
| A1 | 确认存在 | 保持「高」，建议列为第 1 | 高 | `provider/ali_swas.go:33-37`、`ali_ecs.go:32-36`、`scan.go:133-137`、`scan.go:181-185`；SDK `dara/core.go:349-357/533-535/734-737`、`:332` | 是 | 否 | 批次 1 |
| A2 | 确认存在 | 保持「高」 | 高 | `notifier/bus.go:108-115`、`notifier/email.go:49`、`notifier/webhook.go:26`、`syncer/syncer.go:493-497`、`:529-533` | 是 | 是（满载语义，批次 2 前确认） | 批次 2 |
| A3 | 设计/产品决策（已决：保持 HTTP-only + 另加同步健康可观测） | 保持「中」 | 高 | `webui/server.go:240-243`、`syncer/syncer.go:325-342`、README:292/465-467、AGENTS §八、`build/Dockerfile` | 是（新增可观测，不改 health） | 已决 | 批次 4 |
| A4 | 确认存在 | 保持「中高」 | 高 | `config/store.go:63-85`（`sql.Open` 于 `:64`、`busy_timeout` 于 `:74`）；驱动 `driver.go:50-54`、`sqlite.go:207-237`、`conn.go:56,135-138`、`lib/sqlite.go:10339-10350` | 是 | 否 | 批次 5 |
| A5 | 已修复 | 降为已关闭（残留接口清理为低） | 高 | `webui/api/sync.go:41-73`；`coordinator.go:90-144`；`deps.go:110-142`；`webui/api/sync_test.go:107` | 否（可选清理） | 否 | 批次 8 |
| A6 | 原描述已修复；相邻新缺陷确认存在（见 A20） | 原「中」关闭；A20 升为「中」 | 高 | `syncer/syncer.go:159-168`（ticker 守卫）、`:169-179`（trigger 守卫）、`state_test.go:111-124`（有界清理）；A20 见 `syncer.go:157-184`、`:307-309` | 是（A20） | 已决（并入同批） | 批次 3 |
| A7 | 已修复 | 已关闭 | 高 | `syncer/syncer.go:146`、`:194`、`:196-215`、`:274-283`；`state_test.go:348` | 否 | 否 | — |
| A8 | 已修复 | 已关闭（AGENTS 措辞待改，低） | 高 | `coordinator.go:72-80`、`export.go:114-116`、`deps.go:88-104`、`state.go:74-83`；`import_runtime_test.go:195` | 否 | 已决（AGENTS 改「确定重置」） | 批次 7 |
| A9 | 确认存在 | 建议「中低」→「中」 | 高 | `config/store.go:239-244`、`:449-464`、`syncer/syncer.go:555-570`、`webui/api/bundle_v2.go:283-298`、`:456-469` | 是 | 已决（非法即报错） | 批次 5 |
| A10 | 已修复 | 已关闭 | 高 | `webui/api/decode.go:76-92`、`settings.go:173-190`；`settings_alerts_test.go:267` | 否 | 否 | — |
| A11 | 确认存在 | 保持「中低」 | 高 | `provider/ali_swas.go:111-148`（跳过段 `:117-135`）、`syncer/retry.go:64-78`、`syncer.go:538-542`、`logwriter.go:52-60`、`config/validate.go:39` | 是 | 已决（返回实际写入/跳过数） | 批次 6 |
| A12 | 确认存在 | 建议「中低」→「中」 | 高 | `syncer/retry.go:86-101`、`:14`、`:26`；标准库 `net/http/client.go:737`、`net/http/transport.go:2772-2775` | 是 | 否 | 批次 1 |
| A13 | 确认存在 | 保持「低」 | 高 | `syncer/syncer.go:217-218`、`:253-261`、`run.go:103`、`webui/api/alertset.go:113-121` | 是 | 否 | 批次 4 |
| A14 | 确认存在 | 保持「低」（违反强要求，建议一并修） | 高 | `config/store.go:325-335`、`:664-670`；`webui/api/deps.go:197-202` | 是 | 否 | 批次 5 |
| A15 | 确认存在 | 保持「低」（写错误建议随批修） | 高 | `webui/api/sync.go:122-127`、`logstream.go:167-172`、`webui/server.go:151-157`；标准库 `responsecontroller.go:105-117`、`server.go:503-505`、`:3002-3007` | 是 | 否 | 批次 4/5 |
| A16 | 确认存在 | 保持「低」（生产单次调用） | 高 | `webui/server.go:133-176`、`:182-184`、`:210-235`；`syncer/syncer.go:114-115`、`:307-309` | 是 | 已决（A20 并入同批） | 批次 3 |
| A17 | 确认存在 | 保持「低」 | 中 | `config/store.go:92-161`（迁移 `:154-159`、`return nil` 于 `:160`） | 是 | 否 | 批次 5 |
| A18 | 设计/产品决策（已决：新增 `last_success` + 整轮汇总，不改 `last_sync`） | 建议「低」→「中」 | 高 | `run.go:177-185`、`syncer/syncer.go:462-473`、`:325-342`、`run.go:110-113` | 是 | 已决 | 批次 4 |
| A19 | 设计/产品决策（已决：**完全移除 Windows 兼容**） | 保持「低」（需同步 go.mod/README） | 高 | `config/pidfile_windows.go`、`config/pidfile_unix.go`、`config/deployment.go:66-77`、`README.md:139`、`run.go:116-117`、`go.mod:12` | 是（按决策移除） | 已决 | 批次 7 |
| **A20** | **确认存在（本轮新增）** | **「中」** | 高 | `syncer/syncer.go:157-184`（select 同时含 `stopCh` 与 `triggerCh`/`ticker.C`）、`:307-309`、`run.go:175`、`:185` | 是 | 已决（并入 A6/A16/A18） | 批次 3 |

### 5.2 逐项记录

每项按统一结构记录：判定 / 置信度 / 现行源码证据 / 完整调用链 / 生产可达性 / 触发条件 / 实际影响 / 已有缓解 / 现有测试证明了什么 / 现有测试没有证明什么 / 本轮动态复现 / 与 §二 原描述的差异 / 修复方案（推荐、备选、不变量、范围、回归测试、外部验收、风险与依赖）/ 待用户决策。

#### 5.2.1 A1｜阿里云 SDK 请求缺少应用层超时

- **判定：** 确认存在（4 处生产构造点全部没有任何有限 deadline）。
- **置信度：** 高。缺的证据仅限「真实阿里云网络环境下的长时间挂起观测」（本地阻塞端点已复现 ≥5s 不返回）。
- **现行源码证据：** `provider/ali_swas.go:32-39`、`provider/ali_ecs.go:31-38`、`provider/scan.go:132-139`（SWAS 扫描）、`provider/scan.go:180-187`（ECS 扫描）——`openapi.Config` 只设置 `AccessKeyId/AccessKeySecret/Endpoint`。SDK 侧：`darabonba-openapi/v2@v2.2.4/client/client.go:810-811` 把 runtime 的 connect/read timeout 传入；`tea@v1.5.2/dara/core.go:349-357` 令 `httpClient.Timeout=(ConnectTimeout+ReadTimeout)ms`（零值=0=无超时）；`dara/core.go:533-535` `trans.ResponseHeaderTimeout=ReadTimeout ms`；`dara/core.go:734-737` `net.Dialer{Timeout: ConnectTimeout ms}`；传输层为 `new(http.Transport)`，**从不设置 `TLSHandshakeTimeout`**；`dara/core.go:332` 用 `http.NewRequest`（**无 context**；SWAS v3.0.0 客户端包完全没有 `context.Context`）；`dara/retry.go:274-281` 未配置 `RetryOptions` 时 SDK 只尝试 1 次。对照：腾讯 `profile/http_profile.go:45` 默认 `ReqTimeout: 60` + `common/client.go:662` 写入 `httpClient.Timeout`，因此腾讯路径每次请求 60s 有界。
- **完整调用链：** `run.go:96 syncer.New` ← `syncer.BuildRuntimeState`（`state.go:64-72`）→ 同步 `Run → syncAll(:430) → syncDomain(:482) → retrySync(retry.go:22) → Provider.GetRules/CreateRules/DeleteRules`；连接测试 `webui/api/targets.go:142-190`；资源扫描 `webui/api/scan.go:18-76 → provider.ScanResources`；Dry Run `syncer.go:391-422`。四条入口共用同一批阿里云 client。
- **生产可达性：** 生产可达（正式同步、连接测试、扫描、Dry Run 均真实接线）。
- **触发条件：** 阿里云侧 TCP 建连后迟迟不返回（TLS 无响应、响应头无响应、网络黑洞、SLB 挂起等）。
- **实际影响：** 该轮 `syncAll` 永不返回 → `run.go:185 s.Wait()` 永不返回 → SIGTERM 后进程无法退出；三个 WebUI 请求（`/api/test-connection`、`/api/scan-resources`、`/api/sync/dryrun`）永久挂起（`ReadHeaderTimeout/IdleTimeout` 不限制 handler 时长）。
- **已有缓解：** 无（DNS 有 10s 超时；腾讯 SDK 自带 60s，仅覆盖腾讯）。
- **现有测试证明了什么：** `provider/request_mock_test.go` 用本地 mock 云端点验证四类 Provider 的请求构造（字段、协议、IPv6、删除标识、分批、SWAS 跳过），即协议正确性。
- **现有测试没有证明什么：** 所有 mock 都立即响应，没有任何「连接成功但不回包」用例；`go test -race` 全绿与本项无关。
- **本轮动态复现：** 是（`/tmp/fwreview/provider_repro_test.go`，`-overlay` 注入 `provider` 包）：与生产同形的 `openapi.Config` 指向 127.0.0.1 上「接受连接但永不响应」的监听器 → `调用在 5s 内未返回：openapi.Config 未设置 ConnectTimeout/ReadTimeout 时无任何有限请求超时`。未做真实云验证。
- **与 §二 原描述的差异：** 方向正确、行号仅右界差 1（原 `33-38/32-37/133-138/181-186`）。需补三项关键事实：TLS 握手超时同样为 0；请求完全没有 context；SDK 自带重试默认仅 1 次（应用层 `retrySync` 是唯一重试来源，与 A12 强相关）。
- **修复方案（推荐）：** 在 4 处构造点统一设置 `ConnectTimeout` 与 `ReadTimeout`（SDK 单位**毫秒**、类型 `*int`）。建议集中在 `provider` 包内定义常量，默认 `ConnectTimeout=10_000`、`ReadTimeout=30_000`（比腾讯 60s 更紧）。ECS v7 可用 `WithContext`+`context.WithTimeout` 表达「单次请求总上限」，但 **SWAS v3.0.0 无 ctx API**，因此推荐只用 SDK 两个超时字段，保持 `Provider` 接口不引入 context 参数。
- **备选方案：** (a) 只设 `ReadTimeout`（更简单，但慢建连仍长期占 goroutine）；(b) 在 `retrySync` 外层用 goroutine+select 包裹（覆盖所有 Provider，但产生不可取消的泄漏 goroutine，不推荐）；(c) 新增业务设置项 `cloud_timeout`（可运维但扩大配置面，无诉求则不做）。
- **必须保持的不变量：** Provider 接口与「一轮一快照」不变；重试仍走完整 Describe→Diff→Create/Delete；不使用全量覆盖 API；腾讯 60s 行为不改；超时只作用于单次 SDK 调用，不改变 `Stop` 的「完成当前轮次」语义。
- **建议修改范围：** 生产 `provider/{ali_swas,ali_ecs,scan}.go`（4 处 + 可选常量）；测试 `provider`；API/SQLite/前端/Docker 无变化；README 可选补一句。
- **建议回归测试：** `provider/timeout_test.go`：本地阻塞 listener，对 4 条构造路径断言「在 ReadTimeout+裕量内返回错误」，且错误可被 A12 的新分类识别；`-race`。
- **外部验收需求：** 真实阿里云在弱网/黑洞下的超时行为需用户真机验证。
- **风险与依赖：** 超时过短会把慢但正常的 API 判为失败并触发重试与告警（30s + 3 次重试可缓解）；**与 A12 强绑定**：A1 修好后超时会成为常态错误，若 A12 未修则不会被重试。建议同批且 A1 先行。
- **待用户决策：** 无。

#### 5.2.2 A2｜异步告警无并发上界，SMTP 无 deadline

- **判定：** 确认存在。三个命题分开：SMTP 无 deadline=**确认**；无并发上限=**确认**；已发生资源耗尽=**未证实**（可信推论）。
- **置信度：** 高（代码 + 标准库行为 + 隔离复现）。
- **现行源码证据：** `notifier/bus.go:96-115`（`Publish` 对每个接口订阅者 `go func(...)`，无 WaitGroup/信号量/队列/超时）；`notifier/email.go:42-49`（直接 `smtp.SendMail`，无 context/无 deadline）；`notifier/webhook.go:19-28`（`&http.Client{Timeout: 10*time.Second}`）；发布点 `syncer/syncer.go:493-497`（DNS 失败，逐域名逐轮）、`:529-533`（域同步失败）。标准库：`net/smtp/smtp.go:321-376` 全程无 `SetDeadline`；`:53-60` `Dial` 用零值 `net.Dialer`；包内不 import `time`；`:414-422` `Quit()` 在服务端不回 221 时永久阻塞，且 `defer c.Close()`（`:334`）在 `SendMail` 返回前不执行。生产订阅者静态计数：`EventSyncError` ≤3（StoreLogWriter+邮件+Webhook）、`EventDNSFailed` ≤2、`EventDomainSyncComplete` =1。
- **完整调用链：** `run.go:96-113`（唯一 EventBus + AlertManager + StoreLogWriter）→ 每域发布事件 → `Publish` 立即返回（`:462 wg.Wait()` **不等待**告警发送）→ 告警 goroutine 独立执行 SMTP/HTTP。
- **生产可达性：** 生产可达；需「启用告警 + 大量失败 + SMTP 半开」同时成立才积累。
- **触发条件：** SMTP 半开；或 DNS 长时间故障使每轮每域都发布 `EventDNSFailed`（interval 越短发布越频繁）。
- **实际影响：** 在途 goroutine 无上限地随时间增长，每个持有未设 deadline 的 socket；告警可能静默丢失（无队列/无重试/无 WARN 计数）；进程退出不等待在途告警（`run.go` 只 `s.Wait()`），因此「句柄/内存耗尽」需要长 uptime 才可能发生。
- **已有缓解：** Webhook 10s 客户端超时；SSE channel 容量 32 满则丢弃（与告警无关）；告警热重载不等在途回调（`alertset.go:51-54`、`TestAlertManagerApplyBoundaryInFlightAndNewSubscriptions`）。
- **现有测试证明了什么：** `notifier/bus_test.go`：接口回调异步、慢回调不持全局锁、错误隔离、channel 满则跳过、取消订阅不关 channel；`webui/api/alertset_test.go`：候选构造零副作用、替换与幂等、apply 顺序。
- **现有测试没有证明什么：** 无在途数量上界断言、无 SMTP 会话时长断言、无满载语义断言；`email.go`/`webhook.go` 的真实网络行为属人工验收层（PT-B6-08/09 已记人工验收免除）。
- **本轮动态复现：** 是（`/tmp/fwreview/notifier_repro_test.go`）：(1) 指向「接受连接但不发 220」的本地 SMTP → `SMTP 会话在 5s 内未返回`；(2) 一个永久阻塞的订阅者 + 连续 50 次 `Publish` → `发布 50 个事件耗时=217µs；goroutine: 3 → 52（差值=49）`。**未**复现真实资源耗尽。
- **与 §二 原描述的差异：** 结论准确、`bus.go:108-115` 未漂移（`syncer.go:488-492` → `:493-497`）。需补：`Publish` 不阻塞生产者、同步轮次不等待告警、进程退出不等待在途告警。
- **修复方案（推荐）：** 分两步独立提交。**(1) SMTP 完整会话 deadline**：`smtp.Client` 的 `conn` 未导出（标准库已确认），无法对既有 client 设 deadline，因此改为 `net.Dialer{Timeout:10s}.Dial` + `conn.SetDeadline(now+30s)` + `smtp.NewClient` + `Mail/Rcpt/Data/Quit`，使 dial、TLS/STARTTLS、AUTH、DATA、QUIT 全部有界。**(2) 在途上限**：在告警订阅者（或 `EventBus` 接口投递处）加固定容量在途计数（如 buffered channel 信号量，容量 4），满载丢弃 + `slog.Warn`（只含事件类型与渠道名），保持 `Publish` 不阻塞。
- **备选方案：** (a) 只在 `EventBus` 限流（集中一处但会一并限制 SSE/日志写入者，语义过宽）；(b) 单一告警工作队列 + 合并去重（更好但引入新抽象，与「简单轻量化」相悖）；(c) 只做 SMTP deadline（成本最低，但满载行为仍不可控）。
- **必须保持的不变量：** `Publish` 不阻塞；SSE channel 满则跳过；告警热重载不等在途；日志绝不出现 SMTP 密码/Webhook URL（`alertset.go:110-120`）；同步轮次不等待告警。
- **建议修改范围：** 生产 `notifier/email.go`（必须）、`notifier/webhook.go`/`notifier/bus.go`（若做上界）、可能 `webui/api/alertset.go`；测试 `notifier`；README 告警章节补「有超时上限，满载丢弃并记录 WARN」。
- **建议回归测试：** `notifier/email_timeout_test.go`（静默本地 SMTP，断言 deadline+裕量内返回错误、连接被关闭；可用 goroutine 计数辅助，`-race`）；`notifier/bus_bound_test.go`（阻塞订阅者 + N 次发布，断言在途 ≤ 上限、超出被丢弃且 WARN 被记录）。
- **外部验收需求：** 真实 SMTP/收件箱与 Webhook 端到端仍只能人工验证（PT-B6-08/09），**不得写成已通过**。
- **风险与依赖：** 采用「丢弃」策略时突发故障可能收不到全部告警（需在文档写明）；SMTP 自建会话比 `SendMail` 多约 30 行，需覆盖 STARTTLS/PlainAuth 既有行为。
- **待用户决策：** **满载语义待确认**（推荐：固定上限 + 丢弃最新 + 每条 WARN 一次，不引入队列与重试）。可在批次 2 开工前确认。

#### 5.2.3 A3｜HTTP health 与同步健康

- **判定：** 设计/产品决策。**用户已决：保持 HTTP-only health，另加同步健康可观测。**
- **置信度：** 高（文档与源码一致）。
- **现行源码证据：** `webui/server.go:240-243` 固定 200 `{"status":"ok"}`；`syncer/syncer.go:325-342 SyncStatus{Running,Enabled,LastSync}`；`build/Dockerfile` 的 `HEALTHCHECK … /api/health`；`docker-compose.yml.example` 同；`README.md:292`、`:465-467`；`AGENTS.md` §八。
- **完整调用链：** Docker/Compose → `webui/server.go` mux → health handler（无状态依赖）；同步信息只在 `SyncStatus` 与前端 Dashboard。
- **生产可达性：** 生产可达（Docker 主健康信号）。
- **触发条件：** 同步长期停滞（ticker 空转或每轮全失败）时 health 仍 200 → 容器保持 `healthy`。
- **实际影响：** 同步停滞无法被编排/监控发现，也不触发任何自动动作；**不是** health 实现错误（现行契约即 HTTP 可用性）。
- **已有缓解：** 前端 Dashboard 显示「上次同步」；`sync_logs` 逐域记录。
- **现有测试证明了什么：** `webui/server_test.go:258 TestHTTPRoutesRegression`（health 返回 200 与固定 body）、`TestServerTimeoutContract`；`main_test.go` 以 `/api/health` 作为进程就绪探针。
- **现有测试没有证明什么：** 没有任何测试把「同步停滞」与 health 关联（当前契约下也不应有关联）。
- **本轮动态复现：** 否（语义核对，非缺陷复现）。
- **与 §二 原描述的差异：** 边界说明准确；`server.go:240-243` 未漂移，`syncer.go:317-336` → `:325-342`。
- **修复方案（按用户决策）：** 保持 `/api/health` 不变；在 `SyncStatus`/同步状态上增加健康可观测字段（与 A18 的 `last_success` 同批），前端 Dashboard 给出「同步健康/停滞」提示；如需告警复用既有 `EventSyncError` 通道，不新增机制。
- **备选方案：** （已排除）让 health 反映同步健康并返回 503——用户选择不采用（会改变 Docker 重启语义与现行文档契约）。
- **必须保持的不变量：** `/api/health` 仍只代表 HTTP 可用性；不设置全局 `ReadTimeout/WriteTimeout`；Docker HEALTHCHECK 仍打 `/api/health`；暂停/首次启动/无目标都不得判为「HTTP 不可用」。
- **建议修改范围：** 生产 `syncer/syncer.go`（与 A18 合并）、可选 `webui/api/sync.go`；前端 Dashboard；API `GET /api/sync/status` 增字段（向后兼容）；Docker 无变化；README 说明「health=HTTP 可用性，同步健康见 /api/sync/status」。
- **建议回归测试：** `syncer`：构造「已跑完但全部失败」「从未成功」「暂停」三种状态断言新字段；`webui/api`：断言新字段存在且 `/api/health` body 不变；`webui/server_test.go` 保持 health 固定断言。
- **外部验收需求：** Docker `healthy/unhealthy` 与前端提示需人工/容器验收（本轮未跑 Docker）。
- **风险与依赖：** 新增字段被误当作 health 会重现 A3 的争议（文档必须写明用途）；依赖 A18（同一状态结构）。
- **待用户决策：** 已决（保持 HTTP-only + 另加可观测）。

#### 5.2.4 A4｜SQLite `busy_timeout` 未覆盖连接池的每条连接

- **判定：** 确认存在。
- **置信度：** 高（驱动/标准库源码 + 隔离复现双向确认）。
- **现行源码证据：** `config/store.go:63-85`：`sql.Open("sqlite", path)`（无 DSN `_pragma`，`:64`）；`:74` 一次性 `PRAGMA busy_timeout=5000`；全仓无 `SetMaxOpenConns/SetMaxIdleConns`。驱动：`modernc.org/sqlite@v1.54.0/driver.go:50-54`（支持 `_pragma` 且可多次）、`sqlite.go:207-237`（拼 `pragma <v>`）、`conn.go:56,135-138`（**每条新物理连接**执行）、`lib/sqlite.go:10339-10350`（`sqlite3_busy_timeout` 写入该连接的 `FbusyTimeout/FbusyHandler`）、`conn.go:940-967`（仅 `db==0`/interrupted 才丢弃连接，空闲连接无限复用且不重跑 DSN pragma）；标准库 `database/sql`：`defaultMaxIdleConns=2`、`maxOpen<=0` 即无上限、`db.Exec` 只取一条连接。注意 `_busy_timeout=` 旧式参数在 v1.54.0 **不存在**（会被静默忽略）。
- **完整调用链：** 写路径 HTTP → `ConfigCoordinator.mutate`（`coordinator.go:90-144`，单锁 + 单写事务）→ `Store.*Tx`；并发写路径 `Syncer` 事件 → `StoreLogWriter.OnEvent`（`logwriter.go:64`）→ `Store.AddSyncLog`（`store.go:655-672`，普通 `db.Exec`，**不在协调器锁内**）；另有 `Store.ReplaceScannedResources`（`:338-353`，`WithTransaction`）与只读导出事务。
- **生产可达性：** 生产可达（同步日志写入与配置/扫描写入天然并发）。
- **触发条件：** 两条不同物理连接同时写，且竞争落在「非启动时那条连接」上。
- **实际影响：** 竞争连接**立即**返回 `database is locked (5) (SQLITE_BUSY)`：`AddSyncLog` 仅 WARN（丢一条日志）；`ReplaceScannedResources` 使 `/api/scan-resources` 返回 500；配置保存事务失败返回 500（重试通常成功）。属偶发可重试，不会损坏数据。
- **已有缓解：** WAL（读不阻塞写）；配置写经协调器串行；日志写失败只 WARN；pidfile 单实例。
- **现有测试证明了什么：** `config/store_test.go`/`store_tx_test.go` 覆盖 CRUD、事务回滚、引用检查等，但**全部单连接顺序执行**。
- **现有测试没有证明什么：** 无多连接并发写测试，无「每条连接 busy_timeout 生效」断言。
- **本轮动态复现：** 是（`/tmp/fwreview/config_repro_test.go`）：`连接[0] busy_timeout=5000 journal_mode=wal`、`连接[1..3] busy_timeout=0`；阶段 A（无 PRAGMA 连接竞争）`耗时=15.166µs err=database is locked (5) (SQLITE_BUSY)`；阶段 B（有 PRAGMA 的连接竞争）`耗时=5.057103125s` 同错误。即 §二 所述「原报告 /tmp 三连接实验」的可复现版本。
- **与 §二 原描述的差异：** 结论与机制一致（原 `store.go:64-76` → 现 `:63-85`，关键行 `:64`、`:74`）。需补：驱动支持 `_pragma` 且按连接执行；`_busy_timeout=` 不存在；错误文本形态；`AddSyncLog` 的 COUNT 错误也被忽略（见 A14）。
- **修复方案（推荐）：** DSN `_pragma`：`sql.Open("sqlite", "file:"+path+"?_pragma=busy_timeout(5000)&_pragma=journal_mode(WAL)")`，并删除启动时那两条一次性 `Exec` 中的 `busy_timeout`（WAL 可保留在 DSN，幂等）。
- **备选方案：** (a) `SetMaxOpenConns(1)` 单连接池（消除写竞争但把读与写、导出与写全部串行化，不推荐）；(b) `RegisterConnectionHook` 每连接执行 PRAGMA（等价但引入驱动特有 API）；(c) 只给 `AddSyncLog` 加写入重试（治标）。
- **必须保持的不变量：** SQLite 仍是唯一业务配置源；配置写仍经单一协调器单事务；`rules.targets` 语义不变；`sync_logs` 保留上限 1000 不变；不新增数据库依赖或迁移。
- **建议修改范围：** 生产 `config/store.go`（DSN + 删除一次性 Exec）；测试 `config`；API/前端/Docker 无变化（不改数据库文件格式，WAL 已是持久设置）。
- **建议回归测试：** `config/store_busy_test.go`：`OpenStore` 后固定 4 条连接逐一断言 `PRAGMA busy_timeout=5000`（判别性：改回旧写法即失败）；再用两条连接制造写锁竞争，断言第二条约 5s 后失败而非立即失败（±1s 容差，`-race`）。
- **外部验收需求：** 无（本地可完整验证）。
- **风险与依赖：** DSN 前缀写错会把连接串当文件名（需覆盖路径含 `?`/`#` 的用例）；`journal_mode(WAL)` 在每条连接重复执行（幂等）。与 A9/A14/A17 同文件，建议同批不同提交。
- **待用户决策：** 无。

#### 5.2.5 A5｜pause/resume 双重运行时写入

- **判定：** 已修复。
- **置信度：** 高。
- **现行源码证据：** `webui/api/sync.go:41-73` 两个 handler 只调用 `d.coordinator().Mutate(...SetSettingTx("sync_enabled", ...))`，**commit 后无任何** `Syncer.Pause()/Resume()`；`coordinator.go:90-144` 在锁内完成「事务 → 事务内快照 → 候选 RuntimeState → commit → apply」；`deps.go:110-142 applyCandidate` 是唯一发布点；`syncer.go:226-251 Pause/Resume/setSyncEnabled` 仍存在但生产无调用者。
- **完整调用链：** `POST /api/sync/pause` → `handleSyncPause` → `Mutate`（`BreakerPreserve`）→ `SetSettingTx` → `LoadBusinessSnapshotTx` → `BuildRuntimeState` → commit → `applyCandidate`（日志级别 → 告警集合 → `ApplyState`）→ `RuntimeManager.Apply` + 控制通知 → Run 更新本地相位。
- **生产可达性：** 生产可达且已修复（无双写窗口）。
- **触发条件：** 修复前为并发 pause/resume 或导入交错；现 HTTP 路径无法产生第二次运行时写入。
- **实际影响：** 修复前可能造成 SQLite `sync_enabled` 与运行时状态分裂；现在不存在。
- **已有缓解：** 协调器单锁 + 单事务 + 单一发布入口（Build6 §12.4）。
- **现有测试证明了什么：** `webui/api/sync_test.go:107 TestHandleSyncPauseResume`（断言 DB 值、已发布 `RuntimeState`、`applyCount` 恰为 1/2、stub 的 `paused/resumed` 必须为 false）；`TestPauseResumeThroughCoordinator`；本轮 `-race` 下均 PASS。
- **现有测试没有证明什么：** 未做「并发 pause/resume + 导入」随机压力测试（当前结构下不需要）；未覆盖真实浏览器点击顺序。
- **本轮动态复现：** 否（问题已不存在，改为运行判别性用例）。**注意：** §二 记载的「修复前真实失败」属 Build6 Step 7 自检，本次未回退代码复验。
- **与 §二 原描述的差异：** §二 已标「✅ 已修复」，与源码一致；行号 `sync.go:46-54,65-73`、`coordinator.go:77-78`、`syncer.go:235-246` 已漂移到 `:41-73`、`:90-144`、`:239-251`。
- **修复方案（推荐）：** 无需修复。可选清理：`webui/api/deps.go:22-23` 的 `Syncer` 接口仍要求 `Pause()/Resume()` 但生产无调用者，建议从接口删除（实现可保留），或在方法注释中明确「仅供测试，生产必须经协调器」。
- **备选方案：** 保持现状（可接受，多余接口方法不构成缺陷）。
- **必须保持的不变量：** 协调器是唯一运行时写入口；commit 后无失败发布顺序不变；HTTP 成功响应在 apply 之后写出。
- **建议修改范围：** 仅 `webui/api/deps.go`（若清理）+ 对应测试；无 API/SQLite/前端/Docker 变化。
- **建议回归测试：** 保留 `TestHandleSyncPauseResume` 作为判别性用例；删除接口方法后同步调整 stub。
- **外部验收需求：** 无。
- **风险与依赖：** 极低；建议放入最后的清理批次。
- **待用户决策：** 无。

#### 5.2.6 A6｜暂停后的 ticker/trigger（原描述）

- **判定：** 原描述**已修复**（相邻新缺陷见 A20）。
- **置信度：** 高。
- **现行源码证据：** `syncer/syncer.go:159-168` ticker 分支守卫（`if !s.IsEnabled() { slog.Debug(...); break }`，break 出 select 后进入统一状态重读与过渡判定）；`:169-179` trigger 分支守卫（`if !s.IsEnabled() { continue }`）；`:267-272 IsEnabled` 读已发布运行时状态；`:148-156` 暂停子循环不消费 ticker/trigger；`:289-297 drainTrigger` 在 false→true 时清空过期触发；测试有界清理 `syncer/state_test.go:111-124 stopRunBounded`（10s 上限）。
- **完整调用链：** ticker/trigger → Run 的 select → 守卫（已发布状态）→ `syncAll`；暂停时 `enabled=false` 进入暂停子循环，只响应 control/stop。
- **生产可达性：** 生产可达且已修复。
- **触发条件：** 暂停已在轮次进行中提交、控制通知尚未消费，此时 ticker 到期或排队 trigger 被消费。
- **实际影响：** 修复前会启动一轮已暂停的同步（违背 AGENTS §五）；现在会被丢弃并记录 Debug。
- **已有缓解：** 单一可合并控制通知 + 消费时重读最新状态；`drainTrigger` 避免恢复多跑一轮。
- **现有测试证明了什么：** `TestPausedPublishedStateDropsTickerRound`（直接发布暂停状态、不投递通知的消费前窗口；移除守卫时 `GetRules=2, want 1`）、`TestQueuedTriggerNotRunWhilePaused`、`TestStaleTriggerAfterPauseDoesNotAddRound`；本轮 `-race` 下 PASS。
- **现有测试没有证明什么：** issue 原报告的「ticker 额外轮次 12 次探针未复现」在修复前确无确定性探针；现用「已发布状态窗口」模型覆盖。另：现有清理虽已改为有界（10s），但**没有**覆盖「Stop 之后的新轮次」场景（见 A20）。
- **本轮动态复现：** 否（已修复，改为运行判别性用例）。
- **与 §二 原描述的差异：** §二 已标「✅ 已修复」；行号 `syncer.go:163-164` → `:159-168`、`state_test.go:107-115` → `:111-124`。**§二 未涵盖 Stop 门控**，本轮新增为 A20。
- **修复方案（推荐）：** 无需修复 A6 本身；A20 的 Stop 门控建议与本项共用同一守卫函数（见 A20）。
- **备选方案：** —（不需要）。
- **必须保持的不变量：** 「暂停时 ticker 与手动 trigger 均不触发同步」；`drainTrigger` 仍只在 false→true 调用；`IsEnabled` 仍以已发布状态为准。
- **建议修改范围：** 无（若与 A20 合并则见 A20）。
- **建议回归测试：** 保留现有三条判别性用例。
- **外部验收需求：** 无。
- **风险与依赖：** 与 A20/A16 同属调度状态机，建议同批。
- **待用户决策：** 已决（A20 并入同批）。

#### 5.2.7 A7｜恢复同步时可能漏掉立即一轮

- **判定：** 已修复（残留的「同轮内 pause+resume 合并」边界经评估不构成缺陷）。
- **置信度：** 高。
- **现行源码证据：** `syncer/syncer.go:146 wasEnabled := enabled`（本循环已处理相位）；`:194 enabled = latest.Config.SyncEnabled`；`:196-215` 过渡判定（`!wasEnabled && enabled` → `ticker.Reset` + `drainTrigger` + `syncAll()`）；`:89-100 ApplyState` 仍推进镜像，`:274-283 isEnabledMirror` 仅作运行时状态尚未发布时的回退。
- **完整调用链：** `POST /api/sync/resume` → 协调器 commit → `applyCandidate` → `ApplyState`（发布 + 控制通知）→ Run 消费 → 读最新快照 → false→true → 立即一轮。
- **生产可达性：** 生产可达。
- **触发条件：** 状态在 Run 两次读取过渡前值之间被替换（旧实现按镜像判定会误判为 true→true）。
- **实际影响：** 修复前恢复后可能只重置 ticker、延迟到下一个 interval；现在不会。
- **已有缓解：** 单一控制通知 + 消费时重读；`drainTrigger`。
- **现有测试证明了什么：** `syncer/state_test.go:348 TestResumeImmediateRoundAfterPhaseMirrorAdvance` 用 `SetStateAppliedHook` 作屏障注入交错，断言 `GetRules` 达 2 次（判别性：改回镜像判定即失败）；本轮 PASS。
- **现有测试没有证明什么：** 「暂停与恢复都在同一轮内提交、Run 从未观察到 false」无用例。评估：此时进行中的一轮已是当前配置（pause/resume 只改 `sync_enabled`），`ticker.Reset` 后按新 interval 继续，不会漏同步，故不判为缺陷；建议补注释或断言固化该语义。
- **本轮动态复现：** 否（已修复，改为运行判别性用例）。
- **与 §二 原描述的差异：** §二 已标「✅ 已修复」；行号 `:93-100/:150/:190-203` → `:89-100/:146/:196-215`。
- **修复方案（推荐）：** 无逻辑改动；补一句注释固化「同轮内 pause→resume 合并时不额外触发（进行中的一轮已使用当前配置）」。
- **备选方案：** 若产品要求「任何 resume 提交都必须立即一轮」，可改用状态版本号（generation）比较——成本更高，当前无必要。
- **必须保持的不变量：** 一轮一快照；false→true 立即一轮；true→true 只重置 ticker；暂停时不启动新轮次。
- **建议修改范围：** 仅注释（可选）。
- **建议回归测试：** 保留现有判别性用例；批次 3 可补「同轮内 pause+resume 合并」断言。
- **外部验收需求：** 无。
- **风险与依赖：** 低；与 A6/A20 同批做注释与测试收口。
- **待用户决策：** 无。

#### 5.2.8 A8｜完整导入是否应重置 DNS breaker

- **判定：** 已修复（实现与 2026-09-27 用户决策一致）；仅剩 **AGENTS 措辞冲突**待文档处理。
- **置信度：** 高。
- **现行源码证据：** `webui/api/coordinator.go:72-80`（`Mutate` 固定 `BreakerPreserve`、`MutateImport` 固定 `BreakerReset`，共用同一 `mutate` 锁/事务/发布顺序）；`webui/api/export.go:114-116`（导入 handler 调 `MutateImport`）；`webui/api/deps.go:88-104`（`buildCandidate(snapshot, policy)` 透传策略）；`syncer/state.go:74-83`（Preserve → `previous.Breaker.Clone()` + `SetThreshold`；否则 `NewCircuitBreaker`）；`run.go:84`（启动用 `BreakerReset`）；`dns/circuitbreaker.go:41-50 Clone`。
- **完整调用链：** `POST /api/config/import` → 纯数据预校验 → `MutateImport` → （事务内）`buildCandidate(..., BreakerReset)` → commit → `applyCandidate` → `ApplyState`。
- **生产可达性：** 生产可达；启动路径也使用 Reset（首启无历史计数）。
- **触发条件：** 若导入沿用 `BreakerPreserve`，旧域名集合的熔断计数会被带入新配置。
- **实际影响：** 修复前可能「新域名立刻被视为已熔断」或旧计数不释放；现在导入清空、普通变更保留。
- **已有缓解：** `Clone` 的锁内复制保证普通变更线程安全地保留计数。
- **现有测试证明了什么：** `webui/api/import_runtime_test.go:195 TestConfigImportResetsDNSBreakerOrdinaryChangePreserves`（阈值 2 + 2 次失败已熔断 → 导入清空、普通变更保留）；`syncer/state_test.go:575 TestBreakerPolicyPreserveVsReset`、`:617 TestBreakerPreserveAppliesNewThreshold`；本轮 PASS。
- **现有测试没有证明什么：** 未在长跑真实环境验证熔断进度（Build6 已如实记录该边界）。
- **本轮动态复现：** 否（已修复，改为运行判别性用例）。
- **与 §二 原描述的差异：** §二 已标「✅ 已修复（用户决策：完整导入重置）」；其「`deps.go:86-101` 固定传 BreakerPreserve」已过时（现为带 policy 形参的 `:88-104`）。**文档冲突：** `AGENTS.md:180` 仍写「完整导入**允许**新建并清空」（允许≠必须），`Build6.md` §12.3 第 7 条与源码为「**确定**重置」。
- **修复方案（推荐）：** 代码不改；后续把 `AGENTS.md:180` 改为「完整导入确定新建并清空（普通变更保留既有失败计数）」，与 Build6/源码统一。
- **备选方案：** （已否决）保留 AGENTS「允许」并把 Build6 改回可选语义。
- **必须保持的不变量：** 普通变更保留既有失败计数；阈值随完整 `RuntimeState` 原子发布；导入仍为「整库替换」；不新增散装 breaker setter。
- **建议修改范围：** 文档 `AGENTS.md`（1 行）；生产/测试无变化。
- **建议回归测试：** 无需新增（端点级判别已足够）。
- **外部验收需求：** 无。
- **风险与依赖：** 低；建议与 A19 的文档改动同批，避免多次触碰强要求文档。
- **待用户决策：** 已决（后续把 AGENTS 改为「确定重置」）。

#### 5.2.9 A9｜损坏的 `rules.targets` 被解释为「适用于全部目标」

- **判定：** 确认存在（需外部改库/损坏触发）。
- **置信度：** 高（调用链 + 隔离复现）。
- **现行源码证据：** `config/store.go:239-244`（`if targets != "" { … if err := json.Unmarshal(...); err == nil { r.Targets = nums } }`——解析失败**不返回错误**，`Targets` 保持 `nil`）；`syncer/syncer.go:555-570 filterRulesForTarget`（`len(r.Targets)==0` → 适用于所有目标）；`config/store.go:449-464 ReferencingRuleIDsTx`（复用 `loadRules` 做 409 判定）；`webui/api/bundle_v2.go:283-298 toBundleV2`（`r.Targets` → `target_export_ids`，`make([]int, len(nil))` → `[]`）与 `:456-469`（导入只校验字段存在与引用闭包）。
- **完整调用链：** 同步 `syncAll(:430) → filterRulesForTarget(:454)`；删除目标 `handleDeleteTarget → ReferencingRuleIDsTx → 未命中 → 允许删除`；导出 `handleConfigExport → LoadBusinessSnapshotTx → loadRules → toBundleV2`；导入 `validateAndNormalizeBundle → AddRuleTx`（写入 `[]`）。
- **生产可达性：** **仅当数据库被外部修改/损坏时可达**；正常 API 不可能写出非法 JSON（`store.go:252-265 ruleColumns` 用 `json.Marshal([]int)`）。
- **触发条件：** 直接改库、文件损坏或外部程序写坏该列，且文本非空且非合法 JSON 数组。
- **实际影响：** (1) 规则被下发到**全部目标**（范围扩大，可能在非预期实例上开放端口）；(2) 删除目标漏判 409，删除后该规则进一步扩张到剩余全部目标；(3) 导出/导入把「全部目标」固化到新库；全程无错误、无 WARN。
- **已有缓解：** 无（`GetRules`/快照/导出都不报错）。
- **现有测试证明了什么：** `config/store_tx_test.go:133 TestReferencingRuleIDsTx`（精确匹配、避免 LIKE 子串误判）、`TestRuleTargetReferenceIsExact`、`TestTargetDeleteReferencedConflict` 覆盖正常路径。
- **现有测试没有证明什么：** 没有任何用例写入非法 `targets` 文本，也没有断言「空数组」与「非法 JSON」的差异。
- **本轮动态复现：** 是（`/tmp/fwreview/config_repro_test.go::TestReviewRepro_A9CorruptRuleTargets`）：`'[1,'` → `Targets=[]int(nil) (nil=true, len=0)` 且 `GetRules err = <nil>`；`'[]'` → `[]int{}`；`'null'` → `nil`；`ReferencingRuleIDsTx(target=1) = [1]`（损坏行不再被计入引用）；`LoadBusinessSnapshot err = <nil>`。
- **与 §二 原描述的差异：** 结论准确、行号基本一致（`store.go:239-244`/`:449-464` 未漂移，`syncer.go:550-565` → `:555-570`）。需补：`'null'` 与损坏等价；导出/导入会把该行固化为 `[]`。
- **修复方案（按用户决策：非空但非法即报错）：** `loadRules` 区分三种情况：空串 → 保持现状（`Targets=nil`）；`"null"`/`"[]"` → 空数组（显式全部目标）；其余解析失败 → 返回带上下文（规则 ID/host，不含值）的错误，例如 `规则 #%d 的 targets 不是合法 JSON 数组`。错误自然传播到 `GetRules`/`LoadBusinessSnapshotTx`/导出/引用检查（库损坏时删除目标返回 500 而非静默放行）。错误分类仍按内部错误（500 + 安全文案），不新增 4xx。
- **备选方案：** （已否决）解析失败视为「不适用任何目标」；（已否决）维持现状 + WARN。
- **必须保持的不变量：** 空数组仍表示「适用于全部目标」（AGENTS §9.1 与 `ValidateRuleTargetsTx`）；正常写入路径不变；不使用全量覆盖；不静默删除引用、不把规则扩大为全部目标。
- **建议修改范围：** 生产 `config/store.go`（约 5 行）；测试 `config`（新增或扩展）；API：错误码在库损坏时由 409/200 变 500（需文档说明）；SQLite 无迁移；前端沿用现有错误提示；Docker 无变化。
- **建议回归测试：** `config`：直插 `'[1,'`/`'null'`/`'[]'`/`''` 四行，断言 `GetRules`（损坏→错误；其余→空集）、`ReferencingRuleIDsTx` 损坏时返回错误、`LoadBusinessSnapshotTx` 返回错误；`syncer`：断言加载失败时同步轮次报错且不会把损坏规则扩散为全部目标。
- **外部验收需求：** 无（本地可完整验证）。
- **风险与依赖：** 若生产库已被写坏，升级后启动会直接失败（`LoadBusinessSnapshot` 错误 → `run.go` 退出码 1）——这是「宁可失败」的预期，但需在发布说明中提示修复方式；与 A4/A14/A17 同文件，建议同批。
- **待用户决策：** 已决（非法即返回明确错误）。

#### 5.2.10 A10｜配置 reset 接受 JSON `null`

- **判定：** 已修复。
- **置信度：** 高。
- **现行源码证据：** `webui/api/decode.go:76-92 decodeJSONObjectStrict`（空 body→400；首字节非 `{`→400，因此 `null`/`[]`/`1`/`"str"`/`true` 全拒；随后复用 `decodeJSONStrict` 仍拒未知字段/尾随值/多顶层值）；`settings.go:173-190 handleConfigReset` 使用之并解码到空结构体。
- **完整调用链：** `POST /api/config/reset` → `decodeJSONObjectStrict` → `coordinator.Mutate(ResetAllTx)`。
- **生产可达性：** 生产可达（前端有二次确认，非前端调用方也可直接调）。
- **触发条件：** 调用方发送 `null` 或其他非对象。
- **实际影响：** 修复前 `null` 会通过并执行全量清空；现在 400 且零写入零 apply。
- **已有缓解：** 前端二次确认；协调器事务原子性。
- **现有测试证明了什么：** `webui/api/settings_alerts_test.go:267 TestConfigResetStrictBody` 覆盖 `null`、`  null  `、`[]`、`[{}]`、`1`、`"str"`、`true`、未知字段与空 body，断言 400 + 数据保留 + `apply==0`；本轮 PASS。
- **现有测试没有证明什么：** 未显式断言运行时状态指针不变（`apply==0` 已间接覆盖）。
- **本轮动态复现：** 否（已修复，改为运行判别性用例）。
- **与 §二 原描述的差异：** §二 已标「✅ 已修复」；行号 `settings.go:173-181` → `:173-190`、`settings_alerts_test.go:266-281` → `:267-336`。
- **修复方案（推荐）：** 无需修复。
- **备选方案：** —。
- **必须保持的不变量：** reset 只接受单一空对象 `{}`；未知字段/尾随 JSON/多顶层值仍 400；非法输入零写入零 apply。
- **建议修改范围：** 无。
- **建议回归测试：** 保留现有用例。
- **外部验收需求：** 无。
- **风险与依赖：** 无。
- **待用户决策：** 无。

#### 5.2.11 A11｜SWAS 跳过 DROP 后仍被计作「新增成功」

- **判定：** 确认存在。
- **置信度：** 高（静态 + provider 层既有 mock 测试 + 计数层隔离复现）。
- **现行源码证据：** `provider/ali_swas.go:111-148`（`:117-135` 对 `strings.EqualFold(r.Action,"DROP")` 记 `slog.Warn("SWAS 不支持 DROP 规则，跳过", …)` 后 `continue`；全部跳过则 `:133-135 return nil`）；`syncer/retry.go:64-78`（`CreateRules` 返回 nil → `added += len(diff.ToAdd)`）；`syncer/syncer.go:538-542`（`EventDomainSyncComplete{added, deleted}`）；`webui/api/logwriter.go:52-60`（写入 `sync_logs.added`）；`config/validate.go:39`（`validActions` 允许所有云保存 `DROP`）；`README.md:415-417`（说明会 WARN 并跳过）。
- **完整调用链：** 规则保存（允许 DROP）→ 协调器发布 → `syncAll → syncDomain → syncDomainInternal → retrySync → provider.Diff/buildDesired`（生成 DROP action）→ `AliSWAS.CreateRules`（丢弃）→ 返回 nil → `added` 计数 → 事件 → `sync_logs` → 前端历史/告警。
- **生产可达性：** 生产可达（只要给 SWAS 目标配 DROP，含跨目标规则中部分目标为 SWAS）。
- **触发条件：** 规则 `action=DROP` 且目标含 `ali_swas`。
- **实际影响：** 历史与事件报告「新增 N 条」而云端 0 条；用户以为封禁已生效（安全语义误导）。附带：Dry Run 的 `to_add` 也会列出该 DROP 规则，与真实写入不一致。
- **已有缓解：** 每条 DROP 有 WARN 日志（需主动查日志）；README 有说明。
- **现有测试证明了什么：** `provider/request_mock_test.go:587 TestRequest_SWASPortSlashDropSkipAndDelete`（本地 mock 云端点证明：全 DROP 不发请求、混合场景只提交 ACCEPT、ICMP 端口 `-1/-1`、删除只提交非空 RuleId）。
- **现有测试没有证明什么：** 没有断言 `retrySync` 在「被跳过」时的 `added` 取值；`syncer` 计数测试都使用真实写入的 fake provider，因此存在「测试通过但风险仍在」。
- **本轮动态复现：** 是（计数层，`/tmp/fwreview/syncer_repro_test.go::TestReviewRepro_A11SkippedDropStillCountedAsAdded`）：用复刻 SWAS 契约（跳过 DROP、返回 nil）的 fake provider → `added=1 deleted=0 err=<nil>；Provider 实际写入条数=0`。**未**用真实阿里云 API 验证（不做真实写入）。
- **与 §二 原描述的差异：** 结论准确；`ali_swas.go:118-135` → `:117-135`、`retry.go:63-77` → `:64-77`。需补 Dry Run 的 `to_add` 同源不一致。
- **修复方案（按用户决策：返回实际写入数/跳过数）：** 二选一、保持最小：**(a) 可选接口（推荐）**：新增 `type CreateResult struct{ Written, Skipped int }`，`Provider.CreateRules` 签名不变，另加可选接口 `CreateRulesCounted([]config.RuleAction) (CreateResult, error)`；`retrySync` 优先类型断言走带计数版本，其余 Provider 走原路径；`AliSWAS` 在跳过分支累加 `Skipped`，`Written==0 && Skipped>0` 时正常返回结果而不报错；`retrySync` 以 `Written` 计入 `added`，把 `Skipped` 放入 `EventDomainSyncComplete.Data`（新增 `skipped` 键）与日志。**(b)** 直接把 `CreateRules` 改为返回 `(CreateResult, error)`（4 个 Provider + 全部测试 stub 同步改）。
- **备选方案：** （用户已否决）保存阶段拒绝 SWAS+DROP；（用户已否决）仅补文档。若不愿动接口，可在 `AliSWAS.CreateRules` 对「全部跳过」返回可识别哨兵错误由 `retrySync` 识别为「未写入」——最小改动但以错误表达控制流，可读性差，作次选。
- **必须保持的不变量：** SWAS 的 DROP 仍必须跳过而非报错（云 API 无 Policy 字段）；「规则已存在/已不存在」的幂等跳过语义不变；`added/deleted` 口径与 Dry Run 的 `to_add/to_delete` 尽量一致（如需一致，Dry Run 也按 Provider 能力过滤）；跳过不触发重试。
- **建议修改范围：** 生产 `provider/provider.go`（可选接口或签名）、`provider/ali_swas.go`、可能其余 3 个 Provider、`syncer/retry.go`、`syncer/syncer.go:538-542`、可选 `webui/api/logwriter.go`；测试 `provider`/`syncer`；API：事件 Data 增键（前端需容忍未知键）；SQLite：本批建议**不加列**（避免迁移）；README 补「跳过不计入新增」。
- **建议回归测试：** `syncer`：SWAS 契约 fake provider 断言 `added=0, skipped=N` 且事件 Data 含 `skipped`；混合场景（1 DROP + 1 ACCEPT）断言 `added=1, skipped=1`；`provider/request_mock_test.go` 增断言（判别性：改回 `nil` 计数即失败）。
- **外部验收需求：** 真实 SWAS 实例上配置 DROP 后观察日志与历史记录（用户真机）。
- **风险与依赖：** 事件 Data 结构变化会进入 `sync_logs` 与 SSE 消费方；UI 文案建议「已跳过 N 条 SWAS 不支持的 DROP 规则」。与 A18 的整轮汇总共用事件字段设计，建议先 A11 的 Provider 返回、再 A18 的汇总。
- **待用户决策：** 已决（Provider 返回实际写入数/跳过数，事件与日志区分）。

#### 5.2.12 A12｜超时错误文案绕过重试

- **判定：** 确认存在。
- **置信度：** 高（真实错误形态已在本机复现）。
- **现行源码证据：** `syncer/retry.go:86-101 isRetryable`（`strings.Contains(err.Error(), r)`，候选含小写 `timeout`、`connection refused`、`RequestLimitExceeded`、`InternalError`、`FirewallBusy`）；调用点 `:35`（GetRules）、`:53`（DeleteRules）、`:70`（CreateRules）；`maxRetries=3`（`:14`）、退避 `1s/2s`（`:26`）。标准库：`net/http/client.go:737` 生成 `context deadline exceeded (Client.Timeout exceeded while awaiting headers)`；`net/http/transport.go:2772-2775`（`timeoutError.Is(err)=(err==context.DeadlineExceeded)`、`Timeout()=true`）；`net/url/url.go:40` 的 `url.Error.Error()` 前缀 `Op "URL": `。腾讯 SDK 默认 60s 客户端超时；阿里 SDK 目前无超时（A1）。
- **完整调用链：** Provider 方法 → `fmt.Errorf("%w")` → `retrySync` → `isRetryable` 决定「继续重试」或「立即返回错误」→ `syncDomainInternal` 记 `EventSyncError` + `slog.Error`（首轮即失败会立即产生错误告警）。
- **生产可达性：** 生产可达（腾讯 60s 超时、A1 修复后的阿里超时、中间设备断开等）。
- **触发条件：** 任何以 `Client.Timeout exceeded …` 形态出现的超时（最常见形态）。
- **实际影响：** 可恢复超时被当作永久错误：只尝试 1 次、立即告警并写 `sync_logs.failed`，与 AGENTS §六「写入失败自动重试（最多 3 次，指数退避）」不符。
- **已有缓解：** 含小写 `timeout` 的形态（如 `i/o timeout`）与 `connection refused`、限流/内部错误仍会重试。
- **现有测试证明了什么：** `syncer/syncer_test.go` 的重试用例（`TestRetrySync_Counts`、`TestRetrySync_IdempotentErrorsNotCounted`、`TestRetrySync_PartialWriteCounting` 等）用 `errors.New("InternalError: …")` 触发重试，证明重试流程、幂等跳过与计数。
- **现有测试没有证明什么：** **没有任何测试直接调用 `isRetryable`**，也没有使用真实超时错误形态；测试全绿完全不能覆盖本项。
- **本轮动态复现：** 是（`/tmp/fwreview/syncer_repro_test.go::TestReviewRepro_A12IsRetryableTimeoutShape`）：真实形态 `Get "http://127.0.0.1:51590": context deadline exceeded (Client.Timeout exceeded while awaiting headers)` → `isRetryable=false`，同 `errors.Is(err, context.DeadlineExceeded)=true`、`errors.As(err,&net.Error)=true / Timeout()=true`；`context.DeadlineExceeded` 值 → false；`url.Error` 含 `i/o timeout` → true；`net/http: timeout awaiting response headers` → true；`connection refused` → true；`InternalError` → true；中文「请求超时」→ false。
- **与 §二 原描述的差异：** 结论与所引字符串**完全一致**，`retry.go:86-101` 未漂移。需补：SDK 侧（阿里）默认只尝试 1 次、腾讯 SDK 无内置重试，故应用层判定是唯一重试来源；`net.Error.Timeout()` 与 `errors.Is(DeadlineExceeded)` 在当前 Go 版本对上述真实错误均可用，不必依赖字符串。
- **修复方案（推荐）：** `isRetryable` 开头加结构化判定：`errors.Is(err, context.DeadlineExceeded)` → true；`var ne net.Error; errors.As(err,&ne) && ne.Timeout()` → true；保留既有字符串列表作兜底（云厂商业务错误码仍只能靠字符串）。不要只用 `context.DeadlineExceeded`（`net.Error.Timeout()` 覆盖更全）。
- **备选方案：** (a) 改为大小写不敏感的 `strings.Contains(strings.ToLower(msg),"timeout")`（最小改动但语义较糊）；(b) 建立可重试错误码表（需各云错误码清单，过度设计）。
- **必须保持的不变量：** 重试仍重新走完整 Describe→Diff→Create/Delete；最多 3 次与指数退避不变；幂等错误不重试不计数；不绕过云厂商限速间隔。
- **建议修改范围：** 生产 `syncer/retry.go`（约 8 行 + import `context`/`net`）；测试新增 `syncer/retry_test.go`；无 API/SQLite/前端/Docker 变化。
- **建议回归测试：** `syncer/retry_test.go::TestIsRetryableErrorShapes`：表驱动覆盖真实 `Client.Timeout` 错误（用 `httptest` 慢服务器 + 短超时构造，避免硬编码字符串）、`context.DeadlineExceeded`、`i/o timeout`、`connection refused`、`InternalError`、未知业务错误码；再加端到端：`fakeTagProvider.failCreateOn=1` 返回真实超时 → 断言 `CreateRules` 被调用 2 次（判别性：当前为 1 次）。`-race`。
- **外部验收需求：** 真实云超时下的重试行为（用户弱网环境观察 `重试同步 attempt=2`）。
- **风险与依赖：** 重试次数增加会延长单轮时长（与 A18 无超时等待叠加），因此**必须在 A1 的有限超时落地之后**再放宽超时重试；与 A1 强绑定，建议同批且 A1 先行。
- **待用户决策：** 无。

#### 5.2.13 A13｜普通 ticker/trigger 轮次重复记录「告警已更新」

- **判定：** 确认存在（严重度低）。
- **置信度：** 高。
- **现行源码证据：** `syncer/syncer.go:217-218`（每轮循环末尾无条件 `s.logAppliedState(latest)`）；`:253-261 logAppliedState`；`run.go:103`（`SetStateAppliedHook(func(*syncer.RuntimeState){ alertManager.LogStatus("已更新") })`）；`webui/api/alertset.go:113-121 LogStatus`（启用渠道时输出 `邮件告警已更新`/`Webhook 告警已更新`，INFO，只含收件人/渠道名）。
- **完整调用链：** ticker 到期或手动 trigger → `syncAll` → 循环末尾 `logAppliedState` → `LogStatus("已更新")` → slog → stdout + LogBroadcaster（SSE 日志流）。
- **生产可达性：** 生产可达（仅在启用邮件或 Webhook 告警时产生日志）。
- **触发条件：** 任意一次普通定时/手动同步轮次结束。
- **实际影响：** 每次同步都输出「告警已更新」INFO，与配置是否变化无关；运维可能误判「配置被更新过」；日志噪声随 interval 缩短而增加；同时出现在 WebUI 日志流。无功能影响。
- **已有缓解：** `LogStatus` 在未启用任何渠道时不输出（`:114-116`）。
- **现有测试证明了什么：** `webui/api/alertset_test.go:174 TestApplyCandidateOrderAndEffects`、`:306 TestAlertManagerEnabledChannelsMetadata` 覆盖发布顺序与安全日志内容。
- **现有测试没有证明什么：** 没有断言 hook 的触发**频率**，也未区分「配置变更触发的应用」与「普通轮次末尾」。
- **本轮动态复现：** 是（`/tmp/fwreview/syncer_repro_test.go::TestReviewRepro_A13StateAppliedHookPerRound`）：全程无 `ApplyState`（无配置变更）时 `已完成轮次=4，状态应用 hook 调用次数=3`。
- **与 §二 原描述的差异：** 结论准确；`syncer.go:214` → `:218`，`run.go:103` 未漂移。需补「该日志同时进入 WebUI 日志流」。
- **修复方案（推荐）：** 给 `RuntimeState` 增加单调递增的 `Generation uint64`（由 `BuildRuntimeState` 或 `RuntimeManager.Apply` 分配），Run 记录本循环已通知的 generation，仅当 `latest.Generation != lastNotified` 时调用 `logAppliedState` 并更新；启动首次仍通知一次（`run.go:101-102` 的「已启用」保持独立）。
- **备选方案：** (a) 把 hook 移到「消费 controlCh 分支后」（更贴合语义，但通知会合并，需配合 generation 才严谨）；(b) 只在 `applyCandidate`（`deps.go:110-142`）里直接 `LogStatus("已更新")` 并删掉 Syncer hook——最简单，但会失去「状态已被 Run 真正消费」的屏障语义，且 `TestResumeImmediateRoundAfterPhaseMirrorAdvance` 依赖该 hook 作屏障，需另行提供屏障。
- **必须保持的不变量：** hook 仍是「状态已真正生效」的屏障（不得改为纯发布时通知）；`LogStatus` 只记录收件人/渠道名（绝不记录密码/URL）；启动仍输出一次「已启用」。
- **建议修改范围：** 生产 `syncer/state.go`（+1 字段）、`syncer/syncer.go`（约 5 行）；测试 `syncer`；其余无变化。
- **建议回归测试：** `syncer/state_test.go::TestStateAppliedHookOnlyOnRealStateChange`：间隔 50ms 跑若干轮 → 断言 hook 次数不随轮次增长；随后 `ApplyState` 一次 → 断言 hook +1（判别性：当前实现下第一条断言失败）。
- **外部验收需求：** 无。
- **风险与依赖：** 若 generation 加在 `RuntimeManager.Apply`，则 pause/resume、启动、导入等所有发布都会 +1（正确）；需保持 `Snapshot` 返回指针不可变。与 A3/A18 同批（同一状态结构），建议独立提交。
- **待用户决策：** 无。

#### 5.2.14 A14｜未处理的 error（Rollback、Encode 等）

- **判定：** 确认存在（明确违反 AGENTS §十一；实际业务影响需分项看）。
- **置信度：** 高（源码 + 标准库语义）。
- **现行源码证据：** (1) `config/store.go:325-335 WithTransaction`：`if err := fn(tx); err != nil { tx.Rollback(); return err }`——**忽略 `Rollback` 错误**，且 `fn` panic 时没有任何 `defer` 回滚；(2) `webui/api/deps.go:197-202 writeJSON`：`json.NewEncoder(w).Encode(data)` 返回值被丢弃；(3) 相邻等价处理：`config/store.go:664-670 AddSyncLog` 的 `SELECT COUNT(*)` 错误被静默忽略（`:665`）。
- **完整调用链：** `WithTransaction` ← `ReplaceScannedResources`（`store.go:338-353`，`/api/scan-resources`）；`writeJSON` ← 全部 handler 的成功响应路径；`AddSyncLog` ← `StoreLogWriter`（同步日志）。
- **生产可达性：** 代码路径可达，但触发条件苛刻（见下）。
- **触发条件：** `Rollback` 失败需事务因连接断开/被中断而失效；`Encode` 失败需响应体含不可编码值（当前 DTO 全是可编码类型，不可达）；panic 需 `fn` 内 panic（当前 `fn` 只调用 Store 方法）。
- **实际影响：** (1) 回滚失败被静默吞掉 → 缺少「数据可能未回滚」的信号（`database/sql` 通常会把该连接标记为坏并在下次 `Begin` 报错，故实际业务错误概率低）；(2) `Encode` 忽略当前**不可达**（且状态码已写出后无法改变）；(3) COUNT 失败只影响日志裁剪（`sync_logs` 可能超过 1000 条）。
- **已有缓解：** 协调器 `coordinator.go:98-106` 与导出 `export.go:33-41` 已使用正确模式（有条件 defer + 忽略 `sql.ErrTxDone` + 记录其他错误），可直接作为样板。
- **现有测试证明了什么：** `config/store_tx_test.go`、`webui/api/import_failure_test.go`（8 项失败注入 + commit 失败）证明事务回滚与「失败零写入零发布」；`webui/api/redact_test.go` 证明错误文案不泄露值。
- **现有测试没有证明什么：** 没有注入 `Rollback` 失败或 `fn` panic 的用例；`go vet` 不检查未处理的 error（`errcheck` 不属于 vet）。
- **本轮动态复现：** 否（`Rollback` 失败与不可编码响应体在生产代码中难以人为触发，强行构造需替换 `*sql.Tx`，属过度构造）。**本项只完成代码路径核验。**
- **与 §二 原描述的差异：** 两处指认**都仍成立**（`store.go:325-335` 未漂移；`deps.go:196-200` → `:197-202`）。需补第三处（`store.go:665`）与「协调器已有正确样板」；§二「不能据当前证据断言无资源泄漏后果」的边界维持。
- **修复方案（推荐）：** (1) `WithTransaction` 改为协调器模式（`defer` 按 `committed` 标志回滚 + 非 `sql.ErrTxDone` 记 ERROR；panic 时回滚后重新 panic）；更简单的是让 `ReplaceScannedResources` 直接用 `BeginTx` + 既有 defer 模式并**删除 `WithTransaction`**（当前仅 1 个生产调用者 + 3 处测试）。(2) `writeJSON` 处理 `Encode` 错误（记录 ERROR，不改已写状态码）。(3) `AddSyncLog` 的 COUNT 失败记 WARN。
- **备选方案：** (a) 把 `errcheck` 加入 CI（系统性但会带来大量既有告警与新工具，与「简单轻量化」冲突）；(b) 只修 `WithTransaction`（成本最低，但仍是「已知未处理 error」）。
- **必须保持的不变量：** 事务失败必须完整回滚且不产生部分写入；panic 不得被静默吞掉；响应写出顺序与状态码不变；协调器/导出路径的 defer 模式不变。
- **建议修改范围：** 生产 `config/store.go`、`webui/api/deps.go`；测试 `config`（panic 与回滚错误路径）、可选 `webui/api`（自定义 ResponseWriter 注入 Encode 失败）；无 API/schema/前端/Docker 变化。
- **建议回归测试：** `config`：`TestWithTransactionRollsBackOnPanic`（recover 后断言数据未写入）、`TestWithTransactionLogsRollbackError`（注入已 `Commit` 的 tx 触发 `ErrTxDone`，断言不误报 ERROR）；`webui/api`：`Write` 返回错误的 ResponseWriter 下断言 handler 不 panic 且记录 ERROR。
- **外部验收需求：** 无。
- **风险和依赖：** 删除 `WithTransaction` 需同步改 3 处测试；panic-recover 写法不当会吞掉真实 panic（必须重新 panic）。与 A4/A9/A17 同文件，建议同批不同提交。
- **待用户决策：** 无（AGENTS §十一 是强要求；是否引入 errcheck 属可选，未纳入推荐）。

#### 5.2.15 A15｜两类 SSE 忽略写入错误，半开连接缺少写出边界

- **判定：** 确认存在（「忽略写错误」与「无写出边界」均在代码中确认；「Flush 阻塞导致占用」未动态复现）。
- **置信度：** 高（写错误部分有动态复现；阻塞部分依赖标准库与网络栈，未实测）。
- **现行源码证据：** `webui/api/sync.go:122-127`（`json.Marshal` 错误已处理，随后 `fmt.Fprintf(w, "data: %s\n\n", data)` **忽略返回值** + `flusher.Flush()`）；`logstream.go:167-172` 同构；退出条件只有 `r.Context().Done()`（`sync.go:128`）、`d.ShutdownCh`（`:130`）、channel 关闭（`logstream.go:168`）；`webui/server.go:151-157` 明确不设全局 `ReadTimeout/WriteTimeout`。标准库：`net/http/responsecontroller.go:105-117 SetWriteDeadline`（先对 ResponseWriter 断言 `SetWriteDeadline(time.Time) error`），`net/http/server.go:503-505` 的 `*response` 直接实现（HTTP/1 可用）；`Server.WriteTimeout`（`server.go:3002-3007` + `:986-990`）是每请求一次性绝对 deadline，会切断长连接，不能用于 SSE。
- **完整调用链：** 浏览器 EventSource → `GET /api/sync/events`（或 `/api/logs/stream`）→ 订阅 → 事件/日志到达 → 写 + Flush → 半开连接时可能长期阻塞。
- **生产可达性：** 生产可达（两个 SSE 端点是 WebUI 常驻连接）。
- **触发条件：** 客户端异常断开而 TCP 未及时收到 FIN/RST（半开）、或客户端停止读取导致发送缓冲写满。
- **实际影响：** (1) 可检测的写错误被忽略 → handler 不退出、连接对象与订阅继续占用；(2) 若阻塞在 `Write`/`Flush`，goroutine 与订阅长期挂住（此时 `ShutdownCh` 分支无法被选中），只能等 HTTP shutdown 10s 超时后 `Close()` 强制断开。**只检查 `Fprintf` 返回值不能解决已阻塞在 `Flush` 的情况。**
- **已有缓解：** 两类 SSE 都监听服务器级 `shutdownCh` 并 defer `unsubscribe()`（`server.go:210-217` 先关 channel 再 `Shutdown`）；`TestProcessSSEExitsOnServerShutdown` 证明正常客户端下 shutdown 能及时退出。
- **现有测试证明了什么：** `webui/api/sync_test.go:196/226/289`（context 取消、客户端断开、服务器 shutdown 三种情况退出并 `unsubscribe`）、`logstream_test.go:134/176`、`webui/server_test.go:637 TestShutdownTimeoutForcesCloseOfInflightRequest`。
- **现有测试没有证明什么：** 没有用例让 `Write` 返回错误或让 `Flush` 阻塞；测试中的 ResponseWriter 都是 `httptest` 正常实现，故「写错误被忽略」在测试里完全不可见。
- **本轮动态复现：** 部分（`/tmp/fwreview/api_repro_test.go::TestReviewRepro_A15SSEIgnoresWriteError`）：自定义 ResponseWriter 让每次 `Write` 都返回错误 → `写入次数=3，Flush 次数=4`，handler 直到 `ShutdownCh` 关闭才退出，证明写错误被完全忽略。**未复现**「阻塞在 Flush」的半开连接场景。
- **与 §二 原描述的差异：** 结论与边界说明（含「只检查 Fprintf 返回值不能解决已阻塞在 Flush」）**准确**；`logstream.go:171-172` 未漂移，`sync.go:127-128` → `:126-127`。需补：`ResponseController.SetWriteDeadline` 在 HTTP/1 可用，写出边界有可行实现路径。
- **修复方案（推荐）：** 分两步。**(1) 必须**：处理 `fmt.Fprintf` 返回值（`if _, err := fmt.Fprintf(...); err != nil { slog.Debug("SSE 写出失败，退出", "error", err); return }`），两类 handler 同样处理（`Flush` 无返回值，无法从错误判断）。**(2) 可选（半开连接边界）**：每次写出前 `rc := http.NewResponseController(w); rc.SetWriteDeadline(time.Now().Add(15*time.Second))`，写出与 `Flush` 后 `rc.SetWriteDeadline(time.Time{})` 清除；若返回不支持则退回当前行为并记录一次 Debug（注意清除必须放在同一写出周期内，避免影响 HTTP/1 keep-alive 后续请求）。
- **备选方案：** (a) 只做 (1)：可检测失败会退出，真正阻塞在写的半开连接仍只能等 shutdown 10s 强制 Close；(b) 用 `ConnContext`/`Hijack` 登记连接并在 shutdown 主动 `Close`：更彻底但复杂，标准库能力已足够；(c) 为 SSE 单独开 `http.Server` 以设 `WriteTimeout`：与单端口架构冲突，不推荐。
- **必须保持的不变量：** 不设置破坏长连接的全局 `WriteTimeout`；两类 SSE 继续监听 `ShutdownCh` 与 request context；`EventBus` 取消订阅不关 channel 的契约不变；写出失败退出时必须执行既有 `defer unsubscribe()`。
- **建议修改范围：** 生产 `webui/api/sync.go`、`webui/api/logstream.go`（各 3-6 行，含 deadline 再 +3 行）；测试 `webui/api`；无 API 契约/前端/Docker 变化。
- **建议回归测试：** `TestHandleLogStreamExitsOnWriteError`、`TestHandleSyncEventsExitsOnWriteError`（返回错误的 ResponseWriter，断言首个写错误后返回且 `unsubscribe` 被调用；判别性：现状会挂到 shutdown）；若做 deadline，增一条「写出前设置了非零写 deadline」的断言。
- **外部验收需求：** 真实半开连接（拔网线/挂起标签）下 handler 与订阅是否释放，需人工验证。
- **风险和依赖：** 写 deadline 过短会切断正常长连接（本项目无心跳、仅在事件到达时写，风险可控）；deadline 必须在写后清除以免影响同连接后续请求。与 A16 同属 SSE/HTTP 生命周期，建议同批。
- **待用户决策：** 无（是否加写 deadline 属实现取舍；建议先做写错误处理，再评估 deadline）。

#### 5.2.16 A16｜生命周期方法重复调用语义不完整

- **判定：** 确认存在（公共 API 契约不完整；生产接线只调用一次，当前不构成生产故障）。
- **置信度：** 高（`Run` 重复调用 panic 已动态复现；`Start`/`Wait` 为确定性静态结论）。
- **现行源码证据：** `webui/server.go:133-176 Start`（每次 `net.Listen` 并覆盖 `s.listener/s.httpServer`，而 `serveOnce.Do`（`:164-168`）只启动一次 Serve → 第二次的 listener **永不被 Serve 也不被关闭**，`Addr()`（`:107-114`）返回该未服务地址）；`:182-184 Wait`（`<-s.serveDone`，容量 1 且只发送一次 → 第二次永久阻塞）；`syncer/syncer.go:114-115 Run`（`defer close(s.doneCh)` → 第二次调用返回时**重复 close 触发 panic**）；`Stop`（`:307-309`）与 `Shutdown`（`server.go:210-235`）本身幂等。
- **完整调用链（生产，均为单次）：** `run.go:120 srv.Start()` → `:134 go s.Run()` → `:137-138 go func(){ serveErrCh <- srv.Wait() }()` → 收尾 `:162 srv.Shutdown(ctx)` → `:175 s.Stop()` → `:185 s.Wait()`。
- **生产可达性：** 生产路径当前不触发；仅后续改动/测试/被当库使用时触发。
- **触发条件：** 同一 `*webui.Server` 调用 `Start()` 两次；同一 `*Syncer` 调用 `Run()` 两次；`Server.Wait()` 两次。
- **实际影响：** 遗留未服务 listener（端口占用、`Addr()` 失真）；`Run` 重复调用使进程 panic；`Wait` 第二次永久阻塞（若误用会使退出挂死）。
- **已有缓解：** `Stop`/`Shutdown` 幂等；`serveDone` 容量 1；`serveOnce` 防止重复 Serve。
- **现有测试证明了什么：** `webui/server_test.go` 的 `TestStartUsesPreferredPort`、`TestStartFallsBackOnlyAfterEADDRINUSE`、`TestStartDoesNotReleaseBoundListener`、`TestShutdownIdempotent`、`TestShutdownTwiceBeforeStart`、`TestWaitBeforeStartBlocksUntilServeExits`、`TestShutdownAfterServeExitedRepeated`、`TestShutdownNoGoroutineLeak`；`syncer` 的 `TestStopIdempotent`。
- **现有测试没有证明什么：** 没有 `Start` 两次、`Wait` 两次、`Run` 两次的用例，重复调用语义完全未契约化。
- **本轮动态复现：** 部分（`/tmp/fwreview/syncer_repro_test.go::TestReviewRepro_A16RunTwiceClosesDoneChTwice`）：两次 `Run()` + 一次 `Stop()` → `panic 次数 = 1`。`Start` 两次的未服务 listener 与 `Wait` 两次阻塞未动态复现（静态结论已无歧义）。
- **与 §二 原描述的差异：** 结论准确；`server.go:133-168` → `:133-176`，`:182-184` 未漂移，`syncer.go:119-120` → `:114-115`。需补 `Stop`/`Shutdown` 已幂等，问题集中在 `Start`/`Wait`/`Run`。
- **修复方案（推荐）：** 明确并固化「只能调用一次」（符合「不过度防御」）：`Start` 在 `s.mu` 内先判断 `s.listener != nil` 则返回明确错误且不新建 listener；`Wait` 支持多次调用（记录已接收结果，或 `sync.Once` + 保存结果）；`Syncer.Run` 用 `runOnce sync.Once` 包裹主体，使第二次调用立即返回（建议带 `slog.Warn` 以免掩盖接线错误）。三处补注释说明契约。
- **备选方案：** (a) 让 `Run` 接受 `context` 并允许重复调用（改动扩散到 `Stop/Wait`，面较大）；(b) 只补注释（成本 0 但 panic 风险仍在，不推荐）。
- **必须保持的不变量：** `Stop`/`Shutdown` 幂等；`Wait` 在 `Stop` 后能返回；`Shutdown` 先关 SSE channel 再 `http.Server.Shutdown`、超时 `Close`；`serveDone` 不阻塞 Serve goroutine；不改 `Start` 的端口语义（仅 `EADDRINUSE` 降级）。
- **建议修改范围：** 生产 `webui/server.go`（`Start` 守卫 + `Wait` 多次安全）、`syncer/syncer.go`（`runOnce`）；测试 `webui/server_test.go`、`syncer/state_test.go`；其余无变化。
- **建议回归测试：** `TestStartTwiceReturnsErrorAndKeepsFirstListener`、`TestWaitTwiceReturnsSameResult`、`TestRunTwiceDoesNotPanic`（判别性：当前实现下第二条 panic/阻塞）；`-race`。
- **外部验收需求：** 无。
- **风险和依赖：** `runOnce` 使第二次 `Run` 静默返回可能掩盖接线错误（故建议 WARN）；与 A6/A20 同属调度/生命周期语义，建议同批。
- **待用户决策：** 已决（A20 并入同批；`Start`/`Wait`/`Run` 契约本身无争议）。

#### 5.2.17 A17｜迁移失败只告警后继续

- **判定：** 确认存在（严重度低；§二 已纠正原报告「永久无用」的错误说法）。
- **置信度：** 中（代码事实与错误文本已动态核实；真实旧库上非预期迁移失败的具体后果未复现）。
- **现行源码证据：** `config/store.go:92-161 initTables`：先执行整段 `CREATE TABLE IF NOT EXISTS`（`:149-152`，失败即返回错误），随后两条迁移 `:154-156`（`ALTER TABLE rules ADD COLUMN enable_ipv6 …`）与 `:157-159`（`ALTER TABLE alert_webhook ADD COLUMN channel …`）；两条都只在 `err != nil && !strings.Contains(err.Error(), "duplicate column")` 时 `slog.Warn(…)`，随后 `:160 return nil`（**启动继续**）。
- **完整调用链：** `run.go:60 OpenStore → initTables`（迁移失败仅 WARN）→ `run.go:72 LoadBusinessSnapshot → LoadBusinessSnapshotTx → loadRules`（`SELECT … enable_ipv6 …`）→ 列缺失则查询报错 → `run.go:73-76`「加载配置失败」并以 1 退出。
- **生产可达性：** 生产可达（旧版本数据库升级路径）。
- **触发条件：** 正常旧库缺列（会被 ALTER 补上）；或 ALTER 因非「列已存在」原因失败（数据库锁、磁盘满、只读、权限等）。
- **实际影响：** 迁移失败不在启动时暴露原因，而是延迟到首次读库或运行期查询；根因 WARN 仍会被后续错误淹没。不会损坏数据。
- **已有缓解：** 两条 ALTER 幂等（重复执行命中 duplicate column 被忽略）；`CREATE TABLE IF NOT EXISTS` 对新库直接建全列。
- **现有测试证明了什么：** `config/runtime_test.go:29`（空库默认值）、`config/store_test.go`/`store_tx_test.go`（正常读写）、`main_test.go` 真实进程空库启动。
- **现有测试没有证明什么：** 没有「旧库缺列 → 迁移」用例，也没有「迁移失败 → 启动报错」用例。
- **本轮动态复现：** 部分（`/tmp/fwreview/config_repro_test.go::TestReviewRepro_A17MigrationErrorText`）：重复 ALTER 得到 `SQL logic error: duplicate column name: enable_ipv6 (1)`，`strings.Contains(...,"duplicate column") = true`（识别机制有效，不会误报 WARN）；非预期错误形态示例 `SQL logic error: unrecognized token: "1bad" (1)`（当前只 WARN 后继续）。未复现「旧库缺列 + 真实升级」完整路径。
- **与 §二 原描述的差异：** §二 的边界说明（不能称「永久无用」，两条 ALTER 对旧库仍有作用）正确；`store.go:153-159` → `:153-160`。需补「duplicate column 识别在当前驱动版本下确实有效」。
- **修复方案（推荐）：** 两条 ALTER 改为「duplicate column → 忽略；其他错误 → `return fmt.Errorf(...)`」，使启动在迁移失败时立即失败并打印真实原因。
- **备选方案：** (a) 保持 WARN 但在后续读库失败时提示「可能由迁移失败导致」（根因仍靠推断）；(b) 引入 `schema_migrations` 版本表（对仅 2 条 ALTER 属过度设计，不推荐）。
- **必须保持的不变量：** 新库仍由 `CREATE TABLE IF NOT EXISTS` 建全列；重复迁移幂等；不改名/不重建既有数据库文件；启动失败给出可读原因。
- **建议修改范围：** 生产 `config/store.go`（2 处错误处理，约 6 行）；测试 `config`（旧库夹具 + 失败注入）；无 API/前端/Docker 变化；不改 schema。
- **建议回归测试：** `config/store_migration_test.go`：(a) 裸 SQLite 建「旧 schema」→ `OpenStore` → 断言列补齐且数据保留；(b) 再次 `OpenStore` → 无错无 WARN；(c) 失败注入（使 ALTER 失败）→ 断言 `OpenStore` 返回错误且文本含表名（判别性：现状返回 nil）。
- **外部验收需求：** 用真实旧版本数据库做一次升级验证（本轮未做）。
- **风险和依赖：** 迁移错误变致命会在极少数环境（短暂锁/磁盘满）导致启动失败——但此时后续读库也必然失败，提前失败更好；与 A4/A9/A14 同文件，建议同批不同提交。
- **待用户决策：** 无。

#### 5.2.18 A18｜无界等待放大外部挂起，整轮完成缺少成功失败汇总

- **判定：** 设计/产品决策（**用户已决：新增 `last_success` 与整轮汇总字段，不改 `last_sync`**）；其中无界 `Wait()` 是 AGENTS 强要求，不判为缺陷。
- **置信度：** 高。
- **现行源码证据：** `run.go:177-185`（HTTP 收尾最多等 `webui.ShutdownTimeout`=10s，随后**无超时** `s.Wait()`，注释明确「强要求」）；`syncer/syncer.go:462-473`（`wg.Wait()` 后无条件 `s.lastSync = time.Now()`；`EventSyncComplete.Data` 只有 `duration`）；`:325-342 SyncStatus{Running,Enabled,LastSync}`；`run.go:110-113`（注释写「订阅 sync:complete 和 sync:error」，实际订阅 `EventDomainSyncComplete` 与 `EventSyncError`——注释与代码不符，且整轮汇总目前没有任何落地点）；AGENTS §五。
- **完整调用链：** 云调用（A1）→ `syncAll`（跨云并行、云内串行）→ `wg.Wait()` → 刷新 `lastSync` → `sync:complete` → Run 循环；退出侧：信号 → `Shutdown` + `Stop` → `s.Wait()` → 进程退出。
- **生产可达性：** 生产可达（退出必经）。
- **触发条件：** 任一轮次内的外部调用长时间不返回（A1 的阿里云请求、DNS 有界 10s、告警不影响轮次）。
- **实际影响：** 退出收尾时长不可控；整轮失败时 `last_sync` 仍被刷新，前端「上次同步」看起来正常，失败只在 `sync_logs`/告警体现。
- **已有缓解：** DNS 10s 上限；腾讯 SDK 60s 上限；`sync_logs` 逐域记录；`EventSyncError` 触发告警；跨云并行缩短整轮时长。
- **现有测试证明了什么：** `main_test.go:490 TestProcessCompletesInFlightRoundAfterSignal`（真实二进制 + SQLite：信号后当前轮继续并结束、退出码 0）、`syncer/state_test.go:301 TestStopWaitsForBlockedRound`、`main_test.go:443/471`（信号收尾顺序与退出码）。
- **现有测试没有证明什么：** 没有「外部调用永久挂起 → 进程无法退出」用例（也不应自动构造真实挂起）；没有 `last_success`/整轮汇总断言（字段不存在）。
- **本轮动态复现：** 否（A1 的挂起已在 A1 项复现；本项为语义/字段核对）。
- **与 §二 原描述的差异：** 结论与边界准确；`run.go:184-185` 未漂移，`syncer.go:459-470` → `:464-473`。需补「`run.go:110` 注释与实际订阅不符」「整轮汇总无落地点」。
- **修复方案（按用户决策）：** 在 `SyncStatus` 增加 `last_success`（最近一次「整轮无失败」完成时间，从未成功为 null）与整轮汇总（如 `last_round{finished_at, domains_ok, domains_failed, added, deleted}`）；`syncAll` 累计每域结果（`syncDomainInternal` 目前只发事件，需回传或就地累加），仅 `failed==0` 时更新 `lastSuccess`；`EventSyncComplete.Data` 增加同样汇总；**`last_sync` 语义不变**；前端 Dashboard 展示「最近成功同步」。
- **备选方案：** （已否决）把 `last_sync` 改为「最近成功」；（可选）只在 `sync_logs` 侧聚合而不改 API——可观测性不足，不推荐。
- **必须保持的不变量：** 「完成当前轮次再退出」不变（**不引入轮次超时**）；`last_sync` 语义不变；`duration` 字段保留（向后兼容）；新增字段不泄露敏感信息；暂停期间不产生虚假成功。
- **建议修改范围：** 生产 `syncer/syncer.go`（计数与字段）、`syncer/state.go`（与 A13 的 generation 合并）、可选 `webui/api`（推荐直接扩展 `GET /api/sync/status`）；前端 Dashboard；API 兼容新增字段；SQLite 本批建议不加列；Docker 无变化。
- **建议回归测试：** `syncer`：2 域 1 成功 1 失败 → `last_sync` 前进、`last_success` 不前移、`failed=1`；全成功 → `last_success` 前移；从未成功 → nil；暂停 → 不刷新。`webui/api`：`GET /api/sync/status` 含新字段且旧字段不变；`-race`（跨云并行需 atomic/互斥）。
- **外部验收需求：** 前端 Dashboard 展示需浏览器人工验收。
- **风险和依赖：** 汇总计数跨并行 goroutine，必须 atomic/互斥且**不得**破坏「一轮一快照」；事件 Data 增长需评估 SSE 载荷。依赖 A1（先让外部调用有界，否则 `last_success` 可能永不前进）与 A3/A13（同一状态结构），建议顺序 A1 → A18。
- **待用户决策：** 已决（新增 `last_success` + 整轮汇总，不改 `last_sync`）。

#### 5.2.19 A19｜Windows 支持范围

- **判定：** 设计/产品决策。**用户已决：完全移除 Windows 兼容，不再考虑兼容。**
- **置信度：** 高（文档、源码、交叉编译均已核实）。
- **现行源码证据：** `config/pidfile_windows.go:1`（`//go:build windows`，唯一 `golang.org/x/sys/windows` 使用者）、`config/pidfile_unix.go:1`（`//go:build !windows`）、`config/deployment.go:66-77 DefaultDataDir` 的 `case "windows"`（`:70-74`）、`README.md:139`（Windows 数据目录行）、`run.go:116-117`（`signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)`）、`go.mod:12`（`golang.org/x/sys v0.46.0`）；CI 只构建 `linux/amd64` 镜像。
- **完整调用链：** 启动 `run → LoadDeploymentConfig → DefaultDataDir`（平台分支）→ `WritePidFile → processExists`（平台实现）；退出 `run.go:142-149`。
- **生产可达性：** 仅在 Windows 运行时可达；发布形态是 Docker/服务器（Linux）。
- **触发条件：** Windows 前台运行（Ctrl+C → `os.Interrupt`/SIGINT 可优雅退出）；作为 Windows 服务由 SCM 停止时不投递 SIGTERM/SIGINT，因此服务方式停止不走优雅收尾。
- **实际影响：** 保留 Windows 代码意味着维护未验证的平台路径 + 一个仅为它存在的依赖（`golang.org/x/sys`），并让 README 隐含「支持 Windows」。
- **已有缓解：** 无（也无需）。
- **现有测试证明了什么：** 无 Windows 相关测试；全部测试在 darwin/linux 运行。
- **现有测试没有证明什么：** 无 Windows 运行、信号、pidfile、数据目录验证（仅交叉编译）。
- **本轮动态复现：** 仅编译级：`GOOS=windows GOARCH=amd64 go vet ./...` 退出码 0；`GOOS=windows GOARCH=amd64 go build -o /tmp/fwalizer-win-review.exe .` 成功（说明当前代码**可编译**，不能据此说「Windows 无法退出」）。未在 Windows 上运行。
- **与 §二 原描述的差异：** §二 的边界说明正确；`run.go:117`、README 行号未漂移。需补：`golang.org/x/sys` 仅由该文件引入（非 Windows 目标下 `go mod why` 报「main module does not need package」），移除 Windows 支持可连带移除该直接依赖。
- **修复方案（按用户决策：完全移除）：** 删除 `config/pidfile_windows.go`；去掉 `config/pidfile_unix.go` 的 `//go:build !windows`（可重命名为 `pidfile_proc.go`）；删除 `config/deployment.go` 的 `case "windows"`（`default` 注释明确「仅支持 Linux/macOS」）；删除 `README.md:139` 并在数据目录段说明支持平台；从 `go.mod` 移除 `golang.org/x/sys` 并更新 `go.sum`（若被间接依赖则保留为 indirect）；检查 AGENTS 是否有平台承诺（当前 §八/§十 只提 Docker/linux-amd64，无需改）。
- **备选方案：** （用户已否决）尽力兼容或正式支持 Windows 服务。
- **必须保持的不变量：** 非 Windows 平台的数据目录与 pidfile 行为完全不变（macOS/Linux 路径、`FWALIZER_DATA_DIR` 优先）；单实例防护有效；Docker/CI 平台不变（`linux/amd64`）；不引入新的平台依赖。
- **建议修改范围：** 生产 `config/pidfile_windows.go`（删）、`config/pidfile_unix.go`（去 tag/改名）、`config/deployment.go`（删分支）；依赖 `go.mod`/`go.sum`（删 `x/sys`，需单独提交并 `go mod tidy`）；文档 `README.md`；测试无影响；Docker/前端无变化。
- **建议回归测试：** `config/deployment_test.go` 保持 macOS/Linux 期望值断言；CI 增加 `GOOS=linux GOARCH=amd64 go build`（可选 `darwin`）；**不**再为 Windows 保留编译门禁。
- **外部验收需求：** 无（移除支持后无需 Windows 验证）。
- **风险和依赖：** 若有 Windows 用户，升级后无法运行——需在 README/发布说明明确「不再支持 Windows」；`go.mod` 变更会触发 CI 的 `go get -u`（§十 SDK 策略），需确认 `go mod tidy` 后 `x/sys` 是否仍作为 indirect 存在（若存在，删除直接依赖即可）。与 A8 的文档改动同批。
- **待用户决策：** 已决（完全移除 Windows 兼容，不再考虑）。

#### 5.2.20 A20｜`Stop()` 之后仍可能启动一整轮同步（本轮新增）

- **判定：** 确认存在（Issue6 原清单未涵盖，本轮新增并已隔离复现；经用户确认并入 A6/A16/A18 同批修复）。
- **置信度：** 高（动态复现 15 次中 5 次；`select` 多就绪分支随机选择的语义由 Go 语言规范决定）。
- **现行源码证据：** `syncer/syncer.go:157-184`：启用态的 `select` 同时包含 `<-ticker.C`、`<-s.triggerCh`、`<-s.controlCh`、`<-s.stopCh`；两个守卫（`:164`、`:174`）只检查 `IsEnabled()`（暂停），**不检查是否已 Stop**；`Stop`（`:307-309`）只 `close(s.stopCh)`；`TriggerSync`（`:315-320`）在关闭前仍可入队；`run.go:175 s.Stop()` 之后 `:185 s.Wait()` 无超时。
- **完整调用链：** `POST /api/sync/trigger`（`webui/api/sync.go:23-34`，只检查 `Status().Enabled`）→ `TriggerSync` 入队 → 收尾路径 `run.go:142-185`（先启动 HTTP shutdown，再 `s.Stop()`，最后 `s.Wait()`）→ Run 在当前轮结束后进入 `select` → 若选中 `triggerCh` 分支且 `IsEnabled()` 仍为 true，则**启动新一轮**。
- **生产可达性：** 生产可达（停止瞬间在途的 trigger 请求，或 ticker 已到期）。
- **触发条件：** 一轮同步进行中，用户/前端恰好发起手动同步（或 ticker 已到期），紧接着进程收到 SIGTERM。
- **实际影响：** 进程额外执行一整轮真实云写入（可能数十秒到数分钟），`s.Wait()` 无超时等待它；日志出现「同步引擎停止」之前的一轮新同步；与 AGENTS §五「完成当前轮次再退出」的意图相悖。
- **已有缓解：** `Stop` 幂等；`TestStopWaitsForBlockedRound` 覆盖「单轮阻塞时 Stop 后不新增轮次」（该用例 interval=1h 且无排队 trigger，故未覆盖本竞态）；`TestNoNewRoundAfterStop` 覆盖的是 Run 已退出后的 ApplyState。
- **现有测试证明了什么：** ticker 守卫、过期 trigger 丢弃、暂停期 trigger 不执行、Stop 等待当前轮（本轮 `-race` 全部 PASS）。
- **现有测试没有证明什么：** 没有用例同时置位「已关闭 stopCh」与「就绪 triggerCh」，也没有断言「Stop 之后不得再调用 Provider」。
- **本轮动态复现：** 是（`/tmp/fwreview/syncer_repro_test.go::TestReviewRepro_A6StopGateAllowsQueuedTriggerRound`）：复用仓库既有 helper，15 次迭代中 **5 次**在 `Stop()` 之后仍启动新一轮（日志可见「手动触发同步 → 开始同步」出现在「同步引擎停止」之后）。
- **与 §二 原描述的差异：** 原 A6 只覆盖「暂停后的 ticker/trigger」，未覆盖 `Stop`；本条为新增。修 A6 的守卫不能替代本条（两者判定条件不同：一个查已发布开关，一个查停止信号）。
- **修复方案（推荐）：** 把 `stopCh` 变成硬门控：在 `Run` 的 `for` 循环起始处做一次非阻塞检查（`select { case <-s.stopCh: slog.Info("同步引擎停止"); return; default: }`），或在进入 `syncAll()` 前统一调用同一守卫（`if s.stopped() { return }`）；`ticker`/`trigger` 两个分支改为「先查停、再查 enabled」。**不得**中断正在执行的 `syncAll`。
- **备选方案：** (a) 在 `select` 内用嵌套 select 优先判断 `stopCh`（改动更小但可读性差）；(b) `Stop` 时同时清空 `triggerCh`（只解决排队 trigger，已到期 ticker 仍需守卫）。
- **必须保持的不变量：** 当前轮次必须完成；`Stop` 幂等、`Wait` 可多次安全等待；暂停子循环不消费 ticker/trigger；`Stop` 不产生 panic 或永久阻塞；不引入超时中断正在执行的云 API 调用。
- **建议修改范围：** 生产 `syncer/syncer.go`（约 5-10 行，建议与 A6 的守卫共用函数）；测试 `syncer/state_test.go`；无 API/SQLite/前端/Docker 变化。
- **建议回归测试：** `syncer/state_test.go::TestStopRejectsQueuedTriggerRound`：blockAll provider + 1h interval → 等首轮阻塞 → `TriggerSync()` + `Stop()` → 放行 → 断言 `GetRules` 调用次数恒为 1（判别性：去掉守卫时约 50% 失败，建议 `-count=20` 提高判别力）；保留 `TestStopWaitsForBlockedRound`；`-race`。
- **外部验收需求：** 真实进程 SIGTERM + 并发 trigger 的人工验证（可扩展 `main_test.go` 为进程级用例，时序需控制）。
- **风险和依赖：** 门控位置写错可能中断当前轮次（必须只挡新轮次）；与 A16 的生命周期契约共用语义定义，与 A18 的无超时等待直接相关（多一轮 = 多等一轮）。与 A6/A16 同批。
- **待用户决策：** 已决（纳入同批修复）。

### 5.3 推荐修复批次

按依赖与可独立审查性拆分，每批一个提交、带专项回归；**不要**合并成一次大重构。

| 批次 | 内容 | 关键点与顺序约束 | 判别性回归（建议） |
|---|---|---|---|
| **批次 1** | **A1 + A12**：阿里云 4 处构造点补 `ConnectTimeout/ReadTimeout`；`isRetryable` 改结构化超时判定 | **A1 先行或同批**：A1 修好后超时成为常态错误，若 A12 未修则不会重试 | 本地阻塞端点（provider）+ `TestIsRetryableErrorShapes` + 端到端重试计数 2 次 |
| **批次 2** | **A2**：先做 SMTP 完整会话 deadline（独立低风险），再做在途上限 + 满载丢弃 WARN | 开工前确认满载语义（见 §5.7 待确认项） | 本地静默 SMTP 会话超时；阻塞订阅者 + N 次发布的在途计数 |
| **批次 3** | **A20 + A6 + A16**：Run 的 `stopCh` 硬门控 + `runOnce`；`Server.Start/Wait` 契约化；A7 注释收口 | 门控只挡新轮次，不中断当前轮；与 A6 守卫共用函数 | `TestStopRejectsQueuedTriggerRound`（`-count=20`）、`TestRunTwiceDoesNotPanic`、`TestStartTwice…`、`TestWaitTwice…` |
| **批次 4** | **A3 + A18 + A13 + A15（写错误部分）**：`Generation` + `last_success`/整轮汇总 + `SyncStatus` 扩展 + Dashboard；hook 仅在真实状态变更时触发；SSE 处理写错误 | **依赖批次 1**（否则 `last_success` 可能永不前进）；`/api/health` 保持不变 | `TestStateAppliedHookOnlyOnRealStateChange`、同步健康字段用例、`TestHandleSyncEventsExitsOnWriteError` |
| **批次 5** | **A4 + A9 + A14 + A17**：DSN `_pragma`；`loadRules` 损坏即报错；`WithTransaction`/`writeJSON`/`AddSyncLog` error 处理；`initTables` 迁移失败即返回错误 | 同文件但语义独立，可分 4 个提交；A9 的错误语义变化需写进发布说明 | `store_busy_test.go`（4 连接 PRAGMA + 竞争）、损坏 targets 四态、panic/回滚错误路径、旧库迁移夹具 |
| **批次 6** | **A11**：可选接口 `CreateRulesCounted` + SWAS 实现 + `retrySync`/事件/日志改造 | 建议在批次 4 之后，使汇总字段一次设计到位 | SWAS 契约 fake provider：`added=0, skipped=N`；混合场景 `added=1, skipped=1` |
| **批次 7** | **A19 + A8（文档）**：删 Windows 代码与 `golang.org/x/sys`、README 平台说明；`AGENTS.md:180` 改「确定重置」 | 涉及依赖与唯一强要求文档，独立提交 | `go mod tidy` 后 `go vet ./...`；README 无 Windows 表述 |
| **批次 8** | **清理**：从 `webui/api` 的 `Syncer` 接口移除无用的 `Pause/Resume`；评估 SSE 写 deadline | 最后做，避免与批次 3/4 冲突 | 现有判别性用例保持通过 |

**明确不要合并：** A1（SDK 参数）与 A2（告警）；A4（DSN）与 A9（解析语义）虽同文件但语义无关；A19（删代码/依赖）不得与任何运行时行为修复同提交。

### 5.4 优先级复核

| 编号 | §一 原优先级 | 本轮建议 | 理由 |
|---|---|---|---|
| A1 | 高 | **保持高（建议第 1）** | 唯一「无界外部调用」，且放大 A18/退出收尾/A20；已动态复现 |
| A2 | 高 | **保持高** | SMTP 无 deadline 与无并发上界两项均确认；资源耗尽仍是推论，故不升级 |
| A3 | 中 | 保持中 | 已确认为契约/可观测性问题而非实现错误；用户已定方案 |
| A4 | 中高 | **保持中高** | 动态确认新连接立即 `SQLITE_BUSY`；并发写路径真实存在（`AddSyncLog` 不在协调器锁内） |
| A5 | 中高 | **降为已关闭**（残留接口清理归低） | 生产已无双写入口且有判别性用例 |
| A6 | 中 | 原描述**关闭**；**A20 升为中** | A20 直接违背退出契约意图，复现 5/15 |
| A7 | 中 | **降为已关闭** | 已用确定性屏障用例覆盖 |
| A8 | 中 | **降为已关闭**（仅剩 AGENTS 措辞） | 端点级判别用例通过，语义已由用户固定 |
| A9 | 中低 | **升为中** | 后果为「下发范围扩大 + 409 漏判 + 导出/导入固化」，属安全方向问题，修复成本低 |
| A10 | 中低 | **降为已关闭** | 已有判别性用例 |
| A11 | 中低 | **保持中低** | 误导性成功计数，但不影响云端真实状态 |
| A12 | 中低 | **升为中** | A1 修好后超时为常态失败模式，而最常见形态当前不被重试；已有真实错误复现 |
| A13 | 低 | 保持低 | 纯日志噪声 |
| A14 | 低 | 保持低（属强要求违规，建议一并修） | 触发条件苛刻，实际业务影响小 |
| A15 | 低 | 保持低（写错误处理建议随批做） | 写错误忽略已复现；半开连接阻塞仍为静态风险 |
| A16 | 低 | 保持低（A20 已单列为中） | 生产单次调用，但 `Run` 重复调用会 panic |
| A17 | 低 | 保持低 | 仅 2 条迁移，识别机制有效；提前失败更利于排障 |
| A18 | 低 | **升为中** | 与 A3 配套的同步健康语义，用户已决定新增字段 |
| A19 | 低 | 保持低（决策已定：移除） | 实施简单但涉及依赖与文档，需独立提交 |
| **A20** | （新增） | **中** | 额外一整轮真实云写入 + 无超时等待，已动态复现 |

### 5.5 文档准确性问题（本次复核发现的过时点，已在本文顶部与本表登记）

1. **基线过时：** §首「核验基线 2026-09-24 / HEAD `5594532` / 工作区有 8 个文件未提交」是历史事实，但其中的「未提交改动」早已全部提交；本次复核的 HEAD 为 `54efb10`、工作树干净、`main` 相对 `origin/main` **ahead 1**（不是 ahead 3）。
2. **§三「审查快照过时」整段过时：** 其中「`Build6.md` 等进度文档已有他人未提交改动」「待相关工作完成后核对 Step 5/6 证据边界与远端 CI」等表述已被后续提交取代——Step 5/6/7 均已验收通过，O5-02（远端 CI 与镜像推送）已关闭，PT-B6-08/09 记为人工验收免除。
3. **行号漂移（部分条目）：** A2 `syncer.go:488-492`→`:493-497`；A3 `syncer.go:317-336`→`:325-342`；A6 `syncer.go:163-164`→`:159-168`、`state_test.go:107-115`→`:111-124`；A7 `syncer.go:93-100/:150/:190-203`→`:89-100/:146/:196-215`；A8 `deps.go:86-101`→`:88-104`；A9 `syncer.go:550-565`→`:555-570`；A10 `settings.go:173-181`→`:173-190`、`settings_alerts_test.go:266-281`→`:267-336`；A11 `ali_swas.go:118-135`→`:117-135`；A13 `syncer.go:214`→`:218`；A14 `deps.go:196-200`→`:197-202`；A15 `sync.go:127-128`→`:126-127`；A16 `server.go:133-168`→`:133-176`、`syncer.go:119-120`→`:114-115`；A17 `store.go:153-159`→`:153-160`；A18 `syncer.go:459-470`→`:464-473`。未漂移：A1、A4 主行、A5、A12、A15（`logstream`）、A19。
4. **需要补充/修正的事实（逐项）：** A1 补「TLS 握手超时同样为 0、请求无 context、SDK 自带重试默认仅 1 次、腾讯客户端默认 60s 有界」；A2 补「`Publish` 不阻塞、同步轮次不等待告警、进程退出不等待在途告警」；A4 补「驱动支持 `_pragma` 且按连接执行、旧式 `_busy_timeout=` 不存在、错误文本为 `database is locked (5) (SQLITE_BUSY)`」；A9 补「`'null'` 与损坏等价、导出/导入会把损坏行固化为 `[]`」；A11 补「Dry Run 的 `to_add` 同样列出被跳过的 DROP」；A13 补「该日志同时进入 WebUI 日志流」；A15 补「`ResponseController.SetWriteDeadline` 在 HTTP/1 可用」；A16 补「`Stop`/`Shutdown` 已幂等，问题集中在 `Start`/`Wait`/`Run`」；A18 补「`run.go:110` 注释与实际订阅不符、整轮汇总目前无落地点」；A19 补「`golang.org/x/sys` 仅由 `pidfile_windows.go` 引入」。
5. **文档间冲突（已裁决）：** `AGENTS.md:180`「完整导入**允许**新建并清空」 vs `Build6.md` §12.3 第 7 条与源码「**确定**重置」。用户决定后续把 AGENTS 改为「确定重置」（本文件不修改 AGENTS）。
6. **§三 的清理候选**（`Store.LoadConfig`/`DeleteRule`/`UpdateTarget`/`UpdateRule`/`GetAlertEmailConfigTx`/`GetAlertWebhookConfigTx`/`SettingsKeysV2`/`EventRuleChanged`/`SyncDomainResult`/`ResolvedIPs`、`presenceSlice.MarshalJSON`、`toInt` 的 `float64` 分支、`.dockerignore` 中 `.env` 与根目录 `Dockerfile` 条目）本次**未逐项判定**；其中 `.dockerignore` 的 `Dockerfile` 条目与实际构建（`build/Dockerfile`）确实不一致，属低优先级清理。

### 5.6 未验证边界（本次复核明确未执行）

1. **真实云 API：** 未调用腾讯云/阿里云任何接口；A1 的「无超时」只在 localhost 阻塞端点复现；真实云网络挂起、限流错误文本、批量写入部分失败语义均未验证。
2. **真实 SMTP/收件箱：** 未连接任何 SMTP、未发信；A2 仅在本地静默 TCP 服务复现。PT-B6-08 仍无真实通过结论。
3. **真实 Webhook：** 未向钉钉/飞书/Slack 发任何请求（仅源码 + 标准库语义；10s 客户端超时为源码结论）。
4. **Windows 运行时：** 未在 Windows 运行、未验证 Ctrl+C/服务停止、pidfile 与数据目录；仅 `GOOS=windows` 的 vet/build 通过。
5. **Docker/Compose：** 未构建镜像、未运行容器、未复验 HEALTHCHECK 与 `docker stop`。
6. **浏览器/前端：** 未做浏览器回归；`/api/sync/status`、Dashboard、SSE 前端消费与告警页面均未人工验证。
7. **生产数据库：** 未访问任何真实 SQLite 文件；A4/A9/A17 均使用 `t.TempDir()` 新建的虚构数据库；**未验证**真实旧版本数据库的迁移路径。
8. **远端 CI / GitHub Actions：** 未触发任何工作流；`v2.0.0 → run 36300428681` 属文档记录，本次未复核。
9. **资源耗尽/长跑：** 未做长跑观察告警 goroutine 与句柄累积（A2 的「已发生资源耗尽」仍是推论）。
10. **半开 TCP 连接：** 未构造阻塞于 `Flush` 的真实半开连接（A15 第二部分）。
11. **并发压测：** 未对协调器/导入/暂停恢复做随机压力测试（现有结构性保证 + 判别性用例已覆盖主要交错）。

### 5.7 用户决策记录（2026-09-27，本轮提问后确认）

| 项 | 决策 | 影响 |
|---|---|---|
| A3 | **保持 HTTP-only health，另加同步健康可观测** | `/api/health`、Docker HEALTHCHECK、容器重启语义不变；同步健康以 `/api/sync/status` 字段 + 前端提示承载 |
| A6/A20 | **A20 纳入修复范围，与 A6/A16/A18 同批** | 批次 3 增加 Run 的 `stopCh` 硬门控与判别性用例 |
| A8 | **后续把 `AGENTS.md` 措辞改为「确定重置」** | 消除「允许 vs 必须」歧义；本文件不改 AGENTS |
| A9 | **非空但非法的 `rules.targets` 即返回明确错误**，阻止同步/导出/409 检查继续 | 库损坏时改为显式失败（500 + 安全文案）而不是静默扩大目标范围 |
| A11 | **SWAS Provider 返回实际写入数/跳过数**，事件与日志区分 | 保留「允许保存 DROP」与 README 说明；`added` 反映真实写入，跳过量单独可见 |
| A18 | **新增 `last_success` 与整轮汇总字段，不改 `last_sync`** | 保持现有 `last_sync` 语义与前端兼容；`/api/sync/status` 兼容性新增字段 |
| A19 | **完全移除 Windows 兼容，不再考虑兼容** | 删除 `pidfile_windows.go`、`deployment.go` 的 windows 分支、README Windows 行，并从 `go.mod` 移除 `golang.org/x/sys`（需独立提交 + `go mod tidy`） |
| A2（待确认） | **满载语义未定**（推荐：固定上限 + 丢弃最新 + 每条 WARN 一次，不引入队列与重试） | 批次 2 开工前需确认 |

### 5.8 本次实际执行的命令与隔离复现清单

```
# 基线（开始与结束各一次，结果一致）
git status --short --branch / git rev-parse HEAD / git log -8 --oneline --decorate
git diff --name-only / git diff --check
git branch -vv / git rev-list --left-right --count origin/main...HEAD
git log --oneline 5594532..HEAD / git diff --stat 5594532..HEAD

# 仓库既有门禁（只读执行，未改动任何文件）
go test ./notifier -race -count=1   → ok 2.147s
go test ./syncer   -race -count=1   → ok 15.290s
go test ./config   -race -count=1   → ok 1.739s
go test ./provider -race -count=1   → ok 1.344s
go test ./webui    -race -count=1   → ok 2.276s
go test ./webui/api -race -count=1  → ok 5.562s
go test .          -race -count=1   → ok 8.514s（真实二进制进程级用例）
go vet ./...                        → 退出码 0
GOOS=windows GOARCH=amd64 go vet ./...              → 退出码 0
GOOS=windows GOARCH=amd64 go build -o /tmp/fwalizer-win-review.exe .  → 成功

# 判别性专项（-race -count=1 -v，全部 PASS）
TestHandleSyncPauseResume / TestPauseResumeThroughCoordinator / TestConfigResetStrictBody /
TestConfigImportResetsDNSBreakerOrdinaryChangePreserves / TestResumeImmediateRoundAfterPhaseMirrorAdvance /
TestPausedPublishedStateDropsTickerRound / TestStaleTriggerAfterPauseDoesNotAddRound /
TestQueuedTriggerNotRunWhilePaused / TestStopWaitsForBlockedRound /
TestHTTPRoutesRegression / TestServerTimeoutContract

# /tmp 隔离复现（go test -overlay=/tmp/fwreview/overlay.json；虚构数据 + localhost 假服务 + t.TempDir() 临时库）
A1  调用在 5s 内未返回（无有限请求超时）
A2  SMTP 会话在 5s 内未返回；发布 50 事件耗时 217µs、goroutine 3 → 52
A4  连接[0] busy_timeout=5000 / 连接[1..3]=0；无 PRAGMA 连接 15µs 即 database is locked (5) (SQLITE_BUSY)；有 PRAGMA 连接 5.057s
A6/A20  Stop 之后仍启动新一轮 5/15
A9  '[1,' → Targets=nil 且 err=nil；ReferencingRuleIDsTx(1) 不再命中该行；快照/导出同样不报错
A11 SWAS 契约下 added=1、实际写入=0、err=nil
A12 真实 Client.Timeout 文案 → isRetryable=false；errors.Is(DeadlineExceeded)=true；net.Error.Timeout()=true
A13 无配置变更时 hook 调用次数=3（轮次=4）
A15 每次 Write 均失败仍继续 Flush 且不退出（写入 3 次 / Flush 4 次）
A16 Run 重复调用 panic 次数 = 1
A17 duplicate column 错误文本可被正确识别；非预期错误当前只 WARN
```

**证据分层提醒：** 上表所有 `/tmp` 复现均为隔离复现，**不等于**真实云 API、真实 SMTP/Webhook 或生产事故；`go test -race` 全绿也**不**代表 A1/A2/A4/A9/A11/A12/A13/A15/A16/A17/A20 已被覆盖（各项「现有测试没有证明什么」已逐条说明）。

### 5.9 下一步建议（不开始实施）

1. **先确认 A2 的满载语义**（§5.7 唯一待确认项），其余 7 项决策已确认。
2. **实施顺序：** 批次 1 → 批次 3 → 批次 2 → 批次 4 → 批次 5 → 批次 6 → 批次 7 → 批次 8；每批独立提交、独立回归、可独立回滚；批次 1 与批次 4 有硬依赖，顺序不可颠倒。
3. **授权边界建议：** 每批仅授权该批列出的源码文件 + 对应测试 + 该批明确列出的文档行；`go.mod`/`go.sum` 仅在批次 7 授权；`AGENTS.md` 仅在批次 7 授权且只改 §9.1 一行。
4. **文档回写：** 各批完成后在本节对应条目追加「实施记录」（实际改动、判别性回归、`-race` 结果、证据边界），并同步 `Build6.md`/`Issue5.md` 的口径；本轮只写入本文件。
