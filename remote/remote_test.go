package remote

import (
	"testing"
)

func TestCheckIsJSONObject(t *testing.T) {
	valuesToTestFalse := []string{
		"\"Valid JSON String that should be excluded\"",
		"wrong json string",
		"\"{\\\"correct\\\":\\\"json object with \\\"}\""}
	valuesToTestTrue := []string{"{\"correct\":\"json object\"}", " { \"correct\" : \"json object with extra spacing\" } "}
	for _, val := range valuesToTestFalse {
		if isJSONObject(val) {
			t.Fatalf("isJSON() returned true for %s while it should've been false", val)
		}
	}
	for _, val := range valuesToTestTrue {
		if !isJSONObject(val) {
			t.Fatalf("isJSON() returned false for %s while it should've been false", val)
		}
	}
}
