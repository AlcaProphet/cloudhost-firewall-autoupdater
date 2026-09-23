# Issue5.md — FWAlizer 问题追踪（当前）

> **文档定位：** 本文档是 FWAlizer 的当前问题记录（非强制，经验参考），记录 2026-09-22 只读审查发现的 R5-01～R5-03、O5-01～O5-06 和 A5-01。已确认的修复口径由 [Build6.md](./Build6.md) 分步实施，未经对应 Step 验收不得标记为已修复。
> 编码指令以 [AGENTS.md](./AGENTS.md) 为唯一强要求；设计记录见 [Design5.md](./Design5.md)；上一阶段的 Design4、Build5 和 Issue4 已原文移入 [HistoryDocs/](./HistoryDocs/)。

---

## 一、审查结论与建议顺序

本次审查确认 3 个应优先修复的实现问题，另记录依赖、CI、测试、HTTP 生命周期、输入边界和 `.env` Headless 架构收敛。用户已确认 Build6 口径，实施顺序固定为：

1. Step 1：修复 EventBus 并发取消和 TAG 快照，并启用 CI race detector；
2. Step 2：移除全部 CLI 和 `.env` Headless 业务模式；
3. Step 3：完成 HTTP listener、Server 生命周期和优雅关闭；
4. Step 4：建立 API 最小持久化校验边界；
5. Step 5：实施 version 2 完整配置包、ID 映射和原子运行时切换；
6. Step 6：分阶段修复前端依赖并升级 Vite 8；
7. Step 7：补齐高影响路径测试、真实验收和文档闭环。

上述顺序已纳入 Build6，但 Step 1-7 仍必须逐步获得用户授权；Step 0 的文档固定不代表代码已修复。

---

## 二、待修复问题

### R5-01 配置导入后限定目标的规则可能失去关联

- **优先级：** 高
- **现象：** 配置导入接口可以返回成功，目标和规则也都能显示，但原本限定到特定目标的规则可能无法匹配导入后的目标，并在同步时被静默跳过。
- **根因：** 导出数据中的 `targets[].id` 与 `rules[].targets` 使用数据库 ID 建立关联；导入时先删除旧数据，再用不含 ID 的普通 `INSERT` 创建目标，但规则仍原样保存导出文件中的旧目标 ID。SQLite `DELETE` 不会重置 `AUTOINCREMENT`，因此在同一数据库执行导出再导入时，新目标 ID 通常与旧 ID 不同。
- **代码证据：** `webui/api/settings.go` 导出目标和规则后，在导入时依次调用 `BatchAddTargetsTx` 与 `BatchAddRulesTx`；`config/store.go` 的 `AddTargetTx` 忽略 `TargetConfig.ID`，而 `AddRuleTx` 原样序列化 `DomainRule.Targets`。
- **影响范围：** 配置备份恢复、同实例导出再导入、不同实例间迁移；仅影响 `targets` 非空、明确限定目标的规则，适用于全部目标的空列表规则不受该关联问题影响。
- **推荐修复方向：** 导入目标时记录“导出 ID → 新数据库 ID”映射，并在写入规则前重写 `rules[].targets`。不建议依赖清理 `sqlite_sequence`，因为显式映射也能正确支持来自其他实例的配置文件。
- **具体修复内容：**
  1. 在 `config/store.go` 增加一个事务内插入目标并返回新 ID 的方法，例如 `AddTargetTxReturningID(tx, target) (int, error)`；使用 `sql.Result.LastInsertId()` 获取数据库实际分配的 ID，不修改普通目标新增接口的既有语义。
  2. `webui/api/settings.go` 的导入事务在清空旧配置后，逐个写入 `imp.Targets`，建立 `map[int]int` 类型的“导出 ID → 新 ID”映射。映射使用导出文件中的 `TargetConfig.ID` 为键，数据库新插入 ID 为值。
  3. 写入规则前创建规则副本；`targets` 为空时保持“适用于全部目标”的语义不变，非空时逐项按映射替换，不直接修改原始请求对象。
  4. 任一规则引用不存在于本次导入目标列表中的旧 ID 时，整个导入失败并回滚，不允许静默删除引用，也不允许保留无法匹配的新旧混合 ID。
  5. 导入前执行不写库的结构检查：目标导出 ID 必须为正数且不能重复；规则引用必须能在目标集合中解析。此类外部输入错误返回 HTTP 400；数据库执行失败仍返回 HTTP 500。
  6. 按 version 2 完整快照替换凭据、设置和告警；空凭据字段可明确清除旧值。导入保留 `sync_logs`、清空 `scanned_resources`，不重置 `sqlite_sequence`。
  7. 在事务提交前构造候选 Provider / ClientPool / Resolver / Config / Notifier；候选构造失败时回滚，提交后只做无失败分支的运行时原子替换。
- **回归测试设计：**
  - 同一数据库先制造自增历史，再导出、清空并导入，验证规则最终关联的是同一业务目标；
  - 使用来源 ID 与目标数据库当前 ID 完全不同的导入文件，验证跨实例导入语义；
  - 覆盖一条规则引用多个目标、空目标列表、多个规则复用同一目标；
  - 覆盖重复目标 ID、零/负目标 ID、未知目标引用，验证返回 400 且旧配置保持不变；
  - 人为制造目标或规则写入失败，验证事务回滚后目标、规则和设置均保持导入前状态；
  - 验证完整凭据和告警被覆盖，空凭据字段能清除旧值，且响应和日志不泄露敏感值。
