# new-api 二开功能说明（Fork Features）

本文档记录本仓库（`JeffZhu1122/new-api`）相对上游 `QuantumNous/new-api` 所做的全部二次开发内容，供团队成员理解每个功能的动机、实现位置、配置方式和已知限制，并作为后续与上游同步时的冲突排查索引。

| 项目 | 值 |
|---|---|
| 文档覆盖的最后一个功能提交 | `f69683a02` — 2026-09-26 `feat(tokens): bind a key to a primary group plus ordered fallback groups` |
| 上游基线（merge-base） | `c2b7a9a9e` — 2026-09-25 `fix(claude): preserve per-message output_config in Claude messages (#7561)` |
| 最近一次同步 | 2026-09-27，rebase 到 `c2b7a9a9e`，无冲突 |
| fork 专有提交数 | 18 个功能提交（不含本文档自身的提交） |
| 变更规模 | 146 个文件，+8762 / −1214 行 |

同步策略：fork 采用 **rebase 到上游 main** 的方式跟进，因此 `git log c2b7a9a9e..HEAD` 得到的提交（18 个功能提交加本文档的提交）就是全部二开内容，提交的作者日期保留了原始开发时间（2026-08-15 起）。注意中间提交不保证独立可编译（例如 `20d115227` 调用了下一个提交才定义的 `AddFailedChannel`），所有描述以 HEAD 代码为准。

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
14. [仓库维护类变更](#14-仓库维护类变更)
15. [与上游同步的注意事项](#15-与上游同步的注意事项)
16. [已知限制与测试缺口汇总](#16-已知限制与测试缺口汇总)
17. [附录](#17-附录)

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
| 14 | 移除 GitHub workflows | `a034e98b3` | — | `.github/workflows/` | CI |

所有功能均**默认保持上游行为**：开关默认关闭、数值默认 0、JSON 默认为空，因此把 fork 部署到现有环境不会改变任何既有请求的处理结果（第 12 节"新建默认禁用"是唯一的例外，它只影响新建和复制操作）。

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

关键方法与行为：

- `ChannelExtendSettings.Validate()`：超时 `[0, 86400]`，RPM/TPM `[0, MaxInt32]`，输入 token `[0, 10_000_000]` 且两者都 >0 时要求 `max > min`，auth mode 必须是枚举值。`controller/channel.go` `validateChannel()` 调用它，Add/Update/批量接口共用。
- `ChannelExtendSettings.IsZero()`：所有数值为 0 且 auth mode 为 `""` 或 `api_key` 时为真。
- `UpsertChannelExtend(tx, channelId, settings)`：`IsZero()` → 删除该行；否则 `ON CONFLICT(channel_id) DO UPDATE` 全列。`Channel.SaveExtendConfig(tx)` 在 `Channel.Insert()` 和 `BatchInsertChannels` 中调用；`ExtendConfig == nil` 时不动已有行。
- 级联删除：`DeleteChannelExtendByIds` 在 `Channel.Delete()`、`BatchDeleteChannels`、以及新增的 `deleteChannelsWhere`（供 `DeleteChannelByStatus` / `DeleteDisabledChannel` 使用）中调用。
- 读取：`GetChannelExtend(id)` 无行返回零值且 `err == nil`。
- 缓存：`model/channel_cache.go` 的 `channelExtendIDM map[int]ChannelExtendSettings` 在 `InitChannelCache()` 整表加载，并预填到缓存的 `Channel.ExtendConfig`（渠道选择在持锁路径中读取，避免锁内查库）。`GetChannelExtendSettings(id)`：内存缓存开启时查 map，否则查库。
- 请求链路：`middleware/distributor.go` `SetupContextForSelectedChannel` 写入 `constant.ContextKeyChannelExtendSetting` → `relay/common/relay_info.go` `InitChannelMeta` 拷入 `ChannelMeta.ChannelExtendSetting`，适配器由此读取。
- 管理 API：`GET /api/channel/:id` 仅在非零时附带 `extend_config`；`PUT /api/channel/` 仅在请求 JSON **显式包含** `extend_config` 键时写入（显式 `null` 视为清除，缺键则不改）；`CopyChannel` 单独读取并随克隆写入。
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

## 14. 仓库维护类变更

### 14.1 移除 GitHub workflows（`a034e98b3`）

删除 `.github/workflows/` 下全部 6 个文件：`ci.yml`、`docker-build.yml`、`docker-image-branch.yml`、`electron-build.yml`、`release.yml`、`sync-release-to-gitcode.yml`。目的是避免 fork 在 GitHub 上触发上游的构建、发版和镜像同步流水线。`.github/` 下的 issue / PR 模板、`CODE_OF_CONDUCT.md`、`FUNDING.yml`、`SECURITY.md` 保留。

### 14.2 移植提交（`5f22a93b0`）

上游在基线之前重构了四个接缝（seam）：重试判定抽到 `service.DecideRelayRetry`；渠道选择抽到 `service.SelectChannelForRequest`（HTTP distributor 与 Responses WebSocket 共用）；计费准备抽到 `relay.PrepareRequestBilling`；重试设置 UI 迁到 request-policies 页并经 `/api/option/request_policy` 整体校验。该提交把 fork 功能 re-home 到这些接缝上：

- 400 关键词判断移入 `DecideRelayRetry`，5 个重试相关 option key 加入 request-policy 白名单与校验，前端字段从 `SecuritySettings` 移到 `request-policies/`。
- 输入 token 边界、渠道 RPM/TPM、count_tokens 门控在 `SelectChannelForRequest` 内生效（因此 Responses WebSocket 也继承渠道过滤，但见 §4 / §10 中 WS 未覆盖的两点）。
- count_tokens / input_tokens 免费路径进入 `PrepareRequestBilling`。
- 分组模型折扣成为分组设置编辑器的一个标签页。
- 上游测试适配 `GetRandomSatisfiedChannel` 新签名、`channel_extend` 表与新 option。
- 同时把 `channel.ExtendConfig.Validate()` 在 `validateChannel` 中的位置前移；`go.mod` 中 `github.com/fxamacker/cbor/v2` 从 indirect 变为直接依赖。

---

## 15. 与上游同步的注意事项

### 15.1 流程

1. `git fetch https://github.com/QuantumNous/new-api.git main`
2. `git rebase FETCH_HEAD`（fork 提交线性重放）
3. 解决冲突后跑 Go 与前端测试，重点是下面列出的热点文件。
4. 如上游再次重构接缝，参照 `5f22a93b0` 的做法把 fork 逻辑挪到新接缝，而不是在旧位置硬保留。

### 15.2 高频冲突文件

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
| `relay/request_billing.go` / `service/text_quota.go` / `service/quota.go` | 免费路径、TPM 记账、realtime 预扣读 PriceData |
| `middleware/distributor.go` | 输入 token 估算、extend setting 注入、count_tokens 不记亲和 |
| `middleware/auth.go` | 多分组 key 归一化 |
| `controller/token.go` | `applyTokenGroupBinding` |
| `model/option.go` / `model/request_policy.go` / `controller/option.go` | 新 option key |
| `router/relay-router.go` | 两个新路由、`ModelRpmTpmRateLimit` 挂载 |
| `relaykit/dto/channel_extend_settings.go` / `channel_settings.go` / `user_settings.go` | DTO |
| `web/src/features/channels/components/drawers/channel-mutate-drawer.tsx` | 大量新增字段 |
| `web/src/features/channels/lib/channel-form.ts` | extend_config 与 count_tokens 映射 |
| `web/src/i18n/locales/*.json` | 新增文案 |

### 15.3 上游签名变化的传染点

- `model.GetRandomSatisfiedChannel(group, model, retry, filters, excludeChannelIds)` 与 `model.GetChannel(..., excludeChannelIds)`：上游新增调用点需补传 `nil`。
- `relaykit/` 必须独立可编译：改动 `relaykit/dto/*` 后运行 `cd relaykit && GOWORK=off go build ./...`。

---

## 16. 已知限制与测试缺口汇总

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

### 测试缺口

- `DecideRelayRetry` 的 `retry_keyword_matched` 分支。
- `controller/relay.go` `getChannel` 候选耗尽返回可配置状态码的路径。
- `doRequest` 的渠道级 deadline、`cancelOnCloseBody`、`StreamScannerHandler` 的超时覆盖。
- `countTokensPassthrough`、`PostCountTokensLog`、`PrepareRequestBilling` 的跳过分支。
- 渠道 RPM/TPM 与输入 token 边界的前端字段测试；distributor 端到端。

---

## 17. 附录

### 17.1 新增 option key 一览

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

### 17.2 新增 HTTP 端点

| 端点 | 说明 |
|---|---|
| `POST /v1/messages/count_tokens` | Anthropic token 计数透传，免费 |
| `POST /v1/responses/input_tokens` | OpenAI Responses 输入 token 计数透传，免费 |

### 17.3 管理 API 新增字段

| 资源 | 字段 | 说明 |
|---|---|---|
| Channel | `extend_config.{relay_timeout, streaming_timeout, min_input_tokens, max_input_tokens, rpm_limit, tpm_limit, claude_auth_mode}` | §2.1，PUT 缺键不改、`null` 清除 |
| Channel | `settings.count_tokens_enabled` | §6 |
| User | `rate_limit` | §7，`nil` 不改、`{}` 清除 |
| User | `model_discount` | §9，同上 |
| Token | `auto_groups`（普通分组下） | §13，作为备用分组 |

### 17.4 Redis key 一览

| key | 用途 |
|---|---|
| `rateLimit:v2:mrpm:<userId>:<group>:<model>` | 用户 × 模型 RPM 固定窗口 |
| `rateLimit:v2:mtpm:<userId>:<group>:<model>:<unixMinute>` | 用户 × 模型 TPM 分钟桶 |
| `rateLimit:v2:crpm:<channelId>` | 渠道 RPM 固定窗口 |
| `rateLimit:v2:ctpm:<channelId>:<unixMinute>` | 渠道 TPM 分钟桶 |
| `user_extend_rl:<userId>` | 用户限流覆盖缓存 |
| `user_extend_md:<userId>` | 用户模型折扣缓存 |

### 17.5 消费日志 `other` 新增键

| 键 | 含义 |
|---|---|
| `group_model_discount` | 分组模型折扣（≠ 1 时写入） |
| `user_model_discount` | 用户模型折扣（≠ 1 时写入） |
| `endpoint` | `count_tokens` / `input_tokens`，标记零额计数日志 |

### 17.6 fork 提交列表（按时间）

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
