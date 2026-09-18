-- live_template_outputs (up)
-- Issue #764: live in-meeting prompts. Persists one compact execution/output
-- row per (note, template, live stream), the durable job fields a live
-- generation job carries, note-scoped SSE subscriber leases, and a per-note
-- capacity lock row used to serialize lease admission.

CREATE TABLE live_template_outputs (
    id                          uuid PRIMARY KEY DEFAULT gen_random_uuid(),
    note_id                     uuid NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    template_id                 uuid NOT NULL REFERENCES templates(id) ON DELETE CASCADE,
    stream_id                   text NOT NULL,
    owner_id                    uuid NOT NULL,
    desired_revision            integer NOT NULL DEFAULT 0,
    rendered_revision           integer NOT NULL DEFAULT 0,
    event_version               integer NOT NULL DEFAULT 0,
    status                      text NOT NULL DEFAULT 'pending'
                                CHECK (status IN ('pending','running','ready','failed')),
    client_visible              boolean NOT NULL DEFAULT TRUE,
    cancellation_requested_at   timestamptz,
    sections                    jsonb NOT NULL DEFAULT '[]'::jsonb,
    agent_plugin                text,
    model                       text,
    error_code                  text,
    last_started_at             timestamptz,
    last_demand_at              timestamptz NOT NULL DEFAULT now(),
    updated_at                  timestamptz NOT NULL DEFAULT now(),
    ended_at                    timestamptz,
    active_job_id               uuid,
    UNIQUE (note_id, template_id, stream_id)
);

CREATE INDEX live_template_outputs_note_idx ON live_template_outputs(note_id) WHERE client_visible;
CREATE INDEX live_template_outputs_recovery_idx ON live_template_outputs(last_demand_at);
CREATE INDEX live_template_outputs_owner_idx ON live_template_outputs(owner_id);

-- Durable live-generation job fields. NULL for every non-live job type, so
-- every pre-existing INSERT into jobs continues to compile and behave
-- unchanged (mirrors calendar_event_id/brief_id's "unset" convention from
-- the pre_meeting_briefs migration).
ALTER TABLE jobs ADD COLUMN template_id uuid;
ALTER TABLE jobs ADD COLUMN stream_id text;
ALTER TABLE jobs ADD COLUMN live_output_id uuid REFERENCES live_template_outputs(id) ON DELETE CASCADE;
ALTER TABLE jobs ADD COLUMN target_revision integer;

-- At most one pending/running live_generate job per output row.
CREATE UNIQUE INDEX jobs_live_active_uniq ON jobs(live_output_id)
    WHERE type = 'live_generate' AND status IN ('pending','running');

CREATE INDEX jobs_live_output_idx ON jobs(live_output_id) WHERE live_output_id IS NOT NULL;

-- Note-scoped SSE subscriber leases. No transcript or output content lives
-- here -- only enough to enforce the global-per-note 40-subscriber cap
-- across hosted API processes.
CREATE TABLE live_note_subscriptions (
    connection_id   text PRIMARY KEY,
    note_id         uuid NOT NULL REFERENCES notes(id) ON DELETE CASCADE,
    owner_id        uuid NOT NULL,
    lease_expires_at timestamptz NOT NULL
);

CREATE INDEX live_note_subscriptions_note_idx ON live_note_subscriptions(note_id, lease_expires_at);

-- One row per note that has ever accepted a live-prompts subscription,
-- locked with SELECT ... FOR UPDATE to serialize capacity admission
-- (count-then-insert) across hosted API processes sharing one database.
CREATE TABLE live_note_capacity (
    note_id     uuid PRIMARY KEY REFERENCES notes(id) ON DELETE CASCADE,
    updated_at  timestamptz NOT NULL DEFAULT now()
);

-- Extend the generalized job type/target constraints from #763 (pre_meeting_briefs)
-- to admit live_generate, which targets a note (like transcribe/summarize/embed)
-- but additionally always carries template_id/stream_id/live_output_id together.
ALTER TABLE jobs DROP CONSTRAINT jobs_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_type_check
    CHECK (type IN ('transcribe','summarize','embed','pre_generate','live_generate'));

ALTER TABLE jobs ADD CONSTRAINT jobs_live_fields_check CHECK (
    (type <> 'live_generate') OR
    (template_id IS NOT NULL AND stream_id IS NOT NULL AND live_output_id IS NOT NULL)
);
