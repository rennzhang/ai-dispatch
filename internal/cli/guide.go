package cli

import (
	"fmt"
	"io"
)

func guide(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch guide models")
		return 0
	}
	switch argv[0] {
	case "models":
		data, err := skillAssets.ReadFile("skill_assets/ai-dispatch/references/model-routing.md")
		if err != nil {
			fmt.Fprintln(stderr, "ai-dispatch guide models:", err)
			return 1
		}
		fmt.Fprint(stdout, string(data))
		return 0
	default:
		fmt.Fprintf(stderr, "ai-dispatch guide: unknown guide %q\n", argv[0])
		return 2
	}
}
