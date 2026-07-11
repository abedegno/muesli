package storage

import (
	"io"
	"time"
)

// UploadGrant is a short-lived authorization to PUT one object.
type UploadGrant struct {
	URL    string    `json:"url"`
	Method string    `json:"method"`
	Key    string    `json:"key"`
	Expiry time.Time `json:"expires_at"`
}

// Provider issues scoped, time-limited grants and verifies stored objects.
type Provider interface {
	// PresignUpload returns a grant to PUT the given object key.
	PresignUpload(key string, ttl time.Duration) (UploadGrant, error)
	// MaxUploadBytes returns the maximum allowed size for one upload body.
	MaxUploadBytes() int64
	// PresignDownload returns a short-lived signed GET URL for the object.
	PresignDownload(key string, ttl time.Duration) (string, error)
	// Open returns a streaming reader for the stored object.
	Open(key string) (io.ReadCloser, error)
	// Verify reports whether the object exists and its size in bytes.
	Verify(key string) (exists bool, size int64, err error)
	// Delete removes the object. Missing objects are not an error (idempotent).
	Delete(key string) error
}
