package store

import (
	"context"
	"errors"
	"github.com/abedegno/muesli/internal/model"
)

var ErrCaptureActive = errors.New("this recording is still active on another device; wait for it to stop or expire")

// Capture leases belong to a specific capture, not just a user or note. A
// stalled client cannot later stop a replacement capture belonging to that user.
func (s *Store) ClaimCapture(ctx context.Context, owner, note, capture string) (*model.Note, error) {
	ct, err := s.pool.Exec(ctx, `UPDATE notes SET status='recording',
 started_at=CASE WHEN status='recording' AND capture_id=$3::uuid THEN started_at ELSE now() END,
 ended_at=NULL, capture_id=$3::uuid, capture_expires_at=now()+interval '90 seconds',updated_at=now()
 WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL AND audio_object_key IS NULL
 AND (status='draft' OR (status='recording' AND (capture_id=$3::uuid OR capture_expires_at<now())))`, note, owner, capture)
	if err != nil {
		return nil, err
	}
	if ct.RowsAffected() == 0 {
		if _, err = s.GetNote(ctx, owner, note); err != nil {
			return nil, err
		}
		return nil, ErrCaptureActive
	}
	n, err := s.GetNote(ctx, owner, note)
	return &n, err
}
func (s *Store) HeartbeatCapture(ctx context.Context, owner, note, capture string) error {
	ct, err := s.pool.Exec(ctx, `UPDATE notes SET capture_expires_at=now()+interval '90 seconds' WHERE id=$1 AND owner_id=$2 AND capture_id=$3::uuid AND status='recording' AND deleted_at IS NULL`, note, owner, capture)
	if err != nil {
		return err
	}
	if ct.RowsAffected() == 0 {
		return ErrCaptureActive
	}
	return nil
}
func (s *Store) StopCaptureLease(ctx context.Context, owner, note, capture string, recover bool) (*model.Note, error) {
	var ct int64
	if recover {
		result, err := s.pool.Exec(ctx, `UPDATE notes SET status='draft',ended_at=now(),capture_id=NULL,capture_expires_at=NULL,updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL AND status='recording' AND audio_object_key IS NULL AND (capture_expires_at IS NULL OR capture_expires_at<now())`, note, owner)
		if err != nil {
			return nil, err
		}
		ct = result.RowsAffected()
	} else {
		result, err := s.pool.Exec(ctx, `UPDATE notes SET status='draft',ended_at=now(),capture_id=NULL,capture_expires_at=NULL,updated_at=now() WHERE id=$1 AND owner_id=$2 AND deleted_at IS NULL AND status='recording' AND audio_object_key IS NULL AND capture_id=$3::uuid`, note, owner, capture)
		if err != nil {
			return nil, err
		}
		ct = result.RowsAffected()
	}
	n, err := s.GetNote(ctx, owner, note)
	if err != nil {
		return nil, err
	}
	if ct == 0 && n.Status == model.NoteRecording {
		return nil, ErrCaptureActive
	}
	return &n, nil
}
