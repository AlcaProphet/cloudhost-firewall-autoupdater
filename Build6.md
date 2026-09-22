# FWAlizer 功能构建计划（Build6：待实施方案）

> **文档定位：** 本文档是 FWAlizer 下一阶段已固定口径的构建前实施方案（Build 文档仍为非强制执行建议，唯一强要求是 AGENTS.md），整合完整配置导入导出、CLI 与 `.env` Headless 业务模式移除，以及 [Issue5.md](./Issue5.md) 中 R5-01～R5-03、O5-01～O5-06、A5-01 的处理计划。
>
> - 编码指令：[AGENTS.md](./AGENTS.md)（当前唯一强要求；在 Step 0 获批并完成前，本文档不得覆盖其现行要求）
> - 当前设计记录：[Design4.md](./Design4.md)
> - 当前已完成构建方案：[Build5.md](./Build5.md)
> - 本阶段问题输入：[Issue5.md](./Issue5.md)
>
> **授权边界：** 用户已于 2026-09-22 确认本文档的规划方向，并于同日确认本次核验补充的五项固定口径。本文档完善不代表 Step 0 或任一代码 Step 已获实施授权；正式构建必须从 Step 0 开始，每次只实施一个 Step，并在该 Step 验收完成后等待下一步授权。

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
- 移除 `FWALIZER_MODE`、`TARGETS`、`RULES`、业务设置和云凭据等业务环境变量入口；
- 移除 `.env.example`、CLI/Headless 文档、测试和专用运行代码；
- 删除仅服务 CLI 版本输出的 `version` 包和编译期版本注入；
- 程序只接受零个业务命令行参数；出现任意参数时必须输出错误并以非零状态退出，不得意外启动 WebUI；不保留隐藏兼容参数或替代 CLI。

### 1.3 唯一保留的部署变量

仅保留以下三个部署边界：

| 环境变量 | 用途 | 默认值与校验 |
|---------|------|-------------|
| `FWALIZER_DATA_DIR` | SQLite、pidfile 和持久化数据目录 | 未设置时沿用平台默认目录；空白值按未设置处理 |
| `WEBUI_HOST` | HTTP 监听地址 | `127.0.0.1`；Docker 示例显式使用 `0.0.0.0` |
| `WEBUI_PORT` | HTTP 监听端口 | `60200`；必须为 `1～65535` 的十进制整数 |

以上变量不写入 SQLite、不进入配置导入导出包，也不被配置导入覆盖。`webui_port` 不再是数据库业务设置；现有数据库中的同名残留键统一忽略，不增加专门迁移，也不重新导出。

### 1.4 唯一业务配置源

以下配置全部收束到 SQLite，并只通过 WebUI 管理：

- 腾讯云、阿里云访问密钥；
- 云资源目标和域名规则；
- TAG、同步间隔、DNS、DNS 超时、DNS 失败阈值；
- 日志级别、同步开关、主题；
- 邮件告警、SMTP 凭据、Webhook 告警。

配置导入导出是 SQLite 业务配置的完整可迁移表示，不是数据库文件备份，也不恢复运行历史、部署参数或数据库内部状态。

---

## 二、已固定的构建前决策

下列事项已完成研究和用户确认，实施时不得重新隐式选择其他方案：

| # | 决策点 | 固定口径 |
|---|--------|---------|
| 1 | 运行形态 | 只保留 WebUI + SQLite；删除 CLI 和 `.env` 业务模式 |
| 2 | 环境变量 | 只保留 `FWALIZER_DATA_DIR`、`WEBUI_HOST`、`WEBUI_PORT`；云凭据也不保留 ENV override |
| 3 | 配置包 | 明文 JSON version 2、完整敏感快照、强类型固定结构、覆盖式原子导入；version 1 明确拒绝 |
| 4 | 目标删除 | 目标仍被规则引用时返回 HTTP 409，要求先修改规则；不静默移除引用，不产生意外的“适用于全部目标” |
| 5 | TAG | Trim 后非空；禁止 `[`、`]` 和控制字符；最多 48 个 Unicode 字符，保证 SWAS 50 字符限制下 `[TAG]` 前缀完整 |
| 6 | JSON 大小 | 配置导入最大 10 MiB；其余 JSON 请求最大 1 MiB；超限返回 413 |
| 7 | HTTP 超时 | `ReadHeaderTimeout=5s`、`IdleTimeout=120s`、不设置全局 `ReadTimeout/WriteTimeout`；HTTP shutdown 上限 10s |
| 8 | SSE shutdown | 不依赖 `http.Server.Shutdown()` 自动取消 SSE；两类 SSE 使用服务器级 shutdown 信号显式退出 |
| 9 | 运行时重载 | `cfg + providers + resolver` 必须一次原子替换；当前同步轮次使用旧快照，下一轮完整使用新快照 |
| 10 | 前端依赖 | 先处理现有 semver 范围内修复，再升级 Vite 8 + `@vitejs/plugin-vue` 6；不顺带升级 Vue Router 5 或 TypeScript 7 |
| 11 | 安全边界 | 本阶段不增加登录、鉴权、CSRF 或配置包加密；继续以内部网络边界为前提，但敏感配置包必须显式警告和禁止缓存 |
| 12 | 历史数据 | 导入保留 `sync_logs`，清空 `scanned_resources`；不重置 SQLite 自增序列 |

---

## 三、version 2 完整配置包契约

### 3.1 精确 JSON 结构

新导出固定生成以下结构；示例中的每个字段都是必需字段，不能省略。字符串可按后文规则为空，数组必须是 JSON 数组而不是 `null`。

