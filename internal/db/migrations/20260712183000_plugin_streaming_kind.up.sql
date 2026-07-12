ALTER TABLE plugins DROP CONSTRAINT IF EXISTS plugins_kind_check;

ALTER TABLE plugins
  ADD CONSTRAINT plugins_kind_check CHECK (kind IN ('transcriber', 'streaming-transcriber', 'agent'));
