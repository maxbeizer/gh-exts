package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const version = "0.2.0"

// Extension represents a single installed gh extension.
type Extension struct {
	Name    string // e.g. "gh agent-viz"
	Repo    string // e.g. "maxbeizer/gh-agent-viz" (may be empty for local)
	Version string // e.g. "v0.4.0" (may be empty)
}

func (e Extension) Title() string {
	if e.Repo != "" {
		return e.Name + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(e.Repo)
	}
	return e.Name + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("(local)")
}
func (e Extension) Description() string {
	if e.Version != "" {
		return e.Version
	}
	return "local dev"
}
func (e Extension) FilterValue() string {
	return e.Name + " " + e.Repo
}

// RepoInfo holds metadata fetched from the GitHub API.
type RepoInfo struct {
	Description    string `json:"description"`
	Stars          int    `json:"stargazers_count"`
	Language       string `json:"language"`
	License        *struct {
		SPDX string `json:"spdx_id"`
	} `json:"license"`
	Archived  bool   `json:"archived"`
	HTMLURL   string `json:"html_url"`
	UpdatedAt string `json:"updated_at"`
}

// --- messages ---

type readmeMsg struct {
	content  string
	ext      Extension
	repoInfo *RepoInfo
}

type errMsg struct{ err error }

// --- model ---

type view int

const (
	listView view = iota
	detailView
)

type model struct {
	list     list.Model
	viewport viewport.Model
	current  view
	readme   string
	extName  string
	width    int
	height   int
	ready    bool
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Don't intercept keys when the list is filtering.
		if m.current == listView && m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "enter":
			if m.current == listView {
				if item, ok := m.list.SelectedItem().(Extension); ok {
					return m, fetchReadme(item)
				}
			}
		case "esc", "backspace":
			if m.current == detailView {
				m.current = listView
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.list.SetSize(msg.Width-h, msg.Height-v)
		if m.ready {
			m.viewport.Width = msg.Width - h
			m.viewport.Height = msg.Height - v
		}

	case readmeMsg:
		header := formatRepoHeader(msg.ext, msg.repoInfo)
		m.readme = header + "\n\n" + msg.content
		m.extName = msg.ext.Name
		m.current = detailView
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.viewport = viewport.New(m.width-h, m.height-v)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil

	case errMsg:
		m.readme = fmt.Sprintf("Error: %v", msg.err)
		m.current = detailView
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.viewport = viewport.New(m.width-h, m.height-v)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil
	}

	var cmd tea.Cmd
	if m.current == listView {
		m.list, cmd = m.list.Update(msg)
	} else {
		m.viewport, cmd = m.viewport.Update(msg)
	}
	return m, cmd
}

func (m model) View() string {
	if m.current == detailView {
		hint := lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("esc to go back")
		return lipgloss.NewStyle().Margin(1, 2).Render(hint+"\n\n"+m.viewport.View())
	}
	return lipgloss.NewStyle().Margin(1, 2).Render(m.list.View())
}

// --- commands ---

func fetchRepoInfo(repo string) *RepoInfo {
	out, err := exec.Command("gh", "api", "repos/"+repo).Output()
	if err != nil {
		return nil
	}
	var info RepoInfo
	if err := json.Unmarshal(out, &info); err != nil {
		return nil
	}
	return &info
}

func formatRepoHeader(ext Extension, info *RepoInfo) string {
	nameStyle := lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("212"))
	dimStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("8"))
	cyanStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("6"))
	yellowStyle := lipgloss.NewStyle().Foreground(lipgloss.Color("3"))

	if info == nil {
		return nameStyle.Render(ext.Name) + "\n" + dimStyle.Render("Local extension")
	}

	var lines []string

	title := nameStyle.Render(ext.Name)
	if info.Archived {
		title += " " + lipgloss.NewStyle().Bold(true).Foreground(lipgloss.Color("1")).Render("[archived]")
	}
	lines = append(lines, title)

	if info.Description != "" {
		lines = append(lines, info.Description)
	}

	var meta []string
	meta = append(meta, yellowStyle.Render(fmt.Sprintf("★ %d", info.Stars)))
	if info.Language != "" {
		meta = append(meta, cyanStyle.Render(info.Language))
	}
	if info.License != nil && info.License.SPDX != "" && info.License.SPDX != "NOASSERTION" {
		meta = append(meta, info.License.SPDX)
	}
	lines = append(lines, strings.Join(meta, dimStyle.Render(" · ")))

	if info.HTMLURL != "" {
		lines = append(lines, dimStyle.Render(info.HTMLURL))
	}
	if info.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, info.UpdatedAt); err == nil {
			lines = append(lines, dimStyle.Render("Updated "+t.Format("Jan 2, 2006")))
		}
	}

	return strings.Join(lines, "\n")
}

