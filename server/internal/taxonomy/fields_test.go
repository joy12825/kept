package taxonomy

import (
	"context"
	"errors"
	"testing"
	"time"

	"kept/server/internal/apperr"
	"kept/server/internal/store/sqlc"
)

func TestSeedWorkspaceCreatesTheBuiltInFields(t *testing.T) {
	e := newEnv(t)
	tx, err := e.st.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	if err := SeedWorkspace(context.Background(), sqlc.New(e.st.DB).WithTx(tx), 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	if err := tx.Commit(); err != nil {
		t.Fatal(err)
	}
	fields, err := e.s.ListFields(ctx, home)
	if err != nil {
		t.Fatal(err)
	}
	if len(fields) != 2 {
		t.Fatalf("fields = %+v", fields)
	}
	// Pinyin order: daoqiri before jine.
	if f := fields[0]; f.Key != "due_date" || f.Name != "到期日" || f.DataType != "date" || !f.BuiltIn || f.Initial != "D" {
		t.Errorf("due_date = %+v", f)
	}
	if f := fields[1]; f.Key != "amount" || f.Name != "金额" || f.DataType != "monetary" || f.Extra.Currency != "CNY" || !f.BuiltIn {
		t.Errorf("amount = %+v", f)
	}
}

func TestCustomFields(t *testing.T) {
	e := newEnv(t)
	note, err := e.s.CreateField(ctx, home, FieldInput{Name: "户号", DataType: "text"})
	if err != nil || note.DataType != "text" || note.BuiltIn || note.Key != "" {
		t.Fatalf("text field = %+v, %v", note, err)
	}
	usd, err := e.s.CreateField(ctx, home, FieldInput{Name: "美元金额", DataType: "monetary", Extra: FieldExtra{Currency: "USD"}})
	if err != nil || usd.Extra.Currency != "USD" {
		t.Errorf("monetary = %+v, %v", usd, err)
	}
	cny, _ := e.s.CreateField(ctx, home, FieldInput{Name: "报销金额", DataType: "monetary"})
	if cny.Extra.Currency != "CNY" {
		t.Errorf("default currency = %q", cny.Extra.Currency)
	}
	status, err := e.s.CreateField(ctx, home, FieldInput{Name: "报销状态", DataType: "select",
		Extra: FieldExtra{Options: []SelectOption{{Label: "未报销"}, {Label: " 已报销 "}}}})
	if err != nil {
		t.Fatal(err)
	}
	if len(status.Extra.Options) != 2 || status.Extra.Options[1].Label != "已报销" || status.Extra.Options[0].ID == "" || status.Extra.Options[0].ID == status.Extra.Options[1].ID {
		t.Errorf("options = %+v", status.Extra.Options)
	}

	bad := []FieldInput{
		{Name: "x", DataType: "colour"},
		{Name: "x", DataType: "monetary", Extra: FieldExtra{Currency: "rmb"}},
		{Name: "x", DataType: "select"},
		{Name: "x", DataType: "select", Extra: FieldExtra{Options: []SelectOption{{Label: "a"}, {Label: "a"}}}},
		{Name: "x", DataType: "select", Extra: FieldExtra{Options: []SelectOption{{Label: "  "}}}},
	}
	for _, in := range bad {
		if _, err := e.s.CreateField(ctx, home, in); !isKind(err, apperr.KindInvalid) {
			t.Errorf("CreateField(%+v) err = %v, want invalid", in, err)
		}
	}
	if _, err := e.s.CreateField(ctx, home, FieldInput{Name: "户号", DataType: "integer"}); !isKind(err, apperr.KindConflict) {
		t.Errorf("duplicate err = %v", err)
	}

	// Options keep their IDs when renamed; new ones get one; the type cannot change.
	keep := status.Extra.Options[0]
	opts := []SelectOption{{ID: keep.ID, Label: "待报销"}, {Label: "已拒绝"}}
	got, err := e.s.UpdateField(ctx, home, status.ID, FieldPatch{Extra: &FieldExtra{Options: opts}})
	if err != nil {
		t.Fatal(err)
	}
	if len(got.Extra.Options) != 2 || got.Extra.Options[0].ID != keep.ID || got.Extra.Options[0].Label != "待报销" || got.Extra.Options[1].ID == "" {
		t.Errorf("updated options = %+v", got.Extra.Options)
	}
	forged := []SelectOption{{ID: "not-an-existing-option", Label: "x"}}
	if _, err := e.s.UpdateField(ctx, home, status.ID, FieldPatch{Extra: &FieldExtra{Options: forged}}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("unknown option id err = %v", err)
	}

	if err := e.s.DeleteField(ctx, home, note.ID); err != nil {
		t.Fatal(err)
	}
	if err := e.s.DeleteField(ctx, other, usd.ID); !errors.Is(err, ErrNotFound) {
		t.Errorf("delete from another workspace err = %v", err)
	}
}

func TestBuiltInFieldsCanOnlyBeRenamed(t *testing.T) {
	e := newEnv(t)
	tx, _ := e.st.DB.Begin()
	if err := SeedWorkspace(context.Background(), sqlc.New(e.st.DB).WithTx(tx), 1, time.Now()); err != nil {
		t.Fatal(err)
	}
	_ = tx.Commit()
	fields, _ := e.s.ListFields(ctx, home)
	amount := fields[1]

	if err := e.s.DeleteField(ctx, home, amount.ID); !isKind(err, apperr.KindConflict) {
		t.Errorf("deleting a built-in field err = %v, want conflict", err)
	}
	if _, err := e.s.UpdateField(ctx, home, amount.ID, FieldPatch{Extra: &FieldExtra{Currency: "USD"}}); !isKind(err, apperr.KindInvalid) {
		t.Errorf("changing a built-in field's currency err = %v, want invalid", err)
	}
	got, err := e.s.UpdateField(ctx, home, amount.ID, FieldPatch{Name: ptr("金额（含税）")})
	if err != nil || got.Name != "金额（含税）" || got.Key != "amount" {
		t.Errorf("rename built-in = %+v, %v", got, err)
	}
}
