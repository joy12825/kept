package demo

import "testing"

func TestQAEval(t *testing.T) {
	docs, qs := QAEval()
	titles := map[string]bool{}
	for _, d := range docs {
		titles[d.Title] = true
	}
	notFound := 0
	for _, q := range qs {
		if q.NotFound {
			notFound++
		}
		if q.Title != "" && !titles[q.Title] {
			t.Errorf("%s: no document %q", q.Question, q.Title)
		}
		if !q.NotFound && q.Want == "" {
			t.Errorf("%s: nothing to check", q.Question)
		}
	}
	if len(qs) != 100 || notFound != 10 {
		t.Errorf("%d questions, %d not found", len(qs), notFound)
	}
}
