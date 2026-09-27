SRCS := $(filter-out %_test.go, $(wildcard *.go cmd/yactionlint/*.go)) go.mod go.sum .git-hooks/.timestamp
TESTS := $(filter %_test.go, $(wildcard *.go))
TOOL := $(filter %_test.go, $(wildcard scripts/*/*.go))
TESTDATA := $(wildcard \
		testdata/examples/* \
		testdata/err/* \
		testdata/ok/* \
		testdata/config/* \
		testdata/format/* \
		testdata/projects/* \
		testdata/reusable_workflow_metadata/* \
	)
GO_GEN_SRCS := scripts/generate-popular-actions/main.go \
				scripts/generate-popular-actions/popular_actions.json \
				scripts/generate-webhook-events/main.go \
				scripts/generate-availability/main.go

ifeq ($(OS),Windows_NT)
	SHELL := powershell.exe
	.SHELLFLAGS := -NoProfile -ExecutionPolicy Bypass -Command
	TARGET = yactionlint.exe
	TOUCH = powershell -NoProfile -ExecutionPolicy Bypass scripts/touch.ps1
	# It's hard to prepare C toolchain for CGO on Windows
	RACE =
else
	TARGET = yactionlint
	TOUCH = touch
	RACE = -race
endif


all: build test lint

.testtimestamp: $(TESTS) $(SRCS) $(TESTDATA) $(TOOL)
	go test $(RACE) ./...
	$(TOUCH) .testtimestamp

t test: .testtimestamp

coverage.out: $(TESTS) $(SRCS) $(TESTDATA) $(TOOL)
	go test $(RACE) -coverprofile coverage.out -covermode=atomic ./...
	$(TOUCH) .testtimestamp

coverage.html: coverage.out
	go tool cover -html=coverage.out -o coverage.html

cov: coverage.out coverage.html
	go tool cover -func=coverage.out

.linttimestamp: $(TESTS) $(SRCS) $(TOOL) docs/checks.md
	go vet ./...
	# ./... is not available because ./playground/node_dodules/ contains some Go packages
	staticcheck ./ ./scripts/... ./cmd/...
	govulncheck ./...
ifneq ($(OS),Windows_NT)
	GOOS=js GOARCH=wasm staticcheck ./playground
	go run ./scripts/check-checks -quiet ./docs/checks.md
endif
	$(TOUCH) .linttimestamp

l lint: .linttimestamp

popular_actions.go all_webhooks.go availability.go: $(GO_GEN_SRCS)
ifdef SKIP_GO_GENERATE
	$(TOUCH) popular_actions.go all_webhooks.go availability.go
else
	go generate
endif

$(TARGET): $(SRCS)
ifeq ($(OS),Windows_NT)
	go build ./cmd/yactionlint
else
	CGO_ENABLED=0 go build ./cmd/yactionlint
endif

b build: $(TARGET)

yactionlint_fuzz-fuzz.zip:
	go-fuzz-build ./fuzz

fuzz: yactionlint_fuzz-fuzz.zip
	go-fuzz -bin ./yactionlint_fuzz-fuzz.zip -func $(FUZZ_FUNC)

man/yactionlint.1: man/yactionlint.1.ronn
	ronn --roff man/yactionlint.1.ronn
man/yactionlint.1.html: man/yactionlint.1.ronn
	ronn --html man/yactionlint.1.ronn

man: man/yactionlint.1

bench:
	go test -bench Lint -benchmem

.github/yactionlint-matcher.json: scripts/generate-actionlint-matcher/object.mjs
	node ./scripts/generate-actionlint-matcher/main.mjs .github/yactionlint-matcher.json

scripts/generate-actionlint-matcher/test/escape.txt: $(TARGET)
	./yactionlint -color ./testdata/err/one_error.yaml > ./scripts/generate-actionlint-matcher/test/escape.txt || true
scripts/generate-actionlint-matcher/test/no_escape.txt: $(TARGET)
	./yactionlint -no-color ./testdata/err/one_error.yaml > ./scripts/generate-actionlint-matcher/test/no_escape.txt || true
scripts/generate-actionlint-matcher/test/want.json: $(TARGET)
	./yactionlint -format '{{json .}}' ./testdata/err/one_error.yaml > scripts/generate-actionlint-matcher/test/want.json || true

CHANGELOG.md: .bumptimestamp
	changelog-from-release > CHANGELOG.md

c clean:
	rm -f ./$(TARGET) ./.testtimestamp ./.linttimestamp ./yactionlint_fuzz-fuzz.zip ./man/yactionlint.1
	rm -rf ./corpus ./crashers

.git-hooks/.timestamp: .git-hooks/pre-push
ifneq ($(OS),Windows_NT)
	[ -z "${CI}" ] && git config core.hooksPath .git-hooks || true
endif
	$(TOUCH) .git-hooks/.timestamp

.PHONY: all test clean build lint fuzz man bench cov b t c l
