package handler

import (
	"github.com/gin-gonic/gin"

	"github.com/v911/backend/internal/api/response"
	"github.com/v911/backend/internal/pkg/errcode"
	"github.com/v911/backend/internal/service"
)

// registerClusterRequest 是集群注册请求体。
// Kubeconfig 为 kubeconfig YAML 原文,只进入加密存储,绝不回显。
type registerClusterRequest struct {
	Name        string `json:"name" binding:"required"`
	Kubeconfig  string `json:"kubeconfig" binding:"required"`
	ContextName string `json:"contextName"`
	Description string `json:"description"`
	// AccessMode 接入方式:direct(默认) | agent(反连,注册后需在目标集群部署 Agent)。
	AccessMode string `json:"accessMode"`
}

// rotateKubeconfigRequest 是 kubeconfig 轮转请求体。
type rotateKubeconfigRequest struct {
	Kubeconfig  string `json:"kubeconfig" binding:"required"`
	ContextName string `json:"contextName"`
}

// GET /api/v1/clusters
func listClusters(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		items, err := d.Clusters.List(c.Request.Context())
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, gin.H{"items": items, "total": len(items)})
	}
}

// POST /api/v1/clusters
func registerCluster(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req registerClusterRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "name and kubeconfig are required"))
			return
		}
		info, err := d.Clusters.Register(c.Request.Context(), service.RegisterRequest{
			Name:        req.Name,
			Kubeconfig:  req.Kubeconfig,
			ContextName: req.ContextName,
			Description: req.Description,
			AccessMode:  req.AccessMode,
		})
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, info)
	}
}

// GET /api/v1/clusters/:cluster/status
func clusterStatus(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		st, err := d.Clusters.Status(c.Request.Context(), c.Param("cluster"))
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, st)
	}
}

// DELETE /api/v1/clusters/:cluster(危险操作,审计由中间件统一采集)
func deleteCluster(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		if err := d.Clusters.Delete(c.Request.Context(), c.Param("cluster")); err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, nil)
	}
}

// PUT /api/v1/clusters/:cluster/kubeconfig
//
// TODO(next-wave #1): 平滑热更新由 ClusterManager 完成(重建 clientset/Dialer,
// 期间 Degraded);当前整体覆盖加密存储并重置状态。
func rotateKubeconfig(d Deps) gin.HandlerFunc {
	return func(c *gin.Context) {
		var req rotateKubeconfigRequest
		if err := c.ShouldBindJSON(&req); err != nil {
			response.Fail(c, errcode.New(errcode.ParamInvalid, "kubeconfig is required"))
			return
		}
		info, err := d.Clusters.RotateKubeconfig(c.Request.Context(), c.Param("cluster"), req.Kubeconfig, req.ContextName)
		if err != nil {
			response.Fail(c, err)
			return
		}
		response.OK(c, info)
	}
}
