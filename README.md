# gh-exts

Your extensions, but useful.

![demo](demo.gif)

## Install

```bash
gh extension install maxbeizer/gh-exts
```

## Usage

```bash
gh exts              # show all installed extensions with details
gh exts <name>       # show details for one extension
```

## What It Shows

For each installed extension:
- **Description** from the repo
- **Language**, **stars**, **license**
- **Archived** status if applicable

For a single extension (`gh exts <name>`):
- All the above, plus **README excerpt** and **URL**

Basically `gh extension list` but actually useful.

## Uninstall

```bash
gh extension remove exts
```

## License

MIT