- **验收建议：**
  - 在已有自增历史的数据库中创建多个目标和限定目标规则；
  - 导出后重新导入；
  - 验证每条规则关联到与导出前相同的目标，而不是只比较数字 ID；
  - 验证导入事务失败时旧配置不被部分替换；
  - 运行 `go test ./... -race`、`go vet ./...` 与 `git diff --check`。
- **状态：** ☐ 已决策 / 待 Build6 Step 5 实施

### R5-02 EventBus 发布与 SSE 取消订阅并发时可能触发进程 panic

- **优先级：** 高
- **现象：** SSE 客户端断开连接的同时若有同步事件发布，存在偶发 `panic: send on closed channel` 并导致整个进程退出的风险。
- **根因：** `notifier.EventBus.Publish` 在读锁内复制 channel 列表，释放锁后再发送；`SubscribeChan` 返回的取消函数可在两者之间取得写锁、关闭 channel 并将其删除。`Publish` 随后仍可能向复制出的已关闭 channel 发送。非阻塞 `select/default` 不能防止向已关闭 channel 发送时的 panic。
- **代码证据：** `notifier/bus.go` 的 `SubscribeChan` 取消函数关闭 channel；同文件的 `Publish` 在锁外遍历先前复制的 channel 并发送。
- **影响范围：** 同步事件 SSE 与任何基于 `SubscribeChan` 的后续消费者；正常浏览器刷新、关闭日志/事件页面或网络断开均可能触发取消订阅。
- **推荐修复方向：** 优先采用轻量方案：取消订阅时只从 EventBus 的订阅表移除，不关闭事件 channel，由 SSE handler 的请求 context 控制退出。若确需关闭 channel，则需要引入订阅对象，使关闭状态检查与发送处于同一同步边界。
- **具体修复内容：**
  1. 修改 `notifier.EventBus.SubscribeChan` 返回的取消函数：在写锁内检查并删除 `chanSubs[id]`，但不再调用 `close(ch)`。
  2. 保留当前按 ID 删除和“未找到即无操作”的幂等语义；同一个取消函数被重复或并发调用时不能 panic。
  3. SSE handler 继续以 `r.Context().Done()` 作为连接生命周期终点。handler 返回时执行 `defer unsubscribe()`，EventBus 不再持有该 channel，随后由 GC 回收。
  4. `Publish` 仍可在锁内复制订阅快照、锁外非阻塞投递；取消订阅与快照复制并发时，**已经取得快照的一个或多个并发 Publish 都可能继续向仍存活的缓冲 channel 投递**，不承诺"最多一个在途事件"（并发 Publish 各自独立取得快照，无法正确保证数量上限）；只保证不 panic、不阻塞、取消后新的快照不再包含该订阅。
  5. 不使用 `recover` 吞掉 `send on closed channel`，也不把发送操作全部放进全局锁内，避免以隐藏错误或扩大锁持有时间的方式修复。
  6. 本项只修改 `EventBus`。`LogBroadcaster` 当前在同一把锁内完成发布和关闭，不存在相同的“锁外向已关闭快照发送”窗口，不顺手改变其生命周期语义。
- **回归测试设计：**
  - 循环并发执行 `Publish` 和 `unsubscribe`，覆盖取消发生在复制前、复制后、发送前等时序；
  - 对同一取消函数进行重复及并发调用，验证幂等且无 panic；
  - 取消后再次发布，验证该订阅者不再收到后续事件；**不得断言"最多一个在途事件"**（并发 Publish 可各自取得快照）；
  - 建立真实 SSE 请求并取消 context，验证 handler 退出且订阅表恢复到连接前数量；
  - 对 `notifier` 相关测试使用 `go test -race -count=100` 重复运行，并最终执行全仓 race 测试。
- **验收建议：**
  - 增加并发循环测试，同时执行 `Publish` 与取消订阅；
  - 覆盖重复取消的幂等性；
  - 使用 `go test ./... -race` 重复运行；
  - 人工验证 SSE 连接断开后不会泄漏订阅或 goroutine。
- **实施记录（2026-09-23，Build6 Step 1）：**
  - 代码：`notifier/bus.go` 取消订阅只删除订阅表记录、不再 `close(ch)`，取消函数用 `sync.Once` 幂等；`Publish` 在读锁内复制接口订阅者 slice 与 channel 订阅快照，锁外异步回调 + 非阻塞投递。
  - 修复前红灯（真实结果）：新测试 `TestEventBus_InFlightSnapshotStillDelivers` 直接 `panic: send on closed channel`；`TestEventBus_CancelDoesNotCloseChannel` 失败；接口订阅 slice 并发用例如实报出 2 处 `DATA RACE`（`Subscribe`/`Unsubscribe` 原地写 vs `Publish` 读），并发 Publish/取消用例报出 `close` vs `chansend` 竞态。
  - 修复后：`go test ./... -race -count=1` 通过；`go test -race -count=100 ./notifier` 通过（`ok 45.358s`）。
  - 新增 SSE 用例（`webui/api/sync_test.go`）锁定"request context 取消 / 真实连接断开后 handler 退出且 `defer unsubscribe()` 生效"契约；订阅表真实清理由 `notifier` 包内白盒断言覆盖。两用例在修复前即通过，属契约保护而非缺陷复现。
  - 门禁（2026-09-23）：`go test -race -count=100 ./notifier` 通过（`ok 45.358s`）；`go test ./... -race -count=1` 通过；`go vet ./...`、`git diff --check` 通过。
- **状态：** ✅ 已修复并验收通过（Build6 Step 1 四道门禁真实通过）

### R5-03 同步配置快照未覆盖 retrySync 的 TAG 读取

