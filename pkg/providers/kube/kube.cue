// kube.cue

#Apply: {
	#do:       "apply"
	#provider: "kube"

	$params: {
		// +usage=The cluster to use
		cluster: *"" | string
		// +usage=The resource to apply
		value: {...}
		// +usage=The patcher that will be applied to the resource, you can define the strategy of list merge through comments. Reference doc here: https://kubevela.io/docs/platform-engineers/traits/patch-trait#patch-in-workflow-step
		patch?: {...}
	}

	$returns?: {
		// +usage=The resource after applied will be filled in this field after the action is executed
		value?: {...}
		// +usage=The error message if the action failed
		err?: string
	}
	...
}

#Patch: {
	#do:       "patch"
	#provider: "kube"

	$params: {
		// +usage=The cluster to use
		cluster: *"" | string
		// +usage=The resource to patch, we'll first get the resource from the cluster, then apply the patcher to it
		value: {...}
		// +usage=The patcher that will be applied to the resource, you can define the strategy of list merge through comments. Reference doc here: https://kubevela.io/docs/platform-engineers/traits/patch-trait#patch-in-workflow-step
		patch: {...}
	}

	$returns?: {
		// +usage=The resource after patched will be filled in this field after the action is executed
		result?: {...}
	}
	...
}

#ApplyInParallel: {
	#do:       "apply-in-parallel"
	#provider: "kube"

	$params: {
		// +usage=The cluster to use
		cluster: *"" | string
		// +usage=The resources to apply in parallel
		value: [...{...}]
	}

	$returns?: {
		// +usage=The resource after applied will be filled in this field after the action is executed
		value?: [...{...}]
	}
	...
}

#Read: {
	#do:       "read"
	#provider: "kube"

	$params: {
		// +usage=The cluster to use
		cluster: *"" | string
		// +usage=The resource to read, this field will be filled with the resource read from the cluster after the action is executed
		value: {...}
	}

	$returns?: {
		// +usage=The read resource will be filled in this field after the action is executed
		value?: {...}
		// +usage=The error message if the action failed
		err?: string
	}
	...
}

#List: {
	#do:       "list"
	#provider: "kube"

	$params: {
		// +usage=The cluster to use
		cluster: *"" | string
		// +usage=The resource to list
		resource: {
			// +usage=The api version of the resource
			apiVersion: string
			// +usage=The kind of the resource
			kind: string
		}
		// +usage=The filter to list the resources
		filter?: {
			// +usage=The namespace to list the resources
			namespace: *"" | string
			// +usage=The label selector to filter the resources
			matchingLabels?: {...}
		}
	}

	$returns?: {
		// +usage=The listed resources will be filled in this field after the action is executed
		values?: {...}
		// +usage=The error message if the action failed
		err?: string
	}
	...
}

#Delete: {
	#do:       "delete"
	#provider: "kube"

	$params: {
		// +usage=The cluster to use
		cluster: *"" | string
		// +usage=The resource to delete
		value: {
			// +usage=The api version of the resource
			apiVersion: string
			// +usage=The kind of the resource
			kind: string
			// +usage=The metadata of the resource
			metadata: {
				// +usage=The name of the resource
				name?: string
				// +usage=The namespace of the resource
				namespace: *"default" | string
			}
		}
		// +usage=The filter to delete the resources
		filter?: {
			// +usage=The namespace to list the resources
			namespace?: string
			// +usage=The label selector to filter the resources
			matchingLabels?: {...}
		}
	}

	$returns?: {
		// +usage=The deleted resource will be filled in this field after the action is executed
		value?: {...}
		// +usage=The error message if the action failed
		err?: string
	}
	...
}
