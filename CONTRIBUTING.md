# Contributing to Samoyed

## Commit Messages and PR Titles

This project uses [Conventional Commits](https://www.conventionalcommits.org/).

Format: `<type>[optional scope]: <description>`

Examples:
- `feat: add IL2P protocol support`
- `fix(audio): correct buffer overflow`
- `docs: update installation guide`

Common types: `feat`, `fix`, `docs`, `style`, `refactor`, `perf`, `test`, `build`, `ci`, `chore`, `revert`

PR titles are automatically validated - the CI check will fail if they don't follow this format.

## Development

1. Fork the repository
2. Create a feature branch
3. Make your changes
4. Ensure tests pass: `make test`
5. Ensure linting passes: `make check`
6. Submit a PR with a conventional commit title

