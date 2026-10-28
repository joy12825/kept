package api

import (
	"encoding/json"
	"net/http"
	"strings"
	"testing"
	"time"
)

// pair generates a pairing code as c (in its current workspace) and pairs a
// phone with it, returning the phone as a bearer-token client.
func pair(t *testing.T, c *client, name string) *client {
	t.Helper()
	pc := c.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body
	r := c.env.anon().mustDo(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": pc["code"], "deviceName": name}, http.StatusOK)
	phone := c.env.anon()
	phone.bearer = r.body["token"].(string)
	return phone
}

// pairFull is pair for an App 2.0 phone (full access).
func pairFull(t *testing.T, c *client, name string) *client {
	t.Helper()
	pc := c.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body
	r := c.env.anon().mustDo(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": pc["code"], "deviceName": name, "access": "full"}, http.StatusOK)
	if r.body["access"] != "full" {
		t.Fatalf("paired: %v", r.body)
	}
	phone := c.env.anon()
	phone.bearer = r.body["token"].(string)
	return phone
}

func TestPairingCodeAndQRCode(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, _ := home(t, e)
	r := alice.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body
	code := r["code"].(string)
	if len(code) != 8 || strings.Trim(code, "0123456789") != "" || r["workspaceName"] != "王家" {
		t.Fatalf("pairing code: %v", r)
	}
	var qr struct {
		V    int      `json:"v"`
		URLs []string `json:"urls"`
		Code string   `json:"code"`
		WS   string   `json:"ws"`
	}
	if err := json.Unmarshal([]byte(r["qr"].(string)), &qr); err != nil || qr.V != 1 || qr.Code != code || qr.WS != "王家" || len(qr.URLs) != 1 || qr.URLs[0] != "http://example.com" {
		t.Errorf("qr = %+v (%v)", qr, err)
	}
	if exp, _ := time.Parse(time.RFC3339, r["expiresAt"].(string)); exp.Sub(time.Now()) > 10*time.Minute || exp.Sub(time.Now()) < 9*time.Minute {
		t.Errorf("expires at %v", r["expiresAt"])
	}
	// A viewer gets a code, but only a full-access phone (App 2.0) can use
	// it: an upload-only phone could do nothing.
	vc := carol.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body["code"]
	if r := e.anon().do(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": vc}); r.code != http.StatusForbidden {
		t.Errorf("viewer's upload-only phone: %d", r.code)
	}

	// The code works once; a regenerated one replaces the previous.
	anon := e.anon()
	old := alice.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body["code"].(string)
	fresh := alice.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body["code"].(string)
	if r := anon.do(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": old}); r.code != http.StatusBadRequest {
		t.Errorf("superseded code: %d %s", r.code, r.raw)
	}
	spaced := fresh[:4] + " " + fresh[4:]
	anon.mustDo(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": spaced, "deviceName": "小王的 iPhone"}, http.StatusOK)
	if r := anon.do(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": fresh}); r.code != http.StatusBadRequest {
		t.Errorf("reused code: %d", r.code)
	}
}

func TestPairingCodeExpires(t *testing.T) {
	e, clk := newEnvWithClock(t, "auto")
	alice := e.user("alice")
	code := alice.mustDo(http.MethodPost, "/api/v1/devices/pairing-codes", nil, http.StatusCreated).body["code"]
	clk.Add(11 * time.Minute)
	if r := e.anon().do(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": code}); r.code != http.StatusBadRequest {
		t.Errorf("expired code: %d", r.code)
	}
}

func TestWrongPairingCodesAreRateLimited(t *testing.T) {
	e := newEnv(t, "auto", nil)
	anon := e.anon()
	for i := 0; i < 10; i++ {
		anon.mustDo(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": "00000000"}, http.StatusBadRequest)
	}
	if r := anon.do(http.MethodPost, "/api/v1/devices/pair", map[string]any{"code": "00000000"}); r.code != http.StatusTooManyRequests {
		t.Errorf("11th wrong code: %d", r.code)
	}
}

func TestDeviceTokenOnlyUploads(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, _, homeID := home(t, e)
	tagID := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "水电"}, http.StatusCreated).body["id"].(float64)
	drawerID := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "房子"}, http.StatusCreated).body["drawer"].(map[string]any)["id"].(float64)
	phone := pair(t, alice, "小王的 iPhone")

	boot := phone.mustDo(http.MethodGet, "/api/v1/mobile/bootstrap", nil, http.StatusOK).body
	if boot["workspace"].(map[string]any)["id"] != homeID || len(boot["drawers"].([]any)) != 1 || len(boot["tags"].([]any)) != 1 || boot["maxUploadSize"].(float64) <= 0 {
		t.Errorf("bootstrap: %v", boot)
	}

	// An X-Workspace-ID header cannot move the phone to another workspace.
	phone.headers["X-Workspace-ID"] = "1"
	fields := map[string]string{"clientUploadId": "u-1", "title": "三月电费", "tagIds": itoa(tagID), "drawerId": itoa(drawerID)}
	r := phone.upload("scan.txt", "国家电网 电费 2024-03-05", fields)
	if r.code != http.StatusAccepted {
		t.Fatalf("phone upload: %d %s", r.code, r.raw)
	}
	jobID := r.body["jobId"]
	// The same client ID again returns the same job and stores nothing new.
	if again := phone.upload("scan.txt", "国家电网 电费 2024-03-05", fields); again.code != http.StatusAccepted || again.body["jobId"] != jobID {
		t.Errorf("repeat upload: %d %v, want job %v", again.code, again.body, jobID)
	}
	var st []any
	deadline := time.Now().Add(20 * time.Second)
	for {
		st = phone.mustDo(http.MethodPost, "/api/v1/uploads/status", map[string]any{"clientUploadIds": []string{"u-1", "nope"}}, http.StatusOK).list
		if st[0].(map[string]any)["status"] == "succeeded" || time.Now().After(deadline) {
			break
		}
		time.Sleep(30 * time.Millisecond)
	}
	got := st[0].(map[string]any)
	if got["status"] != "succeeded" || got["documentId"] == nil || st[1].(map[string]any)["status"] != "unknown" {
		t.Fatalf("status: %v", st)
	}
	doc := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(got["documentId"].(float64)), nil, http.StatusOK).body
	if doc["source"] != "mobile" || doc["title"] != "三月电费" || doc["drawerId"] != drawerID || doc["drawerManual"] != true ||
		len(doc["tagIds"].([]any)) != 1 || doc["tagIds"].([]any)[0] != tagID {
		t.Errorf("document from the phone: %v", doc)
	}
	if n := alice.mustDo(http.MethodGet, "/api/v1/documents", nil, http.StatusOK).body["total"]; n != float64(1) {
		t.Errorf("documents in 王家: %v", n)
	}
	// The activity log names the phone.
	audit := alice.mustDo(http.MethodGet, "/api/v1/audit?action=device.", nil, http.StatusOK).body["items"].([]any)
	if len(audit) != 1 || audit[0].(map[string]any)["actorVia"] != "device" || !strings.Contains(audit[0].(map[string]any)["summary"].(string), "小王的 iPhone") {
		t.Errorf("pairing audit: %v", audit)
	}

	// Everything else is closed to the phone.
	for _, probe := range []struct{ method, path string }{
		{http.MethodGet, "/api/v1/documents"},
		{http.MethodGet, "/api/v1/documents/" + itoa(got["documentId"].(float64))},
		{http.MethodGet, "/api/v1/me/tokens"},
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodPost, "/api/v1/devices/pairing-codes"},
		{http.MethodDelete, "/api/v1/documents/" + itoa(got["documentId"].(float64))},
		{http.MethodGet, "/api/v1/jobs"},
	} {
		if r := phone.do(probe.method, probe.path, nil); r.code != http.StatusForbidden || code(r) != "device_forbidden" {
			t.Errorf("phone %s %s: %d %s", probe.method, probe.path, r.code, r.raw)
		}
	}
}

