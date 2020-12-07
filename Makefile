CURL = $(shell which curl)
DOCKER = $(shell which docker)
DOCKER_COMPOSE = $(shell which docker-compose)
IMAGE = ghcontrib
VERSION = 0.0.1

PHONY+=docker-build
docker-build:
	$(DOCKER) build -t $(IMAGE):$(VERSION) -f docker/Dockerfile .


PHONY+=run
make up:
	$(DOCKER_COMPOSE) up --build -d

PHONY+=shutdown
down:
	$(DOCKER_COMPOSE) down

PHONY+=test
test:
	$(CURL) http://localhost:10000/top/Barcelona?items=10