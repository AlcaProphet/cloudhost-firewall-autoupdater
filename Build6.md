# FWAlizer 功能构建计划（Build6：当前构建方案）

> **文档定位：** 本文档是 FWAlizer 当前已固定口径的分步实施方案（Build 文档仍为非强制执行建议，唯一强要求是 AGENTS.md），整合完整配置导入导出、CLI 与 `.env` Headless 业务模式移除，以及 [Issue5.md](./Issue5.md) 中 R5-01～R5-03、O5-01～O5-06、A5-01 的处理计划。
>
> - 编码指令：[AGENTS.md](./AGENTS.md)（当前唯一强要求）
> - 当前设计记录：[Design5.md](./Design5.md)
> - 当前问题记录：[Issue5.md](./Issue5.md)
> - 已完成构建与设计记录：[HistoryDocs/](./HistoryDocs/)
>
> **授权边界：** 用户已于 2026-09-22 确认本文档的规划方向与 Step 0 文档改动方案。Step 0 完成不代表任一代码 Step 已获实施授权；后续必须每次只实施一个 Step，并在该 Step 验收完成后等待下一步授权。
>
> **实施解释：** 本文中的数据契约、状态码、事务边界、并发不变量、Step 顺序和验收边界是 Build6 的固定口径；第十二节代码为贴合当前仓库的参考实现/伪代码，函数名和文件拆分可做等价调整，但不得改变其标注的不变量。若实现者认为必须改变固定口径，应停止当前 Step，记录冲突、影响和推荐方案，等待用户决策，不得用“实现方便”代替设计决策。

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
| 0 | 文档体系切换与设计契约固定 | 本文档 §一～四、Issue5 A5-01 | ✅ 验收通过 |
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

### 5.1 AI 构建执行协议

后续参与构建的 AI 助手必须按以下协议执行，避免把整份 Build6 当作一次性改造清单：

1. 开始一个 Step 前，重新读取 `AGENTS.md`、本 Step、相关 `Issue5.md` 条目和当前 Git 状态；不得只依据历史会话摘要施工；
2. 先核查本文“当前基线”是否仍与源码一致；如源码已变化，记录差异并判断是否影响固定契约，不直接照抄伪代码；
3. 一次只把一个 Step 标记为 `◧ 进行中`，不得并行进入下一 Step，也不得在当前 Step 顺手完成后续 Step 的重构；
4. 修改前列出本 Step 的文件范围、不变量和专项测试；发现超出范围的必要改动时先说明因果关系；
5. 实现中保留用户已有改动，禁止用 reset/checkout 覆盖工作树；出现与用户改动重叠且无法安全合并时停止并报告；
6. 专项测试通过后再运行该 Step 的完整验收；失败必须保留真实结果，不得用较窄命令替代原门禁后宣称通过；
7. 只有代码、自动门禁和该 Step 明确要求的人工证据都满足时，才可标记 `✅ 验收通过`；缺真实云、SMTP、Webhook 或浏览器证据时保持 `◧ 进行中`；
8. 每个 Step 完成时在本文件追加“实际改动、自动证据、人工证据、未完成项、偏差/决策”五类记录，并同步 `Issue5.md` 对应状态；
9. 文档更新只能描述已经发生的事实。不得把“目标代码”“伪代码”“测试计划”写成已实现或已验证；
10. 当前 Step 验收完成后停止，等待用户授权下一 Step。

### 5.2 每个 Step 的开始/结束模板

开始施工时在对应 Step 下临时加入或更新：

```markdown
**实施状态：** ◧ 进行中（YYYY-MM-DD）

- 当前 HEAD：`<commit>`
- 工作树基线：干净 / 有以下用户改动：`...`
- 本 Step 文件范围：`...`
- 固定不变量：`...`
- 本轮不处理：`...`
```

完成或暂停时使用：

```markdown
**实际证据（YYYY-MM-DD）：**

- 实际改动：...
- 自动检查：`命令` → 通过/失败（关键结果）
- 人工检查：已执行/未执行；证据边界为 ...
- 未完成项：无 / ...
- 与计划偏差：无 / 原因、影响、用户决定
- 状态：✅ 验收通过 / ◧ 进行中
```

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

**实际验收（2026-09-22）：**

- `Design4.md`、`Build5.md`、`Issue4.md` 原文迁入 `HistoryDocs/`，历史文档合计 13 份；
- 新建 `Design5.md`，`AGENTS.md`、本文档、`Issue5.md` 和 README 当前文档指针已切换；
- AGENTS、Design5、Build6 和 Issue5 对唯一运行形态、三个部署变量、version 2 敏感快照、数据保留、校验和实施顺序表述一致；
- README 保留现行 CLI/Headless 使用说明，并增加 Build6 目标尚未实施的阶段警示；
- 根目录当前文档链接检查和 `git diff --check` 通过；本 Step 未修改代码、依赖或运行时行为。

### Step 1：并发正确性基线与 CI race 门禁

