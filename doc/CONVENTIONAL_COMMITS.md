# Conventional Commits

This project follows the [Conventional Commits](https://www.conventionalcommits.org/) specification for commit messages and PR titles.

## Format

Commit messages and PR titles should follow this format:

```
<type>[optional scope]: <description>

[optional body]

[optional footer(s)]
```

### Examples

- `feat: add support for IL2P protocol`
- `fix: correct audio buffer overflow`
- `docs: update README with installation instructions`
- `refactor: simplify APRS packet parsing`
- `test: add unit tests for FX.25 decoder`
- `ci: update GitHub Actions workflow`
- `chore: update dependencies`

## Types

The following types are allowed:

- **feat**: A new feature
- **fix**: A bug fix
- **docs**: Documentation only changes
- **style**: Changes that do not affect the meaning of the code (white-space, formatting, etc)
- **refactor**: A code change that neither fixes a bug nor adds a feature
- **perf**: A code change that improves performance
- **test**: Adding missing tests or correcting existing tests
- **build**: Changes that affect the build system or external dependencies
- **ci**: Changes to our CI configuration files and scripts
- **chore**: Other changes that don't modify src or test files
- **revert**: Reverts a previous commit

## Validation

PR titles are automatically validated to ensure they follow the conventional commits format. Individual commit messages within a PR are also checked.

If your PR title doesn't follow the format, the CI check will fail and you'll need to update it before merging.

## Why?

Using conventional commits allows us to:

- Automatically determine version bumps (semantic versioning)
- Generate changelogs automatically
- Differentiate which PRs should trigger releases after merge
- Make the git history more readable and meaningful

## Learn More

For more details, see the [Conventional Commits specification](https://www.conventionalcommits.org/).
