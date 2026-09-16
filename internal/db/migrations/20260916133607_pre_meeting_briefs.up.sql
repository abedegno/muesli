-- pre_meeting_briefs (up)
--
-- Generalizes jobs from a mandatory note target to an explicit note/calendar-event
-- target union, and adds the event_briefs table that stores durable, owner-scoped
-- (via calendar_events) generated pre-meeting briefs. See issue #763.

-- 1. Jobs: note_id becomes optional; add typed nullable calendar-event / brief
--    target columns. brief_id deliberately carries NO foreign key -- deleting a
--    brief must not block or cascade-delete a running job for it; guarded
--    publication (id + generation) is what makes that safe.
ALTER TABLE jobs
    ALTER COLUMN note_id DROP NOT NULL,
    ADD COLUMN calendar_event_id UUID REFERENCES calendar_events(id) ON DELETE CASCADE,
    ADD COLUMN brief_id UUID,
    ADD COLUMN brief_generation INTEGER;

ALTER TABLE jobs DROP CONSTRAINT jobs_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_type_check
    CHECK (type IN ('transcribe','summarize','embed','pre_generate'));

-- Exactly one target: a note job carries note_id only, an event job carries
-- calendar_event_id only.
ALTER TABLE jobs ADD CONSTRAINT jobs_target_xor_check CHECK (
    (note_id IS NOT NULL AND calendar_event_id IS NULL) OR
    (note_id IS NULL AND calendar_event_id IS NOT NULL)
);

-- Existing job types continue to require a note; the new pre_generate type
-- requires an event (and, in application code, always sets brief_id/generation
-- too -- enforced there rather than here so a defensive manual insert can never
-- violate this check by omitting brief bookkeeping alone).
ALTER TABLE jobs ADD CONSTRAINT jobs_target_type_check CHECK (
    (type = 'pre_generate' AND calendar_event_id IS NOT NULL) OR
    (type <> 'pre_generate' AND note_id IS NOT NULL)
);

-- brief_generation is only meaningful alongside a brief_id.
ALTER TABLE jobs ADD CONSTRAINT jobs_brief_fields_check CHECK (
    (brief_id IS NULL AND brief_generation IS NULL) OR
    (brief_id IS NOT NULL AND brief_generation IS NOT NULL)
);

-- Dedicated indexed columns (not JSON payload inspection) back both brief
-- cleanup ("delete queued pre jobs for this brief") and global admin
-- monitoring of event-targeted jobs.
CREATE INDEX jobs_calendar_event_idx ON jobs (calendar_event_id) WHERE calendar_event_id IS NOT NULL;
CREATE INDEX jobs_brief_idx ON jobs (brief_id) WHERE brief_id IS NOT NULL;

-- The race guard for overlapping reconcilers: at most one active (pending or
-- running) pre_generate job may exist for a given (brief, generation) pair.
-- Terminal (done/failed/cancelled) rows, and rows for a superseded
-- generation, are exempt so history never blocks new work.
CREATE UNIQUE INDEX jobs_pre_generate_active_unique_idx
    ON jobs (brief_id, brief_generation)
    WHERE brief_id IS NOT NULL AND status IN ('pending','running');

-- 2. event_briefs: one current row per (event, template) pair. Owner isolation
--    is derived through the event_id join to calendar_events, never stored
--    directly here (see model.EventBrief / store queries).
CREATE TABLE event_briefs (
    id            UUID PRIMARY KEY,
    event_id      UUID NOT NULL REFERENCES calendar_events(id) ON DELETE CASCADE,
    template_id   UUID NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    template_name TEXT NOT NULL,
    input_hash    TEXT,
    generation    INTEGER NOT NULL DEFAULT 1 CHECK (generation > 0),
    status        TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending','ready','failed')),
    agent_plugin  TEXT NOT NULL DEFAULT '',
    model         TEXT NOT NULL DEFAULT '',
    sections      JSONB NOT NULL DEFAULT '[]'::jsonb,
    created_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE (event_id, template_id)
);
CREATE INDEX event_briefs_event_idx ON event_briefs (event_id);
CREATE INDEX event_briefs_template_idx ON event_briefs (template_id);