- **目标：** 修复 EventBus 取消订阅竞态和同步轮次 TAG 快照越界，并让 CI 持续运行 race detector。
- **实施参照：** §12.2 当前基线、§12.14 EventBus 参考修复；本 Step 只传递本轮 TAG，不提前实现 §12.5 的完整运行时状态。
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
- **实施参照：** §12.3 的 `DeploymentConfig` 边界和 §12.13 的 main 目标顺序；本 Step 只完成参数/配置源收束，显式 HTTP 生命周期留给 Step 3。
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
- **实施参照：** §12.13；SSE 必须由同一个 server shutdown channel 退出，但 EventBus 仍沿用 Step 1 的“不关闭订阅 channel”契约。
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
- **实施参照：** §12.4、§12.7～12.10、§12.15；先形成严格 DTO、事务/引用边界和配置变更协调器骨架，不提前切换 version 2 或显式 Provider 凭据。
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
- **实施参照：** §12.3～12.12；必须把 Step 4 协调器骨架收束成完整不可变状态发布，删除分次 reload 和包级可变凭据，不能只实现配置包 JSON 表面协议。
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

Issue5 R5-01 中“凭据不导入”的旧安全边界已被本阶段用户决策明确替代为“version 2 导出并导入完整凭据”；Step 0 已更新当前 Issue5，旧口径只保留在 Git 历史。

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
| 本机工具链 | Go 1.26.6、Node 26.7.0、npm 11.19.0 | 项目 CI/Docker 仍按 Go 1.25、Node 24 验收 |
| 前端生产构建 | `npm ci` 和 `npm run build` 通过 | 本机 `~/.npm` 有 root 所有文件，本次使用临时 cache 绕过；不是仓库问题 |
| Go 静态与编译门禁 | `go vet ./...` 和 `go build ./...` 通过 | 仅证明当前代码基线 |
| Compose 配置 | `docker compose -f docker-compose.yml.example config --quiet` 通过 | 未在本 Step 构建镜像或运行容器 |
| `npm audit` | 3 high、1 moderate | 当前 lockfile 基线；Step 6 前不声称已修复 |
| `npm audit --omit=dev` | 1 high | `nanoid` 经依赖链被统计；需 Step 6 修复 |
| `npm audit fix --dry-run` | 可将 nanoid 3.3.16→3.3.19、brace-expansion 2.1.2→2.1.7 | dry-run 未修改 lockfile；Vite/esbuild 仍需主版本升级 |
| Vite 8 研究 | Node 20.19+/22.12+；Vite 8 改用 Rolldown | 来自 Vite 官方说明，实施时需重新核对最新稳定版和迁移指南 |
| 真实外部链路 | 未执行 | 本次仅构建前文档/源码/依赖核验，不代表云 API、浏览器、Docker、SMTP 或 Webhook 已通过 |

本次核验同时确认并修正文档中的三个错误前提：EventBus 取消竞态不保证“最多一个”在途事件；`http.Server.Shutdown()` 不能单独保证 SSE 主动退出；多次独立 reload 不能构成完整运行时原子快照。

---

## 十二、实现基线、固定不变量与参考伪代码

### 12.1 如何使用本节

本节用于把 §一～十的设计落到当前仓库，降低后续 AI 构建时自行补全设计的空间。

- 标注“**必须**”的是 Build6 固定不变量；实现可以改名、拆文件或选择等价标准库写法，但结果必须满足；
- 标注“**参考**”的代码只表达依赖方向、锁边界、事务顺序和错误边界，不要求逐字复制；
- 伪代码省略的 error 处理在真实实现中仍必须补全，不能因为示例简化而忽略；
- 当前源码仍处于 Step 0 完成、Step 1 未开始的过渡状态；本节的“目标接口”不代表已经存在；
- 如当前代码与本节基线不同，先判断是仓库后来已实现、文档过期，还是出现偏离；不得同时保留两套语义。

### 12.2 2026-09-22 当前源码基线映射

| 关注点 | 当前实现位置 | 当前问题 | Build6 目标归属 |
|--------|-------------|---------|-----------------|
| 启动模式与信号 | `main.go`、`app/mode.go`、`app/cli.go`、`app/app.go` | CLI、env/WebUI 双模式并存；HTTP 启动错误无法反馈给 main | Step 2、3 |
| 部署/业务 ENV | `config/env.go` | `.env` 同时承载业务配置和监听参数 | Step 2 |
| SQLite Schema/CRUD | `config/store.go` | 写入方法多为单语句；RowsAffected、跨表事务和一致快照不足 | Step 4、5 |
| 运行时配置 | `config.Config` | 业务配置与 `WebUIHost/WebUIPort/Mode` 混合 | Step 2 |
| 云凭据 | `provider/credentials.go` | 包级可变全局值，连接测试/扫描会覆盖同步使用的凭据 | Step 5 |
| SDK Client 复用 | `provider/common.go` 的 `ClientPool` | pool 不持有不可变凭据，client 创建闭包读取全局值 | Step 5 |
| 同步热重载 | `syncer/syncer.go` | `Reload`、`ReloadProviders`、`ReloadResolver` 分次应用；`retrySync` 越过快照读取 TAG | Step 1、5 |
| EventBus/SSE | `notifier/bus.go`、`webui/api/sync.go` | 取消时关闭 channel，可与锁外 Publish 发送竞态；SSE 只看 request context | Step 1、3 |
| 日志 SSE | `webui/api/logstream.go` | 广播器自身锁内发送/关闭无同类 panic，但 handler 没有 server shutdown 信号 | Step 3 |
| HTTP listener | `webui/server.go` | 先探测端口再 `ListenAndServe`，存在 TOCTOU；无显式 `http.Server` 生命周期 | Step 3 |
| 普通 API 解码 | `webui/api/*.go` | 无统一大小限制、未知字段/尾随值未拒绝、路径 ID 宽松解析 | Step 4 |
| settings/alerts | `webui/api/settings.go`、`alerts.go` | map 任意键、多次独立写入，可能部分成功 | Step 4 |
| version 1 导入导出 | `webui/api/settings.go` | GET 导出、无敏感配置、直接复用 DB ID，导入会破坏规则引用 | Step 5 |
| 前端导入导出 | `webui/frontend/src/views/Settings.vue` | `window.open` GET 下载、旧安全文案、成功后非完整刷新 | Step 5 |
| 前端依赖 | `webui/frontend/package*.json` | 审计基线见 §十一 | Step 6 |

