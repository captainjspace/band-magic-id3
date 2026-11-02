# band-magic-id3 — build, test, container.
#   make            vet + test + build both binaries into bin/
#   make image      container for Cloud Run (linux/amd64, even on Apple silicon)
#   make push REGISTRY=us-central1-docker.pkg.dev/<project>/band-magic-id3
#   make run-api ROOT=/Volumes/…/Audio

VERSION   ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
CONTAINER ?= docker
IMAGE     ?= band-magic-id3
REGISTRY  ?=
ROOT      ?= .
PORT      ?= 8080
GOBUILD   := CGO_ENABLED=0 go build -buildvcs=false -trimpath -ldflags="-s -w"

.PHONY: all build test vet fmt image push local_deploy run-api clean

all: vet test build

build:
	$(GOBUILD) -o bin/band-magic-id3 ./cmd/band-magic-id3
	$(GOBUILD) -o bin/band-magic-id3-api ./cmd/band-magic-id3-api

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w .

image:
	$(CONTAINER) build --platform linux/amd64 -t $(IMAGE):$(VERSION) .

push: image
	@test -n "$(REGISTRY)" || { echo "set REGISTRY=<region>-docker.pkg.dev/<project>/<repo>"; exit 1; }
	$(CONTAINER) tag $(IMAGE):$(VERSION) $(REGISTRY)/$(IMAGE):$(VERSION)
	$(CONTAINER) push $(REGISTRY)/$(IMAGE):$(VERSION)

# local cluster; k8s_* come from .envrc (direnv)
local_deploy:
	ks show container | podman login -u container --password-stdin $(k8s_registry)
	podman build --platform linux/amd64 -f Dockerfile -t $(k8s_tagname) .
	podman push $(k8s_tagname)
	kubectl apply -k k8s/base
	kubectl -n $(k8s_namespace) set image deployment/$(k8s_name) $(k8s_name)=$(k8s_tagname)

run-api: build
	PORT=$(PORT) bin/band-magic-id3-api -root "$(ROOT)"

clean:
	rm -rf bin