- **优先级：** 中高
- **现象：** 同步进行期间通过 WebUI 热重载 TAG 时，同一轮同步可能在筛选旧规则和生成新规则描述时使用不同配置；也存在未受同一锁保护的并发读取风险。
- **根因：** `syncAll` 在锁内取得 `cfg`、providers 和 resolver 快照，但调用链进入 `retrySync` 后又直接读取 `s.cfg.Tag`，绕过了本轮配置快照。
- **代码证据：** `syncer/syncer.go` 的 `syncAll` 创建快照并将 resolver 向下传递；`syncer/retry.go` 的 `retrySync` 使用 `s.cfg.Tag` 执行 `OwnedRules` 和 `tag.Format`。
- **影响范围：** 同步进行期间修改设置、导入配置或清空配置等触发热重载的场景；可能造成旧 TAG 规则未被识别、新规则使用新 TAG，形成单轮语义不一致。
- **推荐修复方向：** 将本轮快照中的 TAG（或一个只读轮次配置结构）显式传入 `syncDomain`、`syncDomainInternal` 与 `retrySync`，重试期间不再读取可被替换的 `s.cfg`。
- **具体修复内容：**
  1. `syncAll` 在现有读锁快照中取出本轮 `cfg`、providers 和 resolver 后，同时把 `cfg.Tag` 保存为本轮不可变字符串。
  2. 将 TAG 作为显式参数沿 `syncDomain` → `syncDomainInternal` → `retrySync` 调用链传递；`retrySync` 的每次 Describe、OwnedRules、描述生成和重试都只使用该参数。
  3. 删除 `retrySync` 中对 `s.cfg.Tag` 的直接读取，确保完整的 Describe → Diff → Delete/Create 及后续重试均属于同一个配置快照。
  4. Dry Run 已使用其捕获的 `cfg.Tag`，保持现状；正式同步与 Dry Run 的 TAG 快照语义应一致。
  5. 不在本项中引入新的大型“轮次上下文”抽象。当前只有 TAG 发生越界读取，优先采用最小的显式参数；若后续发现更多轮次级字段，再评估小型只读结构。
  6. 热重载期间，新配置只从下一轮同步开始生效；正在执行的轮次不得混用新旧 TAG。Provider 和 resolver 继续沿用现有快照边界。
- **回归测试设计：**
  - 使用可阻塞的 fake Provider，在首次 Describe 后触发 `Reload` 修改 TAG，再释放同步继续执行；验证本轮筛选与新增描述始终使用旧 TAG；
  - 让第一次写入返回可重试错误，在退避期间修改 TAG，验证第二次 Describe/Diff 仍使用旧 TAG；
  - 触发下一轮同步，验证新 TAG 从下一轮开始生效；
  - 同时运行同步、Dry Run 与 Reload，使用 race detector 验证无未受保护的配置读取；
  - 更新直接调用 `retrySync` 的既有单元测试，使其显式传入 TAG，并覆盖空注释和描述截断不受影响。
- **验收建议：**
  - 增加同步与 TAG 热重载并发测试；
  - 验证同一轮 Describe → Diff → Create/Delete 始终使用同一个 TAG；
  - 使用 `go test ./... -race` 验证无竞态；
  - 验证下一轮同步才使用新 TAG。
- **实施记录（2026-09-23，Build6 Step 1）：**
  - 代码：`syncAll` 在既有读锁快照内捕获 `roundTag := cfg.Tag`，经 `syncDomain → syncDomainInternal → retrySync` 显式传参；`retrySync` 的 `OwnedRules`、描述生成与全部重试只使用该参数，删除对 `s.cfg.Tag` 的直接读取。Dry Run 保持使用其已捕获的 `cfg.Tag`。
  - 修复前红灯（真实结果）：`TestSyncRound_TagSnapshotDuringReload` 在本轮首次 Describe 阻塞期间通过真实 `Reload` 替换 TAG 后，本轮改为使用**新** TAG（新增描述 `[new-tag] 测试`、旧 TAG 云端规则未被识别删除）；`TestRetrySync_TagSnapshotAcrossRetry` 在重试轮同样改用新 TAG；`TestSyncRound_ConcurrentReloadStress` 报出 `retry.go:40` 无锁读与 `syncer.go:143` 锁内写的 `DATA RACE`。
  - 修复后：`go test ./... -race -count=1` 通过；`go test -race -count=20 ./syncer` 通过（`ok 134.735s`）。
  - 可达性说明：`s.cfg` 仅由 Run goroutine 在锁内写入，而 Run 同步执行 `syncAll` 且 `Reload` 是阻塞式 channel 发送，因此"一轮同步进行中替换配置"在**当前生产流程中不可达**；本项是真实的锁旁路与单轮快照契约缺失，Step 5 的原子运行时替换会使其成为可达路径。相关测试交错为测试构造，已在 Build6 Step 1 记录中如实标注。
  - 门禁（2026-09-23）：`go test -race -count=100 -timeout 40m ./syncer` 通过（`ok 665.887s`，0 次 DATA RACE；默认 10 分钟包级超时不足，需显式放宽 `-timeout`）；`go test ./... -race` 通过。
- **状态：** ✅ 已修复并验收通过（Build6 Step 1 四道门禁真实通过）

---

## 三、依赖与构建门禁优化

### O5-01 前端构建依赖存在已知漏洞

