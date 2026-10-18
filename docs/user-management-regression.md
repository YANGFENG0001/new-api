# 用户批量操作与管控界面修复验证（2026-10-18）

## 修复范围

- 批量菜单的标签必须置于 `DropdownMenuGroup` 内，修复点击即触发 Base UI #31 错误页。
- 批量预览参数改变后禁止继续执行；二次验证时隐藏原弹窗；重试携带绑定原操作、失败用户集合和批次哈希的一次性凭据。
- 修正任务分页参数、异步任务启动提示、重试累计成功数，以及已删除/不存在用户的预览跳过计数。
- 策略、名单、用户覆盖复用业务弹窗；搜索分页勾选用户，跨页保留选择；成员部分失败保留待重试用户并展示原因。
- 修复逗号分隔字段输入；刷新或保存后保持当前页签；限制摘要、状态、说明、权限和错误随界面语言显示。
- 无数据库结构或迁移变更，无新增依赖，不改现有权限边界。

## 自动验证

在仓库根目录运行：

```powershell
go test ./service/userbatch ./service/usercontrol ./service/authz -count=1 -timeout=120s
go test ./controller -run '^TestUserBatch' -v -count=1 -timeout=120s
```

后一个命令分别在下列真实数据库执行，三个用例均通过：

| 环境变量 `TEST_SECURITY_DIALECT` | 数据库实测版本 | 连接变量 |
| --- | --- | --- |
| sqlite（默认） | SQLite 3.50.4 | 自动创建临时文件 |
| mysql | MySQL 8.0.46 | `TEST_MYSQL_DSN` |
| postgres | PostgreSQL 16.15 | `TEST_POSTGRES_DSN` |

MySQL/PostgreSQL 均为回环地址上的一次性容器；测试为主库和审计库分别创建隔离数据库。未连接生产库。测试覆盖无凭据拒绝、参数变化导致预览失效、凭据过期/消费后重放拒绝、提额幂等、重试保留既往成功结果、缺失及已删除用户的跳过统计。

在 `web/` 运行：

```powershell
bun run typecheck
bun run build
bun run test -- src/lib/server-error-message.test.ts src/lib/__tests__/server-error-notifications.test.ts src/features/user-control/__tests__ src/features/users/components/__tests__/batch-actions.test.tsx src/features/users/components/__tests__/batch-jobs.test.tsx src/i18n/__tests__ src/components/ui/__tests__/dialog-i18n.test.tsx
```

相关交互、布局、翻译及错误呈现回归测试通过；变更 TS/TSX 文件的 oxlint、oxfmt 检查及 `git diff --check` 通过。七种语言的功能文案、动态键和占位符完成定向审计，原有 reasoning 清洗开关翻译保留。

## 浏览器验收

使用本地隔离 SQLite 数据和测试账号，生产构建中验证：

1. 勾选普通用户，批量禁用菜单、预览、密码二次验证、执行成功；列表状态更新为已禁用。
2. 新建策略弹窗正文独立滚动，底部操作按钮可见；保存两条自定义时间段和多个模型。
3. 名单成员支持用户列表、搜索和分页；跨两页选择两位用户，添加后结果显示成功 2、跳过 0，当前成员列表展示两人。
4. 用户覆盖先搜索并勾选用户，再进入中文覆盖编辑器。

截图和编译产物不纳入 Git。上述验收不是生产部署，发布后仍需重新构建并部署服务。

## 安全复核范围

参考 OWASP ASVS 5.0.0 的认证/会话方向及以下 Cheat Sheets，核对本次批量执行和重试没有绕过既有服务端二次验证、操作上下文绑定、有效期和一次性消费控制：

- Authentication Cheat Sheet：`https://cheatsheetseries.owasp.org/cheatsheets/Authentication_Cheat_Sheet.html`
- Session Management Cheat Sheet：`https://cheatsheetseries.owasp.org/cheatsheets/Session_Management_Cheat_Sheet.html`
- Transaction Authorization Cheat Sheet：`https://cheatsheetseries.owasp.org/cheatsheets/Transaction_Authorization_Cheat_Sheet.html`

本次未重写身份验证或会话机制，也未执行全站 ASVS 审计，不声明全站合规。
