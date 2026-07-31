/*
Copyright 2026 The KubeVela Authors.

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package sensitive

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestExtractPathsAreAbsoluteFromTheTemplateRoot(t *testing.T) {
	// A step output's valueFrom is a path from the template root, so the paths
	// have to be comparable to it without the caller knowing which block the
	// marker sat in.
	got, err := ExtractPaths(`
output: {
	value: {
		host: string
		// +sensitive
		password: string
	}
}
parameter: {
	// +sensitive
	token: string
}`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"output.value.password", "parameter.token"}, got)
}

func TestExtractPathsDescendsAndDedupes(t *testing.T) {
	got, err := ExtractPaths(`
output: {
	db: {
		host: string
		// +sensitive
		password: string
		tls: {
			// +sensitive
			key: string
		}
	}
	// +sensitive
	token: string
}
objStatus: {
	// +sensitive
	token: string
}`)
	require.NoError(t, err)
	require.ElementsMatch(t,
		[]string{"output.db.password", "output.db.tls.key", "output.token", "objStatus.token"},
		got)
}

// read-object's whole output: block is two `if` arms. kubevela's extractor
// skips comprehensions, so a mark in one would have been missed entirely.
func TestExtractPathsDescendsIntoIfAndForBodies(t *testing.T) {
	got, err := ExtractPaths(`
output: {
	if parameter.withAuth {
		// +sensitive
		token: string
	}
	for k, v in parameter.extras {
		"\(k)": {
			// +sensitive
			secret: v
		}
	}
}`)
	require.NoError(t, err)
	require.Contains(t, got, "output.token",
		"a field in an if body sits at the enclosing level once evaluated")
	require.Contains(t, got, "output.secret",
		"a dynamic label names no matchable segment, but the mark under it is still found")
}

// `output: op.#Read & {...}` is the shape every kube-reading step template has.
func TestExtractPathsDescendsIntoEmbeddingsAndConjunctions(t *testing.T) {
	got, err := ExtractPaths(`
output: {
	op.#Read & {
		value: {
			// +sensitive
			data: {}
		}
	}
}
other: op.#Read & {
	// +sensitive
	credential: string
}`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"output.value.data", "other.credential"}, got)
}

func TestExtractPathsHandlesListElementsAndQuotedLabels(t *testing.T) {
	got, err := ExtractPaths(`
output: {
	members: [...{
		name: string
		// +sensitive
		token: string
	}]
	// +sensitive
	"api-key": string
}`)
	require.NoError(t, err)
	require.ElementsMatch(t, []string{"output.members.token", "output.api-key"}, got)
}

func TestExtractPathsOnTemplatesWithNothingToFind(t *testing.T) {
	for name, template := range map[string]string{
		"empty":        ``,
		"no marker":    `output: {host: string}`,
		"not a struct": `output: string`,
	} {
		t.Run(name, func(t *testing.T) {
			got, err := ExtractPaths(template)
			require.NoError(t, err)
			require.Nil(t, got)
		})
	}
}

// The failure this guards is silent under-redaction: a caller handed an empty
// list cannot tell "nothing is secret" from "I could not read the markers".
func TestExtractPathsRejectsAnUnparseableTemplate(t *testing.T) {
	got, err := ExtractPaths(`this is not cue {{{`)
	require.Error(t, err)
	require.Nil(t, got)
}

func TestExtractPathsNeedsTheMarkerOnTheField(t *testing.T) {
	got, err := ExtractPaths(`
// +sensitive
output: {
	host: string
}`)
	require.NoError(t, err)
	require.Equal(t, []string{"output"}, got,
		"the mark is on output itself, which covers everything beneath it")
}

func TestCoversRunsInBothDirections(t *testing.T) {
	marks := []string{"output.value.data", "token"}

	require.True(t, Covers("output.value.data", marks), "the marked path itself")
	require.True(t, Covers("output.value.data.password", marks),
		"reading beneath a mark exposes what the mark protects")
	require.True(t, Covers("output.value", marks),
		"extracting a parent extracts the marked child with it")
	require.True(t, Covers("output", marks))
	require.True(t, Covers("token", marks))

	require.False(t, Covers("output.value.status", marks), "a sibling is not covered")
	require.False(t, Covers("tokenish", marks), "a prefix that is not a path segment")
	require.False(t, Covers("", marks))
	require.False(t, Covers("token", nil))
}

func TestCoversDropsListIndices(t *testing.T) {
	marks := []string{"output.members.token"}
	require.True(t, Covers("output.members.0.token", marks),
		"a mark on one element applies to them all")
	require.False(t, Covers("output.members.0.name", marks))
}

func TestIsPlainFieldPath(t *testing.T) {
	require.True(t, IsPlainFieldPath("output.value.data"))
	require.True(t, IsPlainFieldPath("$returns.token"))
	require.False(t, IsPlainFieldPath(""))
	require.False(t, IsPlainFieldPath(`output["value"]`))
	require.False(t, IsPlainFieldPath(`[ for v in output {v}]`))
}