func TestPersonalUploadsRecordTheirSource(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice := e.user("alice")
	r := alice.upload("a.txt", "网页上传", nil)
	id := alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	if s := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id), nil, http.StatusOK).body["source"]; s != "web" {
		t.Errorf("web upload source = %v", s)
	}
	tok := alice.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "脚本"}, http.StatusCreated).body["token"].(string)
	script := e.anon()
	script.bearer = tok
	r = script.upload("b.txt", "脚本上传", nil)
	id = alice.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
	if s := alice.mustDo(http.MethodGet, "/api/v1/documents/"+itoa(id), nil, http.StatusOK).body["source"]; s != "api" {
		t.Errorf("API upload source = %v", s)
	}
}

func TestDevicesListAndDisconnect(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob, _, homeID := home(t, e)
	phone := pair(t, alice, "小王的 iPhone")
	pad := pair(t, alice, "小王的 iPad")
	bobPhone := pair(t, bob, "小李的手机")

	list := alice.mustDo(http.MethodGet, "/api/v1/devices", nil, http.StatusOK).list
	if len(list) != 2 || list[0].(map[string]any)["name"] != "小王的 iPad" || list[0].(map[string]any)["workspaceId"] != homeID || list[0].(map[string]any)["workspaceName"] != "王家" {
		t.Errorf("alice's devices: %v", list)
	}
	// Device tokens are not personal API tokens.
	if toks := alice.mustDo(http.MethodGet, "/api/v1/me/tokens", nil, http.StatusOK).list; len(toks) != 0 {
		t.Errorf("tokens list shows devices: %v", toks)
	}
	// Bob cannot disconnect alice's phone.
	padID := list[0].(map[string]any)["id"].(float64)
	if r := bob.do(http.MethodDelete, "/api/v1/devices/"+itoa(padID), nil); r.code != http.StatusNotFound {
		t.Errorf("bob disconnects alice's pad: %d", r.code)
	}
	alice.mustDo(http.MethodDelete, "/api/v1/devices/"+itoa(padID), nil, http.StatusNoContent)
	if r := pad.do(http.MethodGet, "/api/v1/mobile/bootstrap", nil); r.code != http.StatusUnauthorized {
		t.Errorf("disconnected pad: %d", r.code)
	}
	// A phone can forget itself; a browser cannot use that.
	if r := alice.do(http.MethodDelete, "/api/v1/devices/current", nil); r.code != http.StatusBadRequest {
		t.Errorf("browser forgets current device: %d", r.code)
	}
	phone.mustDo(http.MethodDelete, "/api/v1/devices/current", nil, http.StatusNoContent)
	if r := phone.do(http.MethodGet, "/api/v1/mobile/bootstrap", nil); r.code != http.StatusUnauthorized {
		t.Errorf("forgotten phone: %d", r.code)
	}
	// Removing bob from the workspace cuts his phone off too.
	bobPhone.mustDo(http.MethodGet, "/api/v1/mobile/bootstrap", nil, http.StatusOK)
	alice.mustDo(http.MethodDelete, "/api/v1/workspaces/"+itoa(homeID)+"/members/"+itoa(userID(bob)), nil, http.StatusNoContent)
	if r := bobPhone.do(http.MethodGet, "/api/v1/mobile/bootstrap", nil); r.code != http.StatusUnauthorized {
		t.Errorf("phone of a removed member: %d", r.code)
	}
}

