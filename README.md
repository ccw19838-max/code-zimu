# 团队代码目录 · code-zimu

独立部署的 GitHub 项目展示与复用入口。展示项目功能、应用场景、成果示例与开发说明，帮助成员找到已有代码并继续开发。

Go 1.24+ · 原生 HTML/CSS/JavaScript · 单二进制 · 桌面界面

![团队代码目录](docs/ui/github-projects-demo.png)

## 能做什么

- 按名称、功能、场景、成果搜索，按类型、语言、可见性和归档状态筛选。
- 查看功能说明、成果截图、运行依赖、代码入口、维护人和扩展方向。
- 跳转 GitHub 仓库，阅读团队规范并下载协作模板。

基础信息由 GitHub API 提供，功能与成果由维护者确认；资料未完善时显示“待补充”。项目详情和截图均可独立维护。

[项目详情](docs/ui/github-project-detail.png) · [二次开发说明](docs/ui/github-project-development.png) · [团队规范](docs/ui/github-team-rules.png)

## 快速开始

```bash
python3 scripts/run-demo.py --port 18092
```

脚本构建应用并创建独立演示账号，无需 GitHub 组织或令牌。管理员为 admin，随机口令保存在本机 `data/demo/dev-account.json`，权限 0600。程序运行时无需 Python、Node 或数据库服务：

```bash
go build -o bin/github-catalog .
./bin/github-catalog -demo -addr 127.0.0.1:18092 -public-url http://127.0.0.1:18092
```

未配置管理员密码的 Demo 仅提供示例浏览。默认二进制端口仍为 18091，与 Workbench Suite 同机时使用 18092，以避开 XRplan 的内部端口。

## 正式接入与权限

使用 `-github-org YOUR_ORG` 接入组织。服务令牌仅在后端持有；独立登录模式的真实目录需配置至少 12 字符的 GITHUB_CATALOG_ADMIN_PASSWORD。

可选 Workbench SSO 保持独立端口、前端和进程。启用后使用导航分配的身份，停用、撤权及全局退出在线生效，旧目录密码不参与认证。普通成员仅看公开仓库，显式目录 admin 可查看服务器获准读取的私有元数据及刷新目录。GitHub 源码访问权和组织成员资格仍由 GitHub 管理。

- [部署、参数与可选 SSO](docs/deployment.md) · [GitHub 凭据与权限](docs/github-access.md)
- [项目资料维护](docs/project-profiles.md) · [团队代码规则](docs/team-code-rules.md) · [协作模板](templates/README.md)
- [SSO v1 接口](docs/sso-contract.md) · [本仓库贡献与分支约定](CONTRIBUTING.md)

## 开发检查

```bash
GOWORK=off go test -race ./...
go vet ./...
node --check web/app.js
```

SSO 契约单测自带 mock，不需要其他仓库或真实凭据。真实跨应用验收可将本仓库二进制交给导航仓库的 `scripts/sso-smoke.py`。

`main` 为稳定分支，下一阶段开发使用 `codex/v010`。尚未实现 GitHub 用户 OAuth、自动组织邀请与安装令牌自动续期；演示仓库数据与入组交互均为示例。字体与图标许可见 [字体 OFL](web/fonts/OFL.txt)和[图标来源](web/icons/SOURCE.md)。