```json
{
  "version": 2,
  "metadata": {
    "exported_at": "2026-09-22T08:00:00Z"
  },
  "targets": [
    {
      "export_id": 1,
      "cloud_type": "tc_lighthouse",
      "region": "ap-guangzhou",
      "resource_id": "lhins-example"
    }
  ],
  "rules": [
    {
      "host": "example.com",
      "protocol": "TCP",
      "ports": "443",
      "action": "ACCEPT",
      "target_export_ids": [1],
      "comment": "生产 API",
      "enable_ipv6": false
    }
  ],
  "settings": {
    "credentials": {
      "tencent": {
        "secret_id": "AKID...",
        "secret_key": "..."
      },
      "aliyun": {
        "access_key_id": "LTAI...",
        "access_key_secret": "..."
      }
    },
    "tag": "auto-dns",
    "interval": "5m",
    "dns": "223.5.5.5",
    "dns_timeout": "10s",
    "dns_fail_threshold": 5,
    "log_level": "info",
    "sync_enabled": true,
    "theme": "light"
  },
  "alerts": {
    "email": {
      "enabled": false,
      "host": "",
      "port": "587",
      "username": "",
      "password": "",
      "from_addr": "",
      "to_addr": ""
    },
    "webhook": {
      "enabled": false,
      "url": "",
      "channel": "dingtalk"
    }
  }
}
```

固定说明：

- `metadata.exported_at` 使用 UTC RFC3339；仅用于人工识别和审计，导入时校验格式但不参与运行配置；
- `targets[].export_id` 来自导出数据库中的目标 ID，只用于配置包内部引用；必须为正数且唯一；
- 规则数据库 ID 不导出，也不尝试恢复；规则按数组顺序重新插入；
- `target_export_ids` 为空数组表示“适用于全部目标”；非空时每一项必须为正数、同一规则内不得重复，并且都能映射到本包内的目标；
- `credentials` 四个字段必须存在，但允许空字符串，以支持明确清除旧凭据；
- 导出补齐全部默认值：`tag=auto-dns`、`interval=5m`、`dns=223.5.5.5`、`dns_timeout=10s`、`dns_fail_threshold=5`、`log_level=info`、`sync_enabled=true`、`theme=light`、邮件端口 `587`、Webhook 渠道 `dingtalk`；
- 新导出只生成 version 2；version 1 和其他版本均返回 400，不做迁移、猜测或字段补全；
- 使用独立的 `ConfigBundleV2` DTO；不得直接复用包含数据库 `id` 的持久化模型。

### 3.2 严格解码和请求边界

- `POST /api/config/import` 使用 `http.MaxBytesReader` 限制为 10 MiB；超过限制返回 413；
- 只接受一个顶层 JSON 对象；拒绝尾随第二个 JSON 值；
- 使用 `DisallowUnknownFields` 拒绝任意层级未知字段；
- 通过带 presence 信息的解码 DTO 检查必需字段，不能把“字段缺失”误当作 false、0、空字符串或空数组；
- `targets`、`rules` 必须非 `null`，但允许为空数组；空数据库导出的配置包必须可以重新导入；
- JSON/字段/引用错误返回 400，超限返回 413，数据库或内部错误返回 500；
- 错误响应只指出字段路径和原因，不回显原值、请求体、凭据、SMTP 密码、Webhook URL 或完整配置。

### 3.3 导出协议

- 端点固定为 `POST /api/config/export`，请求不携带业务参数；旧 `GET` 端点删除；
- 在一个 SQLite 只读事务中读取目标、规则、设置和告警，保证导出快照内部一致；
- 响应头固定包含：
  - `Content-Type: application/json; charset=utf-8`
  - `Content-Disposition: attachment; filename="fwalizer-config-v2-<UTC时间>.json"`
  - `Cache-Control: no-store`
- 文件名 UTC 时间格式固定为 `20060102T150405Z`；
- 导出 JSON 使用缩进格式，便于离线核查；不得在页面、console 或服务日志中展示响应体。

### 3.4 导出和保留边界

| 数据 | 导出 | 导入处理 | 原因 |
|------|------|---------|------|
| 目标、规则 | 是 | 完整替换并重建引用 | 核心业务配置 |
| 云凭据 | 是 | 完整替换，空值可清除旧值 | version 2 完整快照 |
| TAG/DNS/同步等设置 | 是 | 完整替换并补齐默认 | 唯一业务配置源 |
| 邮件、SMTP、Webhook | 是 | 完整替换 | 完整告警配置 |
| 同步日志 `sync_logs` | 否 | 保留目标实例原日志 | 运行历史，不是业务配置 |
| 扫描结果 `scanned_resources` | 否 | 导入成功时清空 | 派生缓存，可能与新凭据不匹配 |
| WebUI host/port | 否 | 不处理 | 部署参数 |
| 数据目录、pidfile | 否 | 不处理 | 运行和部署状态 |
| SQLite 自增序列 | 否 | 不重置 | 数据库内部实现 |
| Docker 卷和端口映射 | 否 | 不处理 | 宿主环境配置 |

### 3.5 原子导入和运行时切换

```text
限制大小并严格解码
        ↓
校验全部字段、枚举、值、目标 export_id 和规则引用
        ↓
开启 SQLite 事务
        ↓
清空 targets / rules / settings / alerts
        ↓
插入目标并取得 LastInsertId
        ↓
建立 export_id → 新数据库 ID 映射
        ↓
复制规则并重写 target_export_ids
        ↓
写入完整 settings / credentials / alerts
        ↓
清空 scanned_resources；保留 sync_logs
        ↓
基于新 ID 和显式凭据构造候选 Provider / ClientPool / Resolver / Config / Notifier
        ↓
提交事务
        ↓
无失败分支地原子替换 Syncer 状态，并刷新日志级别和告警订阅
```

事务内候选 Provider 构造只创建 SDK client，不调用真实云 API。任一校验、写入或候选构造失败时必须回滚，旧数据库和运行时保持不变，也不触发 reload。Commit 之后的运行时应用只允许锁内状态替换和无错误 setter，不再执行可能失败的数据库读取、网络访问或 Provider 构造，从而避免“接口报错但数据库已经改变”。

