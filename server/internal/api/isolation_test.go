package api

import (
	"net/http"
	"net/url"
	"strings"
	"testing"
)

// Isolation tests: account A must never see or change account B's data.
// Every new workspace- or user-scoped resource adds a case to
// TestCrossAccountAccessIsNotFound: create it as alice, then check that bob
// gets 404 (not 403, which would reveal that the ID exists).

func TestAccountsHaveSeparateWorkspaces(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob := e.user("alice"), e.user("bob")
	wa := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)
	wb := bob.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)
	if wa["id"] == wb["id"] {
		t.Fatalf("alice and bob share workspace %v", wa["id"])
	}
	if wa["kind"] != "personal" || wa["role"] != "owner" {
		t.Errorf("workspace: %v", wa)
	}
}

func TestCrossAccountAccessIsNotFound(t *testing.T) {
	e := newEnv(t, "auto", nil)
	alice, bob := e.user("alice"), e.user("bob")

	cases := []struct {
		name   string
		create func() (method, path string, body any) // run as alice; returns bob's probe
	}{
		{"revoke another user's API token", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/me/tokens", map[string]any{"name": "a"}, http.StatusCreated)
			id := r.body["apiToken"].(map[string]any)["id"].(float64)
			return http.MethodDelete, "/api/v1/me/tokens/" + itoa(id), nil
		}},
		{"rename another user's workspace", func() (string, string, any) {
			return http.MethodPatch, workspacePath(alice, "房子"), map[string]any{"name": "taken over"}
		}},
		{"delete another user's workspace", func() (string, string, any) {
			return http.MethodDelete, workspacePath(alice, "车"), map[string]any{"name": "车"}
		}},
		{"list the members of another user's workspace", func() (string, string, any) {
			return http.MethodGet, workspacePath(alice, "健康") + "/members", nil
		}},
		{"add a member to another user's workspace", func() (string, string, any) {
			return http.MethodPost, workspacePath(alice, "证件") + "/members", map[string]any{"login": "bob", "role": "owner"}
		}},
		{"change a role in another user's workspace", func() (string, string, any) {
			return http.MethodPatch, workspacePath(alice, "工作") + "/members/" + itoa(userID(alice)), map[string]any{"role": "viewer"}
		}},
		{"remove a member from another user's workspace", func() (string, string, any) {
			return http.MethodDelete, workspacePath(alice, "孩子") + "/members/" + itoa(userID(alice)), nil
		}},
		{"rename another user's correspondent", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/correspondents", map[string]any{"name": "国家电网"}, http.StatusCreated)
			return http.MethodPatch, "/api/v1/correspondents/" + itoa(r.body["id"].(float64)), map[string]any{"name": "taken over"}
		}},
		{"delete another user's document type", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/document-types", map[string]any{"name": "账单"}, http.StatusCreated)
			return http.MethodDelete, "/api/v1/document-types/" + itoa(r.body["id"].(float64)), nil
		}},
		{"move another user's tag", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/tags", map[string]any{"name": "税务"}, http.StatusCreated)
			return http.MethodPatch, "/api/v1/tags/" + itoa(r.body["id"].(float64)), map[string]any{"name": "taken over"}
		}},
		{"delete another user's custom field", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/custom-fields", map[string]any{"name": "户号", "dataType": "text"}, http.StatusCreated)
			return http.MethodDelete, "/api/v1/custom-fields/" + itoa(r.body["id"].(float64)), nil
		}},
		{"read another user's document", func() (string, string, any) {
			return http.MethodGet, "/api/v1/documents/" + itoa(uploadedDocument(alice, "secret.txt")), nil
		}},
		{"download another user's original", func() (string, string, any) {
			return http.MethodGet, "/api/v1/documents/" + itoa(uploadedDocument(alice, "secret2.txt")) + "/original", nil
		}},
		{"change another user's mail account", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/mail-accounts", map[string]any{"host": "imap.example.com", "username": "a", "password": "p"}, http.StatusCreated)
			return http.MethodPatch, "/api/v1/mail-accounts/" + itoa(r.body["id"].(float64)), map[string]any{"host": "evil.example.com", "username": "x"}
		}},
		{"edit the pages of another user's document", func() (string, string, any) {
			return http.MethodPut, "/api/v1/documents/" + itoa(uploadedDocument(alice, "secret5.txt")) + "/pages", map[string]any{"delete": []int{1}}
		}},
		{"split another user's document", func() (string, string, any) {
			return http.MethodPost, "/api/v1/documents/" + itoa(uploadedDocument(alice, "secret6.txt")) + "/split", map[string]any{"after": []int{1}}
		}},
		{"merge another user's documents", func() (string, string, any) {
			return http.MethodPost, "/api/v1/documents/merge", map[string]any{"ids": []float64{uploadedDocument(alice, "secret7.txt"), uploadedDocument(alice, "secret8.txt")}}
		}},
		{"read another user's thumbnail", func() (string, string, any) {
			return http.MethodGet, "/api/v1/documents/" + itoa(uploadedDocument(alice, "secret4.txt")) + "/thumbnail", nil
		}},
		{"reprocess another user's document", func() (string, string, any) {
			return http.MethodPost, "/api/v1/documents/" + itoa(uploadedDocument(alice, "secret3.txt")) + "/reprocess", nil
		}},
		{"read another user's drawer", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "房子"}, http.StatusCreated)
			return http.MethodGet, "/api/v1/drawers/" + itoa(r.body["drawer"].(map[string]any)["id"].(float64)), nil
		}},
		{"change another user's drawer rule", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "车"}, http.StatusCreated)
			return http.MethodPatch, "/api/v1/drawers/" + itoa(r.body["drawer"].(map[string]any)["id"].(float64)), map[string]any{"rule": map[string]any{"text": "x"}}
		}},
		{"delete another user's drawer", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "健康"}, http.StatusCreated)
			return http.MethodDelete, "/api/v1/drawers/" + itoa(r.body["drawer"].(map[string]any)["id"].(float64)), nil
		}},
		{"add a fact to another user's drawer", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "证件"}, http.StatusCreated)
			return http.MethodPost, "/api/v1/drawers/" + itoa(r.body["drawer"].(map[string]any)["id"].(float64)) + "/facts",
				map[string]any{"kind": "pinned", "label": "x", "config": map[string]any{}}
		}},
		{"edit another user's fact", func() (string, string, any) {
			r := alice.mustDo(http.MethodPost, "/api/v1/drawers", map[string]any{"name": "工作"}, http.StatusCreated)
			f := alice.mustDo(http.MethodPost, "/api/v1/drawers/"+itoa(r.body["drawer"].(map[string]any)["id"].(float64))+"/facts",
				map[string]any{"kind": "pinned", "label": "固定文档", "config": map[string]any{}}, http.StatusCreated)
			return http.MethodPatch, "/api/v1/drawer-facts/" + itoa(f.body["fact"].(map[string]any)["id"].(float64)), map[string]any{"label": "x", "config": map[string]any{}}
		}},
		{"rename another user's personal workspace", func() (string, string, any) {
			id := alice.mustDo(http.MethodGet, "/api/v1/me", nil, http.StatusOK).body["workspace"].(map[string]any)["id"].(float64)
			return http.MethodPatch, "/api/v1/workspaces/" + itoa(id), map[string]any{"name": "taken over"}
		}},
	}
	for _, tc := range cases {
		if !eeBuild && strings.Contains(tc.name, "mail account") { // a paid module (v1.13)
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			method, path, body := tc.create()
			r := bob.do(method, path, body)
			if r.code != http.StatusNotFound {
				t.Errorf("bob %s %s: status %d, want 404; body %s", method, path, r.code, r.raw)
			}
		})
	}

	if list := bob.mustDo(http.MethodGet, "/api/v1/me/tokens", nil, http.StatusOK).list; len(list) != 0 {
		t.Errorf("bob sees alice's tokens: %v", list)
	}
	if r := bob.mustDo(http.MethodGet, "/api/v1/documents?inbox=true", nil, http.StatusOK); r.body["total"] != float64(0) {
		t.Errorf("bob sees alice's documents: %v", r.body)
	}
	if list := bob.mustDo(http.MethodGet, "/api/v1/jobs", nil, http.StatusOK).list; len(list) != 0 {
		t.Errorf("bob sees alice's jobs: %v", list)
	}
	if list := bob.mustDo(http.MethodGet, "/api/v1/tasks", nil, http.StatusOK).list; len(list) != 0 {
		t.Errorf("bob sees alice's tasks: %v", list)
	}
	e.waitIndexed()
	search := "/api/v1/documents?filter=" + url.QueryEscape(`{"text":"secret"}`)
	if r := alice.mustDo(http.MethodGet, search, nil, http.StatusOK); r.body["total"] == float64(0) {
		t.Errorf("alice should find her own documents: %v", r.body)
	}
	if r := bob.mustDo(http.MethodGet, search, nil, http.StatusOK); r.body["total"] != float64(0) {
		t.Errorf("bob's search finds alice's documents: %v", r.body)
	}
	for _, path := range []string{"/api/v1/correspondents", "/api/v1/document-types", "/api/v1/tags"} {
		if list := bob.mustDo(http.MethodGet, path, nil, http.StatusOK).list; len(list) != 0 {
			t.Errorf("bob sees alice's %s: %v", path, list)
		}
	}
	if list := bob.mustDo(http.MethodGet, "/api/v1/custom-fields", nil, http.StatusOK).list; len(list) != 2 {
		t.Errorf("bob should see only his own two built-in fields: %v", list)
	}
	if list := bob.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list; len(list) != 1 {
		t.Errorf("bob sees alice's workspaces: %v", list)
	}
	// Nothing bob tried took effect.
	for _, w := range alice.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list {
		if w.(map[string]any)["name"] == "taken over" {
			t.Errorf("bob renamed alice's workspace: %v", w)
		}
	}
	list := alice.mustDo(http.MethodGet, "/api/v1/workspaces", nil, http.StatusOK).list
	if len(list) != 7 {
		t.Errorf("alice has %d workspaces, want 7", len(list))
	}
	for _, w := range list {
		if w := w.(map[string]any); w["memberCount"] != float64(1) || w["role"] != "owner" {
			t.Errorf("bob changed the membership of alice's workspace: %v", w)
		}
	}
}

