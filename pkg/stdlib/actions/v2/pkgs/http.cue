#Do: {
	#do:       "do"
	#provider: "http"

	method: *"GET" | "POST" | "PUT" | "DELETE"
	url:    string
	request?: {
		timeout?: string
		body?:    string
		header?: [string]:  string
		trailer?: [string]: string
		ratelimiter?: {
			limit:  int
			period: string
		}
		...
	}
	tls_config?: secret: string
	response: {
		body: string
		header?: [string]: [...string]
		trailer?: [string]: [...string]
		statusCode: int
		...
	}
	...
}
