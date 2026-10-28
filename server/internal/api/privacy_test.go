package api

import (
	"net/http"
	"testing"
)

// v1.8.0: a family member's private documents. In home(), alice owns the
// space, bob edits and carol only views. Mum is bob's own account here.

type privateHome struct {
	alice, bob, carol *client
	homeID, mum       float64
}

func newPrivateHome(t *testing.T, e *testEnv) privateHome {
	t.Helper()
	alice, bob, carol, homeID := home(t, e)
	mum := alice.mustDo(http.MethodPost, "/api/v1/persons", map[string]any{"name": "妈妈"}, http.StatusCreated).body["id"].(float64)
	// nobody looks after mum yet: anyone who may write links her account
	alice.mustDo(http.MethodPut, personAccess(mum, userID(bob)), map[string]any{"level": "self"}, http.StatusOK)
	return privateHome{alice, bob, carol, homeID, mum}
}

func personAccess(person, user float64) string {
	return "/api/v1/persons/" + itoa(person) + "/access/" + itoa(user)
}

// uploadFor uploads a file for a person and returns the document.
func (c *client) uploadFor(name, content string, person float64, fields map[string]string) float64 {
	c.env.t.Helper()
	f := map[string]string{"personId": itoa(person)}
	for k, v := range fields {
		f[k] = v
	}
	j := c.waitJob(c.upload(name, content, f).body["jobId"].(float64))
	if j["status"] != "succeeded" {
		c.env.t.Fatalf("upload %s: %v", name, j)
	}
	return j["documentId"].(float64)
}

func ids(items []any) []float64 {
	var out []float64
	for _, it := range items {
		out = append(out, it.(map[string]any)["id"].(float64))
	}
	return out
}

// login signs an existing account in.
func (e *testEnv) login(name, password string) *client {
	c := e.anon()
	c.mustDo(http.MethodPost, "/api/v1/auth/login", map[string]any{"login": name, "password": password}, http.StatusOK)
	return c
}