- **优先级：** 中
- **审查结果：** 2026-09-22 执行 `npm audit` 报告 3 个 high、1 个 moderate，涉及 `nanoid 3.3.16`、Vite、esbuild 与 brace-expansion。
- **风险边界：** 这些依赖主要用于前端构建和开发服务器；生产二进制嵌入的是构建后的静态文件，不应直接将开发服务器漏洞等同为生产 WebUI 可利用漏洞。但开发机和 CI 仍会执行相关工具，应进行升级评估。
- **推荐处理方向：**
  - 先更新可在现有主版本范围内修复的间接依赖，至少将 nanoid 升级到已修复版本；
  - Vite 的审计建议涉及主版本升级，应单独评估兼容性，不直接执行不受控的强制升级；
  - 升级后执行 `npm run build` 并人工检查主要页面；
  - 评估在 CI 中增加 `npm audit` 或其他依赖扫描门禁。
- **具体处理内容：**
  1. 在不修改依赖前先保存 `npm audit --json` 结果，确认每条告警的依赖链、受影响版本、修复版本及是否只存在于 devDependency。
  2. 第一阶段只执行现有 semver 范围内的锁文件更新，优先消除可通过 `npm update` 修复的 `nanoid`、`brace-expansion` 等间接依赖；不得使用 `npm audit fix --force`。
  3. 分别记录更新前后的 `package.json`、`package-lock.json`、Node/npm 版本和 audit 结果，避免把锁文件的大面积变化误认为业务依赖升级。
  4. 若 Vite/esbuild 告警只能通过 Vite 主版本升级消除，则单独形成升级项：先阅读对应 Vite、`@vitejs/plugin-vue`、Vue 和 `vue-tsc` 的官方迁移说明，再同步兼容版本，不混入普通锁文件刷新。
  5. CI 门禁分两层：生产依赖可使用 `npm audit --omit=dev` 作为阻断项；包含 devDependency 的完整 audit 先作为可见报告，待当前已知告警清零并确认上游稳定性后再决定是否阻断构建。
  6. 不把开发服务器漏洞直接描述为已确认的生产 WebUI 漏洞；若告警利用条件依赖 `vite dev` 或本地文件服务，应在验收结论中明确风险边界。
- **验收建议：**
  - `npm ci` 和 `npm run build` 通过，且工作树中没有未解释的生成文件；
  - `npm audit` 的剩余条目逐项记录接受、升级或上游等待理由；
  - 人工打开仪表盘、目标、规则、设置、日志、模拟测试和告警页面，检查路由、主题、弹窗和表单；
  - 随后运行 Go race 测试、vet 和 Docker 构建，确认嵌入前端产物仍可被单二进制提供。
- **状态：** ☐ 已决策 / 待 Build6 Step 6 实施

### O5-02 CI 未运行 Go race detector

- **优先级：** 中
- **现状：** `.github/workflows/docker-publish.yml` 使用 `go test -v ./...`，未启用 `-race`；已存档 Build5 的既有验收记录多次使用 `go test ./... -race`。
- **影响：** 普通测试无法持续发现 EventBus、热重载和同步并发路径中的数据竞态，CI 门禁弱于当前人工验收口径。
- **推荐处理方向：** 将 CI 测试门禁调整为 `go test -race ./...`。项目规模较小，预计额外成本可控；若未来耗时明显增加，再拆分普通测试与 race job。
- **具体修复内容：**
  1. 将 `.github/workflows/docker-publish.yml` 当前的 `go test -v ./...` 改为 `go test -race -v ./...`，继续放在前端构建之后，保证 `webui/embed.go` 所需的 `frontend/dist` 已存在。
  2. 保留独立的 `go build -v ./...` 和 `go vet ./...`；race 测试不能替代普通编译及 vet。
  3. 不在第一步加入 `-count`、覆盖率上传或复杂矩阵，先控制 CI 时长与稳定性；针对 R5-02 的高并发测试可在包内设置足够循环次数。
  4. 确认 GitHub Actions 的 Linux amd64 环境支持 CGO/race；Docker 最终镜像仍保持 `CGO_ENABLED=0` 静态编译，CI race 所需 CGO 不改变发布二进制约束。
  5. 如果 race job 后续明显拖慢 tag 发布，再将测试拆为独立 job 并让 Docker 发布依赖其成功，不得简单退回普通测试。
- **验收建议：**
  - 本地执行 `go test -race ./...`；
  - 在 PR 工作流确认 race 命令真实运行且失败会阻止后续镜像构建；
  - 检查 tag 构建仍只在所有编译、vet、race 测试通过后登录并推送镜像。
- **实施记录（2026-09-23，Build6 Step 1）：**
  - 代码：`.github/workflows/docker-publish.yml` 的 `go test -v ./...` 已改为 `go test -race -v ./...`；前端构建仍在 Go 测试之前，独立 `go build -v ./...` 与 `go vet ./...` 保留，Docker 产物仍为 `CGO_ENABLED=0`（`build/Dockerfile`）。
  - 本地证据：`go test ./... -race -count=1` 通过；`go vet ./...` 通过；工作流经 YAML 解析确认步骤顺序为 前端构建 → build+vet → race 测试 → Docker 推送（推送仅在 tag 路径）。
  - 证据边界：**未推向远端、未获得 GitHub Actions 运行结果**，因此不得声称 CI 已通过；远端验证已列入 `ProdTestList.md` 第三节第 1 项。
  - 耗时说明：`./syncer -count=100` 会超出 Go 默认 10 分钟包级超时（需 `-timeout 40m`），因此 CI 侧维持单轮 race 测试；若后续需要 100 轮门禁，按本项第 5 条拆分为独立 job 并让发布依赖其成功。
  - 门禁（2026-09-23）：Build6 Step 1 四道门禁在本机全部真实通过（含 `./notifier`、`./webui/api`、`./syncer` 各自 100 轮 race）。
