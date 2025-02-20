NAME=vpoker
SVC_NAME=vpoker
TAG=latest
BASE_BUILDER_IMAGE_NAME=base-builder:latest
IMAGE_NAME=$(NAME):$(TAG)
OUT=$(NAME)

.PHONY: install-deps
install-deps:
	@go mod download

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
	@./tools/gen-version.sh pkg/version/version_generated.go

.PHONY: build
build: vet
	@go build -o bin/$(OUT) .

.PHONY: install
install: generate build
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

.PHONY: coverage
coverage: vet
	@./tools/coverage.sh

.PHONY: coverage-html
coverage-html: vet
	@./tools/coverage.sh html

.PHONY: nginx-config
nginx-config:
	./deploy/nginx-config vpoker

.PHONY: service
service: container-image
	install -m 0644 ./deploy/$(SVC_NAME).service /etc/systemd/system/
	systemctl daemon-reload
	systemctl stop $(SVC_NAME) || true
	systemctl --now enable $(SVC_NAME)
	sleep 2	# give time to capture any immediate failures
	systemctl status -l --no-page $(SVC_NAME)

.PHONY: deploy
deploy:
	git push $(MAKEFLAGS) origin master
	./deploy/deploy.sh

.PHONY: local-run
local-run:
	@go run main.go