实施者应优先在这些现有边界上收束，不创建第二套 store、第二个事件总线或平行 Web server。删除旧实现后再更新本表的“当前问题”，不能让旧/新入口长期共存。

### 12.3 最终配置模型和所有权边界

最终必须明确区分三类数据，不再用一个 `config.Config` 混装：

```go
// 参考：仅启动时读取，不进入 SQLite、不热重载、不导入导出。
type DeploymentConfig struct {
    DataDir string
    Host    string
    Port    int
}

// 参考：SQLite 业务配置的完整、已归一化值。
// 发布到运行时后视为不可变；切片必须深拷贝后再发布。
type RuntimeConfig struct {
    Credentials     provider.Credentials
    Targets         []TargetConfig
    DomainRules     []DomainRule
    Tag             string
    Interval        time.Duration
    DNS             string
    DNSTimeout      time.Duration
    DNSFailThreshold int
    LogLevel        string
    SyncEnabled     bool
    Theme           string
    Email           AlertEmailConfig
    Webhook         AlertWebhookConfig
}

// 参考：一次构建并一次发布的完整运行时状态。
type RuntimeState struct {
    Config    RuntimeConfig
    Pool      *provider.ClientPool
    Providers []provider.Provider
    Resolver  *dns.Resolver
    Breaker   *dns.CircuitBreaker
}
```

固定所有权：

1. `DeploymentConfig` 由 main 启动时读取一次；`WEBUI_HOST/PORT` 变化必须重启进程才生效；
2. `RuntimeConfig` 只由已校验的 SQLite 快照或已校验的导入 DTO 构造；handler 不直接拼半套配置；
3. `RuntimeState` 发布后不可修改。同步轮次、Dry Run、连接测试和资源扫描开始时各获取一次指针快照，并在该操作全程使用同一快照；
4. `Providers`、`Targets`、`DomainRules` 等 slice 在发布前深拷贝；不得把可继续 append/修改的请求 DTO slice 直接放入状态；
5. `ClientPool` 持有创建时的不可变 `Credentials`，SDK client 的 cache key 至少区分 cloud type、region 和账户标识；不得再读取包级全局凭据；
6. 连接测试和扫描可按请求目标临时创建 Provider，但必须使用请求开始时取得的同一 `RuntimeState.Pool/Credentials`；不得重新从数据库零散读取四个密钥；
7. 普通配置变更保留既有 DNS 熔断计数；阈值变更通过线程安全 clone/setter 生效。完整导入允许创建新 breaker 并清空计数；
8. `theme` 是业务配置但不参与同步器；仍属于导入导出完整快照；
9. `sync_enabled` 的持久化真值在 SQLite，运行时镜像由统一协调器在 commit 后应用；pause/resume 不绕开该协调器。

### 12.4 所有业务配置写入必须经过同一个协调器

**必须新增一个进程内配置变更协调器**（名称可不同），串行覆盖以下入口：目标/规则增删改、settings、alerts、pause/resume、reset、配置导入。仅在 Store 各自方法内加 SQLite 事务还不够，因为会出现“后提交先应用、先提交后应用”的运行时回退。

