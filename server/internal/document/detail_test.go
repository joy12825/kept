package document

import "testing"

func TestLinkedFromAndNextASN(t *testing.T) {
	e := newEnv(t)
	e.fixture()
	e.exec(`INSERT INTO custom_fields (id, workspace_id, name, sort_key, data_type, extra, created_at, updated_at) VALUES (53, 1, '合同', 'hetong', 'document_link', '{}', 1, 1)`)
	// 1 and 2 (bills) link to the contract 3; so do the deleted 6 and 3 itself
	for _, from := range []int64{1, 2, 6, 3} {
		e.exec(`INSERT INTO custom_field_values (document_id, field_id, value_doclink) VALUES (?, 53, 3)`, from)
	}
	d, err := e.svc.GetDetail(e.ctx, e.scope, 3)
	if err != nil {
		t.Fatal(err)
	}
	if len(d.LinkedFrom) != 2 || d.LinkedFrom[0].ID != 2 || d.LinkedFrom[1].ID != 1 || d.LinkedFrom[0].Field != "合同" {
		t.Errorf("linked from = %+v", d.LinkedFrom)
	}
	if d, _ := e.svc.GetDetail(e.ctx, e.scope, 4); d.LinkedFrom == nil || len(d.LinkedFrom) != 0 {
		t.Errorf("nothing links to 4: %+v", d.LinkedFrom)
	}

	if n, err := e.svc.NextASN(e.ctx, e.scope); err != nil || n != 1 {
		t.Errorf("next asn with none = %d, %v", n, err)
	}
	e.exec(`UPDATE documents SET asn = 12 WHERE id = 1`)
	e.exec(`UPDATE documents SET asn = 40 WHERE id = 6`) // in the trash: still taken
	e.exec(`UPDATE documents SET asn = 99 WHERE id = 7`) // another workspace
	if n, _ := e.svc.NextASN(e.ctx, e.scope); n != 41 {
		t.Errorf("next asn = %d, want 41", n)
	}
}
