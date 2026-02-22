package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
)

const version = "0.1.0"

type RepoInfo struct {
	Name        string `json:"name"`
	FullName    string `json:"full_name"`
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`
	License     struct {
		SPDX string `json:"spdx_id"`
	} `json:"license"`
	HTMLURL   string `json:"html_url"`
	UpdatedAt string `json:"updated_at"`
	Archived  bool   `json:"archived"`
}

func usage() {
	fmt.Printf(`gh-exts v%s — Your extensions, but useful

Usage:
  gh exts              Show all installed extensions with details
  gh exts <name>       Show details for one extension
  gh exts -h           Show help
`, version)
}

func main() {
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			usage()
			return
		case "-v", "--version", "version":
			fmt.Printf("gh-exts v%s\n", version)
			return
		default:
			showOne(os.Args[1])
			return
		}
	}

	showAll()
}

func getExtensions() []string {
	out, err := exec.Command("gh", "extension", "list").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error listing extensions:", err)
		os.Exit(1)
	}

	var exts []string
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 {
			exts = append(exts, fields[1])
		} else if len(fields) == 1 {
			exts = append(exts, fields[0])
		}
	}
	return exts
}

func fetchRepoInfo(repo string) (*RepoInfo, error) {
	out, err := exec.Command("gh", "api", "repos/"+repo).Output()
	if err != nil {
		return nil, err
	}
	var info RepoInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil, err
	}
	return &info, nil
}

func fetchReadmeExcerpt(repo string) string {
	out, err := exec.Command("gh", "api", "repos/"+repo+"/readme",
		"--jq", ".content").Output()
	if err != nil {
		return ""
	}

	decoded, err := exec.Command("bash", "-c",
		fmt.Sprintf("echo '%s' | base64 -d 2>/dev/null | head -20", strings.TrimSpace(string(out)))).Output()
	if err != nil {
		return ""
	}

	lines := strings.Split(string(decoded), "\n")
	var excerpt []string
	pastHeader := false
	for _, line := range lines {
		trimmed := strings.TrimSpace(line)
		if strings.HasPrefix(trimmed, "#") {
			pastHeader = true
			continue
		}
		if trimmed == "" {
			if pastHeader && len(excerpt) > 0 {
				break
			}
			continue
		}
		if strings.HasPrefix(trimmed, "![") || strings.HasPrefix(trimmed, "[![") {
			continue
		}
		if pastHeader || !strings.HasPrefix(trimmed, "#") {
			pastHeader = true
			excerpt = append(excerpt, trimmed)
			if len(excerpt) >= 3 {
				break
			}
		}
	}

	return strings.Join(excerpt, " ")
}

func showAll() {
	exts := getExtensions()
	if len(exts) == 0 {
		fmt.Println("  No extensions installed.")
		return
	}

	dim := "\033[2m"
	bold := "\033[1m"
	yellow := "\033[33m"
	cyan := "\033[36m"
	reset := "\033[0m"

	fmt.Printf("\n  %sgh exts%s — %d extension(s)\n\n", bold, reset, len(exts))

	for _, repo := range exts {
		info, err := fetchRepoInfo(repo)
		if err != nil {
			fmt.Printf("  %s%s%s\n", bold, repo, reset)
			fmt.Printf("    %s(couldn't fetch details)%s\n\n", dim, reset)
			continue
		}

		fmt.Printf("  %s%s%s", bold, info.FullName, reset)
		if info.Archived {
			fmt.Printf(" %s[archived]%s", yellow, reset)
		}
		fmt.Println()

		desc := info.Description
		if desc == "" {
			desc = "(no description)"
		}
		fmt.Printf("    %s\n", desc)

		meta := []string{}
		if info.Language != "" {
			meta = append(meta, fmt.Sprintf("%s%s%s", cyan, info.Language, reset))
		}
		if info.Stars > 0 {
			meta = append(meta, fmt.Sprintf("%s★%s %d", yellow, reset, info.Stars))
		}
		if info.License.SPDX != "" && info.License.SPDX != "NOASSERTION" {
			meta = append(meta, info.License.SPDX)
		}
		if len(meta) > 0 {
			fmt.Printf("    %s%s%s\n", dim, strings.Join(meta, "  ·  "), reset)
		}

		fmt.Println()
	}
}

func showOne(name string) {
	dim := "\033[2m"
	bold := "\033[1m"
	yellow := "\033[33m"
	cyan := "\033[36m"
	reset := "\033[0m"

	exts := getExtensions()
	var repo string
	for _, ext := range exts {
		if strings.Contains(ext, name) || strings.HasSuffix(ext, "/gh-"+name) || strings.HasSuffix(ext, "/"+name) {
			repo = ext
			break
		}
	}

	if repo == "" {
		fmt.Fprintf(os.Stderr, "  Extension '%s' not found. Run 'gh exts' to see installed extensions.\n", name)
		os.Exit(1)
	}

	info, err := fetchRepoInfo(repo)
	if err != nil {
		fmt.Fprintf(os.Stderr, "  Couldn't fetch info for %s\n", repo)
		os.Exit(1)
	}

	fmt.Printf("\n  %s%s%s\n", bold, info.FullName, reset)
	if info.Archived {
		fmt.Printf("  %s[archived]%s\n", yellow, reset)
	}

	desc := info.Description
	if desc == "" {
		desc = "(no description)"
	}
	fmt.Printf("  %s\n\n", desc)

	if info.Language != "" {
		fmt.Printf("  Language:  %s%s%s\n", cyan, info.Language, reset)
	}
	fmt.Printf("  Stars:     %s★%s %d\n", yellow, reset, info.Stars)
	if info.License.SPDX != "" && info.License.SPDX != "NOASSERTION" {
		fmt.Printf("  License:   %s\n", info.License.SPDX)
	}
	fmt.Printf("  URL:       %s\n", info.HTMLURL)
	fmt.Printf("  Updated:   %s%s%s\n", dim, info.UpdatedAt[:10], reset)

	excerpt := fetchReadmeExcerpt(repo)
	if excerpt != "" {
		fmt.Printf("\n  %sFrom README:%s\n", bold, reset)
		fmt.Printf("  %s%s%s\n", dim, truncate(excerpt, 200), reset)
	}

	fmt.Println()
}

func truncate(s string, max int) string {
	if len(s) <= max {
		return s
	}
	return s[:max-1] + "…"
}
