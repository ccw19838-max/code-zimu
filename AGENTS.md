# 独立仓库开发约定

- 稳定分支为 `main`，当前开发基线为 `codex/v010`；提交与合并按 `CONTRIBUTING.md`。

- 本项目是独立端口、独立前端的代码目录，不合并工作台页面或进程管理。构建和测试不依赖相邻仓库、父目录 go.work 或其他项目数据。Go 1.24+。
- 根目录执行 `GOWORK=off go test -race ./...`、`go vet ./...`、`node --check web/app.js`。SSO 契约单测使用本地 mock，无需真实导航/GitHub 凭据。
- 可选 Workbench SSO 的约定在 `docs/sso-contract.md`。`internal/workbenchsso` 与受控应用适配器/契约测试保持 v1 兼容。开启后禁用旧目录密码及 Cookie，不能回退绕过认证。
- 导航授权只代表目录身份，不是 GitHub 成员资格或仓库权限。普通成员不能读取私有元数据或触发强制刷新；显式目录 admin 才有这些能力。不可把组织服务器令牌的可见仓库全部公开。
- GitHub 令牌只在服务端，禁止进入浏览器、URL、日志或 Git。私有项目媒体不能放在公开静态目录。保留桌面设计，不新增手机端。
- 独立演示不设置 SSO 变量，使用 `scripts/run-demo.py` 和临时数据。跨仓库验收交付自己的二进制即可。
