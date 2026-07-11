-- Allow the new 'embed' job type (semantic-search embedding generation).
ALTER TABLE jobs DROP CONSTRAINT jobs_type_check;
ALTER TABLE jobs ADD CONSTRAINT jobs_type_check
    CHECK (type IN ('transcribe','summarize','embed'));
