package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func TestParseCommand(t *testing.T) {
	var out bytes.Buffer
	if err := run([]string{"parse", "-config", "../../configs", "ngày tàn"}, &out); err != nil {
		t.Fatal(err)
	}
	var res struct {
		Syllables []struct{ Nucleus, Spelled string }
		Keys      map[string]map[string]string
	}
	if err := json.Unmarshal(out.Bytes(), &res); err != nil {
		t.Fatal(err)
	}
	if len(res.Syllables) != 2 || res.Syllables[0].Nucleus != "AW" || res.Syllables[1].Spelled != "tàn" {
		t.Errorf("syllables = %+v", res.Syllables)
	}
	if res.Keys["nam"]["L0"] != "AW.j.2 A.ng.2" {
		t.Errorf("nam L0 = %q", res.Keys["nam"]["L0"])
	}
}

func TestRhymesCommand(t *testing.T) {
	var out bytes.Buffer
	args := []string{"rhymes", "-config", "../../configs", "-lexicon", "../../internal/domain/rhyme/testdata/lexicon.tsv", "ngày", "tàn"}
	if err := run(args, &out); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(out.String(), "[may mắn]") {
		t.Errorf("output:\n%s", out.String())
	}
	for _, bad := range [][]string{nil, {"nope"}, {"parse", "-config", "../../configs", "wifi"}, {"rhymes", "-level", "L9", "tàn"}} {
		if err := run(bad, &out); err == nil {
			t.Errorf("%q: want error", bad)
		}
	}
}
