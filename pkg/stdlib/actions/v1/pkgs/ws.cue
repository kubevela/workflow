#Load: {
	#do:        "load"
	component?: string
	value?: {...}
	...
}

#Export: {
	#do:       "export"
	component: string
	value:     _
}

#DoVar: {
	#do: "var"

	// +usage=The method to call on the variable
	method: *"Get" | "Put"
	// +usage=The path to the variable
	path: string
	// +usage=The value of the variable
	value?: _
}
