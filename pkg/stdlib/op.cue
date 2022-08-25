import (
	"encoding/json"
	"encoding/base64"
	"strings"
)

#ConditionalWait: {
	#do:      "wait"
	continue: bool
	message?: string
}

#Break: {
	#do:      "break"
	message?: string
}

#Fail: {
	#do:      "fail"
	message?: string
}

#Apply: kube.#Apply

#ApplyInParallel: kube.#ApplyInParallel

#Read: kube.#Read

#List: kube.#List

#Delete: kube.#Delete

#DingTalk: #Steps & {
	message: {...}
	dingUrl: string
	do:      http.#Do & {
		method: "POST"
		url:    dingUrl
		request: {
			body: json.Marshal(message)
			header: "Content-Type": "application/json"
		}
	}
}

#Lark: #Steps & {
	message: {...}
	larkUrl: string
	do:      http.#Do & {
		method: "POST"
		url:    larkUrl
		request: {
			body: json.Marshal(message)
			header: "Content-Type": "application/json"
		}
	}
}

#Slack: #Steps & {
	message: {...}
	slackUrl: string
	do:       http.#Do & {
		method: "POST"
		url:    slackUrl
		request: {
			body: json.Marshal(message)
			header: "Content-Type": "application/json"
		}
	}
}

#HTTPGet: http.#Do & {method: "GET"}

#HTTPPost: http.#Do & {method: "POST"}

#HTTPPut: http.#Do & {method: "PUT"}

#HTTPDelete: http.#Do & {method: "DELETE"}

#ConvertString: util.#String

#Log: util.#Log

#DateToTimestamp: time.#DateToTimestamp

#TimestampToDate: time.#TimestampToDate

#SendEmail: email.#Send

#PatchK8sObject: util.#PatchK8sObject

#Steps: {
	#do: "steps"
	...
}

#Task: task.#Task

NoExist: _|_

context: _
