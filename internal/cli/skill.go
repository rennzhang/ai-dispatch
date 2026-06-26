package cli

import (
	"embed"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

//go:embed skill_assets/ai-dispatch
var skillAssets embed.FS

func skill(argv []string, stdout io.Writer, stderr io.Writer) int {
	if len(argv) == 0 || argv[0] == "--help" || argv[0] == "-h" || argv[0] == "help" {
		fmt.Fprintln(stdout, "Usage:")
		fmt.Fprintln(stdout, "  ai-dispatch skill install [--target codex|claude|all] [--dir <skills-dir>]")
		return 0
	}
	switch argv[0] {
	case "install":
		return skillInstall(argv[1:], stdout, stderr)
	default:
		fmt.Fprintf(stderr, "ai-dispatch skill: unknown subcommand %q\n", argv[0])
		return 2
	}
}

func skillInstall(argv []string, stdout io.Writer, stderr io.Writer) int {
	target := "all"
	customDir := ""
	for i := 0; i < len(argv); i++ {
		switch argv[i] {
		case "--target":
			if i+1 >= len(argv) {
				fmt.Fprintln(stderr, "ai-dispatch skill install: --target requires a value")
				return 2
			}
			target = argv[i+1]
			i++
		case "--dir":
			if i+1 >= len(argv) {
				fmt.Fprintln(stderr, "ai-dispatch skill install: --dir requires a value")
				return 2
			}
			customDir = argv[i+1]
			i++
		case "--help", "-h":
			fmt.Fprintln(stdout, "Usage: ai-dispatch skill install [--target codex|claude|all] [--dir <skills-dir>]")
			return 0
		default:
			fmt.Fprintf(stderr, "ai-dispatch skill install: unknown flag %q\n", argv[i])
			return 2
		}
	}
	dirs, err := skillInstallDirs(target, customDir)
	if err != nil {
		fmt.Fprintln(stderr, "ai-dispatch skill install:", err)
		return 2
	}
	installed := []string{}
	for _, dir := range dirs {
		dest := filepath.Join(dir, "ai-dispatch")
		if err := writeEmbeddedSkill(dest); err != nil {
			fmt.Fprintln(stderr, "ai-dispatch skill install:", err)
			return 1
		}
		installed = append(installed, dest)
	}
	for _, dir := range installed {
		fmt.Fprintln(stdout, dir)
	}
	return 0
}

func skillInstallDirs(target string, customDir string) ([]string, error) {
	if customDir != "" {
		return []string{customDir}, nil
	}
	home, err := os.UserHomeDir()
	if err != nil || home == "" {
		return nil, fmt.Errorf("home directory unavailable")
	}
	codexHome := os.Getenv("CODEX_HOME")
	if codexHome == "" {
		codexHome = filepath.Join(home, ".codex")
	}
	switch strings.ToLower(target) {
	case "codex":
		return []string{filepath.Join(codexHome, "skills")}, nil
	case "claude":
		return []string{filepath.Join(home, ".claude", "skills")}, nil
	case "all":
		return []string{
			filepath.Join(codexHome, "skills"),
			filepath.Join(home, ".claude", "skills"),
		}, nil
	default:
		return nil, fmt.Errorf("--target must be codex, claude, or all")
	}
}

func writeEmbeddedSkill(dest string) error {
	if err := os.RemoveAll(dest); err != nil {
		return err
	}
	return fs.WalkDir(skillAssets, "skill_assets/ai-dispatch", func(path string, entry fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel("skill_assets/ai-dispatch", path)
		if err != nil {
			return err
		}
		if rel == "." {
			return os.MkdirAll(dest, 0o755)
		}
		target := filepath.Join(dest, rel)
		if entry.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		data, err := skillAssets.ReadFile(path)
		if err != nil {
			return err
		}
		mode := fs.FileMode(0o644)
		if strings.HasPrefix(filepath.ToSlash(rel), "scripts/") {
			mode = 0o755
		}
		return os.WriteFile(target, data, mode)
	})
}
