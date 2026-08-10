VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || printf 'dev')
LDFLAGS = -s -w -X pocikode/bookshelf/internal/version.Version=$(VERSION)

.PHONY: assets check dev test build clean

assets:
	bun install --frozen-lockfile
	bun run build

check:
	test -s internal/web/dist/app.css
	test $$(wc -c < internal/web/dist/app.css) -ge 3000
	test -s internal/web/dist/reader.js
	test -s internal/web/dist/upload.js
	test -s internal/web/dist/pdf.worker.js
	test -s internal/web/dist/favicon.svg

test: assets check
	go test -race -cover ./...

build: assets check
	CGO_ENABLED=0 go build -trimpath -ldflags="$(LDFLAGS)" -o bookshelf ./cmd/bookshelf

dev: assets check
	go build -ldflags="$(LDFLAGS)" ./cmd/bookshelf

clean:
	find internal/web/dist -type f ! -name .gitkeep -delete
	rm -f bookshelf
