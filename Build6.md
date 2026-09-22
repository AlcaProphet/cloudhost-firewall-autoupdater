# FWAlizer 功能构建计划（Build6：待实施方案）

> **文档定位：** 本文档是 FWAlizer 下一阶段的构建前实施方案，整合完整配置导入导出、CLI 与 `.env` Headless 业务模式移除，以及 [Issue5.md](./Issue5.md) 中 R5-01～R5-03、O5-01～O5-06、A5-01 的处理计划。
>
> - 编码指令：[AGENTS.md](./AGENTS.md)（当前唯一强要求；在 Step 0 获批并完成前，本文档不得覆盖其现行要求）
> - 当前设计记录：[Design4.md](./Design4.md)
> - 当前已完成构建方案：[Build5.md](./Build5.md)
> - 本阶段问题输入：[Issue5.md](./Issue5.md)
>
> **授权边界：** 用户已于 2026-09-22 确认本文档所述规划方向，并授权写入 Build6 作为构建前准备；这不代表任一代码 Step 已获实施授权。正式构建前应先执行 Step 0、完成文档契约同步并再次取得实施授权。

---

## 一、构建目标与最终形态

### 1.1 唯一运行形态

最终只保留：

```text
WebUI 单二进制 + SQLite + Docker/服务器部署
```

“移除 Headless”特指移除 `.env` 驱动、无 WebUI 的业务配置和同步模式，不移除服务器或 Docker 的无图形桌面运行能力。程序仍可运行在 Linux 服务器或容器中，用户通过浏览器访问 WebUI 完成全部业务配置。

### 1.2 移除范围

- 移除 `.env` Headless 业务模式、自动模式检测和 `.env` 业务配置解析；
- 移除全部 CLI 子命令：`version`、`validate`、`backup`、`restore`；
- 移除 `FWALIZER_MODE` 以及 TARGETS/RULES 等业务环境变量入口；
- 移除 `.env.example`、CLI/Headless 文档、测试和专用运行代码；
- 删除仅服务 CLI 版本输出的 `version` 包和编译期版本注入；
- 未知命令行参数必须报错并以非零状态退出，不得意外启动 WebUI。

### 1.3 保留的部署变量

仅保留以下部署边界：

| 环境变量 | 用途 |
|---------|------|
| `FWALIZER_DATA_DIR` | SQLite、pidfile 和持久化数据目录 |
| `WEBUI_HOST` | HTTP 监听地址 |
| `WEBUI_PORT` | HTTP 监听端口 |

以上变量不写入 SQLite、不进入配置包、不被配置导入覆盖。`webui_port` 不再作为数据库业务设置；现有数据库中的同名残留键应在实施时明确删除或忽略。

### 1.4 唯一业务配置源

以下配置全部收束到 SQLite，并只通过 WebUI 管理：

- 腾讯云、阿里云访问密钥；
- 云资源目标和域名规则；
- TAG、同步间隔、DNS、DNS 超时、DNS 失败阈值；
- 日志级别、同步开关、主题；
- 邮件告警、SMTP 凭据、Webhook 告警。

---

## 二、完整配置包契约

### 2.1 格式和版本

- 新格式固定为明文 JSON `version: 2`；
- 新导出只生成 version 2；
- version 1 明确拒绝，不做隐式迁移或模糊补全；
- 配置包是完整快照，导入语义为“替换”，不是“合并”；
- 固定结构采用强类型对象并拒绝未知字段；
- 导出应补齐业务默认值，不让恢复结果依赖目标数据库原有缺省状态。

### 2.2 导出内容

完整配置包包含：

1. 全部目标；
2. 全部规则及其目标引用；
3. 腾讯云 SecretId/SecretKey；
4. 阿里云 AccessKeyId/AccessKeySecret；
5. TAG、同步间隔、DNS、DNS 超时、失败阈值、日志级别、同步开关、主题；
6. 邮件告警完整配置，包括 SMTP 用户名和密码；
7. Webhook 告警完整配置，包括 Webhook URL 和渠道。

### 2.3 不导出的内容

| 数据 | 导入处理 | 原因 |
|------|---------|------|
| 同步日志 `sync_logs` | 保留目标实例原日志 | 运行历史，不是业务配置 |
| 扫描结果 `scanned_resources` | 导入成功时清空 | 派生缓存，可能与新密钥不匹配 |
| WebUI host/port | 不处理 | 部署参数 |
| 数据目录、pidfile | 不处理 | 运行和部署状态 |
| SQLite 自增序列 | 不重置 | 数据库内部实现 |
| Docker 卷和端口映射 | 不处理 | 宿主环境配置 |

