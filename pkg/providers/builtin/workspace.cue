// workspace.cue

#DoVar: {
	#do:       "var"
	#provider: "builtin"

	$params: {
		// +usage=The method to call on the variable
		method: *"Get" | "Put"
		// +usage=The path to the variable
		path: string
		// +usage=The value of the variable
		value?: _
	}

	$returns?: {
		// +usage=The value of the variable
		value: _
	}
}

#ConditionalWait: {
	#do:       "wait"
	#provider: "builtin"

	$params: {
		// +usage=If continue is false, the step will wait for continue to be true.
		continue: *false | bool
		// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
		message?: string
	}
}

#Suspend: {
	#do:       "suspend"
	#provider: "builtin"

	$params: {
		// +usage=Specify the wait duration time to resume automaticlly such as "30s", "1min" or "2m15s"
		duration?: string
		// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
		message?: string
	}
}

#Break: {
	#do:       "break"
	#provider: "builtin"

	$params: {
		// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
		message?: string
	}
}

#Fail: {
	#do:       "fail"
	#provider: "builtin"

	$params: {
		// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
		message?: string
	}
}

#Message: {
	#do:       "message"
	#provider: "builtin"

	$params: {
		// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
		message?: string
	}
}

#Steps: {
	...
}

