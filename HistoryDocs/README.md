# FWAlizer 历史文档索引

> **2026-11-09 更新，用户授权。** Issue9 与旧待办导航已归档。项目当前状态和编码要求见 [AGENTS.md](../AGENTS.md)。本目录保存阶段设计、实施记录、历史台账和验收证据。

## 本次归档

| 文档 | 归档结论 | 剩余事项与当前入口 |
|---|---|---|
| [Issue9.md](Issue9.md) | I9-R01～R11 阶段台账归档，已确认的修复已实施。 | SMTP 验收已收口，容量与平台证据按既有边界处理。 |
| [TODOLIST.md](TODOLIST.md) | 原待办导航归档。 | 早期 [TODOLIST-history.md](TODOLIST-history.md) 单独保留。 |
| [Design5.md](Design5.md) | WebUI 单二进制 + SQLite、配置与内部使用边界已形成既有设计，阶段达标。 | 已实施合同继续查 AGENTS；阶段问题见 Issue9。 |
| [Build6.md](Build6.md) | Step 0～7 已完成的历史构建记录；当时版本 2 配置包已被 Build7 的版本 3 取代。 | SMTP 验收见 Issue9 I9-R08。 |
| [Build7.md](Build7.md) | 告警与运行健康 Step 0～7 已完成，后续补记按各自批次保存。 | SMTP 验收已收口；Webhook/Kuma 继续豁免。 |
| [Issue8.md](Issue8.md) | I8-R01～R27 阶段台账按“完成部分归档、剩余完整转交”收口。 | 原 R01→I9-R08，R14 容量→I9-R09，R23→I9-R10，R25→I9-R11；全部 27 项对照见 [Issue9 §4](Issue9.md#4-issue8-全部编号迁移对照)。R06 专项工具缺陷独立为 I9-R05/R06。 |

上阶段核对的发布为源码 `26e5c8f` / `v3.0.1`，[Actions 41374458503](https://github.com/AlcaProphet/cloudhost-firewall-autoupdater/actions/runs/41374458503) 测试及镜像构建推送成功，包含 R26/R27。归档正文的“尚未提交/远端未执行”是原批次事实，不能据此判断当前实现未合并；发布结果也不代表用户运行容器已升级、全部平台长期稳定。

## 更早阶段

| 类别 | 文档 |
|---|---|
| 设计记录 | [Design1](Design1.md)、[Design2](Design2.md)、[Design3](Design3.md)、[Design4](Design4.md) |
| 构建记录 | [Build1](Build1.md)、[Build2](Build2.md)、[Build3](Build3.md)、[Build4](Build4.md)、[Build5](Build5.md) |
| 问题记录 | [Issue1](Issue1.md)、[Issue2](Issue2.md)、[Issue3](Issue3.md)、[Issue4](Issue4.md)、[Issue5](Issue5.md)、[Issue6](Issue6.md)、[Issue7](Issue7.md) |
| 历史审计 | [fwalizer-audit-final1.md](fwalizer-audit-final1.md)，报告完成、后续问题经 Issue8 转 Issue9；不表示全部历史发现均作为缺陷修复。 |
| 更早快照 | [Issue8-history.md](Issue8-history.md)、[TODOLIST-history.md](TODOLIST-history.md) |

## 阅读与跟进

- `Issue8.md` 是前一阶段归档的 R01～R27 台账；`Issue8-history.md` 是更早的 I8/Q/实施补记快照。两个文件均保留，不覆盖、不合并其原始次数或日期。
- 历史授权、步骤与测试记录只适用于当时阶段；归档不恢复旧授权、不要求重跑全部步骤。当前用户指示、AGENTS 与 Issue9 的接受边界优先用于新工作。
- 普通云/浏览器人工验收按用户确认收口；SMTP 验收结果见 [ProdTestList.md](../ProdTestList.md)。Webhook/Kuma 继续豁免且不阻断，豁免不等于真实通过。
- 过期目标、IP 或端口残留允许作为个人静默工具的设计取舍；当前排除和接受项见 [Issue9 §5](Issue9.md#5-接受排除和已失效的报告项目)，不从旧报告重新生成高优先级待修清单。
- Issue9 和 TODOLIST 均为历史记录。新增事项按实际需求单独建立记录。
