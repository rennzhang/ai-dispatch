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
	models, err := routing.RegistryModels()
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
	fmt.Fprintln(stdout, "`~/.ai-dispatch/config.json` 的 `models` 字段优先于内置 registry，用于维护用户认可的短名到 provider/model 候选链。")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "## Provider")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "- `codex`：调用 `codex exec`。")
	fmt.Fprintln(stdout, "- `claude`：调用 `claude -p`。")
	fmt.Fprintln(stdout, "- `opencode`：调用 `opencode run`。")
	fmt.Fprintln(stdout, "- `antigravity`：调用 `agy --print`。")
	fmt.Fprintln(stdout, "")
	fmt.Fprintln(stdout, "## Built-in registry targets")
	fmt.Fprintln(stdout, "")
	if len(models) == 0 {
		fmt.Fprintln(stdout, "当前 registry 没有可展示的 target。")
		return nil
	}
	for _, model := range models {
		aliases := ""
		if len(model.Aliases) > 0 {
			aliases = "；aliases: `" + strings.Join(model.Aliases, "`, `") + "`"
		}
		fmt.Fprintf(stdout, "- `%s` -> provider `%s`, model `%s`%s\n", model.Key, model.DispatchRunner, firstNonEmpty(model.DispatchModel, model.ActualModelID), aliases)
	}
	return nil
}
