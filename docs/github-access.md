# GitHub 组织接入与权限

本文说明当前目录服务的 GitHub 读取范围，以及后续用户授权、入组和默认权限的接入方案。快速启动见 [README](../README.md)，项目说明配置见 [项目资料维护](project-profiles.md)。

## 服务器需要什么权限

只展示项目目录时，服务器读取仓库元数据即可，无须复制源代码，也无须收集每位成员的个人令牌。建议以后由组织所有者安装一个 GitHub App，授权它读取需要展示的组织仓库。

| 用途 | 服务器使用的身份或凭据 | 最小权限与范围 |
| --- | --- | --- |
| 展示公开组织仓库 | 可匿名访问 GitHub API | 只返回公开资源；认证后有相应的 API 配额 |
| 展示组织私有仓库的名称、说明、语言、主题、链接等 | 推荐 GitHub App 安装令牌 | 已授权仓库的 `Metadata: read`；目录接口不需要 `Contents` |
| 临时接入真实组织目录 | Fine-grained PAT | Resource owner 选择目标组织，仅选择所需仓库，保留 `Metadata: read`；可能需要组织审批 |
| 将来读取 README、源代码或 clone 备份 | GitHub App 安装令牌或相应 PAT | 在目录权限之外申请所需仓库的 `Contents: read`；当前未实现 |
| 绑定成员的 GitHub 身份 | GitHub App 用户授权流程或 OAuth App | 用户主动同意；后端完成授权码交换；权限仍受用户本人和应用授权范围限制 |
| 将来邀请成员加入组织 | 组织授权的 GitHub App 安装令牌，或组织所有者凭据 | 组织级 `Members: write`；属于独立的成员管理能力，当前未实现 |

