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
gh exts --browse     # browse and install new extensions from GitHub
gh exts -h           # show help
gh exts -v           # show version
```

## How It Works

1. Lists all your installed extensions in a filterable picker
2. Type `/` to search/filter by name
3. Press `Enter` to view the full README (rendered with glamour)
4. Press `Esc` to go back, `q` to quit

### Browse Mode (`--browse`)

1. Searches GitHub for repositories tagged with `gh-extension`, sorted by stars
2. Shows results in the same picker UI with an `[installed]` indicator
3. Press `i` to install the selected extension
4. Press `Enter` to view the README before installing

Basically `gh extension list` but with depth.

## Uninstall

```bash
gh extension remove exts
```

## License

MIT
