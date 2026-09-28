Installation
============

This document describes how to install [yactionlint](../README.md).

> [!NOTE]
> The package-manager distributions below install upstream `actionlint`, not yactionlint. Use this repository's releases, its download script, or Go install for yactionlint.


## Upstream actionlint packages (external)

The following package-manager instructions are for upstream `actionlint`; they do not install yactionlint.

### Windows

- [Chocolatey](https://chocolatey.org/): `choco install actionlint`
- [Scoop](https://scoop.sh/): `scoop install actionlint`
- [Winget](https://learn.microsoft.com/en-us/windows/package-manager/): `winget install actionlint`

### Linux

- [Arch Linux](https://archlinux.org/): `pacman -S actionlint`
- [AUR][aur]: `actionlint-bin` and `actionlint-git` packages are available.
- [Nix](https://nixos.wiki/): `nix-env -iA nixos.actionlint` on NixOS, or
  `nix-env -iA nixpkgs.actionlint` elsewhere.

### macOS

[Homebrew][homebrew] provides the upstream [`actionlint`][formula] formula:

```sh
brew install actionlint
```

## Prebuilt binaries

Download an archive from [yactionlint's releases page][releases] for your platform, unarchive it,
and put the `yactionlint` executable file in a directory in `$PATH`.

Prebuilt binaries are built at each release by CI for the following OS and arch:

- macOS (x86_64, arm64)
- Linux (i386, x86_64, arm32, arm64)
- Windows (i386, x86_64, arm64)
- FreeBSD (i386, x86_64)

Note that the following targets are not tested since GitHub Actions doesn't support them:

- Linux i386, arm32
- Windows i386
- FreeBSD i386, x86_64

The [`gh`][gh] command can download these binaries. For x86_64 Linux:

```sh
gh release download --repo mkusaka/yactionlint --pattern '*_linux_amd64.tar.gz' v0.0.7
tar xf yactionlint_0.0.7_linux_amd64.tar.gz
./yactionlint -version
```

Optionally, verify the [attestation][attestations] of the downloaded artifact:

```sh
gh attestation verify -R mkusaka/yactionlint yactionlint_0.0.7_linux_amd64.tar.gz
```

<a id="download-script"></a>
## Download script

To install yactionlint with one command, run [the download script](../scripts/download-yactionlint.bash). It downloads the latest
binary (`yactionlint.exe` on Windows and `yactionlint` on other OSes) to the current directory.

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/mkusaka/yactionlint/main/scripts/download-yactionlint.bash)
```

To install a specific release, pass its version as the first argument:

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/mkusaka/yactionlint/main/scripts/download-yactionlint.bash) 0.0.7
```

This script downloads `yactionlint` (or `yactionlint.exe` on Windows) binary to the current working directory. When you need to put
the downloaded binary to some other directory, please give the directory path to the 2nd command line argument. The following
example installs the latest version to `/usr/bin`.

```sh
bash <(curl -fsSL https://raw.githubusercontent.com/mkusaka/yactionlint/main/scripts/download-yactionlint.bash) latest /usr/bin
```

For the usage of yactionlint on GitHub Actions, see [the usage document](usage.md#on-github-actions).

## Docker image

The [upstream actionlint Docker image](usage.md#docker) is external and does not contain yactionlint. For yactionlint, use the
release binary above or build a container from this repository.

## External version managers for upstream actionlint

The [asdf][asdf] and [mise][mise] integrations below install upstream `actionlint`, not yactionlint.

### asdf

The [asdf-actionlint][asdf-plugin] plugin manages upstream `actionlint` release binaries:

```bash
asdf plugin add actionlint
asdf install actionlint latest
asdf global actionlint latest
```

### mise

```bash
mise install actionlint@latest
mise use -g actionlint@latest
```

## Build from source

Go 1.26 or 1.27 is required to build yactionlint from source. The last two major versions of Go are supported.

```sh
# Install yactionlint v0.0.7
go install github.com/mkusaka/yactionlint/cmd/yactionlint@v0.0.7

# Install the head of yactionlint's main branch
go install github.com/mkusaka/yactionlint/cmd/yactionlint@main
```

---

[Checks](checks.md) | [Usage](usage.md) | [Configuration](config.md) | [Go API](api.md) | [References](reference.md)

[formula]: https://formulae.brew.sh/formula/actionlint
[homebrew]: https://brew.sh/
[releases]: https://github.com/mkusaka/yactionlint/releases
[gh]: https://docs.github.com/en/github-cli/github-cli/about-github-cli
[attestations]: https://docs.github.com/en/actions/concepts/security/artifact-attestations
[Go]: https://golang.org/
[asdf]: https://asdf-vm.com/
[asdf-plugin]: https://github.com/crazy-matt/asdf-actionlint
[aur]: https://aur.archlinux.org/
[mise]: https://github.com/jdx/mise
