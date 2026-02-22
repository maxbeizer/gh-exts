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
gh exts <name>       # jump directly to an extension's README
gh exts -h           # show help
gh exts -v           # show version
```

The `<name>` argument is fuzzy-matched against your installed extensions:

- **One match** → shows the README detail view immediately
- **Multiple matches** → opens the picker pre-filtered to just those matches
- **No matches** → exits with a helpful error

For example, `gh exts contrib` will jump straight to `gh-contrib`'s README.

## How It Works

1. Lists all your installed extensions in a filterable picker (or jumps directly if a name argument is given)
2. Type `/` to search/filter by name
3. Press `Enter` to view the full README (rendered with glamour)
4. Press `Esc` to go back, `q` to quit

Basically `gh extension list` but with depth.

## Uninstall

```bash
gh extension remove exts
```

## License

MIT
