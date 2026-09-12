// Package k8s 实现多集群 K8s 访问核心:ClusterManager 持有每个集群的
// ClusterRuntime(rest.Config、clientset、dynamic client、缓存 RESTMapper、
// InformerPool),并提供健康检查状态机与懒启动 informer 池。
//
// 生命周期(01-architecture §3):
//
//	Register ──► Ready ──► Degraded(连续 3 次探活失败)──► Reconnecting(连续 10 次,指数退避 1s→60s)
//	                   ▲                                            │
//	                   └──────────────── 恢复 ◄────────────────────┘
//
// 所有 goroutine(健康检查循环、informer、空闲回收器)均有明确退出路径:
// Unregister 先停 goroutine 等待退出,再从注册表移除。
//
// Agent 隧道预留:集群为 Agent 接入模式时,由下一波(agent 反连)通过
// Manager.SetDialer 注入 TunnelDialer,构造 rest.Config 时经 rest.Config.Dial
// 挂载,client-go 全部能力零改动经隧道工作(01-architecture §4.3)。
package k8s
