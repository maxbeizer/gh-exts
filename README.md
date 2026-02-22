# gh-exts

Your extensions, in depth. An interactive browser for your installed `gh` extensions.

![demo](demo.gif)

## Install

```bash
gh extension install maxbeizer/gh-exts
```

## Usage

```bash
gh exts              # launch interactive extension browser
gh exts --export     # export install script to stdout
gh exts --export-json # export extensions as JSON
gh exts -h           # show help
gh exts -v           # show version
```

## How It Works

1. Lists all your installed extensions in a filterable picker
2. Shows health indicators for each extension:
   - 🗄️ if the repository is archived
   - ⚠️ if there has been no push in 6+ months (stale)
   - ★ star count
   - Current version
3. Type `/` to search/filter by name
4. Press `Enter` to view the full README (rendered with glamour)
5. Press `Esc` to go back, `q` to quit

Health metadata is fetched concurrently on startup via `gh api`.

## Uninstall

```bash
gh extension remove exts
```

## License

MIT
