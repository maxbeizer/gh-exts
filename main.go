package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const version = "0.4.0"

// gh-native color palette
var (
	dimStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("242"))
	boldStyle    = lipgloss.NewStyle().Bold(true)
	greenStyle   = lipgloss.NewStyle().Foreground(lipgloss.Color("#3fb950"))
	yellowStyle  = lipgloss.NewStyle().Foreground(lipgloss.Color("#d29922"))
	cyanStyle    = lipgloss.NewStyle().Foreground(lipgloss.Color("#58a6ff"))
	redStyle     = lipgloss.NewStyle().Foreground(lipgloss.Color("#f85149"))
)

// HealthInfo holds repository health metadata for an extension.
type HealthInfo struct {
	Archived   bool
	PushedAt   time.Time
	Stars      int
	Forks      int
	OpenIssues int
}

// Extension represents a single installed gh extension.
type Extension struct {
	Name          string      // e.g. "gh agent-viz"
	Repo          string      // e.g. "maxbeizer/gh-agent-viz" (may be empty for local)
	Version       string      // e.g. "v0.4.0" (may be empty)
	LatestVersion string      // e.g. "v0.5.0" (fetched from GitHub releases)
	Health        *HealthInfo // nil until fetched
}

func (e Extension) Title() string       { return e.Name }
func (e Extension) FilterValue() string { return e.Name + " " + e.Repo }

func (e Extension) Description() string {
	var parts []string

	if e.Repo != "" {
		parts = append(parts, e.Repo)
	} else {
		parts = append(parts, "local")
	}

	if e.Version != "" {
		parts = append(parts, e.Version)
	}

	return strings.Join(parts, " · ")
}

func (e Extension) HasUpdate() bool {
	if e.Version == "" || e.LatestVersion == "" || e.Version == e.LatestVersion {
		return false
	}
	// Don't flag commit-hash versions as outdated vs tag versions
	if isCommitHash(e.Version) {
		return false
	}
	return true
}

// isCommitHash returns true if the version looks like a git commit hash.
func isCommitHash(v string) bool {
	if len(v) < 7 {
		return false
	}
	for _, c := range v {
		if !((c >= '0' && c <= '9') || (c >= 'a' && c <= 'f')) {
			return false
		}
	}
	return true
}

// RepoInfo holds metadata fetched from the GitHub API for the detail view header.
type RepoInfo struct {
	Description string `json:"description"`
	Stars       int    `json:"stargazers_count"`
	Language    string `json:"language"`
	License     *struct {
		SPDX string `json:"spdx_id"`
	} `json:"license"`
	Archived  bool   `json:"archived"`
	HTMLURL   string `json:"html_url"`
	UpdatedAt string `json:"updated_at"`
}

// BrowseExtension represents a gh extension from the GitHub search API.
type BrowseExtension struct {
	FullName  string
	Desc      string
	Stars     int
	Installed bool
}

func (b BrowseExtension) Title() string       { return b.FullName }
func (b BrowseExtension) FilterValue() string { return b.FullName + " " + b.Desc }

func (b BrowseExtension) Description() string {
	var parts []string
	if b.Desc != "" {
		parts = append(parts, b.Desc)
	}
	parts = append(parts, fmt.Sprintf("★ %d", b.Stars))
	return strings.Join(parts, " · ")
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
	content  string
	ext      Extension
	repoInfo *RepoInfo
}

type changelogMsg struct {
	content string
	ext     Extension
}

type versionsMsg struct {
	versions map[string]string // repo -> latest version
}

type healthMsg struct {
	data map[string]HealthInfo
}

type browseReadmeMsg struct {
	content string
	ext     BrowseExtension
}

