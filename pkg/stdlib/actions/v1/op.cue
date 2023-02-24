import (
	"encoding/json"
	"encoding/base64"
	"strings"
)

#ConditionalWait: {
	#do: "wait"

	// +usage=If continue is false, the step will wait for continue to be true.
	continue: bool
	// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
	message?: string
}

#Break: {
	#do: "break"

	// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
	message?: string
}

#Fail: {
	#do: "fail"

	// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
	message?: string
}

#Message: {
	#do: "message"

	// +usage=Optional message that will be shown in workflow step status, note that the message might be override by other actions.
	message?: string
}

#Apply: kube.#Apply

#Patch: kube.#Patch

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

#HTTPDo: http.#Do

#HTTPGet: http.#Do & {method: "GET"}

#HTTPPost: http.#Do & {method: "POST"}

#HTTPPut: http.#Do & {method: "PUT"}

#HTTPDelete: http.#Do & {method: "DELETE"}

#ConvertString: util.#String

#Log: util.#Log

#DateToTimestamp: time.#DateToTimestamp

#TimestampToDate: time.#TimestampToDate

#SendEmail: email.#Send

// The providers about the config
#CreateConfig: config.#Create
#DeleteConfig: config.#Delete
#ReadConfig:   config.#Read
#ListConfig:   config.#List

#PromCheck: metrics.#PromCheck

#PatchK8sObject: util.#PatchK8sObject

#Steps: {
	#do: "steps"
	...
}

#Task: task.#Task

NoExist: _|_

context: _
