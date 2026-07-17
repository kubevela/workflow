import (
	"net"
	"strings"
)

metadata: {
	name:        "workflow-http-deny"
	alias:       "Workflow HTTP Denylist"
	description: "Additional host, IP, and CIDR destinations denied for workflow HTTP requests."
	scope:       "system"
	sensitive:   false
}

#IPAddress: string & net.IP
#IPCIDR:    string & net.IPCIDR

#Hostname:         string & =~"^([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(\\.([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?))*\\.?$"
#WildcardHostname: string & =~"^\\*\\.([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?)(\\.([A-Za-z0-9]([A-Za-z0-9-]{0,61}[A-Za-z0-9])?))*\\.?$"
#DenyHost:         #IPAddress | #Hostname | #WildcardHostname
#DenyCIDR:         #IPAddress | #IPCIDR

template: {
	outputs: configMap: {
		apiVersion: "v1"
		kind:       "ConfigMap"
		metadata: {
			name:      context.name
			namespace: context.namespace
		}
		data: {
			denyHosts: strings.Join(parameter.denyHosts, "\n")
			denyCIDRs: strings.Join(parameter.denyCIDRs, "\n")
		}
	}
	parameter: {
		// +usage=Exact hostnames, IP addresses, or leading wildcard hostnames to deny.
		denyHosts: [...#DenyHost]
		// +usage=IP addresses or CIDR ranges to deny.
		denyCIDRs: [...#DenyCIDR]
	}
}
