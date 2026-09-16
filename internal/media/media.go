package media

import (
	"io"

	"github.com/knadh/listmonk/models"
	"gopkg.in/volatiletech/null.v6"
)

// Media represents an uploaded object.
type Media struct {
	models.ResourceScope

	ID          int         `db:"id" json:"id"`
	UUID        string      `db:"uuid" json:"uuid"`
	Filename    string      `db:"filename" json:"filename"`
	FolderID    null.Int    `db:"folder_id" json:"folder_id"`
	ContentType string      `db:"content_type" json:"content_type"`
	Thumb       string      `db:"thumb" json:"-"`
	CreatedAt   null.Time   `db:"created_at" json:"created_at"`
	ThumbURL    null.String `json:"thumb_url"`
	Provider    string      `json:"provider"`
	Meta        models.JSON `db:"meta" json:"meta"`
	URL         string      `json:"url"`

	Total int `db:"total" json:"-"`
}

// MediaFolder is a logical folder in the media library. Folders are stored in
// the database and do not change the provider object name, which keeps old
// media URLs and both filesystem and S3 providers compatible.
type MediaFolder struct {
	ID         int       `db:"id" json:"id"`
	Name       string    `db:"name" json:"name"`
	ParentID   null.Int  `db:"parent_id" json:"parent_id"`
	CreatedAt  null.Time `db:"created_at" json:"created_at"`
	UpdatedAt  null.Time `db:"updated_at" json:"updated_at"`
	MediaCount int       `db:"media_count" json:"media_count"`
	ChildCount int       `db:"child_count" json:"child_count"`

	// Workspace fields are used by Core to prevent cross-workspace folder
	// moves, but are not part of the public folder DTO.
	OrganizationID null.Int `db:"organization_id" json:"-"`
	OwnerUserID    null.Int `db:"owner_user_id" json:"-"`
}

// Store represents functions to store and retrieve media (files).
type Store interface {
	Put(string, string, io.ReadSeeker) (string, error)
	Delete(string) error
	GetURL(string) string
	GetBlob(string) ([]byte, error)
}
