package main

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/list"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/glamour"
	"github.com/charmbracelet/lipgloss"
)

const version = "0.3.0"

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
	return e.Version != "" && e.LatestVersion != "" && e.Version != e.LatestVersion
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

type errMsg struct{ err error }

// --- model ---

type viewState int

const (
	listView viewState = iota
	detailView
	changelogView
)

// ghDelegate renders list items in a minimal gh-CLI style.
type ghDelegate struct {
	extensions []Extension // for health/version enrichment on installed items
}

func (d ghDelegate) Height() int                               { return 2 }
func (d ghDelegate) Spacing() int                              { return 0 }
func (d ghDelegate) Update(_ tea.Msg, _ *list.Model) tea.Cmd   { return nil }

func (d ghDelegate) Render(w io.Writer, m list.Model, index int, item list.Item) {
	selected := index == m.Index()
	cursor := "  "
	if selected {
		cursor = greenStyle.Render("▸ ")
	}

	switch it := item.(type) {
	case Extension:
		// Enrich with health/version data from model's extensions slice
		for _, ext := range d.extensions {
			if ext.Name == it.Name {
				it = ext
				break
			}
		}

		name := it.Name
		if selected {
			name = boldStyle.Render(name)
		}

		// Build metadata parts
		var meta []string
		if it.Repo != "" {
			meta = append(meta, it.Repo)
		} else {
			meta = append(meta, "local")
		}
		if it.Version != "" {
			meta = append(meta, it.Version)
		}
		if it.Health != nil {
			if it.Health.Stars > 0 {
				meta = append(meta, fmt.Sprintf("★%d", it.Health.Stars))
			}
			if it.Health.Archived {
				meta = append(meta, redStyle.Render("archived"))
			} else if !it.Health.PushedAt.IsZero() && it.Health.PushedAt.Before(time.Now().AddDate(0, -6, 0)) {
				meta = append(meta, yellowStyle.Render("stale"))
			}
		}
		if it.HasUpdate() {
			meta = append(meta, yellowStyle.Render("↑ "+it.LatestVersion))
		}

		metaStr := dimStyle.Render(strings.Join(meta, " · "))
		fmt.Fprintf(w, "%s%s\n  %s\n", cursor, name, metaStr)

	case BrowseExtension:
		name := it.FullName
		if selected {
			name = boldStyle.Render(name)
		}
		var meta []string
		meta = append(meta, fmt.Sprintf("★%d", it.Stars))
		if it.Installed {
			meta = append(meta, greenStyle.Render("installed"))
		}
		if it.Desc != "" {
			// Truncate description to fit
			desc := it.Desc
			if len(desc) > 60 {
				desc = desc[:57] + "…"
			}
			meta = append(meta, desc)
		}
		metaStr := dimStyle.Render(strings.Join(meta, " · "))
		fmt.Fprintf(w, "%s%s\n  %s\n", cursor, name, metaStr)
	}
}

// newStyledList creates a list with gh-native styling.
func newStyledList(items []list.Item, title string, exts []Extension) list.Model {
	d := ghDelegate{extensions: exts}
	l := list.New(items, d, 80, 24)
	l.Title = title
	l.SetShowStatusBar(true)
	l.SetFilteringEnabled(true)
	l.SetShowHelp(true)

	// gh-native title style: plain bold, no background
	l.Styles.Title = boldStyle
	l.Styles.TitleBar = lipgloss.NewStyle().Padding(0, 0, 1, 0)

	// Minimal filter styling
	l.Styles.FilterPrompt = cyanStyle
	l.Styles.FilterCursor = cyanStyle

	return l
}

