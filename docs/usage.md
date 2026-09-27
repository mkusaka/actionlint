Usage
=====

This document describes how to use [yactionlint](../README.md).

## `yactionlint` command

With no argument, yactionlint finds all workflow files in the current repository and checks them.

```sh
yactionlint
```

When paths to YAML workflow files are given as arguments, yactionlint checks them.

```sh
yactionlint path/to/workflow1.yaml path/to/workflow2.yaml
```

When `-` argument is given, yactionlint reads inputs from stdin and checks it as workflow source.

```sh
cat path/to/workflow.yaml | yactionlint -
```

To know all flags and options, run `yactionlint -h`.

### Ignore some errors

To ignore some errors, `-ignore` option offers to filter errors by messages using regular expression. The option is repeatable.
The regular expression syntax is the same as [RE2][re2].

```sh
yactionlint -ignore 'label ".+" is unknown' -ignore '".+" is potentially untrusted'
```

`-shellcheck` and `-pyflakes` specifies file paths of executables. Setting empty string to them disables `shellcheck` and
`pyflakes` rules. As a bonus, disabling them makes yactionlint much faster Since these external linter integrations spawn many
processes.

```sh
yactionlint -shellcheck= -pyflakes=
```

<a id="format"></a>
### Format error messages

`-format` option can flexibly format error messages with [Go template syntax][go-template].

Before explaining the formatting details, let's see some examples.

#### Example: Serialized into JSON

```sh
yactionlint -format '{{json .}}'
```

Output:

```
[{"message":"unexpected key \"branch\" for ...
```

#### Example: Markdown

````sh
yactionlint -format '{{range $err := .}}### Error at line {{$err.Line}}, col {{$err.Column}} of `{{$err.Filepath}}`\n\n{{$err.Message}}\n\n```\n{{$err.Snippet}}\n```\n\n{{end}}'
````

Output:

````markdown
### Error at line 21, col 20 of `test.yaml`

property "platform" is not defined in object type {os: string}

```
          key: ${{ matrix.platform }}-node-${{ hashFiles('**/package-lock.json') }}
                   ^~~~~~~~~~~~~~~
```
````

#### Example: Serialized in [JSON Lines][jsonl]

```sh
yactionlint -format '{{range $err := .}}{{json $err}}{{end}}'
```

Output:

```
{"message":"unexpected key \"branch\" for ...
{"message":"character '\\' is invalid for branch ...
{"message":"label \"linux-latest\" is unknown. ...
```

#### Example: [Error annotation][ga-annotate-error] on GitHub Actions

````sh
yactionlint -format '{{range $err := .}}::error file={{$err.Filepath}},line={{$err.Line}},col={{$err.Column}}::{{$err.Message}}%0A```%0A{{replace $err.Snippet "\\n" "%0A"}}%0A```\n{{end}}' -ignore 'SC2016:'
````

Output:

<img src="https://github.com/rhysd/ss/blob/master/actionlint/ga-annotate.png?raw=true" alt="annotations on GitHub Actions" width="731" height="522"/>

