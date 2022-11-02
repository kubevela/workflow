#PatchK8sObject: {
	#do:       "patch-k8s-object"
	#provider: "util"
	value: {...}
	patch: {...}
	result: {...}
	...
}

#String: {
	#do:       "string"
	#provider: "util"

	bt:   bytes
	str?: string
	...
}

#Log: {
	#do:       "log"
	#provider: "util"

	// +usage=The data to print in the controller logs
	data?: {...} | string
	// +usage=The log level of the data
	level: *3 | int
	// +usage=The log source of this step. You can specify it from a url or resources. Note that if you set source in multiple op.#Log, only the latest one will work
	source?: close({
		// +usage=Specify the log source url of this step
		url: string
	}) | close({
		// +usage=Specify the log resources of this step
		resources?: [...{
			// +usage=Specify the name of the resource
			name?: string
			// +usage=Specify the cluster of the resource
			cluster?: string
			// +usage=Specify the namespace of the resource
			namespace?: string
			// +usage=Specify the label selector of the resource
			labelSelector?: {...}
		}]
	})
}
