GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

GO ?= go

PROVIDER_DIR ?= .
PROVIDER_NAME ?= orchestra
BIN_DIR ?= bin
PROVIDER_MAIN_PKG ?= .
PROVIDER_BINARY ?= $(BIN_DIR)/terraform-provider-$(PROVIDER_NAME)

# Where tfplugindocs will write the rendered markdown into this repo.
RENDERED_WEBSITE_DIR ?= docs

# Optional: path to a `terraform providers schema -json` output file.
# When set, tfplugindocs will skip building the provider and calling Terraform CLI.
PROVIDERS_SCHEMA ?=

# Where tfplugindocs writes temporary intermediate files.
TFPLUGINDOCS_TEMP_DIR ?= .tmp-tfplugindocs

# `git ls-files` lists deleted-but-tracked files and misses new ones, so walk
# the tree instead. gofmt skips directories beginning with a dot.
GOFMT_PATHS ?= .

# Only engine and agents are committed; the provider does not call admin.
SPEC_BASE ?= https://docs.getorchestra.io/api
SPEC_DIR ?= internal/apicontract

.PHONY: help docs docs-clean build compile test vet clean provider-bin format format-check openapi enums

help:
	@echo "Usage:"
	@echo "  make build         Compile Go packages"
	@echo "  make compile       Compile Go packages & tests (no test run)"
	@echo "  make test          Run unit tests"
	@echo "  make vet           Run go vet"
	@echo "  make provider-bin Build Terraform provider executable (if cmd entrypoint exists)"
	@echo "  make clean         Remove temp artifacts"
	@echo "  make openapi       Refresh the committed Engine and Agents specs, then regenerate enums"
	@echo "  make enums         Regenerate internal/enums from the Engine spec"
	@echo "  make docs          Generate Terraform provider docs into ./docs"
	@echo "  make docs-clean    Remove generated docs and temp artifacts"
	@echo ""
	@echo "Notes:"
	@echo "  The docs generator is tfplugindocs, pinned in go.mod and run with go tool."
	@echo "  Provider schema/build requirements depend on what tfplugindocs expects from $(PROVIDER_DIR)."
	@echo ""
	@echo "Optional env vars:"
	@echo "  PROVIDERS_SCHEMA=/path/to/providers-schema.json"
	@echo "    Skips compiling the provider and skips calling Terraform CLI."

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

openapi:
	@for s in engine agents; do \
		echo "Fetching $(SPEC_BASE)/$$s.yaml"; \
		curl --fail --silent --show-error --location --output "$(SPEC_DIR)/$$s.yaml" "$(SPEC_BASE)/$$s.yaml" || exit 1; \
	done
	@$(MAKE) enums

enums:
	@$(GO) generate ./internal/enums

# Needs the Terraform CLI on PATH. On macOS it also needs a short TMPDIR: the
# plugin handshake uses a unix socket, and paths over 104 characters fail with
# "bind: invalid argument".
docs:
	@rm -rf "$(RENDERED_WEBSITE_DIR)"
	@mkdir -p "$(RENDERED_WEBSITE_DIR)"
	@rm -rf "$(TFPLUGINDOCS_TEMP_DIR)"
	@if [ -n "$(PROVIDERS_SCHEMA)" ]; then \
		if [ ! -f "$(PROVIDERS_SCHEMA)" ]; then \
			echo "ERROR: PROVIDERS_SCHEMA is set but file not found: $(PROVIDERS_SCHEMA)" 1>&2; \
			exit 2; \
		fi; \
		$(GO) tool tfplugindocs generate \
			--provider-dir "$(PROVIDER_DIR)" \
			--provider-name "$(PROVIDER_NAME)" \
			--providers-schema "$(PROVIDERS_SCHEMA)" \
			--rendered-website-dir "$(RENDERED_WEBSITE_DIR)" \
			--website-temp-dir "$(TFPLUGINDOCS_TEMP_DIR)"; \
	else \
		$(GO) tool tfplugindocs generate \
			--provider-dir "$(PROVIDER_DIR)" \
			--provider-name "$(PROVIDER_NAME)" \
			--rendered-website-dir "$(RENDERED_WEBSITE_DIR)" \
			--website-temp-dir "$(TFPLUGINDOCS_TEMP_DIR)"; \
	fi

docs-clean:
	@rm -rf "$(RENDERED_WEBSITE_DIR)" "$(TFPLUGINDOCS_TEMP_DIR)"

format:
	@echo "Running gofmt..."
	@gofmt -w $(GOFMT_PATHS)

format-check:
	@echo "Checking gofmt..."
	@formatted="$$(gofmt -l $(GOFMT_PATHS))"; \
	if [ -n "$$formatted" ]; then \
		echo "The following files are not gofmt-formatted:"; \
		echo "$$formatted"; \
		exit 1; \
	fi
