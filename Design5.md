# Design5.md — FWAlizer 设计记录（当前）

> **文档定位：** 本文档是 FWAlizer 的当前设计记录（设计大方向、架构构想与决策记录，非强制，供参考），承接已存档的 [Design1-4](./HistoryDocs/)。
> 编码约束遵循 [AGENTS.md](./AGENTS.md)（唯一强要求）；[Build6.md](./Build6.md) 与 [Build7.md](./Build7.md) 均为已完成的构建记录（Build7 Step 0～7 已完成）；当前 P1-01 TAG 所有权与目标级同步实施合同见 [Issue7.md](./Issue7.md)（Step 0 文档合同已完成，Step 1～5 待实施）。问题历史见 [Issue5.md](./Issue5.md) 与 [Issue6.md](./Issue6.md)。
> **Build7 实施状态（2026-09-28）：** 本文档第三节描述的 **version 2 配置包设计已由 Build7 的 version 3 取代并实施完成**：version 3 是唯一导入/导出版本（version 1/2 与其他版本直接 400）；`GET/PUT /api/alerts` 为四对象严格契约（触发策略 / 邮件含主题正文 / Webhook / Uptime Kuma Push）；新增 `POST /api/alerts/test-email`、唯一 `OperationalHealth` 计算源（`internal/health`）+ 30 秒内部监督器 + `GET /api/health/operational`（`/api/health` 保持静态存活语义）。逐步证据见 [Build7.md](./Build7.md) 第八节。**未执行的真实外部验收**（真实 SMTP/收件箱、Webhook、Uptime Kuma HTTP/Push、真实云 API、远端 CI）见 [ProdTestList.md](./ProdTestList.md)，不得写成通过。
> **实施状态：** 2026-09-24 Build6 Step 1、Step 2、Step 3 与 Step 4 已验收通过：运行时已收束为唯一 WebUI + SQLite，CLI、`.env` Headless、业务环境变量入口和 `webui_port` 业务设置已从代码与当前文档移除，监听参数只由三个部署变量提供；HTTP 生命周期已形成最终形态（同步 listener、仅 `EADDRINUSE` 降级、显式 `http.Server`、`Wait`、幂等 `Shutdown`、两类 SSE 服务器级退出、main 统一收尾）；普通 API 最小持久化校验边界已落地（统一严格解码与 1 MiB、领域校验与归一化、`RowsAffected`/引用检查、事务化 settings/alerts、删除被引用目标 409、500 安全文案、日志级别动态更新）。远端 GitHub Actions 结果已由 2026-09-27 tag `v2.0.0` 的真实运行 `36300428681`（成功）确认，详见下段与 Issue5 O5-02。
>
> **Build6 Step 5（当时的 version 2 完整配置包与原子运行时切换）已于 2026-09-27 验收通过：** 工程实现与本地自动门禁完成，代码于 2026-09-24 提交（`c35eb9d`）；2026-09-27 用户确认生产 WebUI、真实云/DNS及同步链路真机验收通过。跨实例不同自增历史的人工交叉导入因当前无该使用场景而免除，底层 ID 映射继续由自动化覆盖。真实 Email/SMTP/收件箱与 Webhook 验收按用户决定移交后续处理，不阻塞 Step 5；远端 GitHub Actions 结果继续由 Issue5 O5-02 跟踪。
>
> 本文档其余内容是已固定的目标契约；Step 6（前端依赖受控升级）阶段 A 与阶段 B 的工程实施、本地自动门禁、Docker 构建与容器 health/stop 均已完成（`nanoid`/`brace-expansion` lockfile 内修复，`vite 8.3.1` + `@vitejs/plugin-vue 6.0.9` 升级，`esbuild`/`rollup` 由 `rolldown`/`lightningcss` 取代，完整 audit 与生产依赖 audit 均为 0 漏洞，CI 双阻断 audit 门禁已落地，Node 固定为 `node:24.21-alpine` / `node-version: '24.21.0'`，前端源码、`vite.config.ts`、`tsconfig.json` 与业务代码未改动）；2026-09-27 用户确认浏览器真机回归通过，Step 6 已验收通过。**Step 7（高影响路径补测、真实验收与文档闭环）的自动部分已于 2026-09-27 完成：** 自动补测（notifier/syncer/provider/webui/api/webui 的高影响空白）、统一验收门禁（前端构建与两条 audit、`go test ./... -race`、`vet`、`build`、compose config、`docker build`）、真实二进制与 Docker 容器验收（空库、health、静态资源、非 root、healthy、SIGTERM/docker stop、端口释放、日志脱敏）均已真实通过；并按用户确认的最小边界修复 Issue6 A10/A5/A7/A6/A8（reset 拒绝 `null`、pause/resume 单一运行时写入口、恢复立即一轮、ticker 已发布状态守卫、完整导入确定重置 DNS 熔断计数）；**其后按用户一次性授权完成 Issue6 批次 1（A1、A12：阿里云应用层超时、超时错误进入重试）、批次 3（A20、A16：Stop 硬门控、Start/Wait/Run 重复调用契约）、批次 2（A2：SMTP deadline 与每渠道在途上限）、批次 4（A3、A18、A13、A15 与 A11 接口部分）、批次 5（A4、A9、A14、A17：SQLite busy_timeout、损坏 targets、错误处理、迁移失败）、批次 6（A11 收尾）与批次 7（A19：移除 Windows 支持；A8：AGENTS 措辞收口）**；剩余为批次 8 的低风险清理。真实云/DNS/浏览器证据按 2026-09-27 用户真机确认继承，不重复执行。**Step 7 已于 2026-09-27 ✅ 验收通过。** 远端 GitHub Actions 取得真实结果：tag `v2.0.0` 触发的运行 `36300428681` 成功（含远端 `go test -race -v ./...` 与两条阻断式 audit），并真实推送 `ghcr.io/alcaprophet/fwalizer:2.0.0`/`2.0`/`2`，Issue5 O5-02 关闭。真实 Email/SMTP/收件箱（PT-B6-08）与 Webhook（PT-B6-09）经用户 2026-09-27 明确决定跳过、由用户自行处理，属**人工验收免除**（沿用 PT-B6-04 先例），不阻塞 Step 7，**但这两个外部链路仍无真实通过结论，不得写成已通过**。