GitHub 官方的 [List organization repositories](https://docs.github.com/en/rest/repos/repos#list-organization-repositories) 明确列出 `Metadata: read`，并支持 App 安装令牌、App 用户令牌和 fine-grained PAT。实际可见范围还受安装时选择的仓库、令牌范围和组织策略约束，令牌不会赋予凭据所有者原本没有的权限。

正式服务推荐 App 安装身份，便于由组织统一管理。Fine-grained PAT 可以作为首次验证的过渡方案；需考虑所有者离职、令牌到期、组织审批和轮换。不要为了目录展示直接申请 classic PAT 的宽泛 `repo` 权限。详见 [管理个人访问令牌](https://docs.github.com/en/authentication/keeping-your-account-and-data-secure/managing-your-personal-access-tokens)。

## 连接已创建的组织

使用 `-demo` 查看示例目录，使用 `-github-org <组织名>` 查询真实目录；两种模式互斥。`-github-org` 填组织登录名，不是 URL。

独立登录的真实模式需要设置 `GITHUB_CATALOG_ADMIN_PASSWORD`，至少 12 个字符，用于本目录服务自己的管理员登录；启用可选 [Workbench SSO](sso-contract.md) 后禁用该密码，改用导航分配的目录角色。目录身份与 GitHub 密码及 GitHub API 令牌分别管理。GitHub 令牌由服务端环境变量 `GITHUB_CATALOG_TOKEN` 提供；只读取公开目录时可以留空。

在本项目目录中，可以用下面的方式临时验证真实组织：

```bash
go build -o bin/github-catalog .
read -rsp '目录管理员密码（至少 12 个字符）: ' GITHUB_CATALOG_ADMIN_PASSWORD
printf '\n'
read -rsp 'GitHub token（仅公开目录可留空）: ' GITHUB_CATALOG_TOKEN
printf '\n'
export GITHUB_CATALOG_ADMIN_PASSWORD GITHUB_CATALOG_TOKEN
./bin/github-catalog \
  -addr 127.0.0.1:18091 \
  -public-url http://127.0.0.1:18091 \
  -github-org YOUR_ORG
```

当前程序在启动时读取令牌和管理员密码，更新环境配置后需重启服务。管理员会话在内存中保存，8 小时后过期，重启服务也会失效。令牌只供后端调用 GitHub API，不返回浏览器。不要将令牌放在启动参数、前端代码、提交到仓库的配置文件或聊天记录中。

当前实现通过 GitHub REST API 读取元数据，缓存 10 分钟后在下次请求时更新；管理员可以手动刷新。它不是后台定时同步任务，也不保存仓库代码。

| 接口 | 当前用途与访问范围 |
| --- | --- |
| `GET /api/session` | 查询本目录服务自己的会话状态 |
| `POST /api/login` | JSON 字段 `username: "admin"` 和 `password`，登录本目录的管理员 |
| `POST /api/logout` | 需要目录管理员会话与 CSRF 校验，退出本目录 |
| `GET /api/github/projects` | 演示模式匿名可见全部示例；真实模式匿名只见公开仓库，目录管理员可见服务器获准访问的私有仓库 |
| `POST /api/github/refresh` | 需要目录管理员会话与 CSRF 校验 |

接口请求的 Host 必须匹配 `-public-url`，所有 POST 还需同源的 `Origin`。退出和刷新使用 `GET /api/session` 返回的 `csrf` 作为 `X-CSRF-Token`。会话 Cookie 名为 `github_catalog_session`。

**目录尚未绑定每位成员的 GitHub 身份。** 真实模式的私有目录仅供本服务管理员查看，未实现按成员 GitHub 权限过滤项目。演示模式中的“私有”标记是虚构状态，因此可匿名展示。链接跳转后，能否打开真实私有仓库仍由 GitHub 判断。

以后向普通成员开放私有目录前，需要接入 GitHub 用户授权，再按其仓库权限或明确的组织共享策略过滤数据；不能把服务器令牌可见的全部私有项目直接公开。本地管理员登录不等于 GitHub 身份认证。

### 安装令牌不是长期静态密码

GitHub App 的安装令牌有效期为 **1 小时**。正式服务应由后端保管 App 私钥，根据 installation ID 生成并轮换安装令牌；私钥及令牌不应发给浏览器。当前实现 **只读取外部提供的 `GITHUB_CATALOG_TOKEN`，尚未实现 App 私钥签名、令牌生成或自动续期**。临时传入安装令牌可以验证目录，持续运行需增加轮换实现，或者先使用受限 PAT 并管理到期时间。

安装令牌与用户授权令牌是两类凭据；不要混用其刷新机制。GitHub App 的过期用户令牌通常为 8 小时，按所启用的授权方式使用 refresh token 换新；它不能替代安装令牌的生成流程。参见 [生成安装令牌](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-an-installation-access-token-for-a-github-app) 和 [生成用户令牌](https://docs.github.com/en/apps/creating-github-apps/authenticating-with-a-github-app/generating-a-user-access-token-for-a-github-app)。

## 用户授权与入组流程（规划）

可以将大部分操作串成一个流程，但需要分别完成**用户授权**和**组织授予成员资格**。用户授权登录不会赋予组织管理权限，也不会让其自动成为组织成员。

建议将来采用以下流程：

1. 用户在目录服务点击「连接 GitHub」，跳转到 GitHub 的授权页面并明确同意。目录后端校验 `state`，使用 PKCE（GitHub App 网页授权支持 `S256`），完成授权码交换并建立绑定 GitHub 用户 ID 的成员会话。用户不用手动复制 token，系统也不能在没有用户同意时凭空获得令牌。
2. 用户申请加入团队。目录服务根据已确认的组织准入规则或管理员审核判断是否可以发出邀请；连接 GitHub 账号本身不代表获准加入组织。
3. 组织所有者事先安装并授权具有 `Members: write` 的管理 App。后端通过该组织身份调用 `POST /orgs/{org}/invitations`，使用绑定后的 GitHub 用户 ID，角色固定为普通成员 `direct_member`，可附预设的 `team_ids`。用户本人提供普通 token 并不能让系统拥有邀请权限。
4. 用户在 GitHub 接受邀请；接受前仍是待加入状态。邀请通常 7 天后过期，组织如要求 2FA，用户须先启用；企业身份策略和许可条件也可能影响加入。
5. 后端确认成员状态后，再将界面标为「已加入」。邀请附带的 Team 或后续 Team 分配决定可访问的仓库；成员继续用自己的 GitHub 账号操作仓库。

生产接入宜把目录只读能力与成员管理能力分别配置，避免为了展示项目就授予成员管理权限。涉及邀请、取消邀请和权限变更时还应记录操作人及结果，避免重复邀请。当前界面演示只解释上述步骤，不会执行这些写操作。

邀请接口的权限、普通成员角色及 `team_ids` 参数见 [Create an organization invitation](https://docs.github.com/en/rest/orgs/members#create-an-organization-invitation)。接受邀请、有效期与 2FA 条件见 [邀请用户加入组织](https://docs.github.com/en/organizations/managing-membership-in-your-organization/inviting-users-to-join-your-organization)。Enterprise Managed Users 使用 IdP/SCIM 的管理流程，不适用普通个人账号邀请流程。

## 默认权限怎么设置

推荐组织的 Base permissions 先设为 **None**，创建一个普通成员 Team，并只对希望共享的仓库授予 **Read**；项目开发成员再通过各自 Team 获得 Write。邀请时选择对应 Team，就能让成员接受后获得预先配置的访问范围。

如果确实希望所有组织成员都能读取所有组织仓库，可以把 Base permissions 设为 Read，但这会覆盖所有现有和未来成员，影响范围比指定 Team 更大。组织默认权限不作用于 outside collaborators；更高的直接或 Team 授权仍然有效。参见 [设置组织基础权限](https://docs.github.com/en/organizations/managing-access-to-your-organizations-repositories/setting-base-permissions-for-an-organization)。

## 从 Demo 到正式使用

1. 创建 GitHub Organization，确定所有者、成员 Team 和仓库权限；将准备集中管理的团队仓库转入组织。
2. 先安装只有仓库元数据读取权限的 GitHub App，或用受限 fine-grained PAT 验证真实目录。确认组织策略审批与仓库选择范围。
3. 配置服务器的组织名和凭据，验证公开目录及管理员私有目录；正式使用 App 时实现短期安装令牌轮换。
4. 若需要按每个人的 GitHub 权限展示私有目录，继续接入 GitHub 用户授权、真实回调和服务端凭据存储；这与已支持的 Workbench 统一登录分开。当前 SSO 普通成员仍只能看公开仓库，不会因为进入目录而获得 GitHub 组织或仓库权限。
5. 确定成员准入规则后，再独立启用组织管理权限、真实邀请、成员状态确认和 Team 分配。

若以后需要局域网备份，可以让 Forgejo 从 GitHub 单向拉取经过授权的组织仓库，或运行专门的备份任务。目录索引和代码备份需要不同权限，当前实现提供目录索引。

文档中的 GitHub 权限与授权流程依据以上官方文档核对；核对日期：2026-11-11。
