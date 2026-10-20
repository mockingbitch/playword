# Local settings and secrets (Google client, admin token…) live in .env,
# which is git-ignored; .env.example lists them. Values already in the
# environment win.
ifneq (,$(wildcard .env))
ENV_VARS := $(shell sed -n 's/^\([A-Z_][A-Z0-9_]*\)=.*/\1/p' .env)
$(foreach v,$(ENV_VARS),$(if $(filter environment command,$(firstword $(origin $(v)))),,\
  $(eval $(v) := $(shell sed -n 's/^$(v)=//p' .env))))
export $(ENV_VARS)
endif

DATABASE_URL ?= postgres://playword:playword@127.0.0.1:55432/playword?sslmode=disable
export DATABASE_URL

.PHONY: check uicheck run generate loadtest ingest-en docker backup build test test-integration cover fuzz bench lint sqlc db-up migrate ingest-hnd rebuild-keys freq-viwiki

# Local development: plain HTTP (cookies without Secure), a fixed dev salt.
run: generate
	PLAYWORD_ADDR=$${PLAYWORD_ADDR:-:8020} PLAYWORD_INSECURE_COOKIES=true PLAYWORD_IP_SALT=$${PLAYWORD_IP_SALT:-dev-salt} PLAYWORD_PUBLIC_URL=$${PLAYWORD_PUBLIC_URL:-http://localhost:8020} go run ./cmd/playword

generate:
	go tool templ generate

build: generate
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/playword ./cmd/playword
	CGO_ENABLED=0 go build -trimpath -ldflags="-s -w" -o bin/playword-cli ./cmd/playword-cli

test:
	go test -race ./...

# Needs PostgreSQL (make db-up); each test creates and drops its own database.
test-integration:
	PLAYWORD_TEST_DATABASE_URL=$(DATABASE_URL) go test -race -count=1 ./internal/ingest/... ./internal/adapters/postgres/...

cover:
	go test -race -cover ./pkg/... ./internal/domain/...

fuzz:
	go test ./pkg/vnphon -run x -fuzz FuzzParseSyllable -fuzztime 60s

bench:
	go test ./internal/adapters/index/lookup -run x -bench . -benchmem

lint:
	golangci-lint run ./...

# check is the definition of done for a change (CLAUDE.md): generated code
# fresh, formatted, vetted, linted, tested with the race detector.
STATICCHECK = honnef.co/go/tools/cmd/staticcheck@v0.8.1
check: generate
	@test -z "$$(gofmt -l ./cmd ./internal ./pkg ./.scripts)" || { gofmt -l ./cmd ./internal ./pkg ./.scripts; echo "gofmt: files above need formatting"; exit 1; }
	go vet ./...
	go -C .scripts/benchgate vet ./...  # the tools are their own modules: ./... skips dot directories
	go -C .scripts/uicheck vet ./...
	@if command -v golangci-lint >/dev/null; then golangci-lint run ./...; else go run $(STATICCHECK) ./...; fi
	go test -race -count=1 ./...

# uicheck screenshots pages of a running server at several widths and fails
# on horizontal overflow or console errors: make uicheck BASE=http://localhost:8020
BASE ?= http://localhost:8020
uicheck:
	go -C .scripts/uicheck run . -base $(BASE) -out $(CURDIR)/.uicheck-out $(UICHECK_FLAGS)

sqlc:
	docker run --rm -u $$(id -u):$$(id -g) -v "$$PWD":/src -w /src sqlc/sqlc:1.30.0 generate

db-up:
	docker compose up -d --wait

migrate:
	go run ./cmd/playword-cli migrate up

ingest-hnd:
	go run ./cmd/playword-cli ingest hnd

rebuild-keys:
	go run ./cmd/playword-cli rebuild-keys

VIWIKI_DIR ?= /tmp/viwiki
freq-viwiki:
	.scripts/viwiki-text.sh $(VIWIKI_DIR)
	go run ./cmd/playword-cli freq -corpus $(VIWIKI_DIR)/viwiki.txt

# M3 DoD: p99 < 50ms at 500 RPS. Start the server with PLAYWORD_RATE_LIMIT_RPS=0 first.
loadtest:
	docker run --rm --network host -v $$PWD/.scripts/k6:/k6 grafana/k6:latest run /k6/search.js

EN_DIR ?= /tmp/playword-en
ingest-en:
	mkdir -p $(EN_DIR)
	curl -sfL -o $(EN_DIR)/en_50k.txt https://raw.githubusercontent.com/hermitdave/FrequencyWords/master/content/2018/en/en_50k.txt
	go run ./cmd/playword-cli ingest cmudict
	go run ./cmd/playword-cli ingest oewn
	go run ./cmd/playword-cli freq -lang en -list $(EN_DIR)/en_50k.txt -name en-subtitles-freq \
		-license "CC BY-SA 4.0 — FrequencyWords (Hermit Dave), OpenSubtitles 2018" -url https://github.com/hermitdave/FrequencyWords

docker:
	docker build -t playword:dev --build-arg VERSION=$$(git rev-parse --short HEAD 2>/dev/null || echo dev) .

backup:
	.scripts/backup.sh
