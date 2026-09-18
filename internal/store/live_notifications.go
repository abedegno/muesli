package store

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

// LiveNoteUpdatesChannel is the PostgreSQL LISTEN/NOTIFY channel carrying
// only a note id (issue #764). It is a wake-up only -- the database is
// authoritative, so every handler always rereads owner-scoped rows rather
// than trusting the payload for anything beyond "check this note".
const LiveNoteUpdatesChannel = "live_note_updates"

// notifyLiveNoteTx sends a note-ID-only notification on the caller's
// transaction (which may itself be a savepoint -- PostgreSQL defers actual
// delivery until the outermost transaction commits, and a savepoint that is
// released rather than rolled back still delivers its NOTIFYs then). Safe to
// call redundantly; listeners coalesce.
func notifyLiveNoteTx(ctx context.Context, tx pgx.Tx, noteID string) error {
	_, err := tx.Exec(ctx, `SELECT pg_notify($1, $2)`, LiveNoteUpdatesChannel, noteID)
	return err
}

// LiveNoteListener is a reconnecting PostgreSQL LISTEN client for
// LiveNoteUpdatesChannel. It owns one dedicated pool connection (LISTEN is
// per-connection) and fans out each notified note id to Notify's callback.
// The database remains authoritative: a dropped notification is repaired by
// the API's own periodic heartbeat reread, never by this listener retrying
// delivery.
type LiveNoteListener struct {
	pool   *pgxpool.Pool
	notify func(noteID string)
	cancel context.CancelFunc
	done   chan struct{}
}

// NewLiveNoteListener starts listening in the background. notify is called
// (from the listener's own goroutine) once per received notification; it
// must not block.
func NewLiveNoteListener(pool *pgxpool.Pool, notify func(noteID string)) *LiveNoteListener {
	ctx, cancel := context.WithCancel(context.Background())
	l := &LiveNoteListener{pool: pool, notify: notify, cancel: cancel, done: make(chan struct{})}
	go l.run(ctx)
	return l
}

// Close stops the listener and releases its connection.
func (l *LiveNoteListener) Close() {
	l.cancel()
	<-l.done
}

func (l *LiveNoteListener) run(ctx context.Context) {
	defer close(l.done)
	backoff := time.Second
	const maxBackoff = 30 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		default:
		}
		if err := l.listenOnce(ctx); err != nil {
			slog.ErrorContext(ctx, "live note listener: connection lost, reconnecting", "error", err, "backoff", backoff)
			select {
			case <-ctx.Done():
				return
			case <-time.After(backoff):
			}
			backoff *= 2
			if backoff > maxBackoff {
				backoff = maxBackoff
			}
			continue
		}
		backoff = time.Second
	}
}

func (l *LiveNoteListener) listenOnce(ctx context.Context) error {
	conn, err := l.pool.Acquire(ctx)
	if err != nil {
		return err
	}
	defer conn.Release()

	if _, err := conn.Exec(ctx, `LISTEN `+LiveNoteUpdatesChannel); err != nil {
		return err
	}
	for {
		n, err := conn.Conn().WaitForNotification(ctx)
		if err != nil {
			return err
		}
		if l.notify != nil {
			l.notify(n.Payload)
		}
	}
}
