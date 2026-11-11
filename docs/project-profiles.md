# 项目资料维护

代码目录同时包含两类信息：GitHub 提供的仓库基础资料，以及项目维护者确认的功能、场景、成果和开发指引。后者由本服务启动时读取的 JSON 配置维护，不会根据仓库名称推测，也不会自动运行成员仓库中的代码。

## 使用方式

1. 复制 [`examples/profiles.example.json`](../examples/profiles.example.json)，以 `owner/repository` 为键填写希望展示的项目。
2. 根据项目 README 和可复现结果填写内容，提交给项目维护者审阅。不要包含密码、令牌、客户数据或未获授权的内部附件。
3. 将文件保存到服务端受控目录，在真实组织启动参数中增加 `-profiles /path/to/profiles.json`，更新文件后重启目录服务。

示例配置的顶层结构是 `{"version":1,"projects":{"example-team/example-project":{...}}}`。名称匹配忽略大小写；仓库转移或重命名后须更新键。真实模式默认不使用内嵌演示说明；无匹配项返回 `profile: null`。填写资料不能把 GitHub 未返回的仓库加入列表，也不能扩大访问权限；私有仓库及其说明仅对目录管理员返回。

演示模式内置六份资料；这些内容用于体验界面。配置不需要额外的 GitHub `Contents` 权限。若以后从仓库中的 README 或说明文件自动获取内容，则需要另行设计读取权限、内容校验与更新流程。

## 填写哪些内容

| 字段 | 应回答的问题 |
| --- | --- |
| `kind`、`title`、`summary` | 是团队项目还是参考项目？它解决什么问题？ |
| `scenarios` | 什么人，在什么业务或实验场景下适合使用？ |
| `capabilities` | 已实现的具体功能是什么？避免把规划写成已实现功能。 |
| `outputs` | 输入经过处理后得到什么文件、界面、API 响应或记录？写明输出形式与用途。 |
| `screenshots` | 有哪些已脱敏的结果示例？注明示例数据、截图来源和适用版本。 |
| `development.requirements` | 语言版本、依赖、服务、硬件和配置前提是什么？ |
| `development.quick_start` | 从干净环境到第一个结果的步骤是什么？禁止把真实令牌写在命令中。 |
| `development.entry_points` | 核心源码、接口、配置或测试在哪个文件？每个入口有什么职责？ |
| `development.extension_points` | 新成员通常从哪些模块扩展，哪些边界需要保留？ |
| `development.validation` | 怎样复现结果或运行测试？区分验证方法和已完成的验证记录。 |
| `development.limitations` | 哪些场景不支持，尚有哪些风险或后续工作？ |
| `docs` | 项目 README、开发文档或验证记录的 GitHub 链接在哪里？ |
| `maintainer`、`maturity` | 联系谁？项目处于探索、可试用还是维护阶段？未知就明确待确认。 |
| `source_note`、`license_note` | 说明依据哪个版本？复用时需要遵守哪些许可证和第三方要求？ |

`kind` 使用 `team` 或 `reference`。`outputs` 为 `{title, description}` 数组；`entry_points` 为 `{path, description}` 数组；`docs` 为 `{label, url}` 数组；`screenshots` 为 `{src, alt, caption}` 数组。开发相关的其余字段为字符串数组。

成果应优先提供可复现的例子，例如「借还登记形成审批与归还记录」「上传新版本后可下载历史文件」。如要宣称耗时、准确率、并发或成本改善，需同时提供测试输入、环境、版本和验证记录；没有依据时不要填写效果数字。

## 图片与数据边界

当前 `screenshots.src` 只支持 `/images/` 下的本地图片，文档链接只支持 HTTPS GitHub 地址；不执行 HTML、脚本或外部嵌入。配置文件本身不由静态文件服务公开。文件应放在 `web/` 之外，并由部署账号限制读取。

**静态图片可以直接访问。** 本次内置的三张成果图均为可展示的脱敏演示资源，来源见 [`web/images/SOURCE.md`](../web/images/SOURCE.md)。真实私有项目的截图和附件不得直接放入 `web/`；需要展示这类资料时，应先实现按仓库权限鉴权的媒体存储与访问接口。目录管理员登录不意味着静态目录获得了权限保护。

## 随代码同步维护

功能或运行方式改变的 PR 应同步更新 README 和目录资料；结果截图标注版本，过时资料应及时撤下或标记。目录只保存说明与链接，GitHub 中对应版本的源码、测试和发布记录仍是复现与二次开发的依据。
