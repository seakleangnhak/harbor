package domain

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"time"
)

var (
	ErrNotFound  = errors.New("not found")
	ErrConflict  = errors.New("conflict")
	ErrInvalid   = errors.New("invalid input")
	ErrIntegrity = errors.New("content integrity check failed")
)

func NewID() string {
	var b [16]byte
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

type Node struct {
	ID        string     `json:"id"`
	ParentID  string     `json:"parent_id"`
	Name      string     `json:"name"`
	Kind      string     `json:"kind"`
	Size      int64      `json:"size"`
	MimeType  string     `json:"mime_type"`
	SHA256    string     `json:"sha256"`
	Favorite  bool       `json:"favorite"`
	TrashedAt *time.Time `json:"trashed_at"`
	CreatedAt time.Time  `json:"created_at"`
	UpdatedAt time.Time  `json:"updated_at"`
}

type UploadSpec struct {
	Name     string `json:"name"`
	ParentID string `json:"parent_id"`
	Size     int64  `json:"size"`
	MimeType string `json:"mime_type"`
	SHA256   string `json:"sha256"`
}

type Upload struct {
	ID             string    `json:"id"`
	Name           string    `json:"name"`
	ParentID       string    `json:"parent_id"`
	Size           int64     `json:"size"`
	MimeType       string    `json:"mime_type"`
	SHA256         string    `json:"sha256"`
	ChunkSize      int64     `json:"chunk_size"`
	TotalChunks    int       `json:"total_chunks"`
	ReceivedChunks []int     `json:"received_chunks"`
	ReceivedBytes  int64     `json:"received_bytes"`
	Status         string    `json:"status"`
	CreatedAt      time.Time `json:"created_at"`
}

type Manifest struct {
	ID        string
	Name      string
	ParentID  string
	Size      int64
	MimeType  string
	SHA256    string
	CreatedAt time.Time
}

type StorageStatus struct {
	State         string     `json:"state"`
	LastCheckedAt *time.Time `json:"last_checked_at,omitempty"`
	RetryCount    uint64     `json:"retry_count"`
}