```go
// 参考：锁覆盖 DB 事务、候选状态构造、commit 和运行时 apply。
// buildCandidate 只构造本地对象和 SDK client，不访问云 API/DNS/SMTP/Webhook。
type ConfigCoordinator struct {
    mu      sync.Mutex
    store   *config.Store
    runtime *RuntimeManager
    alerts  *AlertManager
    level   *slog.LevelVar
}

func (c *ConfigCoordinator) Mutate(
    mutate func(tx *sql.Tx) error,
    breakerPolicy BreakerPolicy,
) error {
    c.mu.Lock()
    defer c.mu.Unlock()

    tx, err := c.store.BeginTx()
    if err != nil { return err }
    committed := false
    defer func() {
        if !committed {
            if rbErr := tx.Rollback(); rbErr != nil && !errors.Is(rbErr, sql.ErrTxDone) {
                slog.Error("回滚配置事务失败", "error", rbErr)
            }
        }
    }()

    if err := mutate(tx); err != nil { return err }

    snapshot, err := c.store.LoadBusinessSnapshotTx(tx)
    if err != nil { return err }
    candidate, err := BuildRuntimeState(snapshot, breakerPolicy)
    if err != nil { return err }
    alertSet := BuildAlertSet(snapshot.Alerts) // 不发网络请求，构造后应用不失败

    if err := tx.Commit(); err != nil { return err }
    committed = true

    // 以下步骤必须是无 error 的内存操作；顺序固定且在返回 HTTP 成功前完成。
    c.level.Set(parseSlogLevel(snapshot.LogLevel))
    c.alerts.Apply(alertSet)
    // RuntimeState 最后发布：任何取得新状态的操作都必然看见新日志/告警配置。
    c.runtime.Apply(candidate)
    return nil
}
```

固定要求：

- handler 在调用 `Mutate` 前完成 JSON 解码；领域校验既可在锁前做一次快速拒绝，也必须在事务内对依赖数据库的引用/存在性再检查；
- 协调器锁内禁止真实云 API、DNS、SMTP、Webhook 和任意可能长期阻塞的外部 I/O；SDK client 构造必须确认是本地操作；
- 候选构造失败则 rollback，数据库和旧运行时均不变；commit 失败不得 apply；
- commit 之后不能再调用会返回 error 的 reload 函数；若某组件当前只能失败式重载，先重构为“预构造候选 + 无失败 Apply”；日志级别和告警集合先应用，`RuntimeState` 最后发布，避免新运行时操作观察到旧告警/日志配置；
- HTTP 成功响应只能在 Apply 完成后写出，因此响应完成后启动的新操作必然取得新状态；响应前已开始的操作允许继续使用旧状态；
- reset 与 import 使用同一协调器；不得保留一个直接 `ResetAll()` 后异步 `notifyReload()` 的旁路；
- 只读导出不使用变更锁，但必须使用独立只读事务取得内部一致快照；
- 若以后支持多进程共享同一个 SQLite，本进程 mutex 不再足够，必须另行设计；Build6 仍以 pidfile 保证单实例为前提。

### 12.5 不可变运行时状态与同步轮次快照

参考接口：

```go
type RuntimeManager struct {
    mu    sync.RWMutex
    state *RuntimeState
}

func (m *RuntimeManager) Snapshot() *RuntimeState {
    m.mu.RLock()
    defer m.mu.RUnlock()
    return m.state // state 发布后不可变，允许共享指针
}

func (m *RuntimeManager) Apply(next *RuntimeState) {
    m.mu.Lock()
    m.state = next
    m.mu.Unlock()
}

func (s *Syncer) syncAll() {
    state := s.runtime.Snapshot()
    // 本轮只传 state.Config.Tag、state.Providers、state.Resolver、state.Breaker。
    // 禁止下游函数再次读取 s.runtime、s.cfg 或全局凭据。
    forEachCloudInParallel(state, func(p provider.Provider, rule config.DomainRule) {
        s.syncDomain(state, p, rule)
    })
}

func (s *Syncer) retrySync(
    p provider.Provider,
    rule config.DomainRule,
    resolved []dns.ResolvedIP,
    tag string,
) (added, deleted int, err error) {
    // 每次重试重新 Describe/Diff，但 owned/description 始终只使用参数 tag。
}
```

调度控制与状态快照是两个概念：

- 状态 Apply 一次替换完整指针；ticker reset、pause/resume/立即触发通过单个带版本/状态的 control message 交给 `Run()` goroutine；
- control channel 应可合并重复通知，但不能因 buffer 满而永久丢失最终状态。参考做法是容量 1 的“最新状态通知”加原子/锁内当前状态，消费时重新读取最新快照；
- `false → true`：更新 ticker 后立即触发一轮；`true → true`：只按新 interval reset ticker，不立即同步；`true → false`：停止后续 ticker/trigger，当前轮完成；
- `TriggerSync` 在暂停状态返回 409，排队中的 trigger 在消费前也重新检查 enabled，不能在暂停后意外启动；
- `Stop()` 必须通过 `sync.Once` 幂等关闭；`Wait()` 可多次安全等待；停止开始后 reload/control 不得造成新一轮同步；
- Dry Run 不受暂停开关影响，但与同步一样取得一个完整不可变快照；同一时刻仍只允许一个 Dry Run。

必须测试的顺序场景：A/B 两个 settings 请求、settings 与 import、pause 与 import、reset 与 target create 并发。测试通过 channel/barrier 强制交错，最终 SQLite、`RuntimeManager.Snapshot()`、ticker/enabled、日志级别和告警订阅必须全部对应最后一次成功提交，且 `go test -race` 通过。

### 12.6 显式凭据与 ClientPool 参考形态

```go
package provider

type Credentials struct {
    TencentSecretID  string
    TencentSecretKey string
    AliyunAccessKeyID string
    AliyunAccessKeySecret string
}

type ClientPool struct {
    mu          sync.Mutex
    credentials Credentials // 构造后不变，不提供 setter
    clients     map[string]any
}

func NewClientPool(credentials Credentials) *ClientPool {
    return &ClientPool{credentials: credentials, clients: make(map[string]any)}
}
```

