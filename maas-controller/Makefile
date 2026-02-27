# MaaS Controller Makefile
# Build and deploy the MaaS control plane (MaaSModel, MaaSAuthPolicy, MaaSSubscription)

.PHONY: build run test test-integration tidy install uninstall image-build image-push docker-build generate manifests envtest help

help:
	@echo "MaaS Controller make targets:"
	@echo "  make build        - build manager binary to bin/manager"
	@echo "  make run          - build and run manager locally"
	@echo "  make test              - run unit tests"
	@echo "  make test-integration  - run integration tests (envtest + BDD)"
	@echo "  make generate     - generate deepcopy code for API types"
	@echo "  make manifests    - generate CRD manifests"
	@echo "  make install      - apply config/default (CRDs, RBAC, deployment)"
	@echo "  make uninstall    - delete config/default resources"
	@echo "  make image-build  - build container image (podman/buildah/docker); default IMAGE=quay.io/maas/maas-controller (temporary)"
	@echo "  make image-push   - build and push image; override with IMAGE=... IMAGE_TAG=..."

BINARY_NAME := manager
BUILD_DIR := bin
LOCALBIN ?= $(shell pwd)/bin
ENVTEST_K8S_VERSION ?= 1.31.0

# Container build: use podman, buildah, or docker (default: podman if available, else buildah, else docker)
CONTAINER_RUNTIME := $(shell command -v podman 1>/dev/null 2>&1 && echo podman || (command -v buildah 1>/dev/null 2>&1 && echo buildah || echo docker))
# Default image; for now you can use quay.io/maas/maas-controller (temporary – replace with your own registry when ready)
IMAGE ?= quay.io/maas/maas-controller
IMAGE_TAG ?= latest
FULL_IMAGE := $(IMAGE):$(IMAGE_TAG)

build: tidy $(BUILD_DIR)
	go build -o $(BUILD_DIR)/$(BINARY_NAME) ./cmd/manager

$(BUILD_DIR):
	mkdir -p $(BUILD_DIR)

run: build
	./$(BUILD_DIR)/$(BINARY_NAME)

test: tidy
	go test ./...

tidy:
	go mod tidy

# Install setup-envtest tool
envtest:
	GOBIN=$(LOCALBIN) go install sigs.k8s.io/controller-runtime/tools/setup-envtest@latest

# Run integration tests (envtest + BDD)
test-integration: tidy envtest
	KUBEBUILDER_ASSETS="$$($(LOCALBIN)/setup-envtest use $(ENVTEST_K8S_VERSION) --bin-dir $(LOCALBIN) -p path)" \
	go test -tags=integration -count=1 -v ./pkg/controller/maas/

# Generate deepcopy code for API types
generate:
	controller-gen object paths="./api/..."

# Generate CRD manifests
manifests:
	controller-gen crd paths="./api/..." output:crd:dir=./config/crd/bases

# Install CRDs, RBAC, and manager deployment (default: opendatahub namespace)
install:
	kubectl apply -k config/default

uninstall:
	kubectl delete -k config/default --ignore-not-found

# Build container image (podman, buildah, or docker)
# buildah uses "bud" (build-using-dockerfile) instead of "build"
BUILD_CMD := $(if $(filter buildah,$(CONTAINER_RUNTIME)),bud,build)
image-build:
	$(CONTAINER_RUNTIME) $(BUILD_CMD) -t $(FULL_IMAGE) -f Containerfile .
	@echo "Built $(FULL_IMAGE) with $(CONTAINER_RUNTIME)"

# Alias for image-build (backward compatibility)
docker-build: image-build

# Push image to registry (must be logged in, e.g. podman login quay.io)
image-push: image-build
	$(CONTAINER_RUNTIME) push $(FULL_IMAGE)
	@echo "Pushed $(FULL_IMAGE)"