配置切换语义固定为：正在执行的同步或模拟测试继续使用旧的完整快照；下一轮同步、下一次模拟测试、连接测试和资源扫描完整使用新配置，不允许混用新旧 TAG、Provider、Resolver 或凭据。

### 3.6 敏感信息边界

完整配置包包含可直接使用的云密钥、SMTP 密码和 Webhook URL，其敏感等级等同于生产 Secret 或 SQLite 数据库备份。

- 前端导入、导出都使用危险级 `NModal preset="card"`，确认按钮 `type="error"`；
- 导出由 `fetch` 获取 Blob 后下载，不使用 `window.open`，不把 JSON 交给通用 JSON 请求封装；
- 后端不得记录导入请求体或导出响应体；
- README 明确禁止将配置包提交 Git、上传公共网盘或通过不可信渠道传输；
- `.gitignore` 增加常见 `fwalizer-config-v2-*.json` 导出文件模式；
- 本阶段不引入配置包密码加密；后续如需要必须另行设计格式版本和密钥管理。

---

## 四、统一最小校验契约

### 4.1 JSON API 通用边界

- 除配置导入外，带 JSON body 的接口统一限制为 1 MiB；
- 固定结构 DTO 拒绝未知字段、尾随 JSON 和多个顶层值；
- 路径 ID 使用 `strconv.Atoi` 严格解析并要求大于 0，不使用会接受前缀数字的宽松扫描；
- 请求错误为 400，目标仍被引用等状态冲突为 409，不存在的更新/删除对象为 404，内部错误为 500；
- 非法请求不得写库、不得部分写入、不得触发 reload。

### 4.2 目标

- `cloud_type` 只允许 `tc_lighthouse`、`tc_cvm`、`ali_swas`、`ali_ecs`；
- `region`、`resource_id` Trim 后必须非空并保存 Trim 后的值；
- 不实时校验地域是否存在于预填列表，不猜测各云资源 ID 的完整格式，不调用云 API；
- 更新/删除不存在的目标返回 404；
- 删除目标前检查规则引用；仍被任一规则引用时返回 409，要求先修改规则；
- 连接测试和资源扫描复用 cloud type、region、resource ID 的相同基础校验。

### 4.3 规则

- `host` Trim 后非空；不增加复杂域名正则；
- `protocol` Trim 并转大写，只允许 `TCP`、`UDP`、`TCP+UDP`、`ICMP`；
- `action` Trim 并转大写，只允许 `ACCEPT`、`DROP`；
- 非 ICMP 的 `ports` Trim 后非空；ICMP 统一归一化为 `ALL`；端口范围的完整合法性继续由现有转换和云 API 边界处理；
- `target IDs` 必须为正数且存在；普通 CRUD 对当前数据库目标校验，导入对本次配置包的 `export_id` 集合校验；
- 空目标数组保留“适用于全部目标”语义；不静默删除、补全或猜测未知引用；
- 更新/删除不存在的规则返回 404。

### 4.4 设置

普通 `PUT /api/settings` 只接受以下键：

```text
tc_access_id, tc_access_key, ali_access_id, ali_access_key,
tag, interval, dns, dns_timeout, dns_fail_threshold,
log_level, theme
```

`sync_enabled` 只由 pause/resume 端点和配置导入写入；前端保存设置时只提交上述可编辑键。`webui_port` 和其他未知键一律返回 400。

- 凭据允许空字符串；
- TAG 遵守 §二第 5 项固定限制；
- `interval`、`dns_timeout` 必须能被 `time.ParseDuration` 解析且大于 0；
- `dns_fail_threshold` 必须为正整数；
- `log_level` 只允许 `debug`、`info`、`warn`、`error`；
- `theme` 只允许 `light`、`dark`；
- DNS 接受 hostname 或 IPv4，可选 `:port`；IPv6 使用 `[address]` 或 `[address]:port`；端口省略时为 53，显式端口必须为 `1～65535`；不测试 DNS 是否在线；
- 多键设置写入使用一个事务，任一键失败时不得留下部分更新。

### 4.5 告警

- 邮件端口必须是 `1～65535`；启用邮件时 host、from_addr、to_addr 必须非空，用户名和密码允许为空以兼容无认证 SMTP；
- Webhook channel 只允许 `dingtalk`、`feishu`、`slack`；启用时 URL 必须是绝对 `http` 或 `https` URL；
- 同一个 `PUT /api/alerts` 中的邮件和 Webhook 使用一个事务，不能只保存其中一半；
- 配置包导入复用相同校验，不为导入另写一套宽松规则。

---

## 五、构建进度追踪和顺序

| Step | 内容 | 主要依据 | 状态 |
|------|------|---------|------|
| 0 | 文档体系切换与设计契约固定 | 本文档 §一～四、Issue5 A5-01 | ☐ 未开始 |
| 1 | 并发正确性基线与 CI race 门禁 | Issue5 R5-02、R5-03、O5-02 | ☐ 未开始 |
| 2 | 移除 CLI 与 `.env` Headless 业务模式 | Issue5 A5-01、本文件 §一 | ☐ 未开始 |
| 3 | HTTP Listener、Server 生命周期与优雅关闭 | Issue5 O5-04、O5-05 | ☐ 未开始 |
| 4 | API 最小持久化校验边界 | Issue5 O5-06、本文件 §四 | ☐ 未开始 |
| 5 | version 2 完整配置包与原子运行时切换 | Issue5 R5-01、本文件 §三 | ☐ 未开始 |
| 6 | 前端依赖受控升级 | Issue5 O5-01 | ☐ 未开始 |
| 7 | 高影响路径补测、真实验收与文档闭环 | Issue5 O5-03 | ☐ 未开始 |

> 状态标记：☐ 未开始 / ◧ 进行中 / ✅ 验收通过

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
Step 5 完整配置包和原子运行时切换
   ↓
Step 6 前端依赖升级
   ↓
