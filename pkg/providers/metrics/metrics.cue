// metrics.cue

#PromCheck: {
	#do:       "promCheck"
	#provider: "metrics"

	$params: {
		query:          string
		metricEndpoint: *"http://prometheus-server.o11y-system.svc:9090" | string
		condition:      string
		failDuration:   *"2m" | string
		duration:       *"5m" | string
	}

	$returns?: {
		message?: string
		failed:	bool
		result: bool
	}
	...
}