// A full-access phone (App 2.0) works with its owner's role in the paired
// workspace, but not with account security or administration.
func TestFullAccessPhone(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, _, carol, homeID := home(t, e)
	alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "房子"}, http.StatusCreated)
	phone := pairFull(t, alice, "小王的 iPhone")

	boot := phone.mustDo(http.MethodGet, "/api/v1/mobile/bootstrap", nil, http.StatusOK).body
	if boot["access"] != "full" || boot["role"] != "owner" || boot["workspace"].(map[string]any)["id"] != homeID {
		t.Errorf("bootstrap: %v", boot)
	}
	phone.mustDo(http.MethodGet, "/api/v1/drawers", nil, http.StatusOK)
	phone.mustDo(http.MethodGet, "/api/v1/documents", nil, http.StatusOK)
	phone.mustDo(http.MethodGet, "/api/v1/alerts", nil, http.StatusOK)
	for _, c := range []struct{ method, path string }{
		{http.MethodPost, "/api/v1/me/password"},
		{http.MethodPost, "/api/v1/me/tokens"},
		{http.MethodPost, "/api/v1/devices/pairing-codes"},
		{http.MethodGet, "/api/v1/devices"},
		{http.MethodGet, "/api/v1/admin/system"},
	} {
		if r := phone.do(c.method, c.path, map[string]any{}); r.code != http.StatusForbidden || code(r) != "device_forbidden" {
			t.Errorf("%s %s from a phone: %d %s", c.method, c.path, r.code, r.raw)
		}
	}
	devices := alice.mustDo(http.MethodGet, "/api/v1/devices", nil, http.StatusOK).list
	if devices[0].(map[string]any)["access"] != "full" {
		t.Errorf("devices: %v", devices)
	}

	// A viewer's phone browses but cannot upload.
	viewer := pairFull(t, carol, "小李的 iPhone")
	viewer.mustDo(http.MethodGet, "/api/v1/documents", nil, http.StatusOK)
	if r := viewer.upload("scan.txt", "电费", map[string]string{"clientUploadId": "v-1"}); r.code != http.StatusForbidden {
		t.Errorf("viewer's phone upload: %d %s", r.code, r.raw)
	}
}

