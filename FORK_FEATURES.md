# new-api 二开功能说明（Fork Features）

本文档记录本仓库（`JeffZhu1122/new-api`）相对上游 `QuantumNous/new-api` 所做的全部二次开发内容，供团队成员理解每个功能的动机、实现位置、配置方式和已知限制，并作为后续与上游同步时的冲突排查索引。

| 项目 | 值 |
|---|---|
| 上游基线（merge-base） | `b48b74ab7` — 2026-10-05 `feat(jsplugin): decode hook results with moejs ToGoInto`（上游版本 `v1.0.0-rc.41` 之后） |
| 最近一次同步 | 2026-10-06，rebase 到 `b48b74ab7`，2 处冲突（见 §19.4） |
| fork 专有提交数 | 30 个（含本文档相关提交） |
| 变更规模 | 180 个文件，+14440 / −1343 行（不含本文档） |

同步策略：fork 采用 **rebase 到上游 main** 的方式跟进，因此 `git log b48b74ab7..HEAD` 得到的提交就是全部二开内容，提交的作者日期保留了原始开发时间（2026-08-15 起）。注意中间提交不保证独立可编译（例如 `20d115227` 调用了下一个提交才定义的 `AddFailedChannel`），所有描述以 HEAD 代码为准。

---

## 目录