- **状态：** ◧ 代码已实施并有本地静态/命令证据 / 待远端 GitHub Actions 运行确认（远端验证见根目录 `ProdTestList.md` 第三节第 1 项）

---

## 四、测试覆盖优化

### O5-03 高影响路径测试不足

- **优先级：** 中
- **2026-09-22 覆盖率快照：**

| 包 | 覆盖率 |
|---|---:|
| `dns` | 94.9% |
| `internal/portconv` | 100.0% |
| `internal/tag` | 100.0% |
| `syncer` | 65.1% |
| `config` | 56.6% |
| `webui` | 31.2% |
| `notifier` | 27.8% |
| `webui/api` | 23.7% |
| `provider` | 15.0% |
| 根包、`app` | 0.0% |

- **判断：** 不建议只为提高总覆盖率补测试，应优先覆盖数据恢复、并发与云规则转换等高影响行为。
- **推荐测试顺序：**
  1. 配置导入导出保持目标关联；
  2. EventBus 发布/取消并发；
  3. 同步中热重载 TAG、Provider 和 Resolver；
  4. 配置导入失败时事务回滚；
  5. 告警热重载期间继续发布事件；
  6. Provider 的规则转换、ICMP/IPv6 和精确删除边界；
  7. 前端关键流程：配置导入确认、清空数据、暂停/恢复同步、规则目标选择。
- **具体补测内容：**
  1. `webui/api`：优先使用 `httptest` 覆盖配置导入成功、无效引用、事务回滚、目标/规则最小校验、暂停/恢复状态码，以及清空数据后重载通知。
  2. `notifier`：覆盖接口订阅的注册/取消、channel 订阅的发布/取消竞态、慢订阅者满缓冲时非阻塞丢弃，以及告警 Subscriber 返回错误时不影响其他订阅者。
  3. `syncer`：使用 fake Provider/Resolver 覆盖热重载快照、重试计数、部分写入后重新 Diff、暂停门控、恢复后立即同步和停止时等待当前轮次。
  4. `provider`：不调用真实云 API，针对纯转换与 Diff 逻辑覆盖 TCP+UDP 拆分、ICMP 端口、IPv4/IPv6 字段、ECS ICMPv6 跳过、描述长度及精确删除标识。
  5. `webui`：使用真实 `net.Listener` 或 `httptest.Server` 覆盖指定端口、随机端口、健康检查、静态资源和 Shutdown；避免依赖固定端口造成并行测试冲突。
  6. 前端：只为高风险交互引入最小测试设施；若当前没有前端测试框架，应先评估引入成本，不为了覆盖率数字一次性建立重型 E2E 系统。配置导入、清空数据等仍保留人工浏览器验收清单。
  7. 每项修复在同一变更中补对应回归测试；不创建脱离具体风险的“覆盖率冲刺”。覆盖率报告用于发现空白，不设置任意全仓百分比目标。
- **证据边界：** 单元测试与 `httptest` 只能证明本地代码语义；Provider mock 不证明真实云 API，前端构建不证明浏览器交互，均不得替代相应人工或外部链路验收。
- **状态：** ☐ 已纳入 Build6 各 Step 与 Step 7 / 待实施

---

## 五、次级实现优化

### O5-04 HTTP 端口选择存在探测与监听时间窗口

- **优先级：** 低至中
- **现状：** `webui/server.go` 先通过 `net.Listen` 探测端口，关闭 listener 后再调用 `http.ListenAndServe`。
- **影响：** 在探测结束到正式监听之间，端口可能被其他进程占用，造成已选择的端口启动失败。该问题发生概率较低，但实现上可以直接消除。
- **推荐处理方向：** 保留已创建的 `net.Listener`，构造 `http.Server` 后调用 `Serve(listener)`，避免关闭后重新绑定。
- **具体修复内容：**
  1. 将“选择监听地址”和“开始 HTTP 服务”合并为一次 `net.Listen`：先尝试 `host:preferredPort`，仅在确认属于地址占用错误时再监听 `host:0`。
  2. 不再由 `findAvailablePort` 返回一个随后重新绑定的整数；改为返回已经成功绑定的 `net.Listener`，实际端口从 `listener.Addr()` 读取。
  3. `Server.Start` 使用同一个 listener 调用 `http.Server.Serve(listener)`，从端口选择到正式服务之间不释放端口。
  4. 首选端口因权限、非法地址或其他非占用原因失败时直接返回原始错误，不将所有监听错误都伪装成“端口被占用”并随机降级。
  5. 保持默认端口占用时选择随机端口并记录 WARN 的现有产品语义；Docker 仍建议固定端口，因为宿主机端口映射不会跟随容器内随机端口。
  6. 本项与 O5-05 都会修改 `webui.Server`，实施时应放在同一 Build Step 中，直接形成最终的 listener + `http.Server` 生命周期，避免连续重构两次。
- **回归测试设计：**
  - 首选端口可用时验证实际端口等于首选值；
  - 首选端口被其他 listener 占用时验证选择非零随机端口且健康端点可访问；
  - 使用不可绑定地址验证返回错误而不是随机降级；
  - 并发启动测试中确认不会出现“探测成功但正式监听失败”的时间窗口；
  - 测试结束统一 Shutdown/Close，避免遗留 goroutine 和监听端口。
- **状态：** ☐ 已决策 / 待 Build6 Step 3 实施

### O5-05 HTTP 服务缺少显式超时与优雅关闭

