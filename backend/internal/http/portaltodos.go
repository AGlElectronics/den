package http

import (
	"net/http"
	"net/url"
	"sort"
	"sync"
	"time"

	"github.com/AGlElectronics/den/backend/internal/auth"
	"github.com/AGlElectronics/den/backend/internal/httperror"
)

// GET /api/v1/portal/todos: the signed-in person's to-dos from every module
// they can use. Den calls each module's GET /api/portal/todos itself, with
// the module's pairing credential and the person's issuer and subject in
// headers, so no user token ever leaves Den. Modules that do not answer in
// five seconds, answer with an error or do not implement the route are
// reported in "modules" and otherwise skipped.

const maxTodosPerModule = 50

// Headers Den sends with a to-do request.
const (
	headerUserIssuer  = "X-Den-User-Issuer"
	headerUserSubject = "X-Den-User-Subject"
)

var todoSeverities = map[string]bool{"info": true, "warning": true, "danger": true}

type portalTodo struct {
	ID         string  `json:"id"`
	Title      string  `json:"title"`
	Detail     string  `json:"detail,omitempty"`
	DueAt      *string `json:"due_at"`
	URL        string  `json:"url"`
	Severity   string  `json:"severity"`
	ModuleSlug string  `json:"module_slug"`
	ModuleName string  `json:"module_name"`
}

type todoModuleStatus struct {
	Slug        string `json:"slug"`
	DisplayName string `json:"display_name"`
	// Status is ok, unsupported (404), not_paired_for_todos (Den cannot
	// present the credential; pair again) or error.
	Status string `json:"status"`
	Count  int    `json:"count"`
}

func validTodoURL(raw string) bool {
	u, err := url.Parse(raw)
	return err == nil && (u.Scheme == "https" || u.Scheme == "http") && u.Host != ""
}

func (c *moduleCaller) portalTodos(w http.ResponseWriter, r *http.Request) {
	u, ok := auth.FromContext(r.Context())
	if !ok {
		httperror.Render(w, r, httperror.Unauthorized("not signed in"))
		return
	}
	out := map[string]any{"enabled": len(c.key) > 0, "items": []portalTodo{}, "modules": []todoModuleStatus{}}
	if len(c.key) == 0 {
		writeJSON(w, http.StatusOK, out)
		return
	}
	mods, err := c.visibleModules(r.Context(), u)
	if err != nil {
		httperror.Render(w, r, httperror.Internal(err))
		return
	}

	statuses := make([]todoModuleStatus, len(mods))
	results := make([][]portalTodo, len(mods))
	var wg sync.WaitGroup
	for i, m := range mods {
		statuses[i] = todoModuleStatus{Slug: m.Slug, DisplayName: m.DisplayName, Status: "error"}
		wg.Add(1)
		go func() {
			defer wg.Done()
			addr, err := c.callAddr(m)
			if err != nil {
				return
			}
			cred, err := c.credential(r.Context(), m.ID)
			if err != nil {
				return
			}
			if cred == "" {
				statuses[i].Status = "not_paired_for_todos"
				return
			}
			var body struct {
				Items []portalTodo `json:"items"`
			}
			hdr := http.Header{}
			hdr.Set("Authorization", "Bearer "+cred)
			hdr.Set(headerUserIssuer, u.Issuer)
			hdr.Set(headerUserSubject, u.Subject)
			code, err := c.getJSON(r.Context(), addr+"/api/portal/todos", hdr, &body)
			if code == http.StatusNotFound || code == http.StatusNotImplemented {
				statuses[i].Status = "unsupported"
				return
			}
			if err != nil {
				c.log.Debug().Err(err).Str("module", m.Slug).Msg("portal to-dos failed")
				return
			}
			items := make([]portalTodo, 0, len(body.Items))
			for _, it := range body.Items {
				if it.ID == "" || it.Title == "" || !validTodoURL(it.URL) {
					continue
				}
				if !todoSeverities[it.Severity] {
					it.Severity = "info"
				}
				if it.DueAt != nil {
					if _, err := time.Parse(time.RFC3339, *it.DueAt); err != nil {
						it.DueAt = nil
					}
				}
				it.ModuleSlug, it.ModuleName = m.Slug, m.DisplayName
				items = append(items, it)
				if len(items) == maxTodosPerModule {
					break
				}
			}
			results[i] = items
			statuses[i].Status, statuses[i].Count = "ok", len(items)
		}()
	}
	wg.Wait()

	all := make([]portalTodo, 0)
	responded := 0
	for i, items := range results {
		if statuses[i].Status == "ok" {
			responded++
		}
		all = append(all, items...)
	}
	rank := map[string]int{"danger": 0, "warning": 1, "info": 2}
	due := func(t portalTodo) time.Time {
		if t.DueAt == nil {
			return time.Date(9999, 1, 1, 0, 0, 0, 0, time.UTC)
		}
		d, _ := time.Parse(time.RFC3339, *t.DueAt)
		return d
	}
	sort.SliceStable(all, func(i, j int) bool {
		di, dj := due(all[i]), due(all[j])
		if !di.Equal(dj) {
			return di.Before(dj)
		}
		return rank[all[i].Severity] < rank[all[j].Severity]
	})
	out["items"], out["modules"], out["responded"] = all, statuses, responded
	writeJSON(w, http.StatusOK, out)
}