// Guard: the set of endpoints reachable without login is derived from the
// spec (`security: []`). Adding one must be a deliberate, reviewed change.
func TestPublicOperationsAreExactlyTheseEighteen(t *testing.T) {
	s, err := NewServer(Deps{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]bool{
		"gethealth": true, "getversion": true, "getsetupstatus": true,
		"completesetup": true, "login": true, "logintwofactor": true,
		"getbranding": true, "getbrandinglogo": true,
		"pairdevice": true, // a phone has no account session yet; the one-time code is the credential
		// a share link's random token is the credential (v0.3 stage 18)
		"getpublicshare": true, "getpublicsharefile": true,
		// a share bundle's random token, likewise (v1.4.2)
		"getpublicbundle": true, "getpublicbundlefile": true, "getpublicbundlezip": true,
		// an upload link's random token, likewise; it only takes files in (v1.6.2)
		"getpublicuploadlink": true, "uploadthroughlink": true,
		// single sign-on: browser redirects; the provider's signed token is the credential (v1.0 stage 35)
		"startsso": true, "ssocallback": true,
	}
	for op := range s.public {
		if !want[op] {
			t.Errorf("operation %q is public but not in the reviewed list", op)
		}
	}
	for op := range want {
		if !s.public[op] {
			t.Errorf("operation %q should be public", op)
		}
	}
}

// workspacePath creates a family workspace as c and returns its URL.
func workspacePath(c *client, name string) string {
	r := c.mustDo(http.MethodPost, "/api/v1/workspaces", map[string]any{"name": name, "kind": "family"}, http.StatusCreated)
	return "/api/v1/workspaces/" + itoa(r.body["id"].(float64))
}

// Guard: the role each operation needs in the current workspace comes from
// the spec (`x-kept-min-role`, defaulting to viewer for GET and editor for
// anything else). "none" marks operations that act on the account itself or
// name their workspace in the path and check the role there. Adding an
// operation means adding it here, deliberately.
func TestOperationMinRoles(t *testing.T) {
	s, err := NewServer(Deps{})
	if err != nil {
		t.Fatal(err)
	}
	want := map[string]string{
		"logout": "none",
		"getme":  "none", "updateme": "none", "setmylocale": "none", "changepassword": "none",
		"listtokens": "none", "createtoken": "none", "revoketoken": "none",
		"gettwofactor": "none", "begintwofactor": "none", "enabletwofactor": "none",
		"disabletwofactor": "none", "regeneraterecoverycodes": "none",
		"listworkspaces": "none", "createworkspace": "none", "updateworkspace": "none", "deleteworkspace": "none",
		"listmembers": "none", "addmember": "none", "updatemember": "none", "removemember": "none",
		"adminlistusers": "none", "admincreateuser": "none", "adminupdateuser": "none",
		"adminsetpassword": "none", "admindisabletwofactor": "none",
		"adminupdatebranding": "none", "adminsetbrandinglogo": "none", "admindeletebrandinglogo": "none",
		// v1.12: the edition, shown to everyone; the license, the administrators'
		"getedition": "none", "admininstalllicense": "none", "adminremovelicense": "none",
		"listaudit": "owner", "adminlistaudit": "none",
		"listcorrespondents": "viewer", "createcorrespondent": "editor", "updatecorrespondent": "editor", "deletecorrespondent": "editor",
		"listdocumenttypes": "viewer", "createdocumenttype": "editor", "updatedocumenttype": "editor", "deletedocumenttype": "editor",
		"listtags": "viewer", "createtag": "editor", "updatetag": "editor", "deletetag": "editor",
		"verifysession":         "none",
		"listemergencycontacts": "owner", "addemergencycontact": "owner", "updateemergencycontact": "owner", "removeemergencycontact": "owner",
		"approveemergencyaccess": "owner", "denyemergencyaccess": "owner", "revokeemergencyaccess": "owner",
		"listmyemergencyaccess": "none", "requestemergencyaccess": "none", "cancelemergencyaccess": "none",
		"listpersons": "viewer", "createperson": "editor", "updateperson": "editor", "deleteperson": "editor",
		"listlocations": "viewer", "createlocation": "editor", "updatelocation": "editor", "deletelocation": "editor", "assignlocationbyasn": "editor",
		"attachchecklistdocument": "editor", "detachchecklistdocument": "editor", "uploadtochecklistitem": "editor",
		"listchecklists": "viewer", "getchecklist": "viewer", "checkchecklist": "viewer", "createchecklist": "editor", "updatechecklist": "editor", "deletechecklist": "editor",
		"listuploadlinks": "editor", "createuploadlink": "editor", "revokeuploadlink": "editor", "bulkdeletedirectoryitems": "editor", "mergedirectoryitems": "editor", "adddocumentversionfromdocument": "editor",
		"listcustomfields": "viewer", "createcustomfield": "editor", "updatecustomfield": "editor", "deletecustomfield": "editor",
		"uploaddocument": "editor", "listdocuments": "viewer", "getdocument": "viewer", "getdocumentoriginal": "viewer", "watermarkdocument": "viewer", "getdocumentsignatures": "viewer", "getdocumentseal": "viewer",
		"getdocumentpreview": "viewer", "getdocumentthumbnail": "viewer", "reprocessdocument": "editor", "listjobs": "viewer", "retryjob": "editor", "getevents": "viewer",
		"getdocumentfacets": "viewer", "updatedocument": "editor", "nextdocumentasn": "editor", "deletedocument": "editor", "restoredocument": "editor",
		"bulkdocuments": "editor", "downloaddocuments": "viewer", "getdocumenthistory": "viewer",
		"setdocumentpages": "editor", "splitdocument": "editor", "mergedocuments": "editor",
		"listnotes": "viewer", "createnote": "editor", "deletenote": "editor",
		"getdocumentbook": "viewer", "setdocumentbook": "editor", "deletedocumentbook": "editor", "enablelibrary": "editor", "disablelibrary": "editor",
		"lookupbook": "editor", "setbookcover": "editor", "deletebookcover": "editor", "fetchbookcover": "editor", "getdocumentcover": "viewer", "getreadingprogress": "viewer", "savereadingprogress": "viewer", "setreadingstate": "viewer",
		"listdocumenthighlights": "viewer", "createhighlight": "viewer", "searchhighlights": "viewer", "updatehighlight": "viewer", "deletehighlight": "viewer",
		"listbooklists": "viewer", "createbooklist": "editor", "getbooklist": "viewer", "updatebooklist": "editor", "deletebooklist": "editor",
		"addbooklistitem": "editor", "updatebooklistitem": "editor", "removebooklistitem": "editor", "reorderbooklist": "editor", "getdocumentbooklists": "viewer", "getsimilardocuments": "viewer", "getdocumentfileinfo": "viewer", "askdocument": "viewer", "movedocuments": "editor", "exportworkspace": "owner",
		"listqaconversations": "viewer", "createqaconversation": "viewer", "getqaconversation": "viewer", "deleteqaconversation": "viewer", "askquestion": "viewer",
		"organizedocuments": "editor", "listorganizesuggestions": "viewer", "refreshorganizesuggestions": "editor",
		"listfactsuggestions": "viewer", "writeyearbooknarrative": "editor",
		"startsso": "none", "ssocallback": "none", "adminunlinkusersso": "none",
		"getstorage": "viewer", "adminlistworkspaces": "none", "adminsetworkspacequota": "none",
		"adminsetworkspacestorage": "none", "adminliststorages": "none", "adminteststorage": "none",
		"admingetexporttarget": "none", "adminsetexporttarget": "none", "adminrunexporttostorage": "none",
		"setstoragecold": "owner", "adminsetworkspacecold": "none", "setkeepversions": "owner", "checksamename": "editor", "adminsetwatch": "none",
		"listmemberuploads": "owner", "setmemberuploadquota": "owner",
		"getremoteinbox": "owner", "setremoteinbox": "owner", "scanremoteinbox": "owner",
		"adminenablebackupversioning": "none", "admingetbackuptarget": "none", "adminsetbackuptarget": "none", "adminrunbackuptostorage": "none",
		"admincreatestorage": "none", "adminupdatestorage": "none", "admindeletestorage": "none", "admintrystorage": "none", "getstoragelocation": "owner", "setstoragelocation": "owner",
		// v1.11.0: encrypted storages
		"adminsetstorageencryption": "none", "admingetencryption": "none", "adminnewrecoverycode": "none", "adminrecoverencryption": "none",
		"listcollections": "viewer", "createcollection": "owner", "updatecollection": "owner", "deletecollection": "owner", "sharecollection": "owner", "unsharecollection": "owner",
		"listsharedcollections": "viewer", "getsharedcollection": "viewer", "listsharedcollectiondocuments": "viewer", "getsharedcollectiondocument": "viewer", "getsharedcollectiondocumentfile": "viewer",
		"assistbook": "editor", "getbooksummary": "viewer", "deletebooksummary": "editor", "listreviewcards": "viewer", "generatereviewcards": "viewer", "reviewcard": "viewer", "deletereviewcard": "viewer",
		"listtrash": "viewer", "purgedocument": "owner", "emptytrash": "owner",
		"listdrawers": "viewer", "createdrawer": "editor", "getdrawer": "viewer", "updatedrawer": "editor", "deletedrawer": "editor",
		"reorderdrawers": "editor", "previewdrawers": "editor", "createdrawersfromtemplates": "editor", "listdrawertemplates": "viewer",
		"createdrawerfact": "editor", "updatedrawerfact": "editor", "deletedrawerfact": "editor", "reorderdrawerfacts": "editor",
		"listalerts": "viewer", "dismissalert": "editor", "restorealert": "editor", "listyearbookyears": "viewer", "getyearbook": "viewer",
		"listdrawershares": "viewer", "sharedrawer": "owner", "unsharedrawer": "owner", "listshareddrawers": "viewer", "getshareddrawer": "viewer",
		"listshareddocuments": "viewer", "getshareddocument": "viewer", "getshareddocumentfile": "viewer",
		"listtasks": "viewer", "admingetsystem": "none", "adminlistlogs": "none", "adminrunbackup": "none", "adminsetbackupdir": "none",
		"listdevices": "none", "revokedevice": "none", "forgetcurrentdevice": "none", "createpairingcode": "none",
		"getmobilebootstrap": "viewer", "getuploadstatus": "viewer",
		"listmailaccounts": "owner", "createmailaccount": "owner", "updatemailaccount": "owner", "deletemailaccount": "owner",
		"testmailaccount": "owner", "fetchmailaccount": "owner", "listmailmessages": "owner", "refetchmailmessage": "owner",
		"listworkflows": "owner", "createworkflow": "owner", "updateworkflow": "owner", "deleteworkflow": "owner", "reorderworkflows": "owner", "listworkflowruns": "owner",
		"listdocumentversions": "viewer", "adddocumentversion": "editor", "removedocumentpassword": "editor", "deletedocumentversion": "editor",
		"setcurrentdocumentversion": "editor", "getdocumentversionfile": "viewer", "getdocumentversiontext": "viewer", "getdocumentversionpreview": "viewer", "getdocumentsheets": "viewer", "getdocumentword": "viewer", "getdocumentwordimage": "viewer", "listzipentries": "viewer", "importzipentries": "editor",
		"getagentsettings": "viewer", "setagentsettings": "owner", "listagentruns": "owner", "listdocumentproposals": "viewer",
		"acceptproposal": "editor", "rejectproposal": "editor", "revertproposal": "editor", "admintestai": "none",
		"getmilestonerules": "viewer", "setmilestonerules": "owner",
		"listduplicates": "viewer", "scanduplicates": "editor", "keepduplicate": "editor", "dismissduplicate": "editor",
		"listsharelinks": "editor", "createsharelink": "editor", "revokesharelink": "editor",
		"listactiveshares": "editor", "createsharebundle": "editor", "revokesharebundle": "editor",
		"emaildocuments": "editor", "admintestsmtp": "none", "getnotificationprefs": "viewer", "setnotificationprefs": "viewer",
		"getclassifier": "viewer", "trainclassifier": "editor", "testmatchrule": "viewer",
		"listsavedviews": "viewer", "createsavedview": "editor", "updatesavedview": "editor", "deletesavedview": "editor", "reordersavedviews": "editor",
		// v1.8.0: a person manages their own privacy whatever their role; the service checks who may
		"setpersonaccess": "viewer", "removepersonaccess": "viewer", "reviewpersonaccess": "viewer", "revokepersonuploader": "viewer",
		"setpersonemergency": "viewer", "removepersonemergency": "viewer", "denypersonemergency": "viewer",
		"requestpersonemergency": "viewer", "cancelpersonemergency": "viewer", "getmyprivacy": "viewer",
		// v1.8.1: keys and cloud AI, the same
		"setuppersonkey": "viewer", "newpersonrecoverycode": "viewer", "checkpersonrecoverycode": "viewer", "setpersoncloudai": "viewer",
		// v1.8.2: metrics from lab reports
		"enablehealthtemplate": "editor", "gethealthoverview": "viewer", "gethealthseries": "viewer", "listmetrics": "viewer", "createmetric": "editor",
		"updatemetric": "editor", "getdocumentlab": "viewer", "savedocumentlab": "editor", "extractdocumentlab": "editor", "getvisit": "viewer",
		"listhealthalerts": "viewer", "dismisshealthalert": "editor", "restorehealthalert": "editor", "gethealthsettings": "viewer", "sethealthsettings": "owner",
		"gethealthreport": "viewer", "sharehealthreport": "editor", "setvisitdocuments": "editor",
		"listscenes": "viewer", "createscene": "editor", "updatescene": "editor", "deletescene": "editor", "enablescenetemplate": "editor",
		"createobservation": "editor", "deleteobservation": "editor", "listmedications": "viewer", "createmedication": "editor", "updatemedication": "editor", "deletemedication": "editor",
	}
	for op, role := range s.minRole {
		if s.public[op] {
			continue
		}
		if want[op] != string(role) {
			t.Errorf("operation %q needs role %q, reviewed list says %q", op, role, want[op])
		}
	}
	for op := range want {
		if _, ok := s.minRole[op]; !ok {
			t.Errorf("operation %q is in the reviewed list but not in the spec", op)
		}
	}
}

// TestWorkspaceRoleIsolation covers the three membership dimensions for
// every resource that lives in the current workspace (selected with
// X-Workspace-ID): an outsider gets 404, a role that is too low gets 403,
// and a member who was removed gets 404 from then on.
//
// Every milestone that adds a workspace-scoped resource (tags, documents,
// drawers, ...) MUST add a row here. needs is the lowest role that may make
// the request.
func TestWorkspaceRoleIsolation(t *testing.T) {
	cases := []struct {
		name         string
		method, path string
		body         any    // a func() any is called for a fresh body on every request
		needs        string // viewer | editor | owner
		okStatus     int
	}{
		{"read the activity log", http.MethodGet, "/api/v1/audit", nil, "owner", http.StatusOK},
		{"list correspondents", http.MethodGet, "/api/v1/correspondents", nil, "viewer", http.StatusOK},
		{"create a correspondent", http.MethodPost, "/api/v1/correspondents", uniqueName, "editor", http.StatusCreated},
		{"list document types", http.MethodGet, "/api/v1/document-types", nil, "viewer", http.StatusOK},
		{"create a document type", http.MethodPost, "/api/v1/document-types", uniqueName, "editor", http.StatusCreated},
		{"list tags", http.MethodGet, "/api/v1/tags", nil, "viewer", http.StatusOK},
		{"create a tag", http.MethodPost, "/api/v1/tags", uniqueName, "editor", http.StatusCreated},
		{"list custom fields", http.MethodGet, "/api/v1/custom-fields", nil, "viewer", http.StatusOK},
		{"list documents", http.MethodGet, "/api/v1/documents", nil, "viewer", http.StatusOK},
		{"search documents", http.MethodGet, "/api/v1/documents?sort=relevance&filter=%7B%22text%22%3A%22%E7%94%B5%E8%B4%B9%22%7D", nil, "viewer", http.StatusOK},
		{"count documents by facet", http.MethodGet, "/api/v1/documents/facets?groups=type,tag", nil, "viewer", http.StatusOK},
		{"list the trash", http.MethodGet, "/api/v1/trash", nil, "viewer", http.StatusOK},
		{"empty the trash", http.MethodDelete, "/api/v1/trash", nil, "owner", http.StatusOK},
		{"list saved views", http.MethodGet, "/api/v1/saved-views", nil, "viewer", http.StatusOK},
		{"save a view", http.MethodPost, "/api/v1/saved-views", func() any {
			b := uniqueName().(map[string]any)
			b["filter"] = map[string]any{}
			return b
		}, "editor", http.StatusCreated},
		{"list jobs", http.MethodGet, "/api/v1/jobs", nil, "viewer", http.StatusOK},
		{"mobile bootstrap", http.MethodGet, "/api/v1/mobile/bootstrap", nil, "viewer", http.StatusOK},
		{"list mail accounts", http.MethodGet, "/api/v1/mail-accounts", nil, "owner", http.StatusOK},
		{"upload status", http.MethodPost, "/api/v1/uploads/status", map[string]any{"clientUploadIds": []string{"x"}}, "viewer", http.StatusOK},
		{"create a custom field", http.MethodPost, "/api/v1/custom-fields", func() any {
			b := uniqueName().(map[string]any)
			b["dataType"] = "text"
			return b
		}, "editor", http.StatusCreated},
	}
	rank := map[string]int{"viewer": 1, "editor": 2, "owner": 3}
	for _, tc := range cases {
		if paidPath(tc.path) {
			continue
		}
		t.Run(tc.name, func(t *testing.T) {
			e := newEnv(t, "auto", nil)
			alice, bob, carol, homeID := home(t, e) // owner, editor, viewer
			outsider := e.user("dave").inSpace(homeID)

			body := func() any {
				if f, ok := tc.body.(func() any); ok {
					return f()
				}
				return tc.body
			}
			if r := outsider.do(tc.method, tc.path, body()); r.code != http.StatusNotFound || code(r) != "workspace_not_found" {
				t.Errorf("outsider: %d %v, want 404 workspace_not_found", r.code, r.body)
			}
			for role, c := range map[string]*client{"owner": alice, "editor": bob, "viewer": carol} {
				want := tc.okStatus
				if rank[role] < rank[tc.needs] {
					want = http.StatusForbidden
				}
				if r := c.do(tc.method, tc.path, body()); r.code != want {
					t.Errorf("%s: %d %v, want %d", role, r.code, r.body, want)
				}
			}

			// Promote bob so he is allowed, check, then remove him: the very
			// next request must not find the workspace.
			members := "/api/v1/workspaces/" + itoa(homeID) + "/members/" + itoa(userID(bob))
			alice.mustDo(http.MethodPatch, members, map[string]any{"role": "owner"}, http.StatusOK)
			bob.mustDo(tc.method, tc.path, body(), tc.okStatus)
			alice.mustDo(http.MethodDelete, members, nil, http.StatusNoContent)
			if r := bob.do(tc.method, tc.path, body()); r.code != http.StatusNotFound || code(r) != "workspace_not_found" {
				t.Errorf("after removal: %d %v, want 404 workspace_not_found", r.code, r.body)
			}
		})
	}
}

var uniqueCounter int

// uniqueName is a request body whose name has not been used yet, for
// creating the same kind of item several times in one test.
func uniqueName() any {
	uniqueCounter++
	return map[string]any{"name": "item-" + itoa(float64(uniqueCounter))}
}

// uploadedDocument uploads a text file as c and returns the new document's ID.
func uploadedDocument(c *client, name string) float64 {
	c.env.t.Helper()
	r := c.upload(name, "content of "+name, nil)
	if r.code != http.StatusAccepted {
		c.env.t.Fatalf("upload %s: %d %s", name, r.code, r.raw)
	}
	return c.waitJob(r.body["jobId"].(float64))["documentId"].(float64)
}

// TestViewersCannotUpload: uploads are multipart, outside the JSON table above.
func TestViewersCannotUpload(t *testing.T) {
	e := newEnv(t, "auto", nil)
	_, bob, carol, homeID := home(t, e)
	if r := carol.upload("a.txt", "x", nil); r.code != http.StatusForbidden {
		t.Errorf("viewer upload: %d", r.code)
	}
	if r := bob.upload("a.txt", "x", nil); r.code != http.StatusAccepted {
		t.Errorf("editor upload: %d %s", r.code, r.raw)
	}
	outsider := e.user("dave").inSpace(homeID)
	if r := outsider.upload("a.txt", "y", nil); r.code != http.StatusNotFound {
		t.Errorf("outsider upload: %d", r.code)
	}
}
