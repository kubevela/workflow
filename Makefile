include makefiles/const.mk
include makefiles/dependency.mk
include makefiles/e2e.mk

.PHONY: all
all: build

##@ General

# The help target prints out all targets with their descriptions organized
# beneath their categories. The categories are represented by '##@' and the
# target descriptions by '##'. The awk commands is responsible for reading the
# entire set of makefiles included in this invocation, looking for lines of the
# file as xyz: ## something, and then pretty-format the target and help. Then,
# if there's a line with ##@ something, that gets pretty-printed as a category.
# More info on the usage of ANSI control characters for terminal formatting:
# https://en.wikipedia.org/wiki/ANSI_escape_code#SGR_parameters
# More info on the awk command:
# http://linuxcommand.org/lc3_adv_awk.php

.PHONY: help
help: ## Display this help.
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-15s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development

.PHONY: reviewable
reviewable: manifests fmt vet lint staticcheck helm-doc-gen
	go mod tidy

.PHONY: check-diff
check-diff: reviewable ## Execute auto-gen code commands and ensure branch is clean.
	git --no-pager diff
	git diff --quiet || ($(ERR) please run 'make reviewable' to include all changes && false)
	@$(OK) branch is clean

.PHONY: manifests
manifests: controller-gen ## Generate CustomResourceDefinition objects.
	$(CONTROLLER_GEN) crd paths="./..." output:crd:artifacts:config=config/crd/bases
	mv config/crd/bases/* charts/vela-workflow/crds/

.PHONY: generate
generate: controller-gen ## Generate code containing DeepCopy, DeepCopyInto, and DeepCopyObject method implementations.
	$(CONTROLLER_GEN) object:headerFile="hack/boilerplate.go.txt" paths="./..."

.PHONY: lint
lint: golangci
	$(GOLANGCILINT) run ./...

.PHONY: staticcheck
staticcheck: staticchecktool
	$(STATICCHECK) ./...

.PHONY: fmt
fmt: goimports ## Run go fmt against code.
	go fmt ./...
	$(GOIMPORTS) -local github.com/oam-dev/kubevela -w $$(go list -f {{.Dir}} ./...)

.PHONY: vet
vet: ## Run go vet against code.
	go vet ./...

.PHONY: test
test: manifests generate fmt vet envtest ## Run tests.
	KUBEBUILDER_ASSETS="$(shell $(ENVTEST) use $(ENVTEST_K8S_VERSION) -p path)" GODEBUG=x509sha1=1 go test $(shell go list ./... | grep -v e2e) -coverprofile coverage.txt

.PHONY: helm-doc-gen
helm-doc-gen: helmdoc
	readme-generator -v charts/vela-workflow/values.yaml -r charts/vela-workflow/README.md

##@ Build

.PHONY: build
build: generate fmt vet ## Build manager binary.
	go build -o bin/manager ./cmd/main.go

.PHONY: run
run: manifests generate fmt vet ## Run a controller from your host.
	go run ./cmd/main.go

.PHONY: docker-build
docker-build: ## Build docker image with the manager.
	docker build --build-arg=VERSION=$(VELA_VERSION) --build-arg=GITVERSION=$(GIT_COMMIT) --build-arg=OS=$(OS) --build-arg=ARCH=${ARCH} -t ${IMG}:${IMG_TAG} .

# load docker image to the k3d cluster
image-load: 
	docker build -t ${IMG}:${IMG_TAG} -f Dockerfile.e2e .
	k3d image import ${IMG}:${IMG_TAG} || { echo >&2 "kind not installed or error loading image: ${IMG}:${IMG_TAG}"; exit 1; }

.PHONY: docker-push
docker-push: ## Push docker image with the manager.
	docker push ${IMG}:${IMG_TAG}

##@ Deployment

ifndef ignore-not-found
  ignore-not-found = false
endif

.PHONY: install
install: manifests kustomize ## Install CRDs into the K8s cluster specified in ~/.kube/config.
	$(KUSTOMIZE) build config/crd | kubectl apply -f -

.PHONY: uninstall
uninstall: manifests kustomize ## Uninstall CRDs from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/crd | kubectl delete --ignore-not-found=$(ignore-not-found) -f -

.PHONY: deploy
deploy: manifests kustomize ## Deploy controller to the K8s cluster specified in ~/.kube/config.
	cd config/manager && $(KUSTOMIZE) edit set image controller=${IMG}:${IMG_TAG}
	$(KUSTOMIZE) build config/default | kubectl apply -f -

.PHONY: undeploy
undeploy: ## Undeploy controller from the K8s cluster specified in ~/.kube/config. Call with ignore-not-found=true to ignore resource not found errors during deletion.
	$(KUSTOMIZE) build config/default | kubectl delete --ignore-not-found=$(ignore-not-found) -f -
