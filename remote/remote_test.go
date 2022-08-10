package remote

import (
	"reflect"
	"testing"
)

func TestCheckIsJSONObjectOrArray(t *testing.T) {
	tests := map[string]struct {
		input string
		want  bool
	}{
		"empty string":                                {want: false, input: ""},
		"just single quotes":                          {want: false, input: "''"},
		"just double quotes":                          {want: false, input: `""`},
		"normal text with no quotes":                  {want: false, input: "normal text"},
		"number with no quotes":                       {want: false, input: "123456"},
		"normal text inside double quotes":            {want: false, input: `"normal text inside quotes"`},
		"normal text inside single quotes":            {want: false, input: "'normal text inside quotes'"},
		"normal text inside double quotes w/ padding": {want: false, input: `   "normal text inside quotes"    `},
		"normal text inside single quotes w/ padding": {want: false, input: "   'normal text inside quotes'    "},
		"missing quotes json":                         {want: false, input: `{"broken":json"}`},
		"json inside extra quotes":                    {want: false, input: `"{"broken":"json"}"`},
		"missing closing parenthesis":                 {want: false, input: `{"broken":"json object"`},
		"wrong numerical key json":                    {want: false, input: ` { 1 : " wrong" } `},
		"array object":                                {want: true, input: "[1,2,3]"},
		"array object with strings":                   {want: true, input: "[\"a\",\"b\",\"c\"]"},
		"array object with strings & padding":         {want: true, input: "  [\"a\",\"b\",\"c\"]    "},
		"standard json":                               {want: true, input: `{"object":"json"}`},
		"json with extra spacing":                     {want: true, input: `  { "object space" : "with spacing" } `},
		"empty array should be true":                  {want: true, input: "[]"},
		"empty object should be true":                 {want: true, input: "{}"},
		"object surrounded with single quotes":        {want: true, input: `'{"object":"json"}'`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := isJSONObjectOrArray(tc.input)
			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' isJSONObjectOrArray(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}

func TestGetCleanWpCliArgumentArray(t *testing.T) {
	tests := map[string]struct {
		errString string
		input     string
		want      []string
	}{
		"vip whatever should not be changed":                                          {errString: "", want: []string{"vip", "whatever"}, input: "vip whatever"},
		"wp option update with array should not be changed":                           {errString: "", want: []string{"wp", "option", "update", "someoption", `["stuff","things"]`, "--format=json"}, input: "wp option update someoption [\"stuff\",\"things\"] --format=json"},
		"wp option update with array wrapped in single quotes should not be changed":  {errString: "", want: []string{"wp", "option", "update", "someoption", `'["stuff","things"]'`, "--format=json"}, input: "wp option update someoption '[\"stuff\",\"things\"]' --format=json"},
		"wp option update with object should not be changed":                          {errString: "", want: []string{"wp", "option", "update", "someoption", `{"val1":"stuff","val2":"things"}`, "--format=json"}, input: "wp option update someoption {\"val1\":\"stuff\",\"val2\":\"things\"} --format=json"},
		"wp option update with object wrapped in single quotes should not be changed": {errString: "", want: []string{"wp", "option", "update", "someoption", `'{"val1":"stuff","val2":"things"}'`, "--format=json"}, input: "wp option update someoption '{\"val1\":\"stuff\",\"val2\":\"things\"}' --format=json"},
		"wp option update with quotes in value should be changed":                     {errString: "", want: []string{"wp", "option", "update", "someoption", `stuff,things`}, input: "wp option update someoption \"stuff\",\"things\""},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got, err := getCleanWpCliArgumentArray(tc.input)

			if err != nil && tc.errString != err.Error() {
				t.Fatalf("testing '%v' getCleanWpCliArgumentArray(\"%v\") expected error: %v, got: %v", name, tc.input, tc.errString, err.Error())
			}

			if err == nil && tc.errString != "" {
				t.Fatalf("testing '%v' getCleanWpCliArgumentArray(\"%v\") expected error string: %v, got: nil", name, tc.input, tc.errString)
			}

			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' getCleanWpCliArgumentArray(\"%v\") expected:\n\t%v\ngot:\n\t%v", name, tc.input, tc.want, got)
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
		"config edit should fail":                    {errString: "WP CLI command 'config' is not permitted", want: "", input: "config edit"},
		"db create should fail":                      {errString: "WP CLI command 'db create' is not permitted", want: "", input: "db create"},
		"db export should fail":                      {errString: "WP CLI command 'db export' is not permitted", want: "", input: "db export somefile.sql"},
		"db reset --yes should fail":                 {errString: "WP CLI command 'db reset' is not permitted", want: "", input: "db reset --yes"},
		"db query without a query param should fail": {errString: "WP CLI command 'db query' requires a query parameter", want: "", input: "db query"},
		"db query with a query param should pass":    {errString: "", want: "db query \"SELECT * FROM whatever\"", input: "db query \"SELECT * FROM whatever\""},
		"db query with trailing spaces should fail":  {errString: "WP CLI command 'db query' requires a query parameter", want: "", input: "db query     "},
		"media regenerate should fail":               {errString: "WP CLI command 'media regenerate' is not permitted", want: "", input: "media regenerate"},
		"media import file should pass":              {errString: "", want: "media import https://example.com/cutekitties.png", input: "media import https://example.com/cutekitties.png"},
		"vip support-user should fail":               {errString: "WP CLI command 'vip support-user' is not permitted", want: "", input: "vip support-user"},
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
