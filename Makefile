NAME=vpoker
TAG=latest
BASE_BUILDER_IMAGE_NAME=base-builder:latest
IMAGE_NAME=$(NAME):$(TAG)
OUT=$(NAME)

.PHONY: install-deps
install-deps:
	@go mod download

# .PHONY: tools
# tools:
# 	@cd tools && go install ./...

.PHONY: lint
lint:
	@golint ./...

# collect all linters and checks eventually
.PHONY: check
check: lint
	@staticcheck ./...

.PHONY: vet
vet:
	@go vet ./...

.PHONY: generate
generate:
	echo generating
# 	@./tools/gen-assertions.sh
#	@./tools/gen-version.sh

.PHONY: build
build: generate vet
	@go build -o bin/$(OUT) .

.PHONY: install
install: build
	@go install ./...

.PHONY: test
test: vet
	# -race causes panics in BoltDB code
	go test -timeout=10s ./...

.PHONY: base-builder-image
base-builder-image:
	@docker build -t $(BASE_BUILDER_IMAGE_NAME) -f Dockerfile.base-builder .

.PHONY: container-image
container-image:
	@docker build -t $(IMAGE_NAME) .

# .PHONY: coverage
# coverage: vet
# 	@./tools/coverage.sh

# .PHONY: coverage-html
# coverage-html: vet
# 	@./tools/coverage.sh html