type model struct {
	list          list.Model
	viewport      viewport.Model
	current       viewState
	readme        string
	extName       string
	currentExt    Extension
	width         int
	height        int
	ready         bool
	extensions    []Extension
	outdatedOnly  bool
	browseMode    bool
	statusMsg     string
	confirmRemove bool
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
		// Don't intercept keys when the list is filtering.
		if m.current == listView && m.list.FilterState() == list.Filtering {
			var cmd tea.Cmd
			m.list, cmd = m.list.Update(msg)
			return m, cmd
		}

		// Handle confirm-remove state (#6).
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
		case "c":
			if m.current == detailView && !m.browseMode {
				if m.currentExt.Repo != "" {
					return m, fetchChangelog(m.currentExt)
				}
			}
		case "i":
			if m.current == listView && m.browseMode {
				if item, ok := m.list.SelectedItem().(BrowseExtension); ok {
					if item.Installed {
						m.list.NewStatusMessage(item.FullName + " is already installed")
						return m, nil
					}
					m.list.NewStatusMessage("Installing " + item.FullName + "...")
					return m, installExtension(item)
				}
			}
		case "u":
			if m.current == listView && !m.browseMode {
				if item, ok := m.list.SelectedItem().(Extension); ok {
					m.list.NewStatusMessage("Updating " + item.Name + "…")
					return m, updateExtension(item)
				}
			}
		case "U":
			if m.current == listView && !m.browseMode {
				m.list.NewStatusMessage("Updating all extensions…")
				return m, updateAllExtensions()
			}
		case "x":
			if m.current == listView && !m.browseMode {
				if item, ok := m.list.SelectedItem().(Extension); ok {
					m.confirmRemove = true
					m.list.NewStatusMessage("Remove " + item.Name + "? Press x/y to confirm, any other key to cancel.")
				}
				return m, nil
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
		header := formatRepoHeader(msg.ext, msg.repoInfo)
		m.readme = header + "\n\n" + msg.content
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

	case versionsMsg:
		for i, ext := range m.extensions {
			if v, ok := msg.versions[ext.Repo]; ok {
				m.extensions[i].LatestVersion = v
			}
		}
		return m, m.rebuildList()

	case healthMsg:
		for i, ext := range m.extensions {
			if h, ok := msg.data[ext.Repo]; ok {
				m.extensions[i].Health = &h
			}
		}
		return m, m.rebuildList()

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
			var newExts []Extension
			for _, ext := range m.extensions {
				if ext.Name != msg.ext.Name {
					newExts = append(newExts, ext)
				}
			}
			m.extensions = newExts
			return m, m.rebuildList()
		}
		return m, nil

	case updateAllMsg:
		if msg.err != nil {
			m.list.NewStatusMessage("✗ Update all failed: " + msg.err.Error())
		} else {
			m.list.NewStatusMessage("✓ All extensions updated")
		}
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

// rebuildList refreshes the list items from m.extensions, applying outdated filter.
func (m *model) rebuildList() tea.Cmd {
	var items []list.Item
	for _, ext := range m.extensions {
		if m.outdatedOnly && !ext.HasUpdate() {
			continue
		}
		items = append(items, ext)
	}
	title := fmt.Sprintf("Showing %d extension(s)", len(items))
	if m.outdatedOnly {
		title = fmt.Sprintf("Showing %d extension(s) with updates", len(items))
	}
	m.list.Title = title
	m.list.SetDelegate(ghDelegate{extensions: m.extensions})
	return m.list.SetItems(items)
}

func (m model) View() string {
	if m.current == changelogView {
		header := boldStyle.Render(m.extName+" — Changelog") +
			dimStyle.Render("  esc to go back")
		return lipgloss.NewStyle().Margin(1, 2).Render(header + "\n\n" + m.viewport.View())
	}
	if m.current == detailView {
		hints := "esc to go back"
		if !m.browseMode {
			hints += " · c changelog"
		}
		hint := dimStyle.Render(hints)
		return lipgloss.NewStyle().Margin(1, 2).Render(hint + "\n" + m.viewport.View())
	}
	return lipgloss.NewStyle().Margin(0, 2).Render(m.list.View())
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
  /        Filter
  Esc      Go back
  c        Changelog (in detail view)
  q        Quit

Keys (browse mode):
  Enter    View README
  i        Install selected extension
  /        Filter
  Esc      Go back
  q        Quit
`, version)
}

func extHelpKeys() []key.Binding {
	return []key.Binding{
		key.NewBinding(key.WithKeys("u"), key.WithHelp("u", "update")),
		key.NewBinding(key.WithKeys("U"), key.WithHelp("U", "update all")),
		key.NewBinding(key.WithKeys("x"), key.WithHelp("x", "remove")),
	}
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

	// Browse mode (#5)
	if browseMode {
		browse := getBrowseExtensions(installed)
		if len(browse) == 0 {
			fmt.Println("No extensions found.")
			return
		}
		items := make([]list.Item, len(browse))
		for i, b := range browse {
			items[i] = b
		}

		l := newStyledList(items, fmt.Sprintf("Browse extensions · %d found · i install · enter readme", len(browse)), nil)

		m := model{list: l, browseMode: true}

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

	// Direct argument jump (#1)
	displayExts := installed
	if query != "" {
		matches := fuzzyMatch(installed, query)
		switch len(matches) {
		case 0:
			fmt.Fprintf(os.Stderr, "No installed extension matches %q.\nRun 'gh exts' to see all extensions.\n", query)
			os.Exit(1)
		case 1:
			// Exactly one match — jump straight to detail view.
			items := make([]list.Item, len(installed))
			for i, e := range installed {
				items[i] = e
			}
			l := newStyledList(items, fmt.Sprintf("Showing %d extension(s)", len(installed)), installed)
			l.AdditionalShortHelpKeys = extHelpKeys

			m := model{list: l, extensions: installed, outdatedOnly: outdated}
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

	items := make([]list.Item, len(displayExts))
	for i, e := range displayExts {
		items[i] = e
	}

	title := fmt.Sprintf("Showing %d extension(s)", len(displayExts))
	if outdated {
		title = "Checking for updates…"
	}
	l := newStyledList(items, title, installed)
	l.AdditionalShortHelpKeys = extHelpKeys

	m := model{list: l, extensions: installed, outdatedOnly: outdated}

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
