# 01 · 平台基座:认证 / 用户 / RBAC / 审计 / 主题

## 1. 登录与会话

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| P-01 | 登录成功 | `/login` 输入 admin/admin123,点击登录 | 跳转 `/clusters`;顶栏显示用户名 admin;localStorage 存在 accessToken/refreshToken |
| P-02 | 登录失败文案 | 输入 admin/wrongpwd | 提示**中文**「用户名或密码错误」(不是 unauthorized);审计落 deny 记录(username=admin) |
| P-03 | 回车提交 | 密码框内按 Enter | 等同点击登录按钮 |
| P-04 | Token 静默续期 | 登录后等待或触发 401 | 先静默 refresh 一次并重放请求,不弹回登录页;refresh 失败才跳 `/login` |
| P-05 | 退出登录 | 右上角用户菜单 → 退出 | 清凭证、跳登录页;旧 accessToken 后续请求 401 |
| P-06 | 未登录访问受保护路由 | 未登录直接打开 `/users` | 路由守卫跳 `/login` |

## 2. 用户管理(admin 专属,`/users`)

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| P-07 | 创建用户 | 创建 operator 角色用户 op1 | 列表即时出现 op1;返回一次性密码;新密码可登录 |
| P-08 | 禁用用户 | 禁用 op1(输入名称确认) | op1 登录被拒(40100 类文案);审计落 deny |
| P-09 | 重置密码 | 对 op1 重置 | 弹窗展示一次性新密码;旧密码失效;旧会话全部吊销 |
| P-10 | 删除用户 | 删除 op1 | 列表移除;op1 无法登录;审计留痕 |
| P-11 | 保护规则 | 尝试删除 admin(自己) | 后端 40300,前端展示错误,不生效 |
| P-12 | 修改自己密码 | `/users/me/password`(个人菜单) | 新密码登录生效 |
| P-13 | viewer 越权 | 用 viewer 账号访问 /users 或调 POST /users | UI 隐藏入口;直接调 API 返回 40300 |
| P-14 | 时间格式化 | 「创建时间」列 | 相对时间(如 5m),悬停显示完整时间 |

## 3. RBAC 权限点(`GET /api/v1/users/me/permissions`)

| # | 用例 | 预期 |
|---|---|---|
| P-15 | admin | `permissions=["*"]` |
| P-16 | operator | 含 `secrets:read`、`terminal:use` 与全部写权限点;不含用户管理 |
| P-17 | viewer | 只读权限点 + `secrets:read-masked`;**无** `terminal:use`、无写操作 |
| P-18 | viewer Secret | viewer 打开 Secret 详情:值显示 `***`;`?reveal=true` 请求被拒或隐藏入口 |
| P-19 | operator Secret | operator 可解码查看,且每次查看落审计(action=reveal-secret) |
| P-20 | viewer 终端 | viewer 看不到行级「终端」按钮;直接调 terminal WS 返回 403 |

## 4. 审计日志(`/audit`)

| # | 用例 | 预期 |
|---|---|---|
| P-21 | 全量留痕 | 依次执行:登录、创建 ConfigMap、YAML 编辑、删除、Pod 重启、Deployment 伸缩、Secret 解码查看 → 审计列表各有一条,字段齐全 |
| P-22 | 字段正确性 | username=操作者;resourceType(auth/user/configmap/pod/deployment/secret…);action(login/write/delete/restart/scale/create/reveal-secret);cluster/namespace/name 与操作对象一致;result=allow/deny;sourceIp/userAgent 非空 |
| P-23 | 匿名端点 | 登录失败记录含提交的 username、result=deny、action=login、resourceType=auth |
| P-24 | 不可删 | 无删除审计的 API/按钮;筛选(username/cluster/action)与分页可用 |
| P-25 | 时间格式化 | 「时间」列相对时间 + 悬停完整时间 |

## 5. 主题

| # | 用例 | 预期 |
|---|---|---|
| P-26 | 亮暗切换 | 顶栏切换即时生效,全页面(表格/弹窗/终端/日志)配色协调,无刺眼白块 |
| P-27 | 主题记忆 | 切换后刷新页面保持选择;默认暗色 |
