package api_test

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
)

func TestSetupRaceCreatesOnlyOneAccount(t *testing.T) {
	t.Parallel()
	srv, st := newTestServer(t)
	ctx := context.Background()

	if _, err := st.Pool().Exec(ctx, `
CREATE OR REPLACE FUNCTION setup_users_insert_sleep() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
	PERFORM pg_sleep(0.2);
	RETURN NEW;
END;
$$;
`); err != nil {
		t.Fatalf("create sleep function: %v", err)
	}
	if _, err := st.Pool().Exec(ctx, `
CREATE TRIGGER setup_users_insert_sleep
BEFORE INSERT ON users
FOR EACH ROW
EXECUTE FUNCTION setup_users_insert_sleep();
`); err != nil {
		t.Fatalf("create sleep trigger: %v", err)
	}

	const n = 10
	results := make([]int, n)
	start := make(chan struct{})
	var ready sync.WaitGroup
	var done sync.WaitGroup
	ready.Add(n)
	done.Add(n)

	for i := 0; i < n; i++ {
		go func(i int) {
			defer done.Done()
			ready.Done()
			<-start
			body := fmt.Sprintf(`{"email":"attacker%d@example.com","password":"password123"}`, i)
			req := httptest.NewRequest(http.MethodPost, "/api/setup", strings.NewReader(body))
			req.Header.Set("Content-Type", "application/json")
			rec := httptest.NewRecorder()
			srv.Handler().ServeHTTP(rec, req)
			results[i] = rec.Code
		}(i)
	}

	ready.Wait()
	close(start)
	done.Wait()

	created := 0
	for i, code := range results {
		switch code {
		case http.StatusCreated:
			created++
		case http.StatusConflict:
		default:
			t.Fatalf("request %d returned %d, want 201 or 409", i, code)
		}
	}
	if created != 1 {
		t.Fatalf("expected exactly 1 of %d concurrent /api/setup requests to succeed, got %d", n, created)
	}

	if nUsers, err := st.CountUsers(ctx); err != nil {
		t.Fatalf("CountUsers: %v", err)
	} else if nUsers != 1 {
		t.Fatalf("user count = %d, want 1", nUsers)
	}
}
