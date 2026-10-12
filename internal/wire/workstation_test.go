package wire

import (
	"reflect"
	"testing"
)

func TestBrowserStatusesReadBothDaemons(t *testing.T) {
	stdout := `{"browsers":[{"instance_id":"afc17aef","label":""}]}
---
{"browsers":[{"instance_id":"113dacb8","label":"MacBook"}]}
`
	got := browserStatuses(stdout)
	want := []BrowserStatus{
		{Daemon: "workstation", State: "connected", Connected: []string{"afc17aef"}},
		{Daemon: "owner", State: "connected", Connected: []string{"113dacb8 (MacBook)"}},
	}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %+v", got)
	}
	got = browserStatuses("{\"browsers\":[]}\n---\noff\n")
	if got[0].State != "waiting" || got[1].State != "off" {
		t.Fatalf("got %+v", got)
	}
	got = browserStatuses("error: daemon not running\n---\n{\"browsers\":[]}\n")
	if got[0].State != "error" || got[0].Error != "bsk status: error: daemon not running" || got[1].State != "waiting" {
		t.Fatalf("got %+v", got)
	}
}
