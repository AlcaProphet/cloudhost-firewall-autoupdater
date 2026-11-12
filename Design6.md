# Design6 — EdgeOne 回源同步与 DNS 协同

> 2026-11-12 · 已确认方案的设计记录 · 实施状态见 [Build8](Build8.md)。
> 基线为 `beta` 的 `102d838f3bcc3d1624c32e44ed65f2eb6feaa0b4`。当前约束见 [AGENTS.md](AGENTS.md)，历史依据见 [Design5](HistoryDocs/Design5.md)、[Build7](HistoryDocs/Build7.md) 和 [Issue7](HistoryDocs/Issue7.md)。本文描述目标行为，现有发布和验收状态仍以仓库记录为准。

[SQLite 与迁移](#schema-reference) · [API 与配置包](#api-reference) · [TEO 客户端](#teo-reference) · [预检与调度](#preflight-reference) · [目标写入和确认](#target-reference) · [界面与健康](#view-reference)

## 一、功能与默认值

在左侧栏「域名规则」后新增「EdgeOne 回源」模块，路由为 `#/edgeone`。一个 `ZoneId` 对应一条站点配置，可绑定多个「云资源管理」中的目标，并指定入站协议、端口和地址族。绑定清单代表该站点的全部实际源站。腾讯云的七层域名和四层代理绑定信息可供核对，实际源站范围由配置者确定。选择安全组时，页面说明规则影响整个安全组及其关联实例。

| 设置 | 默认值与行为 |
|---|---|
| 总暂停 | 保留现有 `sync_enabled` 和仪表盘入口，管理两项正式同步的准入。恢复时仅调度已启用的功能。 |
| DNS 同步 | 新实例启用，沿用现有 DNS 间隔 `5m`。可单独关闭。 |
| EdgeOne 同步 | 新实例关闭。启用后，已绑定目标的站点进入调度；首次启用和新增站点安排首轮。 |
| DNS TAG | 新实例和明确清空配置后的默认值为 `auto-ddns`。已存实例的有效 TAG 保留。 |
| EdgeOne TAG | 默认 `edgeone`，在「全局设置」中可自定义，使用现有 TAG 格式和长度校验。 |
| EdgeOne 同步间隔 | 在「全局设置」中可自定义，默认 `72h`，使用现有正时长语法。它控制正常站点查询周期，与 DNS 间隔分别计时。 |

两种 TAG 去除首尾空白后按现有大小写敏感的所有权语义比较。相同值在保存、导入和完整快照校验时拒绝。描述统一使用现有 `[TAG]` 渲染，删前继续严格检查所有权。EdgeOne 首版按无历史 EdgeOne 规则的新功能设计。

## 二、持久化与兼容

- SQLite 保存站点及其目标引用，站点的 `ZoneId` 唯一，绑定至少一个有效目标。重复本地目标和指向同一实际资源的别名按云类型、地域和资源标识识别。多个站点可以指向同一实际目标，规划时聚合所有适用站点。删除被引用的目标沿用 HTTP 409，导入后按 `export_id` 重映射引用。站点、设置及目标校验复用配置协调器，校验和构造候选状态成功后一次提交与发布。
- 旧数据库中 `tag` 缺失或为空白时，有效值原为 `auto-dns`。在首次升级事务中识别已初始化旧库，显式写入原有效值并标记迁移完成。新建库、重启和 reset 各自验证，避免 reset 后又被识别成待迁移旧库。显式保存的 DNS TAG 原样承接其归一化值，云规则由现有描述继续归属。
- 计划导出 version 4，继续严格读取现有 version 3。v4 保存两个功能开关、两个 TAG、两个同步间隔及站点绑定。v3 导入保留原 DNS TAG、DNS 间隔和总暂停，补入「DNS 启用、EdgeOne 关闭、无站点」。如果旧配置原本把 DNS TAG 设为 `edgeone`，升级或 v3 导入时保留 DNS TAG，给新模块初始化为 `edgeone-origin`。显式 v4 重名按校验失败处理。v1、v2 和未知版本继续拒绝。
- 已查实的全局冲突和仍需保护的网段过渡状态保存在本机 SQLite 运行记录中。它们与导出备份的用户配置分开。普通保存、导入和重启后通过正式预检重新判定，显式清空配置时清理对应运行记录。

## 三、EdgeOne 查询与计划

使用腾讯云 TEO Go SDK，按 `ZoneId` 调用 `DescribeOriginACL` 和 `ConfirmOriginACLUpdate`。中国站为 `POST https://teo.tencentcloudapi.com/`，JSON 业务参数为 `ZoneId`，API 版本 `2022-09-01`，地域可省略，签名使用腾讯云 API 3.0 的 `TC3-HMAC-SHA256`。配置查询和确认所需的站点级 CAM 权限，复用现有腾讯云凭据设置。客户端需有请求超时，去除日志中的凭据；查询和确认按腾讯云频率限制调度，站点查询结果供其所有目标复用。 [查询接口](https://cloud.tencent.com/document/product/1552/120408) · [确认接口](https://cloud.tencent.com/document/product/1552/120409) · [签名方法](https://cloud.tencent.com/document/product/1552/73159) · [CAM 权限](https://cloud.tencent.com/document/product/598/99327)

- 初次同步使用 `CurrentOriginACL.EntireAddresses`。有待更新版本时，采用当前版与 `NextOriginACL.EntireAddresses` 的并集，保留版本及生效时间。校验状态、字段完整性和地址族，规范 CIDR 掩码并去重。明确无下一版是有效状态；读取失败、缺少必要字段、无有效网段和非法前缀进入查询失败状态。
- 独立调度保留每项最近结果和下次到期时间。实际改变某项间隔时，从处理该变更开始重新计时；保存相同间隔保留剩余等待，EdgeOne 间隔变更仅调整 EdgeOne 调度。正常周期、首次启用、明确的立即同步、失败重试和确认前查询由同一执行入口协调。配置通知合并、过短间隔下的请求合并和 API 限速都需验证。默认 `72h` 对应腾讯云建议的查询周期，定时到期和请求实际发出时间受在途工作与限速影响。 [腾讯云更新流程](https://cloud.tencent.com/document/product/1552/76086)
- 既有 Provider 继续负责防火墙读写。规划输入支持保留前缀掩码的地址类型，DNS 地址仍规范为 `/32` 或 `/128`。按物理目标串行使用同一规则快照和容量，按功能 key 去重，按 TAG 确定创建与删除归属。一个目标上 EdgeOne 的清理依据包括所有适用站点，即使其中部分站点本次未到查询时间。四平台的能力、S0/S1/S2、腾讯云版本和阿里云稳定删除定位继续生效。 [目标规划器](provider/plan.go) · [目标同步](syncer/target.go)

## 四、写入前全局检查与自动恢复

当 DNS 和 EdgeOne 同时启用时，在第一个目标写入前，从同一运行时配置取所有适用 DNS 规则的解析地址和所有有效 EdgeOne 站点当前及待更新网段。按 IPv4/IPv6 的 CIDR 包含关系判断。DNS 的 IPv4 地址在比较时使用 IPv4 表示，保留既有云端快照地址身份。协议、端口、动作和目标差异均不影响全局重叠判定。没有适用规则或站点时可得到有效空集；查询失败导致的未知集合单独标记。

| 观测 | 正式同步决策 |
|---|---|
| 单项启用 | 只运行已启用功能及其查询，使用该功能的现有安全门。 |
| 两项启用，任一结果证明重叠 | 记录域名、IP、网段和 `ZoneId`，两侧新增、删除及站点确认均为零。部分查询失败时，已取得的正向冲突证据仍生效。 |
| 已查实重叠，后续数据不足或继续重叠 | 保持两侧写入停止，继续只读复查。重启后恢复该状态。 |
| 已查实重叠，后续两侧完整且无重叠 | 自动解除冲突。定时或手动触发均可完成复查，并按调度恢复已启用的功能。 |
| 无已查实的冲突，一侧数据不足 | 停止失败侧，正常侧继续按自己的安全门写入。DNS 失败时，正常的 EdgeOne 仍可在全部绑定目标验证和版本重查通过后立即确认。状态明确标记为数据不足、单侧继续。 |
| 总暂停 | 阻止新一轮正式写入与站点确认，已准入轮沿用现有收尾语义。模拟测试仍可只读运行。 |

已知冲突只在两项同时启用时约束写入。关闭一项可继续另一项；重新同时启用时先复查，状态不足时保留已知冲突。正常 DNS 轮可使用有效的 EdgeOne 查询快照；快照随站点、凭据及计划切换状态失效。冲突判定的覆盖范围是本轮可用数据。允许单侧继续意味着后续可能发现重叠，届时阻止后续写入，保留已经成功写入的规则和实际确认记录。

两项同时启用时，目标重试使用本轮已做全局判定的地址，重新读取云规则、版本和删除定位。下一轮重新解析 DNS。仅启用 DNS 时保持原逐 attempt 解析。EdgeOne 查询失败和地址冲突使用各自的错误状态，DNS 熔断计数继续依据真实解析结果。 [现有调度](syncer/syncer.go) · [DNS 轮次](syncer/dns_round.go)

## 五、覆盖验证、站点确认与旧网段清理

1. 先增量添加，再回读。另一功能 TAG 下的等价残留规则作为目标级冲突报告，禁止把它计入覆盖或改标。手工规则保持既有精确等价和安全删除边界。平台不支持所需地址族、目标容量不足、相关相反动作冲突或读回缺失时，该站点保留待处理状态。
2. 按 `ZoneId` 聚合全部绑定目标的回读证明。同一实际资源只验证一次。`CoverageReady` 只是输入之一，还需验证该站点所需的全部规则及相关冲突。确认前重查站点、当前及待更新版本和两者的规范化网段，校验绑定和相关配置仍有效；已暂停或已关闭站点的确认准入同时复核。通过后自动调用确认，按站点记录结果和 `RequestId`。接口只接受 `ZoneId`，重查与确认之间存在版本变化窗口。 [确认接口](https://cloud.tencent.com/document/product/1552/120409)
3. 确认超时、响应不确定或返回版本已最新时，先读取实际站点状态再决定是否需要继续。站点确认和新版生效分别记录。确认成功、计划时间到达、下一版字段消失和服务重启均不足以单独授权清理。待有效查询证明新版成为当前版本后，在现有安全门内清理过时的 EdgeOne TAG 规则。过渡期间继续保护仍可能使用的网段。腾讯云可能在期限后强制切换，实际状态由查询确认。 [源站防护与更新流程](https://cloud.tencent.com/document/product/1552/76086)

## 六、界面、状态与验收边界

- 「EdgeOne 回源」提供站点与多目标配置、模块开关、版本、最近查询及下次同步、目标覆盖、确认状态和错误。「全局设置」提供 DNS 开关、两种 TAG 及 EdgeOne 间隔。「模拟测试」显示两项功能的有效计划、全局冲突、预计新增及过渡期容量、目标限制。模拟使用正式检查和规划逻辑，保持只读且独立于总暂停，模拟结果本身不改变正式冲突状态。
- 区分无任务、已暂停、地址重叠、数据不足单侧继续、站点待确认与已确认。EdgeOne 的低频等待使用自己的到期时间，运行健康继续使用相应功能的状态，避免把低频等待误报为 DNS 停滞。已查实的冲突保持可见，恢复时记录一次状态变化。
- 本阶段的本地回归、真实站点与目标的权限和响应、跨平台容量以及发布各自记录证据。当前只有设计和基线核验，实施与验收清单见 [Build8](Build8.md)。

## 七、实现参考

下列 SQL、字段与代码用于指导 Step 实施。现有可复用入口以链接指向的仓库文件为准。跨层流程片段中的新函数名是建议接线，不要求原样复制。

<a name="schema-reference"></a>

### 7.1 SQLite 表与迁移

#### 业务配置

沿用 `settings(key TEXT PRIMARY KEY, value TEXT NOT NULL)`，与既有设置一样保存字符串。总暂停仍由专用端点写入。导入和完整快照保留既有告警、监控及凭据字段。

| SQLite key | 新库 / reset | 值及语义 |
|---|---|---|
| `sync_enabled` | `true` | 现有总开关。 |
| `dns_enabled` | `true` | DNS 功能开关。 |
| `edgeone_enabled` | `false` | EdgeOne 功能开关。 |
| `tag` | `auto-ddns` | DNS TAG，沿用现有键。 |
| `interval` | `5m` | DNS 间隔，沿用现有键。 |
| `edgeone_tag` | `edgeone` | EdgeOne TAG。 |
| `edgeone_interval` | `72h` | EdgeOne 同步间隔，沿用现有正时长解析。 |

建议将站点参数存在主表，目标绑定单独存行。首版一站配置一组协议、端口和地址族，所有绑定目标使用这一组参数。站点的防火墙动作固定为 `ACCEPT`。协议沿用既有规则枚举，ICMP 的端口沿用 `ALL` 语义。地址族用 `ipv4`、`ipv6`、`dual` 表示，表单初始选 IPv4，实际使用双栈的站点应选择双栈。保存时检查端口表达式有可用项，防止空输入经现有 `portconv.Parse` 回退成 `ALL`。 [领域校验](config/validate.go) · [端口解析](internal/portconv/portconv.go)

```sql
CREATE TABLE IF NOT EXISTS edgeone_sites (
    zone_id        TEXT NOT NULL PRIMARY KEY CHECK (length(trim(zone_id)) > 0),
    protocol       TEXT NOT NULL CHECK (protocol IN ('TCP', 'UDP', 'TCP+UDP', 'ICMP')),
    ports          TEXT NOT NULL CHECK (length(trim(ports)) > 0),
    address_family TEXT NOT NULL CHECK (address_family IN ('ipv4', 'ipv6', 'dual'))
);

CREATE TABLE IF NOT EXISTS edgeone_site_targets (
    zone_id   TEXT    NOT NULL,
    target_id INTEGER NOT NULL CHECK (target_id > 0),
    PRIMARY KEY (zone_id, target_id)
);

CREATE INDEX IF NOT EXISTS idx_edgeone_site_targets_target_id
    ON edgeone_site_targets (target_id);
```

`edgeone_site_targets.zone_id` 指向 `edgeone_sites.zone_id`，`target_id` 指向现有 `targets.id`。沿用 Store 的事务内引用校验，并在 `LoadBusinessSnapshotTx` 用同一事务的目标集合验证完整闭包。一个站点必须至少有一条有效绑定，空数组不表示全部目标。相同本地 ID、同一实际资源的别名均在同一站点的请求中拒绝；不同站点可以共享实际目标。目标更新如果会让已有站点的两个绑定落到同一资源，也须在最终候选快照中拒绝。历史配置中同一目标存在多个本地记录时，正式规划按实际资源合并，不据此改写存量 DNS 引用。 [Store](config/store.go) · [目标 API](webui/api/targets.go) · [配置协调器](webui/api/coordinator.go)

站点创建、修改绑定和删除使用同一写事务。修改站点配置时将 `zone_id` 当作标识，路径标识与请求体冲突返回 400；要改绑另一站点，按新站点配置。删除站点先删绑定、再删站点及其过渡记录。总 reset 与完整导入先删除站点绑定，再删除站点和目标；完整导入时本机已知全局冲突继续由有效预检解除。运行记录保留与清除规则见下节。数据库删配置不直接发云防火墙请求。

#### 本机运行记录

```sql
CREATE TABLE IF NOT EXISTS sync_address_conflict (
    id               INTEGER PRIMARY KEY CHECK (id = 1),
    dns_name         TEXT NOT NULL,
    dns_address      TEXT NOT NULL,
    edgeone_prefix   TEXT NOT NULL,
    zone_id          TEXT NOT NULL,
    first_seen_at    TEXT NOT NULL,
    last_seen_at     TEXT NOT NULL
);

CREATE TABLE IF NOT EXISTS edgeone_origin_transitions (
    zone_id                 TEXT NOT NULL PRIMARY KEY,
    base_version            TEXT NOT NULL,
    pending_version         TEXT NOT NULL,
    pending_cidrs_json      TEXT NOT NULL,
    protected_cidrs_json    TEXT NOT NULL,
    confirmation_state      TEXT NOT NULL CHECK (
        confirmation_state IN ('not_attempted', 'unknown', 'accepted')
    ),
    confirmation_request_id TEXT NOT NULL DEFAULT '',
    updated_at              TEXT NOT NULL
);
```

- 冲突表缺行表示本机没有已查实且未解除的冲突。记录一条稳定排序的冲突证据即可，字段按真实结果写入，站点引用即使被移除也可保留为历史证据，直至正式复查证明解除。禁止以日志文本、UI 状态或备份中的值恢复这条记录。记录或解除操作失败时，当前轮按已知冲突状态继续阻止写入，并报告存储错误。
- 过渡表的 `base_version` 是发现待更新时的当前版本，`pending_version` 和 `pending_cidrs_json` 表示最新待更新版本及其规范前缀。 `protected_cidrs_json` 保存本次过渡仍需保留的前缀集合。两个 JSON 字段均为稳定排序、去重、非 null 的 CIDR 字符串数组。只在过渡或确认状态变化时写入，查询历史继续使用现有日志体系。站点删除或总 reset 可清除对应记录；完整导入对仍存在的同一站点保留过渡保护，移除不存在站点的记录。配置变化后的旧结果不能写回新配置的状态。
- UTC RFC3339 文本用于运行记录的时间戳。读取坏记录应报告状态不可用并保守保护相关云规则。运行表不存凭据、签名或 SDK 原始载荷，也不进入 v4 配置包。
- 将短事务用于本机状态写入。云查询、S0/S1/S2 和确认调用都在事务外执行。写前、发布前核对操作捕获的配置及站点身份。采用现有锁顺序和 SQLite 事务入口。 [SQLite 事务入口](config/store.go) · [运行时发布](syncer/state.go)

#### TAG 迁移参考

现有 `initTables` 先建表再迁移。首次迁移前在同一个事务内读取 `sqlite_schema`，确认 `settings` 是否已存在，再判断一次性迁移标记。基线未使用 `PRAGMA user_version`，可用它记录这次 Schema 迁移，值由实施时统一分配。标记、实际写入和建表必须同事务提交。 `strings.TrimSpace` 用于识别旧空白值，保持和现有读取行为一致。

```text
begin immediate transaction
  inspect settings table existence and migration version
  create/upgrade schema
  if this migration is new:
      if previous settings table existed and old tag is missing or blank:
          persist tag = "auto-dns"
      persist migration version
  commit
```

新库与已迁移库的 reset 通过新默认值解析为 `auto-ddns`，reset 保留 Schema 迁移标记。旧库有非法非空 TAG 时沿用现有校验错误，保持显式值和隐式默认的区别。阶段升级或 v3 导入时需要生成兼容 EdgeOne TAG，只有当原 DNS TAG 与 `edgeone` 相同才用 `edgeone-origin`。用户明确提交的 v4 配置重名直接报错。 [当前建表迁移与设置加载](config/store.go) · [TAG 校验](config/validate.go)

<a name="api-reference"></a>

### 7.2 配置、状态与备份接口

| 方法和路径 | 预期接口行为 |
|---|---|
| `GET /api/settings` | 保留现有字符串映射，增加两个功能开关、EdgeOne TAG 和间隔。 |
| `PUT /api/settings` | 继续用严格部分更新 DTO。全局开关仍由暂停和恢复端点处理。 |
| `GET /api/edgeone/sites` | 返回 `{"sites":[]}` 形状；含配置的列表按 `zone_id` 稳定排序。 |
| `POST /api/edgeone/sites` | 创建站点，成功返回 201 与站点配置，已存在返回 409。 |
| `PUT /api/edgeone/sites/{zone_id}` | 全量替换该站点的规则参数与绑定，成功返回 200，缺站点返回 404。 |
| `DELETE /api/edgeone/sites/{zone_id}` | 按现有配置删除端点风格返回 200，缺站点返回 404。 |
| `GET /api/edgeone/status` | 返回站点运行状态，不向腾讯云发请求。包含当前/待更新版本、最近查询、目标验证、计划时间和确认结果。 |
| `GET /api/sync/status` | 原字段继续可读，新增 `dns`、`edgeone` 的开关和调度摘要，以及全局预检状态。 |
| `POST /api/sync/trigger` | 排队运行已启用功能，沿用 202、总暂停时 409 与去重。 |
| `POST /api/sync/dryrun` | 只读取得计划和预检结果，保留现有并发保护及 `results`、`warnings`。 |
| `POST /api/config/export` / `POST /api/config/import` | 导出 v4，导入严格 v3 或 v4。 |

复用 `decodeJSONStrict`、`writeJSON` 及现有错误映射。类型错误、字段缺失、空绑定和无效目标 ID 走请求校验；站点路径作为不可变标识，去除首尾空白并拒绝空值、控制字符和路径分隔符，保留腾讯云 ID 的大小写。写入失败返回既有错误形状，成功返回前完成事务提交与候选快照发布。站点缺权限、配额不足和查询失败是运行结果，在页面和模拟中显示。 [路由与 JSON 写入](webui/api/deps.go) · [严格解码](webui/api/decode.go) · [设置 DTO](webui/api/settings.go)

```json
{
  "zone_id": "zone-example",
  "target_ids": [12, 19],
  "protocol": "TCP",
  "ports": "80,443",
  "address_family": "ipv4"
}
```

配置设置 API 沿用字符串映射，示例是一次部分更新。

```json
{
  "dns_enabled": "true",
  "edgeone_enabled": "true",
  "edgeone_tag": "edgeone",
  "edgeone_interval": "36h"
}
```

配置包中的开关使用 JSON boolean，目标关系使用 `target_export_ids`。以下是 v4 完整形状的脱敏示例。 `export_id` 可与当前数据库 ID 不同。没有站点时 `edgeone_sites` 写 `[]`。

```json
{
  "version": 4,
  "metadata": {"exported_at": "2026-11-12T00:00:00Z"},
  "targets": [
    {"export_id": 3, "cloud_type": "tc_cvm", "region": "ap-guangzhou", "resource_id": "sg-example"},
    {"export_id": 5, "cloud_type": "ali_ecs", "region": "cn-hangzhou", "resource_id": "sg-example2"}
  ],
  "rules": [],
  "edgeone_sites": [
    {
      "zone_id": "zone-example",
      "target_export_ids": [3, 5],
      "protocol": "TCP",
      "ports": "80,443",
      "address_family": "ipv4"
    }
  ],
  "settings": {
    "credentials": {
      "tencent": {"secret_id": "", "secret_key": ""},
      "aliyun": {"access_key_id": "", "access_key_secret": ""}
    },
    "tag": "auto-ddns",
    "interval": "5m",
    "dns": "223.5.5.5",
    "dns_timeout": "10s",
    "dns_fail_threshold": 5,
    "log_level": "info",
    "sync_enabled": true,
    "dns_enabled": true,
    "edgeone_enabled": false,
    "edgeone_tag": "edgeone",
    "edgeone_interval": "72h",
    "theme": "light"
  },
  "alerts": {
    "policy": {
      "dns_failed_enabled": false,
      "sync_error_enabled": false,
      "operational_error_enabled": false,
      "health_timeout": "10m"
    },
    "email": {
      "enabled": false,
      "host": "",
      "port": "587",
      "security": "compat_starttls",
      "username": "",
      "password": "",
      "from_addr": "",
      "to_addr": "",
      "subject": "[FWAlizer] 告警通知",
      "body": "FWAlizer 检测到运行异常，请检查同步日志。"
    },
    "webhook": {"enabled": false, "url": "", "channel": ""}
  },
  "monitoring": {"uptime_kuma_push": {"enabled": false, "url": "", "interval": "60s"}}
}
```

严格解码先识别 `version` 再校验该版本完整结构，沿用现有请求上限、重复字段及非法 JSON 校验。v3 按自己的字段集合验证，随后转换到共用的已校验配置模型。v4 显式字段全部要求存在，保留有效的 `false`、空列表及允许的空凭据。配置导入沿用 `MutateImport`，顺序为预校验、清理业务配置、插入目标并建立映射、插规则与站点绑定、写完整设置及告警、读完整快照、提交发布。 [v3 参考实现](webui/api/bundle_v3.go) · [必填字段检查](webui/api/bundle_presence.go) · [导入导出入口](webui/api/export.go)

<a name="teo-reference"></a>

### 7.3 TEO 客户端与领域对象

TEO 是地址来源，云防火墙 Provider 继续使用现有实现。业务模型与 SDK 指针类型隔离。站点配置和运行查询结果分别保存，`RuntimeState` 只携带深拷贝的配置和本地构造的客户端引用。SDK 模块版本和现有 `common` 的依赖关系由 Step 5 核对。

```go
// 配置对象参考，建议放在 config。TargetIDs 深拷贝后才发布。
type EdgeOneSite struct {
    ZoneID        string `json:"zone_id"`
    TargetIDs     []int  `json:"target_ids"`
    Protocol      string `json:"protocol"`
    Ports         string `json:"ports"`
    AddressFamily string `json:"address_family"`
}

// 只读 TEO 结果参考，Prefix 已经 Masked，地址族已验证。
type OriginACLVersion struct {
    Version  string
    Prefixes []netip.Prefix
}

type OriginACLSnapshot struct {
    ZoneID          string
    Current         OriginACLVersion
    Next            *OriginACLVersion
    PlannedActiveAt *time.Time
    ObservedAt      time.Time
    RequestID       string
}
```

签名调用使用腾讯云 SDK 的 context 入口。下面省略公共的限速、日志脱敏与 SDK 错误转换，`ctx` 由调用方提供请求预算。

```go
// import common、profile，以及 teo "…/tencentcloud/teo/v20220901"。
clientProfile := profile.NewClientProfile()
clientProfile.HttpProfile.Endpoint = "teo.tencentcloudapi.com"
client, err := teo.NewClient(common.NewCredential(secretID, secretKey), "", clientProfile)
if err != nil {
    return err
}
req := teo.NewDescribeOriginACLRequest()
req.ZoneId = common.StringPtr(zoneID)
resp, err := client.DescribeOriginACLWithContext(ctx, req)
// err 为 nil 后继续检查 resp.Response.OriginACLInfo 和必要字段。
```

| 输入或事件 | 转换与调度动作 |
|---|---|
| 源站防护有效、当前版完整、无下一版 | 返回 `Next=nil`，当前版参与规划。 |
| 当前版和下一版完整 | 保存版本与规范网段。相同前缀去重；`AddedAddresses`、`RemovedAddresses` 可展示，规则依据使用完整列表。 |
| 源站防护未启用、当前版缺失、下一版存在但字段不全、CIDR 不合法或无有效网段 | 作为查询错误处理，禁止将它解释为新空集合或清理授权。 |
| 明确合法的空地址族 | 按有效空列表读入；若用户选中的必要地址族缺失，站点本次无法取得覆盖证明。 |
| 权限拒绝、超时、限流 | 分类记录。失败查询不刷新有效期；只有仍符合当前站点、凭据和时间边界的旧快照可继续参与检查。 |
| 配置换站点、换凭据或已到刷新时间 | 重新查询。到达已知计划切换时间时，按实际查询结果推进状态。 |

同一轮一个站点只准备一份正式地址集，供全部目标和全局判定共用。确认前的重查使用独立请求，获取的版本若变化，放入后续正式轮重新规划。只读模拟可按请求获取地址用于预览，正式缓存、下一次到期和恢复状态由正式执行管理。 [TEO 查询与响应示例](https://cloud.tencent.com/document/product/1552/120408)

<a name="preflight-reference"></a>

### 7.4 全局预检、状态恢复与调度参考

预检输入同时保存成功部分和完整性。存在可适用规则但解析失败是未知；没有适用规则是完整空集。全局检查读取适用域名实际取得的 A/AAAA 与站点当前及下一版完整网段，目标地址族筛选仍只影响防火墙计划。检测输入以正规 IPv4/IPv6 表示，IPv4 DNS 结果执行 `Unmap`，保留已有云规则读取的 IPv4-mapped 身份合同。地址全局重叠判断采用 CIDR 包含关系，目标覆盖仍按规范功能 key 精确匹配。

```go
type DNSAddress struct {
    Host string
    Addr netip.Addr
}

type EdgeOnePrefix struct {
    ZoneID string
    Prefix netip.Prefix
}

func firstOverlap(dns []DNSAddress, edge []EdgeOnePrefix) (DNSAddress, EdgeOnePrefix, bool) {
    // 调用前按域名/IP、站点/CIDR 排序，保证证据稳定。
    for _, host := range dns {
        address := host.Addr.Unmap()
        for _, network := range edge {
            if network.Prefix.Contains(address) {
                host.Addr = address
                return host, network, true
            }
        }
    }
    return DNSAddress{}, EdgeOnePrefix{}, false
}

type Preflight struct {
    DNSEnabled, EdgeOneEnabled   bool
    DNSComplete, EdgeOneComplete bool
    KnownConflict, Overlap       bool
}

type Admission struct {
    DNS, EdgeOne bool
    KeepConflict bool
}

func decideAdmission(in Preflight) Admission {
    result := Admission{
        DNS:          in.DNSEnabled,
        EdgeOne:      in.EdgeOneEnabled && in.EdgeOneComplete,
        KeepConflict: in.KnownConflict,
    }
    if !in.DNSEnabled || !in.EdgeOneEnabled {
        // 仅 DNS 的错误/重试策略继续由现有目标链处理。
        return result
    }
    if in.Overlap || (in.KnownConflict && !(in.DNSComplete && in.EdgeOneComplete)) {
        return Admission{KeepConflict: true}
    }
    return Admission{DNS: in.DNSComplete, EdgeOne: in.EdgeOneComplete}
}
```

`Admission` 是数据层准入，最终写入还要满足总暂停、到期来源和目标覆盖门。表中的已查实冲突在正式轮持久化成功后报告；完整无重叠时，先持久化解除，再放行。任何一次正式预检都可以识别新的重叠，包含部分成功的输入。只读模拟使用相同判断生成预览，不执行这两种状态写入。站点确认使用当前轮的数据准入和站点独立证明，DNS 查询失败但没有已知重叠时，仍能走 EdgeOne 确认。

```text
Syncer.Run
  消费启动 / 定时到期 / 手动触发 / 恢复 / 配置通知
  按同一准入点读取不可变配置、总暂停、待执行来源
  按需准备各自地址集 → 如两项启用，执行全局预检并记录结果
  只有获准且到期的来源进入正式目标执行
  等目标任务完成 → 处理需要确认的 EdgeOne 站点
  各自结算下次到期，发布真实状态和结果
```

仅 EdgeOne 到期时可读取 DNS 以完成预检，但不因此执行 DNS 防火墙写入或重置 DNS 计时。仅 DNS 到期同理，可使用有效 EdgeOne 结果。已知冲突由下一次调度或手动触发复查，无独立写入 goroutine。总暂停、Stop、同间隔保存和短间隔调度均复用现有准入约束。源数据准备完成后按实际目标分组；同一 Provider 目标只由一个执行单元负责，现有跨平台并行在全局预检通过后保留。 [调度入口](syncer/syncer.go) · [熔断轮次](syncer/dns_round.go)

<a name="target-reference"></a>

### 7.5 目标规划与站点确认参考

- 将解析结果在进入规划器前转换为统一前缀。DNS 单地址用 `netip.PrefixFrom(addr, addr.BitLen())`，EdgeOne 用规范的网段前缀。规划条目保留 DNS 规则 ID 或 EdgeOne 站点 ID，后者用于从同一目标回读中提取每站覆盖结果。
- 按现有地址族、CIDR、协议、端口、动作组成 `FunctionalKey`。多个 EdgeOne 站点产生同一 key 时只新增一条规则，保留所有来源用于回读证明和清理。删除时检查目标上的所有适用站点，防止 A 的旧网段仍被 B 使用。跨来源完全等价的旧规则标记为目标冲突，精确等价手工规则的覆盖语义保持。各平台的合并端口、模板和相反动作继续按原安全门处理。
- S0 计划用于容量估算和新增。S1 采用同一份预检输入验证全部所需项。只有覆盖、TAG、目标定位和版本均满足时才删除；有删除则按现有合同读 S2。对同一目标发生版本冲突时，重新 S0 和规划。两项启用时保持输入前缀，DNS 单独运行仍按现有路径重试。 [功能 key、覆盖和清理](provider/plan.go) · [Provider 接口](provider/provider.go) · [目标 attempt](syncer/target.go)

| 平台 | 复用的写入与删除定位 | 本阶段重点 |
|---|---|---|
| 腾讯云 Lighthouse | S0 / S1 的 `FirewallVersion`，按当前快照原值条件删除 | 多端口展开、版本变化后重试。 |
| 腾讯云 CVM | 安全组 `Version` 与 S1 `PolicyIndex` | 现有入站数量保护和新旧并存预算。 |
| 阿里云 ECS | S1 回读 `SecurityGroupRuleId` | 完整分页、分批和部分成功计数。 |
| 阿里云 SWAS | S1 回读 `RuleId` | IPv6 不可用、完整分页和有界读取。 |

站点确认的输入是该站点目标集合上实际回读的证明。可参考以下对象，状态与配置分别保留。

```go
type SiteCoverage struct {
    ZoneID         string
    TargetKey      string
    Current        string
    Pending        string
    SnapshotDigest [32]byte // 两版规范排序后的完整网段摘要
    ConfigEpoch    uint64
    Complete       bool
    HasConflicts   bool
}

func siteCoverageReady(required []string, proofs map[string]SiteCoverage, want SiteCoverage) bool {
    if len(required) == 0 || want.Pending == "" {
        return false
    }
    for _, target := range required {
        got, ok := proofs[target]
        if !ok || !got.Complete || got.HasConflicts || got.TargetKey != target ||
            got.ZoneID != want.ZoneID || got.Current != want.Current || got.Pending != want.Pending ||
            got.SnapshotDigest != want.SnapshotDigest || got.ConfigEpoch != want.ConfigEpoch {
            return false
        }
    }
    return true
}
```

`required` 来自当前站点配置里已去重的实际目标，`proofs` 只包含该站点本轮的目标回读。`Complete` 表示该站点所有必需地址族和功能 key 在完整快照中得到证明。`ConfigEpoch` 标识相关配置快照，摘要按前缀规范化并排序后计算；同版本内容变化时重新规划。示例的比较函数只判断站点证明，发确认前仍经过全局准入、版本重查和总暂停检查。

```text
for 每个本轮需要确认的站点:
    要求配置的每一个实际目标均有同一版本、同一配置的完整回读
    复核地址族、功能 key、跨 TAG 残留及相关相反动作
    重新 DescribeOriginACL，核对版本、规范网段和当前配置
    若站点仍启用且当前可发起确认:
        持久化确认状态 unknown
        调用 ConfirmOriginACLUpdate
        成功则记录 accepted 和 RequestId
    超时 / 已是最新版 / 状态未知 → 先查询实际状态，再决定后续动作
```

`unknown` 在请求之前落库，使进程在请求与结果持久化之间退出时，重启可以先查询实际状态。重新查询无待更新版本时，应按当前版本、生效证据与本机过渡记录判定，不能单凭一次空字段认定先前确认成功。只有查询证明待更新版本成为当前且网段吻合时，才把旧过渡网段交给普通安全清理。若站点出现另一新待更新版本，先保护已铺设前缀并刷新证明。 [站点确认接口](https://cloud.tencent.com/document/product/1552/120409)

```go
// 仅在上述全部检查通过且 unknown 已提交后调用。
req := teo.NewConfirmOriginACLUpdateRequest()
req.ZoneId = common.StringPtr(zoneID)
resp, err := client.ConfirmOriginACLUpdateWithContext(ctx, req)
// 成功读取 RequestId 后记录 accepted。结果不确定时由调用方转入只读核对。
```

<a name="view-reference"></a>

### 7.6 响应、界面和可观测性参考

运行状态建议沿用现有 JSON 风格。三个开关分层展示。总开关字段沿用 `enabled`；`dns.enabled`、`edgeone.enabled` 表示配置开关，`effective` 表示与总开关合取后的执行资格。定时和进度字段保持区分，尚无记录时返回 null。

```json
{
  "enabled": true,
  "dns": {"enabled": true, "effective": true, "next_due_at": null},
  "edgeone": {"enabled": true, "effective": true, "next_due_at": null},
  "address_preflight": {
    "state": "overlap",
    "conflict": {
      "dns_name": "home.example",
      "dns_address": "203.0.113.7",
      "edgeone_prefix": "203.0.113.0/24",
      "zone_id": "zone-example"
    }
  }
}
```

此示例仅列本阶段新增的状态及既有总开关，现有轮次、健康和时间字段继续输出。 `address_preflight.state` 可使用 `single_source`、`clear`、`overlap`、`data_unavailable`；已查实冲突且复查数据不足时保留 `overlap`，同时附失败来源。恢复时发布一次状态变化。Dry Run 使用本次预览的判定和实际受影响目标；云端未知配额标为未知，已读快照数和预计新增数分别显示。建议只增加适配既有前端结构的字段，保持 `results`、`warnings` 等数组为空时为 `[]`。 [同步响应](syncer/syncer.go) · [前端类型](webui/frontend/src/types.ts) · [模拟测试页面](webui/frontend/src/views/RunTest.vue)

日志和健康沿用现有事件与端点。健康使用来源自己的到期与结果，区分可继续的部分结果与两侧全局阻断。诊断只包含安全字段，例如 `ZoneId`、目标标识、版本、状态和 RequestId。目标明确写入数量、候选数、已清理数和残留分别统计。用本地模拟证明各入口边界，真实站点数据形状、规则配额、权限、确认后的实际时序由 Build8 Step 17 留存独立证据。 [运行健康](internal/health/health.go) · [日志入库](webui/api/logwriter.go) · [构建追踪](Build8.md#step-17)