Step 7 高影响测试与总验收
```

排序理由：

1. 先固定强要求、设计和实施契约，避免代码与现行 AGENTS.md 冲突；
2. 先修复会导致 panic、数据竞态或同轮配置混用的问题，并让 CI race 覆盖后续修改；
3. 先移除双运行模式，再形成最终 HTTP 生命周期，避免重复修改 main；
4. 先建立普通 API 的统一校验，再让高风险完整导入复用；
5. 完整导入必须同时解决 ID 映射、事务、显式凭据和原子运行时切换，不能拆成互相不完整的半成品；
6. 前端依赖升级单独成步，避免锁文件和构建器变化污染业务审查；
7. 各 Step 随改随测，最终 Step 只补剩余高影响空白并记录分层证据。

---

## 六、分步构建计划

### Step 0：文档体系切换与设计契约固定

- **目标：** 在代码修改前，将已确认的产品、配置、安全和实施契约写入当前项目文档。
- **文件范围：** `AGENTS.md`、新建 `Design5.md`、`Build6.md`、`Issue5.md`、文档索引和 `HistoryDocs/`；README 只增加阶段说明或链接，不提前改写尚未实施的运行方式。
- **处理内容：**
  1. 将 WebUI 单二进制定义为唯一目标运行形态，并写明当前代码仍待 Step 2 收束；
  2. 在 AGENTS.md 写明最终只保留三个部署变量、SQLite 是唯一业务配置源；
  3. 写入 version 2 Schema、明文敏感配置、原子覆盖和数据保留契约；
  4. 写入目标删除 409、TAG 限制、JSON 大小和 HTTP 超时固定口径；
  5. 新建 `Design5.md`，记录本阶段设计决策和明确排除项；
  6. 将已完成的 `Design4.md`、`Build5.md`、`Issue4.md` 原样移入 `HistoryDocs/`，更新根目录和 AGENTS.md 指针；
  7. 将 `Build6.md` 提升为当前构建方案，将 `Issue5.md` 提升为当前问题记录；
  8. 历史文档正文不改写；仅因移动产生的索引说明由当前文档承担；
  9. README 的 CLI/Headless 使用说明保留到 Step 2 与代码同批删除，避免文档先于产品行为失真。
- **验收：**
  - 当前文档对运行形态、配置源、敏感导出、数据保留、校验和顺序表述一致；
  - 根目录当前文档链接全部有效，`HistoryDocs/` 清单数量同步；
  - README 明确区分“当前仍可用”和“Build6 目标”，不声称未实施功能已经完成；
  - `git diff --check` 通过。
- **授权门禁：** 完成后先由用户审阅文档，再决定是否授权 Step 1。

### Step 1：并发正确性基线与 CI race 门禁

- **目标：** 修复 EventBus 取消订阅竞态和同步轮次 TAG 快照越界，并让 CI 持续运行 race detector。
- **R5-02 EventBus：**
  1. `Publish` 在读锁内分别复制接口订阅者 slice 和 channel 订阅表快照，锁外只遍历独立副本；
  2. `SubscribeChan` 取消时只删除订阅表记录，不关闭 channel；
  3. 重复和并发取消保持幂等；SSE 生命周期由请求 context 和 Step 3 的服务器 shutdown 信号控制；
  4. 取消与发布并发时，已经取得快照的一个或多个并发 Publish 可以继续向仍存活的缓冲 channel 投递；不承诺“最多一个事件”，但 handler 退出后不再消费，channel 最终由 GC 回收；
  5. 不使用 `recover`，不在持有全局锁时调用接口订阅者，也不把全部 channel 发送放进全局锁；
  6. `LogBroadcaster` 当前在同一把锁内完成发送和关闭，不属于同一竞态；本 Step 不顺手改变它，Step 3 只接入 shutdown 退出。
- **R5-03 TAG 快照：**
  1. `syncAll` 捕获本轮 TAG；
  2. TAG 显式沿 `syncDomain` → `syncDomainInternal` → `retrySync` 传递；
  3. `retrySync` 的 Describe、OwnedRules、描述生成和全部重试都只使用参数，不再读取 `s.cfg.Tag`；
  4. 新 TAG 只从下一轮同步开始生效；Dry Run 保持使用其完整快照；
  5. 本 Step 不引入大型“轮次上下文”抽象；Step 5 再处理整个运行时状态的一次性替换。
- **O5-02 CI：**
  1. `.github/workflows/docker-publish.yml` 将 `go test -v ./...` 改为 `go test -race -v ./...`；
  2. 前端仍先构建，独立 build 和 vet 继续保留；
  3. Docker 发布产物仍为 `CGO_ENABLED=0`，CI race 所需 CGO 不改变镜像约束；
  4. 暂不加入覆盖率上传、复杂矩阵或全仓 `-count=100`。
- **测试：**
  - 并发 Publish/Subscribe/Unsubscribe、接口订阅 slice 并发修改、重复取消、缓冲满的慢 channel；
  - 真实 SSE 请求 context 退出后订阅表恢复；
  - 同步和重试期间 Reload TAG、本轮旧 TAG、下一轮新 TAG；
  - 测试不得依赖“取消后最多一个事件”的错误假设。
- **验收：**

```bash
go test -race -count=100 ./notifier ./syncer ./webui/api
go test ./... -race
go vet ./...
git diff --check
```

### Step 2：移除 CLI 与 `.env` Headless 业务模式

- **目标：** 运行时收束为唯一 WebUI + SQLite 模式，同时保持 Docker/服务器无桌面部署能力。
- **主要文件：** `main.go`、`app/`、`config/`、`.env.example`、`.gitignore`、`Makefile`、`build/Dockerfile`、`.github/workflows/docker-publish.yml`、`docker-compose.yml.example`、README 和相关测试。
- **处理内容：**
  1. 删除 `app/cli.go`、`app/mode.go` 和只服务 Headless runner 的代码；日志公共能力保留在语义明确的文件中；
  2. 删除 `.env` 加载、TARGETS/RULES 解析、旧 `Config.Validate()` 和专用测试；Step 4 将建立新的持久化 DTO 校验；
  3. 删除 `Config.Mode`，并将 host/port 等启动参数与 SQLite 业务 Config 分离；
  4. 新建轻量运行时部署参数读取，只处理数据目录、监听地址和端口；空白 `FWALIZER_DATA_DIR` 按未设置处理，端口按 §1.3 校验；
  5. 删除四个云凭据和全部业务设置的环境变量入口，不提供 ENV > SQLite 优先级；
  6. 删除 `version` 包、CLI version 输出、Makefile ldflags、Docker `VERSION` ARG 和工作流 build arg；
  7. 删除 `.env.example`、Compose 的 `.env` 挂载和全部 Headless/自动模式说明；
  8. Compose 仅保留三个部署变量，容器内固定 `WEBUI_HOST=0.0.0.0`；
  9. Dockerfile 和 Compose 健康检查只请求 `/api/health`，删除 `pgrep` fallback；
  10. 从 SQLite 加载、设置默认值、前端设置表单和导入导出语义中删除 `webui_port`；数据库残留键忽略；
  11. `len(os.Args) > 1` 时打印“不支持命令行参数”并以非零退出；不保留 `--help`、`--version` 等例外；
  12. README 与代码同批删除 CLI、backup/restore、`.env` 和业务变量说明，并明确配置包是配置迁移方式而不是 SQLite 在线备份。
- **测试：**
  - 空数据库无参数启动；
  - 任意无关 `TARGETS`、`TC_ACCESS_ID`、`INTERVAL` 环境变量不改变 SQLite 配置；
  - 三个部署变量分别生效，非法端口启动失败；
  - 任意命令行参数均非零退出且不监听端口；
  - Docker HTTP 健康检查，确认进程存活但 HTTP 失败时容器仍判为 unhealthy。
- **验收：**

```bash
cd webui/frontend && npm ci && npm run build && cd ../..
go test ./... -race
go vet ./...
go build ./...
docker compose -f docker-compose.yml.example config --quiet
docker build -f build/Dockerfile -t fwalizer:build6-step2 .
git diff --check
```

同时静态检查活跃源码和当前文档不再提供 CLI、Headless、业务 ENV 或 `webui_port` 入口；`HistoryDocs/` 中历史记录不参与残留判定。

### Step 3：HTTP Listener、Server 生命周期与优雅关闭

- **目标：** 合并处理 O5-04 和 O5-05，一次形成最终 HTTP 生命周期，消除端口 TOCTOU，并使 main 可感知服务失败。
- **Listener 和启动：**
  1. `Server.Start()` 同步执行 `net.Listen`、保存 listener 和 `http.Server`、启动 Serve goroutine，然后返回实际端口；绑定失败时 Start 直接返回错误；
  2. 首选 `host:preferredPort`；只有 `errors.Is(err, syscall.EADDRINUSE)` 时才降级到 `host:0`；权限、非法地址等错误原样返回；
  3. 同一个 listener 直接交给 `http.Server.Serve`，不得关闭后重新绑定；
  4. `Server.Wait()` 将 Serve 的非正常错误传给 main；`http.ErrServerClosed`、`net.ErrClosed` 在已进入关闭流程时归一化为正常退出；
  5. main 在开始 Syncer 前必须已经确认 HTTP 绑定成功。
- **Server 参数：**
  - `ReadHeaderTimeout: 5s`
  - `IdleTimeout: 120s`
  - `ReadTimeout: 0`
  - `WriteTimeout: 0`

  不设置短全局读写超时，避免周期性切断 `/api/sync/events` 和 `/api/logs/stream`。
- **关闭顺序：**
  1. `Server.Shutdown(ctx)` 通过 `sync.Once` 幂等关闭服务器级 SSE shutdown channel，并调用 `http.Server.Shutdown(ctx)`；不手工提前关闭同一个 listener，避免 Shutdown 再次关闭 listener 时产生伪错误；
  2. main 收到信号后立即在 goroutine 中启动带 10 秒 context 的 `Server.Shutdown`；该调用先关闭监听入口，再等待普通 HTTP 请求；
  3. 两类 SSE handler 同时监听 request context 与 shutdown channel，立即返回并执行 unsubscribe；
  4. main 启动 HTTP shutdown 后立即调用 `Syncer.Stop()`，阻止当前轮次结束后再开始下一轮；
  5. HTTP 收尾超过 10 秒时，调用 `http.Server.Close()` 强制关闭剩余 HTTP 连接并记录 WARN；该强制关闭不作用于 Syncer；
  6. 无论 HTTP 是否超时，main 仍调用 `Syncer.Wait()` 无超时等待当前同步轮次完成；
  7. 服务自身发生非正常 Serve 错误时，main 走同一收尾路径，完成 Syncer 收尾后以非零状态退出；
  8. 重复 Shutdown、未启动时 Shutdown、Serve 已退出后 Shutdown 均安全且不 double close。
- **测试：**
  - 首选端口、占用后随机端口、权限/非法地址错误；
  - 健康端点、SPA 静态资源、API；
  - 两类 SSE 在 shutdown 信号后退出且订阅数恢复；
  - 普通请求在 10 秒内完成、超时后强制 HTTP 收尾；
  - 重复 Shutdown、启动失败、Serve 运行错误；
  - SIGTERM/SIGINT 和 Docker stop，验证不开始新同步轮次且当前轮次完成。
- **验收：**

```bash
go test -race ./webui ./webui/api ./...
go vet ./...
git diff --check
```

另外执行真实进程信号测试与 Docker stop；自动测试结果不能替代这两项进程级证据。

### Step 4：API 最小持久化校验边界

- **目标：** 让普通 API 和 Step 5 配置导入复用 §四的轻量校验，避免只依赖前端，同时保持“不实时验证地域/资源”的不过度防御边界。
- **实现边界：**
  1. 在 `config` 包中定义可复用的目标、规则、设置和告警校验/归一化函数；不引入第三方 validation 框架；
  2. handler 使用独立请求 DTO，避免客户端写入数据库 ID 或内部字段；
  3. 增加统一严格 JSON 解码 helper，普通 body 上限 1 MiB；
  4. 目标/规则更新和删除检查 RowsAffected，不存在返回 404；
  5. 新增目标引用查询；删除被引用目标返回 409；
  6. 规则写入前验证当前数据库目标引用；
  7. Settings PUT 只提交明确可编辑键，并在单事务内保存；前端不再回传 GET 响应中的任意键；
  8. Alerts PUT 在单事务内保存邮件和 Webhook；
  9. 非法输入在任何写入或 reload 前返回；合法写入完成后才触发一次 reload；
  10. 日志错误和 HTTP 错误不得包含敏感字段值。
- **运行时设置：** 本 Step 建立可动态更新的 `slog.LevelVar`，使普通设置保存后的日志级别即时生效；DNS 失败阈值提供线程安全 setter 并保留已有计数。Step 5 再将 Config、Provider 和 Resolver 合并为一次原子状态替换。
- **测试：**
  - §四全部正常值、Trim/大写归一化、空白、未知枚举、非法时长、零/负值；
  - TAG 括号、控制字符、49 字符拒绝和 48 字符通过；
  - DNS hostname/IPv4/带端口/括号 IPv6，非法端口和未加括号 IPv6；
  - 列表外地域仍可保存；
  - 未知目标引用、被引用目标删除 409、未引用目标删除成功；
  - 多键 settings 和双告警写入中途失败时完整回滚；
  - 1 MiB 上限、未知字段、尾随 JSON；
  - 400/404/409/413/500 状态码和错误脱敏；
  - 非法输入不 reload，合法事务只 reload 一次。
- **验收：**

```bash
go test -race ./config ./webui/api
go test ./... -race
go vet ./...
git diff --check
```

### Step 5：version 2 完整配置包与原子运行时切换

- **目标：** 端到端实现 §三的完整敏感配置快照、R5-01 ID 关联恢复、事务覆盖以及一致的运行时切换。
- **显式凭据与候选运行时：**
  1. 用不可变 `provider.Credentials` 值取代进程级全局凭据；
  2. `ClientPool` 在创建时接收凭据，Provider 工厂和资源扫描从该 pool/显式参数取得凭据；
  3. 连接测试、扫描、启动和 reload 使用同一凭据来源，不再通过 `SetCredentials` 修改全局状态；
  4. 导入事务取得新目标 ID 后，在事务提交前构造候选 Provider 列表、ClientPool、Resolver 和 Config；构造阶段不得调用云 API；
  5. 候选构造失败回滚事务，不影响当前运行时。
- **R5-01 ID 映射：**
  1. 导入前验证 `export_id` 为正且唯一；
  2. 验证每个 `target_export_ids` 都存在于本次目标集合；
  3. 事务插入目标时通过 `LastInsertId()` 获取实际 ID；
  4. 建立 `map[exportID]newDatabaseID`；
  5. 为每条规则创建副本并重写引用，不修改原始请求对象；
  6. 空引用数组保持“适用于全部目标”；
  7. 不重置 `sqlite_sequence`，不强行复用来源数据库 ID。
- **数据库事务：** 同一事务替换 targets、rules、settings、alert_email、alert_webhook，清空 scanned_resources，保留 sync_logs；任何阶段失败完整回滚。
- **运行时应用：**
  1. Syncer 提供一次性 `ReloadState` 接口，在一个锁边界内替换 Config、Provider slice 和 Resolver；
  2. 正在执行的同步/Dry Run 使用旧快照，后续执行使用新快照；
  3. 同步 interval、enabled、DNS 失败阈值同步更新；导入为完整快照，熔断计数允许重置；`false → true` 与 Resume 一致立即触发一轮，`true → true` 只重置 ticker、不额外触发，导入为 false 时当前轮完成后进入暂停；
  4. `slog.LevelVar` 更新日志级别；
  5. 取消旧邮件/Webhook 订阅并按新配置注册；旧的已在途异步通知允许完成；
  6. Commit 后的上述 setter/锁内替换无 error 返回，不重新读库、不构造 Provider、不访问网络；
  7. 导入响应返回前完成运行时应用。
- **后端协议：** 严格执行 §三的 POST 导出、10 MiB 导入、no-store、附件文件名、必需字段和错误脱敏。
- **前端：**
  1. 导入/导出均为危险级卡片确认，文案明确包含全部密钥；
  2. POST 导出后读取 Blob，解析安全的附件文件名或使用固定 fallback；
  3. 导入在前端只做 JSON 语法预检查，后端仍是唯一结构校验边界；
  4. 导入成功后显示成功消息并整页 reload，使主题、凭据状态、设置、告警、目标、规则和扫描缓存统一刷新；
  5. 失败时不 reload，当前页面和运行时保持旧配置。
- **自动测试：**
  - 空数据库和完整数据库导出 Schema、默认值、UTC metadata、响应头；
  - 同实例自增历史、跨实例不同 ID、多目标、一条规则多目标、多规则复用目标、空引用；
  - 重复/零/负 export ID、未知引用、null/缺失/未知字段、version 1、尾随 JSON、10 MiB 上限；
  - 完整凭据和告警覆盖、空值清除旧值、主题/同步开关/日志级别更新；
  - 扫描缓存清空、同步日志保留、自增序列不重置；
  - 使用临时数据库 trigger 制造每个写入阶段失败，验证完整回滚；
  - 候选 Provider 构造失败时回滚且不 reload；
  - 当前同步轮次旧完整快照、下一轮新完整快照；
  - 响应、应用日志和运行日志无密钥泄露。
- **人工验收：** 两个具有不同自增历史的数据库交叉导入；逐项核对业务关系、主题、同步状态和告警；再分别执行连接测试、资源扫描、真实云 API 同步、SMTP/收件箱和 Webhook。各证据单独记录。

### Step 6：前端依赖受控升级

- **目标：** 清除当前 high/critical 审计项，同时把 Vite 主版本变化与业务重构隔离。
- **已核验基线（2026-09-22）：**
  - `npm audit`：3 high、1 moderate；
  - `npm audit --omit=dev`：1 high；
  - `nanoid 3.3.16`、`brace-expansion 2.1.2` 可在现有依赖范围内修复；
  - `vite 5.4.21 / esbuild 0.21.5` 的剩余项需要 Vite 主版本升级；
  - Vite 8 要求 Node `20.19+` 或 `22.12+`，当前 CI/Docker Node 24 满足；
  - Vite 8 使用 Rolldown，属于需要单独验证的构建器变化。
- **阶段 A：现有范围内修复：**
  1. 实施时重新记录 Node/npm 版本和 `npm audit --json`；
  2. 只更新 lockfile 可解析的安全版本，至少使 `nanoid >= 3.3.18`、`brace-expansion >= 2.1.4`；
  3. 禁止 `npm audit fix --force`；
  4. `npm ci && npm run build` 后记录剩余项。
- **阶段 B：构建器升级：**
  1. 阅读 Vite 6、7、8 官方迁移说明和 Vite 8 Rolldown 说明；
  2. 将 Vite 升到 8.x、`@vitejs/plugin-vue` 升到 6.x，并重新生成 lockfile；
  3. 不顺带升级 Vue Router 5、TypeScript 7；Vue、Naive UI、vue-tsc 只在 peer compatibility 或构建需要时做最小兼容更新并记录原因；
  4. 检查 `vite.config.ts`、资源路径、动态路由 chunk、CSS 和生产 dist；
  5. 对比升级前后产物文件和浏览器行为，不只以命令退出码判定成功。
- **安全门禁：**
  - `npm audit --audit-level=high` 与 `npm audit --omit=dev --audit-level=high` 都必须通过；
  - 若仍有 moderate/low，逐项记录“影响范围、是否进入生产静态产物、接受或等待上游”的依据；
  - 不把 Vite dev server 漏洞描述成已确认的生产单二进制漏洞，也不因只发布静态文件而忽略构建链 high 风险。
- **验收：**

```bash
cd webui/frontend
npm ci
npm run build
npm audit --audit-level=high
npm audit --omit=dev --audit-level=high
cd ../..
go test ./... -race
go vet ./...
docker build -f build/Dockerfile -t fwalizer:build6-step6 .
git diff --check
```

人工打开仪表盘、目标、规则、设置、日志、模拟测试和告警页面，并完整复测配置导入导出。研究依据以 [Vite 8 官方说明](https://vite.dev/blog/announcing-vite8) 和 [Vite 官方版本策略](https://vite.dev/releases) 为准。

### Step 7：高影响路径补测、真实验收与文档闭环

- **目标：** 完成 O5-03；不设置任意覆盖率目标，只补高影响行为并形成可追溯的分层证据。
- **notifier：** 接口订阅 clone、注册/取消、channel Publish/取消竞态、重复取消、缓冲满、Subscriber 错误隔离、告警热重载边界。
- **syncer：** Provider/Resolver/TAG/Config 完整快照、重试重新 Diff、部分写入计数、pause/resume、导入开关、停止等待、DNS 阈值更新。
- **provider：** TCP+UDP 拆分、ICMP 端口、IPv4/IPv6、ECS ICMPv6 跳过、描述长度、48 字符 TAG、精确删除、幂等错误和 CVM 上限；只测纯转换和 mock，不声称真实云 API 已验证。
- **webui/runtime：** 指定/随机端口、非占用错误、健康检查、静态资源、Serve 错误、Shutdown、SSE、空数据库、三个部署变量、参数退出、信号和 Docker。
- **配置与 API：** 严格 JSON、校验/状态码、目标删除 409、settings/alerts 原子写入、version 2 一致快照、ID 映射、失败回滚和敏感错误脱敏。
- **前端：** 优先测试可抽取的纯逻辑和高风险交互；不为覆盖率数字一次性引入重型 E2E。导入、导出、清空数据、暂停/恢复保留真实浏览器验收。
- **外部链路验收：**
  1. 至少一个腾讯云目标和一个阿里云目标的连接测试/资源扫描；
  2. 真实 DNS → Diff → 增量写入 → 精确删除；
  3. SMTP 发信并确认收件箱；
  4. 每个实际支持渠道的 Webhook；
  5. 不具备凭据或外部环境时，Step 7 保持 ◧ 进行中，并明确列为待用户执行，不得以 mock 代替后标记完成。
- **文档闭环：** 更新 Issue5 每项状态、Build6 每 Step 实际证据、Design5 当前状态和 README；历史文档不改写。
- **证据分层：** 源码核验、单元/集成测试、race、build/vet、Docker、浏览器人工、真实云 API、SMTP/收件箱、Webhook 分别记录，互不替代。

---

## 七、Issue5 对应关系

| Issue5 项目 | Build6 Step | 固定结果 |
|-------------|-------------|---------|
| R5-01 目标关联丢失 | Step 5 | version 2 使用 `export_id → 新数据库 ID` 显式映射 |
| R5-02 EventBus panic | Step 1 | channel 不关闭；接口和 channel 订阅快照都复制 |
| R5-03 TAG 快照越界 | Step 1、Step 5 | TAG 显式传递；最终运行时状态一次替换 |
| O5-01 前端依赖漏洞 | Step 6 | 范围内修复后升级 Vite 8/plugin-vue 6 |
| O5-02 CI 无 race | Step 1 | CI 强制 race，保留 build/vet |
| O5-03 高影响测试不足 | 各 Step + Step 7 | 修复随测，末步补齐并分层记录 |
| O5-04 端口 TOCTOU | Step 3 | 单一 listener 从绑定到 Serve 不释放 |
| O5-05 HTTP 生命周期 | Step 3 | 显式 Server、SSE shutdown、10s HTTP 收尾 |
| O5-06 持久化校验 | Step 4、Step 5 | 普通 API 与导入共用轻量领域校验 |
| A5-01 Headless 模式 | Step 0、Step 2 | 文档先固定，代码和 README 同步移除 |

Issue5 R5-01 中“凭据不导入”的旧安全边界已被本阶段用户决策明确替代为“version 2 导出并导入完整凭据”；实施时应在 Step 0 更新当前 Issue5 说明，历史记录只保留在 Git 历史，不让当前 Issue 与 Build6 相互矛盾。

---

## 八、明确不包含的范围

- 配置包密码加密、签名或密钥托管；
- 登录、鉴权、权限角色或 CSRF 系统；
- `.env` 到 SQLite 的迁移器；
- version 1 配置迁移器或兼容壳；
- CLI 的替代命令；
- 云平台 API 算法重构；
- 修改增量添加、精确删除和 `[TAG]` 所有权契约；
- 实时地域/资源 ID 合法性校验；
- 同步日志导入导出；
- 扫描缓存导出；
- SQLite 文件在线备份功能；
- Desktop、托盘、开机自启；
- Vue Router 5、TypeScript 7 的顺带升级；
- 重型前端 E2E 框架；
- 为覆盖率数字进行低收益测试扩张；
- 改写 `HistoryDocs/` 内历史记录。

---

## 九、风险与控制

| 风险 | 控制 |
|------|------|
| 明文配置包泄露密钥 | 危险确认、POST、no-store、Blob 下载、不写日志、README 与 gitignore 警告 |
| 全量导入误清空配置 | 必需字段、完整预校验、单事务、候选运行时预构造、失败注入测试 |
| 规则目标关联失效 | `export_id` 显式映射，不依赖自增序列 |
| 删除目标使规则语义扩大 | 被引用目标删除返回 409 |
| 热重载混用配置 | Step 1 固定 TAG；Step 5 原子替换完整运行时状态 |
| Provider 全局凭据混用 | 显式不可变 Credentials 注入 ClientPool |
| SSE 断开导致 panic 或 Shutdown 卡住 | channel 不关闭；服务器 shutdown channel 主动结束 SSE |
| HTTP 端口探测 TOCTOU | 同一 listener 直接 Serve |
| HTTP Shutdown 中断同步 | HTTP 最多 10s；Syncer 当前轮次仍无超时等待完成 |
| 移除 Headless 后 Docker 不可用 | 保留三个部署变量并做真实 HTTP health/Docker 验收 |
| 删除 CLI 后误执行参数启动服务 | 任意参数非零退出 |
| 设置/告警部分写入 | 多字段请求单事务 |
| TAG 过长破坏所有权前缀 | 禁止括号/控制字符并限制 48 Unicode 字符 |
| Vite 主版本引入构建差异 | 单独 Step、官方迁移说明、生产构建和逐路由浏览器复测 |
| 外部链路无条件验收 | 保持 Step 7 进行中并明确待用户证据，不用 mock 冒充 |

---

## 十、统一验收门禁

每个 Step 先运行专项测试；最终自动门禁至少执行：

```bash
cd webui/frontend
npm ci
npm run build
npm audit --audit-level=high
npm audit --omit=dev --audit-level=high

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
- `npm audit` 在 Step 6 前作为已知失败基线记录，Step 6 后 high/critical 必须清零；
- `go test -race` 不能替代 build 和 vet；
- Docker 构建不能替代 Compose 配置、健康检查和 stop 验收；
- 自动测试、mock、源码核验或 build 成功不得替代真实浏览器、云 API、SMTP/收件箱和 Webhook 证据；
- 每个 Step 只更新自身状态和实际证据，不预先标记后续 Step。

