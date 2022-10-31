#Do: {
	#do:       "do"
	#provider: "http"

	// +usage=The method of HTTP request
	method: *"GET" | "POST" | "PUT" | "DELETE"
	// +usage=The url to request
	url: string
	// +usage=The request config
	request?: {
		// +usage=The timeout of this request
		timeout?: string
		// +usage=The request body
		body?: string
		// +usage=The header of the request
		header?: [string]: string
		// +usage=The trailer of the request
		trailer?: [string]: string
		// +usage=The rate limiter of the request
		ratelimiter?: {
			limit:  int
			period: string
		}
		...
	}
	// +usgae=The tls config of the request
	tls_config?: secret: string
	// +usage=The response of the request will be filled in this field after the action is executed
	response: {
		// +usage=The body of the response
		body: string
		// +usage=The header of the response
		header?: [string]: [...string]
		// +usage=The trailer of the response
		trailer?: [string]: [...string]
		// +usage=The status code of the response
		statusCode: int
		...
	}
	...
}
