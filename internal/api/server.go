package api

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/abedegno/muesli/internal/adminui"
	"github.com/abedegno/muesli/internal/auth"
	"github.com/abedegno/muesli/internal/backup"
	"github.com/abedegno/muesli/internal/config"
	"github.com/abedegno/muesli/internal/crypto"
	"github.com/abedegno/muesli/internal/embed"
	"github.com/abedegno/muesli/internal/ratelimit"
	"github.com/abedegno/muesli/internal/storage"
	"github.com/abedegno/muesli/internal/store"
	"github.com/abedegno/muesli/internal/worker"
	"github.com/go-chi/chi/v5"
)

// maxBodyBytes is the maximum request body size for JSON API routes (1 MiB).
// Audio-upload routes (/audio-upload-url and /audio-uploaded) are excluded
// because they have their own guards.
const maxBodyBytes = 1 << 20 // 1 MiB

// limitBody is a middleware that caps the request body at maxBodyBytes.
// When a handler subsequently calls json.Decode (or any body read), it receives
// an error if the body exceeds the limit, causing the handler to return 413.
func limitBody(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		r.Body = http.MaxBytesReader(w, r.Body, maxBodyBytes)
		next.ServeHTTP(w, r)
	})
}

// Deps holds the server's collaborators.
type Deps struct {
	Store   *store.Store
	Storage storage.Provider
	Crypto  *crypto.Crypto
	Worker  *worker.Pool // may be nil in API-only tests
	Config  config.Config
	// Embedder generates query embeddings for semantic search. It may be nil,
	// in which case /api/search degrades to lexical-only results.
	Embedder embed.Embedder
	// Prober is used by /readyz to probe external deps. If nil, the default
	// HTTP HEAD prober (3-second timeout) is used.
	Prober DepProber
	// BackupRunner runs pg_dump for the admin backup endpoints and the
	// scheduled backup worker (BAK01). If nil, the default implementation
	// (backup.PgDumpRunner, which shells out to pg_dump) is used.
	BackupRunner backup.Runner
	// BackupListRunner runs pg_restore --list for the admin backup-verify
	// endpoint (ADM08). If nil, the default implementation
	// (backup.PgRestoreListRunner, which shells out to pg_restore) is used.
	BackupListRunner backup.ListRunner
	// PluginHealthProber is used by GET /api/admin/health to probe each
	// enabled plugin's /health endpoint server-side (ADM05). If nil, the
	// default implementation (plugin.New(url, token).Health, bounded by
	// pluginHealthProbeTimeout) is used. Mirrors the Prober seam above.
	PluginHealthProber PluginHealthProber
	// ChatGenerator is used by the chat-send handlers (CHT04) to call the
	// resolved default agent plugin's /generate endpoint for one chat turn.
	// If nil, the default implementation (plugin.New(url, token), built from
	// Store.DefaultPlugin(ctx, Crypto, model.PluginAgent)) is used. Mirrors
	// the PluginHealthProber seam above: tests inject a fake so chat-send
	// handler tests never make a real HTTP call to a plugin.
	ChatGenerator ChatGenerator
}

// Server wires dependencies to an HTTP router.
type Server struct {
	deps   Deps
	router chi.Router

	// resummarizeGuard prevents two concurrent /resummarize requests for the
	// same note id from racing (SUM03). Keyed by note id; reusable by any
	// future per-note summarize-trigger endpoint. Zero value is ready to use.
	resummarizeGuard inFlightGuard

	// retranscribeGuard prevents two concurrent /retranscribe requests for the
	// same note id from racing. Keyed by note id; zero value is ready to use.
	retranscribeGuard inFlightGuard

	// chatSendGuard prevents two concurrent message sends for the same
	// conversation id from racing (CHT04). Keyed by conversation id (a
	// create-and-send request is keyed by the newly-created conversation's
	// own id, since it's assigned before the guard is acquired). Zero value
	// is ready to use.
	chatSendGuard inFlightGuard

	// googleOAuthStates holds the short-lived state values for the Google
	// OAuth connect flow. Zero value is ready to use.
	googleOAuthStates googleOAuthStateStore
	// microsoftOAuthStates holds the short-lived state values for the
	// Microsoft OAuth connect flow. Zero value is ready to use.
	microsoftOAuthStates microsoftOAuthStateStore
}