### 2.4 敏感信息安全边界

完整配置包含有可直接使用的云密钥、SMTP 密码和 Webhook URL，其敏感等级等同于生产 Secret 或数据库备份。

- 导出改为确认后的 `POST /api/config/export`；
- 前端使用 Blob 触发附件下载，不在页面或 console 展示内容；
- 响应设置 `Cache-Control: no-store`；
- 后端不得记录导入请求体或导出响应体；
- 错误信息不得回显密钥、SMTP 密码、Webhook URL 或完整敏感配置；
- 前端导入、导出都必须使用危险级卡片式确认；
- README 明确禁止把配置包提交 Git、上传公共网盘或通过不可信渠道传输；
- 本阶段不引入配置包密码加密，后续如有需要另行设计格式和密钥管理。

### 2.5 原子导入语义

```text
解析并限制请求体大小
        ↓
校验 version 2 固定结构、字段和值
        ↓
校验目标导出 ID 和规则引用
        ↓
开启 SQLite 事务
        ↓
清空旧目标、规则、settings 和告警配置
        ↓
插入目标并建立“导出 ID → 新数据库 ID”映射
        ↓
复制规则、重写 targets 并插入
        ↓
写入全部设置、密钥和告警配置
        ↓
清空扫描缓存，保留同步日志
        ↓
提交事务
        ↓
触发完整运行时重载
```

任一步数据库操作失败时必须回滚；Commit 成功前不得触发 reload。

---

## 三、构建进度追踪

| Step | 内容 | 主要依据 | 状态 |
|------|------|---------|------|
| 0 | 文档体系切换与设计契约固定 | 本文档 §一～二、Issue5 A5-01 | ☐ 未开始 |
| 1 | 并发正确性基线与 CI race 门禁 | Issue5 R5-02、R5-03、O5-02 | ☐ 未开始 |
| 2 | 移除 CLI 与 `.env` Headless 业务模式 | Issue5 A5-01、本文件 §1 | ☐ 未开始 |
| 3 | HTTP Listener、Server 生命周期与优雅关闭 | Issue5 O5-04、O5-05 | ☐ 未开始 |
| 4 | API 最小持久化校验边界 | Issue5 O5-06 | ☐ 未开始 |
| 5 | version 2 完整配置包与目标 ID 关联恢复 | Issue5 R5-01、本文件 §2 | ☐ 未开始 |
| 6 | 前端依赖审计与受控升级 | Issue5 O5-01 | ☐ 未开始 |
| 7 | 高影响路径补测、真实验收与文档闭环 | Issue5 O5-03 | ☐ 未开始 |

> 状态标记：☐ 未开始 / ◧ 进行中 / ✅ 验收通过

> 执行原则：所有 Step 串行实施；每个 Step 自带回归测试和独立验收；不得把“规划确认”或“Step 0 文档授权”视为后续代码实施授权。

---

## 四、构建顺序与依赖

```text
Step 0 文档契约
   ↓
Step 1 并发基线和 race CI
   ↓
Step 2 WebUI-only 运行时
   ↓
Step 3 HTTP 生命周期
   ↓
Step 4 持久化校验
   ↓
Step 5 完整配置导入导出
   ↓
Step 6 前端依赖升级
   ↓
Step 7 高影响测试与总验收
```

排序理由：

1. 先固定强要求和设计契约，避免代码与现行 AGENTS.md 冲突；
2. 先修复会导致 panic 或配置混用的并发问题，并让 CI race 覆盖后续修改；
3. 先移除双运行模式，再重构 main 和 HTTP 生命周期，避免重复修改；
4. 先建立统一校验边界，再接入高风险的完整敏感配置导入；
5. 业务重构稳定后单独处理前端依赖，避免锁文件变化污染功能审查；
6. 测试随 Step 增加，最终 Step 只补剩余高影响空白并做全链路验收。

---

## 五、分步构建计划

### Step 0：文档体系切换与设计契约固定

- **目标：** 在代码修改前，将已确认的产品、配置和安全契约写入当前项目文档。
- **文件范围：** `AGENTS.md`、新建 `Design5.md`、`Build6.md`、`Issue5.md`、`README.md`、`HistoryDocs/`，以及当前文档指针。
- **处理内容：**
  1. 将 WebUI 单二进制定义为唯一运行形态；
  2. 写明 CLI、`.env` Headless 和业务环境变量的移除范围；
  3. 写明仅保留三个部署环境变量；
  4. 写明 SQLite 为唯一业务配置源；
  5. 写入 version 2 完整配置包、明文密钥和原子覆盖契约；
  6. 将 Issue5 每项映射到本文档对应 Step；
  7. 推荐将已完成的 `Design4.md`、`Build5.md`、`Issue4.md` 移入 `HistoryDocs/`，新建 `Design5.md` 并提升 `Issue5.md` 为当前问题记录；
  8. 历史文档正文保持不动。
