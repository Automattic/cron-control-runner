package remote

import (
	"regexp"
	"slices"
	"strings"
)

const redacted = "<redacted>"

// sensitiveFlagName matches flag names whose values are secrets. It matches parts of names so
// custom commands' flags such as --client_secret, --access-token and --apikey are covered too.
var sensitiveFlagName = regexp.MustCompile(`(?i)^(?:api|pw|login)$|pass|secret|token|key|credential|salt`)

// secretConfigName matches wp-config constants that hold secrets.
var secretConfigName = regexp.MustCompile(`(?i)^(?:DB_PASSWORD|(?:AUTH|SECURE_AUTH|LOGGED_IN|NONCE)_(?:KEY|SALT))$`)

// globalFlagsWithSeparateValue are WP-CLI global flags that may take their value as the next argument.
var globalFlagsWithSeparateValue = []string{"context", "exec", "http", "path", "require", "ssh", "url", "user"}

func isSensitiveFlag(name string) bool {
	if sensitiveFlagName.MatchString(name) {
		return true
	}

	// "auth" covers auth tokens and headers, but not author fields such as --post_author.
	name = strings.ToLower(name)
	return strings.Contains(name, "authoriz") || strings.Contains(strings.ReplaceAll(name, "author", ""), "auth")
}

// redactArgs returns a copy of WP-CLI arguments that is safe to log. It replaces the values of
// sensitive flags, and the positional values of commands that take secrets (option values,
// DB_PASSWORD, SQL queries...), with <redacted>.
//
// Keep the rules in sync with WPCLICommand.sanitizeCommandForAuditLog in Parker (vip-go-api), which
// redacts the same commands for the VIP Dashboard's WP-CLI log.
func redactArgs(args []string) []string {
	out := slices.Clone(args)

	positional := make([]int, 0, len(out)) // indexes in out of the positional arguments
	bareSensitiveFlag := false
	for i := 0; i < len(out); i++ {
		if !strings.HasPrefix(out[i], "--") {
			positional = append(positional, i)
			continue
		}

		name, _, hasValue := strings.Cut(out[i][2:], "=")
		nextIsValue := !hasValue && i+1 < len(out) && !strings.HasPrefix(out[i+1], "--")

		switch {
		case isSensitiveFlag(name) && hasValue:
			out[i] = "--" + name + "=" + redacted
		case isSensitiveFlag(name) && nextIsValue:
			i++
			out[i] = redacted
		case isSensitiveFlag(name):
			bareSensitiveFlag = true
		case nextIsValue && slices.Contains(globalFlagsWithSeparateValue, strings.ToLower(name)):
			i++
		}
	}

	word := func(n int) string {
		if n < len(positional) {
			return strings.ToLower(out[positional[n]])
		}
		return ""
	}

	o := 0
	if word(0) == "wp" {
		o = 1
	}
	command, subcommand, third := word(o), word(o+1), word(o+2)
	metaCommands := []string{"comment", "post", "site", "term", "user"}
	setActions := []string{"add", "set", "update"}
	patchActions := []string{"insert", "update"}

	value := -1 // index in positional of the secret value
	redactRemainder := false
	switch {
	case command == "config" && subcommand == "set" && secretConfigName.MatchString(third):
		value = o + 3
	case command == "option" && subcommand == "get" && bareSensitiveFlag:
		value = o + 3
	case command == "db" && subcommand == "query":
		value = o + 2
		redactRemainder = true
	case command == "user" && subcommand == "check-password":
		value = o + 3
	case command == "option" && slices.Contains(setActions, subcommand):
		value = o + 3
	case slices.Contains([]string{"cache", "option", "transient"}, command) && subcommand == "patch" && slices.Contains(patchActions, third):
		if len(positional) > o+5 {
			value = len(positional) - 1
		}
	case slices.Contains(metaCommands, command) && subcommand == "meta" && slices.Contains(setActions, third):
		value = o + 5
	case slices.Contains(metaCommands, command) && subcommand == "meta" && third == "patch" && slices.Contains(patchActions, word(o+3)):
		if len(positional) > o+7 {
			value = len(positional) - 1
		}
	case command == "transient" && subcommand == "set":
		value = o + 3
	case command == "cache" && slices.Contains([]string{"add", "replace", "set"}, subcommand):
		value = o + 3
	case command == "site" && subcommand == "transient" && third == "set":
		value = o + 4
	case command == "site" && subcommand == "option" && slices.Contains(setActions, third):
		value = o + 4
	}

	if value < 0 || value >= len(positional) {
		return out
	}

	if redactRemainder {
		return append(out[:positional[value]], redacted)
	}

	out[positional[value]] = redacted
	return out
}
