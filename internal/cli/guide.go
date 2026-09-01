package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/routing"
)

func guide(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch guide models")
		return 0
	}
	switch argv[0] {
	case "models":
		if err := printModelGuide(stdout); err != nil {
			fmt.Fprintln(stderr, "ai-dispatch guide models:", err)
			return 1
		}
		return 0
	default:
		fmt.Fprintf(stderr, "ai-dispatch guide: unknown guide %q\n", argv[0])
		return 2
	}
}

func printModelGuide(stdout io.Writer) error {
	models, err := routing.ConfiguredModels()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "# 模型指南")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "用户偏好读取：")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "```bash")
	fmt.Fprintln(stdout, "ai-dispatch preferences path")
	fmt.Fprintln(stdout, "ai-dispatch preferences show")
	fmt.Fprintln(stdout, "```")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "真实路由检查：")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "```bash")
	fmt.Fprintln(stdout, "ai-dispatch models")
	fmt.Fprintln(stdout, "ai-dispatch models resolve <target> --format json")
	fmt.Fprintln(stdout, "```")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "请求的 target 只是意图，真实执行结果必须看返回 JSON 里的 `provider_used`、`model_used`、`requested_target`、`route_trace`、`degraded`、`degrade_reason`。")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "`~/.ai-dispatch/config.json` 的 `models` 是唯一可执行短名路由。config 里没有的短名会直接失败。")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "## Provider")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "- `codex`：调用 `codex exec`。")
	fmt.Fprintln(stdout, "- `claude`：调用 `claude -p`。")
	fmt.Fprintln(stdout, "- `opencode`：调用 `opencode run`。")
	fmt.Fprintln(stdout, "- `antigravity`：调用 `agy --print`。")
	fmt.Fprintln(stdout, "- `grok`：调用 Grok CLI。")
	fmt.Fprintln(stdout, "- `cursor`：调用 `cursor-agent`。")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "## Configured short names")
	fmt.Fprintln(stdout, "")
	if len(models) == 0 {
		fmt.Fprintln(stdout, "当前 config.json 没有 models。把确认能用的短名写进 ~/.ai-dispatch/config.json。")
		return nil
	}
	for _, model := range models {
		parts := make([]string, 0, len(model.Candidates))
		for _, candidate := range model.Candidates {
			parts = append(parts, candidate.Provider+"/"+candidate.Model)
		}
		fmt.Fprintf(stdout, "- %s -> %s\n", model.Key, strings.Join(parts, "; "))
	}
	return nil
}