- **验收：** 当前文档对运行模式、配置源、敏感导出、数据保留和实施顺序表述一致；`git diff --check` 通过。
- **授权门禁：** 完成后先由用户审阅文档，再决定是否授权 Step 1～7。

### Step 1：并发正确性基线与 CI race 门禁

- **目标：** 修复 EventBus 取消订阅竞态和同步轮次 TAG 快照越界，并让 CI 持续运行 race detector。
- **R5-02：**
  1. `SubscribeChan` 取消时只删除订阅表记录，不关闭 channel；
  2. 重复和并发取消保持幂等；
  3. SSE 生命周期由请求 context 控制；
  4. 不使用 `recover`，不把全部发送放进全局锁；
  5. 取消与发布竞态边界内允许最多一个在途事件。
- **R5-03：**
  1. `syncAll` 捕获本轮 TAG；
  2. TAG 显式沿 `syncDomain` → `syncDomainInternal` → `retrySync` 传递；
  3. 重试路径不再读取 `s.cfg.Tag`；
  4. 新 TAG 只从下一轮同步开始生效。
- **O5-02：** CI 将 `go test -v ./...` 改为 `go test -race -v ./...`，继续保留 build 和 vet；Docker 发布产物仍为 `CGO_ENABLED=0`。
- **测试：** 并发 Publish/取消循环、重复取消、慢订阅者、SSE context 退出、同步和重试期间 Reload、下一轮新 TAG、全仓 race。
- **验收：** `go test -race -count=100 ./notifier ./webui/api`、`go test ./... -race`、`go vet ./...`、`git diff --check`。

### Step 2：移除 CLI 与 `.env` Headless 业务模式

- **目标：** 运行时收束为唯一 WebUI + SQLite 模式。
- **主要文件：** `main.go`、`app/cli.go`、`app/mode.go`、`app/app.go`、`config/env.go`、`config/env_test.go`、`.env.example`、`Makefile`、`build/Dockerfile`、`docker-compose.yml.example`、README 和相关测试。
- **处理内容：**
  1. 删除全部 CLI 子命令、模式检测和 Headless runner；
  2. 删除 `.env` 业务解析和相关测试；
  3. 删除 `FWALIZER_MODE`、TARGETS/RULES 自动切换语义；
  4. 保留并重命名部署变量读取逻辑，使其只处理数据目录、监听地址和端口；
  5. 迁移仍被 WebUI 使用的日志公共能力；
  6. 删除无用途的 version 包、ldflags 和 Docker VERSION build arg；
  7. 删除 Compose 中业务环境变量和 `.env` 挂载；
  8. Docker 健康检查只请求 `/api/health`，删除 `pgrep` fallback；
  9. 从 SQLite 配置语义移除 `webui_port`；
  10. 未知命令行参数报错并非零退出。
- **测试：** 空数据库启动、无关 TARGETS 不改变模式、三个部署变量生效、业务变量不再覆盖 SQLite、未知参数退出、Docker HTTP 健康检查。
- **验收：** 活跃源码和当前文档无 CLI/Headless 入口；Compose config、Docker 构建与运行、全仓 race、vet、build、`git diff --check` 通过。

### Step 3：HTTP Listener、Server 生命周期与优雅关闭

- **目标：** 合并处理 O5-04 和 O5-05，一次形成最终 HTTP 生命周期。
- **处理内容：**
  1. 直接创建并保留 `net.Listener`；
  2. 首选端口仅在确认地址占用时降级到 `host:0`；
  3. 权限、非法地址等其他错误原样返回；
  4. 使用显式 `http.Server` 和同一个 listener 调用 `Serve`；
  5. 设置 `ReadHeaderTimeout` 和合理 `IdleTimeout`；
  6. 不设置会周期性切断 SSE 的短 `WriteTimeout`；
  7. 增加幂等 `Shutdown(ctx)`；
  8. main 能收到启动/运行错误并受控退出；
  9. 收到信号后先停止接收 HTTP 请求和退出 SSE，再停止新同步轮次并等待当前轮次完成；
  10. HTTP 收尾超时不得破坏“同步当前轮次完成后退出”的约束。
