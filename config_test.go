package main

import "testing"

func TestBuildConfigOmitsRoutingWhenNil(t *testing.T) {
	cfg := buildConfig("127.0.0.1", 10808, 0, map[string]interface{}{"tag": "proxy"}, nil)
	if _, ok := cfg["routing"]; ok {
		t.Fatal("nil routing must not add a routing section")
	}
}

func TestBuildConfigIncludesRoutingWhenSet(t *testing.T) {
	routing := map[string]interface{}{"domainStrategy": "AsIs"}
	cfg := buildConfig("127.0.0.1", 10808, 0, map[string]interface{}{"tag": "proxy"}, routing)
	got, ok := cfg["routing"].(map[string]interface{})
	if !ok || got["domainStrategy"] != "AsIs" {
		t.Fatalf("routing not wired through, got %v", cfg["routing"])
	}
}