- **优先级：** 低至中
- **现状：** 当前使用包级 `http.ListenAndServe`，未设置 `ReadHeaderTimeout` 等超时；收到 SIGTERM/SIGINT 后只停止并等待 Syncer，没有对 HTTP Server 调用 `Shutdown`。
- **影响：** 退出时正在处理的配置请求或 SSE 连接会由进程退出直接中断；长期空闲或异常请求也缺少服务端超时边界。
- **推荐处理方向：** 使用显式 `http.Server`，设置适合内部 WebUI 的合理超时；退出时停止接收新请求，并在有界 context 内调用 `Shutdown`，同时继续遵守“完成当前同步轮次再退出”的要求。
- **具体修复内容：**
  1. 在 `webui.Server` 中持有显式 `*http.Server`、实际 `net.Listener` 以及必要的状态锁；`Start`/`Serve` 返回实际端口，并能区分正常关闭的 `http.ErrServerClosed` 与真正启动/运行错误。
  2. 至少设置 `ReadHeaderTimeout`；`ReadTimeout`、`WriteTimeout` 和 `IdleTimeout` 需考虑 SSE 长连接，不能设置会定期切断 `/api/sync/events` 或 `/api/logs/stream` 的全局短 `WriteTimeout`。优先使用请求头超时和合理的 IdleTimeout，再由 request context 管理 SSE 生命周期。
  3. 增加幂等 `Shutdown(ctx)`。收到 SIGTERM/SIGINT 后先停止接收新的 HTTP 请求，使现有 API/SSE context 开始退出，再通知 Syncer 停止并等待当前同步轮次完成；具体先后顺序在 Build Step 中固定，并设置有界的 HTTP shutdown 超时。
  4. main goroutine 必须能收到 HTTP 启动/运行错误；当前 goroutine 只记录错误、主流程仍继续运行的行为需要改为可感知的错误通道或受控退出，避免 WebUI 启动失败但同步进程静默存活。
  5. Shutdown 超时只限制 HTTP 请求收尾，不得破坏“当前同步轮次完成后退出”的强约束；若同步等待本身没有超时，文档和日志需要如实说明。
  6. SSE handler 保持监听 `r.Context().Done()`，Shutdown 时连接应退出并执行取消订阅；不得依赖强制关闭进程来清理订阅。
  7. 多次 Shutdown、未启动时 Shutdown、Serve 已退出后 Shutdown 均应安全返回，不产生 double close。
- **回归测试设计：**
  - 启动真实 HTTP Server，请求 `/api/health` 后 Shutdown，验证 Serve 返回正常关闭语义；
  - 建立两类 SSE 连接后 Shutdown，验证请求 context、handler 和订阅均退出；
  - 构造一个进行中的普通请求，验证在超时内完成；构造超时请求，验证 Shutdown 有界返回；
  - 模拟监听失败，验证 main 能感知并进入退出路径；
  - 模拟正在运行的同步轮次收到 SIGTERM，验证 HTTP 停止接收新请求且同步轮次仍完成后退出。
- **状态：** ☐ 已决策 / 待 Build6 Step 3 实施

### O5-06 API 与配置导入缺少最小持久化边界校验

- **优先级：** 中
- **现状：** 目标、规则和设置 API 主要依赖前端控件保证格式，后端可将任意 `cloud_type`、协议、空字段或不可解析设置写入数据库；配置导入还接受用户提供的外部 JSON 文件。
- **约束边界：** AGENTS.md 要求“不过度防御”，因此不建议引入复杂校验框架或重复云平台合法性校验；地域仍应允许预填列表外的值，由云 API 自行判断。
- **推荐处理方向：** 只增加持久化边界所需的最小校验：
  - `cloud_type` 属于四个支持值之一；
  - host、region、resource_id 等核心字段非空；
  - 协议属于 TCP、UDP、TCP+UDP、ICMP；
  - interval、DNS timeout、失败阈值和 WebUI 端口可解析且为正值；
  - 导入规则引用的目标 ID 能通过本次导入映射解析。
- **具体修复内容：**
  1. 在 `config` 包增加轻量、可复用的目标、规则和设置值校验函数，不引入第三方 validation 框架；POST、PUT 与配置导入共用相同业务规则，避免 handler 各写一份。
  2. 目标校验：`cloud_type` 只允许四个已支持枚举；`region`、`resource_id` 去除首尾空白后必须非空；不校验地域是否存在于预填列表，也不臆测各云资源 ID 的完整格式。
  3. 规则校验：`host` 非空；协议只允许 TCP/UDP/TCP+UDP/ICMP；action 只允许 ACCEPT/DROP；非 ICMP 的端口必须非空，ICMP 统一归一化为 `ALL`；目标 ID 必须为正数且引用当前数据库或本次导入中存在的目标。
  4. 设置接口采用允许列表；`interval`、`dns_timeout` 使用 `time.ParseDuration` 且必须大于零，`dns_fail_threshold` 必须是正整数，`log_level` 限定为 debug/info/warn/error，`theme` 限定为 light/dark。`sync_enabled` 只由 pause/resume 端点和配置导入写入；`webui_port` 不再属于 SQLite 业务设置。
  5. DNS 字段只做“非空且能形成有效 host:port”的最小解析检查；仍允许域名或 IP 形式的自定义 DNS，不验证服务器是否在线。
  6. 导入请求先完成全量结构与引用校验，再进入清空旧数据的事务；即使事务本身会回滚，也应尽早以 HTTP 400 返回明确的外部输入错误。
  7. JSON 解码增加单个顶层对象和未知字段策略的明确约束：建议对固定结构请求使用 `DisallowUnknownFields`，但 settings map 仍由允许列表控制；同时限制请求体大小，避免内部工具也可被异常大文件耗尽内存。
  8. Store 层保留数据库错误返回，不把所有数据合法性责任只放在前端；但不增加云 API 连通性校验、实时地域校验或复杂域名正则，遵守“不过度防御”。