- **测试：** 首选/随机端口、非占用错误、健康端点、静态资源、两类 SSE、重复 Shutdown、启动失败、SIGTERM 和 Docker stop。
- **验收：** `go test -race ./webui ./webui/api ./...`、真实进程信号测试、Docker stop、vet、`git diff --check`。

### Step 4：API 最小持久化校验边界

- **目标：** 为普通 API 和完整配置导入提供同一套轻量校验，避免只依赖前端。
- **目标校验：** cloud type 只允许四平台；region/resource ID 去空白后非空；不实时验证地域或云资源格式。
- **规则校验：** host 非空；协议为 TCP/UDP/TCP+UDP/ICMP；action 为 ACCEPT/DROP；非 ICMP 端口非空；ICMP 归一化为 ALL；目标 ID 为正且可解析。
- **设置校验：** 使用允许列表；duration 和阈值为正；log level、sync enabled、theme 为支持值；DNS 只做最小结构检查；凭据允许为空。
- **JSON 边界：** 单个顶层对象、拒绝未知字段、限制请求体大小；外部输入错误返回 400，数据库错误返回 500；错误信息不回显敏感值。
- **行为边界：** 非法输入不写库、不触发 reload；不引入第三方 validation 框架，不调用真实云 API。
- **测试：** 正常值、空白、未知枚举、非法时长、零/负值、列表外地域、未知目标引用、错误脱敏。
- **验收：** `go test -race ./config ./webui/api`、全仓 race、vet、`git diff --check`。

### Step 5：version 2 完整配置包与目标 ID 关联恢复

- **目标：** 端到端实现完整敏感配置导出、原子覆盖导入和 R5-01 关联恢复。
- **后端协议：**
  1. 定义强类型 `ConfigBundleV2`，含 metadata、targets、rules、settings 和 alerts；
  2. 导出补齐默认值并包含云密钥、SMTP 密码和 Webhook URL；
  3. 只接受 version 2，version 1 明确返回 400；
  4. 导出改为 POST，设置 no-store 和附件响应头；
  5. 不导出运行日志、扫描缓存和部署参数。
- **R5-01 映射：**
  1. 导入前验证目标导出 ID 为正且唯一；
  2. 验证所有规则引用都存在于本次导入目标集合；
  3. 事务插入目标时通过 `LastInsertId()` 获取实际 ID；
  4. 建立 `map[导出ID]新ID`；
  5. 复制规则及 targets 切片后重写引用；
  6. 空 targets 保持“适用于全部目标”；
  7. 不重置自增序列，不强行复用来源数据库 ID。
- **全量覆盖：** 同一事务替换目标、规则、settings、邮件告警和 Webhook 告警，清空扫描缓存但保留同步日志；任何失败完整回滚。
- **运行时刷新：** Commit 后重建凭据、Provider/ClientPool、Resolver、Syncer 配置和告警订阅，刷新同步开关、主题和日志级别；日志级别使用 `slog.LevelVar` 或等价轻量方案即时生效。
- **前端：** 危险级导入/导出确认；POST + Blob 下载；成功后刷新设置、凭据状态、主题和告警；失败不改变当前配置。
- **自动测试：** 同实例自增历史、跨实例不同 ID、多目标、复用目标、空 targets、非法/未知 ID、完整密钥和告警覆盖、空值清除旧值、扫描缓存清空、同步日志保留、version 1/未知字段拒绝、各写入阶段失败回滚、失败不 reload、错误脱敏。
- **人工验收：** 两个不同 ID 历史数据库交叉导入；核对业务目标关系；连接测试、资源扫描、真实云 API、SMTP、Webhook；检查浏览器和日志无密钥泄露。

### Step 6：前端依赖审计与受控升级

- **目标：** 独立处理 O5-01，避免依赖锁文件变化混入业务重构。
- **处理内容：**
  1. 实施时重新保存 `npm audit --json`，记录 Node/npm 版本和依赖链；
  2. 第一阶段只更新现有 semver 范围内可修复依赖；
  3. 禁止 `npm audit fix --force`；
  4. Vite/esbuild 若需主版本升级，先阅读官方迁移说明并作为独立子阶段处理；
  5. `npm audit --omit=dev` 可作为生产依赖阻断项；全量 devDependency audit 先保持可见报告，待告警清零和稳定后再决定是否阻断。
- **验收：** 更新前后 audit 对比、`npm ci`、`npm run build`、全部主要路由人工检查、完整导入导出复测、Go race/vet、Docker 构建、`git diff --check`。

### Step 7：高影响路径补测、真实验收与文档闭环

