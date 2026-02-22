package main

import (
	"encoding/base64"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"

	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const version = "0.2.0"

// Extension represents a single installed gh extension.
type Extension struct {
	Name          string // e.g. "gh agent-viz"
	Repo          string // e.g. "maxbeizer/gh-agent-viz" (may be empty for local)
	Version       string // e.g. "v0.4.0" (may be empty)
	LatestVersion string // e.g. "v0.5.0" (fetched from GitHub releases)
}

func (e Extension) Title() string {
	if e.Repo != "" {
		return e.Name + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(e.Repo)
	}
	return e.Name + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("(local)")
}
func (e Extension) Description() string {
	if e.Version == "" {
		return "local dev"
	}
	if e.LatestVersion == "" {
		return e.Version
	}
	if e.Version != e.LatestVersion {
		return e.Version + " → " + e.LatestVersion + " available"
	}
	return e.Version + " ✓ latest"
}

func (e Extension) HasUpdate() bool {
	return e.Version != "" && e.LatestVersion != "" && e.Version != e.LatestVersion
}
func (e Extension) FilterValue() string {
	return e.Name + " " + e.Repo
}

// --- messages ---

type readmeMsg struct {
	content string
	ext     Extension
}

type versionsMsg struct {
	versions map[string]string // repo -> latest version
}

type errMsg struct{ err error }

// --- model ---

type view int

const (
	listView view = iota
	detailView
)

type model struct {
	list       list.Model
	viewport   viewport.Model
	current    view
	readme     string
	extName    string
	width      int
	height     int
	ready      bool
	extensions []Extension
	outdated   bool
}

func (m model) Init() tea.Cmd {
	return fetchVersions(m.extensions)
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
		m.readme = msg.content
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

	case versionsMsg:
		for i, ext := range m.extensions {
			if v, ok := msg.versions[ext.Repo]; ok {
				m.extensions[i].LatestVersion = v
			}
		}
		var items []list.Item
		for _, ext := range m.extensions {
			if m.outdated && !ext.HasUpdate() {
				continue
			}
			items = append(items, ext)
		}
		if m.outdated && len(items) == 0 {
			// All extensions are up to date; show a message.
			items = nil
		}
		m.list.SetItems(items)
		m.list.Title = fmt.Sprintf("gh exts — %d extension(s)", len(items))
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
		header := lipgloss.NewStyle().
			Bold(true).
			Foreground(lipgloss.Color("212")).
			Render(m.extName) +
			lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render("  (esc to go back)")
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

func fetchVersions(exts []Extension) tea.Cmd {
	return func() tea.Msg {
		versions := make(map[string]string)
		var mu sync.Mutex
		var wg sync.WaitGroup

		for _, ext := range exts {
			if ext.Repo == "" || ext.Version == "" {
				continue
			}
			wg.Add(1)
			go func(repo string) {
				defer wg.Done()
				out, err := exec.Command("gh", "api",
					"repos/"+repo+"/releases/latest",
					"--jq", ".tag_name").Output()
				if err != nil {
					return
				}
				tag := strings.TrimSpace(string(out))
				if tag != "" {
					mu.Lock()
					versions[repo] = tag
					mu.Unlock()
				}
			}(ext.Repo)
		}

		wg.Wait()
		return versionsMsg{versions: versions}
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

func usage() {
	fmt.Printf(`gh-exts v%s — Your extensions, in depth

Usage:
  gh exts              Interactive extension browser
  gh exts --outdated   Show only extensions with available updates
  gh exts -h           Show help
  gh exts -v           Show version
`, version)
}

func main() {
	outdated := false
	for _, arg := range os.Args[1:] {
		switch arg {
		case "-h", "--help", "help":
			usage()
			return
		case "-v", "--version", "version":
			fmt.Printf("gh-exts v%s\n", version)
			return
		case "--outdated":
			outdated = true
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
	title := fmt.Sprintf("gh exts — %d extension(s)", len(exts))
	if outdated {
		title = "gh exts — checking for updates…"
	}
	l.Title = title
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	m := model{list: l, extensions: exts, outdated: outdated}

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