func NewServer(deps Deps) *Server {
	s := &Server{deps: deps, router: chi.NewRouter()}
	s.routes()
	return s
}

func (s *Server) routes() {
	s.router.Use(securityHeaders)
	s.router.Use(corsMiddleware(s.deps.Config.AllowedOrigins))

	s.router.Get("/healthz", s.handleHealthz)
	s.router.Get("/readyz", s.handleReadyz)

	// Public JSON routes — body limit applied.
	s.router.With(limitBody).Post("/api/setup", s.handleSetup)
	s.router.Get("/api/setup/status", s.handleSetupStatus)
	s.router.With(limitBody, ratelimit.NewIPLimiter(s.deps.Config.RateLoginRPS, s.deps.Config.RateLoginBurst)).Post("/api/login", s.handleLogin)

	// Embedded admin SPA (static assets + SPA fallback). No auth at the
	// transport layer; the SPA authenticates against /api/* like any client.
	s.router.Handle("/admin", adminui.Handler())
	s.router.Handle("/admin/*", adminui.Handler())

	// Signed object uploads (signature-authenticated, not session-authenticated).
	if h, ok := s.deps.Storage.(interface{ UploadHandler() http.Handler }); ok {
		s.router.Handle("/_storage/*", h.UploadHandler())
	}

	// Authenticated JSON routes — body limit applied to all routes in this group.
	s.router.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.deps.Store, withUserID))
		r.Use(limitBody)
		r.Post("/api/tokens", s.handleCreateToken)
		r.Post("/api/notes", s.handleCreateNote)
		r.Get("/api/notes", s.handleListNotes)
		r.Get("/api/search", s.handleSearch)
		r.Get("/api/notes/{id}", s.handleGetNote)
		r.Delete("/api/notes/{id}", s.handleDeleteNote)
		r.Post("/api/notes/{id}/duplicate", s.handleDuplicateNote)
		r.Post("/api/notes/{id}/pin", s.handlePinNote)
		r.Delete("/api/notes/{id}/pin", s.handleUnpinNote)
		r.Post("/api/notes/{id}/event", s.handleSetNoteEvent)
		r.Delete("/api/notes/{id}/event", s.handleClearNoteEvent)
		r.Get("/api/notes/trash", s.handleListTrash)
		r.Post("/api/notes/{id}/restore", s.handleRestoreNote)
		r.Delete("/api/notes/{id}/permanent", s.handlePurgeNote)
		r.Get("/api/notes/{id}/full", s.handleGetNoteFull)
		r.Get("/api/notes/{id}/export", s.handleGetNoteExport)
		r.Post("/api/notes/{id}/resummarize", s.handleResummarize)
		r.Post("/api/notes/{id}/retranscribe", s.handleRetranscribe)
		r.Post("/api/notes/{id}/templates/{templateID}/summarize", s.handleSummarizeTemplate)
		r.Post("/api/notes/{id}/retry", s.handleRetryNote)
		r.Post("/api/notes/{id}/process-next", s.handleProcessNextNote)
		r.Put("/api/notes/{id}/body", s.handleUpdateNoteBody)
		r.Patch("/api/notes/{id}", s.handleUpdateNoteTitle)
		r.Get("/api/tags", s.handleListTags)
		r.Put("/api/tags/{id}", s.handleRenameTag)
		r.Delete("/api/tags/{id}", s.handleDeleteTag)
		r.Post("/api/notes/{id}/tags", s.handleAddNoteTag)
		r.Delete("/api/notes/{id}/tags", s.handleRemoveNoteTag) // ?name=<tag>

		r.Get("/api/smart-lists", s.handleListSmartLists)
		r.Get("/api/smart-lists/trash", s.handleListTrashedSmartLists)
		r.Post("/api/smart-lists", s.handleCreateSmartList)
		r.Put("/api/smart-lists/{id}", s.handleUpdateSmartList)
		r.Delete("/api/smart-lists/{id}", s.handleDeleteSmartList)
		r.Post("/api/smart-lists/{id}/restore", s.handleRestoreSmartList)
		r.Delete("/api/smart-lists/{id}/permanent", s.handlePurgeSmartList)

		r.Get("/api/folders", s.handleListFolders)
		r.Get("/api/folders/trash", s.handleListTrashedFolders)
		r.Post("/api/folders", s.handleCreateFolder)
		r.Put("/api/folders/{id}", s.handleUpdateFolder)
		r.Put("/api/folders/{id}/reorder", s.handleReorderFolder)
		r.Put("/api/folders/{folderID}/notes/{noteID}/reorder", s.handleReorderNoteInFolder)
		r.Delete("/api/folders/{id}", s.handleDeleteFolder)
		r.Post("/api/folders/{id}/restore", s.handleRestoreFolder)
		r.Delete("/api/folders/{id}/permanent", s.handlePurgeFolder)

		r.Get("/api/people", s.handleListPeople)
		r.Get("/api/people/{id}", s.handleGetPerson)
		r.Get("/api/people/{id}/notes", s.handleListPersonNotes)
		r.Post("/api/people/refresh", s.handleRefreshPeople)
		r.Get("/api/companies", s.handleListCompanies)
		r.Get("/api/companies/{id}", s.handleGetCompany)

		r.Get("/api/calendar/sources", s.handleListCalendarSources)
		r.Post("/api/calendar/sources", s.handleCreateCalendarSource)
		r.Post("/api/calendar/sources/{id}/select", s.handleSelectCalendarSource)
		r.Delete("/api/calendar/sources/{id}", s.handleDeleteCalendarSource)
		r.Post("/api/calendar/refresh", s.handleRefreshCalendar)
		r.Get("/api/calendar/events", s.handleListCalendarEvents)
		r.Get("/api/calendar/oauth/google/status", s.handleGoogleOAuthStatus)
		r.Get("/api/calendar/oauth/microsoft/status", s.handleMicrosoftOAuthStatus)

		r.Post("/api/conversations", s.handleCreateConversation)
		r.Get("/api/conversations", s.handleListConversations)
		r.Get("/api/conversations/{id}", s.handleGetConversation)
		r.Delete("/api/conversations/{id}", s.handleDeleteConversation)
		r.Get("/api/conversations/{id}/messages", s.handleListMessages)
		r.Post("/api/conversations/{id}/messages", s.handleSendMessage)

		r.Get("/api/templates", s.handleListTemplates)
		r.Post("/api/templates", s.handleCreateTemplate)
		r.Put("/api/templates/{id}", s.handleUpdateTemplate)
		r.Delete("/api/templates/{id}", s.handleDeleteTemplate)

		r.Post("/api/notes/{id}/folders", s.handleAddNoteFolder)
		r.Delete("/api/notes/{id}/folders/{folderID}", s.handleRemoveNoteFolder)

		r.Get("/api/notes/{id}/people", s.handleListNotePeople)
		r.Post("/api/notes/{id}/relink-speakers", s.handleRelinkNoteSpeakers)
		r.Get("/api/notes/{id}/speaker-aliases", s.handleListSpeakerAliases)
		r.Put("/api/notes/{id}/speaker-aliases/{label}", s.handleUpsertSpeakerAlias)
		r.Put("/api/notes/{id}/speaker-aliases/{label}/person", s.handleSetSpeakerAliasPerson)
		r.Delete("/api/notes/{id}/speaker-aliases/{label}", s.handleDeleteSpeakerAlias)

		r.Get("/api/notes/{id}/transcript/review", s.handleGetDiarizationReview)
		r.Post("/api/notes/{id}/transcript/review", s.handlePostDiarizationReview)

		r.Post("/api/audio/dedup-check", s.handleAudioDedupCheck)

		r.Get("/api/admin/jobs", s.handleListJobs)
		r.Post("/api/admin/jobs/{id}/retry", s.handleRetryJob)
		r.Post("/api/admin/jobs/{id}/cancel", s.handleCancelJob)
		r.Post("/api/admin/jobs/{id}/process-next", s.handleProcessNextJob)
		r.Get("/api/admin/notes/{id}/jobs", s.handleListNoteJobs)
		r.Get("/api/admin/webhook-deliveries", s.handleListWebhookDeliveries)
		r.Post("/api/admin/webhook-deliveries/{id}/retry", s.handleRetryWebhookDelivery)
		r.Get("/api/admin/plugins", s.handleListPlugins)
		r.Post("/api/admin/plugins", s.handleCreatePlugin)
		r.Patch("/api/admin/plugins/{id}", s.handlePatchPlugin)
		r.Delete("/api/admin/plugins/{id}", s.handleDeletePlugin)
		// Static convenience route must be registered before the parameterized route.
		r.Get("/api/admin/plugins/default-transcriber/status", s.handleGetDefaultTranscriberStatus)
		r.Get("/api/admin/plugins/{id}/status", s.handleGetPluginStatus)
		r.Post("/api/admin/plugins/{id}/health", s.handleCheckPluginHealth)

		// BAK01: in-app Postgres backup. No restore endpoint by design — restore
		// stays the documented manual pg_restore/psql procedure (docs/BACKUP.md).
		r.Post("/api/admin/backup", s.handleCreateBackup)
		r.Get("/api/admin/backups", s.handleListBackups)
		r.Get("/api/admin/backups/{filename}", s.handleDownloadBackup)
		r.Get("/api/admin/backups/{filename}/verify", s.handleVerifyBackup)

		// EMB01: embeddings status (read-only config + state).
		r.Get("/api/admin/embeddings", s.handleAdminEmbeddingsStatus)

		// EMB02: on-demand admin re-embed-all (live done/total + trigger),
		// distinct from EMB01's static config-status endpoint above.
		r.Get("/api/admin/embeddings/status", s.handleAdminReembedStatus)
		r.Post("/api/admin/embeddings/reembed", s.handleAdminReembedAll)

		// ADM05: aggregated admin health panel (server info, per-plugin
		// probes, job-queue depth, embedding coverage, storage disk usage).
		// Always 200 - per-section failures are captured inline.
		r.Get("/api/admin/health", s.handleAdminHealth)

		// ADM06: read-only, redacted effective-configuration view (MUESLI_*
		// fields only, secret-shaped values collapsed to "(set)"/"(unset)").
		r.Get("/api/admin/config", s.handleAdminConfig)
	})

	// OAuth browser flows are the one place the system browser needs a
	// one-time token handoff; the callback validates the short-lived state.
	s.router.Get("/api/calendar/oauth/google/start", s.handleGoogleOAuthStart)
	s.router.Get("/api/calendar/oauth/google/callback", s.handleGoogleOAuthCallback)
	s.router.Get("/api/calendar/oauth/microsoft/start", s.handleMicrosoftOAuthStart)
	s.router.Get("/api/calendar/oauth/microsoft/callback", s.handleMicrosoftOAuthCallback)

	// Authenticated audio-upload routes — body limit intentionally excluded.
	// These routes have their own size guards (presign URL and object verification).
	s.router.Group(func(r chi.Router) {
		r.Use(auth.Middleware(s.deps.Store, withUserID))
		r.With(ratelimit.NewIPLimiter(s.deps.Config.RateUploadRPS, s.deps.Config.RateUploadBurst)).Post("/api/notes/import", s.handleImportNote)
		r.Get("/api/notes/{id}/audio-url", s.handleAudioDownloadURL)
		r.With(ratelimit.NewIPLimiter(s.deps.Config.RateUploadRPS, s.deps.Config.RateUploadBurst)).Post("/api/notes/{id}/audio-upload-url", s.handleAudioUploadURL)
		r.Post("/api/notes/{id}/audio-uploaded", s.handleAudioUploaded)
	})
}

// Handler exposes the router for tests and embedding.
func (s *Server) Handler() http.Handler { return s.router }

// Run starts the HTTP server and blocks until ctx is cancelled, then shuts down gracefully.
func (s *Server) Run(ctx context.Context, addr string) error {
	httpSrv := &http.Server{Addr: addr, Handler: s.router}
	errCh := make(chan error, 1)
	go func() { errCh <- httpSrv.ListenAndServe() }()

	select {
	case err := <-errCh:
		if errors.Is(err, http.ErrServerClosed) {
			return nil
		}
		return err
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return httpSrv.Shutdown(shutdownCtx)
	}
}
