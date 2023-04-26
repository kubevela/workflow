// time.cue

#DateToTimestamp: {
	#do:       "timestamp"
	#provider: "op"

	date:   string
	layout: *"" | string

	timestamp?: int64
	...
}

#TimestampToDate: {
	#do:       "date"
	#provider: "op"

	timestamp: int64
	layout:    *"" | string

	date?: string
	...
}
