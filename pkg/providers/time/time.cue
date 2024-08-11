// time.cue

#DateToTimestamp: {
	#do:       "timestamp"
	#provider: "time"

	$params: {
		date:   string
		layout: *"" | string
	}

	$returns?: {
		timestamp: int64
	}
	...
}

#TimestampToDate: {
	#do:       "date"
	#provider: "time"

	$params: {
		timestamp: int64
		layout:    *"" | string
	}

	$returns?: {
		date: string
	}
	...
}