type installMsg struct {
	ext BrowseExtension
	err error
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

type pruneMsg struct {
	removed []string
	errors  []string
}

type auditMsg struct {
	content string
	ext     Extension
}

type copilotAuditMsg struct {
	analysis string
}

type convertSearchMsg struct {
	ext       Extension
	candidate string // "owner/repo" of the best match, empty if none
	err       error
}

type convertMsg struct {
	ext  Extension
	repo string
	err  error
}

type errMsg struct{ err error }

type spinnerTickMsg struct{}

var spinnerFrames = []string{"⠋", "⠙", "⠹", "⠸", "⠼", "⠴", "⠦", "⠧", "⠇", "⠏"}

func spinnerTick() tea.Cmd {
	return tea.Tick(80*time.Millisecond, func(time.Time) tea.Msg {
		return spinnerTickMsg{}
	})
}

// --- model ---

type viewState int

const (
	listView viewState = iota
	detailView
	changelogView
	auditView
)

// listItem is a union type for items displayed in the picker.
type listItem struct {
	ext    *Extension
	browse *BrowseExtension
}

func (li listItem) name() string {
	if li.ext != nil {
		return li.ext.Name
	}
	return li.browse.FullName
}

func (li listItem) matchText() string {
	if li.ext != nil {
		return li.ext.Name + " " + li.ext.Repo
	}
	return li.browse.FullName + " " + li.browse.Desc
}

type model struct {
	// List state
	items      []listItem
	filtered   []int // indices into items that match filter
	cursor     int
	filter     string
	filtering  bool
	scrollOff  int // first visible item index in filtered list

	// Detail state
	viewport   viewport.Model
	current    viewState
	readme     string
	extName    string
	currentExt Extension

	// App state
	extensions    []Extension
	outdatedOnly  bool
	browseMode    bool
	statusMsg     string
	confirmRemove    bool
	confirmConvert   bool
	convertCandidate string // "owner/repo" pending confirmation
	converting       bool
	updating      map[string]bool // names of extensions currently being updated
	updatingAll     bool
	auditContent    string // raw audit markdown before Copilot
	auditRepo       string // repo being audited, for Copilot request
	awaitingCopilot bool
	spinnerFrame    int
	width           int
	height          int
	ready           bool
}

func (m *model) applyFilter() {
	m.filtered = m.filtered[:0]
	q := strings.ToLower(m.filter)
	for i, it := range m.items {
		if q == "" || strings.Contains(strings.ToLower(it.matchText()), q) {
			m.filtered = append(m.filtered, i)
		}
	}
	if m.cursor >= len(m.filtered) {
		m.cursor = max(0, len(m.filtered)-1)
	}
	m.scrollOff = 0
}

func (m model) selectedItem() *listItem {
	if m.cursor >= 0 && m.cursor < len(m.filtered) {
		return &m.items[m.filtered[m.cursor]]
	}
	return nil
}

// visibleLines returns how many list items fit on screen.
func (m model) visibleLines() int {
	// Reserve lines: 1 filter/status bar at top, 1 hints at bottom
	v := m.height - 2
	if v < 1 {
		v = 1
	}
	return v
}

func (m model) Init() tea.Cmd {
	if m.browseMode {
		return nil
	}
	return tea.Batch(fetchHealth(m.extensions), fetchVersions(m.extensions))
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.KeyMsg:
		// Filtering mode — capture typing
		if m.filtering && m.current == listView {
			switch msg.Type {
			case tea.KeyEsc:
				m.filtering = false
				m.filter = ""
				m.applyFilter()
				return m, nil
			case tea.KeyEnter:
				m.filtering = false
				return m, nil
			case tea.KeyBackspace:
				if len(m.filter) > 0 {
					m.filter = m.filter[:len(m.filter)-1]
					m.applyFilter()
				}
				return m, nil
			default:
				if msg.Type == tea.KeyRunes {
					m.filter += string(msg.Runes)
					m.applyFilter()
					return m, nil
				}
			}
		}

		// Confirm-remove state
		if m.confirmRemove {
			m.confirmRemove = false
			if msg.String() == "x" || msg.String() == "y" {
				if it := m.selectedItem(); it != nil && it.ext != nil {
					m.statusMsg = "Removing " + it.ext.Name + "…"
					return m, removeExtension(*it.ext)
				}
			}
			m.statusMsg = ""
			return m, nil
		}

		// Confirm-convert state
		if m.confirmConvert {
			m.confirmConvert = false
			if msg.String() == "I" || msg.String() == "y" {
				if it := m.selectedItem(); it != nil && it.ext != nil {
					m.converting = true
					m.statusMsg = "Converting " + it.ext.Name + " → " + m.convertCandidate + "…"
					return m, convertExtension(*it.ext, m.convertCandidate)
				}
			}
			m.statusMsg = ""
			m.convertCandidate = ""
			return m, nil
		}

		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit

		// Navigation
		case "up", "k":
			if m.current == listView {
				if m.cursor > 0 {
					m.cursor--
				}
				if m.cursor < m.scrollOff {
					m.scrollOff = m.cursor
				}
			}
		case "down", "j":
			if m.current == listView {
				if m.cursor < len(m.filtered)-1 {
					m.cursor++
				}
				vis := m.visibleLines()
				if m.cursor >= m.scrollOff+vis {
					m.scrollOff = m.cursor - vis + 1
				}
			}

		case "/":
			if m.current == listView {
				m.filtering = true
				m.filter = ""
				m.applyFilter()
			}

		case "enter":
			if m.current == listView {
				it := m.selectedItem()
				if it == nil {
					return m, nil
				}
				if it.browse != nil {
					return m, fetchBrowseReadme(*it.browse)
				}
				if it.ext != nil {
					return m, fetchReadme(*it.ext)
				}
			}

		case "c":
			if m.current == detailView && !m.browseMode && m.currentExt.Repo != "" {
				return m, fetchChangelog(m.currentExt)
			}
		case "s":
			if m.current == detailView && m.currentExt.Repo != "" {
				m.current = auditView
				m.awaitingCopilot = true // reuse spinner for scanning phase
				m.spinnerFrame = 0
				m.viewport = viewport.New(m.width, m.height-1)
				m.viewport.SetContent("")
				m.ready = true
				return m, tea.Batch(runSecurityAudit(m.currentExt), spinnerTick())
			}

		case "i":
			if m.current == listView && m.browseMode {
				if it := m.selectedItem(); it != nil && it.browse != nil {
					if it.browse.Installed {
						m.statusMsg = it.browse.FullName + " is already installed"
						return m, nil
					}
					m.statusMsg = "Installing " + it.browse.FullName + "…"
					return m, installExtension(*it.browse)
				}
			}

		case "u":
			if m.current == listView && !m.browseMode {
				if it := m.selectedItem(); it != nil && it.ext != nil {
					if m.updating == nil {
						m.updating = make(map[string]bool)
					}
					m.updating[it.ext.Name] = true
					m.statusMsg = "Updating " + it.ext.Name + "…"
					return m, updateExtension(*it.ext)
				}
			}
		case "U":
			if m.current == listView && !m.browseMode {
				m.updatingAll = true
				if m.updating == nil {
					m.updating = make(map[string]bool)
				}
				for _, ext := range m.extensions {
					if ext.Repo != "" {
						m.updating[ext.Name] = true
					}
				}
				m.statusMsg = "Updating all extensions…"
				return m, updateAllExtensions()
			}
		case "x":
			if m.current == listView && !m.browseMode {
				if it := m.selectedItem(); it != nil && it.ext != nil {
					m.confirmRemove = true
					m.statusMsg = "Remove " + it.ext.Name + "? (x/y to confirm)"
				}
				return m, nil
			}
		case "I":
			if m.current == listView && !m.browseMode {
				if it := m.selectedItem(); it != nil && it.ext != nil && it.ext.Repo == "" {
					m.statusMsg = "Searching for official " + it.ext.Name + "…"
					return m, searchForOfficialExtension(*it.ext)
				}
			}
		case "p":
			if m.current == listView && !m.browseMode {
				m.statusMsg = "Pruning archived extensions…"
				return m, pruneArchived(m.extensions)
			}

		case "esc", "backspace":
			if m.current == changelogView || m.current == auditView {
				m.current = detailView
				m.viewport = viewport.New(m.width, m.height-1)
				m.viewport.SetContent(m.readme)
				return m, nil
			}
			if m.current == detailView {
				m.current = listView
				m.statusMsg = ""
				return m, nil
			}
		}

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		if m.ready {
			m.viewport.Width = msg.Width
			m.viewport.Height = msg.Height - 1
		}

	case readmeMsg:
		header := formatRepoHeader(msg.ext, msg.repoInfo)
		m.readme = header + "\n" + msg.content
		m.extName = msg.ext.Name
		m.currentExt = msg.ext
		m.current = detailView
		m.viewport = viewport.New(m.width, m.height-1)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil

	case changelogMsg:
		m.current = changelogView
		m.viewport = viewport.New(m.width, m.height-1)
		m.viewport.SetContent(msg.content)
		m.ready = true
		return m, nil

	case auditMsg:
		m.current = auditView
		m.statusMsg = ""
		m.auditContent = msg.content
		m.auditRepo = msg.ext.Repo

		rendered, _ := glamour.Render(msg.content, "dark")
		m.viewport = viewport.New(m.width, m.height-1)
		m.viewport.SetContent(rendered)
		m.ready = true

		hasCopilot := exec.Command("gh", "copilot", "--version").Run() == nil
		if hasCopilot && strings.Contains(msg.content, "finding(s)") {
			m.awaitingCopilot = true
			m.spinnerFrame = 0
			return m, tea.Batch(fetchCopilotAudit(msg.ext.Repo, msg.content), spinnerTick())
		}
		return m, nil

	case copilotAuditMsg:
		m.awaitingCopilot = false
		display := m.auditContent + "\n## Copilot Analysis\n\n" + msg.analysis + "\n"
		rendered, _ := glamour.Render(display, "dark")
		m.viewport.SetContent(rendered)
		return m, nil

	case spinnerTickMsg:
		if m.awaitingCopilot {
			m.spinnerFrame = (m.spinnerFrame + 1) % len(spinnerFrames)
			return m, spinnerTick()
		}
		return m, nil

	case browseReadmeMsg:
		m.readme = msg.content
		m.extName = msg.ext.FullName
		m.current = detailView
		m.viewport = viewport.New(m.width, m.height-1)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil

	case installMsg:
		if msg.err != nil {
			m.statusMsg = redStyle.Render("✗") + " Install failed: " + msg.err.Error()
		} else {
			m.statusMsg = greenStyle.Render("✓") + " Installed " + msg.ext.FullName
			for i := range m.items {
				if m.items[i].browse != nil && m.items[i].browse.FullName == msg.ext.FullName {
					m.items[i].browse.Installed = true
				}
			}
		}
		return m, nil

	case versionsMsg:
		for i, ext := range m.extensions {
			if v, ok := msg.versions[ext.Repo]; ok {
				m.extensions[i].LatestVersion = v
			}
		}
		m.rebuildItems()
		return m, nil

	case healthMsg:
		for i, ext := range m.extensions {
			if h, ok := msg.data[ext.Repo]; ok {
				m.extensions[i].Health = &h
			}
		}
		m.rebuildItems()
		return m, nil

	case updateMsg:
		delete(m.updating, msg.ext.Name)
		if msg.err != nil {
			m.statusMsg = redStyle.Render("✗") + " Update failed: " + msg.err.Error()
		} else {
			m.statusMsg = greenStyle.Render("✓") + " Updated " + msg.ext.Name
			m.refreshExtensions()
			return m, tea.Batch(fetchHealth(m.extensions), fetchVersions(m.extensions))
		}
		return m, nil

	case removeMsg:
		if msg.err != nil {
			m.statusMsg = redStyle.Render("✗") + " Remove failed: " + msg.err.Error()
		} else {
			m.statusMsg = greenStyle.Render("✓") + " Removed " + msg.ext.Name
			var newExts []Extension
			for _, ext := range m.extensions {
				if ext.Name != msg.ext.Name {
					newExts = append(newExts, ext)
				}
			}
			m.extensions = newExts
			m.rebuildItems()
		}
		return m, nil

	case updateAllMsg:
		m.updatingAll = false
		m.updating = nil
		if msg.err != nil {
			m.statusMsg = redStyle.Render("✗") + " Update all failed: " + msg.err.Error()
		} else {
			m.statusMsg = greenStyle.Render("✓") + " All extensions updated"
			m.refreshExtensions()
			return m, tea.Batch(fetchHealth(m.extensions), fetchVersions(m.extensions))
		}
		return m, nil

	case pruneMsg:
		if len(msg.removed) == 0 && len(msg.errors) == 0 {
			m.statusMsg = dimStyle.Render("No archived extensions to prune")
		} else {
			parts := []string{}
			if len(msg.removed) > 0 {
				parts = append(parts, greenStyle.Render("✓")+" Pruned "+strings.Join(msg.removed, ", "))
			}
			if len(msg.errors) > 0 {
				parts = append(parts, redStyle.Render("✗")+" Failed: "+strings.Join(msg.errors, ", "))
			}
			m.statusMsg = strings.Join(parts, "  ")
			m.refreshExtensions()
			return m, tea.Batch(fetchHealth(m.extensions), fetchVersions(m.extensions))
		}
		return m, nil

	case convertSearchMsg:
		if msg.err != nil {
			m.statusMsg = redStyle.Render("✗") + " " + msg.err.Error()
			return m, nil
		}
		if msg.candidate == "" {
			m.statusMsg = dimStyle.Render("No official extension found for " + msg.ext.Name)
			return m, nil
		}
		m.confirmConvert = true
		m.convertCandidate = msg.candidate
		m.statusMsg = "Install " + msg.candidate + " to replace local " + msg.ext.Name + "? (I/y to confirm)"
		return m, nil

	case convertMsg:
		m.converting = false
		m.convertCandidate = ""
		if msg.err != nil {
			m.statusMsg = redStyle.Render("✗") + " Convert failed: " + msg.err.Error()
			return m, nil
		}
		m.statusMsg = greenStyle.Render("✓") + " Converted to " + msg.repo
		m.refreshExtensions()
		return m, tea.Batch(fetchHealth(m.extensions), fetchVersions(m.extensions))

	case errMsg:
		m.readme = fmt.Sprintf("Error: %v", msg.err)
		m.current = detailView
		m.viewport = viewport.New(m.width, m.height-1)
		m.viewport.SetContent(m.readme)
		m.ready = true
		return m, nil
	}

	// Pass through to viewport in detail/changelog views
	if m.current != listView {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m *model) rebuildItems() {
	m.items = m.items[:0]
	for i := range m.extensions {
		if m.outdatedOnly && !m.extensions[i].HasUpdate() {
			continue
		}
		m.items = append(m.items, listItem{ext: &m.extensions[i]})
	}
	m.applyFilter()
}

