/*
 Copyright 2021. The KubeVela Authors.

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

package time

import (
	"context"
	"fmt"
	"testing"

	"github.com/pkg/errors"
	"github.com/stretchr/testify/require"
)

func TestTimestamp(t *testing.T) {
	ctx := context.Background()
	testcases := map[string]struct {
		from        Vars
		expected    int64
		expectedErr error
	}{
		"test convert date with default time layout": {
			from: Vars{
				Date: "2021-11-07T01:47:51Z",
			},
			expected:    1636249671,
			expectedErr: nil,
		},
		"test convert date with RFC3339 layout": {
			from: Vars{
				Date:   "2021-11-07T01:47:51Z",
				Layout: "2006-01-02T15:04:05Z07:00",
			},
			expected:    1636249671,
			expectedErr: nil,
		},
		"test convert date with RFC1123 layout": {
			from: Vars{
				Date:   "Fri, 01 Mar 2019 15:00:00 GMT",
				Layout: "Mon, 02 Jan 2006 15:04:05 MST",
			},
			expected:    1551452400,
			expectedErr: nil,
		},
		"test convert without date": {
			from:        Vars{},
			expected:    0,
			expectedErr: fmt.Errorf("empty date to convert"),
		},
		"test convert date with wrong time layout": {
			from: Vars{
				Date:   "2021-11-07T01:47:51Z",
				Layout: "Mon, 02 Jan 2006 15:04:05 MST",
			},
			expected:    0,
			expectedErr: errors.New(`parsing time "2021-11-07T01:47:51Z" as "Mon, 02 Jan 2006 15:04:05 MST": cannot parse "2021-11-07T01:47:51Z" as "Mon"`),
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			res, err := Timestamp(ctx, &Params{
				Params: tc.from,
			})
			if tc.expectedErr != nil {
				r.Equal(tc.expectedErr.Error(), err.Error())
				return
			}
			r.NoError(err)
			r.Equal(tc.expected, res.Timestamp)
		})
	}
}

func TestDate(t *testing.T) {
	ctx := context.Background()
	testcases := map[string]struct {
		from     Vars
		expected string
	}{
		"test convert timestamp to default time layout": {
			from: Vars{
				Timestamp: 1636249671,
			},
			expected: "2021-11-07T01:47:51Z",
		},
		"test convert date to RFC3339 layout": {
			from: Vars{
				Timestamp: 1636249671,
				Layout:    "2006-01-02T15:04:05Z07:00",
			},
			expected: "2021-11-07T01:47:51Z",
		},
		"test convert date to RFC1123 layout": {
			from: Vars{
				Timestamp: 1551452400,
				Layout:    "Mon, 02 Jan 2006 15:04:05 MST",
			},
			expected: "Fri, 01 Mar 2019 15:00:00 UTC",
		},
		"test convert without timestamp": {
			from:     Vars{},
			expected: "1970-01-01T00:00:00Z",
		},
	}

	for name, tc := range testcases {
		t.Run(name, func(t *testing.T) {
			r := require.New(t)
			res, err := Date(ctx, &Params{
				Params: tc.from,
			})
			r.NoError(err)
			r.Equal(tc.expected, res.Date)
		})
	}
}
