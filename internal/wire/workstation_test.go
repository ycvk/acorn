package wire

import (
	"reflect"
	"testing"
)

func TestParseBrowserStatus(t *testing.T) {
	got := parseBrowserStatus(`{"browsers":[{"instance_id":"afc17aef","label":""}]}`)
	if want := (BrowserStatus{State: "connected", Connected: []string{"afc17aef"}}); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	if got := parseBrowserStatus(`{"browsers":[]}`); got.State != "waiting" {
		t.Fatalf("got %+v", got)
	}
	if got := parseBrowserStatus("error: daemon not running\n"); got.State != "error" || got.Error != "bsk status: error: daemon not running" {
		t.Fatalf("got %+v", got)
	}
}
