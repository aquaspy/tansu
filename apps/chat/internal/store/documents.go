package store

import (
	"time"
)

// Document describes one uploaded PDF. The bytes live on disk under the
// data dir; rows cascade with their message.
type Document struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	ByteSize    int64
	SHA256      string
	CreatedAt   time.Time
}

const documentCols = `id, message_id, filename, content_type, byte_size, sha256, created_at`

func (s *Store) CreateDocument(messageID int64, filename, contentType string, byteSize int64, sha256 string) (*Document, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO documents
		(message_id, filename, content_type, byte_size, sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, messageID, filename, contentType, byteSize, sha256, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, _ := parseTime(ts)
	return &Document{ID: id, MessageID: messageID, Filename: filename,
		ContentType: contentType, ByteSize: byteSize, SHA256: sha256, CreatedAt: t}, nil
}

func (s *Store) GetDocument(id int64) (*Document, error) {
	doc := &Document{ID: id}
	var created string
	err := s.db.QueryRow(`SELECT message_id, filename, content_type, byte_size, sha256, created_at
		FROM documents WHERE id = ?`, id).
		Scan(&doc.MessageID, &doc.Filename, &doc.ContentType, &doc.ByteSize, &doc.SHA256, &created)
	if err != nil {
		return nil, err
	}
	doc.CreatedAt, _ = parseTime(created)
	return doc, nil
}

func (s *Store) ListDocuments(messageID int64) ([]*Document, error) {
	rows, err := s.db.Query(`SELECT `+documentCols+` FROM documents
		WHERE message_id = ? ORDER BY id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Document
	for rows.Next() {
		doc := &Document{}
		var created string
		if err := rows.Scan(&doc.ID, &doc.MessageID, &doc.Filename, &doc.ContentType,
			&doc.ByteSize, &doc.SHA256, &created); err != nil {
			return nil, err
		}
		doc.CreatedAt, _ = parseTime(created)
		out = append(out, doc)
	}
	return out, rows.Err()
}
