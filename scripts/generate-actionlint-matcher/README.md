generate-yactionlint-matcher
============================

This script generates [`yactionlint-matcher.json`](../../.github/yactionlint-matcher.json).

## Usage

```sh
make .github/yactionlint-matcher.json
```

or directly run the script

```sh
node ./scripts/generate-actionlint-matcher/main.mjs .github/yactionlint-matcher.json
```

## Test

```sh
node ./scripts/generate-actionlint-matcher/test.mjs
```

The test uses test data at `./scripts/generate-actionlint-matcher/test/*.txt`. Update them when yactionlint changes
the default error message format. To update them:

```sh
make ./scripts/generate-actionlint-matcher/test/escape.txt
make ./scripts/generate-actionlint-matcher/test/no_escape.txt
make ./scripts/generate-actionlint-matcher/test/want.json
```

or expand glob by your shell:

```sh
make ./scripts/generate-actionlint-matcher/test/*
```
