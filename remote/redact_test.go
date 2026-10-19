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
		"custom config password":    {input: "config set SMTP_PASSWORD hunter2", want: "config set SMTP_PASSWORD <redacted>"},
		"non-ASCII config constant": {input: "config set ACMÉ_KEY hunter2", want: "config set ACMÉ_KEY <redacted>"},
		"option value":              {input: "option update my_key hunter2 --autoload=no", want: "option update my_key <redacted> --autoload=no"},
		"value after --":            {input: "option update api_secret -- --hunter2", want: "option update api_secret -- <redacted>"},
		"-- as an argument":         {input: "option update -- hunter2", want: "option update -- <redacted>"},
		"meta value":                {input: "--url example.com user meta update 5 api_secret hunter2", want: "--url example.com user meta update 5 api_secret <redacted>"},
		"network meta value":        {input: "network meta update 1 api_secret hunter2", want: "network meta update 1 api_secret <redacted>"},
		"network option patch":      {input: "site option patch update settings api_key hunter2", want: "site option patch update settings api_key <redacted>"},
		"db query and the rest":     {input: "db query SELECT 'hunter2' --skip-column-names", want: "db query <redacted>"},
		"boolean sensitive flag":    {input: "db --show-password query SELECT 'hunter2'", want: "db --show-password <redacted> <redacted>"},
		"empty flag name":           {input: "config set DB_PASSWORD --=hunter2", want: "config set DB_PASSWORD <redacted>"},
		"global flag before value":  {input: "config set DB_PASSWORD --url hunter2", want: "config set DB_PASSWORD --url <redacted>"},
		"global flag in command":    {input: "db --user query SELECT 'hunter2'", want: "db --user query <redacted>"},
		"mixed flag readings":       {input: "--url example.com db --show-password query SELECT 'hunter2'", want: "--url example.com db --show-password <redacted> <redacted>"},
		"too many ambiguous flags":  {input: strings.Repeat("--url x ", 9) + "option update k hunter2", want: "--url " + strings.Repeat("<redacted> ", 21)},
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
