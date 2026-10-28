package api

import (
	"net/http"
	"testing"
)

// v1.4.4: the document's date from the words around it, the file's name
// when the text says nothing, and the other dates offered.
func TestDocumentDateChoice(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	get := func(name, text string) map[string]any {
		id := alice.waitJob(alice.upload(name, text, nil).body["jobId"].(float64))["documentId"].(float64)
		return alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id), nil, http.StatusOK).body
	}
	d := get("身份证.txt", "居民身份证\n出生 2015年4月12日\n签发日期 2022年5月10日\n有效期限 2022.05.10-2032.05.10")
	date := d["fieldSources"].(map[string]any)["date"].(map[string]any)
	if d["createdDate"] != "2022-05-10" || date["basis"] != "签发日期 2022年5月10日" {
		t.Errorf("id card: %v %v", d["createdDate"], date)
	}

	d = get("扫描_20240305.txt", "物业费收据 1260 元")
	date = d["fieldSources"].(map[string]any)["date"].(map[string]any)
	if d["createdDate"] != "2024-03-05" || date["source"] != "file" || date["basis"] != "文件名“扫描_20240305.txt”" {
		t.Errorf("by name: %v %v", d["createdDate"], date)
	}

	d = get("IMG_20240301.txt", "账单周期 2024/02/06 - 2024/03/05\n账单日 2024/03/05")
	date = d["fieldSources"].(map[string]any)["date"].(map[string]any)
	alts, _ := date["alternatives"].([]any)
	if d["createdDate"] != "2024-03-05" || len(alts) != 2 || alts[0].(map[string]any)["date"] != "2024-02-06" || alts[1].(map[string]any)["date"] != "2024-03-01" {
		t.Errorf("alternatives: %v %v", d["createdDate"], date)
	}
}