// refreshExtensions reloads from gh extension list, preserving health/version data.
func (m *model) refreshExtensions() {
	old := make(map[string]Extension)
	for _, ext := range m.extensions {
		old[ext.Name] = ext
	}
	m.extensions = getExtensions()
	for i, ext := range m.extensions {
		if prev, ok := old[ext.Name]; ok {
			m.extensions[i].Health = prev.Health
			m.extensions[i].LatestVersion = prev.LatestVersion
		}
	}
	m.rebuildItems()
}

func (m model) View() string {
	if m.current == changelogView {
		hint := dimStyle.Render("esc to go back")
		return hint + "\n" + m.viewport.View()
	}
	if m.current == auditView {
		hint := dimStyle.Render("esc to go back")
		if m.awaitingCopilot {
			label := "Scanning source…"
			if m.auditContent != "" {
				label = "Asking Copilot…"
			}
			spinner := yellowStyle.Render(spinnerFrames[m.spinnerFrame] + " " + label)
			hint += "  " + spinner
		}
		return hint + "\n" + m.viewport.View()
	}
	if m.current == detailView {
		hints := "esc to go back"
		if !m.browseMode {
			hints += " · c changelog · s security audit"
		}
		return dimStyle.Render(hints) + "\n" + m.viewport.View()
	}
	return m.renderList()
}