---

## 一、目标产品形态

FWAlizer 最终只保留：

```text
WebUI 单二进制 + SQLite + Docker/服务器部署
```

“移除 Headless”特指移除 `.env` 驱动、无 WebUI 的业务配置和同步模式，不移除 Linux 服务器或 Docker 的无图形桌面部署能力。用户在服务器或容器中运行单二进制，通过浏览器访问 WebUI 完成全部业务配置。

最终形态不保留：

- `.env` Headless 业务模式、自动模式检测或 `.env` 业务配置解析；
- `version`、`validate`、`backup`、`restore` 或任何替代 CLI；
- 隐藏兼容参数、弃用过渡周期或 `.env` 到 SQLite 迁移器；
- Desktop、托盘、开机自启或原生桌面打包。

程序最终只接受零个业务命令行参数；出现任意参数时输出错误并以非零状态退出，不得意外启动 WebUI。

---

## 二、配置边界

### 2.1 部署参数

仅保留三个进程部署环境变量：

| 变量 | 用途 | 固定边界 |
|------|------|---------|
| `FWALIZER_DATA_DIR` | SQLite、pidfile 和持久化数据目录 | 未设置或空白时使用平台默认目录 |
| `WEBUI_HOST` | HTTP 监听地址 | 默认 `127.0.0.1`；Docker 示例显式使用 `0.0.0.0` |
| `WEBUI_PORT` | HTTP 监听端口 | 默认 `60200`；必须是 `1～65535` 的十进制整数 |

这三项不写入 SQLite、不进入配置包、不被配置导入覆盖。数据库中现有 `webui_port` 残留键统一忽略，不增加专门迁移。

### 2.2 唯一业务配置源

SQLite 是唯一业务配置源，并只通过 WebUI 管理：

- 腾讯云和阿里云访问密钥；
- 云资源目标和域名规则；
- TAG、同步间隔、DNS、DNS 超时和失败阈值；
- 日志级别、同步开关和主题；
- 邮件告警、SMTP 凭据和 Webhook 告警。

云凭据不保留环境变量 override，避免页面、连接测试、资源扫描、同步和导出使用不同的有效凭据源。

---

## 三、version 3 完整配置包（当前）

### 3.1 定位与 Schema

配置包是 SQLite 业务配置的完整可迁移表示，不是 SQLite 文件备份。新导出只生成 version 3，使用独立强类型 DTO，包含：

- UTC RFC3339 导出时间；
- 带正数且唯一 `export_id` 的目标；
- 通过 `target_export_ids` 引用目标的规则；
- 腾讯云/阿里云完整凭据；
- TAG、同步、DNS、日志和主题设置；
- `alerts.policy`、`alerts.email`（含主题/正文与 SMTP 密码）、`alerts.webhook`；
- `monitoring.uptime_kuma_push`。

