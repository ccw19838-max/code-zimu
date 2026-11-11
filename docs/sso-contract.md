# Workbench 首方登录协议 v1

本文件随各应用保存，是单仓库环境中的接入约定。导航负责账号、密码、停用、应用授权和中央会话；应用通过 HTTP 验证身份，保留自己的数据和业务权限。无需共享源码目录、数据库或浏览器 Cookie。本协议只用于受控的首方应用，不是通用 OIDC。

## 地址与配置

| 配置 | 用途 |
| --- | --- |
| `WORKBENCH_PORTAL_URL` | 浏览器可达的导航 origin，例如 `https://nav.example.com` |
| `WORKBENCH_PUBLIC_URL` | 浏览器可达的本应用地址；独立模式为自己的 origin，网关模式为导航 origin 加 `/apps/<id>` |
| `WORKBENCH_INTERNAL_URL` | 可选的导航服务端地址，默认等于 PORTAL_URL；请求仍使用导航公共 Host |
| `WORKBENCH_CLIENT_SECRET` | 本应用独有的服务凭据，至少 32 字符，与导航登记一致 |
| `WORKBENCH_GATEWAY_SECRET` | 仅同机网关模式设置；至少 32 字符，与导航登记一致 |

以上变量全部省略时使用应用原有的独立开发方式。部分配置缺失必须拒绝启动，不能静默退回本地登录。`assets`、`xrplan`、`code-zimu` 是各自固定的应用 ID；财务与资产使用同一个 `assets` 授权和数据库。

独立 origin 模式不设置网关密钥，导航条目不配置 upstream。可以部署在不同机器或环境。带 `/apps/<id>` 的模式必须使用同机回环网关，不能为了跨机器部署而取消网关密钥或回环检查。code-zimu 始终使用自己的 origin、端口和前端。

生产浏览器入口及跨机器认证链路使用 HTTPS。服务端必须可访问导航 INTERNAL_URL；浏览器必须可访问导航和各应用 PUBLIC_URL。HTTP origin 的协议、主机、端口必须完全一致，不能混用 localhost、IP 和域名。无需开放跨源 CORS，也不要共享 Cookie Domain。

## 登录与身份

1. 应用 `GET /auth/sso/login` 创建 32 字节随机 state，保存为 5 分钟的 HttpOnly、SameSite=Lax Cookie，跳转导航 `/sso/authorize?app=<id>&state=...`。
2. 导航未登录时先登录，再检查该用户的应用授权，只向登记的固定 callback 返回 code 和 state。禁止浏览器提交任意 redirect_uri。code 有效 60 秒且只能消费一次。
3. 应用核对 state，使用 HTTP Basic `<app id>:<service secret>` 调用 `POST /api/sso/token`，JSON 为 `{"code":"..."}`，取得不透明应用 token。
4. 每个受保护请求使用同样的服务认证调用 `POST /api/sso/introspect`，JSON 为 `{"token":"..."}`，返回 `subject/name/account/role`。token 只能用于原应用；后端不得跟随认证服务的 HTTP 重定向。
5. `subject` 是中央稳定账号，`account` 绑定既有业务 ID，`role` 为该应用的 `user` 或 `admin`；导航管理员不自动成为业务管理员。不要覆盖原上传者、历史 ID、资产归属或项目 ACL。本地停用、毕业只读等业务限制继续执行。

中央会话最长 8 小时，重启失效。修改密码、用户状态或授权会使该用户旧会话版本失效。每请求在线校验，不能用本地密码、旧 Cookie、浏览器身份头或过期缓存绕过。

应用 Cookie 名为 `wb_<id>_session`，仅本应用路径、HttpOnly、SameSite=Lax，HTTPS 下 Secure；不向 JavaScript 返回 token 或服务密钥。登录的 `return` 只接受本应用路径下的站内相对地址，保留查询和片段；拒绝外站、反斜线、控制字符及 SSO 循环路径。

## 浏览器会话与退出

| 应用接口 | 约定 |
| --- | --- |
| `GET /auth/sso/info` | 返回 `enabled/version/portal_url/app/base_path`，不含凭据 |
| `GET /auth/sso/session` | 在线验权后返回 `user/csrf/portal_url/version`；user 含 username、account、name、role |
| `POST /api/logout` | 必须带本应用准确 Origin 和上述会话的 `X-CSRF-Token`；应用经服务端调用中央 `/api/sso/logout` 后清除应用 Cookie |

CSRF 值与本应用会话绑定，不能用中央页面或另一个应用的 CSRF 值替代。所有应用的“全局退出”均撤销关联中央会话和其他应用的 token，包括独立 origin 模式。`/api/sso/revoke` 保留为显式撤销单应用 token 的服务接口，不用于全局退出按钮。

同源标签页可用 storage 通知退出。不同 origin 无法共享该通知，页面切回前台/获得焦点时必须重新验权，前台每 15 秒检查；无论界面是否刷新，下一次受保护服务端请求都必须拒绝已失效会话。避免迟到请求恢复旧身份或旧私有内容。

API 未登录或已撤销返回 401 JSON，包含正确应用前缀的 `login_url`；不足权限返回 403。认证网络故障返回 503，不能当作成功或回退身份。浏览器页面可在 401 时跳转登录，503 应保留故障信息，不产生重定向循环。健康检查不依赖用户登录；内部指标需要服务凭据。

## 独立发布与验证

导航公开 `GET /.well-known/workbench-sso`，返回协议版本和固定认证端点。新版适配器的服务请求带 `X-Workbench-SSO-Version: 1`；导航允许旧版省略此头，显式不支持的版本返回 400。v1 的新增字段和端点保持向后兼容；删除、改名或改变身份含义必须新建协议版本并保留迁移窗口。

本次为浏览器退出补齐了 Origin 和 CSRF 校验：旧的直接调用 `/api/logout` 的客户端需要先取本应用 `/auth/sso/session`，不能继续无 CSRF 调用。新版应用前端和适配器须一起构建发布；现有服务端 code 交换与 introspect 格式保持兼容。

每个应用仓库自带 `internal/workbenchsso` 及其契约测试（XRplan 位于 platform 下），不需要父目录的其他仓库。适配器和契约测试保持一致；修改时先跑本仓库单测，再用独立构建的二进制做真实联调。不能仅因 mock 测试通过就宣称所有线上服务互通。

code-zimu 的 SSO 只代表代码目录身份：普通成员只看公开仓库，显式授予该应用 admin 才可刷新目录及查看服务令牌可见的私有元数据。GitHub 的组织成员、源码访问权限和用户 OAuth 不由 Workbench 登录自动授予。小智和沐心当前是外部链接，未实现此协议，不可宣称已有统一登录。
