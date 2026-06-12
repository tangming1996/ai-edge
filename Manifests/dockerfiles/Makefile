.PHONY: help build push clean

REGISTRY ?= tming3379
IMAGE_PREFIX ?= edgeai-
IMAGES := apiserver controller gateway-runtime

help:
	@echo "Image build/push targets:"
	@echo "  make build          Build all images"
	@echo "  make build-<name>  Build single image (apiserver, controller, gateway-runtime)"
	@echo "  make push           Push all images to registry"
	@echo "  make push-<name>   Push single image"
	@echo ""
	@echo "Variables:"
	@echo "  REGISTRY=$(REGISTRY)"

build: $(IMAGES)

$(IMAGES):
	cd ../.. && docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$@:latest \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$@:$$(git rev-parse --short HEAD 2>/dev/null || echo "dev") \
		-f Manifests/dockerfiles/Dockerfile.$@ \
		--push \
		.

build-%:
	cd ../.. && docker buildx build \
		--platform linux/amd64,linux/arm64 \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$*:latest \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$*:$$(git rev-parse --short HEAD 2>/dev/null || echo "dev") \
		-f Manifests/dockerfiles/Dockerfile.$* \
		--push \
		.

push: $(IMAGES)

push-%: build-%