Provider factory 可以继续接收 pool，但各 provider 的 client 创建闭包只能读取 `pool` 内凭据。必须删除 `provider.SetCredentials` 和四个包级凭据变量；测试中也不得通过重设全局值模拟账户切换。

以下场景必须覆盖：旧状态使用账户 A 正在同步时导入账户 B；连接测试/扫描在导入响应后只能使用 B；旧同步重试仍只能使用 A 的已有 Provider/ClientPool，不得中途切换到 B。

### 12.7 Store 查询边界和事务内复用

为避免导出/导入事务中意外调用 `s.db` 跳出事务，Store 的底层查询和写入 helper 必须接收最小接口：

```go
type DBTX interface {
    ExecContext(context.Context, string, ...any) (sql.Result, error)
    QueryContext(context.Context, string, ...any) (*sql.Rows, error)
    QueryRowContext(context.Context, string, ...any) *sql.Row
}

func loadTargets(ctx context.Context, q DBTX) ([]TargetConfig, error) { /* ... */ }
func loadRules(ctx context.Context, q DBTX) ([]DomainRule, error) { /* ... */ }
```

固定要求：

- 只读导出使用 `BeginTx(ctx, &sql.TxOptions{ReadOnly: true})`，所有读取都传该 tx；读取完成后 commit；
- 导入、settings、alerts 和引用检查的全部语句都传同一写事务；
- `AddTargetTx` 必须返回 `LastInsertId()`，不得只返回 error；
- Update/Delete 检查 `RowsAffected()`：0 → 404 领域错误，不能把不存在当成功；
- 删除目标时，在同一事务中检查规则 `targets` JSON 引用再删除；当前 Schema 未建关系表，因此必须有覆盖所有规则的可靠查询/解析测试；
- 规则写入前在同一事务中确认每个 target ID 存在；正数、去重检查先于查询；
- 所有 rows 都要 `defer rows.Close()` 并返回 `rows.Err()`；Rollback/Commit/Close error 按适用语义处理，不能裸忽略；
- 不引入级联删除；不修改 `sqlite_sequence`；
- Step 5 可以重构现有 Tx helper，但不得保留会跳出事务的同名旁路供导入调用。

### 12.8 严格 JSON 解码 helper

普通 JSON 请求和配置导入必须共用同一语义，只有 size limit 不同。

```go
func decodeJSON(w http.ResponseWriter, r *http.Request, limit int64, dst any) error {
    r.Body = http.MaxBytesReader(w, r.Body, limit)
    dec := json.NewDecoder(r.Body)
    dec.DisallowUnknownFields()

    if err := dec.Decode(dst); err != nil {
        var tooLarge *http.MaxBytesError
        if errors.As(err, &tooLarge) { return ErrBodyTooLarge }
        return ErrInvalidJSON
    }
    var extra json.RawMessage
    if err := dec.Decode(&extra); !errors.Is(err, io.EOF) {
        if err == nil { return ErrMultipleJSONValues }
        var tooLarge *http.MaxBytesError
        if errors.As(err, &tooLarge) { return ErrBodyTooLarge }
        return ErrInvalidJSON
    }
    return nil
}
```

真实实现的错误类型要保留安全的字段路径/原因，并按以下顺序处理：大小超限 413；语法、类型、未知字段、尾随值 400；领域校验 400；不存在 404；引用/状态冲突 409；内部错误 500。500 响应使用通用文案并带服务端 request/error ID（如实现），详细 error 只写服务日志；任何层级都不得记录请求 body。

必需字段不能只依赖 Go 零值判断。导入 wire DTO 对 scalar 使用指针，对 object 使用指针，对 array 使用 `*[]T` 或自定义 presence 类型；缺失与 `null` 均拒绝，空数组则接受。例如：

```go
type RuleWire struct {
    Host            *string `json:"host"`
    Protocol        *string `json:"protocol"`
    Ports           *string `json:"ports"`
    Action          *string `json:"action"`
    TargetExportIDs *[]int  `json:"target_export_ids"`
    Comment         *string `json:"comment"`
    EnableIPv6      *bool   `json:"enable_ipv6"`
}
```

导出使用非 pointer 的 value DTO，保证所有字段必然出现、数组编码为 `[]` 而非 `null`。不得为了导入 presence 检查而让导出产生 `null`。

### 12.9 普通 API 的精确请求/响应边界

为避免前端把 GET 响应原样回传后夹带内部键，Step 4 固定以下口径：

