package api

import (
	"net/http"
	"strings"
	"testing"
)

func TestDuplicatesAPI(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	text := "电子发票 普通发票 发票代码 011002300111 发票号码 24871634 开票日期 2024年03月05日 校验码 12345 67890 11223 34455 " +
		"购买方 名称 王小明 销售方 名称 北京京东世纪贸易有限公司 纳税人识别号 911101087921245832 " +
		"货物或应税劳务名称 计算机外部设备 罗技 MX Master 3S 无线鼠标 台 1 646.02 646.02 税率 13% 税额 83.98 合计 646.02 83.98 " +
		"价税合计 大写 柒佰叁拾圆整 小写 730.00 收款人 张三 复核 李四 开票人 王五"
	a := alice.waitJob(alice.upload("发票.txt", text, nil).body["jobId"].(float64))["documentId"].(float64)
	b := alice.waitJob(alice.upload("发票扫描.txt", strings.ReplaceAll(text, " ", "："), nil).body["jobId"].(float64))["documentId"].(float64)
	note := alice.waitJob(alice.upload("便条.txt", "买牛奶", nil).body["jobId"].(float64))["documentId"].(float64)

	list := alice.mustDo(http.MethodGet, "/api/v1/duplicates", nil, http.StatusOK).body
	if len(list["pairs"].([]any)) != 0 || list["lastScanAt"] != nil {
		t.Fatalf("before scanning: %v", list)
	}
	scan := alice.mustDo(http.MethodPost, "/api/v1/duplicates/scan", nil, http.StatusAccepted)
	if task := alice.waitTask(scan.body["jobId"].(float64)); task["status"] != "succeeded" || task["summary"] != "2 份文档，1 对疑似重复" {
		t.Fatalf("task: %v", task)
	}
	list = alice.mustDo(http.MethodGet, "/api/v1/duplicates", nil, http.StatusOK).body
	pairs := list["pairs"].([]any)
	if len(pairs) != 1 || list["lastScanAt"] == nil {
		t.Fatalf("after scanning: %v", list)
	}
	p := pairs[0].(map[string]any)
	if p["a"].(map[string]any)["id"] != a || p["b"].(map[string]any)["id"] != b || !strings.HasPrefix(p["aSnippet"].(string), "电子发票") {
		t.Fatalf("pair: %v", p)
	}
	// the details page asks for the pairs of one document
	if l := alice.mustDo(http.MethodGet, "/api/v1/duplicates?document="+itoa(b), nil, http.StatusOK).body; len(l["pairs"].([]any)) != 1 {
		t.Fatalf("of b: %v", l)
	}
	if l := alice.mustDo(http.MethodGet, "/api/v1/duplicates?document="+itoa(note), nil, http.StatusOK).body; len(l["pairs"].([]any)) != 0 {
		t.Fatalf("of the note: %v", l)
	}
	id := itoa(p["id"].(float64))
	bob := e.user("bob")
	bob.mustDo(http.MethodPost, "/api/v1/duplicates/"+id+"/keep", map[string]any{"documentId": a}, http.StatusNotFound)
	alice.mustDo(http.MethodPost, "/api/v1/duplicates/"+id+"/keep", map[string]any{"documentId": a}, http.StatusNoContent)
	alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(b), nil, http.StatusNotFound)
	if l := alice.mustDo(http.MethodGet, "/api/v1/duplicates", nil, http.StatusOK).body; len(l["pairs"].([]any)) != 0 {
		t.Fatalf("after keeping: %v", l)
	}
}
