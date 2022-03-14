package remote

import (
	"testing"
)

func TestCheckIsJSONObject(t *testing.T) {
	valuesToTestFalse := []string{
		"\"Valid JSON String that should be excluded\"",
		"wrong json string"}
	valuesToTestTrue := []string{"{\"correct\":\"json object\"}", " { \"correct\" : \"json object with extra spacing\" } "}
	for _, val := range valuesToTestFalse {
		if isJSONObject(val) {
			t.Fatalf("isJSONObject() returned true for %s while it should've been false", val)
		}
	}
	for _, val := range valuesToTestTrue {
		if !isJSONObject(val) {
			t.Fatalf("isJSONObject() returned false for %s while it should've been true", val)
		}
	}
}
