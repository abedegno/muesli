package store_test

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/model"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/testutil"
)

func testCrypto(t *testing.T) *crypto.Crypto {
	t.Helper()
	// 32 zero bytes, base64. Fine for tests; never use a fixed key in prod.
	c, err := crypto.New("AAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAAA=")
	if err != nil {
		t.Fatalf("crypto: %v", err)
	}
	return c
}

func TestPluginsCRUDAndEncryption(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr := testCrypto(t)
	ctx := context.Background()

	p, err := st.CreatePlugin(ctx, cr, model.Plugin{
		Kind:        model.PluginTranscriber,
		Name:        "whisper",
		EndpointURL: "http://transcriber:9000",
		Token:       "plugin-token-1",
		Config:      json.RawMessage(`{"api_key":"sk-secret"}`),
		Enabled:     true,
	})
	if err != nil {
		t.Fatalf("create: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected an id")
	}

	// The config is encrypted at rest: raw DB read must NOT contain the secret.
	var rawConfig string
	if err := pool.QueryRow(ctx, `SELECT config::text FROM plugins WHERE id=$1`, p.ID).Scan(&rawConfig); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if contains(rawConfig, "sk-secret") {
		t.Fatalf("plaintext secret leaked into DB: %s", rawConfig)
	}

	// Get decrypts the config and returns the token.
	got, err := st.GetPlugin(ctx, cr, p.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if string(got.Config) != `{"api_key":"sk-secret"}` {
		t.Fatalf("decrypted config = %s", got.Config)
	}
	if got.Token != "plugin-token-1" {
		t.Fatalf("token = %q", got.Token)
	}

	// Update changes endpoint + config.
	got.EndpointURL = "http://transcriber:9999"
	got.Config = json.RawMessage(`{"api_key":"sk-rotated"}`)
	if err := st.UpdatePlugin(ctx, cr, got); err != nil {
		t.Fatalf("update: %v", err)
	}
	reread, _ := st.GetPlugin(ctx, cr, p.ID)
	if reread.EndpointURL != "http://transcriber:9999" || string(reread.Config) != `{"api_key":"sk-rotated"}` {
		t.Fatalf("update not applied: %+v", reread)
	}

	// List (admin view) redacts config and omits the token.
	list, err := st.ListPlugins(ctx, cr)
	if err != nil || len(list) != 1 {
		t.Fatalf("list len=%d err=%v", len(list), err)
	}
	if list[0].Token != "" {
		t.Fatal("list must not expose the token")
	}

	// Delete.
	if err := st.DeletePlugin(ctx, p.ID); err != nil {
		t.Fatalf("delete: %v", err)
	}
	if _, err := st.GetPlugin(ctx, cr, p.ID); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound after delete, got %v", err)
	}
}

func TestDefaultPluginPerKind(t *testing.T) {
	t.Parallel()
	st := store.New(testutil.NewPool(t))
	cr := testCrypto(t)
	ctx := context.Background()

	a, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "a", EndpointURL: "http://a", Enabled: true, Config: json.RawMessage(`{}`)})
	b, _ := st.CreatePlugin(ctx, cr, model.Plugin{Kind: model.PluginAgent, Name: "b", EndpointURL: "http://b", Enabled: true, Config: json.RawMessage(`{}`)})

	if err := st.SetDefaultPlugin(ctx, a.ID); err != nil {
		t.Fatalf("set default a: %v", err)
	}
	def, err := st.DefaultPlugin(ctx, cr, model.PluginAgent)
	if err != nil || def.ID != a.ID {
		t.Fatalf("default = %+v err=%v", def, err)
	}

	// Setting b default unsets a (exactly one default per kind).
	if err := st.SetDefaultPlugin(ctx, b.ID); err != nil {
		t.Fatalf("set default b: %v", err)
	}
	def, _ = st.DefaultPlugin(ctx, cr, model.PluginAgent)
	if def.ID != b.ID {
		t.Fatalf("default should be b, got %s", def.ID)
	}

	// No default for a kind with no plugins.
	if _, err := st.DefaultPlugin(ctx, cr, model.PluginTranscriber); err != store.ErrNotFound {
		t.Fatalf("want ErrNotFound, got %v", err)
	}
}

