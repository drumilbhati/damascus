package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestParseRequestSpecs(t *testing.T) {
	specs, err := ParseRequestSpecs(`[
	  {"name":"list","url":"http://svc:8080/items?q={{seq}}"},
	  {"name":"create","method":"post","url":"http://svc:8080/items","body":{"id": "{{uuid}}", "qty": 1},"weight":2,"allow_mutation":true},
	  {"method":"POST","url":"http://svc:8080/raw","body":"plain=text"}
	]`)
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if len(specs) != 3 {
		t.Fatalf("got %d specs", len(specs))
	}
	if specs[0].Method != "GET" || specs[0].Body != "" {
		t.Errorf("default method should be GET: %+v", specs[0])
	}
	if specs[1].Method != "POST" || specs[1].Body != `{"id":"{{uuid}}","qty":1}` || !specs[1].AllowMutation || specs[1].Weight != 2 {
		t.Errorf("object body not compacted: %+v", specs[1])
	}
	if specs[2].Name != "post-3" || specs[2].Body != "plain=text" {
		t.Errorf("string body or generated name wrong: %+v", specs[2])
	}
}

func TestParseRequestSpecsRejectsInvalid(t *testing.T) {
	for name, raw := range map[string]string{
		"not array":  `{"a":1}`,
		"bad method": `[{"method":"DELETE","url":"http://x/"}]`,
		"bad url":    `[{"url":"ftp://x/"}]`,
		"duplicate":  `[{"name":"a","url":"http://x/"},{"name":"a","url":"http://y/"}]`,
		"get body":   `[{"url":"http://x/","body":{"a":1}}]`,
		"unknown":    `[{"url":"http://x/","payload":"x"}]`,
	} {
		if _, err := ParseRequestSpecs(raw); err == nil {
			t.Errorf("%s: expected error", name)
		}
	}
}

func TestLoadRequestSpecsPrefersFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "endpoints.json")
	if err := os.WriteFile(path, []byte(`[{"name":"from-file","url":"http://x/"}]`), 0o600); err != nil {
		t.Fatal(err)
	}
	specs, err := LoadRequestSpecs(`[{"name":"inline","url":"http://y/"}]`, path)
	if err != nil || len(specs) != 1 || specs[0].Name != "from-file" {
		t.Fatalf("specs=%+v err=%v", specs, err)
	}
	if specs, err := LoadRequestSpecs("", ""); err != nil || specs != nil {
		t.Fatalf("empty config should yield no specs: %+v %v", specs, err)
	}
}