- **目标：** 完成 O5-03，不追求任意覆盖率数字，只补高影响行为。
- **notifier：** 接口订阅、取消、慢订阅者、Subscriber 错误隔离、告警热重载期间发布。
- **syncer：** Provider/Resolver/TAG 快照、重试重新 Diff、部分写入、暂停恢复、停止等待当前轮次。
- **provider：** TCP+UDP 拆分、ICMP 端口、IPv4/IPv6、ECS ICMPv6 跳过、描述长度、精确删除、CVM 规则上限；只测纯转换和 mock，不声称真实云 API 已验证。
- **webui/runtime：** 端口、健康检查、静态资源、Shutdown、SSE、空数据库、部署变量、信号退出、Docker 健康检查。
- **前端：** 优先测试纯逻辑和高风险交互；不为覆盖率数字一次性引入重型 E2E 框架。
- **文档闭环：** 更新 Issue5 状态、Build6 验收结果、Design5 当前状态和 README；历史文档不改写。
- **证据分层：** 自动测试、race、build/vet、Docker、浏览器人工、真实云 API、SMTP/收件箱、Webhook 分别记录，不互相替代。

---

## 六、Issue5 对应关系

| Issue5 项目 | Build6 Step | 计划结果 |
|-------------|-------------|---------|
| R5-01 目标关联丢失 | Step 5 | version 2 导入使用显式 ID 映射 |
| R5-02 EventBus panic | Step 1 | 取消订阅不关闭 channel |
| R5-03 TAG 快照越界 | Step 1 | 本轮 TAG 显式传递 |
| O5-01 前端依赖漏洞 | Step 6 | 受控审计与升级 |
| O5-02 CI 无 race | Step 1 | CI 强制 race |
| O5-03 高影响测试不足 | 各 Step + Step 7 | 修复随测，末步补齐 |
| O5-04 端口 TOCTOU | Step 3 | 单一 listener |
| O5-05 HTTP 生命周期 | Step 3 | 显式 Server 和 Shutdown |
| O5-06 持久化校验 | Step 4、Step 5 | 普通 API 与导入共用边界 |
| A5-01 Headless 模式 | Step 2 | 扩展为 CLI + Headless 全移除 |

---

## 七、明确不包含的范围

- 配置包密码加密；
- 登录、鉴权或 CSRF 系统；
- `.env` 到 SQLite 的迁移器；
- version 1 配置迁移器或兼容壳；
- CLI 的替代命令；
- 云平台 API 算法重构；
- 修改增量添加、精确删除和 TAG 标识契约；
- 同步日志导入导出；
- 扫描缓存导出；
- Desktop、托盘、开机自启；
- 重型前端 E2E 框架；
- 为覆盖率数字进行无风险收益的测试扩张；
- 改写 `HistoryDocs/` 内历史记录。

---

## 八、风险与控制

| 风险 | 控制 |
|------|------|
| 明文配置包泄露密钥 | 危险确认、POST、no-store、不写日志、文档警告 |
| 全量导入误清空配置 | 完整预校验、单事务、分阶段失败回滚测试 |
| 规则目标关联失效 | 显式 ID 映射，不依赖自增序列 |
| 热重载混用配置 | Step 1 先完成 TAG 快照修复 |
| SSE 断开导致 panic | Step 1 先修复并加入 race CI |
| 移除 Headless 后 Docker 不可用 | 保留部署变量并做真实 HTTP 健康验收 |
| 删除 CLI 后误执行参数启动服务 | 未知参数非零退出 |
| HTTP Shutdown 中断同步 | HTTP 有界收尾，Syncer 仍等待当前轮次 |
| 依赖升级污染业务审查 | 依赖升级独立放在 Step 6 |
| 旧配置文件语义不确定 | 明确拒绝 version 1 |

---

## 九、统一验收门禁

每个 Step 运行其专项测试；最终至少执行：

```bash
cd webui/frontend
npm ci
npm run build
npm audit --omit=dev

cd ../..
go test ./... -race
go vet ./...
go build ./...
docker compose -f docker-compose.yml.example config --quiet
docker build -f build/Dockerfile -t fwalizer:build6 .
git diff --check
```

说明：

- 前端必须先构建 `dist`，再运行依赖嵌入资源的 Go 门禁；
- `npm audit` 结果按 Step 6 的风险边界解释，不把开发服务器问题直接描述为已确认的生产 WebUI 漏洞；
- Docker、浏览器、真实云 API、SMTP 和 Webhook 验收必须单独记录；
- 自动测试、mock 或 build 成功不得替代真实外部链路证据。

---

## 十、变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.0 | 2026-09-22 | 建立构建前方案：整合完整配置包、CLI/Headless 移除及 Issue5 全部事项；所有 Step 均未开始 |
