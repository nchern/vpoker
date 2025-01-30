NAME=vpoker
SVC_NAME=vpoker
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

.PHONY: config-nginx
config-nginx :
	@install -m 0644 ./deploy/nginx/$(SVC_NAME) /etc/nginx/sites-available/
	@systemctl restart nginx
	sleep 2	# give time to capture any immediate failures
	systemctl status -l --no-page nginx

.PHONY: service
service: container-image
	@install -m 0644 ./deploy/$(SVC_NAME).service /etc/systemd/system/
	systemctl daemon-reload
	systemctl stop $(SVC_NAME) || true
	systemctl --now enable $(SVC_NAME)
	sleep 2	# give time to capture any immediate failures
	systemctl status -l --no-page $(SVC_NAME)
