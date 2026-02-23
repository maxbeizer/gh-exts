# gh-exts

Your extensions, in depth. An interactive browser for your installed `gh` extensions — with health indicators, update checks, extension management, and discovery.

![demo](demo.gif)

## Install

```bash
gh extension install maxbeizer/gh-exts
```

## Usage

```bash
gh exts                # launch interactive extension browser
gh exts <name>         # jump directly to the detail view for <name>
gh exts --outdated     # show only extensions with available updates
gh exts --browse       # browse and install new extensions from GitHub
gh exts --export       # export install script to stdout
gh exts --export-json  # export extensions as JSON
gh exts -h             # show help
gh exts -v             # show version
```

### Direct Argument

Pass a name to fuzzy-match against installed extensions. If exactly one matches, its README opens immediately. If multiple match, the picker opens pre-filtered.

```bash
gh exts contrib        # jumps straight to gh-contrib's README
```

### Outdated Mode

Show only extensions that have newer versions available on GitHub:

```bash
gh exts --outdated
```

### Browse & Install

Discover popular `gh` extensions from GitHub and install them interactively:

```bash
gh exts --browse
```

Press `i` on any extension to install it.

### Export

Back up your extension list for easy reinstall on another machine:

```bash
gh exts --export > install-exts.sh   # shell script
gh exts --export-json > exts.json    # JSON manifest
```

## Keybindings

### Installed Extensions List

| Key     | Action                                  |
|---------|-----------------------------------------|
| `Enter` | View README (with repo metadata header) |
| `c`     | View changelog (in detail view)         |
| `s`     | Security audit (in detail view)         |
| `u`     | Update selected extension               |
| `U`     | Update all extensions                   |
| `x`     | Remove selected extension (confirm)     |
| `p`     | Prune all archived extensions            |
| `/`     | Search / filter by name                 |
| `Esc`   | Go back                                 |
| `q`     | Quit                                    |

### Browse Mode

| Key     | Action                     |
|---------|----------------------------|
| `Enter` | View README                |
| `i`     | Install selected extension |
| `/`     | Search / filter            |
| `Esc`   | Go back                    |
| `q`     | Quit                       |

## Features

- **Health indicators** — each extension shows ★ stars and archived status
- **Update checks** — shows `↑v1.1` when a newer release is available
- **Repo metadata** — detail view header shows description, stars, language, license, last updated
- **Changelog** — press `c` to see releases newer than your installed version
- **Security audit** — press `s` to scan extension source for security-relevant patterns (network, exec, credentials). Uses Copilot for analysis if available
- **Manage** — update (`u`), update all (`U`), or remove (`x`) extensions without leaving the TUI
- **Browse** — discover and install popular extensions from GitHub
- **Direct jump** — `gh exts <name>` for instant README access
- **Export** — back up your extension list as a shell script or JSON

## Uninstall

```bash
gh extension remove exts
```

## License

MIT