- `GET /api/settings` 只返回 §4.4 中 11 个可编辑键，并补齐默认值；不返回 `sync_enabled`、`webui_port` 或数据库未知键；
- `PUT /api/settings` 是部分更新：使用 11 个 pointer 字段的固定 DTO，至少出现一个字段；省略字段保持不变，显式空字符串只对四个凭据字段合法；未知字段 400；
- `sync_enabled` 只通过 pause/resume 或 version 2 导入修改，通过 `GET /api/sync/status` 读取；
- `GET /api/alerts` 返回完整 `email` 与 `webhook` 对象；`PUT /api/alerts` 两个对象均为必需字段且每个子字段都必需，一次事务覆盖保存，不能用 `null` 表示“不改”；
- 目标/规则 POST/PUT 使用不含 `id` 的 request DTO；响应中的持久化对象可包含 DB ID；客户端提交 `id` 属于未知字段并返回 400；
- test-connection request 固定为 `cloud_type/region/resource_id` 三字段；scan request 固定为 `cloud_type/region` 两字段；均走 1 MiB/严格解码和 §四基础校验；
- `POST /api/config/reset` 接受单一空对象 `{}`；未知字段或非对象返回 400；成功前通过协调器完成新空状态 Apply；
- 成功消息可保持现有 `{ "message": "..." }` 形态；错误统一为 `{ "error": "安全文案" }`，前端不得依赖数据库/SDK 原始错误全文。

设置部分更新的参考 DTO：

```go
type SettingsPatch struct {
    TCAccessID       *string `json:"tc_access_id"`
    TCAccessKey      *string `json:"tc_access_key"`
    AliAccessID      *string `json:"ali_access_id"`
    AliAccessKey     *string `json:"ali_access_key"`
    Tag              *string `json:"tag"`
    Interval         *string `json:"interval"`
    DNS              *string `json:"dns"`
    DNSTimeout       *string `json:"dns_timeout"`
    DNSFailThreshold *string `json:"dns_fail_threshold"`
    LogLevel         *string `json:"log_level"`
    Theme            *string `json:"theme"`
}
```

`dns_fail_threshold` 在普通 settings API 中继续使用字符串是为了维持当前前端表单/API 形态；进入领域层后必须严格转为正整数。version 2 配置包中它仍是 JSON number，不能混为字符串。

### 12.10 校验、归一化和旧数据库行为

固定执行顺序为：presence/type → Trim/大写等归一化 → 单字段校验 → 跨字段校验 → 数据库引用/存在性校验 → 写入。Store 只接收已归一化领域值，但仍返回底层错误；不能依靠前端验证。

- Trim 后保存的字段：target region/resource ID、rule host/ports、TAG、DNS、邮件 host/port/from/to、Webhook URL/channel；rule comment、四个云凭据、SMTP username/password 作为用户文本或不透明凭据按原值保存；
- 枚举比较先 Trim 再转固定大小写；存储使用文档规定的规范值；
- 配置包内 `export_id` 必须全局唯一；普通规则的 `targets` 数组和导入规则的 `target_export_ids` 数组中，每项都必须为正数且同一数组内唯一，重复时返回 400，不静默去重；不额外禁止两个目标记录指向相同云资源；
- `comment` 允许空；禁止控制字符只针对 TAG，不扩展成未批准的全局文本限制；
- 邮件 `to_addr` 的多收件人语法继续由现有 notifier 约定处理，本阶段不引入新的邮箱解析器；
- URL 校验只接受 absolute `http/https` 且 host 非空，不主动发请求；
- 新安装缺失 setting 使用 §3.1 默认值。已有数据库中“缺失或空白的非凭据默认键”按缺失处理；已有非空但非法值不得静默改成默认值，应返回带键名但不含值的启动/重载错误；
- 四个云凭据允许缺失/空值；禁用状态下告警其他字段仍需类型正确，只有启用时才要求 host/from/to 或 URL 非空；
- `LoadBusinessSnapshotTx` 与 API/import 必须调用同一组领域校验函数，避免“能写入但重启加载失败”。

### 12.11 version 2 导出参考流程

```go
func (d *Deps) handleConfigExport(w http.ResponseWriter, r *http.Request) {
    ctx := r.Context()
    tx, err := d.Store.BeginReadOnlyTx(ctx)
    if err != nil { writeInternal(w); return }
    defer rollbackUnlessCommitted(tx)

    snapshot, err := d.Store.LoadBusinessSnapshotTx(ctx, tx)
    if err != nil { writeInternal(w); return }
    bundle := ToBundleV2(snapshot, time.Now().UTC())
    if err := tx.Commit(); err != nil { writeInternal(w); return }

    // 先完成所有可能失败的 marshal，再写 response header，避免半个附件。
    body, err := json.MarshalIndent(bundle, "", "  ")
    if err != nil { writeInternal(w); return }
    body = append(body, '\n')

    w.Header().Set("Content-Type", "application/json; charset=utf-8")
    w.Header().Set("Content-Disposition", attachmentName(bundle.Metadata.ExportedAt))
    w.Header().Set("Cache-Control", "no-store")
    w.WriteHeader(http.StatusOK)
    if _, err := w.Write(body); err != nil {
        slog.Warn("发送配置导出失败", "error", err)
    }
}
```

附加固定点：导出数组按数据库 ID 升序稳定排序；每条规则的 `target_export_ids` 保留规则内目标顺序还是排序必须唯一化。Build6 固定为**按 export_id 升序输出**，以获得可审查的稳定文件；导入不依赖数组顺序表达优先级。metadata 时间精度按 `time.RFC3339` 输出 UTC `Z`。

### 12.12 version 2 导入参考流程

