package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/rennzhang/ai-dispatch/internal/config"
	"sort"
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
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	fmt.Fprintln(stdout, "# 模型与入口")
	fmt.Fprintln(stdout, "")
	fmt.Fprintf(stdout, "默认：%s / %s / %s\n", cfg.Defaults.Provider, cfg.Defaults.Surface, cfg.Defaults.Model)
	fmt.Fprintln(stdout, "显式入口和模型优先；模型目录描述支持关系，不触发跨入口降级。")
	fmt.Fprintln(stdout, "用 providers scan 更新本机目录；--dry-run 仅预览。")
	fmt.Fprintln(stdout, "用 models resolve [target] --model <id> --surface cli|app 检查解析，不发模型请求。")
	fmt.Fprintln(stdout, "Codex App 必须指定 codex --surface app；resume 继承已记录渠道。")
	fmt.Fprintln(stdout, "未列出的原生模型ID可显式透传，目录命中不保证账号权限或额度。")
	fmt.Fprintln(stdout, "偏好文档只表达场景建议，不覆盖 config.json 或本次明确输入。")
	keys := make([]string, 0, len(cfg.Models))
	for key := range cfg.Models {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	for _, key := range keys {
		entry := cfg.Models[key]
		paths := []string{}
		for provider, surfaces := range entry.Targets {
			for surface, target := range surfaces {
				paths = append(paths, provider+"/"+surface+" -> "+target.Model)
			}
		}
		sort.Strings(paths)
		fmt.Fprintf(stdout, "- %s: %s\n", key, strings.Join(paths, "; "))
	}
	return nil
}
