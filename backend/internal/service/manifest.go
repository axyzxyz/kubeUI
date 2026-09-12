package service

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"text/template"

	"github.com/v911/backend/internal/pkg/errcode"
)

// Agent manifest 渲染参数与默认值。
const (
	// ManifestDefaultImage Agent 镜像,可用 V911_AGENT_IMAGE 覆盖。
	ManifestDefaultImage = "ghcr.io/v911/agent:latest"
	// manifestNamespace Agent 部署命名空间。
	manifestNamespace = "v911-system"
)

// manifestTemplate 渲染 kubectl apply -f - 多文档 YAML:Deployment + 最小
// 权限 RBAC。Agent 只与平台建立反向隧道并透传字节,不调用 K8s API,
// 因此 Role 规则为空(仅保留 ServiceAccount 绑定骨架)。
const manifestTemplate = `# v911 Agent 反连部署清单(集群: {{.Cluster}})
# 用法: kubectl apply -f manifest.yaml
apiVersion: v1
kind: Namespace
metadata:
  name: {{.Namespace}}
---
apiVersion: v1
kind: ServiceAccount
metadata:
  name: v911-agent
  namespace: {{.Namespace}}
---
# 最小权限:Agent 仅做隧道透传,不访问本集群 K8s API,规则为空。
apiVersion: rbac.authorization.k8s.io/v1
kind: Role
metadata:
  name: v911-agent-minimal
  namespace: {{.Namespace}}
rules: []
---
apiVersion: rbac.authorization.k8s.io/v1
kind: RoleBinding
metadata:
  name: v911-agent-minimal
  namespace: {{.Namespace}}
roleRef:
  apiGroup: rbac.authorization.k8s.io
  kind: Role
  name: v911-agent-minimal
subjects:
  - kind: ServiceAccount
    name: v911-agent
    namespace: {{.Namespace}}
---
apiVersion: apps/v1
kind: Deployment
metadata:
  name: v911-agent
  namespace: {{.Namespace}}
  labels:
    app.kubernetes.io/name: v911-agent
    app.kubernetes.io/part-of: v911
spec:
  replicas: 1
  selector:
    matchLabels:
      app.kubernetes.io/name: v911-agent
  template:
    metadata:
      labels:
        app.kubernetes.io/name: v911-agent
    spec:
      serviceAccountName: v911-agent
      containers:
        - name: agent
          image: {{.Image}}
          imagePullPolicy: IfNotPresent
          env:
            - name: V911_SERVER_URL
              value: "{{.ServerURL}}"
            - name: V911_ENROLL_TOKEN
              value: "{{.Token}}"
          resources:
            requests:
              cpu: 10m
              memory: 16Mi
            limits:
              cpu: 200m
              memory: 64Mi
`

// manifestData 是模板渲染参数。
type manifestData struct {
	Cluster   string
	Namespace string
	Image     string
	ServerURL string
	Token     string
}

// RenderAgentManifest 渲染指定集群的 Agent 部署 YAML;token 必须有效
// (仅校验不消费)。渲染结果包含 token 原文,禁止落日志。
func (s *EnrollService) RenderAgentManifest(ctx context.Context, token, serverURL string) (string, error) {
	cluster, err := s.Check(ctx, token)
	if err != nil {
		return "", err
	}
	if serverURL == "" {
		return "", errcode.New(errcode.ParamInvalid, "serverUrl is not configured")
	}
	image := os.Getenv("V911_AGENT_IMAGE")
	if image == "" {
		image = ManifestDefaultImage
	}
	tmpl, err := template.New("agent-manifest").Parse(manifestTemplate)
	if err != nil {
		return "", fmt.Errorf("parse agent manifest template: %w", err)
	}
	var buf bytes.Buffer
	if err := tmpl.Execute(&buf, manifestData{
		Cluster:   cluster,
		Namespace: manifestNamespace,
		Image:     image,
		ServerURL: serverURL,
		Token:     token,
	}); err != nil {
		return "", fmt.Errorf("render agent manifest: %w", err)
	}
	return buf.String(), nil
}