1. [功能总览](#1-功能总览)
2. [基础设施：两张侧表](#2-基础设施两张侧表)
3. [重试：400 关键词重试](#3-重试400-关键词重试)
4. [重试：避开本次请求已失败的渠道](#4-重试避开本次请求已失败的渠道)
5. [渠道级超时](#5-渠道级超时)
6. [免费 token 计数端点（count_tokens / input_tokens）](#6-免费-token-计数端点count_tokens--input_tokens)
7. [用户 × 模型 RPM/TPM 限流](#7-用户--模型-rpmtpm-限流)
8. [渠道级 RPM/TPM 上限](#8-渠道级-rpmtpm-上限)
9. [分组 / 用户模型折扣](#9-分组--用户模型折扣)
10. [渠道输入 token 边界（min/max input tokens）](#10-渠道输入-token-边界minmax-input-tokens)
11. [Anthropic 渠道认证模式](#11-anthropic-渠道认证模式)
12. [新建 / 复制渠道默认禁用](#12-新建--复制渠道默认禁用)
13. [API Key 主分组 + 有序备用分组](#13-api-key-主分组--有序备用分组)
14. [渠道响应头过滤（黑名单 / 白名单）](#14-渠道响应头过滤黑名单--白名单)
15. [渠道额度上限与成本倍率（达上限自动禁用）](#15-渠道额度上限与成本倍率达上限自动禁用)
16. [渠道可用时段（时段外不参与选路）](#16-渠道可用时段时段外不参与选路)
17. [实时 RPM 与 TPM 统计](#17-实时-rpm-与-tpm-统计)
18. [仓库维护类变更](#18-仓库维护类变更)
19. [与上游同步的注意事项](#19-与上游同步的注意事项)
20. [已知限制与测试缺口汇总](#20-已知限制与测试缺口汇总)
21. [附录](#21-附录)
22. [品牌主题层（Aurora Glass）](#22-品牌主题层aurora-glass)

---

## 1. 功能总览

| # | 功能 | 主要提交 | 默认状态 | 存储位置 | 主要影响面 |
|---|---|---|---|---|---|
| 3 | 400 关键词重试 | `20d115227` `5f22a93b0` | 关闭 | options 表（2 个 key） | 重试决策 |
| 4 | 避开已失败渠道 | `274d26fe0` `5f22a93b0` | 关闭 | options 表（3 个 key） | 重试 + 渠道选择 |
| 5 | 渠道级超时 | `c7ed417aa` | 0 = 继承全局 | `channel_extend` | 上游 HTTP 请求 / 流式 |
| 6 | count_tokens / input_tokens 免费端点 | `746188d58` `ce0dca5c8` | 渠道开关默认关 | 渠道 settings JSON | 路由、计费跳过 |
| 7 | 用户 × 模型 RPM/TPM | `3bde0f328` `55673f314` `b7c5bbd2e` | 关闭 | options 表 + `user_extend` | 中间件 |
| 8 | 渠道级 RPM/TPM | `09ac2e64d` | 0 = 不限 | `channel_extend` | 渠道选择 |
| 9 | 分组 / 用户模型折扣 | `6104962f2` `5b114b6cf` | 空 = 1.0 | options 表 + `user_extend` | 计费倍率 |
| 10 | 渠道输入 token 边界 | `e65956272` `0fe0e61f5` | 0 = 不限 | `channel_extend` | 渠道选择 |
| 11 | Anthropic 认证模式 | `455ec8282` | api_key | `channel_extend` | Claude 适配器请求头 |
| 12 | 新建 / 复制默认禁用 | `168dbbafc` | 始终生效 | 无 | 渠道管理 |
| 13 | Key 主分组 + 备用分组 | `f69683a02` | 无变化 | tokens 表既有列 | 鉴权、路由、计费 |
| 14 | 渠道响应头过滤（黑 / 白名单） | `b0a715551` | 关闭 = 复制全部 | `channel_extend` | 上游响应头回传 |
| 15 | 渠道额度上限 + 成本倍率（达上限自动禁用） | 与本文档同一提交 | 0 = 不限 | `channel_extend` | 结算后置状态、渠道启用路径、渠道列表 |
| 16 | 渠道可用时段（时段外不参与选路） | 与本文档同一提交 | 空 = 全天可用 | `channel_extend` | 渠道选择（HTTP + Responses WebSocket）、渠道列表 |
| 17 | 实时 RPM 与 TPM 统计 | 与本文档同一提交 | 开启（`RPM_STATS_ENABLED`），只计数 | Redis（无 Redis 时进程内存） | 分发与结算计数、渠道 / 用户列表、使用日志页统计条 |
| 18 | 移除 GitHub workflows | `a034e98b3` | — | `.github/workflows/` | CI |
| 22 | 品牌主题层（Aurora Glass） | 与本文档同一提交 | 默认预设即生效，其他预设只叠加结构效果 | `web/src/styles/brand.css` + 前端组件 class | 首页、认证页、定价页、控制台、错误页的外观 |

所有功能均**默认保持上游行为**：开关默认关闭、数值默认 0、JSON 默认为空，因此把 fork 部署到现有环境不会改变任何既有请求的处理结果（第 12 节"新建默认禁用"是唯一的例外，它只影响新建和复制操作；第 17 节实时统计默认开启，但只计数，不改变请求的处理结果；它改变了使用日志页 RPM / TPM 的口径，见该节"兼容性"；第 22 节品牌主题层对默认预设直接生效，但只改变前端外观，不改变任何功能、接口与数据）。

---

## 2. 基础设施：两张侧表

fork 的原则是**不修改上游 `channels` / `users` 表结构**，所有 fork 专有的按渠道、按用户配置放在两张独立的侧表里，无行即表示"全部继承全局配置"。这样上游对主表做 AutoMigrate 变更时不会产生冲突。

### 2.1 `channel_extend`

- 模型：`model/channel_extend.go`（`ChannelExtend`），迁移：`model/main.go` `migrateDB()` 中的 `DB.AutoMigrate(&Channel{}, &ChannelExtend{}, …)`，纯 GORM AutoMigrate。
- 传输 DTO：`relaykit/dto/channel_extend_settings.go`（`ChannelExtendSettings`），JSON 字段与列同名、全部 `omitempty`。
- 挂载点：`model.Channel.ExtendConfig *dto.ChannelExtendSettings`，标记 `json:"extend_config,omitempty" gorm:"-"`，只作 API 载体不落主表。

| 列 | 类型 | 默认 | 含义 | 所属功能 |
|---|---|---|---|---|
| `channel_id` | int, PK | — | 渠道 ID | — |
| `relay_timeout` | int | 0 | 单次上游请求总超时（秒），0 = 全局 `RELAY_TIMEOUT` | §5 |
| `streaming_timeout` | int | 0 | 流式事件间空闲超时（秒），0 = 全局 `STREAMING_TIMEOUT` | §5 |
| `min_input_tokens` | int | 0 | 估算输入 token 必须 **>** 此值 | §10 |
| `max_input_tokens` | int | 0 | 估算输入 token 必须 **≤** 此值 | §10 |
| `rpm_limit` | int | 0 | 渠道整体每分钟请求上限 | §8 |
| `tpm_limit` | int | 0 | 渠道整体每分钟 token 上限 | §8 |
| `claude_auth_mode` | varchar(16) | `''` | `""` / `api_key` / `oauth` / `auto` | §11 |
| `response_header_mode` | varchar(16) | `''` | `""`（复制全部）/ `blacklist` / `whitelist` | §14 |
| `response_headers` | text | 无 | JSON 字符串数组，过滤涉及的响应头名称 | §14 |
| `cost_ratio` | double | 0 | 成本倍率：每计费 $1 站内额度实际付给上游的美元数，仅用于成本展示 | §15 |
| `quota_limit` | bigint | 0 | 额度上限（quota 单位，`QuotaPerUnit` = $1），累计 `used_quota` 达到即自动禁用，0 = 不限 | §15 |
| `schedule` | text | 无 | JSON `dto.ChannelSchedule`，每周可用时段规则；空 = 全天可用 | §16 |

关键方法与行为：

- `ChannelExtendSettings.Validate()`：超时 `[0, 86400]`，RPM/TPM `[0, MaxInt32]`，输入 token `[0, 10_000_000]` 且两者都 >0 时要求 `max > min`，auth mode 必须是枚举值，成本倍率为 `[0, 1000]` 内的有限数，额度上限 `[0, 2^53 − 1]`（quota 单位），可用时段规则见 §16。`controller/channel.go` `validateChannel()` 调用它，Add/Update/批量接口共用。
- `ChannelExtendSettings.IsZero()`：所有数值为 0（含成本倍率、额度上限）、响应头过滤为空、无时段规则且 auth mode 为 `""` 或 `api_key` 时为真。
- `UpsertChannelExtend(tx, channelId, settings)`：`IsZero()` → 删除该行；否则 `ON CONFLICT(channel_id) DO UPDATE` 全列。`Channel.SaveExtendConfig(tx)` 在 `Channel.Insert()` 和 `BatchInsertChannels` 中调用；`ExtendConfig == nil` 时不动已有行。
- 级联删除：`DeleteChannelExtendByIds` 在 `Channel.Delete()`、`BatchDeleteChannels`、以及新增的 `deleteChannelsWhere`（供 `DeleteChannelByStatus` / `DeleteDisabledChannel` 使用）中调用。
- 读取：`GetChannelExtend(id)` 无行返回零值且 `err == nil`；`GetChannelExtendsByIds(ids)` 按 id 批量读取，供渠道列表 / 搜索接口附带 `extend_config`。
- 缓存：`model/channel_cache.go` 的 `channelExtendIDM map[int]ChannelExtendSettings` 在 `InitChannelCache()` 整表加载，并预填到缓存的 `Channel.ExtendConfig`（渠道选择在持锁路径中读取，避免锁内查库）。`GetChannelExtendSettings(id)`：内存缓存开启时查 map，否则查库。
- 请求链路：`middleware/distributor.go` `SetupContextForSelectedChannel` 写入 `constant.ContextKeyChannelExtendSetting` → `relay/common/relay_info.go` `InitChannelMeta` 拷入 `ChannelMeta.ChannelExtendSetting`，适配器由此读取。
- 管理 API：`GET /api/channel/:id` 与列表 / 搜索接口（`GetAllChannels` / `SearchChannels`）的每一行仅在非零时附带 `extend_config`；`PUT /api/channel/` 仅在请求 JSON **显式包含** `extend_config` 键时写入（显式 `null` 视为清除，缺键则不改）；`CopyChannel` 单独读取并随克隆写入。
- 权限：`controller/channel_authz.go` `channelHasSensitiveChanges` 把 `extend_config` 的任何变化视为敏感变更，需要 `authz.ChannelSensitiveWrite` 权限；原值读取失败时 fail-closed。
- 前端：`web/src/features/channels/lib/channel-form.ts` `buildExtendConfig()` 在 create 与 update payload 中总是携带 `extend_config`（全零即清除）；`transformChannelToFormDefaults` 从 `channel.extend_config?.*` 回填。字段在编辑抽屉 `other` 页签的 **"渠道额外设置（Channel Extra Settings）"** 卡片中，任一值 > 0 时卡片自动展开。

测试：`model/channel_extend_test.go`（生命周期、限流列持久化、无内存缓存时回退查库、单删 / 批删级联）、`relaykit/dto/channel_extend_settings_test.go`（Validate / IsZero）、`controller/channel_authz_test.go`（extend 变化敏感 / 原样回传不敏感 / null 清除敏感）。

### 2.2 `user_extend`

- 模型：`model/user_extend.go`（`UserExtend`），主键 `user_id`，`model/main.go` 已加入 AutoMigrate。

| 列 | 类型 | 含义 | 所属功能 |
|---|---|---|---|
| `user_id` | int, PK | 用户 ID | — |
| `rate_limit` | text | JSON `dto.RateLimitOverride`，用户级 RPM/TPM 覆盖 | §7 |
| `model_discount` | text | JSON `map[string]float64`，用户级模型折扣 | §9 |

- 写入：`UpdateUserRateLimitOverride` / `UpdateUserModelDiscount` 用 `clause.OnConflict` 按 `user_id` upsert 且只更新各自的列；内容为空时清空该列，并在**两列都为空**时删除整行（`deleteEmptyUserExtend`）。
- 缓存：Redis key `user_extend_rl:<userId>` / `user_extend_md:<userId>`，TTL `common.RedisKeyCacheSeconds()`，写穿透，`"null"` 作负缓存。
- 传输：`model.User` 上新增 `RateLimit *dto.RateLimitOverride`（`json:"rate_limit"`）与 `ModelDiscount map[string]float64`（`json:"model_discount,omitempty"`），均 `gorm:"-:all"`，只作 `GET/PUT /api/user/` 的载体。更新语义：字段缺省 `nil` = 不改动，`{}` = 清除。

测试：`model/user_extend_test.go`（两列各自 round-trip、清一列不影响另一列、两列皆空才删行）。

---

## 3. 重试：400 关键词重试

### 动机

上游默认可重试状态码为 `100-199,300-399,401-407,409-499,500-503,505-523,525-599`（`setting/operation_setting/status_code_ranges.go`），**400 被有意排除**。但部分上游厂商会把"换一个渠道就能成功"的错误（内容策略拦截、invalid prompt、某些模型参数不兼容）以 400 返回。本功能允许运营配置关键词，当 400 错误文本命中时强制重试到其他渠道。

### 实现

- 判定位置：`service/relay_error.go` `DecideRelayRetry(c, err, retryTimes)`。判定顺序为：err==nil → strict session → pinned channel → `IsChannelError` → `IsSkipRetryError` → **`retryTimes <= 0` 停止** → 2xx → 状态码越界 → always-skip（504/524、`ErrorCodeBadResponseBody`）→ **fork 插入点** → `ShouldRetryByStatusCode`。

  ```go
  if code == http.StatusBadRequest && operation_setting.AutomaticRetryKeywordsEnabled && len(operation_setting.AutomaticRetryKeywords) > 0 {
      if matched, _ := AcSearch(strings.ToLower(err.Error()), operation_setting.AutomaticRetryKeywords, true); matched {
          return PolicyDecision{Action: "retry", Reason: "retry_keyword_matched", Source: "global"}
      }
  }
  ```

- 匹配规则：匹配对象是 `err.Error()` 的完整错误文本；关键词存储时已 `ToLower`，文本也 `ToLower`，因此**不区分大小写**；`AcSearch`（`service/str.go`）是 Aho-Corasick 多模式**子串**匹配。
- 受最大重试次数约束（`retryTimes <= 0` 的检查在关键词之前）。本地生成、带 `ErrOptionWithSkipRetry` 的 400 会先被 `IsSkipRetryError` 拦下，不走关键词重试。
- 解析：`setting/operation_setting/operation_setting.go` `AutomaticRetryKeywordsFromString/ToString`，共用新抽出的 `parseKeywordLines`（按 `\n` 切分、TrimSpace、ToLower、丢弃空行），`AutomaticDisableKeywordsFromString` 也改为复用此函数。

### 配置

| option key | 类型 | 默认 |
|---|---|---|
| `AutomaticRetryKeywordsEnabled` | bool | `false` |
| `AutomaticRetryKeywords` | 换行分隔字符串 | `""` |

两者缺一不生效。可通过通用 `GET/PUT /api/option/` 或请求策略接口 `GET/PATCH /api/option/request_policy` 读写（后者是 UI 实际使用的；`model/request_policy.go` 的 `IsRequestPolicyOption` / `requestPolicyDefaultOptions` / `BuildRequestPolicy` 已加入这两个 key）。

### 前端

系统设置 → 请求策略 → **会话与重试**（`/system-settings/request-policies/routing`），`web/src/features/system-settings/request-policies/retry-section.tsx`，紧接上游的"最大重试次数 / 自动重试状态码"之后：

- Switch **按关键词重试 400 错误**（Retry 400 errors by keywords）——总开关。
- Textarea **400 错误重试关键词**（Retry keywords for 400 errors），每行一个。

### 测试

`setting/operation_setting/operation_setting_test.go`：`TestAutomaticRetryKeywordsFromString`、`TestAutomaticRetryKeywordsRoundTrip`。**缺口**：`service/relay_error_test.go` 没有 `retry_keyword_matched` 分支的用例。

---

## 4. 重试：避开本次请求已失败的渠道

### 动机

上游重试语义是按 retry 序号逐级"降优先级层"，同层内按权重随机，可能反复选到本次请求中已经失败的渠道。开启后，重试时排除本请求已失败的渠道，不再按序号降层，而是在**剩余渠道中取最高优先级层**。多 key 渠道不排除（重选同渠道会轮换 key）。全部渠道失败后返回可配置的状态码与错误信息。

### 实现

- 排除集合：`service/channel_select.go` `RetryParam` 新增 `ExcludeChannelIds map[int]bool`（nil = 未启用）与 `AddFailedChannel(channelId)`（惰性建 map）。集合挂在指针上，随 `retryParam` 贯穿整个请求的重试循环。
- 收集时机：`controller/relay.go` `Relay` 循环中，失败后 `DecideRelayRetry` → `RecordPolicyFailure` → `processChannelError` → `if RetryAvoidFailedChannelsEnabled && !ContextKeyChannelIsMultiKey { retryParam.AddFailedChannel(channel.Id) }`。任务提交路径 `executeTaskSubmissionWith` 同样收集（非 `LocalError` 时）。
- 传递：`cacheGetRandomSatisfiedChannelOnce` 在 auto 分组与普通分组两处均把 `param.ExcludeChannelIds` 传入 `model.GetRandomSatisfiedChannel`；auto 分组下当前分组剩余为空 → 返回 nil → 推进下一分组。上游的 `SelectChannelForRequest` 内部仍调用 `CacheGetRandomSatisfiedChannel(retry)`，无需改动即继承。
- 选择器：`model/channel_cache.go` `GetRandomSatisfiedChannel(group, model, retry, filters, excludeChannelIds)`——内存缓存路径过滤候选 ID，剩余为空返回 `(nil, nil)`，否则 **`retry = 0`**（放弃按序号降层，取剩余最高优先级层后按权重随机）。`model/ability.go` `GetChannel(..., excludeChannelIds)` DB 路径同样排除并 `retry = 0`，`getChannelQuery` 用 `channel_id NOT IN (?)` + `MAX(priority)` 子查询。
- 耗尽处理：`controller/relay.go` `getChannel` 中 `channel == nil && len(ExcludeChannelIds) > 0` → 返回 `RetryAvoidFailedChannelsHTTPStatusCode()` + `RetryAvoidFailedChannelsMessage(model)`，带 `ErrOptionWithSkipRetry`，循环结束。
- **未覆盖的路径**：`relay/responses_websocket.go` 的 WebSocket 重试循环调用 `DecideRelayRetry` 但未调用 `AddFailedChannel`，Responses WebSocket 不参与此功能。

### 配置

| option key | 类型 | 默认 |
|---|---|---|
| `RetryAvoidFailedChannelsEnabled` | bool | `false` |
| `RetryAvoidFailedChannelsStatusCode` | int，100-599 | `429` |
| `RetryAvoidFailedChannelsErrorMessage` | 字符串，支持 `{model}` 占位符 | `all available channels for model {model} have failed in this request, no channels left to retry` |

`RetryAvoidFailedChannelsHTTPStatusCode()` 在值不在 100-599 时回退 429；`RetryAvoidFailedChannelsMessage(model)` 在 TrimSpace 后为空时回退默认串。状态码校验在 `controller/option.go` 与 `BuildRequestPolicy` 两处。

### 前端

同 §3 的会话与重试卡片：

- Switch **重试时避开失败渠道**（Avoid failed channels on retry）。
- Input(100-599) **所有渠道失败时的状态码**。
- Textarea **所有渠道失败时的错误信息**（支持 `{model}`，留空用默认）。

表单逻辑在 `routing-form.ts`（zod schema、bool 字符串转换、`\r\n → \n`）与 `defaults.ts`。

### 注意事项

- **`ExcludeChannelIds` 与 §8 渠道级 RPM/TPM 限流共用**：限流饱和的渠道也会被 `AddFailedChannel`。因此即使本开关关闭，限流导致候选耗尽时，也会返回上面配置的状态码与信息（默认 429）。
- 上游其他调用 `GetRandomSatisfiedChannel` / `GetChannel` 的位置传 `nil`，签名变化是与上游合并时的固定冲突点。

### 测试

`model/channel_cache_exclude_test.go`（排除 / 不降层取剩余最高优先级 / 全部排除返回 nil / 未排除保持原降层语义 / DB 路径）、`service/channel_select_exclude_test.go`（排除后选另一渠道、auto 分组推进）、`setting/operation_setting/operation_setting_test.go`（状态码回退、占位符替换）。**缺口**：controller `getChannel` 耗尽返回可配置状态码的路径没有测试。

---

## 5. 渠道级超时

### 动机

允许单个渠道覆盖全局 `RELAY_TIMEOUT`（整体请求）与 `STREAMING_TIMEOUT`（流式事件间空闲）：慢模型可以放宽，劣质渠道可以收紧，而不用改全局环境变量。

### 存储

`channel_extend.relay_timeout` / `channel_extend.streaming_timeout`，单位秒，0 = 继承全局，范围 `[0, 86400]`。全局值来源：`common.RelayTimeout`（env `RELAY_TIMEOUT`，默认 0，在 `service/http_client.go` 设为 `http.Client.Timeout`）；`constant.StreamingTimeout`（env `STREAMING_TIMEOUT`，默认 300）。

### 实现

- **整体超时**：`relay/channel/api_request.go` `doRequest()`。若 `info.ChannelExtendSetting.RelayTimeout > 0`，在缓存 client 的浅拷贝上把 `Timeout` 置 0（消除全局值），改用 `context.WithTimeout(req.Context(), t)` 替换请求 context。deadline 覆盖连接、响应头和**完整 body 读取**（含流式）；cancel 不在函数内 defer，而是包进 `cancelOnCloseBody` 随 `resp.Body.Close()` 释放，`Do` 出错或 resp 为 nil 时立即 cancel。渠道值**完全替代**全局值（可长可短）。覆盖 `DoApiRequest`、`DoFormRequest`、`DoRequest`、`DoTaskApiRequest`；`DoWssRequest` 不经 `doRequest`，不受影响。
- **流式空闲超时**：`relay/helper/stream_scanner.go` `StreamScannerHandler` 中 `streamingTimeout` 先取全局，`ChannelExtendSetting.StreamingTimeout > 0` 则覆盖；作为 ticker 周期，每收到事件 `ticker.Reset`，到期设 `StreamEndReasonTimeout`。
- **AWS/Bedrock**：`relay/channel/aws/relay-aws.go` 新增 `newAwsInvokeContext(parent, info)`：`timeout := common.RelayTimeout`，渠道值 > 0 则覆盖；≤ 0 用 `WithCancel`。`awsHandler`、`awsStreamHandler`、`handleNovaRequest` 使用。AWS 流式走 SDK 事件流，不经 `StreamScannerHandler`，`streaming_timeout` 对其无效。
- 上下文键：`constant/context_key.go` 新增 `ContextKeyChannelExtendSetting`；`relay/common/relay_info.go` `ChannelMeta` 新增 `ChannelExtendSetting` 字段。

### 前端

渠道编辑抽屉 → 渠道额外设置：

- **转发超时（秒）**（Relay Timeout (seconds)）："该渠道单次上游请求的最长耗时（含完整响应体读取）。0 表示使用全局 RELAY_TIMEOUT。设置过短可能导致慢模型触发自动禁用。"
- **流式空闲超时（秒）**（Streaming Idle Timeout (seconds)）："该渠道流式响应两个事件之间的最长空闲时间。0 表示使用全局 STREAMING_TIMEOUT。"
- 校验文案：渠道超时必须在 0 到 86400 秒之间（`constants.ts ERROR_MESSAGES.INVALID_CHANNEL_TIMEOUT`）。两字段属敏感字段（`SENSITIVE_FORM_FIELDS`）。

### 测试

`relay/channel/aws/relay_aws_test.go` `TestNewAwsInvokeContextInheritsParent`（无全局 / 有全局 / 仅渠道值 / 渠道覆盖全局并继承父 cancel）。**缺口**：`doRequest` 的 deadline、`cancelOnCloseBody`、`StreamScannerHandler` 覆盖逻辑没有直接测试。

---

## 6. 免费 token 计数端点（count_tokens / input_tokens）

### 动机

Claude Code / Anthropic SDK 在发送前用 `/v1/messages/count_tokens` 估算上下文占用（决定是否压缩、是否超窗）；Codex / OpenAI SDK 对 Responses API 用 `/v1/responses/input_tokens` 做同样的事。上游 new-api 只有一个基于本地 tokenizer 的 `controller.CountClaudeTokens`，且路由已被注释禁用，客户端拿不到厂商精确值。fork 把这两个请求**原样透传给真实上游**，返回厂商计数。厂商本身不对这两个端点计费，响应也不含可结算 usage，因此网关侧完全跳过计费链，只留一条零额审计日志。

### 新增端点

| 路径 | RelayFormat | RelayMode | 允许渠道类型 |
|---|---|---|---|
| `POST /v1/messages/count_tokens` | `RelayFormatClaude` | `RelayModeClaudeCountTokens` | 仅 Anthropic（type 14） |
| `POST /v1/responses/input_tokens` | `RelayFormatOpenAIResponsesInputTokens`（新增） | `RelayModeResponsesInputTokens` | 仅 OpenAI（type 1） |

- 路由：`router/relay-router.go`，均在 `middleware.Distribute()` 之后。`Path2RelayMode` 中 `/v1/responses/input_tokens` 判断置于 `/v1/responses` 前缀之前。
- 常量：`constant/count_tokens.go`（`ClaudeCountTokensPath`、`OpenAIInputTokensPath`、`IsClaudeCountTokensPath`、`IsOpenAIInputTokensPath`、`IsCountTokensPath`）。
- 分派：`controller/relay.go` `Relay()` 按路径 / RelayFormat 分派到 `relay.ClaudeCountTokensHelper` / `relay.OpenAIInputTokensHelper`，二者都调用 `relay/count_tokens_handler.go` 的 `countTokensPassthrough(c, info, allowedChannelType, endpoint)`。
- 上游 URL：Claude 在 `relay/channel/claude/adaptor.go` `GetRequestURL` 中 `RelayModeClaudeCountTokens` → `{base}/v1/messages/count_tokens`（`claude_beta_query` 开启则追加 `?beta=true`）；OpenAI 适配器未改动，走默认分支得到 `{base}/v1/responses/input_tokens`。
- Azure、Bedrock/Vertex 上的 Claude、其他 OpenAI 兼容类型均不可用；handler 内 `info.ChannelType != allowedChannelType` → 503 作纵深防御。

### 渠道开关

- 字段：`relaykit/dto/channel_settings.go` `ChannelOtherSettings.CountTokensEnabled`，JSON key `count_tokens_enabled`，存于渠道 settings JSON，默认 `false`。
- 过滤：`model/channel_constraint.go` `channelMatchesFilter` 在 `IsCountTokensPath(path)` 时要求 `CountTokensEnabled` 且类型匹配。该判定同时作用于内存缓存路径（`filterCandidateIDs`）、DB 直查路径（`filterAbilitiesByConstraints`）、亲和渠道与令牌指定渠道（`ChannelSatisfiesFilters`）。
- 无可用渠道：随机选择返回 nil → `SelectChannelForRequest` 返回 503 通用"分组 X 下模型 Y 无可用渠道"，不提示开关；仅令牌指定渠道路径有专门 403 文案 "token counting is only available on channels with count_tokens enabled …"。
- `distributor.go`：计数路径成功后不调用 `RecordChannelAffinity`，避免影响正式流量粘性。

### 计费

- `relay/request_billing.go` `PrepareRequestBilling`：`isCountTokens` 为真时，敏感词检查后直接 `return nil`，跳过 `EstimateRequestToken`、`ModelPriceHelper`、`PreConsumeBilling`。**不预扣**，`info.Billing` 为 nil，模型无价格配置也能用。`controller/relay.go` 中 `!isCountTokens` 才执行 `PrepareTieredBillingForSelectedGroup`。
- 日志：`service/text_quota.go` `PostCountTokensLog(ctx, info, inputTokens, endpoint)` 仅在上游 200 时写 `RecordConsumeLog`：`Quota: 0`、`PromptTokens` = 上游响应的 `input_tokens`、`Content: "<endpoint> 调用，不计费"`、`Other.endpoint = "count_tokens" | "input_tokens"`。不触碰用户 / 令牌 / 渠道额度。

### 请求校验与透传

- Claude：复用 `helper.GetAndValidateClaudeRequest`（messages 非空、model 必填、`max_tokens` 上界）。
- OpenAI：新增 `relay/helper/valid_request.go` `GetAndValidateResponsesInputTokensRequest`，只要求 `model`（`input` 可缺，允许 `previous_response_id` / `conversation`）；`relay/common/relay_info.go` 新增 `GenRelayInfoResponsesInputTokens`。
- 透传：`ModelMappedHelper` 解析映射名；若映射则用 `sjson.SetBytes` 只改原始字节的 `model` 字段，否则原样转发；不经 DTO 重建。响应非 200 → `RelayErrorHandler` 进入常规重试；200 → 逐字节返回并复制上游头。无流式。

### 前端

渠道编辑抽屉，字段透传设置区（与 `allow_service_tier`、`claude_beta_query` 同组），`currentType === 14 || 1` 时显示 Switch **允许 count_tokens 端点**，描述按类型："此渠道可处理 /v1/messages/count_tokens 请求（免费，不计费）" / "此渠道可处理 /v1/responses/input_tokens 请求（免费，不计费）"。`channel-form.ts` `buildSettingsJSON` 仅类型 14/1 写入该 key。

### 测试

`middleware/count_tokens_channel_test.go`、`model/count_tokens_channel_filter_test.go`（双路径、无合格渠道）、`relay/channel/claude/count_tokens_url_test.go`、`relay/channel/openai/input_tokens_url_test.go`、`relay/helper/responses_input_tokens_request_test.go`、`relay/constant/relay_mode_test.go`、`web/.../__tests__/count-tokens-setting.test.ts`。**缺口**：`countTokensPassthrough` 本体、`PostCountTokensLog`、`PrepareRequestBilling` 的跳过分支。

### 注意事项

- 上游遗留的 `controller.CountClaudeTokens` 及其测试仍在但未路由。
- 敏感词检查仍生效。
- 计数请求会占用 §8 渠道 RPM/TPM 槽位；上游非 200 会走 `processChannelError`，可能触发渠道自动禁用。
- Claude 校验沿用 `/v1/messages` 规则，`max_tokens` 越界会 400，尽管该字段对 count_tokens 无意义。

---

## 7. 用户 × 模型 RPM/TPM 限流

### 动机

上游只有分组级请求数限流（`ModelRequestRateLimit`）。本功能新增按 **(用户, 分组, 模型)** 粒度的 RPM（每分钟请求数）与 TPM（每分钟 token 数）限制，支持全局 / 分组 / 模型多层规则和用户级覆盖。代码注释明确其定位为"配额管理而非安全边界"，因此 Redis 故障时 **fail-open** 放行。

### 规则存储与匹配

- Option key：`ModelRateLimitEnabled`（bool 总开关）、`ModelRateLimitRules`（JSON 字符串）。`model/option.go` 注册、`setting.CheckModelRateLimitRules` 校验、`setting.UpdateModelRateLimitRulesByJSONString` 生效。
- JSON 结构（`setting/model_rate_limit.go` `ModelRateLimitRulesConfig`）：

  ```json
  {
    "default": {"rpm": 60, "tpm": 200000},
    "models":  {"claude-sonnet-5": {"rpm": 20, "tpm": 80000}},
    "groups":  {"vip": {"default": {"rpm": 120, "tpm": 500000}, "models": {"gpt-4o": {"rpm": 30}}}}
  }
  ```

  叶子类型 `dto.RateLimitValues{Rpm *int; Tpm *int}`（`relaykit/dto/user_settings.go`）：nil = 未设置继续回退，**0 = 明确不限**。取值范围 `[0, 2147483647]`。
- 匹配：`ResolveModelRateLimit(group, model, userOverride)` 对模型名做 **精确匹配**，无前缀 / 通配符。模型名取客户端原始请求名（映射前）；分组取令牌分组，为空回退用户分组；auto 令牌（含 §13 多分组 key）用字面量 `"auto"`。候选顺序：用户覆盖.models[model] > 用户覆盖.default > groups[group].models[model] > groups[group].default > models[model] > default；**RPM 与 TPM 各自独立取第一个非 nil 值**。
- 用户覆盖：`user_extend.rate_limit`，JSON `dto.RateLimitOverride{Default *RateLimitValues; Models map[string]RateLimitValues}`。覆盖是**优先级替换**而非"取更严格"。

### 计数实现

- 窗口：固定 60 秒。
- RPM：Redis 用 Lua 脚本 `common.RedisFixedWindowTake`（`common/redis_fixed_window.go`，INCR+EXPIRE+TTL 原子执行，从 `middleware/rate-limit.go` 抽出，后者现仅委托），键 `rateLimit:v2:mrpm:<userId>:<group>:<model>`。**请求前 +1**，失败请求不退还。无 Redis 时用 `middleware.inMemoryRateLimiter`（单实例）。
- TPM：键 `rateLimit:v2:mtpm:<userId>:<group>:<model>:<unixMinute>`。中间件只读 GET 当前分钟累计，`used >= tpm` 即拒绝；**计入的是真实用量**，由 `service.RecordModelTokensUsed`（`service/model_rate_limit_tokens.go`）在结算时 IncrBy + Expire 3 分钟写入。调用点：`PostTextConsumeQuota`、`PostWssConsumeQuota`、`PostAudioConsumeQuota`。Playground 请求不计入用户 TPM。因事后记账，窗口内最后一个请求可能超冲。无 Redis 时用 `common.ModelTpmMemoryCounter`。
- 超限：HTTP **429**，OpenAI 风格错误体，附 `Retry-After`。文案硬编码中文："您已达到模型请求频率限制(RPM=%d),请 %d 秒后重试" / "您已达到模型 token 用量限制(TPM=%d),请 %d 秒后重试"。`userId == 0` 返回 401。
- Redis 错误：记日志后放行。
- 不影响计费。

### 中间件挂载

`router/relay-router.go`：`/v1` 组 `TokenAuth → ModelRequestRateLimit → ModelRpmTpmRateLimit → Distribute`；`/v1beta` Gemini 组同样顺序。未挂载在 `/pg`、`/mj`、`/v1/models`。

### 管理 API

- 全局规则：现有 option 接口写 `ModelRateLimitEnabled` / `ModelRateLimitRules`。
- 用户覆盖：`controller/user.go` `GetUser` 返回 `rate_limit`；`UpdateUser` 接收 `rate_limit`，经 `setting.CheckRateLimitOverride` 校验后写入。`nil` = 不改动，`{}` = 清除。

### 前端

- 系统设置 → 安全设置 → **模型 RPM/TPM 限流**（`security/section-registry.tsx` id `model-rate-limit`，位于"限流"与"SSRF 防护"之间）。
- `request-limits/model-rate-limit-section.tsx`：总开关 + 规则字段，可在**可视化模式 / JSON 模式**切换。JSON 模式用 `JsonCodeEditor`，zod 只允许 default/models/groups 键。`b7c5bbd2e` 修复了字段跨栏（`data-settings-form-span='full'`）。
- `model-rate-limit-visual-editor.tsx`：把 JSON 展平为 (Group, Model, RPM, TPM) 行表格，支持搜索、增删改，显示 Inherit / Unlimited。
- `model-rate-limit-dialog.tsx`：Group（空 = 全局）、Model（空 = 作用域默认）、RPM、TPM，至少填一项；编辑时作用域不可改。
- 用户编辑抽屉 **限流覆盖**（Rate Limit Override）区块（仅编辑模式）：默认 RPM / 默认 TPM 输入框 + 按模型 JSON 文本域。`user-form.ts` `buildRateLimitOverride` 在更新时总是发送 `rate_limit`。

### 测试

`setting/model_rate_limit_test.go`（优先级、显式 0 禁用、无规则、校验）、`middleware/model_rpm_tpm_limit_test.go`（miniredis：RPM 拒绝与恢复、按模型隔离、TPM、记账键与检查键一致、用户覆盖优先、Redis 错误 fail-open、内存回退、解析失败仍计数、关闭直通）、`model/user_extend_test.go`、前端 `rules-field-layout.test.tsx`。

---

## 8. 渠道级 RPM/TPM 上限

### 动机

为单个渠道设置全渠道（跨所有用户、密钥、模型）的 RPM/TPM 上限，达到上限的渠道在**选路时被跳过**，流量自动转移到其他渠道。用途是贴合上游供应商配额（建议设为略低于上游限额）。独立于 §7 的用户级总开关。

### 存储

`channel_extend.rpm_limit` / `channel_extend.tpm_limit`，0 = 不限，范围 `[0, MaxInt32]`。`model/channel_cache.go` `InitChannelCache` 维护 `anyChannelRateLimit` 标志；`HasAnyChannelRateLimit()` 内存模式读标志，DB 模式按 `rpm_limit > 0 OR tpm_limit > 0` 计数并缓存 1 分钟（出错 fail-open 返回 true）。

### 实现

- `service/channel_rate_limit.go` `TakeChannelRateLimit(c, channel) bool`：**先查 TPM**（只读 GET `rateLimit:v2:ctpm:<channelId>:<minute>`，`used >= tpm` 即饱和，避免浪费 RPM 计数），**再取 RPM**（`RedisFixedWindowTake` 于 `rateLimit:v2:crpm:<channelId>`，60 秒固定窗口，选路时 +1）。Redis 出错 fail-open；饱和仅 `LogWarn`，不暴露给 API 用户。
- `service/channel_select.go` `CacheGetRandomSatisfiedChannel` 改为循环：选出渠道后 `TakeChannelRateLimit`，饱和则 `param.AddFailedChannel(channel.Id)` 后重选，候选集严格收缩，直到选择器返回 nil。含防御性断路（选择器返回已排除渠道则记错误并返回 nil）。
- `SelectChannelForRequest`：**指定渠道**饱和 → 503 `MsgDistributorNoAvailableChannel`；**亲和渠道**在确认分组资格后才 `TakeChannelRateLimit`，饱和则放弃亲和走常规选择。
- TPM 记账：`RecordModelTokensUsed` 渠道侧按 `relayInfo.GetChannelID()` 查 `TpmLimit > 0` 后写 `ChannelTpmRedisKey`（Expire 3 分钟）。**不依赖 `ModelRateLimitEnabled`**，Playground 请求也计入渠道 TPM。
- 与重试关系：每次选路尝试（含重试）都会消耗目标渠道一个 RPM 槽位，失败不退还；饱和渠道在本请求剩余重试中被排除（见 §4 注意事项）。

### 前端

渠道编辑抽屉 → 渠道额外设置，紧随"最大输入 Tokens"：

- **渠道 RPM 上限**："每分钟最多路由到本渠道的请求数（跨所有用户和密钥）… 0 表示不限。"
- **渠道 TPM 上限**："每分钟计入本渠道的最大 token 数。用量在计费结算后记账，建议设置为略低于上游限额。"
- 校验：`渠道 RPM/TPM 上限必须在 0 到 2147483647 之间`。

### 测试

`service/channel_rate_limit_test.go`（内存 RPM/TPM、高优先级渠道饱和后转移到下一渠道、11 个饱和渠道后仍能到达健康渠道、用户级开关关闭时渠道侧仍记账）、`model/channel_extend_test.go` `TestUpsertChannelExtendPersistsRateLimits`、`relaykit/dto/channel_extend_settings_test.go`。**缺口**：前端无针对渠道 rpm/tpm 字段的测试。

---

## 9. 分组 / 用户模型折扣

### 动机

上游只有两种分组维度倍率：`GroupRatio`（按计费分组）和 `GroupGroupRatio`（用户分组 × 计费分组的"专属倍率"），两者都与模型无关。fork 新增"模型"维度：**分组 × 模型折扣**与**用户 × 模型折扣**，作为乘数叠加进最终生效的 `GroupRatio`，不替代上游任何倍率。让管理员对特定分组 / 特定用户的某些模型做定向让价（或加价），且所有结算路径无需改动——它们本来就读 `PriceData.GroupRatioInfo.GroupRatio`。

### 分组级折扣

- Option key：`GroupModelDiscount`，默认 `{}`。`model/option.go` 注册与分发，`controller/option.go` 保存时调 `CheckGroupModelDiscount`。
- 实现：`setting/ratio_setting/group_model_discount.go`，内存结构 `RWMap[string, map[string]float64]`。
- JSON：`{分组: {模型名: 折扣}}`，例如 `{"default": {"gpt-4o": 0.9, "*": 0.95}}`。扁平 map 会被拒绝。
- 匹配：`GetGroupModelDiscount(usingGroup, modelName)` 用 `relayInfo.UsingGroup`（auto 分组解析后的**实际计费分组**）取条目；模型名经 `FormatMatchingModelName` 归一化后由 `MatchModelDiscount`：**精确匹配优先，其次 `"*"` 兜底**，否则 1.0。无前缀 / glob。
- 校验：每个值须在 `(0, 10]`；NaN、≤ 0、> 10 报错。

### 用户级折扣

- 存储：`user_extend.model_discount`，JSON `map[string]float64`（如 `{"gpt-4o": 0.8, "*": 0.9}`）。
- API：`model.User.ModelDiscount`（`json:"model_discount,omitempty"`）；`GetUser` 回填，`UpdateUser` 校验后写入，`nil` = 不改，`{}` = 清除。
- **叠加方式：乘积**。`discountMultiplier = GroupModelDiscount × UserModelDiscount`。读库失败时按 1 处理并记 `SysError`（注释："宁多收不少收"）。

### 计费链路

- 入口：`relay/helper/price.go` `HandleGroupRatio(ctx, relayInfo)`。算出基础分组倍率后：`BaseGroupRatio = GroupRatio`（折前），写入 `GroupModelDiscount`、`UserModelDiscount`，然后 `GroupRatio *= multiplier`；若 `HasSpecialRatio` 则 `GroupSpecialRatio *= multiplier`。
- 受影响字段：只有 `types/price_data.go` `GroupRatioInfo` 中的字段（新增 `BaseGroupRatio`、`GroupModelDiscount`、`UserModelDiscount`）。`ModelRatio`、`CompletionRatio`、`CacheRatio`、`ModelPrice` 等均不变。由于所有额度公式都以 `GroupRatio` 作整体乘数，折扣对输入、输出、缓存读写、图片、音频 token 一律等比生效；**按次计费**与 **tiered_expr** 同样生效。
- 预扣与结算一致：预扣 `tokens × modelRatio × GroupRatio`（含折扣）；结算路径 `service/text_quota.go`、`service/quota.go`、`service/image_billing.go`、`service/tiered_settle.go` 都读 `PriceData.GroupRatioInfo.GroupRatio`。本功能还把 `PreWssConsumeQuota` 从"用 `ratio_setting` 重算倍率"改为读 `relayInfo.PriceData`，否则 realtime 预扣会绕过折扣。
- 任务计费：`relay/relay_task.go` 用 `groupRatioInfo.GroupRatio`；`service/task_billing.go` `LogTaskConsumption` 写折后 `group_ratio` 并调 `appendModelDiscountInfo`。
- 重试换分组后 `controller/relay.go` 重新装配，折扣跟随新分组。
- 折扣以客户端请求的 `OriginModelName` 为 key，而非计费模型名。

### 日志展示

`service/log_info_generate.go` `appendModelDiscountInfo`：仅当值 `!= 1 && != 0` 时写 `other.group_model_discount` / `other.user_model_discount`（public，用户可见）。日志中 `group_ratio` 已是折后值。前端 `usage-logs/.../details-dialog.tsx` 在"分组倍率 / 用户专属倍率"行之后追加"分组模型折扣 / 用户模型折扣"行，仅 ≠ 1 时显示。

### 前端

- 系统设置 → 计费 → **分组定价**（Group Pricing）→ `group-ratio-form.tsx`。可视化编辑器 `group-ratio-visual-editor.tsx`（`5b114b6cf`）增加 **"分组模型折扣"** 标签页：按分组添加（不在定价表内的分组显示警告），每组一张表（模型 / 折扣），对话框输入模型名（提示 `"*"` 匹配该分组所有模型）与折扣（`(0, 10]` 才可保存）；删掉最后一个模型时连带删组。JSON 模式为 `JsonCodeEditor`。
- 用户编辑抽屉（仅编辑态）**模型折扣覆盖**（Model Discount Override）Textarea，`user-form.ts` 校验：空串通过；非数组对象；每个值有限且 `(0, 10]`。
- 文案：分组模型折扣、用户模型折扣、"0.9 = 九折。取值范围 (0, 10]。"、"与该分组的分组倍率相乘生效。`*` 作为该分组下所有模型的兜底折扣"。

### 边界

- 折扣 **0 不允许**（免费必须由显式的 GroupRatio / 模型倍率决定）；**> 1 允许**（最多 10，可当加价系数）；> 10 拒绝以拦截百分数式误填（如 80）。前后端范围一致。
- 空配置 / 无 `user_extend` 行 → 1.0，旧日志与其他路径不受影响。

### 测试

`setting/ratio_setting/group_model_discount_test.go`、`relay/helper/price_test.go` `TestHandleGroupRatioAppliesModelDiscounts`（2×0.5、通配、专属倍率也打折、预扣金额）、`model/user_extend_test.go`、`service/wss_quota_test.go` `TestPreWssConsumeQuotaUsesAssembledPriceData`、前端 `model-discount-validation.test.ts`。

---

## 10. 渠道输入 token 边界（min/max input tokens）

### 动机

每个渠道可配置 `min_input_tokens` / `max_input_tokens`，请求在分发时先估算输入 token，只有估算值落在 **(min, max]** 区间的渠道才可入选。语义为 **min 排他下界（须严格大于）、max 包含上界（须小于等于）**，目的是让两个渠道以同一边界值划分流量而"无缝隙、无重叠"。典型场景：短提示走廉价渠道（设 max）、长上下文走长窗口渠道（设 min）。

### 存储与校验

`channel_extend.min_input_tokens` / `max_input_tokens`，0 = 不限，范围 `[0, 10_000_000]`，两者都 > 0 时要求 `max > min`（`max == min` 也拒绝）。前端 `channel-form.ts` `superRefine` 做同样校验。

### 估算实现

- `service/input_tokens_estimate.go` `EstimateInputTokens(c, modelName) (int, bool)`。
- 支持路径：`/chat/completions`（含 `/pg/`）、`/v1/messages`、`/v1/responses`、`/v1/responses/compact`、含 `:generateContent` / `:streamGenerateContent` 的 Gemini 路径。embeddings、audio、images、任务、count_tokens 探测均不支持 → 不挂过滤器（fail-open）。要求 `Content-Type` 以 `application/json` 开头。
- 估算内容：用 gjson 读取顶层 `messages, system, instructions, input, contents, systemInstruction, system_instruction`，递归（深度 ≤ 8）收集字符串及 `text/content/parts` 下的文本。**不计** tools 定义、图片 / 音频等非文本 part、角色 / JSON 结构开销。
- Tokenizer：`EstimateTokenByModel`（`service/token_estimator.go`）按模型名选权重，调用启发式 `EstimateToken`，**非 tiktoken**，为近似值。
- 位置：`middleware/distributor.go` `Distribute()`，`getModelRequest` 之后、选渠道之前；仅当 `model.HasAnyInputTokensLimit()` 为真才估算；结果作为 `dto.ChannelFilter{Kind: FilterInputTokens, InputTokens}` 写入 `service.GetChannelConstraints(c)`。
- 估算值**不复用于计费预扣**，与计费是两套 token 计数。

### 选择逻辑

- 谓词：`model/channel_constraint.go` `channelMatchesFilter` `case dto.FilterInputTokens`：`ExtendConfig == nil` 放行；`min > 0 && est <= min` 拒；`max > 0 && est > max` 拒。过滤顺序：request_path（含 count_tokens 门控）→ task_plugin_identity → **input_tokens** → responses_websocket。
- 内存缓存模式：`GetRandomSatisfiedChannel` → `filterCandidateIDs`。DB 模式：`filterAbilitiesByConstraints`（`model/ability.go`）批量加载 `channel_extend` 回填后用同一谓词，查询失败 fail-open。`HasAnyInputTokensLimit` DB 模式 1 分钟 TTL 计数。
- 全部被过滤：指定渠道路径返回 400 通用"无可用渠道"；随机选择返回 503 同一消息；distributor 记日志 `no available channel with input_tokens filter active … estimated_input_tokens=%d`。客户端无法区分被输入范围拒绝。
- **Responses WebSocket（`GET /v1/responses`）不经 `Distribute()`，未挂 FilterInputTokens，即 WS 路径不做输入范围门控。**
- 重试：复用首次估算，不重算。

### 前端

渠道编辑抽屉 → 渠道额外设置，位于超时字段之后、RPM/TPM 之前：**最小输入 Tokens** / **最大输入 Tokens**。描述："仅当请求的估算输入 Tokens 大于 / 不超过该值时才会路由到此渠道。估算为近似值，仅对文本类请求生效。0 表示不限制。请为每个模型至少保留一个未设置最小 / 最大输入的渠道。"校验文案：`最小/最大输入 Tokens 必须在 0 到 10000000 之间`、`最大输入 Tokens 必须大于最小输入 Tokens`。两字段属敏感字段。

### 测试

`model/input_tokens_channel_filter_test.go`（双模式边界 100/101/5000/5001、无合格渠道、`HasAnyInputTokensLimit` 及其失效）、`service/input_tokens_estimate_test.go`（各格式文本提取、图片 part 忽略、路径白名单）、`relaykit/dto/channel_extend_settings_test.go`、`controller/channel_authz_test.go`。**缺口**：无前端测试，无 distributor 端到端测试。

---

## 11. Anthropic 渠道认证模式

### 动机

Anthropic 组织 OAuth access token（`sk-ant-oat…`）必须用 `Authorization: Bearer` + `anthropic-beta: oauth-2025-04-20`，普通 key（`sk-ant-api…`）用 `x-api-key`，用错即被拒。本功能让渠道选择方案，或按 key 前缀自动判断，从而支持多 key 渠道混用两类凭据。

### 存储

`channel_extend.claude_auth_mode varchar(16)`。枚举（`relaykit/dto`）：`ClaudeAuthModeApiKey = "api_key"`、`ClaudeAuthModeOAuth = "oauth"`、`ClaudeAuthModeAuto = "auto"`；`""` 等同 `api_key`。`IsZero` 把 `""` 与 `api_key` 都视为零值。

### 实现

- 判定 `dto.ResolveClaudeAuthMode(mode, key)`：`oauth` → oauth；`auto` → key TrimSpace 后以 `sk-ant-oat` 开头则 oauth，否则 api_key；其他 → api_key。只检测 oat 前缀。
- `relay/channel/claude/adaptor.go` `SetupRequestHeader`：在 `CommonClaudeHeadersOperation` **之后**执行。`authMode` **仅当 `info.ChannelType == constant.ChannelTypeAnthropic`（14）** 时取 `ChannelExtendSetting.ClaudeAuthMode`，否则为 `""`——因为 DeepSeek、Moonshot 等类型复用本适配器，须始终 x-api-key。
- `SetClaudeAuthHeader(req, mode, key)`：非 oauth → `Del("Authorization")`、`Set("x-api-key", key)`；oauth → `Del("x-api-key")`、`Set("Authorization", "Bearer "+key)`，`anthropic-beta` 重写为 `oauth-2025-04-20` 置首 + 原有各项（逗号拆分、trim、去重）。常量 `ClaudeOAuthBeta = "oauth-2025-04-20"`。
- 模型列表：`controller/channel.go` `buildFetchModelsHeaders` 对类型 14 先设默认 Claude 头，再 `SetClaudeAuthHeader(&headers, fetchModelsClaudeAuthMode(channel), key)`；`fetchModelsClaudeAuthMode` 优先取请求体 `ExtendConfig`（未保存的表单预览），否则查 `GetChannelExtendSettings`；结果为 `""` 时**回退为 auto**（oat token 用 x-api-key 必失败，前缀检测只会把必败变成可成功）。

### API

`extend_config.claude_auth_mode`：`"api_key" | "oauth" | "auto"`。前端 `buildExtendConfig` 仅当 `type === 14 && mode !== 'api_key'` 时发送。

### 前端

渠道编辑抽屉，类型专属设置区（`currentType === 14`），Select **Claude 鉴权方式**，默认 `api_key`，选项：

- **标准 API 密钥（x-api-key）**："适用于 sk-ant-api01- / sk-ant-api03- 密钥：以 x-api-key 请求头发送"
- **组织访问令牌（Bearer）**："适用于 sk-ant-oat01- / sk-ant-oat03- 令牌：以 Authorization: Bearer 发送，并附加 oauth anthropic-beta 标识"
- **按密钥前缀自动识别**："以 sk-ant-oat 开头的密钥使用 Bearer，其余使用 x-api-key；支持多密钥渠道混用两类凭据"

`parseClaudeAuthMode` 非法值回退 api_key。属敏感字段。

### 兼容性

无行 / `""` → 转发与上游完全一致（x-api-key）；唯一差异是模型列表请求默认 auto。

### 测试

`relay/channel/claude/auth_mode_test.go`：`TestSetClaudeAuthHeader`（6 例，含 beta 合并顺序与互斥头删除）、`TestSetClaudeAuthHeaderAddsOAuthBetaWhenAbsent`、`TestSetupRequestHeaderAppliesAuthModeOnlyForAnthropic`（14 用 Bearer、DeepSeek 用 x-api-key）。

---

## 12. 新建 / 复制渠道默认禁用

### 动机

新建或复制的渠道未经测试，不应承接流量，需管理员测试后显式启用。

### 实现

- 状态值（`common/constants.go`）：`ChannelStatusEnabled = 1`、`ChannelStatusManuallyDisabled = 2`、`ChannelStatusAutoDisabled = 3`。
- **新建**：后端 `AddChannel` 不强制状态，沿用请求体 `status`。默认来自前端 `channel-form.ts` `CHANNEL_FORM_DEFAULT_VALUES.status = CHANNEL_STATUS.MANUAL_DISABLED`（上游为 ENABLED）；管理员可在保存前把开关切回启用。直接调 API 的客户端仍可传 `status = 1`。
- **复制**：后端 `CopyChannel`（`controller/channel.go`）无条件 `clone.Status = ChannelStatusManuallyDisabled`（上游沿用源渠道状态），无参数可关闭。
- **ability 跟随**：`model/ability.go` `AddAbilities` 写入 `Enabled: channel.Status == ChannelStatusEnabled`，因此禁用渠道的 ability 行全部 `enabled = false`，选路不会命中。后续启用走 `UpdateChannelStatus` / `BatchUpdateChannelStatus`。

### 前端

渠道抽屉基本信息区的 `status` 开关（仅新建时显示），描述由"启用或禁用此渠道"改为 **"新渠道默认禁用，测试通过后再启用。"**（en/fr/ja/ru/vi/zh-TW 同步翻译）。

### 测试

`controller/channel_test_internal_test.go` `TestCopyChannelCreatesManuallyDisabledClone`（clone 状态 2、ability 全部禁用、源渠道不变）、`web/.../__tests__/channel-form-defaults.test.ts`（默认值、create payload、显式 status=1 可覆盖）。

---

## 13. API Key 主分组 + 有序备用分组

### 动机

上游只有两种令牌分组模式：单个普通分组（严格只走该分组）和 `auto` 伪分组（按管理员配置的全局 Auto 顺序或令牌自定义快照依次尝试）。fork 新增第三种**多分组令牌**：普通分组作为**主分组**，`auto_groups` 列表作为**有序备用分组**，主分组先试，失败或无渠道时按顺序回退。解决的问题：用户想让某个 key 主要走 vip、耗尽后再走 default，而不需要管理员把 `auto` 加入 UserUsableGroups；顺序由用户自己决定。实现上完全复用上游 auto 路径，**未改动 `service/channel_select.go`**。

### 数据模型

无 schema 变更。复用 `tokens` 表既有列：`group`（主分组）、`auto_groups`（text，JSON 字符串数组）、`cross_group_retry`。上游该列仅在 `group = "auto"` 时有意义，普通分组会被控制器清空；fork 让它在普通分组下承载备用分组。

- `(*Token).IsMultiGroup()`：`Group != ""` 且 `Group != "auto"` 且 `AutoGroups` 可解析为非空数组。
- `(*Token).GetRoutingGroups()`：返回 `[主分组, 备用1, 备用2, …]`，去掉与主分组重复的项；非多分组返回 nil。

### 创建 / 更新校验（`controller/token.go`）

`applyTokenGroupBinding(c, token, input, previousGroup)` 被 `AddToken` 与 `UpdateToken` 调用：

- `Group == "auto"`：走原 `setTokenAutoGroups`，与上游一致。
- 普通分组且请求带 `auto_groups`：为空 / null → 清空列表并强制 `CrossGroupRetry = false`；非空 → `setTokenFallbackGroups`。
- 普通分组且**未传** `auto_groups`：若 previousGroup 也是普通分组，沿用库中已存备用分组并剔除新主分组；若从 `auto` 切到普通分组，视为无备用分组。

`setTokenFallbackGroups` 校验：数量 `len(fallbacks) + 1 > MaxTokenAutoGroups` 报错（**主分组计入上限**，选项键 `MaxTokenAutoGroups` 默认 5）；主分组必须 `IsUserSelectableGroup`；每个备用分组不能等于主分组、不能重复、必须可选。i18n 常量在 `i18n/keys.go`（5 个），文案在 `i18n/locales/{en,zh-CN,zh-TW}.yaml`，例如"每个令牌最多可设置 {{.Max}} 个备用分组"、"备用分组 {{.Group}} 已是主分组"。

### 请求时行为（`middleware/auth.go`）

- `TokenAuth`：若 `IsMultiGroup()`，不再做上游的"tokenGroup 必须在 UserUsableGroups 中"严格检查，而是 `FilterUserTokenAutoGroups(userGroup, GetRoutingGroups())` 为空才 403；只要主分组或任一备用分组仍可选，请求放行，`ContextKeyUsingGroup = "auto"`。**主分组被管理员撤销时仍可靠备用分组路由。**
- `SetupContextForToken`：多分组时 `ContextKeyTokenGroup = "auto"`，`ContextKeyTokenAutoGroups = GetRoutingGroups()`；`CrossGroupRetry` 照常写入。纯 auto 令牌与单分组令牌走原分支不变。
- 由此复用的上游 auto 路径：渠道选择 `cacheGetRandomSatisfiedChannelOnce` **按顺序**遍历分组（非随机），无渠道则切下一组；跨分组重试 `crossGroupRetry && priorityRetry >= RetryTimes` 时切换下一组；计费选中分组写入 `ContextKeyAutoGroup`，`HandleGroupRatio` 按实际分组取倍率，日志记录该分组；模型列表 `getModelListGroups` 返回主分组 + 备用分组模型的并集；会话亲和在路由列表中找首个包含该渠道的分组。
- 副作用：§7 的 RPM/TPM 限流读到的分组为 `"auto"`，与纯 auto 令牌一致，而非主分组的限流配置。

### 与 auto 分组开关的关系

上游要求管理员把 `auto` 加入 UserUsableGroups 才能使用 auto 令牌。多分组令牌绕过这一检查，仅要求绑定分组本身可用，所以 "auto 伪分组不必为用户开放"；前端在 `auto` 未开放时依然显示备用分组编辑器。

### 前端（`web/src/features/keys/`）

- `fallback-group-order-editor.tsx`：新组件 `FallbackGroupOrderEditor`，复用 `AutoGroupOrderItem` + `Reorder.Group` 拖拽排序、上下移动、删除；候选项排除 `auto`、主分组和已选项；显示 "{{count}} / {{max}} 个备用分组"，达上限禁用。
- `api-keys-mutate-drawer.tsx`：`maxFallbackGroups = maxAutoGroups − 1`；选中普通分组且上限 > 0 时显示编辑器；切换主分组时从备用列表剔除新主分组，若剩余为空则关闭 `cross_group_retry`，首次添加备用分组时自动开启。
- `api-key-form.ts`：schema 新增 `fallback_groups`，校验数量、重复、含主分组；`transformFormDataToPayload` 把备用分组写入同一 `auto_groups` 字段；回填时按 `group` 是否为 auto 分流到 `auto_groups` 或 `fallback_groups`。
- `api-key-group-cell.tsx` + `api-keys-columns.tsx`：非 auto 且有备用分组时显示 `GroupBadge` + **"+N"** 徽标，tooltip "回退顺序：vip → default → svip"。
- 文案：备用分组、添加备用分组、暂无备用分组、回退顺序，及三条校验消息（en/zh/zh-TW）。

### 兼容性

无迁移。上游控制器对普通分组一律清空 `auto_groups`，因此存量普通分组行 `IsMultiGroup()` 为 false，走原单分组分支；纯 auto 令牌被 `IsMultiGroup` 排除，逻辑与上游等价。`auto_groups` 解析失败时多分组判定为 false，退回主分组单分组语义。

### 测试

`controller/token_fallback_groups_test.go`（新，约 300 行：创建 / 更新的全部场景、`TestTokenAuthRoutesMultiGroupTokensWithoutAutoGroup` 走真实 `TokenAuth`）、`controller/token_auto_groups_test.go`（调整）、`middleware/token_auto_groups_context_test.go`（归一化、去重、坏 JSON 回退、auto 字面量保留）、前端 `api-keys-mutate-drawer.test.tsx`、`auto-group-form.test.ts`。

---

## 14. 渠道响应头过滤（黑名单 / 白名单）

### 动机

上游只有一条硬编码规则：`service/http.go` 的 ShouldCopyUpstreamHeader 除 `Content-Length` 与 `X-Oneapi-Request-Id` 外**全部复制**上游响应头。非流式透传路径因此会把 `openai-organization`、`x-ratelimit-*`、`anthropic-ratelimit-*`、`cf-ray`、`set-cookie` 等直接透给客户端，而流式路径则一律不复制。本功能允许按渠道决定哪些上游响应头可以回传：**黑名单**丢弃列出的头，**白名单**只保留列出的头。

### 存储与校验

- 列：`channel_extend.response_header_mode varchar(16)`，取值 `""`（关闭，复制全部）/ `blacklist` / `whitelist`；`channel_extend.response_headers text`，JSON 字符串数组。
- DTO：`extend_config.response_header_mode`、`extend_config.response_headers`（`relaykit/dto/channel_extend_settings.go`），常量 `ResponseHeaderModeBlacklist` / `ResponseHeaderModeWhitelist`、`MaxChannelResponseHeaderRules = 64`、`MaxChannelResponseHeaderNameLength = 128`。
- `Validate()`：模式必须是枚举值；有模式必须至少列出一个名称，无模式不能携带名称；最多 64 个，每个 1-128 字符，仅允许 RFC 7230 token 字符，不区分大小写去重。`IsZero()` 把两字段纳入判断，只配置了过滤的行不会被当作全零删除。
- 模型层 `model/channel_extend.go` 用 `common.Marshal` / `common.Unmarshal` 在 `[]string` 与 text 列之间转换；解码失败记 `SysError` 并按空列表处理。
- `[]string` 使 DTO 结构体不可直接比较，`controller/channel_authz.go` 的敏感变更判定改为比较两侧的 JSON 序列化结果（`omitempty` 使 nil 与空数组等价）。

### 过滤实现

- `relaykit/dto` 新增方法 `ChannelExtendSettings.AllowsResponseHeader(name)`：无模式放行；`Content-Type`、`Content-Encoding` **始终放行**（丢掉它们会让响应体不可解释）；黑名单未列出才放行；白名单列出才放行；名称匹配 TrimSpace 且不区分大小写。
- `service/http.go` ShouldCopyUpstreamHeader 在原有两条固定规则之后，从 gin 上下文 `ContextKeyChannelExtendSetting` 读取选中渠道的设置并调用该方法；上下文中没有设置时保持原行为。
- 生效范围是所有经 ShouldCopyUpstreamHeader 的复制点：`IOCopyBytesGracefully`（24 处非流式透传，含 count_tokens）、`copyCodexSSEHeaders`（流式路径仅复制的两个 Codex 头）、`relay/channel/openai/audio.go`、`relay/channel/minimax/tts.go`。只设置 Content-Type 后重新序列化 JSON 的适配器本来就不复制上游头，不受影响。

### 前端

渠道编辑抽屉 → 渠道额外设置，位于"渠道 TPM 上限"之后：Select **响应头过滤**（关闭 / 黑名单 / 白名单，随选项显示说明文案），选择非关闭时显示 Textarea **过滤的响应头**（每行一个名称，逗号亦可）。表单校验对应四条文案：至少一个名称、最多 64 个、名称字符非法、名称重复。两字段列入敏感字段与"渠道额外设置"的已配置标记。`buildExtendConfig` 在关闭时不发送这两个字段，后端视为清除。

### 兼容性

无行或模式为空时行为与上游完全一致。新增两列由 AutoMigrate 添加，text 列不设默认值。

### 测试

`relaykit/dto/channel_extend_settings_test.go`（Validate 新增 9 例、IsZero、`TestChannelExtendSettingsAllowsResponseHeader`）、`model/channel_extend_test.go` `TestUpsertChannelExtendPersistsResponseHeaderFilter`（往返、原地覆盖、关闭后清空、二次 AutoMigrate 幂等）、`service/http_test.go`（新增，覆盖 ShouldCopyUpstreamHeader 与 IOCopyBytesGracefully 端到端过滤）、`controller/channel_authz_test.go`（过滤变更属敏感、空数组等价于未配置）、前端 `lib/__tests__/response-header-filter.test.ts`（名称解析、四条校验、payload 映射、编辑回填）。

### 数据库验证

SQLite（glebarez/sqlite v1.11.0，modernc.org/sqlite v1.40.1，GORM v1.25.12）通过模型测试验证，含二次 AutoMigrate。**MySQL 与 PostgreSQL 本机没有实例，尚未验证**，部署前需在这两种数据库上确认 AutoMigrate 新增两列并重复启动无重复 ALTER。

---

## 15. 渠道额度上限与成本倍率（达上限自动禁用）

### 动机

预付费 / 折扣渠道通常按"买了多少美元额度"计量。上游只维护 `channels.used_quota` 累计消耗，没有"消耗到多少就停"的能力，也没有成本口径。本功能为渠道增加三项：**成本倍率**（每计费 $1 站内额度实际付给上游多少美元）、**额度上限**（累计消耗达到即自动禁用）、以及由前两者算出的**按倍率成本**（只展示，不落库）。上限比较的对象是站内计费消耗 `used_quota`，不是折算成本。

### 存储与校验

- 列：`channel_extend.cost_ratio double`（0 = 未设置）、`channel_extend.quota_limit bigint`（quota 单位，`common.QuotaPerUnit` = $1，0 = 不限）。以 quota 单位存储是为了与 `used_quota` 同单位比较，管理员修改 `QuotaPerUnit` 后判定不漂移。
- DTO：`extend_config.cost_ratio`、`extend_config.quota_limit`（`relaykit/dto/channel_extend_settings.go`），常量 `MaxChannelCostRatio = 1000`、`MaxChannelQuotaLimit = 2^53 − 1`（JS 安全整数）。
- `Validate()`：成本倍率必须是 `[0, 1000]` 内的有限数（拒绝 NaN / ±Inf），上限 `[0, 2^53 − 1]`。`IsZero()` 纳入两字段。
- 新方法 `ChannelExtendSettings.QuotaLimitReached(used int64) bool`：`quota_limit > 0 && used >= quota_limit`，包含边界。`model.Channel.QuotaLimitReached()` 读取渠道设置后调用它，所有启用路径共用。

### 判定与禁用（`model/channel.go`）

- 检查挂在 `updateChannelUsedQuota` 内。它是所有结算调用点（`service/quota.go`、`service/text_quota.go`、`service/task_billing.go`、`service/violation_fee.go`、`service/midjourney.go`、`relay/mjproxy_handler.go`）与批量更新刷盘（`model/utils.go` `batchUpdate`）的共同出口，因此不改任何计费文件，直写模式与 `BATCH_UPDATE_ENABLED` 模式都覆盖。
- 流程 `enforceChannelQuotaLimit(id)`：增量 ≤ 0（退款 / 差额回减）直接返回；`GetChannelExtendSettings(id)` 无上限时零开销返回；有上限则**读回**数据库中的 `status` 与 `used_quota`（多副本与批量刷盘都以持久化值为准）；渠道已非启用状态则返回；达到上限 → `UpdateChannelStatus(id, "", ChannelStatusAutoDisabled, reason)`，reason 形如 `额度已达上限：已用 $12.3456，上限 $10.0000`（前缀常量 `ChannelStatusReasonQuotaLimitReached`）。`UpdateChannelStatus` 自身负责内存缓存与 abilities 同步，状态未变化时返回 false，天然去重。
- 通知：model 不能导入 service，故通过钩子 `model.ChannelQuotaLimitReachedHandler` 由 `service/channel.go` 的 `init()` 注册 `notifyChannelQuotaLimitReached`：关闭该渠道的活动 WebSocket（与 `DisableChannel` 一致），`NotifyRootUser(channel_update_<id>_3, …)` 发送含已用 / 上限 / 折算成本的通知。
- **不受**渠道"自动禁用"开关（`auto_ban`）与全局 `AutomaticDisableChannelEnabled` 约束：上限是管理员显式设置的。

### 防止被重新启用

已耗尽渠道在管理员调高上限前不得回到启用状态，四条启用路径全部加了守卫：

| 路径 | 行为 |
|---|---|
| 定时测试自动启用（`controller/channel-test.go` → `service.ShouldEnableChannel`） | 签名改为接收 `*model.Channel`，`QuotaLimitReached()` 为真时返回 false |
| 单个启用（`controller.UpdateChannelStatus`） | 目标为启用且渠道已耗尽 → `success:false`，消息 `channel.quota_limit_reached`（"该渠道已达额度上限，请先调高额度上限再启用"） |
| 批量启用（`controller.BatchUpdateChannelStatus`） | 跳过已耗尽渠道，不计入 `data`；有跳过时 `message` 为 `channel.quota_limit_skipped`（"N 个渠道已达额度上限，保持禁用"） |
| 标签启用（`model.EnableChannelByTag` → `controller.EnableTagChannels`） | 返回值改为 `([]int, error)`，已耗尽渠道不更新状态并把其 abilities 重新置为禁用；控制器在有跳过时返回同一条消息 |

`restoreMultiKeyChannelIfAvailable` 只恢复原因为 `All keys are disabled` 的渠道，不会误恢复。多 Key 渠道以整渠道维度禁用（`usingKey` 为空即写渠道级状态与原因）。后端 i18n 新增 `channel.quota_limit_reached` / `channel.quota_limit_skipped`（en / zh-CN / zh-TW）。

### 前端

- 渠道编辑抽屉 → 渠道额外设置，紧随"渠道 TPM 上限"：**渠道成本倍率**（number，step any）与**渠道额度上限（USD）**（标签随货币显示模式变化；输入用 `parseQuotaFromDollars` / `quotaUnitsToEditableAmount` 与 quota 单位互转）。编辑已有渠道时帮助文案追加"当前累计已用 X"与"按倍率折算的累计成本 Y"。表单字段名分别为 `cost_ratio`、`quota_limit_amount`（显示货币金额）。
- 校验文案：`渠道成本倍率必须在 0 到 1000 之间`、`渠道额度上限必须为 0 或正数`（含超出安全整数范围）。两字段列入敏感字段与"渠道额外设置"已配置标记。
- 渠道列表 `BalanceCell`：设置了上限的渠道"已用"徽标变为 `已用 · 83%`，≥ 90% 为 warning、≥ 100% 为 danger；悬浮提示追加"额度上限：$Y (83%)"与"折算成本：$Z (×0.2)"。为此列表 / 搜索接口的每一行现在附带非零 `extend_config`（`controller.attachChannelExtendConfigs`，一次 `WHERE channel_id IN (...)`）。标签聚合行不显示上限与成本。
- 被自动禁用后，状态列的原因提示直接显示上述 reason 文本。

### 兼容性

无行或两字段为 0 时行为与上游完全一致。两列由 AutoMigrate 添加并带默认值 0，旧行读出为"未设置"。`used_quota` 是渠道建立以来的累计值，续费时需把上限调高（上限 = 旧上限 + 新充值），而不是填"剩余额度"；不提供重新计数。任务退款会让 `used_quota` 回落，已禁用的渠道不会因此自动恢复。

### 测试

`relaykit/dto/channel_extend_settings_test.go`（Validate 新增 8 例、IsZero、`TestChannelExtendSettingsQuotaLimitReached`）、`model/channel_extend_test.go`（`TestUpsertChannelExtendPersistsQuotaLimit` 含二次 AutoMigrate 与 `GetChannelExtendsByIds`；`TestUpdateChannelUsedQuotaDisablesChannelAtQuotaLimit` 覆盖未达 / 恰好达到 / 禁用后继续结算不重复通知 / 无上限渠道不受影响；`TestUpdateChannelUsedQuotaRefundNeverDisables`；`TestEnableChannelByTagKeepsExhaustedChannelsDisabled` 含 abilities 断言）、前端 `lib/__tests__/channel-quota-limit.test.ts`（倍率与上限校验、显示货币 → quota 单位换算、编辑回填）。

### 数据库验证

SQLite（glebarez/sqlite，GORM）通过模型测试验证，含二次 AutoMigrate。**MySQL 与 PostgreSQL 本机没有实例，尚未验证**，部署前需确认 AutoMigrate 新增 `cost_ratio`（double）与 `quota_limit`（bigint）两列且重复启动无重复 ALTER。

---

## 16. 渠道可用时段（时段外不参与选路）

### 动机

某些渠道只在特定时段可用或划算（供应商优惠时段、账号限制、备用线路只在夜间兜底）。上游选路只看分组、模型、状态、优先级和权重，没有任何读取时间的逻辑。本功能为渠道配置一组按周几加时分的可用窗口：**时段外的渠道不参与选路**，流量自动转到其他渠道；渠道状态不变，不发通知，窗口一到自动恢复参与。已确认的三个决策：粒度为周几 + HH:MM；指定渠道在时段外视同渠道不可用；时段外完全不可用，不做降级兜底。

### 存储与校验

- 列：`channel_extend.schedule text`，JSON 形如 `{"timezone":"Asia/Shanghai","windows":[{"days":[1,2,3,4,5],"start":"09:00","end":"18:00"},{"start":"22:00","end":"06:00"}]}`；空 = 全天可用。
- DTO：`extend_config.schedule`，类型 `dto.ChannelSchedule{Timezone, Windows []ChannelScheduleWindow{Days, Start, End}}`（`relaykit/dto/channel_extend_settings.go`），常量 `MaxChannelScheduleWindows = 16`。`Days` 用 0（周日）到 6（周六），空 = 每天。
- `Validate()`：时区必填且可通过 `time.LoadLocation` 加载（进程内 `sync.Map` 缓存，避免每次选路重新解析 tzdata）；窗口 1 到 16 个；`Start` / `End` 必须是严格的 `HH:MM`；开始不能等于结束；星期在 0 到 6 且不重复。`IsZero()` 纳入该字段，敏感变更判定沿用现有的 JSON 比较。
- `ChannelSchedule.Contains(at)`：把时刻换算到规则时区后按分钟比较。开始含、结束不含；`End <= Start` 表示跨午夜，归属开始的那一天（周五 22:00 到 06:00 覆盖周六 05:59，不覆盖周六 23:00）；多个窗口取并集。时区在运行时加载失败按"规则不生效、全天可用"处理（校验阶段已拒绝未知时区，这只在缺 tzdata 的镜像里发生）。`ChannelExtendSettings.AvailableAt(at)` 是选路谓词的入口。

### 选路接入

- 新过滤种类 `dto.FilterChannelSchedule`（`dto/channel_constraints.go`），携带 `At time.Time`。`model/channel_constraint.go` `channelMatchesFilter`：`ExtendConfig == nil` 放行，否则 `AvailableAt(filter.At)`；评估顺序排在 `input_tokens` 之后。新增 `model.FiltersReadExtendConfig(filters)`，DB 模式的 `filterAbilitiesByConstraints` 与指定 / 亲和渠道的 `fillExtendConfigForFilters`（原 `fillExtendConfigForInputFilter`）都用它决定是否回填 `channel_extend`。
- 挂载点在 `service/channel_select.go` `SelectChannelForRequest` 开头：`HasAnyChannelSchedule()` 为真且本请求尚未挂载时附加过滤器，`At` 取 `ContextKeyRequestStartTime`（缺省 `time.Now()`），同一请求的所有重试复用同一时刻。HTTP distributor 与 Responses WebSocket 都经由该函数，两条路径同时生效（顺带补上了 §10 输入 token 边界未覆盖 WebSocket 的缺口，但仅限时段过滤）。
- `HasAnyChannelSchedule()`（`model/channel_cache.go`）：内存模式由 `InitChannelCache` 维护 `anyChannelSchedule`；DB 模式按 `schedule <> ''` 计数并缓存一分钟，`InitChannelCache` 使其立即失效。无渠道配置时段时零开销。
- 指定渠道（token pin）在时段外：视同渠道不可用，返回 403 `distributor.channel_disabled`，规则本身只进日志（`pinned channel N is outside its availability schedule`）。`origin_task` pin（任务轮询 / 取结果必须回到创建任务的渠道）**豁免**时段过滤。亲和渠道在时段外：放弃亲和走常规选择。候选全部时段外：503 通用"无可用渠道"，distributor 记 `no available channel with channel_schedule filter active`。
- 渠道状态不变、不通知；健康检查与手动测试直接指定渠道不经选路，时段外仍会照常测试；时段过滤在候选集阶段剔除，不进入 RPM/TPM 循环，不消耗 RPM 槽位。

### 前端

- 渠道编辑抽屉 → 渠道额外设置末尾：**可用时段** Switch；开启后显示 **时段时区**（`Combobox`，候选来自 `@/lib/timezones` 的 `COMMON_TIMEZONES`，该列表自 pricing 迁出并在 `billing-expr.ts` 保留 re-export；允许输入其他 IANA 名称）与 **可用时间窗口** 列表（每行星期 `MultiSelect` + 开始 / 结束 `Input type="time"` + 删除按钮；"添加时段"最多 16 条）。表单字段 `schedule_enabled` / `schedule_timezone` / `schedule_windows`，列入敏感字段与已配置标记；`buildExtendConfig` 关闭时不发送 `schedule`。
- 校验文案：需要指定时区；至少一个时段或关闭；最多 16 个；HH:MM 格式；开始结束不能相同。
- 渠道列表：状态列对"启用但当前时段外"的渠道追加中性徽标 **时段外**，悬浮显示时区与各窗口（星期名按界面语言本地化）。卡片视图原本对启用渠道隐藏状态徽标，时段外时改为显示。浏览器端判定 `isInsideChannelSchedule`（`lib/channel-utils.ts`）用 `Intl.DateTimeFormat` 按规则时区换算，与后端同规则。

### 兼容性

无行或 `schedule` 为空时行为与上游完全一致。新列由 AutoMigrate 添加，text 无默认值，旧行 NULL 视为无规则。多副本各自以本机时钟判定，依赖 NTP；内存缓存模式下管理端保存立即刷新本副本，其他副本最长 `SYNC_FREQUENCY` 后生效。

### 测试

`relaykit/dto/channel_extend_settings_test.go`（Validate 新增 11 例、IsZero、`TestChannelScheduleContains` 覆盖边界含 / 不含、周末、跨午夜归属、多窗口、时区换算、时区加载失败）、`model/channel_extend_test.go` `TestUpsertChannelExtendPersistsSchedule`、新文件 `model/channel_schedule_filter_test.go`（内存缓存 / DB 双模式窗口过滤、跨午夜次日凌晨、无合格渠道、`HasAnyChannelSchedule` 及 DB 模式 TTL 失效）、`service/channel_select_exclude_test.go`（时段外渠道被剔除、时段内两渠道都参与、同一请求过滤器只挂一次、token pin 时段外 403、origin_task pin 豁免、时段内 pin 正常）、前端 `lib/__tests__/channel-schedule.test.ts`（时段判定、校验、payload、编辑回填）。本地端到端：DB 模式与 `MEMORY_CACHE_ENABLED=true` 各一轮（两渠道分流 → 一个设为时段外后 20 次请求全部转移 → 改回时段内恢复分流 → 两个都时段外返回 503 且状态仍为启用）；Chrome 界面检查抽屉编辑器与"时段外"徽标。

### 数据库验证

SQLite（glebarez/sqlite，GORM）通过模型测试验证，含二次 AutoMigrate。**MySQL 与 PostgreSQL 本机没有实例，尚未验证**，部署前需确认 AutoMigrate 新增 `schedule` text 列且重复启动无重复 ALTER。

---

## 17. 实时 RPM 与 TPM 统计

### 动机

上游的 RPM / TPM 只在使用日志页显示，每次查看都对 logs 表执行"最近 60 秒消费日志的条数和 token 之和"：一次只能按当前筛选看一组数，依赖消费日志开关，而且 `prompt_tokens` 的口径随上游格式变化（OpenAI 格式含缓存，Claude 格式不含），缓存多的 Claude 流量 TPM 明显偏低，也无法细分缓存。本功能用 Redis 统计两类实时数据：渠道 / 用户列表上的 RPM、TPM 及其明细，以及使用日志页的 RPM、TPM 和四项细分。

### 口径

- **分发次数**（渠道 / 用户列表的 RPM）：计数点是 `RequestPolicyState.BeginAttempt`（`service/request_policy.go`），普通转发、Responses WebSocket 每一轮、异步任务提交、Midjourney 都经过它。渠道 RPM 按每次分发计，重试与失败都计；用户 RPM 只在请求首次分发时计一次。用户 ID 在请求策略状态创建时记录，此时已完成鉴权。
- **结算请求与 token**（TPM 与使用日志页的 RPM）：在写消费日志的同一处计数（`RecordSettledUsage`），记录的用户名、令牌名、模型名、渠道、分组与那条日志完全一致。覆盖文本（含 Responses）、音频、实时语音、异步任务、Midjourney；渠道测试、违规扣费、免费 count_tokens 不是真实流量，不计。
- **四项 token**：输入（不含缓存）、缓存读取、缓存写入、输出；TPM 是四项之和。OpenAI 格式的输入数包含缓存读取与写入，减掉后按 0 封底（缓存写入上报的前缀计数可能重叠）；Claude 格式、旧版 Claude 转 OpenAI 用量、OpenRouter 的 Claude 计费，输入本来就不含缓存。判定规则与计费一致（`settledTextTokens`、`settledOpenAITokens`，`service/token_stats.go`）。只读取计费已算好的汇总，不改变扣费。
- 不计入：渠道测试、自动健康检查、任务轮询，以及未选到渠道就被拒绝的请求。

### 实现（`service/rpm_stats.go`、`service/token_stats.go`）

- 事件只在进程内存累加；有 Redis 时后台每 2 秒用一个 pipeline 批量写入，所有键 120 秒过期。Redis 写入次数只与实例数有关，多个实例写同一组键自然相加；写入失败丢弃该批（每分钟最多一条错误日志），不重试。
- 10 秒一个桶，读取时取结束时间早于"当前时间减 4 秒"的最近 6 个桶，正好一分钟；数字比当前时刻晚 4 到 14 秒，但不会读到写了一半的桶。
- 键名 `rpmstat:<kind>[:<owner>]:<bucket>`（`bucket` = unix 秒 / 10）：
  - 分发次数：`ch`（字段渠道 ID）、`u`（用户 ID）、`cu:<channelId>`（用户 ID）、`uc:<userId>`（渠道 ID）。
  - 结算统计，字段为"指标 \x1f 值"，指标为 `req` / `in` / `cr` / `cw` / `out`：汇总层 `s:all`、`s:ch`、`s:u`、`s:m`（模型）、`s:g`（分组）、`s:t`（令牌名）、`s:cu:<channelId>`；明细层 `s:d:<userId>`，值为"令牌名 \x1f 模型 \x1f 渠道 \x1f 分组"；`s:users` 记录用户 ID → 用户名，供用户名筛选使用，不查用户表。
- 使用日志页的读取（`LogRate`）：无筛选、单一精确条件（渠道 / 用户名 / 模型 / 分组 / 令牌名）、渠道 + 用户名直接读汇总层；其余组合（含 `%` 模糊匹配）读匹配用户的明细层再过滤，普通用户查看自己的日志只读本人明细。筛选语义与原 SQL 一致：精确匹配，或最多 2 个 `%` 的模糊匹配（关键词至少 2 个字符，`_` 按字面），模糊匹配不区分大小写；不合法的模式返回与原查询相同的错误。
- 无 Redis：桶留在进程内存直接读取，只统计本实例，返回 `source: memory`。Redis 读取失败时日志页返回 `rate_source: unavailable`，额度照常返回。
- 开关：环境变量 `RPM_STATS_ENABLED`，默认 `true`；关闭后返回 `source: disabled`。
- 专用 Redis：设置 `RPM_STATS_REDIS_CONN_STRING`（格式同 `REDIS_CONN_STRING`；连接池 `RPM_STATS_REDIS_POOL_SIZE`，默认 10）后，全部统计键改写到这个 Redis，主 Redis 不再承担统计的写入、内存和明细扫描；主 Redis 未开启时也能单独使用。启动时连不上只记日志并自动重连，期间统计少计、读取显示不可用，网关照常转发；连接串格式错误时退回本实例内存统计，绝不改用主 Redis。统计数据可丢，专用 Redis 不需要持久化，建议设置内存上限并使用 `allkeys-lru`。

### 接口

| 端点 | 权限 | 返回 |
|---|---|---|
| `GET /api/channel/rpm?ids=1,2,3` | 管理员 + 渠道读权限 | 各渠道 RPM（`items`）与 token 统计（`tokens`），一次最多 200 个 ID |
| `GET /api/channel/:id/rpm/users?limit=20` | 管理员 + 渠道读权限 | 该渠道下的用户，每行含 RPM 与 token 统计；`limit` 最大 100 |
| `GET /api/user/rpm?ids=1,2,3` | 管理员 | 各用户 RPM 与 token 统计 |
| `GET /api/user/:id/rpm/channels?limit=20` | 管理员 | 该用户在各渠道的 RPM 与 token 统计（渠道拆分取自该用户的明细层） |
| `GET /api/log/stat`、`GET /api/log/self/stat` | 原权限不变 | `quota` 仍按所选时间段查 logs 表；`rpm`、`tpm` 改为实时统计，新增 `tpm_input`、`tpm_cache_read`、`tpm_cache_write`、`tpm_output`、`rate_source`、`rate_window_start`、`rate_window_end`；带 `rate_only=true` 时跳过额度查询 |

token 统计的字段为 `requests`、`input`、`cache_read`、`cache_write`、`output`。明细接口另返回 `total`（RPM 之和）与 `total_tokens`，不受 `limit` 截断。`model.SumUsedQuota` 不再执行 RPM / TPM 的那条 SQL，只算额度。

### 前端

- 共享组件 `web/src/components/live-rpm.tsx`：`useLiveRpmTotals` 按当前页 ID 每 10 秒轮询（页面不可见时暂停，失败时显示"-"而不是旧数字）；`LiveRpmCell` 点开为懒加载的明细弹层，每行显示 RPM 与 TPM；`LiveTpmCell` 显示 TPM，点开为四项细分。`QuotaDetailsPopover` 兼容扩展了可选的 `onOpenChange`。
- 渠道列表与卡片视图、用户列表：新增 RPM、TPM 两列；标签聚合行显示其下渠道之和；隐藏敏感信息时明细只隐藏用户名。
- 使用日志页统计条：额度、RPM、TPM、输入 TPM、缓存读取 TPM、缓存写入 TPM、输出 TPM。RPM / TPM 用 `rate_only` 请求单独每 10 秒刷新，不重复执行额度 SQL；`memory` 时标注"仅统计当前服务实例"，不可用时显示"-"。
- 7 种语言新增 14 条文案。

### 兼容性

不改表结构。默认开启，但只计数，不改变请求处理与扣费；`RPM_STATS_ENABLED=false` 可完全关闭。使用日志页 RPM / TPM 的口径有三处变化：窗口改为"最近一个完整分钟"（原为实时的最近 60 秒）；TPM 统一包含缓存（Claude 流量会变大）；不再计入渠道测试、违规扣费与 count_tokens。

### 测试

`service/rpm_stats_test.go`：经 `BeginAttempt` 的重试计数口径；miniredis 下两个实例写同一个 Redis 后相加（分发次数与结算 token），窗口只含已结算的完整一分钟，所有键都带过期；关闭时返回 `disabled`；配置专用 Redis 后统计键只落在专用 Redis、主 Redis 不增加任何键，连接串错误时退回内存而不是主 Redis；四种用量格式的输入去缓存规则（OpenAI、重叠封底、Claude、Claude 拆分写入、旧版 Claude 转换）与实时语音；使用日志页 14 种筛选组合在内存与 Redis 两种模式下结果相同，非法模式返回错误。前端 `components/__tests__/live-rpm.test.tsx`（明细行 RPM 与 TPM、标签行 TPM 求和与细分弹层、缺数据显示"-"等）与 `features/usage-logs/components/__tests__/live-rate-stats.test.tsx`（细分徽标、`rate_only` 轮询带同样的筛选、不可用显示"-"、仅本实例提示）。

本地端到端（专用 Redis）：主 Redis 与统计 Redis 分别是两个 miniredis，两个网关实例都配置 `RPM_STATS_REDIS_CONN_STRING`。上述 11 种日志页筛选组合全部与预期一致，主 Redis 上的 `rpmstat:*` 键数量不变，新键全部出现在统计 Redis。统计 Redis 停掉后重启网关：启动日志提示 ping 失败，转发正常，日志页额度照常、RPM / TPM 显示不可用；统计 Redis 恢复后无需重启即恢复。

### 数据库验证

没有表结构变更，也没有新增 SQL；`SumUsedQuota` 只删除了 RPM / TPM 那条查询，额度查询不变，不涉及三库验证。miniredis 不等同于真实 Redis，集群模式与内存占用需在测试环境确认。

---

## 18. 仓库维护类变更

### 18.1 移除 GitHub workflows（`a034e98b3`）

删除 `.github/workflows/` 下全部 6 个文件：`ci.yml`、`docker-build.yml`、`docker-image-branch.yml`、`electron-build.yml`、`release.yml`、`sync-release-to-gitcode.yml`。目的是避免 fork 在 GitHub 上触发上游的构建、发版和镜像同步流水线。`.github/` 下的 issue / PR 模板、`CODE_OF_CONDUCT.md`、`FUNDING.yml`、`SECURITY.md` 保留。

### 18.2 移植提交（`5f22a93b0`）

上游在基线之前重构了四个接缝（seam）：重试判定抽到 `service.DecideRelayRetry`；渠道选择抽到 `service.SelectChannelForRequest`（HTTP distributor 与 Responses WebSocket 共用）；计费准备抽到 `relay.PrepareRequestBilling`；重试设置 UI 迁到 request-policies 页并经 `/api/option/request_policy` 整体校验。该提交把 fork 功能 re-home 到这些接缝上：

- 400 关键词判断移入 `DecideRelayRetry`，5 个重试相关 option key 加入 request-policy 白名单与校验，前端字段从 `SecuritySettings` 移到 `request-policies/`。
- 输入 token 边界、渠道 RPM/TPM、count_tokens 门控在 `SelectChannelForRequest` 内生效（因此 Responses WebSocket 也继承渠道过滤，但见 §4 / §10 中 WS 未覆盖的两点）。
- count_tokens / input_tokens 免费路径进入 `PrepareRequestBilling`。
- 分组模型折扣成为分组设置编辑器的一个标签页。
- 上游测试适配 `GetRandomSatisfiedChannel` 新签名、`channel_extend` 表与新 option。
- 同时把 `channel.ExtendConfig.Validate()` 在 `validateChannel` 中的位置前移；`go.mod` 中 `github.com/fxamacker/cbor/v2` 从 indirect 变为直接依赖。

---

## 19. 与上游同步的注意事项

### 19.1 流程

1. `git fetch https://github.com/QuantumNous/new-api.git main`
2. `git rebase FETCH_HEAD`（fork 提交线性重放）
3. 解决冲突后跑 Go 与前端测试，重点是下面列出的热点文件。
4. 如上游再次重构接缝，参照 `5f22a93b0` 的做法把 fork 逻辑挪到新接缝，而不是在旧位置硬保留。

### 19.2 高频冲突文件

| 文件 | 原因 |
|---|---|
| `service/channel_select.go` | 渠道 RPM/TPM 循环、`ExcludeChannelIds`、指定 / 亲和渠道的 `TakeChannelRateLimit` |
| `model/channel_cache.go` | `channelExtendIDM`、`GetRandomSatisfiedChannel` 新增 `excludeChannelIds` 参数、多个 `HasAny*` 标志 |
| `model/ability.go` | `GetChannel` 新参数、`filterAbilitiesByConstraints` 回填 extend |
| `model/channel_constraint.go` | count_tokens 与 input_tokens 谓词 |
| `service/relay_error.go` | `DecideRelayRetry` 中的关键词分支 |
| `controller/relay.go` | `AddFailedChannel`、count_tokens 分派、耗尽错误 |
| `controller/channel.go` | `extend_config` 读写、`CopyChannel` 状态、`buildFetchModelsHeaders` |
| `relay/helper/price.go` | `HandleGroupRatio` 折扣乘法 |
| `service/http.go` | `ShouldCopyUpstreamHeader` 响应头过滤 |
| `relay/request_billing.go` / `service/text_quota.go` / `service/quota.go` | 免费路径、TPM 记账、realtime 预扣读 PriceData |
| `middleware/distributor.go` | 输入 token 估算、extend setting 注入、count_tokens 不记亲和 |
| `middleware/auth.go` | 多分组 key 归一化 |
| `controller/token.go` | `applyTokenGroupBinding` |
| `model/option.go` / `model/request_policy.go` / `controller/option.go` | 新 option key |
| `router/relay-router.go` | 两个新路由、`ModelRpmTpmRateLimit` 挂载 |
| `relaykit/dto/channel_extend_settings.go` / `channel_settings.go` / `user_settings.go` | DTO |
| `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | 大量新增字段 |
| `web/src/features/channels/lib/channel-form.ts` | extend_config 与 count_tokens 映射 |
| `service/request_policy.go` | `BeginAttempt` 中的实时 RPM 计数、`userID` 字段 |
| `service/text_quota.go` / `service/quota.go` / `service/task_billing.go` / `relay/mjproxy_handler.go` | 消费日志参数改为先赋给 `logParams`，再调用 `RecordSettledUsage` 与 `RecordConsumeLog` |
| `model/log.go` / `controller/log.go` | `SumUsedQuota` 只算额度、`ValidateLogTextPattern`；日志统计接口改读 `service.LogRate` |
| `web/src/features/channels/components/channels-table.tsx` / `web/src/features/users/components/users-table.tsx` | RPM 轮询与 `LiveRpmContext` 包裹 |
| `controller/user.go` | `UpdateUser` 中 `rate_limit` / `model_discount` 写入紧挨上游的审计调用 |
| `middleware/access_token_routes.go` | fork 新增的管理接口若不经 `RequirePermission`，必须在这里声明访问令牌权限，否则上游的覆盖测试失败、令牌被拒 |
| `web/src/features/users/api.ts` | 文件头 import |
| `web/src/i18n/locales/*.json` | 新增文案 |
| `web/src/components/ui/button.tsx` | `data-variant` / `data-size` 两个属性，是 `brand.css` 按钮钩子的契约（§22） |
| `web/src/styles/index.css` | Geist / Geist Mono 字体与 `brand.css` 的 import 顺序（必须在 `theme-presets.css` 之后） |
| `web/package.json` / `web/bun.lock` | `@fontsource-variable/geist`、`@fontsource-variable/geist-mono` 依赖 |
| `web/src/components/config-drawer.tsx` | 默认预设色块改为极光渐变 |
| `web/src/features/auth/auth-layout.tsx` | 认证页外壳整体重排：`data-slot='auth-card'` 玻璃卡片（`max-w-[480px]`）、品牌面板、lg 起背景层的边缘渐显与 mesh 峰值位置 |
| `web/src/features/auth/components/oauth-providers.tsx` / `oauth-callback-screen.tsx` | "Or continue with"分隔线改为 flex 行，提供方按钮去掉 `h-11`；回调页图标底块与加载图标颜色 |
| `web/src/components/layout/components/public-header.tsx` / `footer.tsx` | 页头玻璃胶囊（本地 `--glass-bg` 覆盖）、激活链接下划线、登录按钮、移动菜单背景层与 CTA；页脚默认分支的渐变分隔线、文字 class 与列表 key |
| `web/src/features/home/index.tsx`（仅默认首页分支的区块顺序）/ `components/sections/{hero,stats,features,how-it-works,cta}.tsx` / `hero-terminal-demo.tsx` | 首页整体重排为与上游不同的版式（居中 hero + 光地平线 + 统计读数 + 应用条、步骤与终端并排的产品演示、左侧粘性标题的功能区、全宽收尾 CTA）；终端是夜岛（`class="dark"` + `data-brand-island`），标签按钮 `type='button'`、代码行 key 改为内容 |
| `web/src/features/pricing/index.tsx` / `components/{search-bar,pricing-toolbar,pricing-sidebar,loading-skeleton}.tsx` | 定价页头部与 `data-brand-page='pricing'`；搜索框尺寸与内边距；工具栏框体、切换按钮 `h-8`、移动筛选抽屉的 `data-brand-page` 与侧栏 `bg-none`；侧栏框体；加载骨架与新头部同尺寸 |
| `web/src/features/pricing/__tests__/pricing-controls.test.tsx` | 上游测试文件，新增 2 个用例（切换按钮 `h-8`、移动筛选抽屉的品牌作用域） |
| `web/src/features/rankings/index.tsx` | 头部背景层；三态渲染由嵌套三元改为 `if / else` 赋给 `rankingsContent`（结构改写，合并时注意上游对这段的修改）；骨架与错误框圆角 |
| `web/src/features/rankings/components/{rankings-hero,models-section,market-share-section,pulse-section,model-leaderboard,growth-text}.tsx` | 标题渐变字与标签下划线、区块改用 `brand-surface`、`text-muted-foreground/80` 去掉透明度 |
| `web/src/components/layout/components/section-page-layout.tsx` | 控制台页面的 `data-slot='section-page-*'` 钩子与标题 class |
| `web/src/components/search.tsx` | 顶栏搜索按钮 class |
| `web/src/features/dashboard/components/overview/{overview-dashboard,summary-cards,performance-health-panel,api-info-item}.tsx` / `components/ui/{panel-wrapper,stat-card}.tsx` | 概览面板改用 `brand-surface`、设置引导背景层、余额面板深色渐变、API 地址行截断规则、说明文字去掉透明度 |
| `web/src/features/dashboard/components/{models/{log-stat-cards,performance-overview,consumption-distribution-chart,model-charts},flow/flow-charts,users/user-charts}.tsx` | 数据看板各标签页外框改为 `brand-surface rounded-2xl` |
| `web/src/features/errors/{not-found-error,unauthorized-error,forbidden,maintenance-error,general-error}.tsx` | 错误页背景层；状态码改为渐变字，外包 `relative isolate` 的 div 并加 `BrandGlow` |

### 19.3 上游签名变化的传染点

- `model.GetRandomSatisfiedChannel(group, model, retry, filters, excludeChannelIds)` 与 `model.GetChannel(..., excludeChannelIds)`：上游新增调用点需补传 `nil`。
- `relaykit/` 必须独立可编译：改动 `relaykit/dto/*` 后运行 `cd relaykit && GOWORK=off go build ./...`。
- 访问令牌按接口授权（上游 2026-09 起）：新增管理接口时，经 `channelPermissionRoutes` 等 `handlePermissionRoute` 注册的会自动声明；其余（如 `adminRoute.GET(...)`）要在 `accessTokenRouteRules` 里补规则，`router/access_token_scope_test.go` 会检查。

### 19.4 同步记录

- **2026-10-06，rebase 到 `b48b74ab7`**（上游 21 个提交：可授权访问令牌、管理员操作用户需二次验证、任务插件改用 moejs、Responses 自定义工具修复等）。
  - 冲突：`controller/user.go`（保留 fork 的 `rate_limit` / `model_discount` 写入，审计改用上游的 `auditParams`）；`web/src/features/users/api.ts`（两侧 import 并存）。
  - 合并后补充：`GET /api/user/rpm`、`GET /api/user/:id/rpm/channels` 声明 `user:read`；`LiveRpmCell` 在统计数据缺少 `items` 时显示"-"而不是让整张渠道表崩溃（上游新增的渠道表刷新测试暴露）；修正 fork 文件里遗留的 2 个 lint 错误与 8 处格式漂移。
  - 验证：`go vet ./...` 与 `go test ./...` 全部通过，relaykit 独立构建与测试通过；前端类型检查、182 个测试文件 2237 个用例全部通过；本地端到端 38 项（覆盖 §2 到 §17 与上游访问令牌在 fork 接口上的授权）全部通过；Chrome 检查渠道表、渠道编辑抽屉各标签页、用户与令牌页、日志统计条、请求策略、模型限流、分组模型折扣与访问令牌页，无控制台错误。
  - 部署注意：上游新表 `user_access_tokens` 由自动迁移创建；旧的管理令牌升级后只能再用 30 天，依赖它的脚本（如 channel-inspector）需改用带相应权限的新令牌；管理员编辑用户时若提交了 `admin_permissions` 或密码，需要先做二次验证。

---

## 20. 已知限制与测试缺口汇总

### 功能限制

- **Responses WebSocket 路径**：不参与"避开已失败渠道"（§4），也不做输入 token 边界过滤（§10）。
- **RPM/TPM 与排除集合耦合**：渠道限流饱和会写入 `ExcludeChannelIds`，导致即使 §4 开关关闭，候选耗尽时也返回 §4 配置的状态码（默认 429）。
- **限流计数语义**：RPM 在请求前 +1 且失败不退还；TPM 事后记账，窗口最后一个请求可能超冲。用户级 RPM/TPM 的超限文案为硬编码中文。
- **输入 token 估算**：启发式、不计 tools / 图片 / 结构开销；非 JSON 或非白名单路径（embeddings、audio、images、任务）完全绕过边界；估算值不复用于计费。
- **count_tokens**：严格限定渠道类型 14 与 1；仍会占用渠道 RPM/TPM 槽位；上游非 200 可能触发渠道自动禁用；Claude 校验沿用 `/v1/messages` 的 `max_tokens` 上界。
- **模型折扣**：模型名匹配仅精确 + `*`，无前缀 / glob；以客户端请求模型名为 key。
- **渠道超时**：`streaming_timeout` 对 AWS/Bedrock 事件流无效；`DoWssRequest` 不受渠道 `relay_timeout` 影响。
- **多分组 key**：用户级 RPM/TPM 限流读到的分组为 `"auto"`，而非主分组。
- **新建默认禁用**：只影响前端表单默认值，直接调 API 仍可创建启用状态的渠道；复制则由后端强制禁用。
- **响应头过滤**：只作用于会复制上游头的路径（非流式透传、Codex 流式两个头、audio / minimax tts）；流式 SSE 其他头本来不复制，白名单也无法让它们回传；`Content-Type`、`Content-Encoding` 不可被黑名单丢弃。MySQL / PostgreSQL 上的新列迁移尚未实机验证。
- **渠道额度上限**：判定在结算之后，禁用前已在飞的请求仍会结算，`BATCH_UPDATE_ENABLED` 下还会多出一个刷盘间隔（默认 5 秒）的流量；多副本各自以数据库值判定，其他副本的内存缓存最长 `SYNC_FREQUENCY`（默认 60 秒）后感知禁用；预扣费不参与判定。折算成本 = 站内计费额度 × 成本倍率，是估算值而非上游账单。MySQL / PostgreSQL 上的新列迁移尚未实机验证。
- **渠道可用时段**：判定基于各副本本机时钟；窗口边界处进行中的流式响应与 WebSocket 会话不会被中断，只影响新请求；会话亲和在时段外会被放弃，该会话可能换渠道；全部渠道时段外时客户端只看到通用 503；时区运行时加载失败按全天可用处理；粒度只到周几 + 分钟，没有日期范围。MySQL / PostgreSQL 上的新列迁移尚未实机验证。
- **实时 RPM / TPM 统计**：数字比当前时刻晚 4 到 14 秒；token 在请求结算时一次性计入（长流式请求的 token 全部落在结束那一分钟）；Redis 故障期间少计，实例崩溃会丢失最近 2 秒的计数；各实例按本机时钟分桶，时钟偏差超过 10 秒会明显失真；没有 Redis 的多实例部署只能看到本实例；列表只能在当前页查看，不能按 RPM / TPM 给全部渠道或用户排序；列表 RPM 按分发次数、日志页 RPM 按结算请求，两者口径不同。使用日志页不指定用户、又用多个条件或模糊匹配时，要读窗口内所有活跃用户的明细，流量很大时较慢；精确匹配区分大小写，模糊匹配不区分。
- **品牌主题层**：只有默认预设使用完整的极光配色，其他预设保留原配色、只叠加结构效果；图表（VChart）配色未替换；控制台页面内部不用真玻璃（`backdrop-filter`），`AnimatedOutlet` 的 `filter` 终态也会让它失效；夜岛在命名预设下使用 `theme.css` 的经典深色 token。

### 测试缺口

- `DecideRelayRetry` 的 `retry_keyword_matched` 分支。
- `controller/relay.go` `getChannel` 候选耗尽返回可配置状态码的路径。
- `doRequest` 的渠道级 deadline、`cancelOnCloseBody`、`StreamScannerHandler` 的超时覆盖。
- `countTokensPassthrough`、`PostCountTokensLog`、`PrepareRequestBilling` 的跳过分支。
- 渠道 RPM/TPM 与输入 token 边界的前端字段测试；distributor 端到端。
- 控制器层"启用已耗尽渠道被拒绝 / 跳过"（单个 / 批量 / 标签）只有本地端到端验证，没有 Go 单测；前端无渠道成本倍率 / 额度上限字段的组件测试。
- 渠道可用时段：Responses WebSocket 路径只有共用 `SelectChannelForRequest` 的单元覆盖，没有 WS 端到端；前端无时段编辑器的组件测试。
- 实时 RPM / TPM 统计：Midjourney、异步任务提交、实时语音、音频与 Responses WebSocket 路径只靠共用计数点的单元覆盖，没有这几条路径的端到端；真实 Redis（含集群模式）未验证。
- 品牌主题层：vitest 不处理 CSS，测试只覆盖 DOM 契约（`aria-hidden`、`data-*` 钩子、DOM 顺序、链接与按钮）和关键布局 class（标题字号上限、换行与截断 class、控件高度、骨架尺寸），不验证实际渲染的尺寸与颜色；各预设、深浅色、移动端、减少动态效果 / 透明度、高对比度、强制颜色、RTL 以及 Safari / Firefox 下的视觉效果只能人工检查，没有视觉回归测试。玻璃条折射在 Chromium 中是否还有横向接缝尚未复查。

---

## 21. 附录

### 21.1 新增 option key 一览

| key | 类型 | 默认 | 所属 |
|---|---|---|---|
| `AutomaticRetryKeywordsEnabled` | bool | `false` | §3 |
| `AutomaticRetryKeywords` | 换行分隔字符串 | `""` | §3 |
| `RetryAvoidFailedChannelsEnabled` | bool | `false` | §4 |
| `RetryAvoidFailedChannelsStatusCode` | int 100-599 | `429` | §4 |
| `RetryAvoidFailedChannelsErrorMessage` | 字符串（`{model}`） | 见 §4 | §4 |
| `ModelRateLimitEnabled` | bool | `false` | §7 |
| `ModelRateLimitRules` | JSON 字符串 | `""` | §7 |
| `GroupModelDiscount` | JSON 字符串 | `{}` | §9 |

以上 8 个 key 都通过 `GET/PUT /api/option/` 读写；前 5 个另可经 `GET/PATCH /api/option/request_policy` 读写。

### 21.2 新增 HTTP 端点

| 端点 | 说明 |
|---|---|
| `POST /v1/messages/count_tokens` | Anthropic token 计数透传，免费 |
| `POST /v1/responses/input_tokens` | OpenAI Responses 输入 token 计数透传，免费 |
| `GET /api/channel/rpm`、`GET /api/channel/:id/rpm/users` | 渠道实时 RPM / TPM 与按用户明细（§17） |
| `GET /api/user/rpm`、`GET /api/user/:id/rpm/channels` | 用户实时 RPM / TPM 与按渠道明细（§17） |

### 21.3 管理 API 新增字段

| 资源 | 字段 | 说明 |
|---|---|---|
| Channel | `extend_config.{relay_timeout, streaming_timeout, min_input_tokens, max_input_tokens, rpm_limit, tpm_limit, claude_auth_mode, response_header_mode, response_headers, cost_ratio, quota_limit, schedule}` | §2.1，PUT 缺键不改、`null` 清除；列表 / 搜索接口每行也附带非零值（§15） |
| Channel | 指定渠道在可用时段外 | §16，返回 403 `distributor.channel_disabled`；`origin_task` 固定路由豁免 |
| Channel | 启用接口（`UpdateChannelStatus` / `BatchUpdateChannelStatus` / `EnableTagChannels`） | §15，已耗尽渠道：单个返回 `channel.quota_limit_reached`，批量 / 标签跳过并以 `channel.quota_limit_skipped` 作为 `message` |
| Channel | `settings.count_tokens_enabled` | §6 |
| User | `rate_limit` | §7，`nil` 不改、`{}` 清除 |
| User | `model_discount` | §9，同上 |
| Token | `auto_groups`（普通分组下） | §13，作为备用分组 |
| 日志统计 | `/api/log/stat` 与 `/api/log/self/stat` 新增 `tpm_input`、`tpm_cache_read`、`tpm_cache_write`、`tpm_output`、`rate_source`、`rate_window_start`、`rate_window_end`，参数 `rate_only` | §17，`rpm` / `tpm` 改为实时统计 |

### 21.4 Redis key 一览

| key | 用途 |
|---|---|
| `rateLimit:v2:mrpm:<userId>:<group>:<model>` | 用户 × 模型 RPM 固定窗口 |
| `rateLimit:v2:mtpm:<userId>:<group>:<model>:<unixMinute>` | 用户 × 模型 TPM 分钟桶 |
| `rateLimit:v2:crpm:<channelId>` | 渠道 RPM 固定窗口 |
| `rateLimit:v2:ctpm:<channelId>:<unixMinute>` | 渠道 TPM 分钟桶 |
| `user_extend_rl:<userId>` | 用户限流覆盖缓存 |
| `user_extend_md:<userId>` | 用户模型折扣缓存 |
| `rpmstat:ch:<bucket>` / `rpmstat:u:<bucket>` | 实时 RPM：各渠道分发次数 / 各用户请求数，10 秒一桶，120 秒过期（§17） |
| `rpmstat:cu:<channelId>:<bucket>` / `rpmstat:uc:<userId>:<bucket>` | 实时 RPM 明细：渠道下各用户 / 用户在各渠道 |
| `rpmstat:s:{all,ch,u,m,g,t}:<bucket>` / `rpmstat:s:cu:<channelId>:<bucket>` | 结算请求数与四项 token 的汇总（§17） |
| `rpmstat:s:d:<userId>:<bucket>` / `rpmstat:s:users:<bucket>` | 每用户的结算明细（令牌、模型、渠道、分组）/ 用户 ID → 用户名 |

`rpmstat:*` 在配置了 `RPM_STATS_REDIS_CONN_STRING` 时位于专用统计 Redis，否则位于主 Redis。

### 21.5 消费日志 `other` 新增键

| 键 | 含义 |
|---|---|
| `group_model_discount` | 分组模型折扣（≠ 1 时写入） |
| `user_model_discount` | 用户模型折扣（≠ 1 时写入） |
| `endpoint` | `count_tokens` / `input_tokens`，标记零额计数日志 |

### 21.6 fork 提交列表（按时间）

| 提交 | 日期 | 标题 |
|---|---|---|
| `20d115227` | 2026-08-15 | add 400 retry option |
| `274d26fe0` | 2026-08-15 | retry without tried channels |
| `c7ed417aa` | 2026-08-16 | add channel relay timeout |
| `746188d58` | 2026-08-20 | add anthropic token count |
| `3bde0f328` | 2026-08-29 | add rate limited control |
| `55673f314` | 2026-08-30 | add rate limit control gui |
| `a034e98b3` | 2026-08-30 | chore: remove github workflows on fork |
| `6104962f2` | 2026-08-30 | add discount control |
| `b7c5bbd2e` | 2026-08-30 | fix rpm limit control gui |
| `5b114b6cf` | 2026-08-30 | fix rpm discount gui |
| `e65956272` | 2026-08-31 | add min input channel config |
| `0fe0e61f5` | 2026-08-31 | add max input channel config |
| `09ac2e64d` | 2026-08-31 | add channel tpm rpm limit |
| `ce0dca5c8` | 2026-09-12 | add openai responses input_tokens count tokens endpoint |
| `455ec8282` | 2026-09-24 | add anthropic channel auth mode |
| `5f22a93b0` | 2026-09-24 | port fork features onto upstream request-policy and channel-selection refactors |
| `168dbbafc` | 2026-09-24 | feat(channels): create and copy channels as manually disabled |
| `f69683a02` | 2026-09-26 | feat(tokens): bind a key to a primary group plus ordered fallback groups |
| `b0a715551` | 2026-09-27 | feat(channels): filter upstream response headers per channel (blacklist / whitelist) |

---

## 22. 品牌主题层（Aurora Glass）

### 动机

上游前端的外观来自 `theme.css` 的默认配色和 `theme-presets.css` 的命名预设，没有自己的品牌视觉。本功能为 fork 建立一套品牌主题"极光琉璃（Aurora Glass）"：弥散网格渐变（mesh）、带涟漪的玻璃、光晕，用在首页、认证页（登录、注册、忘记密码、重置密码、二次验证、OAuth 回调）、模型广场（定价页）、排行榜、控制台和错误页。

**只改样式与布局**：不新增或修改任何路由、条件、文案、i18n key、事件处理、`role` / `aria-*` 与数据请求，也不新增 i18n key；新增的只有样式钩子属性（`data-slot`、`data-brand-*`、`data-variant` / `data-size`）、`aria-hidden` 的装饰节点、布局用的包裹元素，以及认证页 lg 起显示的品牌面板（根节点是 `aria-hidden` 的 `<div>`，不是 `<aside>`，不增加地标，也没有可聚焦元素，读屏内容与改版前一致；文案复用首页既有 key）；首页的 `landing-animate-*` 与 `opacity-0` 成对保留。管理后台的配置页面保持原布局，只随 token 换色。为通过 oxlint 做的少量非样式改动见"改动的既有文件"末条。

### 设计约束

- **光在后，玻璃在上，数据区保持平面。** 品牌只体现在颜色、光与质感上，不写死任何文字标识；管理员配置的 Logo 位于涟漪中心。
- 真玻璃（`backdrop-filter`）只用于：公共页头滚动后的胶囊、移动端菜单遮罩、认证卡片、认证页 hub 圆盘（96px）、popover / dropdown / select 弹层、CTA 区的折射玻璃条。表格、卡片网格、图表、对话框、聊天流、滚动容器里的吸顶面板以及控制台页面内部一律不用；这些位置用 `brand-surface`（不透明卡片 + 光泽 + 发丝线）。控制台页面渲染在 `AnimatedOutlet`（`components/page-transition.tsx`，由 `authenticated-layout.tsx` 挂载）内，它的动画 `MOTION_VARIANTS.pageEnter` 终态是 `filter: blur(0px)`；非 `none` 的 filter 让该节点成为 backdrop root，页面内的真玻璃只能模糊页面自身的内容（减少动态效果时 `AnimatedOutlet` 渲染普通 div，没有 filter）。`PageTransition` 用同一个 `pageEnter`，只包裹公共的定价页与排行榜内容。
- 每个视口最多一个漂移 mesh、一条边框光束（`brand-beam`）；每页最多一个带动画的渐变文字（`brand-text-aurora--flow`）。
- 文字区下方 mesh 的叠加 alpha：浅色不超过 16%，深色不超过 30%，以保证正文对比度不低于 4.5:1；mesh 峰值放在终端、hub、角落等非文字对象后面，首页 hero 的文案列另有一层 veil 提亮。`brand-text-aurora` 只用于 24px 以上的展示文字。
- 中日韩换行：`brand-display` 行高 1.1、`text-wrap: balance`，并设 `word-break: keep-all` 与 `overflow-wrap: break-word`，只在空格与标点处换行，过长的无空格串仍会折行。首页 hero 副标题用 `break-keep wrap-break-word`，认证面板说明用 `break-keep wrap-anywhere`，首页步骤与小功能说明用 `text-pretty break-keep wrap-break-word`，定价页说明用 `text-balance`，认证卡片内的 `<p>` 用 `text-pretty`，避免单字成行。

### 新增文件

| 文件 | 内容 |
|---|---|
| `web/src/styles/brand.css` | 全部品牌 token、效果类（`brand-*`）、共享组件的 `data-slot` 钩子、无障碍与环境降级 |
| `web/src/components/brand/brand-backdrop.tsx` | `BrandBackdrop`：装饰性光层（mesh、漂移 mesh、veil、网格、噪点，可选静态环 `rings='static'`、声呐涟漪 `rings='sonar'`、两侧折射玻璃条 `flutes`）。8 个 variant：`hero`、`hero-center`（居中首页 hero）、`band`、`pricing`、`auth`、`auth-panel`、`panel`、`dawn`。根节点 `aria-hidden`，不含可聚焦元素；父元素必须是层叠上下文（`relative isolate` 或带 z-index 的定位元素） |
| `web/src/components/brand/brand-glow.tsx` | `BrandGlow`：焦点对象背后的光晕，用 `[--glow-inset:…]` 调整大小 |
| `web/src/components/brand/brand-pulse.tsx` | `BrandPulse`：6px 呼吸点，总是与可见文字搭配 |
| `web/src/components/brand/index.ts` | 统一导出 |
| `web/src/features/auth/components/auth-brand-panel.tsx` | 认证页 lg 及以上的品牌面板。根节点是 `aria-hidden='true'`、`data-slot='auth-brand-panel'` 的 `<div>`（纯装饰，不产生 complementary 地标，读屏不会在表单之后朗读首页标语）。hub 圆盘与协议轨道（Chat / Responses / Claude / Gemini）在 `aria-hidden` 的 `data-slot='auth-brand-hub'` 区域内；该区域在文档流中、位于文案之前，是尺寸容器（`@container-size`，`flex-1`），轨道（`data-slot='auth-brand-orbit'`）尺寸为 `min(30vw, 420px, 100cqh - 56px)`，容器高度不超过 255px 时隐藏，所以任何语言、任何视口高度下都不会压到文案；声呐涟漪用 `--sonar-y`（lg `calc(50% - 60px)`、xl `calc(50% - 108px)`）对准 hub 中心。文案（`data-slot='auth-brand-copy'`，`max-w-xl`，说明 `max-w-md`）复用首页既有 key，只用 `<p>`，不增加标题 |

玻璃、渐变文字、发丝线等做成 class 而不是包裹组件：它们装饰的是既有元素（Button、卡片、标题、页头 `<nav>`），包裹组件会在受测试约束的 DOM 之间插入节点（定价卡片网格父节点、标题列表、label / value 兄弟节点）。常用 class：`brand-glass`、`brand-surface`、`brand-bezel`、`brand-terminal`、`brand-hairline`、`brand-beam`、`brand-cta`、`brand-sheen`、`brand-display`、`brand-text-aurora`、`brand-eyebrow`、`brand-chip`、`brand-icon-tile`、`brand-rule`、`brand-connector`、`brand-dots`、`brand-orbit`；Tailwind 颜色 `border-hairline`、`border-hairline-strong`、`bg-glass`、`bg-glass-strong`、`bg-brand-a/…` 与字体 `font-display` 由 `brand.css` 的 `@theme` 注册。

### 改动的既有文件

基础项写完整路径，其余路径相对 `web/src/`。

- **基础**：`web/src/styles/index.css`（3 行 import）、`web/src/components/ui/button.tsx`（`data-variant` / `data-size`）、`web/src/components/config-drawer.tsx`（默认预设色块）、`web/package.json` / `web/bun.lock`（字体依赖）。
- **公共页**：
  - `components/layout/components/public-header.tsx`：滚动后的胶囊改为 `brand-glass brand-hairline`，并在本地把 `--glass-bg` 指向 `--glass-bg-strong`（浅色 88%、深色 84% 卡片色；`simple-large` 与高对比度下仍为实色）；激活链接加极光下划线；桌面登录按钮与移动菜单 CTA 用 `brand-cta`；移动菜单遮罩加 `<BrandBackdrop variant='dawn' />`。滚动判定、链接、菜单开关与滚动锁定不变。
  - `components/layout/components/footer.tsx`：只改默认分支（渐变分隔线、底部光晕、文字去掉透明度）；`footerHtml` 分支、`LegalLinks`、`ProjectAttribution` 不变。
  - 首页版式（刻意与上游不同）：`features/home/index.tsx` 默认分支的区块顺序改为 Hero → HowItWorks → Features → CTA → Footer（统计在 Hero 内渲染），自定义首页的 iframe / HTML / Markdown 分支与上游一致、不加背景。`hero.tsx` 为居中标题栈（`<BrandBackdrop variant='hero-center' rings='sonar' flutes />`，标题 `text-[clamp(2.5rem,6.2vw,4.75rem)]`），下方是 `.brand-horizon` 光地平线，统计读数（`stats.tsx`，无卡片、细线分隔）与常用应用条放在地平线上；`how-it-works.tsx` 左侧为竖向步骤轨道（`.brand-connector--y` 流光），右侧为 lg 起粘性的 API 终端（原 hero 右栏的终端移到这里，内容与交互不变）；`features.tsx` 左侧粘性标题与 4 项小功能列表，右侧 2×2 等宽功能卡（去掉 bento 跨列）；`cta.tsx` 改为全宽区块，背景上下边缘渐隐。其余：副标题与说明 `break-keep wrap-break-word text-pretty`；CTA 与 hero 按钮行 `flex-wrap`；终端页脚 `flex-wrap`，"stream · sse"不换行并靠右；步骤编号徽标 `bg-primary` 加光晕；功能卡片编号在 DOM 中仍位于标题之前（视觉上靠右）。
- **认证**：
  - `features/auth/auth-layout.tsx` 是全部认证页的唯一入口，DOM 顺序为"首页链接 → 表单卡片 → 品牌面板"（栅格把面板放在视觉左侧；面板 `aria-hidden` 且无可聚焦元素，键盘与读屏只经过表单）；`h-svh` 改为 `min-h-svh`，卡片不加 `overflow-hidden`，375px 下 Turnstile 不被裁切。卡片 `max-w-[480px]`，在 `sm:p-8` 下恢复原 `sm:w-[480px]` 的 416px 文字宽度；卡片内 `<p>` 用零特异性的 `[:where(&_p)]:text-pretty`，以后 `<p>` 上的 `truncate` / `whitespace-nowrap` 仍能覆盖它。lg 起页面背景层只铺右半页（`lg:start-1/2`，起点在品牌面板下面），用 80px 渐显遮罩消除中线接缝，并把 mesh 峰值移到卡片右上角后面（`--mesh-p1` / `--mesh-s1`，mesh 不透明度 100%），让卡片的玻璃有颜色可模糊。
  - 表单文件（登录、注册、忘记 / 重置密码、OTP、`SecureVerificationDialog`、`PasskeyDomainSelector`）均未改，卡片内 44px 输入框与按钮（含重置密码页的 outline 图标复制按钮）和极光提交按钮来自 `brand.css` 的 `[data-slot='auth-card']` 作用域规则；各页标题 h2 在卡片内统一左对齐、使用展示字体，也由该作用域的非分层规则覆盖页面文件里移动端的 `text-center`（页面文件保持上游原样）。
  - `components/oauth-providers.tsx` 只改"Or continue with"分隔线（两段 `aria-hidden` 线 + 文字的 flex 行），并去掉提供方按钮上原有的 `h-11`（保持默认尺寸，由卡片的 44px 控件规则统一高度，任何改 `--spacing` 的预设下都与输入框同高），`components/oauth-callback-screen.tsx` 只改图标底块（`brand-icon-tile`）与加载图标颜色。认证逻辑、顺序、门控与 autocomplete 均未变动。
- **定价与排行榜**：
  - `features/pricing/index.tsx`：外层 `relative isolate` 加 `data-brand-page='pricing'`、`<BrandBackdrop variant='pricing' rings='static' />`；标题 `brand-display brand-text-aurora mx-auto w-fit`（渐变按文字宽度铺开）；模型计数改为带 `BrandPulse` 的 chip；说明 `text-balance`。
  - `components/search-bar.tsx`：48px 高、`rounded-2xl`、玻璃底色；右内边距 `pr-11 sm:pr-16`（⌘K 提示只在 sm 起显示，390px 下占位文字不被截断）。
  - `components/pricing-toolbar.tsx`：框体 `brand-surface rounded-2xl`；4 个 `ToggleGroupItem` 加 `h-8`，与排序按钮同高；"/ 总数"去掉透明度；移动筛选抽屉的 `SheetContent` 加 `data-brand-page='pricing'`（抽屉 portal 到 `body`，需要自带作用域），传给侧栏的 class 加 `bg-none` 去掉光泽。
  - `components/pricing-sidebar.tsx`：框体一个 class 字符串。
  - `components/loading-skeleton.tsx`：头部骨架与新头部同尺寸（标题字号与行高、计数 chip、说明 sm 以下 2 行 / sm 起 1 行、48px 搜索框），侧栏与工具栏占位改为 `brand-surface rounded-2xl`，工具栏占位 `h-8`；按视图模式区分的结构不变。
  - `features/rankings/index.tsx`：背景层 `<BrandBackdrop variant='pricing' />`；加载 / 错误 / 数据三态原为嵌套三元表达式，改为 `if / else` 先赋给 `rankingsContent` 再渲染，分支条件与内容不变；加载骨架与错误框改为 `rounded-2xl`。
  - `features/rankings/components/rankings-hero.tsx`：标题 `brand-display brand-text-aurora w-fit`，激活标签的下划线改为极光渐变加光晕（`role='tab'`、`aria-selected`、`onClick` 不变）。`{models-section,market-share-section,pulse-section}.tsx` 的区块改为 `brand-surface rounded-2xl`。这三个文件与 `{model-leaderboard,growth-text}.tsx` 中共 17 处 `text-muted-foreground/80`（加上 hero 副标题共 18 处）改为不透明的 `text-muted-foreground`。
  - 模型卡片、详情、表格与列定义未改，靠 token 和 `[data-brand-page='pricing']` 作用域钩子着色。计费展示逻辑未动。
- **控制台**：
  - `components/layout/components/section-page-layout.tsx`：`data-slot='section-page-header|title|actions|content|footer'` 钩子；标题 `font-display font-semibold`，前面的渐变短竖线由 CSS 画在 `::before` 上并绝对定位（`ps-[calc(0.5rem+3px)]` 预留位置），块级或 inline-flex 的标题子元素仍能在 `truncate` 的 h2 内截断；页脚分隔线 `border-hairline`。
  - `components/search.tsx`：顶栏搜索按钮改为玻璃色胶囊，`dark:border-hairline-strong` 用来压过 outline 变体的 `dark:border-input`。
  - `features/dashboard/components/overview/`：`overview-dashboard.tsx`（设置引导背景改为 `<BrandBackdrop variant='panel' />`，展开时带静态环；三个 `CardStaggerItem` 改用 `brand-surface`）、`summary-cards.tsx`（外框 `brand-surface`；余额面板浅色渐变的固定色值改为 `var(--brand-c)`，另加以卡片色为底的深色渐变 `dark:bg-[…]`；余额数字 `font-display tabular-nums`）、`performance-health-panel.tsx`（外框）、`api-info-item.tsx`（路由名 `max-w-full shrink-0 break-keep wrap-break-word`，不被说明挤压，过长时按词换行、始终完整可读；说明与 URL 用 `min-w-0 truncate` 先截断；两者去掉透明度）。
  - `features/dashboard/components/ui/panel-wrapper.tsx`（外框）、`ui/stat-card.tsx`（两处说明去掉透明度）。
  - 数据看板各标签页：`features/dashboard/components/models/{log-stat-cards,performance-overview,consumption-distribution-chart,model-charts}.tsx`、`flow/flow-charts.tsx`、`users/user-charts.tsx` 的外框由 `overflow-hidden rounded-lg border` 改为 `brand-surface overflow-hidden rounded-2xl`（`performance-overview` 的空状态条同样处理），`log-stat-cards` 两处说明去掉透明度。VChart 规格本身是透明背景，未改。
  - 其余由 `brand.css` 的全局钩子完成，不改组件文件：侧栏画布光晕只在 inset 布局生效（`[data-slot='sidebar-wrapper']:has([data-slot='sidebar'][data-variant='inset'])`）；md 及以上内容面板有发丝线、顶部高光、阴影、1px 极光顶边（`--panel-rim`）和顶部 dawn 渐变；768px 以下 dawn 在外壳与内容区上连续绘制（内容区按 `--app-header-height` 偏移）；移动端导航抽屉（`data-mobile='true'`）带画布光晕并保持不透明；激活项光条与页面标题短竖线从 `--tick-from` 渐变到 `--brand-a`；卡片光泽、表头着色、对话框顶部光晕、弹层玻璃。
- **错误页**：`features/errors/{not-found-error,unauthorized-error,forbidden,maintenance-error,general-error}.tsx` 根节点加 `relative isolate` 与 `<BrandBackdrop variant='auth' />`；状态码 `<h1>` 改为 `brand-display brand-text-aurora`，外包 `<div className='relative isolate'>`，以 `<BrandGlow className='[--glow-inset:-35%]' />` 作为第一个子元素。`general-error` 在 `minimal` 模式下不渲染背景、光晕、`relative isolate` 与状态码。按钮与跳转（返回上一页、回到首页、报告问题）不变。
- **为通过 oxlint 的非样式改动**（改动文件按 `.oxlintrc.json` 检查时暴露的既有问题，渲染结果不变）：`footer.tsx` 与 `hero-terminal-demo.tsx` 的列表 key 由下标改为内容（`column.title`、`link.href`、代码行文本、匹配位置；`react/no-array-index-key`）；终端标签按钮加 `type='button'`（`react/button-has-type`，按钮不在表单内）；`rankings/index.tsx` 的嵌套三元改为 `if / else`（`no-nested-ternary`）；`panel-wrapper.tsx` 改为 `import type { ReactNode }`（`typescript/consistent-type-imports`）。

### 作用范围与预设

| 预设 | 外观 |
|---|---|
| 默认（`body` 上无 `data-theme-preset` 属性） | 完整极光配色 + 全部效果，`--radius` 0.75rem，标题与等宽字体为 Geist / Geist Mono；现有默认用户直接看到新外观 |
| 7 个配色预设（forest-whisper、lake-view、lavender-dream、ocean-breeze、rose-garden、sunset-glow、underground） | 配色保持原样；玻璃、发丝线、mesh、涟漪、光晕、按钮质感用预设自己的 `--primary` / `--chart-3` / `--chart-2` 着色；CTA 退化为纯色 primary，渐变文字退化为纯色；呼吸点改用 `--primary`（`--chart-2` 在部分预设的 primary 色 chip 上看不清）；侧栏激活项与页面标题的短竖线为单一色相 |
| `anthropic` | 效果强度 50%，光晕颜色减半（`--glow-color` 25%），控制台无画布光晕、dawn 渐变与面板顶边 |
| `simple-large` | 关闭全部氛围效果：背景层与渐变发丝线隐藏，光晕透明，玻璃（含弹层）改为实色，卡片 hover 沿用 `index.css` 原样（无障碍预设） |

用户轴（圆角、字体、缩放、内容布局）照常覆盖；serif 字体轴同时把 `--font-display` 切为衬线。

### 层叠规则（修改 `brand.css` 前必读）

- `index.css` 中的导入顺序为 `theme.css` → `theme-presets.css` → `brand.css`。`theme.css` 在 `html` 上声明 token（`:root`、`.dark`，特异性均为 0,1,0）；预设在 `body` 上声明（`[data-theme-preset='x']` 0,1,0，`.dark [data-theme-preset='x']` 0,2,0）；用户轴在 `body` 上声明（`[data-theme-font|radius|scale]` 0,1,0）。
- `brand.css` 的 token 分 6 块：

| 块 | 选择器 | 特异性 | 内容 |
|---|---|---|---|
| B1 | `:where(body, body .dark)` | 0 | 所有预设的结构 token（浅色值，夜岛也匹配），含控制台的 `--console-canvas`、`--panel-dawn-image`、`--panel-rim` 与短竖线起始色 `--tick-from` |
| B2 | `:where(html.dark body, body .dark)` | 0 | 结构 token 的深色值（夜岛也匹配），按顺序胜过 B1 |
| B3 | `:where(body:not([data-theme-preset]), body[data-theme-preset='default'])` | 0 | 极光浅色核心：全部 `theme.css` token、品牌色、字体、`--radius`；`--tick-from: var(--brand-c)` 在 `body` 上求值，深色时取 B4 的青色（B2 不声明 `--tick-from`） |
| B4 | `:where(html.dark) :is(<默认 body>)`、`:is(<默认 body>) .dark` | 0,1,1 / 0,2,1 | 极光深色核心 + 默认预设下的夜岛 |
| B5 | `[data-brand-island]` | 0,1,0 | 夜岛文字颜色与 `color-scheme: dark` |
| B6 | `[data-theme-preset='anthropic' \| 'simple-large']` | 0,1,0 | 各预设的效果强度（`--fx`）、`--glow-color`，以及控制台画布 / dawn / 顶边的开关 |

- 不变式（也写在 `brand.css` 文件头）：
  1. B3 可以设 `--radius`、`--font-display`、`--font-mono`，用户轴（0,1,0）仍然胜出。
  2. **B4 不得设** `--radius`、`--font-*`、`--text-*`、`--spacing`、`--max-content-width`，否则会压过用户轴。
  3. **B2 与 B3 不得出现同一属性**：B3 在后、特异性相同，会把浅色值漏进深色模式。
  4. 用 `var()` 推导的 token 要在其输入变化的每一处重新声明（`var()` 在声明它的元素上求值），所以 B3 / B4 重复声明 `--accent`、`--sidebar*`、`--table-*`、`--overview-accent-*`，B1 也匹配 `body .dark`。
  5. 默认预设不写 `data-theme-preset` 属性，`PRESET_DEFAULT_FONT.default` 仍为 `'sans'`；`context/__tests__/theme-preferences.test.tsx` 依赖这两点。
- 效果类与全局钩子都在 `@layer components` 内，调用处的 Tailwind utility 仍然胜出；全局钩子用 `:where()` 保持 0 特异性，品牌 class 与调用处 class 可以覆盖它们。
- **`:root body` 前缀**：CSS `@import` 会被提升，`brand.css` 的未分层规则在源码顺序上排在 `index.css` 自身的未分层规则之前。需要压过 `index.css` 的规则时，必须加 `:root body` 前缀提高特异性，不要靠调整顺序。目前只有卡片 hover 阴影：`index.css` 的选择器为 0,4,0，`brand.css` 写作 `:root body:not([data-theme-preset='simple-large']) …`，特异性 0,5,1（定价页卡片 0,6,1），`simple-large` 因此沿用 `index.css` 的 hover。
- **层内顺序**：认证卡片的按钮规则必须排在全局按钮质感规则之后（特异性相同）；`simple-large` 弹层实色规则排在 `@supports` 弹层玻璃规则之后；`prefers-reduced-transparency` 块必须是全局钩子层的最后一块。
- **贴着内阴影画 1px 线**：卡片与内容面板的 `inset 0 1px 0` 高光会盖住第 0 行，定价卡片顶边与内容面板顶边（`--panel-rim`）都放在 `background-position` 的 `0 1px`。
- `brand.css` 只由主题层维护者修改；各页面通过 `className` 使用 `brand-*` class，不在功能目录里写自定义 CSS。

### 夜岛（night island）

- 首页的 API 终端（位于"工作流程"区块右栏）在浅色、深色模式下都显示为深色"仪表"：外层 div 同时加 `class="dark"` 与 `data-brand-island=""`。`.dark` 是普通类选择器，`theme.css` 的深色 token、所有 `dark:` 变体以及 B2 / B4 都会作用到这棵子树；B5 设置文字颜色与 `color-scheme: dark`。
- 目前只有这一处夜岛。新增夜岛时两个属性必须同时加；在命名预设下夜岛使用 `theme.css` 的经典深色 token。

### 字体依赖

- `@fontsource-variable/geist`、`@fontsource-variable/geist-mono`（`^5.3.0`，OFL-1.1），随构建自托管，不走 CDN；在 `index.css` 中紧随 `@fontsource-variable/lora` 导入。
- 只有默认预设把 `--font-display` / `--font-mono` 指向 Geist / Geist Mono（B3），并带中日韩回退字体；构建产物多出 Geist 系 woff2 文件，按需加载。`font-display` 工具类由 `brand.css` 的非 inline `@theme` 注册，输出 `var(--font-display)`，因此默认预设与 serif 轴的 `body` 级覆盖能生效。

### Button 数据属性

`components/ui/button.tsx` 在 `data-slot='button'` 之后、`{...props}` 之前新增 `data-variant={variant ?? 'default'}` 与 `data-size={size ?? 'default'}`，其他不变。`brand.css` 的按钮质感（默认按钮的高光、阴影、hover 加深 / 提亮）、认证卡片 44px 控件与极光提交按钮都依赖这两个属性。副作用排查：既有选择器中只有 ItemGroup 的 `has-data-[size=sm]` / `has-data-[size=xs]` 间距规则可能匹配 Button，它只在 API Keys 页"API 地址"弹层中使用，那里的 item 为 `xs`，间距不变；没有 JavaScript 读取这两个属性。

### 无障碍与降级

- 所有装饰层 `aria-hidden`、`pointer-events: none`，不新增 i18n key。
- 动效全部受 `prefers-reduced-motion: no-preference` 门控；漂移 mesh 与声呐涟漪还要求视口 ≥ 768px。减少动态效果时没有漂移、声呐、光束、渐变平移、呼吸点、连接线流光与轨道旋转，光束改为静态渐变；静态渐变字按 140% 宽显示完整的三段色，只有 `--flow` 动画时用 220%。动画只用 transform / opacity / scale / rotate / translate（光束与 hero 渐变文字平移除外），装饰层不用 `filter: blur()`，`.brand-backdrop` 带 `contain: layout paint style`。
- 浅色下紧贴渐变展示字的光晕（`.brand-glow:has(+ .brand-text-aurora)`，即错误页状态码）核心降到 30%：渐变字最浅一档的对比度为 3.27:1，核心 40% 时只有 2.86:1，低于大字 3:1。其余光晕（首页、统计条、步骤、认证 hub）核心 40%；深色光晕不变。
- 两侧玻璃条在竖向与朝内容方向两个遮罩相交后渐隐，不留硬边；`[dir='rtl']` 下朝内方向随之翻转。折射只用 `blur(6px)`。
- `prefers-reduced-transparency`：玻璃与弹层改为实色，玻璃条隐藏。`prefers-contrast: more`：发丝线加深，玻璃改实色，渐变文字改为前景色。`forced-colors`：隐藏装饰层（背景层、光晕、点阵、连接线、发丝线、光束、光泽），玻璃 / 卡面 / 终端加实线边框，渐变文字用 `CanvasText`。打印时隐藏背景层、光晕与点阵。不支持 `background-clip: text` 时渐变文字退化为纯色。

### 兼容性

- 不涉及后端、接口、数据库与 i18n 文件。
- 默认预设用户的外观会变化（配色、圆角、标题与等宽字体）；选择命名预设的用户配色不变，只多出结构效果；`simple-large` 关闭全部氛围效果，是最接近原有观感的选择。
- 与上游同步时，`brand.css` 与 `components/brand/` 是 fork 独有文件，不会冲突；§19.2 列出的页面文件如果上游改了 class 或结构，先采纳上游版本，再按本节规则把 `brand-*` class、装饰层与 `data-slot` 钩子补回。`button.tsx` 的两个属性是 CSS 契约，合并时不能丢。

### 测试

- `web/src/components/brand/__tests__/brand-backdrop.test.tsx`：7 个 variant 的根节点都是 `aria-hidden`、`data-brand-backdrop` 与 variant 一致、无可聚焦子元素；`rings='sonar'` 渲染 3 个环、`rings='static'` 渲染一个 `.brand-rings`、默认无环；`flutes` 渲染 2 条、`'refract'` 加 `brand-flutes--refract`、默认无。
- `web/src/components/ui/__tests__/button-attributes.test.tsx`：`data-variant` / `data-size` 默认为 `default`，反映 `variant='outline' size='sm'`，`render={<a href='/x' />}` 时仍然存在且 role 保持 `button`。
- `web/src/components/layout/components/__tests__/section-page-layout.test.tsx`：5 个 `data-slot` 钩子各出现一次；只有一个标题且位于标题钩子上；标题含块级子元素时短竖线不占标题文档流；未传送内容时页脚容器为空，`PageFooterPortal` 传送内容后可见；只有传入 Actions 时才渲染操作区。
- `web/src/components/layout/components/__tests__/public-header.test.tsx`：桌面导航链接的目标；只有当前路径的链接处于激活态；未登录时登录按钮指向 `/sign-in`；滚动后胶囊使用 `[--glass-bg:var(--glass-bg-strong)]`；汉堡按钮（"Toggle navigation menu"）打开可交互的遮罩并锁定页面滚动；未登录时移动菜单 CTA 指向 `/sign-in`，点击后关闭菜单、解除滚动锁定；遮罩里的背景层 `aria-hidden`。
- `web/src/components/layout/components/__tests__/footer.test.tsx`：默认页脚的项目署名链接在新窗口打开；按状态开关显示或隐藏用户协议与隐私政策链接。
- `web/src/features/home/__tests__/landing.test.tsx`：未登录时 hero 与 CTA 的"Get Started"指向 `/sign-up`、"View Pricing"指向 `/pricing`；`docs_link` 以 `http` 开头时渲染新窗口外链，否则渲染路由链接；登录后"Go to Dashboard"指向 `/dashboard` 且 CTA 区不渲染；hero（`hero-center`）与 CTA 背景层 `aria-hidden`；终端有 4 个协议标签，点击后切换接口路径。布局回归：hero 标题栈居中，统计读数与 `aria-hidden` 的光地平线在 hero 区块内；终端与三个步骤同在"工作流程"区块；副标题带 `break-keep wrap-break-word`；CTA 按钮行 `flex-wrap`；终端页脚 `flex-wrap`、"stream · sse"不换行；统计标签 `font-sans`；步骤与小功能说明 `break-keep wrap-break-word text-pretty`；功能卡片编号在阅读顺序上位于标题之前。
- `web/src/features/auth/__tests__/auth-layout.test.tsx`：子内容渲染在 `[data-slot='auth-card']` 内；首页链接 `href='/'` 且包含站点名，Logo 的 alt 为"Logo"；加载中显示骨架；品牌面板（`[data-slot='auth-brand-panel']`）在 DOM 中位于卡片之后，不含标题，根节点带 `hidden` 与 `lg:flex`；面板根节点 `aria-hidden`、无可聚焦子元素、页面没有 complementary 地标；卡片 `max-w-[480px]` 且 `<p>` 用 `text-pretty`；所有背景层 `aria-hidden`。
- `web/src/features/auth/__tests__/oauth-providers.test.tsx`：无提供方时不渲染；启用 GitHub 时显示"Or continue with"，需要同意条款而未勾选时按钮禁用；提供方按钮是默认尺寸的 outline 按钮、不带 `h-11`，由卡片的 44px 规则定高。
- `web/src/features/auth/__tests__/auth-brand-panel.test.tsx`：hub 是位于文案之前、在文档流中的尺寸容器（不是绝对定位）；空间过矮时隐藏轨道；文案 `max-w-xl`、说明 `max-w-md`；说明的中日韩换行 class。
- `web/src/features/pricing/__tests__/search-bar.test.tsx`：Ctrl+K / Meta+K 聚焦、Escape 失焦、输入经 `onChange` 上报、空值时显示快捷键提示而非清除按钮、宽右内边距只从 sm 起生效、有值时清除按钮清空。
- `web/src/features/pricing/__tests__/loading-skeleton.test.tsx`：搜索框占位与真实搜索框同高同圆角；区域 `aria-busy`，卡片视图 6 个卡片占位，表格视图显示行占位。
- `web/src/features/pricing/__tests__/pricing-header.test.tsx`：加载中显示 `aria-busy` 骨架而非头部；标题 `w-fit`、说明 `text-balance`。
- `web/src/features/pricing/__tests__/pricing-controls.test.tsx`（既有文件，新增 2 个用例）：Standard / Recharge / `/1M` / `/1K` 与排序按钮都是 `h-8`；移动筛选抽屉带 `data-brand-page='pricing'`，其中的侧栏为 `bg-transparent bg-none shadow-none`。
- `web/src/features/rankings/__tests__/rankings-states.test.tsx`：在 TanStack 内存路由的 `/rankings` 下渲染真实的 `<Rankings />`，只 mock `api.get` 并替换 VChart；覆盖加载（3 个占位，无数据、无错误）、业务失败（显示"Unable to load rankings"与服务端原因）、有数据（Top Models、Market Share、上升 / 下降标题与模型链接）三态，锁定 `rankings/index.tsx` 的 `if / else` 改写。
- `web/src/features/rankings/__tests__/rankings-hero.test.tsx`：只有当前周期标签 `aria-selected`，点击其他标签上报新周期；标题 `w-fit`。
- `web/src/features/errors/__tests__/status-heading.test.tsx`：401 / 403 / 404 / 500 / 503 页各只有一个 h1 且内容为状态码，光晕只有一个且 `aria-hidden`；`general-error` 的 `minimal` 模式没有 h1、光晕与背景层。
- `web/src/features/dashboard/components/overview/__tests__/api-info-item-layout.test.tsx`：路由名 `shrink-0 max-w-full break-keep`、不截断，不被长说明挤压；说明、所在行与 URL 用 `min-w-0` / `truncate` 吸收溢出。
- 改动主题层后需要一并运行的既有用例（设计稿 §10）：`context/__tests__/theme-preferences.test.tsx`、`components/ui/__tests__/dialog-layout.test.tsx`、`features/dashboard/components/overview/__tests__/setup-guide.test.tsx`、`features/system-update/__tests__/update-checking.test.tsx`、`features/security/__tests__/page.test.tsx`、`features/pricing/__tests__/`、`features/performance-metrics/__tests__/summary.test.tsx`、`components/data-table/`、`features/auth/`（含 `passkey`、`otp`、`secure-verification` 的既有用例）、`hooks/__tests__/sidebar-config.test.tsx`、`features/channels/components/__tests__/table-refresh.test.tsx`、`components/__tests__/live-rpm.test.tsx`，以及 `features/keys/`、`features/usage-logs/`、`features/models/` 的列表用例；最后跑一次全量 `bun run test`。
- vitest 不处理 CSS，视觉效果需要人工检查，见 §20 测试缺口。

### 后续（本次未做）

- 图表（VChart）换成极光配色：需要改 `features/dashboard/lib/charts.ts`，并有意更新 `charts.test.ts`。
- 可选的 `classic` 预设（需要在 7 种语言中新增 `preset.classic`）、玻璃 toast（`ui/sonner.tsx`）、指针聚光效果、`<html lang>` 联动的中日韩字距。
- 控制台页面内使用真玻璃：要让 `lib/motion.ts` 的 `MOTION_VARIANTS.pageEnter` 动画结束后清除 `filter`（例如 `transitionEnd: { filter: 'none' }`），`AnimatedOutlet` 才不再是 backdrop root；`PageTransition` 共用该变体，会一并受影响。
