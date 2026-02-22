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
gh exts --outdated   # show only extensions with available updates
gh exts -h           # show help
gh exts -v           # show version
```

## How It Works

1. Lists all your installed extensions in a filterable picker
2. Shows update availability for each extension (e.g. "v0.4.0 → v0.5.0 available")
3. Type `/` to search/filter by name
4. Press `Enter` to view the full README (rendered with glamour)
5. Press `Esc` to go back, `q` to quit
6. Use `--outdated` to see only extensions with pending updates

Basically `gh extension list` but with depth.

## Uninstall

```bash
gh extension remove exts
```

## License

MIT
