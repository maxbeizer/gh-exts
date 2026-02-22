package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const version = "0.3.0"

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

// --- messages ---

type readmeMsg struct {
	content string
	ext     Extension
}

type updateMsg struct {
	ext Extension
	err error
}

type removeMsg struct {
	ext Extension
	err error
}

type updateAllMsg struct {
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
	list          list.Model
	viewport      viewport.Model
	current       view
	readme        string
	extName       string
	width         int
	height        int
	ready         bool
	confirmRemove bool
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

		// Handle confirm-remove state.
		if m.confirmRemove {
			m.confirmRemove = false
			if msg.String() == "x" || msg.String() == "y" {
				if item, ok := m.list.SelectedItem().(Extension); ok {
					m.list.NewStatusMessage("Removing " + item.Name + "…")
					return m, removeExtension(item)
				}
			}
			m.list.NewStatusMessage("Remove cancelled.")
			return m, nil
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
		case "u":
			if m.current == listView {
				if item, ok := m.list.SelectedItem().(Extension); ok {
					m.list.NewStatusMessage("Updating " + item.Name + "…")
					return m, updateExtension(item)
				}
			}
		case "U":
			if m.current == listView {
				m.list.NewStatusMessage("Updating all extensions…")
				return m, updateAllExtensions()
			}
		case "x":
			if m.current == listView {
				if item, ok := m.list.SelectedItem().(Extension); ok {
					m.confirmRemove = true
					m.list.NewStatusMessage("Remove " + item.Name + "? Press x/y to confirm, any other key to cancel.")
				}
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

	case updateMsg:
		if msg.err != nil {
			m.list.NewStatusMessage("✗ Update failed: " + msg.err.Error())
		} else {
			m.list.NewStatusMessage("✓ Updated " + msg.ext.Name)
		}
		return m, nil

	case removeMsg:
		if msg.err != nil {
			m.list.NewStatusMessage("✗ Remove failed: " + msg.err.Error())
		} else {
			m.list.NewStatusMessage("✓ Removed " + msg.ext.Name)
			// Rebuild list without the removed extension.
			var items []list.Item
			for _, item := range m.list.Items() {
				if ext, ok := item.(Extension); ok && ext.Name != msg.ext.Name {
					items = append(items, item)
				}
			}
			m.list.SetItems(items)
			m.list.Title = fmt.Sprintf("gh exts — %d extension(s)", len(items))
		}
		return m, nil

	case updateAllMsg:
		if msg.err != nil {
			m.list.NewStatusMessage("✗ Update all failed: " + msg.err.Error())
		} else {
			m.list.NewStatusMessage("✓ All extensions updated")
		}
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

// extShortName extracts the short name for gh extension commands.
// e.g. "gh contrib" -> "contrib"
func extShortName(ext Extension) string {
	name := ext.Name
	if strings.HasPrefix(name, "gh ") {
		name = strings.TrimPrefix(name, "gh ")
	}
	return name
}

func updateExtension(ext Extension) tea.Cmd {
	return func() tea.Msg {
		name := extShortName(ext)
		cmd := exec.Command("gh", "extension", "upgrade", name)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return updateMsg{ext: ext, err: fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))}
		}
		return updateMsg{ext: ext}
	}
}

func updateAllExtensions() tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command("gh", "extension", "upgrade", "--all")
		out, err := cmd.CombinedOutput()
		if err != nil {
			return updateAllMsg{err: fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))}
		}
		return updateAllMsg{}
	}
}

func removeExtension(ext Extension) tea.Cmd {
	return func() tea.Msg {
		name := extShortName(ext)
		cmd := exec.Command("gh", "extension", "remove", name)
		out, err := cmd.CombinedOutput()
		if err != nil {
			return removeMsg{ext: ext, err: fmt.Errorf("%s: %s", err, strings.TrimSpace(string(out)))}
		}
		return removeMsg{ext: ext}
	}
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
	l.AdditionalShortHelpKeys = func() []key.Binding {
		return []key.Binding{
			key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update")),
			key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "update all")),
			key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
			key.NewBinding(key.WithKeys("enter"), key.WithHelp("enter", "view readme")),
		}
	}

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
