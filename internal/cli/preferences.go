package cli

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"

	"github.com/rennzhang/ai-dispatch/internal/config"
)

func preferences(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch preferences path")
		fmt.Fprintln(stdout, "  ai-dispatch preferences show   # create the default file if missing, then print it")
		fmt.Fprintln(stdout, "  ai-dispatch preferences open   # create the default file if missing, then open it")
		return 0
	}
	path := config.PreferencesPath()
	switch argv[0] {
	case "path":
		fmt.Fprintln(stdout, path)
		return 0
	case "show":
		if _, err := config.EnsurePreferences(); err != nil {
			fmt.Fprintln(stderr, "ai-dispatch preferences:", err)
			return 1
		}
		data, err := os.ReadFile(path)
		if err != nil {
			fmt.Fprintln(stderr, "ai-dispatch preferences:", err)
			return 1
		}
		fmt.Fprint(stdout, string(data))
		return 0
	case "open":
		if _, err := config.EnsurePreferences(); err != nil {
			fmt.Fprintln(stderr, "ai-dispatch preferences:", err)
			return 1
		}
		if err := openWithDefaultApp(path); err != nil {
			fmt.Fprintln(stderr, "ai-dispatch preferences open:", err)
			return 1
		}
		fmt.Fprintln(stdout, path)
		return 0
	default:
		fmt.Fprintf(stderr, "ai-dispatch preferences: unknown subcommand %q\n", argv[0])
		return 2
	}
}

func openWithDefaultApp(path string) error {
	var cmd *exec.Cmd
	switch runtime.GOOS {
	case "darwin":
		cmd = exec.Command("open", path)
	case "windows":
		cmd = exec.Command("rundll32", "url.dll,FileProtocolHandler", path)
	default:
		cmd = exec.Command("xdg-open", path)
	}
	return cmd.Run()
}
