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

// Release represents a GitHub release.
type Release struct {
	TagName     string `json:"tag_name"`
	Name        string `json:"name"`
	Body        string `json:"body"`
	PublishedAt string `json:"published_at"`
}

// --- messages ---

type readmeMsg struct {
	content string
	ext     Extension
}

type changelogMsg struct {
	content string
	ext     Extension
}

type errMsg struct{ err error }

// --- model ---

type view int

const (
	listView view = iota
	detailView
	changelogView
)

type model struct {
	list       list.Model
	viewport   viewport.Model
	current    view
	readme     string
	extName    string
	currentExt Extension
	width      int
	height     int
	ready      bool
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
		case "c":
			if m.current == detailView {
				if m.currentExt.Repo != "" {
					return m, fetchChangelog(m.currentExt)
				}
			}
		case "esc", "backspace":
			if m.current == changelogView {
				m.current = detailView
				h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
				m.viewport = viewport.New(m.width-h, m.height-v)
				m.viewport.SetContent(m.readme)
				return m, nil
			}
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
		m.readme = msg.content
		m.extName = msg.ext.Name
		m.currentExt = msg.ext
		m.current = detailView
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.viewport = viewport.New(m.width-h, m.height-v)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil

	case changelogMsg:
		m.current = changelogView
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.viewport = viewport.New(m.width-h, m.height-v)
		m.viewport.SetContent(msg.content)
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
		// detailView and changelogView both use the viewport
		m.viewport, cmd = m.viewport.Update(msg)
	}
	return m, cmd
}

func (m model) View() string {
	if m.current == changelogView {
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Render(m.extName+" — Changelog") +
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (esc to go back)")
		return lipgloss.NewStyle().Margin(1, 2).Render(header + "\n\n" + m.viewport.View())
	}
	if m.current == detailView {
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Render(m.extName) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (esc to go back · c for changelog)")
		return lipgloss.NewStyle().Margin(1, 2).Render(header+"\n\n"+m.viewport.View())
	}
	return lipgloss.NewStyle().Margin(1, 2).Render(m.list.View())
}

// --- commands ---

func fetchReadme(ext Extension) tea.Cmd {
	return func() tea.Msg {
		repo := ext.Repo
		if repo == "" {
			return readmeMsg{
				content: "Local extension — no remote README available.",
				ext:     ext,
			}
		}

		out, err := exec.Command("gh", "api", "repos/"+repo+"/readme",
			"--jq", ".content").Output()
		if err != nil {
			return readmeMsg{
				content: "No README found for " + repo,
				ext:     ext,
			}
		}

		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
		if err != nil {
			return errMsg{err}
		}

		rendered, err := glamour.Render(string(decoded), "dark")
		if err != nil {
			// Fall back to raw markdown.
			return readmeMsg{content: string(decoded), ext: ext}
		}

		return readmeMsg{content: rendered, ext: ext}
	}
}

func fetchChangelog(ext Extension) tea.Cmd {
	return func() tea.Msg {
		if ext.Repo == "" {
			return changelogMsg{
				content: "Local extension — no changelog available.",
				ext:     ext,
			}
		}

		out, err := exec.Command("gh", "api", "repos/"+ext.Repo+"/releases").Output()
		if err != nil {
			return changelogMsg{
				content: "No releases found for " + ext.Repo + ".",
				ext:     ext,
			}
		}

		var releases []Release
		if err := json.Unmarshal(out, &releases); err != nil {
			return changelogMsg{
				content: "Could not parse releases for " + ext.Repo + ".",
				ext:     ext,
			}
		}

		if len(releases) == 0 {
			return changelogMsg{
				content: "No releases found for " + ext.Repo + ".",
				ext:     ext,
			}
		}

		// Filter to releases newer than installed version
		installedVer := normalizeVersion(ext.Version)
		var newer []Release
		for _, r := range releases {
			if installedVer == "" || compareVersions(normalizeVersion(r.TagName), installedVer) > 0 {
				newer = append(newer, r)
			}
		}

		if len(newer) == 0 {
			return changelogMsg{
				content: "You're up to date! No releases newer than " + ext.Version + ".",
				ext:     ext,
			}
		}

		var sb strings.Builder
		for _, r := range newer {
			title := r.TagName
			if r.Name != "" && r.Name != r.TagName {
				title += " — " + r.Name
			}
			sb.WriteString("## " + title + "\n")
			if r.PublishedAt != "" {
				date := r.PublishedAt
				if len(date) >= 10 {
					date = date[:10]
				}
				sb.WriteString("*Released: " + date + "*\n\n")
			}
			if r.Body != "" {
				sb.WriteString(r.Body + "\n\n")
			} else {
				sb.WriteString("_No release notes._\n\n")
			}
			sb.WriteString("---\n\n")
		}

		rendered, err := glamour.Render(sb.String(), "dark")
		if err != nil {
			return changelogMsg{content: sb.String(), ext: ext}
		}

		return changelogMsg{content: rendered, ext: ext}
	}
}

// normalizeVersion strips a leading "v" prefix.
func normalizeVersion(v string) string {
	return strings.TrimPrefix(strings.TrimSpace(v), "v")
}

// compareVersions compares two dot-separated version strings.
// Returns >0 if a > b, <0 if a < b, 0 if equal.
func compareVersions(a, b string) int {
	aParts := strings.Split(a, ".")
	bParts := strings.Split(b, ".")
	maxLen := len(aParts)
	if len(bParts) > maxLen {
		maxLen = len(bParts)
	}
	for i := 0; i < maxLen; i++ {
		var ai, bi int
		if i < len(aParts) {
			fmt.Sscanf(aParts[i], "%d", &ai)
		}
		if i < len(bParts) {
			fmt.Sscanf(bParts[i], "%d", &bi)
		}
		if ai != bi {
			return ai - bi
		}
	}
	return 0
}

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
