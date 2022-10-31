#Send: {
	#do:       "send"
	#provider: "email"

	// +usage=The info of the sender
	from: {
		// +usage=The address of the sender
		address: string
		// +usage=The alias of the sender
		alias?: string
		// +usage=The password of the sender
		password: string
		// +usage=The host of the sender server
		host: string
		// +usage=The port of the sender server
		port: int
	}
	// +usgae=The email address list of the recievers
	to: [...string]
	// +usage=The content of the email
	content: {
		// +usage=The subject of the email
		subject: string
		// +usage=The body of the email
		body: string
	}
	stepID: context.stepSessionID
	...
}
