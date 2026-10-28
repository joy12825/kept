package notify

import (
	"context"
	"strings"

	"kept/server/internal/i18n"
)

// The language of an email (v1.10.2). Mail the server writes itself
// (reminders, quota warnings, notices about emergency access and private
// documents) is written in Chinese like the rest of the code and put into
// each recipient's language when sent: the language their account is set
// to, or, when it follows the browser, the one they last used the web or
// the app in. Someone the server does not know gets fallback.
//
// What a person wrote (a message sent with documents, a workflow's email)
// is sent as it is.

// LangFor is the language to write to the address in.
func (s *Service) LangFor(ctx context.Context, email string, fallback i18n.Lang) i18n.Lang {
	r, err := s.q.GetUserLangByEmail(ctx, email)
	if err != nil {
		return fallback
	}
	for _, l := range []string{r.Locale, r.SeenLang} {
		switch l {
		case string(i18n.Zh):
			return i18n.Zh
		case string(i18n.En):
			return i18n.En
		}
	}
	return fallback
}

// localize puts a message the server wrote into l, line by line: each line
// of a mail is one sentence of the catalog (or a link, kept as it is).
func localize(l i18n.Lang, text string) string {
	if l == i18n.Zh {
		return text
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		// keep the indentation and bullet the line starts with
		body := strings.TrimLeft(line, " ")
		lines[i] = line[:len(line)-len(body)] + i18n.Message(l, body)
	}
	return strings.Join(lines, "\n")
}

// byLang groups addresses by the language to write to them in, keeping
// their order.
func (s *Service) byLang(ctx context.Context, to []string) ([]i18n.Lang, map[i18n.Lang][]string) {
	var order []i18n.Lang
	groups := map[i18n.Lang][]string{}
	for _, a := range to {
		l := s.LangFor(ctx, a, i18n.Zh)
		if _, ok := groups[l]; !ok {
			order = append(order, l)
		}
		groups[l] = append(groups[l], a)
	}
	return order, groups
}
