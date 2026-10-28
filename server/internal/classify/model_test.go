package classify

import (
	"context"
	"database/sql"
	"fmt"
	"path/filepath"
	"testing"

	"kept/server/internal/store"
)

// corpus: electricity bills (type 1, correspondent 10, tag 100), phone
// contracts (type 2, correspondent 11) and miscellaneous notes (nothing).
func corpus(n int) []Sample {
	var out []Sample
	for i := range n {
		out = append(out,
			Sample{Features: Features(fmt.Sprintf("国家电网 电费 通知单 户号 本期 用电量 第%d期 抄表", i)), Type: 1, Correspondent: 10, Tags: []int64{100}},
			Sample{Features: Features(fmt.Sprintf("中国移动 套餐 合同 协议 期限 甲方 乙方 编号%d", i)), Type: 2, Correspondent: 11},
			Sample{Features: Features(fmt.Sprintf("随手 笔记 菜谱 旅行 照片 清单 备忘 %d", i))},
		)
	}
	return out
}

var allLearn = Learners{
	Types:          map[int64]bool{1: true, 2: true},
	Correspondents: map[int64]bool{10: true, 11: true},
	Tags:           map[int64]bool{100: true},
}

func TestTrainAndPredict(t *testing.T) {
	m, sum := Train(corpus(6), allLearn)
	if m == nil {
		t.Fatalf("no model: %+v", sum)
	}
	b, err := m.Encode()
	if err != nil {
		t.Fatal(err)
	}
	if m, err = Decode(b); err != nil {
		t.Fatal(err)
	}

	p := m.Predict(Features("国家电网 三月 电费 通知单 用电量"))
	if p.Type == nil || p.Type.ID != 1 || p.Type.P < TypeThreshold {
		t.Errorf("type = %+v, want 1 above the threshold", p.Type)
	}
	if p.Correspondent == nil || p.Correspondent.ID != 10 || p.Correspondent.P < CorrespondentThreshold {
		t.Errorf("correspondent = %+v, want 10", p.Correspondent)
	}
	if len(p.Tags) != 1 || p.Tags[0].ID != 100 || p.Tags[0].P < TagThreshold {
		t.Errorf("tags = %+v, want 100", p.Tags)
	}

	p = m.Predict(Features("中国移动 套餐 协议 乙方"))
	if p.Type == nil || p.Type.ID != 2 || p.Correspondent == nil || p.Correspondent.ID != 11 {
		t.Errorf("contract = %+v %+v", p.Type, p.Correspondent)
	}
	if len(p.Tags) == 1 && p.Tags[0].P >= TagThreshold {
		t.Errorf("a contract is not a bill, tag p = %.2f", p.Tags[0].P)
	}

	// Unlike anything learned: “other” wins and nothing is suggested.
	p = m.Predict(Features("旅行 照片 清单 备忘"))
	if p.Type != nil || p.Correspondent != nil {
		t.Errorf("a note should get nothing, got %+v %+v", p.Type, p.Correspondent)
	}
}

func TestTrainNeedsEnoughData(t *testing.T) {
	if m, sum := Train(corpus(3), allLearn); m != nil || sum.Skipped != "too_few_documents" {
		t.Errorf("9 documents: model %v, skipped %q", m != nil, sum.Skipped)
	}
	// An item with a single sample is not learned; its document is left out.
	s := corpus(4)
	s = append(s, Sample{Features: Features("自来水 水费 账单"), Type: 3})
	learn := allLearn
	learn.Types = map[int64]bool{1: true, 2: true, 3: true}
	m, sum := Train(s, learn)
	if m == nil {
		t.Fatal("no model")
	}
	for _, it := range sum.Items {
		if it.Kind == "type" && it.ID == 3 && (it.Learned || it.Samples != 1) {
			t.Errorf("type 3 = %+v, want 1 sample, not learned", it)
		}
	}
	for _, c := range m.Types.Classes {
		if c == 3 {
			t.Error("type 3 must not be a class")
		}
	}
	// Nothing learns automatically.
	if m, sum := Train(corpus(6), Learners{}); m != nil || sum.Skipped != "nothing_to_learn" {
		t.Errorf("no auto items: model %v, skipped %q", m != nil, sum.Skipped)
	}
}

func TestEvaluate(t *testing.T) {
	r := evaluate(corpus(10), allLearn, 5)
	if r.Skipped != "" || r.TypeAccuracy < 0.9 || r.CorrespondentAccuracy < 0.9 {
		t.Errorf("eval = %+v", r)
	}
}

