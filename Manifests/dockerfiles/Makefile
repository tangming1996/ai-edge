.PHONY: help build push clean build-% push-%

REGISTRY ?= tming3379
IMAGE_PREFIX ?= edgeai-
IMAGES := apiserver controller gateway-runtime
PLATFORM ?= linux/amd64
PUSH ?= 0

help:
	@echo "Image build/push targets:"
	@echo "  make build          Build all images"
	@echo "  make build-<name>  Build single image (apiserver, controller, gateway-runtime)"
	@echo "  make push           Push all images to registry"
	@echo "  make push-<name>   Push single image"
	@echo ""
	@echo "Variables:"
	@echo "  REGISTRY=$(REGISTRY)"
	@echo "  PLATFORM=$(PLATFORM)"

build: $(IMAGES)

$(IMAGES):
	cd ../.. && docker buildx build \
		--platform $(PLATFORM) \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$@:latest \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$@:$$(git rev-parse --short HEAD 2>/dev/null || echo "dev") \
		-f manifests/dockerfiles/Dockerfile.$@ \
		$(if $(filter 1,$(PUSH)),--push,--load) \
		.

build-%:
	cd ../.. && docker buildx build \
		--platform $(PLATFORM) \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$*:latest \
		-t $(REGISTRY)/$(IMAGE_PREFIX)$*:$$(git rev-parse --short HEAD 2>/dev/null || echo "dev") \
		-f manifests/dockerfiles/Dockerfile.$* \
		$(if $(filter 1,$(PUSH)),--push,--load) \
		.

push:
	$(MAKE) PUSH=1 build

push-%:
	$(MAKE) PUSH=1 build-$*

clean:
	@echo "No explicit cleanup target is required for docker buildx artifacts."