- **错误响应约定：** 请求 JSON、字段值、引用关系错误返回 400；资源状态冲突可返回 409；数据库或内部执行错误返回 500。错误消息指出字段和原因，但不回显凭据或完整敏感配置。
- **回归测试设计：**
  - 对目标、规则和设置分别覆盖合法值、空白值、未知枚举、零/负数和不可解析值；
  - 验证列表外地域仍可保存；
  - 验证规则引用不存在目标时不写库、不触发 reload；
  - 验证配置导入的所有校验都发生在旧配置清除前，并与 R5-01 的 ID 映射测试共用场景；
  - 验证非法请求返回 400，数据库模拟失败返回 500，响应不包含凭据。
- **状态：** ☐ 已决策 / 待 Build6 Step 4 与 Step 5 实施

---

## 六、架构决策

### A5-01 移除 `.env` headless 业务配置模式

- **优先级：** 已确认架构决策；按 Build6 Step 2 实施
- **现状：** 项目同时维护 SQLite/WebUI 与 `.env` 两套业务配置入口。自动检测依据进程环境中的 `TARGETS`，进入 env 模式后却固定读取工作目录下 `.env`；WebUI 模式只应用 `WEBUI_HOST`/`WEBUI_PORT` 环境覆盖，而 Compose 示例还列出 `INTERVAL`、`DNS`、`LOG_LEVEL` 等容易被误解为 WebUI 覆盖项的变量。两套模式的功能也已不对等：`.env` 不具备配置热重载、历史记录、告警、资源扫描、配置导入导出和完整的每规则能力。
- **已知前提：** 当前不存在存量用户、历史部署或向后兼容要求；不需要弃用周期、自动迁移命令或双模式过渡版本。
- **已固定决策：**
  1. 最终只保留 WebUI + SQLite 业务形态，服务仍可在 Linux 或 Docker 无图形桌面环境运行。
  2. 仅保留 `FWALIZER_DATA_DIR`、`WEBUI_HOST`、`WEBUI_PORT` 三个部署变量。
  3. 不保留 `TC_ACCESS_ID`、`TC_ACCESS_KEY`、`ALI_ACCESS_ID`、`ALI_ACCESS_KEY` 或其他业务 ENV override；云凭据和全部业务设置收束到 SQLite。
  4. 删除全部 CLI，包括 `version`、`validate`、`backup`、`restore`；不提供替代 CLI。
  5. Step 0 只完成文档前置切换；代码、README 使用说明和部署示例在 Step 2 同批收敛。
- **具体实施内容：**
  1. `main.go` 删除 `DetectMode` 和 env/WebUI switch，启动时直接创建数据目录、pidfile、SQLite、WebUI 和 Syncer。
  2. 删除 `app/mode.go`；若 `app.Run` 仅由 env 模式使用，则删除该 headless runner，并保留仍由 WebUI 使用的日志公共能力。
  3. 删除 `config.LoadEnv`、`ParseEnv`、TARGETS/RULES 语法解析及对应专用测试；不能直接删除整个 `config/env.go`，应先把 `ApplyWebUIEnv` 拆到语义明确的运行时配置文件。
  4. 删除 `version`、`validate`、`backup`、`restore` 及相关 README、`version` 包和编译期版本注入。
  5. 删除 `.env.example`、Compose 中 `.env` 文件挂载和 headless 说明；健康检查统一为 HTTP `/api/health`，不再使用 `pgrep` fallback 掩盖 WebUI 启动失败。
  6. 删除 `FWALIZER_MODE`、`TARGETS` 自动切换语义。即使进程环境中意外存在 `TARGETS`，也必须继续启动唯一 WebUI 模式或明确忽略该变量。
  7. 删除四个云凭据和全部业务设置的环境变量入口，不建立 ENV > SQLite 优先级。
  8. Compose 示例只保留实际生效的启动变量。业务设置由 WebUI/SQLite 管理，不再展示看似可覆盖但运行时被忽略的 `INTERVAL`、`DNS`、`LOG_LEVEL` 等项。
  9. 保持 stdout 日志、Docker 非 root、SQLite 数据卷、WebUI 默认 `127.0.0.1`、容器内显式 `0.0.0.0` 等现有部署约束。
- **明确不包含：**
  - 不提供 `.env` → SQLite 迁移器；
  - 不保留隐藏兼容开关或旧解析器；
  - 不改写 `HistoryDocs/` 中的历史记录；
  - 不借机修改云规则算法、Provider API 或 WebUI 业务功能；
  - 不把 A5-01 与前端依赖升级、HTTP 生命周期重构合成一个大变更。
- **回归测试与验收：**
  - 源码和当前文档中除历史说明外，不再存在 `ModeEnv`、`FWALIZER_MODE`、`.env` 业务模式和 TARGETS/RULES 解析入口；
  - 空数据库可正常启动，WebUI 提示用户配置目标和规则；
  - 存在无关 `TARGETS` 环境变量时也不会切换模式或尝试读取 `.env`；
  - `FWALIZER_DATA_DIR`、`WEBUI_HOST`、`WEBUI_PORT` 继续生效；无关业务环境变量不得改变 SQLite 配置；
  - Compose 配置校验通过，容器启动后只能通过 HTTP 健康检查判定健康，并实际请求 `/api/health`；
  - 执行前端构建、`go test ./... -race`、`go vet ./...`、Compose 配置校验、Docker 镜像构建和 `git diff --check`；
  - 人工检查首次配置、保存、重启后持久化、暂停/恢复、模拟测试、同步日志及告警页面。