func TestServiceTrainsOnlyWhenTheDataChanged(t *testing.T) {
	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(t.TempDir(), "app.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()
	db := st.DB
	mustExec(t, db, `INSERT INTO workspaces (id, name, kind, created_at) VALUES (1, 'a', 'family', 1)`)
	mustExec(t, db, `INSERT INTO document_types (id, workspace_id, name, sort_key, created_at, updated_at) VALUES (1, 1, '账单', 'z', 1, 1), (2, 1, '合同', 'h', 1, 1)`)
	mustExec(t, db, `INSERT INTO correspondents (id, workspace_id, name, sort_key, algorithm, match, created_at, updated_at) VALUES (10, 1, '国家电网', 'g', 'any', '国家电网', 1, 1)`)
	for i := range 12 {
		typ, text := 1, "电费 通知单 用电量 抄表"
		if i%2 == 1 {
			typ, text = 2, "套餐 合同 协议 乙方"
		}
		mustExec(t, db, `INSERT INTO documents (workspace_id, title, content, created_date, added_at, modified_at, document_type_id, mime_type, original_filename, original_ext, checksum, inbox)
			VALUES (1, 't', ?, '2024-01-01', 1, 1, ?, 'text/plain', 'a.txt', 'txt', ?, 0)`, text, typ, fmt.Sprint(i))
	}
	s := NewService(db, Options{})
	r, err := s.Train(ctx, 1, false)
	if err != nil || !r.Trained || r.Unchanged {
		t.Fatalf("first training = %+v, %v", r, err)
	}
	m, err := s.Model(ctx, 1)
	if err != nil || m == nil || m.Types == nil || m.Correspondents != nil {
		t.Fatalf("model = %+v, %v (the correspondent has a rule and must not learn)", m, err)
	}
	if r, _ := s.Train(ctx, 1, false); !r.Unchanged {
		t.Error("same data: training should be skipped")
	}
	if r, _ := s.Train(ctx, 1, true); r.Unchanged {
		t.Error("force retrains")
	}
	mustExec(t, db, `UPDATE documents SET document_type_id = 2 WHERE id = 1`)
	if r, _ := s.Train(ctx, 1, false); r.Unchanged {
		t.Error("changed data: training should run")
	}
	st2, err := s.Status(ctx, 1)
	if err != nil || st2.TrainedAt == nil || st2.Summary.DocCount != 12 {
		t.Errorf("status = %+v, %v", st2, err)
	}
	// Too few documents: the old model is replaced by none.
	mustExec(t, db, `UPDATE documents SET inbox = 1 WHERE id > 3`)
	r, _ = s.Train(ctx, 1, false)
	if r.Trained || r.Summary.Skipped != "too_few_documents" {
		t.Errorf("after filing back = %+v", r)
	}
	if m, _ := s.Model(ctx, 1); m != nil {
		t.Error("the old model must be gone")
	}
}

func mustExec(t *testing.T, db *sql.DB, q string, args ...any) {
	t.Helper()
	if _, err := db.Exec(q, args...); err != nil {
		t.Fatalf("%s: %v", q, err)
	}
}

// v1.6.3: a family member is learned like a correspondent.
func TestTrainPersons(t *testing.T) {
	var s []Sample
	for i := range 6 {
		s = append(s,
			Sample{Features: Features(fmt.Sprintf("体检 报告 血压 血常规 王建国 第%d年", i)), Person: 20},
			Sample{Features: Features(fmt.Sprintf("幼儿园 接种 疫苗 乐乐 小班 第%d次", i)), Person: 21},
			Sample{Features: Features(fmt.Sprintf("电费 账单 户号 用电量 %d", i))},
		)
	}
	m, sum := Train(s, Learners{Persons: map[int64]bool{20: true, 21: true}})
	if m == nil || m.Persons == nil {
		t.Fatalf("no person model: %+v", sum)
	}
	if p := m.Predict(Features("乐乐 疫苗 接种 记录")); p.Person == nil || p.Person.ID != 21 || p.Person.P < PersonThreshold {
		t.Errorf("person = %+v, want 21", p.Person)
	}
	if p := m.Predict(Features("电费 账单 户号")); p.Person != nil && p.Person.P >= PersonThreshold {
		t.Errorf("a bill is nobody's: %+v", p.Person)
	}
	// older models without persons still decode and predict nothing for them
	old := &Model{Terms: m.Terms, IDF: m.IDF}
	if p := old.Predict(Features("乐乐")); p.Person != nil {
		t.Errorf("old model: %+v", p.Person)
	}
}
