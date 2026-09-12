# 03 · 资源浏览与操作:14 类资源 / 新建 / YAML / 伸缩 / 重启 / 终端 / 日志

> 支持的 resource 枚举:`deployments | statefulsets | daemonsets | pods | services | ingresses | configmaps | secrets | pvcs | pvs | nodes | namespaces | crds | {crd-plural}`(另 roles/role-bindings 只读)。
> 统一交互:namespace 筛选(动态拉取)、labelSelector、关键字、分页、WS 增量刷新、YAML 查看/编辑、详情抽屉。

## 1. 列表(逐个资源类型过一遍)

| # | 用例 | 预期 |
|---|---|---|
| R-01 | 14 类资源列表逐个打开 | 全部渲染正常,列(名称/namespace/状态/AGE/labels)合理;空类型显示空态 |
| R-02 | namespace 筛选 | 下拉为**动态**集群 namespace 列表;筛选生效;「全部命名空间」可跨 ns 汇总 |
| R-03 | labelSelector/关键字 | 按 label 过滤生效;关键字对 name 模糊匹配 |
| R-04 | 分页 | el-pagination,total/page-size/current-page 正确;翻页重新请求 |
| R-05 | WS 增量刷新 | 页面显示已订阅;外部 kubectl 改资源(如 scale)→ **列表 6s 内自动更新**,无需手动刷新;删除资源行自动消失 |
| R-06 | 行级 namespace 透传 | 「全部命名空间」视图下,任一行的 YAML/详情/删除/终端**都正常**(不得 not found) |
| R-07 | 时间/单位 | AGE 相对时间;无原始 RFC3339 串;无原始纳核/原始 Ki 内存串 |

## 2. 新建资源

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| R-08 | 新建按钮 | deployments 页点「新建」 | 弹出 YAML 编辑 Dialog,预填该 kind 最小模板(name/namespace/骨架) |
| R-09 | 创建成功 | 改名后提交 | toast + 列表刷新出现新资源;kubectl 可见;审计 action=create |
| R-10 | 已存在 | 同名再创建 | 明确提示 40406「已存在」 |
| R-11 | 参数校验 | 删除 metadata.name 提交 | 前端拦截「metadata.name 不能为空」;namespace 级资源无 namespace 时后端 40001 |
| R-12 | 各类型模板 | configmaps/secrets/pvcs/namespaces 逐个新建 | 模板合理,创建均成功 |
| R-13 | CRD 新建 | CRD 动态资源页新建 | 走同构端点透传创建成功 |

## 3. 详情与 YAML

| # | 用例 | 预期 |
|---|---|---|
| R-14 | 详情 | Deployment/Pod 等详情展示元数据、状态、容器列表(真实 containerStatuses) |
| R-15 | YAML 查看 | `GET .../yaml` 返回完整 YAML;Secret data 默认 `***` |
| R-16 | YAML 编辑 | 修改后保存生效;kubectl 验证;resourceVersion 冲突(外部先改一次)时返回 40406/409,前端提示冲突并可重新拉取 |
| R-17 | 删除 | 输入名称确认;删除后行消失;kubectl 验证;审计 |

## 4. 工作负载操作

| # | 用例 | 预期 |
|---|---|---|
| R-18 | Deployment 伸缩 | 行级「伸缩」→ 输入副本数 → 确认 → 实际副本变化;WS 推送后列表即时更新 |
| R-19 | Deployment 重启 | 行级「重启」→ 输入名称确认 → **HTTP 200**,restartedAt 注解打上,Pod 滚动重建 |
| R-20 | StatefulSet 伸缩 | 仅 StatefulSet 有伸缩(DaemonSet 无);生效 |
| R-21 | Pod 重启(删建) | Pod 行「重启」→ 旧 Pod 删除重建;审计 |

## 5. 终端 exec(重点,行级直达)

| # | 用例 | 步骤 | 预期 |
|---|---|---|---|
| R-22 | Pod 行级终端 | pods 列表行点「终端」(**不进详情**) | 直接弹出终端 Dialog,连上容器 shell,提示符可见 |
| R-23 | 工作负载行级终端 | deployments 行点「终端」 | 自动定位一个 Running Pod 并连上;多个容器时顶部下拉可切换;无 Running Pod 时友好提示 |
| R-24 | 终端交互 | 执行 `ls`、`env`、输出大量文本、窗口 resize | 回显正常(<200ms);resize 不乱码;`exit` 结束显示 code |
| R-25 | 断线重连 | 杀掉目标 Pod | 终端显示会话结束;可点「重启会话」重连 |
| R-26 | 权限 | viewer 无「终端」按钮;API 直调 403 | operator/admin 可用 |
| R-27 | 审计 | 终端会话开启/关闭 | 审计记录含 user/cluster/pod/时长(命令内容不记录) |
| R-28 | 多标签 | PodDetailDrawer 内终端 tab 同样可用(与行级入口行为一致) | — |

## 6. Pod 日志

| # | 用例 | 预期 |
|---|---|---|
| R-29 | 历史日志 | 容器下拉切换、tailLines、previous(崩溃容器);内容与 kubectl logs 一致 |
| R-30 | 实时日志流 | follow 模式持续输出;<500ms 延迟;关键字过滤高亮;下载为文件 |
| R-31 | 行级入口 | pods 行「日志」或详情抽屉内打开均正常(namespace 透传) |
