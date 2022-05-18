package remote

import (
	"reflect"
	"testing"
)

func TestCheckIsJSONObject(t *testing.T) {
	tests := map[string]struct {
		input string
		want  bool
	}{
		"normal text with no quotes":                {want: false, input: "normal text"},
		"array object":                              {want: false, input: "[1,2,3]"},
		"valid json string that should be excluded": {want: false, input: `"normal text inside quotes"`},
		"missing quotes json":                       {want: false, input: `{"broken":json"}`},
		"json inside extra quotes":                  {want: false, input: `"{"broken":"json"}"`},
		"missing closing parenthesis":               {want: false, input: `{"broken":"json object"`},
		"wrong numerical key json":                  {want: false, input: ` { 1 : " wrong" } `},
		"standard json":                             {want: true, input: `{"object":"json"}`},
		"json with extra spacing":                   {want: true, input: `  { "object space" : "with spacing" } `},
		"json with extra spacing -- should fail":    {want: false, input: `  { "object space" : "with spacing" } `},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := isJSONObject(tc.input)
			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' isJSONObject(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}
