package remote

import (
	"reflect"
	"strings"
	"testing"
)

func TestRedactArgs(t *testing.T) {
	tests := map[string]struct {
		input string
		want  string
	}{
		"sensitive flag value":      {input: "user update 5 --user_pass=hunter2", want: "user update 5 --user_pass=<redacted>"},
		"custom secret flags":       {input: "foo sync --client_secret=a --access-token=b --apikey=c", want: "foo sync --client_secret=<redacted> --access-token=<redacted> --apikey=<redacted>"},
		"separate flag value":       {input: "foo login --password hunter2 --url example.com", want: "foo login --password <redacted> --url example.com"},
		"non-secret flags are kept": {input: "post list --post_author=3 --url example.com --format=json", want: "post list --post_author=3 --url example.com --format=json"},
		"config secret":             {input: "wp config set DB_PASSWORD hunter2", want: "wp config set DB_PASSWORD <redacted>"},
		"custom config key":         {input: "config set API_KEY hunter2", want: "config set API_KEY <redacted>"},
		"custom config salt":        {input: "config set CUSTOM_SALT hunter2 --type=constant", want: "config set CUSTOM_SALT <redacted> --type=constant"},
		"option value":              {input: "option update my_key hunter2 --autoload=no", want: "option update my_key <redacted> --autoload=no"},
		"meta value":                {input: "--url example.com user meta update 5 api_secret hunter2", want: "--url example.com user meta update 5 api_secret <redacted>"},
		"db query and the rest":     {input: "db query SELECT 'hunter2' --skip-column-names", want: "db query <redacted>"},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			got := redactArgs(strings.Fields(tc.input))

			if want := strings.Fields(tc.want); !reflect.DeepEqual(want, got) {
				t.Fatalf("redactArgs(%q) expected: %q, got: %q", tc.input, want, got)
			}
		})
	}
}
