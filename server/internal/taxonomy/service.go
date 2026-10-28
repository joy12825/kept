// Package taxonomy manages the ways documents are classified: correspondents,
// document types, family members (persons), tags (a hierarchy) and custom fields. Everything lives in
// one workspace and is reached through a tenant.Scope.
package taxonomy

import (
	"context"
	"database/sql"
	"fmt"
	"kept/server/internal/i18n"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"kept/server/internal/audit"
	"kept/server/internal/pinyin"
	"kept/server/internal/store/sqlc"
)

type Options struct {
	Now func() time.Time // default time.Now
}

type Service struct {
	db  *sql.DB
	q   *sqlc.Queries
	now func() time.Time
}

func NewService(db *sql.DB, opt Options) *Service {
	if opt.Now == nil {
		opt.Now = time.Now
	}
	return &Service{db: db, q: sqlc.New(db), now: opt.Now}
}

// Kind names a kind of taxonomy item; it is also the audit action prefix.
type Kind string

const (
	Correspondent Kind = "correspondent"
	DocumentType  Kind = "document_type"
	Tag           Kind = "tag"
	CustomField   Kind = "custom_field"
	Person        Kind = "person" // v1.6.3: 家人
)

// summaries are each kind's audit summaries, whole formats beginning
// with the actor's name so they can be translated (v1.10.1).
type summaries struct{ create, update, rename, delete, merge string }

var kindSummaries = map[Kind]summaries{
	Correspondent: {create: i18n.N("%s 新建了往来方“%s”"), update: i18n.N("%s 修改了往来方“%s”"), rename: i18n.N("%s 将往来方“%s”改名为“%s”"),
		delete: i18n.N("%s 删除了往来方“%s”"), merge: i18n.N("%s 把往来方%s合并到“%s”")},
	DocumentType: {create: i18n.N("%s 新建了类型“%s”"), update: i18n.N("%s 修改了类型“%s”"), rename: i18n.N("%s 将类型“%s”改名为“%s”"),
		delete: i18n.N("%s 删除了类型“%s”"), merge: i18n.N("%s 把类型%s合并到“%s”")},
	Tag: {create: i18n.N("%s 新建了标签“%s”"), update: i18n.N("%s 修改了标签“%s”"), rename: i18n.N("%s 将标签“%s”改名为“%s”"),
		delete: i18n.N("%s 删除了标签“%s”"), merge: i18n.N("%s 把标签%s合并到“%s”")},
	CustomField: {create: i18n.N("%s 新建了自定义字段“%s”"), update: i18n.N("%s 修改了自定义字段“%s”"), rename: i18n.N("%s 将自定义字段“%s”改名为“%s”"),
		delete: i18n.N("%s 删除了自定义字段“%s”")},
	Person: {create: i18n.N("%s 新建了家人“%s”"), update: i18n.N("%s 修改了家人“%s”"), rename: i18n.N("%s 将家人“%s”改名为“%s”"),
		delete: i18n.N("%s 删除了家人“%s”"), merge: i18n.N("%s 把家人%s合并到“%s”")},
	locationKind: {create: i18n.N("%s 新建了存放位置“%s”"), update: i18n.N("%s 修改了存放位置“%s”"), rename: i18n.N("%s 将存放位置“%s”改名为“%s”"),
		delete: i18n.N("%s 删除了存放位置“%s”")},
}

// Matching algorithms (design doc 5.6). Rules are stored now and applied
// when documents are classified (milestone 1.7).
const (
	AlgorithmAuto = "auto"
	AlgorithmNone = "none"
)

var algorithms = map[string]bool{"none": true, "any": true, "all": true, "literal": true, "regex": true, "fuzzy": true, "auto": true}

// MatchRule decides how a new document is matched to an item.
type MatchRule struct {
	Algorithm       string
	Match           string // keywords, a phrase or a regular expression
	CaseInsensitive bool
}

// DefaultRule lets the classifier learn from filed documents.
var DefaultRule = MatchRule{Algorithm: AlgorithmAuto, CaseInsensitive: true}

// ValidateRule checks a rule as saving it would (“试一试” uses it) and
// returns it tidied.
func ValidateRule(r MatchRule) (MatchRule, error) { return r.normalized() }

func (r MatchRule) normalized() (MatchRule, error) {
	if !algorithms[r.Algorithm] {
		return r, errRule
	}
	r.Match = strings.TrimSpace(r.Match)
	if r.Algorithm == AlgorithmAuto || r.Algorithm == AlgorithmNone {
		r.Match = ""
		return r, nil
	}
	if r.Match == "" {
		return r, errKeywords
	}
	if r.Algorithm == "regex" {
		if _, err := regexp.Compile(r.Match); err != nil {
			return r, errRegex(err.Error())
		}
	}
	return r, nil
}

// changes adds the rule fields that differ between a and b.
func (r MatchRule) changes(to MatchRule, into map[string][2]any) {
	if r.Algorithm != to.Algorithm {
		into["algorithm"] = [2]any{r.Algorithm, to.Algorithm}
	}
	if r.Match != to.Match {
		into["match"] = [2]any{r.Match, to.Match}
	}
	if r.CaseInsensitive != to.CaseInsensitive {
		into["caseInsensitive"] = [2]any{r.CaseInsensitive, to.CaseInsensitive}
	}
}

const maxNameLen = 64

func normalizeName(name string) (string, error) {
	name = strings.TrimSpace(name)
	if n := utf8.RuneCountInString(name); n < 1 || n > maxNameLen {
		return "", errName
	}
	return name, nil
}

func quote(name string) string { return "“" + name + "”" }

// record writes an audit event in the current workspace; the summary is
// format filled with the caller's name and then args.
func (s *Service) record(ctx context.Context, q *sqlc.Queries, workspaceID int64, kind Kind, verb string, id int64, changes map[string][2]any, format string, args ...any) error {
	return audit.Record(ctx, q, audit.Event{
		WorkspaceID: workspaceID, Action: string(kind) + "." + verb, TargetType: string(kind), TargetID: strconv.FormatInt(id, 10),
		Summary: fmt.Sprintf(format, append([]any{audit.ActorFrom(ctx).Name}, args...)...), Changes: changes, At: s.now(),
	})
}

// renameSummary describes an update: a rename names both names, anything
// else names the item.
func renameSummary(kind Kind, from, to string) (string, []any) {
	if from != to {
		return kindSummaries[kind].rename, []any{from, to}
	}
	return kindSummaries[kind].update, []any{to}
}

func (s *Service) inTx(ctx context.Context, fn func(*sqlc.Queries) error) error {
	tx, err := s.db.BeginTx(ctx, nil) // BEGIN IMMEDIATE (see store.Open)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if err := fn(s.q.WithTx(tx)); err != nil {
		return err
	}
	return tx.Commit()
}

func boolInt(b bool) int64 {
	if b {
		return 1
	}
	return 0
}

// sortFields returns the stored sort key and the directory group of a name.
func sortFields(name string) (sortKey, initial string) {
	return pinyin.SortKey(name), pinyin.Initial(name)
}