func fetchReadme(ext Extension) tea.Cmd {
	return func() tea.Msg {
		repo := ext.Repo
		if repo == "" {
			return readmeMsg{
				content: "Local extension — no remote README available.",
				ext:     ext,
			}
		}

		info := fetchRepoInfo(repo)

		out, err := exec.Command("gh", "api", "repos/"+repo+"/readme",
			"--jq", ".content").Output()
		if err != nil {
			return readmeMsg{
				content:  "No README found for " + repo,
				ext:      ext,
				repoInfo: info,
			}
		}

		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
		if err != nil {
			return errMsg{err}
		}

		rendered, err := glamour.Render(string(decoded), "dark")
		if err != nil {
			return readmeMsg{content: string(decoded), ext: ext, repoInfo: info}
		}

		return readmeMsg{content: rendered, ext: ext, repoInfo: info}
	}
}

// --- helpers ---

func getExtensions() []Extension {
	out, err := exec.Command("gh", "extension", "list").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error listing extensions:", err)
		os.Exit(1)
	}

	var exts []Extension
	for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
		if line == "" {
			continue
		}
		fields := strings.Split(line, "\t")
		ext := Extension{}
		if len(fields) >= 1 {
			ext.Name = strings.TrimSpace(fields[0])
		}
		if len(fields) >= 2 {
			ext.Repo = strings.TrimSpace(fields[1])
		}
		if len(fields) >= 3 {
			ext.Version = strings.TrimSpace(fields[2])
		}
		if ext.Name != "" {
			exts = append(exts, ext)
		}
	}
	return exts
}

func exportScript(exts []Extension) {
	fmt.Printf("#!/bin/bash\n")
	fmt.Printf("# gh-exts export — generated %s\n", time.Now().Format("2006-01-02"))
	fmt.Printf("# Install your gh extensions on a new machine:\n\n")
	for _, ext := range exts {
		if ext.Repo == "" {
			fmt.Printf("# %s — skipped (local extension)\n", ext.Name)
			continue
		}
		if ext.Version != "" {
			fmt.Printf("# version: %s\n", ext.Version)
		}
		fmt.Printf("gh extension install %s\n", ext.Repo)
	}
}

type exportEntry struct {
	Name    string `json:"name"`
	Repo    string `json:"repo"`
	Version string `json:"version"`
}

func exportJSON(exts []Extension) {
	entries := make([]exportEntry, 0, len(exts))
	for _, ext := range exts {
		if ext.Repo == "" {
			continue
		}
		entries = append(entries, exportEntry{
			Name:    ext.Name,
			Repo:    ext.Repo,
			Version: ext.Version,
		})
	}
	data, err := json.MarshalIndent(entries, "", "  ")
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error marshalling JSON:", err)
		os.Exit(1)
	}
	fmt.Println(string(data))
}

func usage() {
	fmt.Printf(`gh-exts v%s — Your extensions, in depth

Usage:
  gh exts              Interactive extension browser
  gh exts --export     Export install script to stdout
  gh exts --export-json Export extensions as JSON
  gh exts -h           Show help
  gh exts -v           Show version
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
		case "--export":
			exportScript(getExtensions())
			return
		case "--export-json":
			exportJSON(getExtensions())
			return
		}
	}

	exts := getExtensions()
	if len(exts) == 0 {
		fmt.Println("No extensions installed.")
		return
	}

	items := make([]list.Item, len(exts))
	for i, e := range exts {
		items[i] = e
	}

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 80, 24)
	l.Title = fmt.Sprintf("gh exts — %d extension(s)", len(exts))
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	m := model{list: l}

	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	// Ensure JSON output isn't broken by pagers.
	os.Setenv("GH_PAGER", "")
}
