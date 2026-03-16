# CLI 代理 API

[English](README.md) | 中文

一个为 CLI 提供 OpenAI/Gemini/Claude/Codex 兼容 API 接口的代理服务器。

现已支持通过 OAuth 登录接入 OpenAI Codex（GPT 系列）和 Claude Code。

您可以使用本地或多账户的CLI方式，通过任何与 OpenAI（包括Responses）/Gemini/Claude 兼容的客户端和SDK进行访问。

## 赞助商

[![bigmodel.cn](https://assets.router-for.me/chinese-4.7.png)](https://www.bigmodel.cn/claude-code?ic=RRVJPB5SII)

本项目由 Z智谱 提供赞助, 他们通过 GLM CODING PLAN 对本项目提供技术支持。

GLM CODING PLAN 是专为AI编码打造的订阅套餐，每月最低仅需20元，即可在十余款主流AI编码工具如 Claude Code、Cline、Roo Code 中畅享智谱旗舰模型GLM-4.7，为开发者提供顶尖的编码体验。

智谱AI为本软件提供了特别优惠，使用以下链接购买可以享受九折优惠：https://www.bigmodel.cn/claude-code?ic=RRVJPB5SII

---

<table>
<tbody>
<tr>
<td width="180"><a href="https://www.packyapi.com/register?aff=cliproxyapi"><img src="./assets/packycode.png" alt="PackyCode" width="150"></a></td>
<td>感谢 PackyCode 对本项目的赞助！PackyCode 是一家可靠高效的 API 中转服务商，提供 Claude Code、Codex、Gemini 等多种服务的中转。PackyCode 为本软件用户提供了特别优惠：使用<a href="https://www.packyapi.com/register?aff=cliproxyapi">此链接</a>注册，并在充值时输入 "cliproxyapi" 优惠码即可享受九折优惠。</td>
</tr>
<tr>
<td width="180"><a href="https://www.aicodemirror.com/register?invitecode=TJNAIF"><img src="./assets/aicodemirror.png" alt="AICodeMirror" width="150"></a></td>
<td>感谢 AICodeMirror 赞助了本项目！AICodeMirror 提供 Claude Code / Codex / Gemini CLI 官方高稳定中转服务，支持企业级高并发、极速开票、7×24 专属技术支持。 Claude Code / Codex / Gemini 官方渠道低至 3.8 / 0.2 / 0.9 折，充值更有折上折！AICodeMirror 为 CLIProxyAPI 的用户提供了特别福利，通过<a href="https://www.aicodemirror.com/register?invitecode=TJNAIF">此链接</a>注册的用户，可享受首充8折，企业客户最高可享 7.5 折！</td>
</tr>
</tbody>
</table>


## 功能特性

- 为 CLI 模型提供 OpenAI/Gemini/Claude/Codex 兼容的 API 端点
- 新增 OpenAI Codex（GPT 系列）支持（OAuth 登录）
- 新增 Claude Code 支持（OAuth 登录）
- 新增 Qwen Code 支持（OAuth 登录）
- 新增 iFlow 支持（OAuth 登录）
- 支持流式与非流式响应
- 函数调用/工具支持
- 多模态输入（文本、图片）
- 多账户支持与轮询负载均衡（Gemini、OpenAI、Claude、Qwen 与 iFlow）
- 简单的 CLI 身份验证流程（Gemini、OpenAI、Claude、Qwen 与 iFlow）
- 支持 Gemini AIStudio API 密钥
- 支持 AI Studio Build 多账户轮询
- 支持 Gemini CLI 多账户轮询
- 支持 Claude Code 多账户轮询
- 支持 Qwen Code 多账户轮询
- 支持 iFlow 多账户轮询
- 支持 OpenAI Codex 多账户轮询
- 通过配置接入上游 OpenAI 兼容提供商（例如 OpenRouter）
- 可复用的 Go SDK（见 `docs/sdk-usage_CN.md`）

## 新手入门

CLIProxyAPI 用户手册： [https://help.router-for.me/](https://help.router-for.me/cn/)

## 管理 API 文档

请参见 [MANAGEMENT_API_CN.md](https://help.router-for.me/cn/management/api)

## Amp CLI 支持

CLIProxyAPI 已内置对 [Amp CLI](https://ampcode.com) 和 Amp IDE 扩展的支持，可让你使用自己的 Google/ChatGPT/Claude OAuth 订阅来配合 Amp 编码工具：

- 提供商路由别名，兼容 Amp 的 API 路径模式（`/api/provider/{provider}/v1...`）
- 管理代理，处理 OAuth 认证和账号功能
- 智能模型回退与自动路由
- 以安全为先的设计，管理端点仅限 localhost

**→ [Amp CLI 完整集成指南](https://help.router-for.me/cn/agent-client/amp-cli.html)**

## SDK 文档

- 使用文档：[docs/sdk-usage_CN.md](docs/sdk-usage_CN.md)
- 高级（执行器与翻译器）：[docs/sdk-advanced_CN.md](docs/sdk-advanced_CN.md)
- 认证: [docs/sdk-access_CN.md](docs/sdk-access_CN.md)
- 凭据加载/更新: [docs/sdk-watcher_CN.md](docs/sdk-watcher_CN.md)
- 自定义 Provider 示例：`examples/custom-provider`

## API Key 策略与数据迁移

### 周限额与时间窗口

- `每日次数上限` 继续按 `UTC+8` 自然日统计。
- `每日成本上限` 按 `UTC+8` 自然日统计。
- `每周成本上限` 现在支持通过 `weekly-budget-anchor-at` 指定起始时间。
- 当设置了 `weekly-budget-anchor-at` 后，周预算窗口不再固定按“周一 00:00 到下周一 00:00”，而是从该时间开始，按连续 `168` 小时计算。
- 管理中心默认会把“每周预算开始时间”设为当前整点，也可以手动指定到小时。
- 如果未设置 `weekly-budget-anchor-at`，服务端仍兼容旧逻辑，按 `UTC+8` 周一 `00:00` 起算。

示例：

- `weekly-budget-anchor-at: 2026-03-15T10:00:00+08:00`
- 当前周预算窗口为 `2026-03-15 10:00 ~ 2026-03-22 10:00`

### “最近 7 小时”用量为何会变化

- 管理中心中的 `最近 7 小时` 是滚动时间窗口，不是固定分桶。
- 例如：
  - `10:00` 查看时，窗口是 `03:00 ~ 10:00`
  - `10:40` 再查看时，窗口会变成 `03:40 ~ 10:40`
- 因此，即使中间没有新请求，只要早先的一部分请求被滚出窗口，价格和请求数也会发生变化。

### SQLite 存储位置

CLIProxyAPI 目前使用同一份 SQLite 文件同时保存以下数据：

- API Key 每日次数限额计数
- API Key billing / usage 历史数据
- 模型价格表（`model_prices`）

SQLite 路径解析规则如下：

1. 优先使用环境变量 `APIKEY_POLICY_SQLITE_PATH`
2. 否则使用 `WRITABLE_PATH/api_key_policy_limits.sqlite`
3. 如果 `WRITABLE_PATH` 未设置，则回退到配置文件目录下的 `api_key_policy_limits.sqlite`

Docker Compose 默认路径通常是：

- `/CLIProxyAPI/data/api_key_policy_limits.sqlite`

### 模型价格保存在哪里

- 模型价格保存在同一个 SQLite 文件中的 `model_prices` 表。
- 该表不仅保存手工维护的价格，也会保存当前服务端同步后的持久化价格数据。
- 因此，直接迁移这份 SQLite 文件，通常就能同时迁走 usage、限额计数和模型价格。

### 迁移到另一台机器

最稳妥的方式是停机后直接复制 SQLite 文件。

#### 方式一：停机迁移，推荐

1. 停止旧机器上的 CLIProxyAPI
2. 复制以下文件到新机器相同路径
3. 启动新机器上的 CLIProxyAPI

需要复制的文件：

- `api_key_policy_limits.sqlite`
- `api_key_policy_limits.sqlite-wal`（如果存在）
- `api_key_policy_limits.sqlite-shm`（如果存在）

这种方式会一并迁移：

- API Key 次数限额已消费计数
- usage / billing 历史
- 模型价格数据
- 周预算和日预算判断所依赖的数据

#### 方式二：不停机迁移

如果旧机器不能停机，不建议直接复制正在写入中的 SQLite 主文件。此时应改用管理接口导出/导入：

- usage 导出：`GET /v0/management/usage/export`
- usage 导入：`POST /v0/management/usage/import`
- model prices 导出：`GET /v0/management/model-prices/export`
- model prices 导入：`POST /v0/management/model-prices/import`

说明：

- `usage export/import` 用于迁移用量与 billing 快照
- `model-prices export/import` 用于迁移当前持久化模型价格表
- 如果你还想保留“每日次数限额已经消耗到哪里”的状态，仍然建议直接迁移整份 SQLite 文件

### 建议

- 单机、单实例、少量 API Key 场景下，继续使用 SQLite 即可
- 当你需要多实例共享同一套预算与 usage 数据时，再考虑切换到 PostgreSQL
- 对于当前大约几十个 API Key 的规模，优先保证配置语义清晰和迁移流程简单，收益高于过早引入更重的数据库

## 贡献

欢迎贡献！请随时提交 Pull Request。

1. Fork 仓库
2. 创建您的功能分支（`git checkout -b feature/amazing-feature`）
3. 提交您的更改（`git commit -m 'Add some amazing feature'`）
4. 推送到分支（`git push origin feature/amazing-feature`）
5. 打开 Pull Request

## 谁与我们在一起？

这些项目基于 CLIProxyAPI:

### [vibeproxy](https://github.com/automazeio/vibeproxy)

一个原生 macOS 菜单栏应用，让您可以使用 Claude Code & ChatGPT 订阅服务和 AI 编程工具，无需 API 密钥。

### [Subtitle Translator](https://github.com/VjayC/SRT-Subtitle-Translator-Validator)

一款基于浏览器的 SRT 字幕翻译工具，可通过 CLI 代理 API 使用您的 Gemini 订阅。内置自动验证与错误修正功能，无需 API 密钥。

### [CCS (Claude Code Switch)](https://github.com/kaitranntt/ccs)

CLI 封装器，用于通过 CLIProxyAPI OAuth 即时切换多个 Claude 账户和替代模型（Gemini, Codex, Antigravity），无需 API 密钥。

### [ProxyPal](https://github.com/heyhuynhgiabuu/proxypal)

基于 macOS 平台的原生 CLIProxyAPI GUI：配置供应商、模型映射以及OAuth端点，无需 API 密钥。

### [Quotio](https://github.com/nguyenphutrong/quotio)

原生 macOS 菜单栏应用，统一管理 Claude、Gemini、OpenAI、Qwen 和 Antigravity 订阅，提供实时配额追踪和智能自动故障转移，支持 Claude Code、OpenCode 和 Droid 等 AI 编程工具，无需 API 密钥。

### [CodMate](https://github.com/loocor/CodMate)

原生 macOS SwiftUI 应用，用于管理 CLI AI 会话（Claude Code、Codex、Gemini CLI），提供统一的提供商管理、Git 审查、项目组织、全局搜索和终端集成。集成 CLIProxyAPI 为 Codex、Claude、Gemini、Antigravity 和 Qwen Code 提供统一的 OAuth 认证，支持内置和第三方提供商通过单一代理端点重路由 - OAuth 提供商无需 API 密钥。

### [ProxyPilot](https://github.com/Finesssee/ProxyPilot)

原生 Windows CLIProxyAPI 分支，集成 TUI、系统托盘及多服务商 OAuth 认证，专为 AI 编程工具打造，无需 API 密钥。

### [Claude Proxy VSCode](https://github.com/uzhao/claude-proxy-vscode)

一款 VSCode 扩展，提供了在 VSCode 中快速切换 Claude Code 模型的功能，内置 CLIProxyAPI 作为其后端，支持后台自动启动和关闭。

### [ZeroLimit](https://github.com/0xtbug/zero-limit)

Windows 桌面应用，基于 Tauri + React 构建，用于通过 CLIProxyAPI 监控 AI 编程助手配额。支持跨 Gemini、Claude、OpenAI Codex 和 Antigravity 账户的使用量追踪，提供实时仪表盘、系统托盘集成和一键代理控制，无需 API 密钥。

### [CPA-XXX Panel](https://github.com/ferretgeek/CPA-X)

面向 CLIProxyAPI 的 Web 管理面板，提供健康检查、资源监控、日志查看、自动更新、请求统计与定价展示，支持一键安装与 systemd 服务。

### [CLIProxyAPI Tray](https://github.com/kitephp/CLIProxyAPI_Tray)

Windows 托盘应用，基于 PowerShell 脚本实现，不依赖任何第三方库。主要功能包括：自动创建快捷方式、静默运行、密码管理、通道切换（Main / Plus）以及自动下载与更新。

### [霖君](https://github.com/wangdabaoqq/LinJun)

霖君是一款用于管理AI编程助手的跨平台桌面应用，支持macOS、Windows、Linux系统。统一管理Claude Code、Gemini CLI、OpenAI Codex、Qwen Code等AI编程工具，本地代理实现多账户配额跟踪和一键配置。

> [!NOTE]  
> 如果你开发了基于 CLIProxyAPI 的项目，请提交一个 PR（拉取请求）将其添加到此列表中。

## 更多选择

以下项目是 CLIProxyAPI 的移植版或受其启发：

### [9Router](https://github.com/decolua/9router)

基于 Next.js 的实现，灵感来自 CLIProxyAPI，易于安装使用；自研格式转换（OpenAI/Claude/Gemini/Ollama）、组合系统与自动回退、多账户管理（指数退避）、Next.js Web 控制台，并支持 Cursor、Claude Code、Cline、RooCode 等 CLI 工具，无需 API 密钥。

> [!NOTE]  
> 如果你开发了 CLIProxyAPI 的移植或衍生项目，请提交 PR 将其添加到此列表中。

## 许可证

此项目根据 MIT 许可证授权 - 有关详细信息，请参阅 [LICENSE](LICENSE) 文件。

## 写给所有中国网友的

QQ 群：188637136

或

Telegram 群：https://t.me/CLIProxyAPI