func (m model) renderList() string {
	var b strings.Builder
	vis := m.visibleLines()

	// Top bar: filter or status
	if m.filtering {
		b.WriteString(cyanStyle.Render("/") + m.filter + cyanStyle.Render("▌") + "\n")
	} else if m.statusMsg != "" {
		b.WriteString(m.statusMsg + "\n")
	} else if m.confirmRemove {
		b.WriteString(yellowStyle.Render(m.statusMsg) + "\n")
	} else {
		count := len(m.filtered)
		label := fmt.Sprintf("Showing %d extension(s)", count)
		if m.outdatedOnly {
			label = fmt.Sprintf("Showing %d with updates", count)
		}
		if m.browseMode {
			label = fmt.Sprintf("Showing %d extension(s)", count)
		}
		b.WriteString(dimStyle.Render(label) + "\n")
	}

	// Items
	end := m.scrollOff + vis
	if end > len(m.filtered) {
		end = len(m.filtered)
	}
	for vi := m.scrollOff; vi < end; vi++ {
		it := m.items[m.filtered[vi]]
		selected := vi == m.cursor

		cursor := "  "
		if selected {
			cursor = greenStyle.Render("> ")
		}

		b.WriteString(cursor)
		b.WriteString(m.renderItem(it, selected))
		b.WriteString("\n")
	}

	// Pad remaining lines
	rendered := end - m.scrollOff
	for i := rendered; i < vis; i++ {
		b.WriteString("\n")
	}

	// Bottom hints
	if m.browseMode {
		b.WriteString(dimStyle.Render("↑↓ navigate · enter view · i install · / filter · q quit"))
	} else {
		hints := "↑↓ navigate · enter view · u update · x remove · p prune"
		if it := m.selectedItem(); it != nil && it.ext != nil && it.ext.Repo == "" {
			hints += " · I install official"
		}
		hints += " · / filter · q quit"
		b.WriteString(dimStyle.Render(hints))
	}

	return b.String()
}

