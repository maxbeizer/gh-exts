# Contributing to gh-exts

Thanks for your interest in contributing! Here's how to get started.

## Development

```bash
git clone https://github.com/maxbeizer/gh-exts.git
cd gh-exts
make build
make install-local
```

## Making Changes

1. Fork the repo and create a feature branch
2. Make your changes
3. Run `make ci` to build, vet, and test
4. Open a pull request

## Reporting Issues

Open an issue on GitHub. Include:
- What you expected to happen
- What actually happened
- Your `gh --version` and `go version` output

## Code Style

- Run `make fmt` before committing
- Keep it simple — this is a small tool