- **实施记录（2026-09-23，Build6 Step 2）：**
  - 代码：`main.go` 收束为 `os.Exit(run(...))`，新增 `run.go`（`run(args,...) int` + `runWebUI`）；删除 `app/cli.go`、`app/mode.go`、`app/app.go`（Headless `Run`）与 `config/env.go`、`config/validate.go`、`config/env_test.go`、`version/`、`.env.example`；新增 `config/deployment.go` 承载 `DeploymentConfig`（数据目录/监听地址/端口，空白 `FWALIZER_DATA_DIR` 按未设置、`WEBUI_PORT` 仅接受十进制 `1～65535`）；`Config` 删除 `Mode`/`WebUIHost`/`WebUIPort`；`/api/settings` 不再返回或落库 `webui_port`，导入含该键在写事务前返回 400；Makefile/Dockerfile/工作流删除版本注入；Dockerfile 与 Compose 健康检查只走 `/api/health`（动态 `WEBUI_PORT` 拼接，无 `pgrep`）；README 与代码同批删除 CLI/backup/restore/`.env`/业务变量说明。
  - 自动门禁：`go test ./... -race`、`go vet ./...`、`go build ./...`、`git diff --check` 全部通过；修改文件 `gofmt -l` 无输出；`npm ci && npm run build` 通过；`docker compose -f docker-compose.yml.example config --quiet` 通过；`docker build -f build/Dockerfile -t fwalizer:build6-step2 .` 通过。
  - 专项测试：`config/deployment_test.go`（默认值、空白数据目录、Host/Port 独立生效、边界与非法端口、业务 ENV 不影响部署参数）、`main_test.go`（空库无参数启动且 `/api/targets` 返回空数组、12 种参数非零退出且不创建数据目录、非法端口失败、`TARGETS`/`TC_ACCESS_ID`/`INTERVAL`/`FWALIZER_MODE` 不改变 SQLite 配置）、`webui/api/settings_policy_test.go`（GET/PUT/导入/导出与残留键）。
  - Docker 证据：真实容器健康检查 `healthy`；进程存活但 HTTP 不可达时容器为 `unhealthy`（`Connection refused`，无进程 fallback）；`WEBUI_PORT=61234` 覆盖生效且健康检查跟随；`docker stop` 日志显示完成当前轮次后 `ExitCode=0`。临时容器与 `fwalizer:build6-step2` 镜像已清理。
  - 证据边界：**未执行真实浏览器交互**，**未推向远端**（GitHub Actions 工作流改动无远端运行结果）；两项均登记为待办，未把本地命令结果表述为远端 CI 或浏览器已通过。
- **状态：** ✅ 已修复并验收通过（Build6 Step 2 规定门禁与 Docker 容器验收真实通过；浏览器人工复核与远端 CI 为登记待办）

---

## 七、本次审查验证记录

审查日期：2026-09-22。

| 检查项 | 结果 | 证据边界 |
|---|---|---|
| `go test ./... -race` | 通过 | 当前已有自动化测试通过；不代表未覆盖并发路径不存在问题 |
| `go vet ./...` | 通过 | Go 静态检查通过 |
| `npm run build`（`webui/frontend/`） | 通过 | TypeScript 检查与 Vite 生产构建通过 |
| `go test ./... -cover` | 通过 | 覆盖率见 O5-03 |
| `npm audit` | 未通过 | 报告 3 high、1 moderate，见 O5-01 |
| Git 工作树 | 审查开始时干净 | 本次审查阶段未修改代码 |

本次未执行真实云 API、真实 DNS 故障、SMTP、Webhook、浏览器交互、Docker 容器或真实设备验收，因此本文档不把源代码审查和自动化测试结果描述为这些外部链路已通过。

---

## 八、后续处理约定

1. 本文档是当前问题记录，方案口径已纳入 Build6，但不代表 Step 1-7 已获代码实施授权；
2. 每次只实施 Build6 一个 Step，不跳步、不并行构建；
3. 对应 Step 验收完成后，再更新本文档的问题状态和实际证据；
4. 源码核验、自动测试、race、build/vet、Docker、浏览器与真实外部链路证据分层记录，不得互相替代。

---

## 九、变更记录

| 版本 | 日期 | 说明 |
|---|---|---|
| v1.0 | 2026-09-22 | 新建项目审查问题记录：3 个待修复问题，以及依赖、CI、测试、HTTP 生命周期与输入边界优化项 |
| v1.1 | 2026-09-22 | 为 R5-01～R5-03、O5-01～O5-06 补充具体实施内容与回归测试；新增 A5-01 headless 模式移除候选及推荐执行顺序 |
| v1.2 | 2026-09-22 | 升格为当前问题记录；按 Build6 更新完整敏感配置包、全 CLI 移除、三个部署变量和各问题的已决策/待实施状态 |
| v1.3 | 2026-09-23 | 同步 Build6 Step 1 实施证据：R5-02（取消不再关闭 channel、Publish 双重复制）、R5-03（本轮 TAG 显式传参）与 O5-02（CI 启用 race）落地并附真实结果；按用户确认口径修正 R5-02「最多一个在途事件」旧表述；未完成的 `./syncer -count=100` 门禁转入根目录 ProdTestList.md |
| v1.4 | 2026-09-23 | 同步 Build6 Step 2 实施证据：A5-01（CLI/`.env` Headless 移除、三个部署变量收束、`webui_port` 退出业务配置、健康检查去 `pgrep`）落地并附本地门禁与 Docker 容器结果；浏览器人工复核与远端 CI 保持待办 |
