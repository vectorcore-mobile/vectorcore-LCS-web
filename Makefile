.PHONY: build clean deps install uninstall ui dev-ui

BINARY=lcs
APP_VERSION=0.1.1d
PREFIX=/opt/vectorcore
BINDIR=$(PREFIX)/bin
ETCDIR=$(PREFIX)/etc
LOGDIR=$(PREFIX)/log
SYSTEMD=/lib/systemd/system/

build: ui deps
	mkdir -p bin
	go build -o bin/$(BINARY) ./cmd/lcs

ui: ## Build the React UI (requires Node.js / npm)
	cd web && npm install && npm run build

dev-ui: ## Start Vite dev server (proxies /api to localhost:8090)
	cd web && npm install && npm run dev

deps:
	go mod tidy

install: build
	install -d $(BINDIR)
	install -d $(ETCDIR)
	install -d $(LOGDIR)

	install -m755 bin/$(BINARY) $(BINDIR)/$(BINARY)

	if [ ! -f $(ETCDIR)/lcs.yaml ]; then \
		install -m644 config/lcs.yaml $(ETCDIR)/lcs.yaml; \
	fi

	touch $(LOGDIR)/lcs.log
	chmod 644 $(LOGDIR)/lcs.log

	install -m644 systemd/vectorcore-lcs.service $(SYSTEMD)/vectorcore-lcs.service

	systemctl daemon-reload
	systemctl enable vectorcore-lcs
	systemctl start vectorcore-lcs

clean:
	rm -rf bin/

uninstall:
	systemctl stop vectorcore-lcs || true
	systemctl disable vectorcore-lcs || true

	rm -f $(BINDIR)/$(BINARY)
	rm -f $(SYSTEMD)/vectorcore-lcs.service

	systemctl daemon-reload
