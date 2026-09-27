Playground for yactionlint
==========================

The [published yactionlint playground](https://mkusaka.github.io/yactionlint/) is built from this repository.
It is derived from the [upstream actionlint playground](https://rhysd.github.io/actionlint/), which runs upstream code.

The playground is built with HTML/CSS/TypeScript/Wasm. All dependencies are defined in `package.json` and managed by `npm`.
Tasks for development are defined in [`Makefile`](./Makefile).

## Tasks

```sh
# Install dependencies, build main.wasm, start serving the app at localhost:1234
make

# Install dependencies, build main.wasm
make build

# Install dependencies
make dep

# Run tests
make test

# Clean all built files and dependencies
make clean
```

## Lint

Sources are linted with [eslint](https://eslint.org/) with [typescript-eslint](https://github.com/typescript-eslint/typescript-eslint),
[prettier](https://prettier.io/) and [stylelint](https://stylelint.io/).

`lint` npm script applies all the liters:

```sh
npm run lint
```

## Deployment

The [Pages workflow](../.github/workflows/playground.yaml) builds and deploys the site from `main` when playground
or Go sources change. It stages only the compiled frontend, WebAssembly binary, and browser dependencies as a
GitHub Pages artifact; no generated binaries are committed to the repository. It can also be dispatched manually.