---

## 十一、本次构建前核验记录

核验日期：2026-09-22。

| 检查项 | 结果 | 证据边界 |
|--------|------|---------|
| Git 工作树 | 核验开始时干净，`main` 与 `origin/main` 同步 | 不代表远端后续不会变化 |
| `go test ./... -race` | 通过 | 当前已有测试通过；不覆盖本文列出的新增并发/事务场景 |
| 本机工具链 | Go 1.26.4、Node 26.7.0、npm 11.19.0 | 项目 CI/Docker 仍按 Go 1.25、Node 24 验收 |
| `npm audit` | 3 high、1 moderate | 当前 lockfile 基线；Step 6 前不声称已修复 |
| `npm audit --omit=dev` | 1 high | `nanoid` 经依赖链被统计；需 Step 6 修复 |
| `npm audit fix --dry-run` | 可将 nanoid 3.3.16→3.3.19、brace-expansion 2.1.2→2.1.7 | dry-run 未修改 lockfile；Vite/esbuild 仍需主版本升级 |
| Vite 8 研究 | Node 20.19+/22.12+；Vite 8 改用 Rolldown | 来自 Vite 官方说明，实施时需重新核对最新稳定版和迁移指南 |
| 真实外部链路 | 未执行 | 本次仅构建前文档/源码/依赖核验，不代表云 API、浏览器、Docker、SMTP 或 Webhook 已通过 |

本次核验同时确认并修正文档中的三个错误前提：EventBus 取消竞态不保证“最多一个”在途事件；`http.Server.Shutdown()` 不能单独保证 SSE 主动退出；多次独立 reload 不能构成完整运行时原子快照。

---

## 十二、变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.0 | 2026-09-22 | 建立构建前方案：整合完整配置包、CLI/Headless 移除及 Issue5 全部事项；所有 Step 均未开始 |
| v1.1 | 2026-09-22 | 完成逐 Step 构建前核验；固定 version 2 Schema、目标删除 409、TAG/JSON/HTTP 边界、SSE 关闭、原子运行时切换和 Vite 8 升级口径；补齐研究证据、测试矩阵与授权门禁 |
