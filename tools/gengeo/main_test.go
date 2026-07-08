package main

import (
	"testing"

	"github.com/xtls/xray-core/app/router"
	"google.golang.org/protobuf/proto"
)

func TestTrimGeoIPKeepsOnlyWanted(t *testing.T) {
	in := &router.GeoIPList{Entry: []*router.GeoIP{
		{CountryCode: "IR", Cidr: []*router.CIDR{{Ip: []byte{1, 2, 3, 0}, Prefix: 24}}},
		{CountryCode: "US", Cidr: []*router.CIDR{{Ip: []byte{5, 6, 7, 0}, Prefix: 24}}},
		{CountryCode: "PRIVATE", Cidr: []*router.CIDR{{Ip: []byte{10, 0, 0, 0}, Prefix: 8}}},
	}}
	data, err := proto.Marshal(in)
	if err != nil {
		t.Fatal(err)
	}
	out, err := trimGeoIP(data, []string{"IR", "PRIVATE"})
	if err != nil {
		t.Fatal(err)
	}
	var got router.GeoIPList
	if err := proto.Unmarshal(out, &got); err != nil {
		t.Fatal(err)
	}
	if len(got.Entry) != 2 {
		t.Fatalf("want 2 kept entries, got %d", len(got.Entry))
	}
	for _, e := range got.Entry {
		if e.CountryCode == "US" {
			t.Fatal("US should have been trimmed")
		}
	}
}
