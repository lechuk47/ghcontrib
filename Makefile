DOCKER = $(shell which docker)
IMAGE = ghcontrib
VERSION = 0.0.1

docker-build:
	$(DOCKER) build -t $(IMAGE):$(VERSION) -f docker/Dockerfile .