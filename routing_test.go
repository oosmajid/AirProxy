package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestParseList(t *testing.T) {
	got := parseList("  a.com \n\n# a comment\nb.ir\n\t\ndomain:x.ir\n")
	want := []string{"a.com", "b.ir", "domain:x.ir"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %#v want %#v", got, want)
	}
}

func TestBuildRoutingDisabledIsNil(t *testing.T) {
	if buildRouting(bypassRules{Enabled: false, Iran: true}) != nil {
		t.Fatal("disabled bypass must produce nil routing")
	}
}

func TestBuildRoutingEnabledButEmptyIsNil(t *testing.T) {
	if buildRouting(bypassRules{Enabled: true}) != nil {
		t.Fatal("enabled bypass with no active rule must produce nil routing")
	}
}

func TestBuildRoutingFullPresets(t *testing.T) {
	r := buildRouting(bypassRules{Enabled: true, Iran: true, Private: true, BlockQUIC: true})
	got, _ := json.Marshal(r)
	want := `{"domainStrategy":"AsIs","rules":[` +
		`{"network":"udp","outboundTag":"block","port":"443","type":"field"},` +
		`{"domain":["geosite:category-ir","regexp:\\.ir$"],"outboundTag":"direct","type":"field"},` +
		`{"ip":["geoip:ir"],"outboundTag":"direct","type":"field"},` +
		`{"ip":["geoip:private"],"outboundTag":"direct","type":"field"}]}`
	if string(got) != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}

func TestBuildRoutingCustomLists(t *testing.T) {
	r := buildRouting(bypassRules{Enabled: true, Domains: []string{"a.com"}, IPs: []string{"1.2.3.0/24"}})
	got, _ := json.Marshal(r)
	want := `{"domainStrategy":"AsIs","rules":[` +
		`{"domain":["a.com"],"outboundTag":"direct","type":"field"},` +
		`{"ip":["1.2.3.0/24"],"outboundTag":"direct","type":"field"}]}`
	if string(got) != want {
		t.Fatalf("\n got: %s\nwant: %s", got, want)
	}
}