To include newlines in the annotation body, it prints `%0A`. (ref [actions/toolkit#193](https://github.com/actions/toolkit/issues/193)).
And it suppresses `SC2016` shellcheck rule error since it complains about the template argument.

Basically it is more recommended to use [Problem Matchers](#problem-matchers) or reviewdog as explained in
['Tools integration' section](#tools-integ) below.

#### Example: [SARIF format][sarif]

[The Static Analysis Results Interchange Format (SARIF)][sarif] is a standardized format for the results of static analysis tools.

Since this practical format is much more complex than the above examples, the template is not written here. Please read
[the template file in test data](../testdata/format/sarif_template.txt).

Outputs are also too large to be written here. Please read [the output example in test data](../testdata/format/test.sarif).

#### Formatting syntax

In [Go template syntax][go-template], `.` within `{{ }}` means the target object. Here, the target object is a sequence of error
objects.

The sequence can be traversed with `range` action, which is like `for ... = range ... {}` in Go.

```
{{range $err := .}} this part iterates error objects with the iteration variable $err {{end}}
```

The error object has the following fields.

| Field                | Description                                           | Example                                                          |
|----------------------|-------------------------------------------------------|------------------------------------------------------------------|
| `{{$err.Message}}`   | Body of error message                                 | `property "platform" is not defined in object type {os: string}` |
| `{{$err.Snippet}}`   | Code snippet to indicate error position               | `          node_version: 16.x\n          ^~~~~~~~~~~~~`          |
| `{{$err.Kind}}`      | Name of rule the error belongs to                     | `expression`                                                     |
| `{{$err.Filepath}}`  | Canonical relative file path of the error position    | `.github/workflows/ci.yaml`                                      |
| `{{$err.Line}}`      | Line number of the error position (1-based)           | `9`                                                              |
| `{{$err.Column}}`    | Column number of the error's start position (1-based) | `11`                                                             |
| `{{$err.EndColumn}}` | Column number of the error's end position (1-based)   | `23`                                                             |

Functions called in `{{ }}` placeholder are template actions. There are many actions defined by Go standard library. In addition,
there are a few custom actions defined by yactionlint. Most useful action would be `json` as we already used it in the above JSON
example. List of all custom actions are as follows:

| Action           | Description                                                                      | Example usage                             |
|------------------|----------------------------------------------------------------------------------|-------------------------------------------|
| `json x`         | Serialize `x` as JSON string followed by newline character                       | `{{json $err}}`                           |
| `replace x y z`  | Replace string `y` with `z` in `x`                                               | `{{replace $err.Filepath "\\" "/"}}`      |
| `toPascalCase x` | Convert `x` into PascalCase (e.g. 'foo-bar' to 'FooBar')                         | `{{toPascalCase $err.Kind}}`              |
| `allKinds`       | Return an array of kind objects. The kind object is explained in the below table | `{{range $ = allKinds}}{{$.Name}}{{end}}` |
| `getVersion`     | Return the version of yactionlint as string                                      | `{{getVersion}}`                          |

The kind object returned from `allKinds` action has the following fields.

| Field                   | Description                   | Example                                     |
|-------------------------|-------------------------------|---------------------------------------------|
| `{{$kind.Name}}`        | Name of the kind              | `syntax-check`                              |
| `{{$kind.Description}}` | Short description of the kind | `Checks for GitHub Actions workflow syntax` |

For example, the following simple iteration body

```
line is {{$err.Line}}, col is {{$err.Column}}, message is {{$err.Message | printf "%q"}}
```

will produce output like below.

```
line is 21, col is 20, message is "property \"platform\" is not defined in object type {os: string}"
```

In `{{ }}` placeholder, input can be piped and action can be used to transform texts. In above example, the message is piped with
`|` and transformed with `printf "%q"`.

Note that special characters escaped with backslash like `\n` in the format string are automatically unescaped.

### Exit status

`yactionlint` command exits with one of the following exit statuses.

| Status | Description                                             |
|--------|---------------------------------------------------------|
| `0`    | The command ran successfully and no problem was found   |
| `1`    | The command ran successfully and some problem was found |
| `2`    | The command failed due to invalid command line option   |
| `3`    | The command failed due to some fatal error              |

<a id="on-github-actions"></a>
## Use yactionlint on GitHub Actions

Preparing `yactionlint` executable with the download script is recommended. See [the instruction](install.md#download-script) for
more details. It sets an absolute file path of downloaded executable to `executable` output in order to use the executable in the
following steps easily.

Here is an example workflow using yactionlint's release binary. Use `shell: bash` since the default shell for Windows runners is `pwsh`.

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]

jobs:
  yactionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - name: Download yactionlint
        id: get_yactionlint
        run: bash <(curl -fsSL https://raw.githubusercontent.com/mkusaka/yactionlint/main/scripts/download-yactionlint.bash)
        shell: bash
      - name: Check workflow files
        run: ${{ steps.get_yactionlint.outputs.executable }} -color
        shell: bash
```

Or simply download the executable and run it in one step:

```yaml
- name: Check workflow files
  run: |
    bash <(curl -fsSL https://raw.githubusercontent.com/mkusaka/yactionlint/main/scripts/download-yactionlint.bash)
    ./yactionlint -color
  shell: bash
```

The download script allows to specify the version of yactionlint and the download directory. Try to give `--help` argument
to the script for more usage details.

If you want to enable [shellcheck integration](checks.md#check-shellcheck-integ), install `shellcheck` command. Note that
shellcheck is [pre-installed on Ubuntu worker][preinstall-ubuntu].

If you want to [annotate errors][ga-annotate-error] from yactionlint on GitHub, consider using
[Problem Matchers](#problem-matchers).

The existing upstream Docker image is an external alternative for upstream actionlint, not yactionlint. See
[the upstream actionlint Docker image](#docker).

```yaml
name: Lint GitHub Actions workflows
on: [push, pull_request]

jobs:
  actionlint:
    runs-on: ubuntu-latest
    steps:
      - uses: actions/checkout@v6
      - name: Check workflow files
        uses: docker://rhysd/actionlint:latest
        with:
          args: -color
```

## Online playground

Thanks to WebAssembly, the upstream actionlint playground is available externally in your browser. It never sends any data outside your browser.

https://rhysd.github.io/actionlint/

Paste your workflow content to the code editor at left pane. It automatically shows the results at right pane. When editing
the workflow content in the code editor, the results will be updated on the fly. Clicking an error message in the results
table moves a cursor to position of the error in the code editor.

<a id="docker"></a>
## [Upstream actionlint Docker image (external)][docker]

[The upstream actionlint Docker image][docker-image] contains `actionlint` and its dependencies (shellcheck and pyflakes).
It does not contain yactionlint. For yactionlint, use the fork's binary or build a container from this repository.

Available upstream tags are:

- `actionlint:latest`: Latest stable version of upstream actionlint.
- `actionlint:{version}`: A specific upstream actionlint version (for example, `actionlint:1.7.12`).

```sh
docker run --rm rhysd/actionlint:latest -version
```

To check all workflows in your repository with upstream actionlint, mount your repository's root directory as a volume:

```sh
docker run --rm -v $(pwd):/repo --workdir /repo rhysd/actionlint:latest -color
```

To check a file with upstream actionlint in a Docker container, pass the file content via stdin:

```sh
cat /path/to/workflow.yml | docker run --rm -i rhysd/actionlint:latest -color -
```

## Using yactionlint from a Go program

Go APIs are available. See [the Go API document](api.md) for more details.


<a id="tools-integ"></a>
## Tools integration

Unless noted otherwise, the third-party integrations below support upstream `actionlint`, not yactionlint. They do not install
or invoke yactionlint.

### reviewdog

[reviewdog][] officially [supports upstream actionlint][reviewdog-actionlint]. Use yactionlint's download script separately if
you need the fork's checks.

### Problem Matchers

Copy [yactionlint-matcher.json][yactionlint-matcher] to `.github/yactionlint-matcher.json` in your repository.

Then enable the matcher using `add-matcher` command before running yactionlint in the step of your workflow.

```yaml
- name: Check workflow files
  run: |
    echo "::add-matcher::.github/yactionlint-matcher.json"
    bash <(curl -fsSL https://raw.githubusercontent.com/mkusaka/yactionlint/main/scripts/download-yactionlint.bash)
    ./yactionlint -color
  shell: bash
```

When you change your workflow and the changed line causes a new error, CI will annotate the diff with the extracted error message.

<img src="https://github.com/rhysd/ss/blob/master/actionlint/problem-matcher.png?raw=true" alt="Upstream actionlint Problem Matcher annotation" width="715" height="221"/>

### super-linter

[super-linter][] supports upstream actionlint only. It does not run yactionlint.

### pre-commit

[pre-commit][] provides upstream actionlint hooks; they do not install or run yactionlint.

### Editors and Trunk

The [VS Code extension][vsc-extension], Emacs packages, [nvim-lint][], [ALE][vim-ale], the [Pulsar linter][pulsar-linter],
[Actionlint for Nova][nova-extension], and [Trunk][trunk-io] integrations target upstream actionlint. Configure yactionlint
manually if your editor or tool supports a custom executable.

---

[Checks](checks.md) | [Installation](install.md) | [Configuration](config.md) | [Go API](api.md) | [References](reference.md)

[reviewdog-actionlint]: https://github.com/reviewdog/action-actionlint
[reviewdog]: https://github.com/reviewdog/reviewdog
[cmd-manual]: https://rhysd.github.io/actionlint/usage.html
[re2]: https://golang.org/s/re2syntax
[go-template]: https://pkg.go.dev/text/template
[jsonl]: https://jsonlines.org/
[ga-annotate-error]: https://docs.github.com/en/actions/learn-github-actions/workflow-commands-for-github-actions#setting-an-error-message
[sarif]: https://docs.oasis-open.org/sarif/sarif/v2.1.0/sarif-v2.1.0.html
[problem-matchers]: https://github.com/actions/toolkit/blob/master/docs/problem-matchers.md
[super-linter]: https://github.com/github/super-linter
[super-linter-env-var]: https://github.com/super-linter/super-linter#environment-variables
[yactionlint-matcher]: https://raw.githubusercontent.com/mkusaka/yactionlint/main/.github/yactionlint-matcher.json
[preinstall-ubuntu]: https://github.com/actions/runner-images/blob/main/images/ubuntu/Ubuntu2404-Readme.md
[pre-commit]: https://pre-commit.com
[go-install]: https://go.dev/doc/install
[docker]: https://www.docker.com/
[docker-image]: https://hub.docker.com/r/rhysd/actionlint
[vsc-extension]: https://marketplace.visualstudio.com/items?itemName=arahata.linter-actionlint
[vscode]: https://code.visualstudio.com/
[emacs-melpa]: https://melpa.org/
[emacs-flymake]: https://www.gnu.org/software/emacs/manual/html_node/flymake/
[emacs-flymake-extension]: https://github.com/ROCKTAKEY/flymake-actionlint
[emacs-flycheck]: https://www.flycheck.org/
[emacs-flycheck-extension]: https://github.com/tirimia/flycheck-actionlint
[nvim-lint]: https://github.com/mfussenegger/nvim-lint
[vim-ale]: https://github.com/dense-analysis/ale
[pulsar]: https://pulsar-edit.dev/
[pulsar-linter]: https://web.pulsar-edit.dev/packages/linter-github-actions
[nova-extension]: https://extensions.panic.com/extensions/org.netwrk/org.netwrk.actionlint/
[nova]: https://nova.app
[trunk-io]: https://docs.trunk.io/docs
[trunk-docs]: https://docs.trunk.io/docs/check
[trunk-vscode]: https://marketplace.visualstudio.com/items?itemName=trunk.io
