-- Add diarization review lifecycle state to transcripts
-- Valid values: 'pending' | 'in_review' | 'completed'
-- Note: confidence was added to transcript_segments by 20260702085938_segment_diarization_confidence.
ALTER TABLE transcripts ADD COLUMN review_state TEXT NOT NULL DEFAULT 'pending';
