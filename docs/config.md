Configuration
=============

This document describes how to configure [yactionlint](../README.md) behavior.

Note that configuration file is optional. The author tries to keep configuration file as minimal as possible not to
bother users to configure behavior of yactionlint. Running yactionlint without configuration file would work fine in most
cases.

## Configuration file

Configuration file `yactionlint.yaml` or `yactionlint.yml` can be put in `.github` directory.

Note: If you're using [Super-Linter][], the file should be placed in a different directory. Please check the project's document.

```yaml
# Configuration related to self-hosted runner.
self-hosted-runner:
  # Labels of self-hosted runner in array of strings.
  labels:
    - linux.2xlarge
    - windows-latest-xl
    - linux-multi-gpu

# Configuration variables in array of strings defined in your repository or organization.
config-variables:
  - DEFAULT_RUNNER
  - JOB_NAME
  - ENVIRONMENT_STAGE

# Optional allow-list. The built-in GITHUB_TOKEN is always available.
config-secrets:
  - DEPLOY_TOKEN

# Optional policies; leave them unset to preserve the default checks.
#required-actions:
#  - action: actions/checkout
#    version: v4
#require-commit-hash: true
#require-exact-action-version: true
#require-explicit-if-expressions: true
#require-permissions: true
#require-explicit-permissions: true
#timeout-minutes:
#  required: true
#  max: 60
#assume-default-permissions: restricted

# Path-specific configurations.
paths:
  # Glob pattern relative to the repository root for matching files. The path separator is always '/'.
  # This example configures any YAML file under the '.github/workflows/' directory.
  .github/workflows/**/*.{yml,yaml}:
    # List of regular expressions to filter errors by the error messages.
    ignore:
      # Ignore the specific error from shellcheck
      - 'shellcheck reported issue in this script: SC2086:.+'
  # This pattern only matches '.github/workflows/release.yaml' file.
  .github/workflows/release.yaml:
    ignore:
      # Ignore errors from the old runner check. This may be useful for (outdated) self-hosted runner environment.
      - 'the runner of ".+" action is too old to run on GitHub Actions'
```

- `self-hosted-runner`: Configuration for your self-hosted runner environment.
  - `labels`: Label names added to your self-hosted runners as list of pattern. Glob syntax supported by [`path.Match`][pat]
    is available.
- `config-variables`: [Configuration variables][vars]. When an array is set, yactionlint will check `vars` properties strictly.
  An empty array means no variable is allowed. The default value `null` disables the check.
- `config-secrets`: Optional allow-list for `secrets.NAME` references; `null` disables this check and `[]` rejects
  all names except the built-in `GITHUB_TOKEN`. Name comparison is case-insensitive.
- `required-actions`: Actions that each workflow must use. An `action` entry names `owner/repository[/path]`;
  an optional `version` requires at least one matching `uses:` reference with that exact ref.
- `require-commit-hash`: Require a 40-character commit SHA for repository action refs, including actions used
  inside local composite actions. Local and Docker refs are excluded.
- `require-exact-action-version`: Require a complete version tag such as `v4.1.2` or a 40-character commit SHA.
  Major-only tags and branch names are rejected. Local and Docker refs are excluded.
- `require-explicit-if-expressions`: Require `${{ ... }}` around job and step `if:` expressions.
- `require-permissions`: Require a workflow-level `permissions:` declaration.
- `require-explicit-permissions`: For workflows with more than one job, require `permissions: {}` at the
  workflow level and an explicit `permissions:` declaration on every job. These policies are opt-in.
- `timeout-minutes`: `required: true` requires each regular job to set a timeout. `max` rejects constant job
  timeouts above the specified number of minutes; expressions cannot be compared statically. A maximum of
  zero disables the limit.
- `assume-default-permissions`: Controls how an otherwise undeclared caller token is modeled when checking
  permissions requested by a local reusable workflow. `restricted` (default) assumes GitHub's restricted
  defaults; `permissive` assumes write grants except `id-token`, which must always be granted explicitly.
- `paths`: Configurations for specific file path patterns. This is a mapping from a glob pattern and the corresponding
  configuration.
  - `{glob}`: A file path glob pattern to apply the configuration. The path separator is always '/'. It is matched to the
    relative path from the repository root. For example `.github/workflows/**/*.yaml` matches all the workflow files (with
    `.yaml` file extension). For the glob syntax, please read the [doublestar][] library's documentation.
    - `ignore`: The configuration to ignore (filter) the errors by the error messages. This is an array of regular
      expressions. When one of the patterns matches the error message, the error will be ignored. It's similar to the
      `-ignore` command line option.

## Generate the initial configuration

You don't need to write the first configuration file by your hand. `yactionlint` command can generate a default configuration
with `-init-config` flag.

```sh
yactionlint -init-config
vim .github/yactionlint.yaml
```

---

[Checks](checks.md) | [Installation](install.md) | [Usage](usage.md) | [Go API](api.md) | [References](reference.md)

[Super-Linter]: https://github.com/super-linter/super-linter
[pat]: https://pkg.go.dev/path#Match
[vars]: https://docs.github.com/en/actions/learn-github-actions/variables
[doublestar]: https://github.com/bmatcuk/doublestar
