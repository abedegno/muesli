-- cross_analysis_sources (up)
-- Persists the citation sources for a cross-meeting analysis assistant
-- message (issue #765): one row per (assistant message, citation number).
-- note_id is nullable and ON DELETE SET NULL so a later note deletion
-- retains the historical assistant text with the citation's navigation
-- target simply gone (see the accepted spec's "Persistence" section) --
-- never cascades and removes the row itself. No phase constraint here:
-- templates.phase='cross' is enforced at the application layer (see
-- internal/store/templates.go), not the database.
CREATE TABLE message_sources (
    message_id             UUID NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    citation_number         INTEGER NOT NULL,
    note_id                 UUID REFERENCES notes(id) ON DELETE SET NULL,
    transcript_generation   INTEGER NOT NULL,
    segment_index           INTEGER NOT NULL,
    timestamp_ms            INTEGER NOT NULL,
    snippet                 TEXT NOT NULL,
    PRIMARY KEY (message_id, citation_number)
);

CREATE INDEX idx_message_sources_note_id ON message_sources (note_id);