```go
func (d *Deps) handleConfigImport(w http.ResponseWriter, r *http.Request) {
    var wire BundleV2Wire
    if err := decodeJSON(w, r, 10<<20, &wire); err != nil {
        writeDecodeError(w, err)
        return
    }
    bundle, err := ValidateAndNormalizeBundle(wire)
    if err != nil { writeFieldError(w, err); return }

    err = d.Coordinator.Mutate(func(tx *sql.Tx) error {
        if err := clearImportOwnedTables(tx); err != nil { return err }

        idMap := make(map[int]int64, len(bundle.Targets))
        for _, target := range bundle.Targets {
            newID, err := insertTargetTx(tx, target.Value())
            if err != nil { return err }
            idMap[target.ExportID] = newID
        }
        for _, rule := range bundle.Rules {
            dbRule := rule.Value()
            dbRule.Targets = remapIDs(rule.TargetExportIDs, idMap)
            if err := insertRuleTx(tx, dbRule); err != nil { return err }
        }
        if err := replaceAllSettingsTx(tx, bundle.Settings); err != nil { return err }
        if err := replaceAlertsTx(tx, bundle.Alerts); err != nil { return err }
        if _, err := tx.Exec("DELETE FROM scanned_resources"); err != nil { return err }
        // 不触碰 sync_logs，不触碰 sqlite_sequence。
        return nil
    }, ResetBreaker)
    if err != nil { writeSafeMutationError(w, err); return }

    writeJSON(w, http.StatusOK, map[string]string{"message": "导入成功"})
}
```

`clearImportOwnedTables` 的概念顺序固定为先删 `rules` 再删 `targets`，再删 `settings/alert_email/alert_webhook`；即使当前 Schema 没有 foreign key，也按依赖顺序实现。`replaceAllSettingsTx` 必须显式写入 version 2 的完整键集合，不遍历任意 map；不能把未知键从旧数据库带入新快照。

导入预校验必须在打开写事务前完成所有纯数据检查，包括 version、metadata、必需字段、枚举、时长、TAG、告警、export ID 唯一性和引用闭包。事务内只重复依赖数据库/写入结果的检查，并完成 ID 映射和候选构造。错误路径不得返回部分映射、部分写入或部分 reload。

### 12.13 HTTP Server 和 main 生命周期参考

```go
type Server struct {
    mu           sync.Mutex
    httpServer   *http.Server
    listener     net.Listener
    serveDone    chan error
    shutdownOnce sync.Once
    shutdownCh   chan struct{}
}

func (s *Server) Start() (int, error) {
    ln, err := net.Listen("tcp", net.JoinHostPort(s.host, strconv.Itoa(s.port)))
    if errors.Is(err, syscall.EADDRINUSE) {
        ln, err = net.Listen("tcp", net.JoinHostPort(s.host, "0"))
    }
    if err != nil { return 0, err }

    hs := &http.Server{
        Handler: s.mux, ReadHeaderTimeout: 5 * time.Second, IdleTimeout: 120 * time.Second,
    }
    s.publishStartedState(ln, hs)
    go func() { s.serveDone <- normalizeServeError(hs.Serve(ln)) }()
    return ln.Addr().(*net.TCPAddr).Port, nil
}
```

main 的目标顺序固定：读取/校验部署参数 → 建数据目录/pidfile → 开数据库 → 加载并校验业务快照 → 预构造运行时/日志/告警 → 同步绑定 HTTP 成功 → 启动 Syncer → 等待 OS 信号或 Serve 错误 → 同时开始 HTTP shutdown 与 Syncer stop → HTTP 最多 10 秒、必要时 Close → Syncer 无超时 Wait 当前轮 → 关闭 Store/pidfile → 按原因返回 0 或非零。

两个 SSE handler 的 select 都必须包含：数据 channel、`r.Context().Done()`、`serverShutdownCh`。server shutdown 分支只返回，由 defer unsubscribe；不得依赖关闭 EventBus/LogBroadcaster 的订阅 channel 来驱动退出。

### 12.14 EventBus 参考修复

```go
func (b *EventBus) SubscribeChan() (<-chan Event, func()) {
    b.mu.Lock()
    id := b.nextID
    b.nextID++
    ch := make(chan Event, 32)
    b.chanSubs[id] = ch
    b.mu.Unlock()

    var once sync.Once
    return ch, func() {
        once.Do(func() {
            b.mu.Lock()
            delete(b.chanSubs, id)
            b.mu.Unlock()
            // 必须不 close(ch)：Publish 可能已持有该 channel 的快照。
        })
    }
}

func (b *EventBus) Publish(event Event) {
    b.mu.RLock()
    subs := append([]Subscriber(nil), b.subscribers[event.Type]...)
    chans := make([]chan Event, 0, len(b.chanSubs))
    for _, ch := range b.chanSubs { chans = append(chans, ch) }
    b.mu.RUnlock()
    // 锁外通知；channel 满则丢本次事件。
}
```

接口订阅者 slice 也必须复制，不能只复制 channel map。Subscriber 回调仍异步且错误隔离；本 Step 不改变事件顺序保证，也不新增可靠消息队列。

