.PHONY: e2e-setup-controller-pre-hook
e2e-setup-controller-pre-hook:
	sh ./hack/e2e/modify_charts.sh


.PHONY: e2e-setup-controller
e2e-setup-controller:
	helm upgrade --install            						\
		--create-namespace         									\
		--namespace vela-system     								\
		--set image.repository=oamdev/vela-workflow \
		--set image.tag=latest      								\
		--set image.pullPolicy=IfNotPresent         \
		--wait vela-workflow                        \
		./charts/vela-workflow											\
		--debug

.PHONY: end-e2e
end-e2e:
	sh ./hack/e2e/end_e2e.sh

.PHONY: e2e-test
e2e-test:
	# Run e2e test
	go test -v ./e2e