func TestEnsureDefaultPluginIdempotentAndEncrypted(t *testing.T) {
	t.Parallel()
	pool := testutil.NewPool(t)
	st := store.New(pool)
	cr := testCrypto(t)
	ctx := context.Background()

	// First call for each kind creates the plugin and marks it default.
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginTranscriber, "Default transcriber",
		"http://transcriber:9000", "t-secret-token", `{"api_key":"sk-trans"}`); err != nil {
		t.Fatalf("ensure transcriber: %v", err)
	}
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "Default agent",
		"http://agent:9100", "a-secret-token", `{"api_key":"sk-agent"}`); err != nil {
		t.Fatalf("ensure agent: %v", err)
	}

	tDef, err := st.DefaultPlugin(ctx, cr, model.PluginTranscriber)
	if err != nil {
		t.Fatalf("default transcriber: %v", err)
	}
	if !tDef.IsDefault || !tDef.Enabled {
		t.Fatalf("transcriber not default/enabled: %+v", tDef)
	}
	if tDef.EndpointURL != "http://transcriber:9000" || tDef.Token != "t-secret-token" || string(tDef.Config) != `{"api_key":"sk-trans"}` {
		t.Fatalf("transcriber fields = %+v", tDef)
	}

	aDef, err := st.DefaultPlugin(ctx, cr, model.PluginAgent)
	if err != nil {
		t.Fatalf("default agent: %v", err)
	}
	if !aDef.IsDefault || !aDef.Enabled {
		t.Fatalf("agent not default/enabled: %+v", aDef)
	}

	// Secrets must be encrypted at rest (config + token).
	var rawConfig, rawToken string
	if err := pool.QueryRow(ctx, `SELECT config::text, token FROM plugins WHERE id=$1`, tDef.ID).Scan(&rawConfig, &rawToken); err != nil {
		t.Fatalf("raw read: %v", err)
	}
	if contains(rawConfig, "sk-trans") {
		t.Fatalf("plaintext config leaked: %s", rawConfig)
	}

	// Re-run with changed url/token/config: must update in place, not duplicate.
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginTranscriber, "Default transcriber",
		"http://transcriber:9999", "t-rotated", `{"api_key":"sk-rotated"}`); err != nil {
		t.Fatalf("re-ensure transcriber: %v", err)
	}

	list, err := st.ListPlugins(ctx, cr)
	if err != nil {
		t.Fatalf("list: %v", err)
	}
	// Exactly one transcriber and one agent.
	var nTrans, nAgent int
	for _, p := range list {
		switch p.Kind {
		case model.PluginTranscriber:
			nTrans++
		case model.PluginAgent:
			nAgent++
		}
	}
	if nTrans != 1 || nAgent != 1 {
		t.Fatalf("expected exactly 1 of each kind, got trans=%d agent=%d", nTrans, nAgent)
	}

	// Updated url/token/config reflected, still the default.
	tDef2, err := st.DefaultPlugin(ctx, cr, model.PluginTranscriber)
	if err != nil {
		t.Fatalf("default transcriber after re-run: %v", err)
	}
	if tDef2.ID != tDef.ID {
		t.Fatalf("re-run created a new plugin: %s != %s", tDef2.ID, tDef.ID)
	}
	if tDef2.EndpointURL != "http://transcriber:9999" || tDef2.Token != "t-rotated" || string(tDef2.Config) != `{"api_key":"sk-rotated"}` {
		t.Fatalf("re-run did not update fields: %+v", tDef2)
	}

	// Empty configJSON defaults to {} and is accepted.
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "Default agent",
		"http://agent:9100", "a-secret-token", ""); err != nil {
		t.Fatalf("ensure agent empty config: %v", err)
	}
	aDef2, _ := st.DefaultPlugin(ctx, cr, model.PluginAgent)
	if string(aDef2.Config) != `{}` {
		t.Fatalf("empty config should default to {}, got %s", aDef2.Config)
	}

	// Invalid JSON is rejected.
	if err := st.EnsureDefaultPlugin(ctx, cr, model.PluginAgent, "Default agent",
		"http://agent:9100", "a-secret-token", `{not json`); err == nil {
		t.Fatal("expected error for invalid configJSON")
	}
}

func contains(s, sub string) bool {
	return len(s) >= len(sub) && (func() bool {
		for i := 0; i+len(sub) <= len(s); i++ {
			if s[i:i+len(sub)] == sub {
				return true
			}
		}
		return false
	})()
}
