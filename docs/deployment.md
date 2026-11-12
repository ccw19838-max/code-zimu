# 代码目录部署与登录

## 接入 GitHub 组织

先通过部署环境配置以下变量，再启动服务：

| 环境变量 | 用途 |
| --- | --- |
| `GITHUB_CATALOG_ADMIN_PASSWORD` | 本服务的管理员密码，真实模式必填，至少 12 个字符 |
| `GITHUB_CATALOG_TOKEN` | 后端读取 GitHub 的令牌；只展示公开仓库时可留空 |

```bash
./bin/github-catalog -github-org YOUR_ORG
```

`YOUR_ORG` 填组织登录名。团队项目应位于该组织下；加入组织不会自动将成员个人账号的仓库转入组织。

公开目录支持匿名读取。读取私有仓库元数据，推荐使用 GitHub App 安装令牌，首次验证也可使用受限的 fine-grained PAT，授权所需仓库的 `Metadata: read`。令牌由服务端保管，不返回浏览器。

**真实模式下，访客只能看到公开项目；本服务管理员可以查看服务器获准读取的私有项目。** 目前尚未实现成员 GitHub 身份绑定及逐成员仓库权限过滤。

目录缓存 10 分钟，到期后在下次请求时更新；管理员可以手动刷新。当前只读取项目资料，不克隆仓库或保存源代码。安装令牌自动续期、用户授权与组织邀请的接入方案见 [GitHub 组织接入与权限](github-access.md)。

### 启动参数

| 参数 | 默认值 | 说明 |
| --- | --- | --- |
| `-addr` | `127.0.0.1:18091` | HTTP 监听地址 |
| `-public-url` | `http://127.0.0.1:18091` | 浏览器实际访问的源，包含协议、主机和端口 |
| `-demo` | `false` | 使用内置演示数据，不请求 GitHub |
| `-github-org` | 空 | 读取指定组织，与 `-demo` 互斥 |
| `-profiles` | 空 | 真实模式使用的项目资料 JSON 文件 |
| `-navigation-url` | 空 | 可选的外部导航链接 |

在局域网部署时，可使用：

```bash
./bin/github-catalog \
  -github-org YOUR_ORG \
  -addr 0.0.0.0:18091 \
  -public-url http://192.168.1.10:18091
```

其中 IP 为示例，需替换为实际服务地址。`-public-url` 必须与浏览器访问的源一致；使用 HTTPS 反向代理时，填写代理对外提供的源。更新凭据或启动配置后需要重启服务。

### 接入统一导航

与 Workbench Suite 部署在同一服务器时，建议给代码目录使用独立端口 **18092**；套件已将 **18091** 用作 XRplan 的内部端口。配置好本服务的管理员密码及按需提供的 GitHub 令牌后，可运行：

```bash
./bin/github-catalog \
  -github-org YOUR_ORG \
  -addr 0.0.0.0:18092 \
  -public-url http://192.168.9.120:18092 \
  -navigation-url http://192.168.9.120:18080
```

将组织名和示例 IP 替换为实际值；`-navigation-url` 仅提供可选返回链接。导航主页的 `code-zimu` 卡片应指向同一个公开源，成员入口可见性由导航管理员分配，目录自身的管理员登录和 GitHub 仓库权限分别管理。

### 可选：通过导航统一登录

本仓库可单独开发、构建和部署。启用 SSO 后仍保持当前独立界面、端口和进程，浏览器无需再输入目录密码。接入约定见 [SSO v1](sso-contract.md)。

在导航登记 `code-zimu`、自己的公开 URL、固定 `/auth/sso/callback` 和独立 `secret_env`。导航的该凭据与本服务的 `WORKBENCH_CLIENT_SECRET` 取同一随机值（至少 32 字符），分别通过各自环境安全注入。代码服务再配置：

```bash
export WORKBENCH_PORTAL_URL=https://nav.example.com
export WORKBENCH_PUBLIC_URL=https://code.example.com
# WORKBENCH_CLIENT_SECRET 已由环境设置注入，不提交到 Git。
./bin/github-catalog -github-org YOUR_ORG \
  -addr 127.0.0.1:18092 -public-url "$WORKBENCH_PUBLIC_URL"
```

域名替换为实际地址，HTTPS 反代保留公开 Host；本地联调可用自己可达的 HTTP origin。可选 INTERNAL_URL 用于服务端访问导航，不配置 GATEWAY_SECRET。套件导航通过私密 config.json 的 `code_zimu_secret` 和可选 `code_zimu_url` 生成登记信息；代码服务仍由本仓库单独部署。

导航管理员给使用者分配 code-zimu 的 user/admin 授权：user 仅浏览公开目录，admin 可刷新并查看服务器令牌可见的私有元数据。GitHub 仓库的源码权限仍由 GitHub 自己验证。每次请求在线验权，停用/撤权即时生效；“全局退出”撤销其他首方应用会话，不跨源读取导航 Cookie。不同站点的页面在切回前台和每 15 秒重新确认身份。

SSO 启用后旧目录密码与本地 Cookie 均不参与认证，GITHUB_CATALOG_ADMIN_PASSWORD 不再必需；认证服务离线返回 503，不回退本地管理员。所有 SSO 变量均省略时保留原来的独立登录和演示流程，配置不完整则拒绝启动。

单仓库检查使用 `GOWORK=off go test -race ./...` 和 `go vet ./...`，契约单测自带 mock，不需要导航仓库。真实跨应用验收可交付本仓库二进制给导航的 [sso-smoke.py](https://github.com/ccw19838-max/workbench-navigation/blob/main/scripts/sso-smoke.py)，无需在一个环境共同检出所有源码。
