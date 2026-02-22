package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"

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

// BrowseExtension represents a gh extension from the GitHub search API.
type BrowseExtension struct {
	FullName string
	Desc     string
	Stars    int
	Installed bool
}

func (b BrowseExtension) Title() string {
	title := b.FullName
	if b.Installed {
		title += "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("2")).Render("[installed]")
	}
	return title
}
func (b BrowseExtension) Description() string {
	star := fmt.Sprintf("★ %d", b.Stars)
	if b.Desc != "" {
		return b.Desc + "  " + lipgloss.NewStyle().Foreground(lipgloss.Color("8")).Render(star)
	}
	return star
}
func (b BrowseExtension) FilterValue() string {
	return b.FullName + " " + b.Desc
}

// --- messages ---

type readmeMsg struct {
	content string
	ext     Extension
}

type browseReadmeMsg struct {
	content string
	ext     BrowseExtension
}

type installMsg struct {
	ext BrowseExtension
	err error
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
	browseMode bool
	statusMsg  string
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
				if m.browseMode {
					if item, ok := m.list.SelectedItem().(BrowseExtension); ok {
						return m, fetchBrowseReadme(item)
					}
				} else {
					if item, ok := m.list.SelectedItem().(Extension); ok {
						return m, fetchReadme(item)
					}
				}
			}
		case "i":
			if m.current == listView && m.browseMode {
				if item, ok := m.list.SelectedItem().(BrowseExtension); ok {
					if item.Installed {
						m.statusMsg = item.FullName + " is already installed"
						m.list.NewStatusMessage(m.statusMsg)
						return m, nil
					}
					m.statusMsg = "Installing " + item.FullName + "..."
					m.list.NewStatusMessage(m.statusMsg)
					return m, installExtension(item)
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

	case browseReadmeMsg:
		m.readme = msg.content
		m.extName = msg.ext.FullName
		m.current = detailView
		h, v := lipgloss.NewStyle().Margin(1, 2).GetFrameSize()
		m.viewport = viewport.New(m.width-h, m.height-v)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil

	case installMsg:
		if msg.err != nil {
			m.statusMsg = "Install failed: " + msg.err.Error()
		} else {
			m.statusMsg = "Installed " + msg.ext.FullName + " ✓"
			// Mark as installed in the list.
			items := m.list.Items()
			for i, it := range items {
				if b, ok := it.(BrowseExtension); ok && b.FullName == msg.ext.FullName {
					b.Installed = true
					items[i] = b
				}
			}
			m.list.SetItems(items)
		}
		m.list.NewStatusMessage(m.statusMsg)
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

func fetchBrowseReadme(ext BrowseExtension) tea.Cmd {
	return func() tea.Msg {
		out, err := exec.Command("gh", "api", "repos/"+ext.FullName+"/readme",
			"--jq", ".content").Output()
		if err != nil {
			return browseReadmeMsg{
				content: "No README found for " + ext.FullName,
				ext:     ext,
			}
		}

		decoded, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(out)))
		if err != nil {
			return errMsg{err}
		}

		rendered, err := glamour.Render(string(decoded), "dark")
		if err != nil {
			return browseReadmeMsg{content: string(decoded), ext: ext}
		}

		return browseReadmeMsg{content: rendered, ext: ext}
	}
}

func installExtension(ext BrowseExtension) tea.Cmd {
	return func() tea.Msg {
		err := exec.Command("gh", "extension", "install", ext.FullName).Run()
		return installMsg{ext: ext, err: err}
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

type searchResult struct {
	Items []struct {
		FullName    string `json:"full_name"`
		Description string `json:"description"`
		Stars       int    `json:"stargazers_count"`
	} `json:"items"`
}

func getBrowseExtensions(installed []Extension) []BrowseExtension {
	out, err := exec.Command("gh", "api",
		"search/repositories?q=topic:gh-extension&sort=stars&order=desc&per_page=50").Output()
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error searching extensions:", err)
		os.Exit(1)
	}

	var result searchResult
	if err := json.Unmarshal(out, &result); err != nil {
		fmt.Fprintln(os.Stderr, "Error parsing search results:", err)
		os.Exit(1)
	}

	installedSet := make(map[string]bool)
	for _, ext := range installed {
		if ext.Repo != "" {
			installedSet[strings.ToLower(ext.Repo)] = true
		}
	}

	var exts []BrowseExtension
	for _, item := range result.Items {
		exts = append(exts, BrowseExtension{
			FullName:  item.FullName,
			Desc:      item.Description,
			Stars:     item.Stars,
			Installed: installedSet[strings.ToLower(item.FullName)],
		})
	}
	return exts
}

func usage() {
	fmt.Printf(`gh-exts v%s — Your extensions, in depth

Usage:
  gh exts              Interactive extension browser
  gh exts --browse     Browse and install new extensions from GitHub
  gh exts -h           Show help
  gh exts -v           Show version

Keys (browse mode):
  Enter    View README
  i        Install selected extension
  /        Filter
  Esc      Go back
  q        Quit
`, version)
}

func main() {
	browseMode := false
	if len(os.Args) > 1 {
		switch os.Args[1] {
		case "-h", "--help", "help":
			usage()
			return
		case "-v", "--version", "version":
			fmt.Printf("gh-exts v%s\n", version)
			return
		case "--browse":
			browseMode = true
		}
	}

	installed := getExtensions()

	var items []list.Item
	var title string

	if browseMode {
		browse := getBrowseExtensions(installed)
		if len(browse) == 0 {
			fmt.Println("No extensions found.")
			return
		}
		items = make([]list.Item, len(browse))
		for i, b := range browse {
			items[i] = b
		}
		title = fmt.Sprintf("gh exts --browse — %d extension(s)  (i=install, enter=readme)", len(browse))
	} else {
		if len(installed) == 0 {
			fmt.Println("No extensions installed.")
			return
		}
		items = make([]list.Item, len(installed))
		for i, e := range installed {
			items[i] = e
		}
		title = fmt.Sprintf("gh exts — %d extension(s)", len(installed))
	}

	delegate := list.NewDefaultDelegate()
	l := list.New(items, delegate, 80, 24)
	l.Title = title
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)

	m := model{list: l, browseMode: browseMode}

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
