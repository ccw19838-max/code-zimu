# 团队代码模板使用说明

这些模板配套建议规则 v0.1，供团队讨论、调整和采纳。复制文件或在网页阅读规则，不代表 GitHub 已启用保护分支、必需审批或检查。

先将所有 `【待填写】`、`【项目……】` 和 `YOUR_ORG` 等占位内容替换为真实信息，再提交 PR。没有完成的功能、没有执行的验证和没有产生的截图，应明确标记为未完成，不要填写虚构结果。

| 模板 | 建议放入项目的位置 | 使用前需要确认 |
| --- | --- | --- |
| `README.template.md` | 仓库根目录 `README.md` | 功能、启动步骤、真实结果、维护人和许可证 |
| `CONTRIBUTING.template.md` | 仓库根目录 `CONTRIBUTING.md` | 现有分支、验证命令、评审人和异常处理流程 |
| `PULL_REQUEST_TEMPLATE.md` | `.github/PULL_REQUEST_TEMPLATE.md` | 仓库的验证和部署要求 |
| `CODEOWNERS.example` | `.github/CODEOWNERS` | 替换并启用实际存在、对仓库有 Write 权限的用户或 Team |
| `RELEASE_CHECKLIST.md` | `docs/RELEASE_CHECKLIST.md`，也可复制到每次发布的 Issue | 版本、制品、数据迁移、回滚和负责人 |

`CODEOWNERS.example` 中的规则全部为注释，避免示例账号产生错误分配。启用前填写真实规则；同一路径匹配多行时，最后匹配的规则生效。仅添加 CODEOWNERS 不会强制审批，还需由有权限的管理员核实组织计划、仓库类型及规则功能，再配置所需的 ruleset 或保护分支规则。

模板没有替项目选择许可证。先确认团队代码的权利归属和第三方许可，再填写 README 的许可证说明并添加相应文件。

网页下载副本位于项目的 `web/templates/`，应与本目录对应模板保持一致。
