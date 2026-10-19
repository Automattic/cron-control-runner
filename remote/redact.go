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

// secretConfigName matches wp-config constants that hold secrets: any name ending in _KEY, _SALT,
// _PASSWORD, _SECRET or _TOKEN, which covers DB_PASSWORD and WordPress's keys and salts as well as
// custom constants such as API_KEY. The prefix is not restricted, since PHP constant names can be
// non-ASCII or start with "_".
var secretConfigName = regexp.MustCompile(`(?i)_(?:KEY|SALT|PASSWORD|SECRET|TOKEN)$`)

// maxAmbiguousArgs caps the arguments redactArgs reads both as positional and as a flag's value.
const maxAmbiguousArgs = 8

// globalFlagsWithSeparateValue are WP-CLI global flags that people may write with their value as
// the next argument, as in "--url example.com".
var globalFlagsWithSeparateValue = []string{"context", "exec", "http", "path", "require", "ssh", "url", "user"}

// isFlag reports whether WP-CLI reads arg as a flag: "--" followed by a name that doesn't start
// with "=". Anything else, "--=value" included, is a positional argument.
func isFlag(arg string) bool {
	return len(arg) > 2 && strings.HasPrefix(arg, "--") && arg[2] != '='
}

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

	// WP-CLI reads a flag without "=" as a boolean, so the argument after it is positional. People
	// still write "--flag value", though, so a sensitive or global flag may be meant to take the next
	// argument as its value. Each such argument is ambiguous: commands are detected under every
	// combination of readings, and a secret found under any of them is redacted.
	var nonFlags []int  // indexes in args of the arguments WP-CLI reads as positional
	var ambiguous []int // those of them a preceding flag may be meant to take as its value
	bareSensitiveFlag := false
	for i, arg := range args {
		// WP-CLI up to 2.12 treats "--" as an ordinary argument, while newer versions treat it as the
		// end of options. Rather than guess where a secret sits after it, redact everything after it.
		if arg == "--" {
			for j := i + 1; j < len(out); j++ {
				out[j] = redacted
			}
			break
		}

		if !isFlag(arg) {
			nonFlags = append(nonFlags, i)
			continue
		}

		name, _, hasValue := strings.Cut(arg[2:], "=")
		nextIsValue := !hasValue && i+1 < len(args) && args[i+1] != "--" && !isFlag(args[i+1])

		switch {
		case isSensitiveFlag(name) && hasValue:
			out[i] = "--" + name + "=" + redacted
		case isSensitiveFlag(name):
			bareSensitiveFlag = true
			if nextIsValue {
				out[i+1] = redacted
				ambiguous = append(ambiguous, i+1)
			}
		case nextIsValue && slices.Contains(globalFlagsWithSeparateValue, strings.ToLower(name)):
			ambiguous = append(ambiguous, i+1)
		}
	}

	// Each ambiguous argument doubles the readings, so past a few, redact everything from the
	// first one instead.
	if len(ambiguous) > maxAmbiguousArgs {
		for i := ambiguous[0]; i < len(out); i++ {
			out[i] = redacted
		}
		return out
	}

	truncateAt := -1
	for taken := 0; taken < 1<<len(ambiguous); taken++ { // bit j set: ambiguous[j] is a flag's value
		positional := make([]int, 0, len(nonFlags))
		for _, i := range nonFlags {
			if j := slices.Index(ambiguous, i); j < 0 || taken&(1<<j) == 0 {
				positional = append(positional, i)
			}
		}

		i, remainder := secretArg(args, positional, bareSensitiveFlag)
		if i < 0 {
			continue
		}
		if !remainder {
			out[i] = redacted
		} else if truncateAt < 0 || i < truncateAt {
			truncateAt = i
		}
	}

	if truncateAt >= 0 {
		return append(out[:truncateAt], redacted)
	}
	return out
}

// secretArg returns the index in args of the positional argument that holds a secret for the
// command args run, or -1. remainder reports that the secret runs to the end of args, as an SQL
// query does.
func secretArg(args []string, positional []int, bareSensitiveFlag bool) (index int, remainder bool) {
	word := func(n int) string {
		if n < len(positional) {
			return strings.ToLower(args[positional[n]])
		}
		return ""
	}

	o := 0
	if word(0) == "wp" {
		o = 1
	}
	command, subcommand, third := word(o), word(o+1), word(o+2)
	metaCommands := []string{"comment", "network", "post", "site", "term", "user"}
	setActions := []string{"add", "set", "update"}
	patchActions := []string{"insert", "update"}

	value := -1 // index in positional of the secret value
	switch {
	case command == "config" && subcommand == "set" && secretConfigName.MatchString(third):
		value = o + 3
	case command == "option" && subcommand == "get" && bareSensitiveFlag:
		value = o + 3
	case command == "db" && subcommand == "query":
		value = o + 2
		remainder = true
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
	case command == "site" && subcommand == "option" && third == "patch" && slices.Contains(patchActions, word(o+3)):
		if len(positional) > o+6 {
			value = len(positional) - 1
		}
	}

	if value < 0 || value >= len(positional) {
		return -1, false
	}
	return positional[value], remainder
}
