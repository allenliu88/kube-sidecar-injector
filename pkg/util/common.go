package util

import "strings"

func UniformKey(key string) string {
	// https://stackoverflow.com/questions/55573724/create-a-patch-to-add-a-kubernetes-annotation
	// Reference: RFC 6901 https://www.rfc-editor.org/rfc/rfc6901#section-3
	return strings.ReplaceAll(key, "/", "~1")
}

func MergeMaps(one, two map[string]string) map[string]string {
	ret := make(map[string]string)

	// Add key-value pairs from one to the ret
	for key, value := range one {
		ret[key] = value
	}

	// Add key-value pairs from two to the ret
	for key, value := range two {
		ret[key] = value
	}

	return ret
}
