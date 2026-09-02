# Shadow Relay 设计

## 产品与分层

名称统一使用 **Shadow Relay**，仓库 `shadow-relay`。Relay 是传递经过审核的源配置的控制面，不承担全量音视频转发，也不替代媒体服务。管理范围是一个可信管理员管理的私有工作空间；设备使用独立只读令牌。

```mermaid
flowchart LR
  Upstream[上游目录 / 配置文件 / 服务] --> Candidate[候选箱]
  Candidate --> Review[结构检查与人工接纳]
  Review --> Source[逻辑源 + 暂存版本]
  Source --> Probe[分级体检]
  Probe --> Approval[版本批准]
  Approval --> Set[源编排组]
  Set --> Publication[不可变发布物]
  Publication --> Binding[客户端绑定与格式授权]
  Binding --> Client[Shadow Media / 传统客户端]
  Runtime[独立领域运行时] --> Probe
  Runtime --> Client
```

媒体类型、协议、执行方式、能力分别建模。`text.novel` 是媒体类型，`legado-book` 是规则格式，`runtime-backed` 是执行方式，`chapter` 是能力，不混为一个 `kind`。

Shadow Platform 后续只引用 `relayInstanceId`、`relayPublicationId`。Relay 内部使用 Source Set 和 Publication，不使用 Platform Profile。

## 实体与一致性

PostgreSQL 分表保存 catalogs、candidates、sources、endpoints、secrets、revisions、probes、source_sets、publications、bindings、jobs、audits、feedback。

领域文档使用有版本迁移的 JSONB 行；源、运行时、目录、版本、发布、绑定之间的关系通过生成列和外键约束。编排成员是 Source Set 内的有序数组，整组在事务中保存，避免成员与组配置分离更新。原始文件存储在本地内容寻址目录，文件名为原文 SHA-256，文件内容 AES-256-GCM 加密；目录必须由 API 和 Worker 共享。

控制面写事务通过 PostgreSQL 事务级 advisory lock 串行化。网络请求与加密快照写入在事务外执行，提交时复核源的 `updatedAt`，配置有变化就拒绝过期结果。此方案适合个人媒体规模，不把全局写锁用于大量媒体数据读写。

一次发布在同一个事务中读取组、批准版本和运行时状态，完成编译，插入不可变 Publication，再移动组的 current/previous 指针。失败时整个事务回滚。读订阅只读取已保存的发布物，不在请求路径上重新抓取上游。

## 导入与更新状态机

1. 手动导入与候选接纳都创建 **未启用、无活动版本** 的 Source，并保存 staged Revision。
2. 管理员检查规范化内容与差异，批准版本，再独立启用源。
3. 同步遵循 `If-None-Match` / `If-Modified-Since`。304 保留原缓存验证器，不重复保存版本。
4. 原文先加密落盘再解析；结构错误记录 invalid Revision，保持已有活动版本，写入失败体检。
5. `review` 一律暂存；`manual` 不定时抓取；`pinned` 同时禁止手动同步，源回滚会切到 pinned。
6. `auto` 仅在已有批准版本、删除比例低于 30%、没有新域名、非隔离状态且**新内容功能抽样成功**时批准。仅结构或服务可达检查不足以自动批准。
7. 人工拒绝的相同内容哈希不会在后续同步中重新自动批准。内容变化后才作为新版本再次判断。
8. 自动批准不会自动发布编排组；发布是单独的明确操作，所有客户端统一切换到完整版本。

候选目录使用 URL 指纹去重；同一地址重复同步不重复创建，屏蔽后也不会因改名绕过。上游条目不会自动启用。已启用源通过自身更新策略获取内容变化。

## 体检与健康

体检记录包含 level、success、checks、code、latencyMs。level 区分 `structure`、`service`、`functional`，表示真实完成的检查深度。具体协议检查见支持矩阵。

- 功能抽样成功：healthy，100 分。
- 仅结构/服务检查成功：degraded，60 分，表示尚未覆盖内容功能。
- 连续失败 1 次：degraded / 40 分；2 次：failing / 20 分；3 次：quarantined / 0 分。
- 停用与隔离不会因后台一次成功检查自动解除；隔离需要明确释放，再体检。
- 人工批准新版本重置为结构检查等级，不能继承旧版本的功能健康结论。
- 客户端反馈只接受错误枚举，且按绑定、源、错误类型、分钟去重；不允许客户端单方面让源进入隔离。

评分是透明的检查等级和连续失败计数，不宣称具有统计学质量模型，也不宣称测完所有频道、章节、时长、码率或内容完整度。

## 编排与发布

优先级降序，同优先级主源优先，再按源 ID 保持稳定顺序。weight、role、timeoutMs、maxConcurrency 连同能力写入 Bundle，供客户端调度。媒体类型过滤限制成员；语言和地区过滤限制条目。重复流地址、Feed URL、Legado 规则和 XMLTV 频道/节目在编译时去重，优先保留顺序在前的条目。

未启用、未批准、仅目录、失败/隔离、低于阈值、媒体不匹配或运行时不可用的成员被排除，并在发布物记录原因。若没有任何合格成员，则拒绝发布，保留当前版本。只为实际有内容的格式生成文件，未生成的格式返回 404。

Bundle 引用每个源独立的配置快照，聚合文件仅面向传统导出，避免多个 Provider 意外读取同一份合并列表。设备和网络约束是 Bundle 客户端选择提示，不作为安全授权。传统格式无法表达这些提示，直接排除含这些限制的成员。权重和主备关系用于客户端选择；Relay 不主动转发内容流量实施播放故障切换。

## 任务运行

同一二进制支持 `serve`（API + Worker）、`api`、`worker`、`migrate`、`keygen`。数据库队列使用 `FOR UPDATE SKIP LOCKED`、活跃任务唯一索引、2 分钟租约与 20 秒续租。任务最长 95 秒；最多 3 次执行，按尝试次数平方退避。Worker 退出后租约到期可恢复，执行语义为至少一次，更新哈希、候选指纹和事务确保重试幂等。

调度器每 30 秒检查到期的已启用源和目录。每个进程默认 2 个 Worker，每个目标 Host 最多 2 个同时请求，连续 3 个网络/服务端失败熔断 1 分钟。多 Worker 进程的出站并发限制是进程级，不是跨进程分布式限流。

## 后续扩展

内容层统一搜索与解析、规则运行沙箱、设备一次性配对凭据、低流量鉴权网关和 Shadow Media 托管同步分别演进。它们不应通过在 Relay Core 中执行用户脚本来实现。当前客户端保留自己的媒体服务凭据，Relay Bundle 只描述端点与能力。
