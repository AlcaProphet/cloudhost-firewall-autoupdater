# Design5.md — FWAlizer Build6 设计记录（当前）

> **文档定位：** 本文档是 FWAlizer 的当前设计记录（设计大方向、架构构想与决策记录，非强制，供参考），承接已存档的 [Design1-4](./HistoryDocs/)。
> 编码约束遵循 [AGENTS.md](./AGENTS.md)（唯一强要求）；详细分步实施和验收见 [Build6.md](./Build6.md)；问题追踪见 [Issue5.md](./Issue5.md)。
> **实施状态：** 2026-09-22 完成 Build6 Step 0 后，本文档记录的是已固定的目标契约；CLI、`.env` Headless 和后续运行时收敛仍须按 Build6 Step 1-7 实施和验收，不得把本文档视为代码已经完成。

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

## 三、version 2 完整配置包

### 3.1 定位与 Schema

配置包是 SQLite 业务配置的完整可迁移表示，不是 SQLite 文件备份。新导出只生成 version 2，使用独立强类型 DTO，包含：

- UTC RFC3339 导出时间；
- 带正数且唯一 `export_id` 的目标；
- 通过 `target_export_ids` 引用目标的规则；
- 腾讯云/阿里云完整凭据；
- TAG、同步、DNS、日志和主题设置；
- 邮件、SMTP 密码、Webhook URL 和渠道。

所有固定字段必须存在，数组不得为 `null`。新导出补齐全部默认值；version 1 或其他版本均返回 400，不做迁移、猜测或字段补全。精确 JSON Schema 以 [Build6.md 第三节](./Build6.md#三version-2-完整配置包契约) 为实施契约。

### 3.2 安全边界

version 2 是明文完整敏感快照，安全等级等同生产 Secret 或 SQLite 数据库备份。

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
- 云平台 API 算法重构或所有权契约变更；
- 实时地域/资源 ID 合法性验证；
- 同步日志或扫描缓存导出；
- SQLite 文件在线备份；
- Vue Router 5、TypeScript 7 的顺带升级；
- 为覆盖率数字引入重型前端 E2E 系统。

---

## 七、设计继承与实施顺序

[Design4.md](./HistoryDocs/Design4.md) 记录的已实现 UI、同步、扫描、告警和产品标识决策仍保持有效，除非本文档或 Build6 明确替代。产品显示名、二进制名、数据目录兼容标识和 GHCR 镜像名继续使用 `FWAlizer` / `fwalizer`。

实施必须严格按 [Build6.md](./Build6.md) 的 Step 0-7 串行进行：每次只实施一个 Step，验收后等待用户授权下一步。文档、自动测试、本地进程、Docker、浏览器与真实云 API/SMTP/Webhook 证据必须分层记录，不得互相替代。

---

## 八、变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.0 | 2026-09-22 | 建立 Build6 当前设计记录；固定 WebUI-only、SQLite 唯一配置源、version 2 完整敏感快照、原子导入、HTTP/SSE 和运行时快照边界 |
