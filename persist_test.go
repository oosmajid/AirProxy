package main

import (
	"encoding/json"
	"reflect"
	"testing"
)

func TestDecodeStoreSeedsBypassOnEmpty(t *testing.T) {
	s := decodeStore("")
	if !reflect.DeepEqual(s.Bypass, defaultBypass()) {
		t.Fatalf("empty store should seed default bypass, got %+v", s.Bypass)
	}
	if s.Listen != "127.0.0.1" || s.Socks != 10808 {
		t.Fatalf("empty store defaults wrong: %+v", s)
	}
}

func TestDecodeStoreSeedsBypassOnLegacyData(t *testing.T) {
	// دیتای نسخهٔ قبلی که اصلاً فیلد bypass ندارد.
	legacy := `{"sources":["s1"],"listen":"127.0.0.1","socks":10808,"http":10809,"rotate":true}`
	s := decodeStore(legacy)
	if !reflect.DeepEqual(s.Bypass, defaultBypass()) {
		t.Fatalf("legacy data should seed default bypass, got %+v", s.Bypass)
	}
	if len(s.Sources) != 1 || s.Sources[0] != "s1" {
		t.Fatalf("legacy sources must be preserved, got %+v", s.Sources)
	}
}

func TestDecodeStoreRespectsSavedBypass(t *testing.T) {
	// کاربری که همه‌چیز را خاموش کرده و ذخیره کرده است.
	saved := store{Listen: "127.0.0.1", Socks: 10808, Bypass: bypassRules{Enabled: false}}
	b, _ := json.Marshal(saved)
	got := decodeStore(string(b))
	if got.Bypass.Enabled || got.Bypass.Iran {
		t.Fatalf("saved all-off bypass must be respected, got %+v", got.Bypass)
	}
}

func TestBypassJSONRoundTrip(t *testing.T) {
	in := bypassRules{Enabled: true, Iran: true, Private: false, BlockQUIC: true,
		Domains: []string{"a.ir"}, IPs: []string{"10.0.0.0/8"}}
	b, _ := json.Marshal(in)
	var out bypassRules
	if err := json.Unmarshal(b, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("round-trip mismatch: %+v vs %+v", in, out)
	}
}
