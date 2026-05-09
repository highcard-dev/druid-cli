package api

import (
	"reflect"
	"testing"
)

func TestCreateScrollRequestHasNoRuntime(t *testing.T) {
	if _, ok := reflect.TypeOf(CreateScrollRequest{}).FieldByName("Runtime"); ok {
		t.Fatal("CreateScrollRequest should not expose runtime")
	}
}