func (m model) renderItem(it listItem, selected bool) string {
	if it.ext != nil {
		return m.renderExtItem(*it.ext, selected)
	}
	return m.renderBrowseItem(*it.browse, selected)
}

func (m model) renderExtItem(ext Extension, selected bool) string {
	name := ext.Name
	if selected {
		name = boldStyle.Render(name)
	}

	// Show updating indicator
	if m.updating[ext.Name] {
		return name + "  " + yellowStyle.Render("↻ updating…")
	}

	var meta []string
	if ext.Repo != "" {
		meta = append(meta, ext.Repo)
	} else {
		meta = append(meta, cyanStyle.Render("⚙ local"))
	}
	if ext.Version != "" {
		meta = append(meta, ext.Version)
	}
	if ext.Health != nil && ext.Health.Stars > 0 {
		meta = append(meta, fmt.Sprintf("★%d", ext.Health.Stars))
	}
	if ext.Health != nil && ext.Health.Archived {
		meta = append(meta, redStyle.Render("archived"))
	}
	if ext.HasUpdate() {
		meta = append(meta, yellowStyle.Render("↑"+ext.LatestVersion))
	}

	return name + "  " + dimStyle.Render(strings.Join(meta, " · "))
}

func (m model) renderBrowseItem(ext BrowseExtension, selected bool) string {
	name := ext.FullName
	if selected {
		name = boldStyle.Render(name)
	}

	var meta []string
	meta = append(meta, fmt.Sprintf("★%d", ext.Stars))
	if ext.Installed {
		meta = append(meta, greenStyle.Render("installed"))
	}
	if ext.Desc != "" {
		desc := ext.Desc
		maxLen := m.width - len(name) - 30
		if maxLen < 20 {
			maxLen = 20
		}
		if len(desc) > maxLen {
			desc = desc[:maxLen-1] + "…"
		}
		meta = append(meta, desc)
	}

	return name + "  " + dimStyle.Render(strings.Join(meta, " · "))
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

// formatRepoHeader renders metadata in gh repo view style.
func formatRepoHeader(ext Extension, info *RepoInfo) string {
	if info == nil {
		return boldStyle.Render(ext.Name) + "\n" + dimStyle.Render("Local extension") + "\n--"
	}

	var lines []string

	// name: owner/repo  (like gh repo view)
	lines = append(lines, dimStyle.Render("name:")+"\t"+boldStyle.Render(ext.Repo))

	if info.Description != "" {
		lines = append(lines, dimStyle.Render("about:")+"\t"+info.Description)
	}

	// metadata line
	var meta []string
	meta = append(meta, fmt.Sprintf("★ %d", info.Stars))
	if info.Language != "" {
		meta = append(meta, info.Language)
	}
	if info.License != nil && info.License.SPDX != "" && info.License.SPDX != "NOASSERTION" {
		meta = append(meta, info.License.SPDX)
	}
	if ext.Version != "" {
		meta = append(meta, ext.Version)
	}
	lines = append(lines, dimStyle.Render("info:")+"\t"+strings.Join(meta, dimStyle.Render(" · ")))

	if info.Archived {
		lines = append(lines, dimStyle.Render("status:")+"\t"+redStyle.Render("archived"))
	}
	if info.UpdatedAt != "" {
		if t, err := time.Parse(time.RFC3339, info.UpdatedAt); err == nil {
			lines = append(lines, dimStyle.Render("updated:")+"\t"+t.Format("Jan 2, 2006"))
		}
	}
	if ext.LatestVersion != "" && ext.Version != ext.LatestVersion {
		lines = append(lines, dimStyle.Render("update:")+"\t"+yellowStyle.Render(ext.Version+" → "+ext.LatestVersion))
	}

	lines = append(lines, "--")
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

		// Fetch repo metadata for the detail header (#4)
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

func fetchHealth(exts []Extension) tea.Cmd {
	return func() tea.Msg {
		result := make(map[string]HealthInfo)
		for _, ext := range exts {
			if ext.Repo == "" {
				continue
			}
			out, err := exec.Command("gh", "api", "repos/"+ext.Repo,
				"--jq", `{archived, pushed_at, stargazers_count, forks_count, open_issues_count}`).Output()
			if err != nil {
				continue
			}
			var raw struct {
				Archived   bool   `json:"archived"`
				PushedAt   string `json:"pushed_at"`
				Stars      int    `json:"stargazers_count"`
				Forks      int    `json:"forks_count"`
				OpenIssues int    `json:"open_issues_count"`
			}
			if err := json.Unmarshal(out, &raw); err != nil {
				continue
			}
			h := HealthInfo{
				Archived:   raw.Archived,
				Stars:      raw.Stars,
				Forks:      raw.Forks,
				OpenIssues: raw.OpenIssues,
			}
			if t, err := time.Parse(time.RFC3339, raw.PushedAt); err == nil {
				h.PushedAt = t
			}
			result[ext.Repo] = h
		}
		return healthMsg{data: result}
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

// extShortName extracts the short name for gh extension commands.
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

func pruneArchived(exts []Extension) tea.Cmd {
	return func() tea.Msg {
		var removed, errors []string
		for _, ext := range exts {
			if ext.Health == nil || !ext.Health.Archived || ext.Repo == "" {
				continue
			}
			name := extShortName(ext)
			cmd := exec.Command("gh", "extension", "remove", name)
			if out, err := cmd.CombinedOutput(); err != nil {
				errors = append(errors, ext.Name+": "+strings.TrimSpace(string(out)))
			} else {
				removed = append(removed, ext.Name)
			}
		}
		return pruneMsg{removed: removed, errors: errors}
	}
}

// searchForOfficialExtension searches GitHub for a published version of a local extension.
func searchForOfficialExtension(ext Extension) tea.Cmd {
	return func() tea.Msg {
		// Extract the bare name (e.g. "agent-viz" from "gh agent-viz")
		bare := strings.TrimPrefix(ext.Name, "gh ")
		query := fmt.Sprintf("gh-%s+topic:gh-extension", bare)
		out, err := exec.Command("gh", "api",
			"search/repositories?q="+query+"&sort=stars&order=desc&per_page=5").Output()
		if err != nil {
			return convertSearchMsg{ext: ext, err: fmt.Errorf("search failed: %w", err)}
		}
		var result searchResult
		if err := json.Unmarshal(out, &result); err != nil {
			return convertSearchMsg{ext: ext, err: fmt.Errorf("parse error: %w", err)}
		}

		// Look for an exact name match (repo name == "gh-<bare>")
		target := "gh-" + bare
		for _, item := range result.Items {
			parts := strings.SplitN(item.FullName, "/", 2)
			if len(parts) == 2 && strings.EqualFold(parts[1], target) {
				return convertSearchMsg{ext: ext, candidate: item.FullName}
			}
		}

		return convertSearchMsg{ext: ext}
	}
}

// convertExtension removes a local extension and installs the official one.
func convertExtension(ext Extension, repo string) tea.Cmd {
	return func() tea.Msg {
		name := extShortName(ext)
		if out, err := exec.Command("gh", "extension", "remove", name).CombinedOutput(); err != nil {
			return convertMsg{ext: ext, repo: repo, err: fmt.Errorf("remove failed: %s: %s", err, strings.TrimSpace(string(out)))}
		}
		if out, err := exec.Command("gh", "extension", "install", repo).CombinedOutput(); err != nil {
			return convertMsg{ext: ext, repo: repo, err: fmt.Errorf("install failed: %s: %s", err, strings.TrimSpace(string(out)))}
		}
		return convertMsg{ext: ext, repo: repo}
	}
}

// securityPatterns defines what to scan for in extension source code.
var securityPatterns = []struct {
	category string
	patterns []string // grep -E patterns
}{
	{"Network access", []string{
		`net/http|net\.Dial|http\.Get|http\.Post|http\.NewRequest|websocket|grpc`,
		`url\.Parse|net\.Listen|tls\.`,
	}},
	{"Command execution", []string{
		`exec\.Command|os/exec|syscall\.Exec|syscall\.ForkExec`,
	}},
	{"File system writes", []string{
		`os\.Create|os\.WriteFile|os\.MkdirAll|os\.Remove|ioutil\.WriteFile|os\.OpenFile`,
		`io\.Copy|bufio\.NewWriter`,
	}},
	{"Credential / token access", []string{
		`os\.Getenv|os\.LookupEnv|keychain|keyring|credential|\.token|api_key|secret`,
		`\.ssh/|\.gitconfig|\.config/gh`,
	}},
	{"Dangerous operations", []string{
		`unsafe\.|reflect\.|cgo|plugin\.Open`,
		`os\.Setenv|os\.Chmod|os\.Chown`,
	}},
}

func runSecurityAudit(ext Extension) tea.Cmd {
	return func() tea.Msg {
		if ext.Repo == "" {
			return auditMsg{content: "Local extension — source not available for audit.", ext: ext}
		}

		// Clone to temp dir
		tmpDir, err := os.MkdirTemp("", "gh-exts-audit-*")
		if err != nil {
			return auditMsg{content: "Failed to create temp directory: " + err.Error(), ext: ext}
		}
		defer os.RemoveAll(tmpDir)

		cloneCmd := exec.Command("gh", "repo", "clone", ext.Repo, tmpDir, "--", "--depth=1")
		if out, err := cloneCmd.CombinedOutput(); err != nil {
			return auditMsg{
				content: "Failed to clone " + ext.Repo + ":\n" + string(out),
				ext:     ext,
			}
		}

		// Scan for patterns
		var findings strings.Builder
		findings.WriteString("# Security Audit: " + ext.Repo + "\n\n")

		totalFindings := 0
		for _, cat := range securityPatterns {
			var catFindings []string
			for _, pat := range cat.patterns {
				cmd := exec.Command("grep", "-rn", "-E", pat, tmpDir,
					"--include=*.go", "--include=*.py", "--include=*.rb",
					"--include=*.sh", "--include=*.js", "--include=*.ts")
				out, _ := cmd.Output()
				if len(out) > 0 {
					for _, line := range strings.Split(strings.TrimSpace(string(out)), "\n") {
						if line == "" {
							continue
						}
						// Strip temp dir prefix for readability
						clean := strings.TrimPrefix(line, tmpDir+"/")
						catFindings = append(catFindings, clean)
					}
				}
			}
			if len(catFindings) > 0 {
				findings.WriteString("## " + cat.category + "\n\n")
				// Deduplicate
				seen := make(map[string]bool)
				for _, f := range catFindings {
					if !seen[f] {
						seen[f] = true
						findings.WriteString("  " + f + "\n")
						totalFindings++
					}
				}
				findings.WriteString("\n")
			}
		}

		if totalFindings == 0 {
			findings.WriteString("No security-relevant patterns found. This extension appears minimal.\n")
		} else {
			findings.WriteString(fmt.Sprintf("---\n\n**%d finding(s)** across source files.\n", totalFindings))
		}

		return auditMsg{content: findings.String(), ext: ext}
	}
}

func fetchCopilotAudit(repo, scanResults string) tea.Cmd {
	return func() tea.Msg {
		prompt := "Analyze these security-relevant code patterns found in the GitHub CLI extension " + repo + ". " +
			"For each category, assess the risk level (low/medium/high) and whether the usage appears benign or suspicious. " +
			"Be concise. Here are the findings:\n\n" + scanResults

		cmd := exec.Command("gh", "copilot", "-p", prompt)
		out, err := cmd.CombinedOutput()
		if err != nil || len(out) == 0 {
			return copilotAuditMsg{analysis: "(Copilot analysis unavailable)"}
		}

		result := string(out)
		if idx := strings.Index(result, "\nTotal usage est:"); idx > 0 {
			result = result[:idx]
		}
		return copilotAuditMsg{analysis: strings.TrimSpace(result)}
	}
}

// --- helpers ---

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

// fuzzyMatch returns extensions whose name contains the query (case-insensitive).
func fuzzyMatch(exts []Extension, query string) []Extension {
	q := strings.ToLower(query)
	var matches []Extension
	for _, ext := range exts {
		name := strings.ToLower(ext.Name)
		bare := strings.TrimPrefix(strings.TrimPrefix(name, "gh "), "gh-")
		if strings.Contains(bare, q) || strings.Contains(name, q) {
			matches = append(matches, ext)
		}
	}
	return matches
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
  gh exts <name>       Jump directly to the detail view for <name>
  gh exts --outdated   Show only extensions with available updates
  gh exts --browse     Browse and install new extensions from GitHub
  gh exts --export     Export install script to stdout
  gh exts --export-json Export extensions as JSON
  gh exts -h           Show help
  gh exts -v           Show version

The <name> argument is fuzzy-matched against installed extensions.
If exactly one extension matches, its README is shown immediately.
If multiple extensions match, the picker opens pre-filtered.

Keys (installed list):
  Enter    View README
  u        Update selected extension
  U        Update all extensions
  x        Remove selected extension (with confirmation)
  I        Install official version of local extension
  p        Prune archived extensions
  /        Filter
  Esc      Go back
  c        Changelog (in detail view)
  s        Security audit (in detail view)
  q        Quit

Keys (browse mode):
  Enter    View README
  i        Install selected extension
  /        Filter
  Esc      Go back
  q        Quit
`, version)
}

func newModel(exts []Extension, outdated, browse bool) model {
	m := model{
		extensions:   exts,
		outdatedOnly: outdated,
		browseMode:   browse,
	}
	m.rebuildItems()
	return m
}

func newBrowseModel(browse []BrowseExtension) model {
	m := model{browseMode: true}
	for i := range browse {
		m.items = append(m.items, listItem{browse: &browse[i]})
	}
	m.applyFilter()
	return m
}

func main() {
	var query string
	outdated := false
	browseMode := false

	for _, arg := range os.Args[1:] {
		switch arg {
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
		case "--outdated":
			outdated = true
		case "--browse":
			browseMode = true
		default:
			if query == "" {
				query = arg
			}
		}
	}

	installed := getExtensions()

	if browseMode {
		browse := getBrowseExtensions(installed)
		if len(browse) == 0 {
			fmt.Println("No extensions found.")
			return
		}
		m := newBrowseModel(browse)
		p := tea.NewProgram(m, tea.WithAltScreen())
		if _, err := p.Run(); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(1)
		}
		return
	}

	if len(installed) == 0 {
		fmt.Println("No extensions installed.")
		return
	}

	displayExts := installed
	if query != "" {
		matches := fuzzyMatch(installed, query)
		switch len(matches) {
		case 0:
			fmt.Fprintf(os.Stderr, "No installed extension matches %q.\nRun 'gh exts' to see all extensions.\n", query)
			os.Exit(1)
		case 1:
			// Exactly one match — jump straight to detail view.
			m := newModel(installed, outdated, false)
			p := tea.NewProgram(m, tea.WithAltScreen())
			go func() {
				p.Send(fetchReadme(matches[0])())
			}()
			if _, err := p.Run(); err != nil {
				fmt.Fprintln(os.Stderr, err)
				os.Exit(1)
			}
			return
		default:
			displayExts = matches
		}
	}

	m := newModel(displayExts, outdated, false)
	// Keep full list for health/version updates
	m.extensions = installed
	p := tea.NewProgram(m, tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	os.Setenv("GH_PAGER", "")
}
