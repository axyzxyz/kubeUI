// kubeUI Agent 反连客户端入口:读取命令行参数或环境变量并启动受控重连循环,仅做装配。
//
// 用法(参数优先,缺省回退环境变量):
//
//	kubeui-agent -server https://kubeUI.example.com -token <enroll-token>
//	kubeui-agent -server https://kubeUI.example.com -token <token> -insecure
//
// 命令行参数:
//   - -server    平台基础地址(必填;回退 KUBEUI_SERVER_URL)
//   - -token     Enrollment Token(必填,不打印;回退 KUBEUI_ENROLL_TOKEN)
//   - -version   可选,上报版本(回退 KUBEUI_AGENT_VERSION,默认 dev)
//   - -insecure  跳过平台 TLS 证书校验(回退 KUBEUI_AGENT_INSECURE=1/true)
//
// 环境变量等价形式(K8s 部署 YAML 使用):KUBEUI_SERVER_URL / KUBEUI_ENROLL_TOKEN /
// KUBEUI_AGENT_VERSION / KUBEUI_AGENT_INSECURE。
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/axyzxyz/kubeui/backend/internal/agent"
	"github.com/axyzxyz/kubeui/backend/internal/pkg/logx"
)

func main() {
	os.Exit(run())
}

func run() int {
	serverURL := flag.String("server", os.Getenv("KUBEUI_SERVER_URL"),
		"平台基础地址,如 https://kubeUI.example.com(必填;缺省读 KUBEUI_SERVER_URL)")
	token := flag.String("token", os.Getenv("KUBEUI_ENROLL_TOKEN"),
		"Enrollment Token(必填,不打印;缺省读 KUBEUI_ENROLL_TOKEN)")
	agentVersion := flag.String("version", envOr("KUBEUI_AGENT_VERSION", "dev"),
		"上报版本(可选)")
	insecure := flag.Bool("insecure",
		os.Getenv("KUBEUI_AGENT_INSECURE") == "1" || os.Getenv("KUBEUI_AGENT_INSECURE") == "true",
		"跳过平台 TLS 证书校验(仅测试环境)")
	flag.Parse()

	if *serverURL == "" || *token == "" {
		fmt.Fprintln(os.Stderr, "-server and -token are required (or set KUBEUI_SERVER_URL / KUBEUI_ENROLL_TOKEN)")
		flag.Usage()
		return 2
	}

	logx.Init("info", "json")
	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	client := agent.NewClient(agent.ClientConfig{
		ServerURL:          *serverURL,
		Token:              *token,
		AgentVersion:       *agentVersion,
		InsecureSkipVerify: *insecure,
	})
	logx.Info(ctx, "agent starting")
	if err := client.Run(ctx); err != nil && ctx.Err() == nil {
		logx.Error(ctx, "agent exited with error", "err", err)
		return 1
	}
	logx.Info(ctx, "agent stopped")
	return 0
}

func envOr(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}
