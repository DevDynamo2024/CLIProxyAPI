# 工程目录索引

- 安装脚本：`/Users/taylor/code/tools/cliproxyapi-installer`
- 前端（管理中心）：`/Users/taylor/code/tools/Cli-Proxy-API-Management-Center`
- 后端（CLIProxyAPI）：`/Users/taylor/code/tools/CLIProxyAPI`

# 协作提示（Prompt）

- 每次改完代码：需要 **提交（commit）并 push 到 GitHub**（若遇到权限/远端不可用，需明确说明原因与当前状态）。
- 若希望 `cliproxyapi-installer` 安装到最新版本：在 push 之后需要 **打 tag 并发布 release**（后端/前端各自触发 GitHub Actions release 流水线）。推荐使用：`/Users/taylor/code/tools/CLIProxyAPI/scripts/release_publish.sh`