// Every operation is either refused to full-access phones or reviewed as
// fine for them; a new operation must be put in one of the two lists.
func TestFullAccessPhoneOperationsAreReviewed(t *testing.T) {
	s, err := NewServer(Deps{})
	if err != nil {
		t.Fatal(err)
	}
	allowed := map[string]bool{
		// v1.8.0: family members' privacy, the same on the phone as on the web
		"setpersonaccess": true, "removepersonaccess": true, "reviewpersonaccess": true, "revokepersonuploader": true, "setpersonemergency": true,
		"removepersonemergency": true, "denypersonemergency": true, "requestpersonemergency": true, "cancelpersonemergency": true, "getmyprivacy": true,
		"setuppersonkey": true, "newpersonrecoverycode": true, "checkpersonrecoverycode": true, "setpersoncloudai": true,
		"enablehealthtemplate": true, "gethealthoverview": true, "gethealthseries": true, "listmetrics": true, "createmetric": true,
		"updatemetric": true, "getdocumentlab": true, "savedocumentlab": true, "extractdocumentlab": true, "getvisit": true,
		// v1.8.3: health reminders, the summary for a doctor, visits
		"listhealthalerts": true, "dismisshealthalert": true, "restorehealthalert": true, "gethealthsettings": true, "sethealthsettings": true,
		"gethealthreport": true, "sharehealthreport": true, "setvisitdocuments": true,
		// v1.9.0: scenes, entries made by hand, medicines
		"listscenes": true, "createscene": true, "updatescene": true, "deletescene": true, "enablescenetemplate": true,
		"createobservation": true, "deleteobservation": true, "listmedications": true, "createmedication": true, "updatemedication": true, "deletemedication": true,
		"getme": true, "listworkflows": true, "createworkflow": true, "reorderworkflows": true, "updateworkflow": true, "deleteworkflow": true,
		"listworkflowruns": true, "listmailaccounts": true, "createmailaccount": true, "updatemailaccount": true, "deletemailaccount": true,
		"testmailaccount": true, "fetchmailaccount": true, "listmailmessages": true, "refetchmailmessage": true, "forgetcurrentdevice": true, "getmobilebootstrap": true, "getuploadstatus": true, "verifysession": true, "checksamename": true,
		"getnotificationprefs": true, "setnotificationprefs": true, "listworkspaces": true, "listaudit": true, "listcorrespondents": true,
		"createcorrespondent": true, "updatecorrespondent": true, "deletecorrespondent": true, "listdocumenttypes": true, "createdocumenttype": true,
		"updatedocumenttype": true, "deletedocumenttype": true, "listtags": true, "createtag": true, "updatetag": true, "deletetag": true, "bulkdeletedirectoryitems": true, "mergedirectoryitems": true, "adddocumentversionfromdocument": true,
		"listcustomfields": true, "createcustomfield": true, "updatecustomfield": true, "deletecustomfield": true, "listdocuments": true, "uploaddocument": true, "getdocumentfacets": true, "bulkdocuments": true, "downloaddocuments": true, "getdocument": true, "updatedocument": true, "nextdocumentasn": true, "deletedocument": true, "restoredocument": true, "getdocumenthistory": true, "listnotes": true, "createnote": true, "deletenote": true, "assistbook": true,
		"getbooksummary": true, "deletebooksummary": true, "listreviewcards": true, "generatereviewcards": true, "reviewcard": true, "deletereviewcard": true, "getdocumentbook": true, "setdocumentbook": true, "deletedocumentbook": true, "setbookcover": true, "deletebookcover": true, "fetchbookcover": true, "getdocumentcover": true, "getreadingprogress": true, "savereadingprogress": true, "setreadingstate": true, "listdocumenthighlights": true,
		"createhighlight": true, "searchhighlights": true, "updatehighlight": true, "deletehighlight": true, "listbooklists": true, "createbooklist": true,
		"getbooklist": true, "updatebooklist": true, "deletebooklist": true, "addbooklistitem": true, "updatebooklistitem": true, "removebooklistitem": true, "reorderbooklist": true, "getdocumentbooklists": true, "getsimilardocuments": true, "getdocumentfileinfo": true, "askdocument": true, "listqaconversations": true, "createqaconversation": true,
		"getqaconversation": true, "deleteqaconversation": true, "askquestion": true, "organizedocuments": true, "listorganizesuggestions": true,
		"refreshorganizesuggestions": true, "lookupbook": true, "enablelibrary": true, "disablelibrary": true, "listtrash": true, "emptytrash": true,
		"purgedocument": true, "listsavedviews": true, "reordersavedviews": true, "createsavedview": true, "updatesavedview": true, "deletesavedview": true, "getdocumentoriginal": true, "watermarkdocument": true, "getdocumentsignatures": true, "getdocumentseal": true, "getdocumentpreview": true, "getdocumentthumbnail": true, "setdocumentpages": true, "splitdocument": true, "mergedocuments": true,
		"emaildocuments": true, "reprocessdocument": true, "getclassifier": true, "trainclassifier": true, "testmatchrule": true, "listdrawers": true,
		"createdrawer": true, "reorderdrawers": true, "previewdrawers": true, "createdrawersfromtemplates": true, "getdrawer": true, "updatedrawer": true,
		"deletedrawer": true, "listfactsuggestions": true, "createdrawerfact": true, "reorderdrawerfacts": true, "updatedrawerfact": true,
		"deletedrawerfact": true, "listdrawershares": true, "sharedrawer": true, "unsharedrawer": true, "listcollections": true, "createcollection": true,
		"updatecollection": true, "deletecollection": true, "sharecollection": true, "unsharecollection": true, "listsharedcollections": true,
		"getsharedcollection": true, "listsharedcollectiondocuments": true, "getsharedcollectiondocument": true, "getsharedcollectiondocumentfile": true,
		"listshareddrawers": true, "getshareddrawer": true, "listshareddocuments": true, "getshareddocument": true, "getshareddocumentfile": true,
		"listsharelinks": true, "createsharelink": true, "listactiveshares": true, "createsharebundle": true, "revokesharebundle": true, "listdocumentversions": true, "adddocumentversion": true, "removedocumentpassword": true,
		"deletedocumentversion": true, "setcurrentdocumentversion": true, "getdocumentversionfile": true, "getdocumentversiontext": true, "getdocumentversionpreview": true, "getdocumentsheets": true, "getdocumentword": true, "getdocumentwordimage": true, "listzipentries": true, "importzipentries": true, "listduplicates": true, "scanduplicates": true,
		"keepduplicate": true, "dismissduplicate": true, "revokesharelink": true, "listdrawertemplates": true, "listalerts": true, "dismissalert": true,
		"restorealert": true, "listyearbookyears": true, "getmilestonerules": true, "setmilestonerules": true, "getyearbook": true,
		"writeyearbooknarrative": true, "listtasks": true, "getagentsettings": true, "setagentsettings": true, "listagentruns": true,
		"listdocumentproposals": true, "acceptproposal": true, "rejectproposal": true, "revertproposal": true, "listjobs": true, "retryjob": true, "getevents": true,
		"getstorage": true,
		// v1.6.2: errand checklists, and upload links (like share links, a full-access phone may make and revoke them)
		"attachchecklistdocument": true, "detachchecklistdocument": true, "uploadtochecklistitem": true, "listchecklists": true, "getchecklist": true, "checkchecklist": true, "createchecklist": true, "updatechecklist": true, "deletechecklist": true,
		"listuploadlinks": true, "createuploadlink": true, "revokeuploadlink": true,
		// v1.6.3: family members and where originals are kept, like the other directory items
		"listpersons": true, "createperson": true, "updateperson": true, "deleteperson": true,
		"listlocations": true, "createlocation": true, "updatelocation": true, "deletelocation": true, "assignlocationbyasn": true,
		// v1.12: the edition, so the app shows what it includes
		"getedition": true,
	}
	for op := range s.minRole {
		if s.public[op] || strings.HasPrefix(op, "admin") || deviceDenied[op] {
			continue
		}
		if !allowed[op] {
			t.Errorf("operation %q is open to full-access phones but not reviewed: add it to deviceDenied or to this list", op)
		}
	}
	for op := range allowed {
		if !deviceAllowed("full", op) {
			t.Errorf("reviewed operation %q is refused to full-access phones", op)
		}
	}
}

// An empty workspace's bootstrap has empty lists, not null: the phone
// cannot read null and stayed on “connecting” (found checking App 3.3).
func TestBootstrapOfAnEmptyWorkspace(t *testing.T) {
	e := newEnv(t, "auto", nil)
	carol := e.user("carol")
	phone := pair(t, carol, "空空的手机")
	boot := phone.mustDo(http.MethodGet, "/api/v1/mobile/bootstrap", nil, http.StatusOK)
	for _, key := range []string{`"drawers":[]`, `"tags":[]`} {
		if !strings.Contains(boot.raw, key) {
			t.Errorf("bootstrap lacks %s: %s", key, boot.raw)
		}
	}
	if strings.Contains(boot.raw, "null") {
		t.Errorf("bootstrap has null: %s", boot.raw)
	}
}