所有固定字段必须存在，数组和对象不得为 `null`。新导出补齐全部默认值；version 1/2 或其他版本均返回 400，不做迁移、猜测或字段补全。精确当前 Schema 以 [Build7.md](./Build7.md) 与 `AGENTS.md` §9.1 为准；[Build6.md 第三节](./Build6.md#三version-2-完整配置包契约) 只是被 version 3 取代的历史记录。

### 3.2 安全边界

version 3 是明文完整敏感快照，安全等级等同生产 Secret 或 SQLite 数据库备份。

- 导入、导出都使用危险级卡片确认；
- 导出使用 POST、`Cache-Control: no-store` 和 Blob 下载；
- 请求体、响应体、密钥、SMTP 密码和 Webhook URL 不进入日志或错误响应；
- README 必须警告禁止提交 Git、上传公共网盘或通过不可信渠道传输；
- 本阶段不增加配置包加密、签名或密钥托管。

### 3.3 覆盖和保留语义

| 数据 | 导入语义 |
|------|---------|
| 目标、规则、设置、云凭据、告警 | 完整原子替换 |
| 规则目标引用 | 显式映射 `export_id → 新数据库 ID` |
| `sync_logs` | 不导出，导入时保留 |
| `scanned_resources` | 不导出，导入成功时清空 |
| SQLite 自增序列 | 不导出、不重置 |
| WebUI host/port、数据目录、pidfile、Docker 映射 | 不导出、不处理 |

导入必须先完成大小限制、严格解码、全部字段和引用校验，再在一个 SQLite 事务内替换业务配置。提交前基于新 ID 和显式凭据构造候选运行时，任一失败完整回滚。

---

## 四、持久化与 HTTP 边界

- 目标仍被规则引用时返回 HTTP 409，要求先修改规则；
- 规则的 `targets` 必须显式提供（省略返回 400）；空数组仍表示“适用于全部目标”，不得因字段缺失而静默扩大范围；
- 目标/规则的更新与删除按 `RowsAffected=0` 判定 404；
- TAG Trim 后非空，禁止方括号和控制字符，最多 48 个 Unicode 字符；
- 普通 JSON 请求最大 1 MiB，配置导入最大 10 MiB，超限返回 413；
- 固定结构 DTO 使用严格解码，拒绝未知字段、尾随 JSON 和多个顶层值；
- 请求错误为 400，状态冲突为 409，不存在的更新/删除对象为 404，超限为 413，内部错误为 500；
- 校验保持轻量，不实时验证地域、云资源 ID 或 DNS 服务器连通性。

HTTP Server 的最终边界为 `ReadHeaderTimeout=5s`、`IdleTimeout=120s`，全局 `ReadTimeout` / `WriteTimeout` 保持为零，避免周期性中断 SSE。HTTP shutdown 最多等待 10 秒，两类 SSE 通过服务器级 shutdown 信号主动退出；Syncer 仍无超时等待当前轮次完成。

---

## 五、运行时一致性

同步和模拟测试的运行时状态由 Config、Provider 集合、ClientPool、Resolver 和其它相关设置组成。

- Provider 凭据使用显式、不可变值注入，不使用进程级可变全局凭据；
- `cfg + providers + resolver` 在一个锁边界内一次替换；
- 正在运行的同步或模拟测试继续使用旧完整快照；
- 下一轮同步、下一次模拟测试、连接测试和资源扫描完整使用新状态；
- 不允许一轮内混用新旧 TAG、Provider、Resolver 或凭据。

EventBus 取消 channel 订阅时只从订阅表删除记录，不关闭 channel；SSE 生命周期由请求 context 和服务器 shutdown 信号管理。

---

## 六、安全模型和明确排除项

项目继续以内部使用为安全前提，网络访问边界由用户通过防火墙、VPN、Docker 端口映射或反向代理控制。Build6 不增加登录、鉴权、角色权限或 CSRF 系统。

本阶段还不包含：

- 配置包加密、签名或密钥托管；
- version 1 迁移器或兼容壳；
- 本地规则—云规则持久状态表、comment 唯一性/必填校验或在 description 中编码 host/协议/端口；
- 实时地域/资源 ID 合法性验证；
- 同步日志或扫描缓存导出；
- SQLite 文件在线备份；
- Vue Router 5、TypeScript 7 的顺带升级；
- 为覆盖率数字引入重型前端 E2E 系统。

---

## 七、设计继承与实施顺序

[Design4.md](./HistoryDocs/Design4.md) 记录的已实现 UI、同步、扫描、告警和产品标识决策仍保持有效，除非本文档、Build6/Build7 已实施合同或 Issue7 当前定案明确替代。产品显示名、二进制名、数据目录兼容标识和 GHCR 镜像名继续使用 `FWAlizer` / `fwalizer`。

Build6/Build7 的 Step 均已完成，不再作为当前实施顺序。P1-01 后续修复必须严格按 [Issue7.md](./Issue7.md) Step 1～5 串行进行：每次只实施一个 Step，验收后等待用户授权下一步。文档、自动测试、本地进程、Docker、浏览器与真实云 API/SMTP/Webhook 证据必须分层记录，不得互相替代。

---

## 八、P1-01 TAG 所有权与目标级同步定案（2026-09-29）

### 8.1 设计取舍

同步优先保证当前 IP 获得所需防火墙权限，陈旧规则清理为尽力而为的后台收敛。设计接受无法安全定位时的 TAG 残留，不接受为清理旧规则而在新权限生效前制造不可达窗口。

TAG 是唯一操作授权；comment 只是可读备注，不参与身份、Diff、删除或唯一性。所有权只匹配精确 `[TAG]` 或 `[TAG] ` 前缀，不匹配 `[TAG]foo`。非 TAG 规则可只读满足精确等价的功能需求，但永不被接管。

### 8.2 功能身份与流程

云端功能身份改为 `address-family + canonical CIDR + canonical protocol + canonical port + action`，不包含 comment/description/域名/本地 ID/云端 ID。同一云目标的所有本地规则先汇总、解析、平台化展开和去重，再与云端快照比较。

固定流程是 `S0 Describe → Plan → Add → S1 Describe → coverage verification → 安全门 → 条件 Delete → 必要时 S2 验证`。任何 Add 先于 Delete；Add/Describe/覆盖验证失败时旧规则全保留。重试从整个目标重新 Describe/Plan，不复用旧快照删除定位。

### 8.3 平台化清理与健康

- Lighthouse 使用功能唯一性 + 当前 TAG + 同快照 `FirewallVersion`；有歧义即保留。
- CVM 使用同一快照的 `PolicyIndex + Version`，避免逐条删除造成索引漂移。
- SWAS/ECS 只使用 S1 稳定 RuleID；ECS 删除每批不超过 100 个。
- 空期望集、DNS 部分失败、平台能力跳过、快照不完整、定位歧义、TAG 修改和配置 reset/import 均不自动清理。
- 所需功能已确认时，清理延后仍为 `success`/healthy；平台能力不支持为 `partial`/unhealthy；DNS、Describe、Add 或覆盖验证失败为 `failed`/unhealthy。

详细数据合同、审计问题依赖、Step 1～5 及验收停止条件统一见 [Issue7.md](./Issue7.md)。

---

## 九、变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.0 | 2026-09-22 | 建立 Build6 当前设计记录；固定 WebUI-only、SQLite 唯一配置源、version 2 完整敏感快照、原子导入、HTTP/SSE 和运行时快照边界 |
| v1.1 | 2026-09-27 | 同步 Build6 Step 7 实际状态：自动补测/统一门禁/真实二进制与 Docker 容器验收/文档闭环完成，Issue6 A10/A5/A7/A6/A8 按用户确认边界最小修复；当时真实 SMTP/收件箱与 Webhook 尚待用户执行，Step 7 记为 ◧（该状态随后由 v1.2 更新为 ✅） |
| v1.2 | 2026-09-27 | Step 7 ✅ 验收通过：远端 Actions 运行 `36300428681` 成功并真实推送 `ghcr.io/alcaprophet/fwalizer:2.0.0`，O5-02 关闭；真实 SMTP/收件箱与 Webhook 经用户决定免除人工验收、由用户自行处理（不写成已通过） |
| v1.3 | 2026-09-27 | 独立核验后的文档一致性修正：实施状态段中"远端 GitHub Actions 仍待确认"改为已确认（运行 `36300428681`）；覆盖率复测值、非法输入用例计数、audit 阻断边界与 GHCR `latest` 标签按实测事实回写到 Build6/Issue5 对应记录 |
| v1.4 | 2026-09-27 | 核验修正批次落地（不涉及 Issue6 条目）：规则 `targets` 显式必填（省略 400）、更新/删除按 `RowsAffected=0` 判定 404、扫描结果查询参数 `cloud_type` 加枚举校验、协调器 commit 后发布收紧为不可失败；同步 AGENTS §9.1 与 Build6 §4.2/§4.3/§12.9 |
| v1.5 | 2026-09-29 | P1-01 正式定案为“TAG 唯一操作授权 + comment 纯可读 + 目标级功能期望集 + 先增后验 + 平台化条件清理 + 可接受残留”；实施合同转入 Issue7 Step 1～5；当前配置包正文由漂移的 version 2 修正为已实施 version 3 |
