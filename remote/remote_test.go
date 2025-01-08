package remote

import (
	"reflect"
	"testing"
)

func TestValidateCommand(t *testing.T) {
	tests := map[string]struct {
		errString string
		input     string
		want      string
	}{
		"media import file should pass": {errString: "", want: "media import https://example.com/cutekitties.png", input: "media import https://example.com/cutekitties.png"},
		"vip whatever should pass":      {errString: "", want: "vip whatever", input: "vip whatever"},
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

func TestTokenizeString(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"no quotes":            {want: []string{"option", "update", "cow", "a"}, input: "option update cow a"},
		"single quotes":        {want: []string{"option", "update", "cow", `'a b'`}, input: "option update cow 'a b'"},
		"double quotes":        {want: []string{"option", "update", "cow", `"a b"`}, input: `option update cow "a b"`},
		"nested double quotes": {want: []string{"option", "update", "cow", `"a \"b\""`}, input: `option update cow "a \"b\""`},
		"one nested quote":     {want: []string{`"a\"b"`}, input: `"a\"b"`},

		// Compatibility with VIP CLI bugs
		"named parameters": {want: []string{"option", "list", `--search="a b"`}, input: `option list --search="a b"`},

		// These sequences should not occur; if they do, someone is trying to break the system
		"embedded quotes":       {want: []string{`opt""i''on`}, input: `opt""i''on`},
		"unbalanced quotes (1)": {want: []string{`"a 'b`}, input: `"a 'b`},
		"unbalanced quotes (2)": {want: []string{`a" 'b`}, input: `a" 'b`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := tokenizeString(tc.input)

			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' tokenizeString(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}

func TestGetCleanWpCliArgumentArray(t *testing.T) {
	tests := map[string]struct {
		input string
		want  []string
	}{
		"no quotes":                {want: []string{"option", "update", "cow", "a"}, input: "option update cow a"},
		"single quotes":            {want: []string{"option", "update", "cow", "a b"}, input: "option update cow 'a b'"},
		"double quotes":            {want: []string{"option", "update", "cow", "a b"}, input: `option update cow "a b"`},
		"nested double quotes":     {want: []string{"option", "update", "cow", `a "b"`}, input: `option update cow "a \"b\""`},
		"json":                     {want: []string{"option", "update", "cow", `{"a":"b"}`}, input: `option update cow {"a":"b"}`},
		"vip-cli bugs":             {want: []string{"option", "list", `--search=a b`}, input: `option list --search="a b"`},
		"proper quoting":           {want: []string{"option", "list", `--search=a b`}, input: `"option" "list" "--search=a b"`},
		"proper quoting w/nesting": {want: []string{"option", "list", `--search="a b"`}, input: `"option" "list" "--search=\"a b\""`},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := getCleanWpCliArgumentArray(tc.input)

			if !reflect.DeepEqual(tc.want, got) {
				t.Fatalf("testing '%v' getCleanWpCliArgumentArray(\"%v\") expected: %v, got: %v", name, tc.input, tc.want, got)
			}
		})
	}
}
