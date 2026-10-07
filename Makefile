GIT_SHA := $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")

GO ?= go
GOPATH ?= $(shell $(GO) env GOPATH)

TFPLUGINDOCS_PKG := github.com/hashicorp/terraform-plugin-docs/cmd/tfplugindocs
TFPLUGINDOCS_VERSION ?= v0.25.0
TFPLUGINDOCS_BIN ?= $(GOPATH)/bin/tfplugindocs

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

# The docs site split its one openapi.yaml into engine.yaml, agents.yaml and admin.yaml.
# The provider talks to the Engine API, committed as openapi.yaml; the other two
# are fetched only to check for new endpoints.
SPEC_BASE ?= https://docs.getorchestra.io/api
OPENAPI_URL ?= $(SPEC_BASE)/engine.yaml
SPEC_TMP ?= .tmp-spec
OPENAPI_PATH ?= openapi.yaml

.PHONY: help docs docs-clean tfplugindocs build compile test vet clean provider-bin format format-check openapi enums spec-drift

help:
	@echo "Usage:"
	@echo "  make build         Compile Go packages"
	@echo "  make compile       Compile Go packages & tests (no test run)"
	@echo "  make test          Run unit tests"
	@echo "  make spec-drift    Compare openapi.yaml with the live API specs (read-only)"
	@echo "  make vet           Run go vet"
	@echo "  make provider-bin Build Terraform provider executable (if cmd entrypoint exists)"
	@echo "  make clean         Remove temp artifacts"
	@echo "  make openapi       Refresh openapi.yaml from the docs site, then regenerate enums"
	@echo "  make enums         Regenerate internal/enums from openapi.yaml"
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

openapi:
	@echo "Fetching $(OPENAPI_URL)"
	@curl --fail --silent --show-error --location --output "$(OPENAPI_PATH)" "$(OPENAPI_URL)"
	@$(MAKE) enums

# Read-only: fetches the live specs and reports what changed since openapi.yaml,
# without touching anything. A non-zero exit means something needs attention.
spec-drift:
	@mkdir -p "$(SPEC_TMP)"
	@for s in engine agents admin; do \
		curl --fail --silent --show-error --location --output "$(SPEC_TMP)/$$s.yaml" "$(SPEC_BASE)/$$s.yaml" || exit 1; \
	done
	@$(GO) run ./tools/specdrift -old "$(OPENAPI_PATH)" -new "$(SPEC_TMP)/engine.yaml" \
		-also agents="$(SPEC_TMP)/agents.yaml" -also admin="$(SPEC_TMP)/admin.yaml"

enums:
	@python3 scripts/generate_enums.py --openapi "$(OPENAPI_PATH)" --out internal/enums/enums_gen.go
	@gofmt -w internal/enums

# Needs the Terraform CLI on PATH. On macOS it also needs a short TMPDIR: the
# plugin handshake uses a unix socket, and paths over 104 characters fail with
# "bind: invalid argument".
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
