package wiktionary

import (
	"bytes"
	"compress/gzip"
	"context"
	"strings"
	"testing"

	"playword/internal/domain/entry"
)

const sample = `{"word":"may mắn","lang_code":"vi","pos_title":"Tính từ","senses":[{"glosses":["Gặp dịp tốt."],"examples":[{"text":"Thật may mắn."}]}],"sounds":[{"ipa":"maj˧˧ maŋ˧˥","tags":["Saigon"]}]}
{"word":"cỏ","lang_code":"vi","pos_title":"Danh từ","senses":[{"glosses":["Cây thân mềm."]},{"glosses":["Cần sa."],"tags":["slang"]}]}
{"word":"bùn","lang_code":"vi","pos_title":"Tính từ","senses":[{"glosses":["Dạng thay thế của buồn"],"tags":["Internet","alt-of"]}]}
{"word":"money","lang_code":"en","senses":[{"glosses":["tiền"]}]}
{"word":"rỗng","lang_code":"vi","senses":[{"tags":["no-gloss"]}]}
`

func TestParse(t *testing.T) {
	var gz bytes.Buffer
	w := gzip.NewWriter(&gz)
	_, _ = w.Write([]byte(sample))
	_ = w.Close()
	for name, data := range map[string][]byte{"plain": []byte(sample), "gzip": gz.Bytes()} {
		var got []entry.Entry
		err := New("", nil).Parse(context.Background(), bytes.NewReader(data), func(e entry.Entry) error {
			got = append(got, e)
			return nil
		})
		if err != nil {
			t.Fatal(err)
		}
		var lines []string
		for _, e := range got {
			flag := ""
			if e.IsSlang {
				flag = "/slang"
			}
			lines = append(lines, e.Headword+flag+":"+e.Senses[0].Gloss)
		}
		want := "may mắn:Gặp dịp tốt.|cỏ:Cây thân mềm.|cỏ/slang:Cần sa.|bùn/slang:Dạng thay thế của buồn"
		if strings.Join(lines, "|") != want {
			t.Errorf("%s: %v", name, lines)
		}
		if got[0].POS != "tính từ" || got[0].Senses[0].Examples[0] != "Thật may mắn." {
			t.Errorf("%s: %+v", name, got[0])
		}
	}
	if err := New("", nil).Parse(context.Background(), strings.NewReader("{bad"), func(entry.Entry) error { return nil }); err == nil {
		t.Error("bad JSON must fail")
	}
	s := New("", nil)
	if s.Name() != "wiktionary-vi" || s.Kind() != "dictionary" || s.Lang() != entry.VI || s.URL() != DefaultURL || s.License() == "" {
		t.Error("metadata")
	}
}