### 12.15 Step 文件归属与禁止跨步清单

| Step | 允许形成的核心结构 | 本 Step 禁止提前完成 |
|------|-------------------|----------------------|
| 1 | EventBus 快照/不关 channel；TAG 参数链；CI race | 不引入完整 RuntimeManager、凭据注入或 HTTP shutdown 重构 |
| 2 | `DeploymentConfig`；零参数 WebUI-only main；删除旧入口 | 不顺手实现 listener 生命周期或 v2 导入 |
| 3 | 显式 `http.Server/listener/Wait/Shutdown`；SSE shutdownCh | 不改变业务 DTO、配置包 Schema或 Provider 凭据结构 |
| 4 | 严格解码、领域校验、RowsAffected/引用检查、事务 settings/alerts、协调器骨架 | 不提供半成品 version 2；不删除 version 1 后留下无导入导出状态，协议切换留给 Step 5 |
| 5 | RuntimeState、显式 Credentials、v2 export/import、完整协调器应用、前端危险确认 | 不升级 Vite 主版本或扩张云 API 算法 |
| 6 | lockfile/构建器受控升级 | 不混入业务 API/运行时重构 |
| 7 | 剩余高影响测试、真实证据、文档闭环 | 不用 mock 关闭真实外部验收项 |

如果 Step 4 的协调器骨架需要 RuntimeManager 才能保持编译，可先提供只封装现有 reload 的最小接口，但不得声称已达到 Step 5 的完整原子状态；Step 5 必须替换掉分次 reload。反之，不得为了“少改一次”在 Step 4 提前完成 Provider 全局凭据移除。

### 12.16 测试夹具和失败注入基线

为使验收可重复，新增测试优先采用以下轻量方式，不引入重型框架：

- SQLite：每个测试独立 `t.TempDir()` 数据库；需要制造阶段失败时创建临时 trigger，例如对目标/规则/settings/alerts 某次 INSERT 执行 `RAISE(ABORT, 'fixture failure')`，测试结束自动随临时 DB 消失；
- HTTP：`httptest.NewRecorder/NewRequest` 测 handler；Server listener/shutdown 使用 `127.0.0.1:0` 与真实 `net.Listener`；
- 并发：barrier channel 精确控制 Publish/Unsubscribe、A/B mutation、reload/sync retry 的交错，避免只靠 `time.Sleep` 猜竞态；
- 云 API：Provider mock 记录 Describe/Create/Delete 参数和调用顺序；不访问真实云时明确标为 mock；
- 敏感信息：给四个云密钥、SMTP password、Webhook URL 放置唯一 sentinel，扫描 HTTP body、捕获日志和 error 文本，任何出现都失败；配置导出 body 是唯一允许包含这些 sentinel 的 HTTP 响应；
- 前端：Blob 下载、filename fallback、导入失败不 reload、成功整页 reload 可抽成纯函数/最小组件测试；真实浏览器仍按 Step 5/7 单独验收；
- 进程：用临时数据目录启动真实二进制，验证参数退出、信号、端口占用、Serve 失败和退出码；不得在单元测试中向测试宿主进程发 SIGTERM；
- Docker：health 与 stop 使用实际容器；记录镜像 tag、启动命令、health 输出和停止耗时。

失败注入至少覆盖：清表、目标插入、规则插入、settings、email、webhook、scanned_resources 清空、候选 Provider 构造、commit。每一项都断言旧数据库完整、旧 RuntimeState 指针仍生效、未改变日志级别/告警订阅、未发成功响应。

### 12.17 文档和证据完成定义

Build6 最终关闭前，必须能从本文追溯：

```text
固定契约
  → 对应 Step 和代码入口
  → 自动测试名称/命令
  → 本地进程或浏览器证据
  → 真实云/SMTP/Webhook 证据（适用时）
  → Issue5 状态
```

“源码存在”“测试通过”“Docker build 成功”“浏览器打开”“真实云写入成功”是不同证据层。记录时必须写明环境和边界，不使用“全部验证完成”概括替代。任何用户尚未执行/确认的真实外部验收都保留为待办，不由 AI 推断通过。

---

## 十三、变更记录

| 版本 | 日期 | 说明 |
|------|------|------|
| v1.0 | 2026-09-22 | 建立构建前方案：整合完整配置包、CLI/Headless 移除及 Issue5 全部事项；所有 Step 均未开始 |
| v1.1 | 2026-09-22 | 完成逐 Step 构建前核验；固定 version 2 Schema、目标删除 409、TAG/JSON/HTTP 边界、SSE 关闭、原子运行时切换和 Vite 8 升级口径；补齐研究证据、测试矩阵与授权门禁 |
| v1.2 | 2026-09-22 | Step 0 验收通过：切换当前文档体系，建立 Design5，同步 AGENTS/Issue5/README 边界并存档 Design4/Build5/Issue4 |
| v1.3 | 2026-09-22 | 进一步固定 AI 串行构建协议、当前源码映射、配置变更协调器、不可变运行时快照、严格 DTO/Store/HTTP/EventBus 边界，并加入贴合当前仓库的参考代码、失败注入和证据模板；未修改业务代码或 Step 状态 |
