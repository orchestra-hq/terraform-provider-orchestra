GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

GO ?= go
GOPATH ?= $(shell $(GO) env GOPATH)

TFPLUGINDOCS_PKG := github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs
TFPLUGINDOCS_VERSION ?= v0.25.0
TFPLUGINDOCS_BIN ?= $(GOPATH)/bin/tfplugindocs

PROVIDER_DIR ?= .
PROVIDER_NAME ?= orchestra
BIN_DIR ?= bin
PROVIDER_MAIN_PKG ?= ./cmd/terraform-provider-$(PROVIDER_NAME)
PROVIDER_BINARY ?= $(BIN_DIR)/terraform-provider-$(PROVIDER_NAME)

# Where tfplugindocs will write the rendered markdown into this repo.
RENDERED_WEBSITE_DIR ?= docs

# Optional: path to a `terraform providers schema -json` output file.
# When set, tfplugindocs will skip building the provider and calling Terraform CLI.
PROVIDERS_SCHEMA ?=

# Where tfplugindocs writes temporary intermediate files.
TFPLUGINDOCS_TEMP_DIR ?= .tmp-tfplugindocs

GO_FILES := $(shell git ls-files '*.go')

.PHONY: help docs docs-clean tfplugindocs build compile test vet clean provider-bin format format-check

help:
	@echo "Usage:"
	@echo "  make build         Compile Go packages"
	@echo "  make compile       Compile Go packages & tests (no test run)"
	@echo "  make test          Run unit tests"
	@echo "  make vet           Run go vet"
	@echo "  make provider-bin Build Terraform provider executable (if cmd entrypoint exists)"
	@echo "  make clean         Remove temp artifacts"
	@echo "  make docs          Generate Terraform provider docs into ./docs"
	@echo "  make docs-clean    Remove generated docs and temp artifacts"
	@echo ""
	@echo "Notes:"
	@echo "  The docs generator uses tfplugindocs; the target will install it if missing."
	@echo "  Provider schema/build requirements depend on what tfplugindocs expects from $(PROVIDER_DIR)."
	@echo ""
	@echo "Optional env vars:"
	@echo "  PROVIDERS_SCHEMA=/path/to/providers-schema.json"
	@echo "    Skips compiling the provider and skips calling Terraform CLI."

tfplugindocs:
	@mkdir -p "$(dir $(TFPLUGINDOCS_BIN))"
	@if [ -x "$(TFPLUGINDOCS_BIN)" ]; then \
		echo "tfplugindocs already present at $(TFPLUGINDOCS_BIN)"; \
	else \
		echo "Installing tfplugindocs $(TFPLUGINDOCS_VERSION)"; \
		GOSUMDB=off $(GO) install $(TFPLUGINDOCS_PKG)@$(TFPLUGINDOCS_VERSION); \
	fi

build:
	@$(GO) build ./...

# Compile the test binaries but do not run any tests.
compile:
	@$(GO) test -run '^$$' ./...

test:
	@$(GO) test ./...

vet:
	@$(GO) vet ./...

clean:
	@rm -rf "$(RENDERED_WEBSITE_DIR)" "$(TFPLUGINDOCS_TEMP_DIR)"

provider-bin:
	@mkdir -p "$(BIN_DIR)"
	@if $(GO) list "$(PROVIDER_MAIN_PKG)" >/dev/null 2>&1; then \
		echo "Building $(PROVIDER_BINARY)"; \
		$(GO) build -o "$(PROVIDER_BINARY)" "$(PROVIDER_MAIN_PKG)"; \
	else \
		echo "WARNING: Provider entrypoint not found at $(PROVIDER_MAIN_PKG)"; \
		echo "Add a cmd/*/main.go (or update PROVIDER_MAIN_PKG) to enable provider-bin builds."; \
	fi

docs: tfplugindocs
	@rm -rf "$(RENDERED_WEBSITE_DIR)"
	@mkdir -p "$(RENDERED_WEBSITE_DIR)"
	@rm -rf "$(TFPLUGINDOCS_TEMP_DIR)"
	@if [ -n "$(PROVIDERS_SCHEMA)" ]; then \
		if [ ! -f "$(PROVIDERS_SCHEMA)" ]; then \
			echo "ERROR: PROVIDERS_SCHEMA is set but file not found: $(PROVIDERS_SCHEMA)" 1>&2; \
			exit 2; \
		fi; \
		$(TFPLUGINDOCS_BIN) generate \
			--provider-dir "$(PROVIDER_DIR)" \
			--provider-name "$(PROVIDER_NAME)" \
			--providers-schema "$(PROVIDERS_SCHEMA)" \
			--rendered-website-dir "$(RENDERED_WEBSITE_DIR)" \
			--website-temp-dir "$(TFPLUGINDOCS_TEMP_DIR)"; \
	else \
		$(TFPLUGINDOCS_BIN) generate \
			--provider-dir "$(PROVIDER_DIR)" \
			--provider-name "$(PROVIDER_NAME)" \
			--rendered-website-dir "$(RENDERED_WEBSITE_DIR)" \
			--website-temp-dir "$(TFPLUGINDOCS_TEMP_DIR)"; \
	fi

docs-clean:
	@rm -rf "$(RENDERED_WEBSITE_DIR)" "$(TFPLUGINDOCS_TEMP_DIR)"

format:
	@if [ -z "$(GO_FILES)" ]; then echo "No Go files found."; exit 0; fi
	@echo "Running gofmt on Go files..."
	@gofmt -w $(GO_FILES)

format-check:
	@if [ -z "$(GO_FILES)" ]; then echo "No Go files found."; exit 0; fi
	@echo "Checking gofmt..."
	@formatted="$$(gofmt -l $(GO_FILES))"; \
	if [ -n "$$formatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$formatted"; \
		exit 1; \
	fi
