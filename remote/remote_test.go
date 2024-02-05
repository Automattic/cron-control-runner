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

func TestValidateCommand(t *testing.T) {
	tests := map[string]struct {
		errString string
		input     string
		want      string
	}{
		"media import file should pass":              {errString: "", want: "media import https://example.com/cutekitties.png", input: "media import https://example.com/cutekitties.png"},
		"vip whatever should pass":                   {errString: "", want: "vip whatever", input: "vip whatever"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := validateCommand(tc.input)

			if err != nil && tc.errString != err.Error() {
				t.Fatalf("testing '%v' validateCommand(\"%v\") expected error: %v, got: %v", name, tc.input, tc.errString, err.Error())
			}

			if err == nil && tc.errString != "" {
				t.Fatalf("testing '%v' validateCommand(\"%v\") expected error string: %v, got: nil", name, tc.input, tc.errString)
			}

			if tc.want != got {
				t.Fatalf("testing '%v' validateCommand(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}
