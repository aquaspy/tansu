package store

import (
	"time"
)

// Image describes one uploaded file. The bytes live on disk under the data
// dir; derivatives (thumb, model-size) sit next to the original.
type Image struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	ByteSize    int64
	SHA256      string
	CreatedAt   time.Time
}

const imageCols = `id, message_id, filename, content_type, byte_size, sha256, created_at`

func (s *Store) CreateImage(messageID int64, filename, contentType string, byteSize int64, sha256 string) (*Image, error) {
	ts := now()
	res, err := s.db.Exec(`INSERT INTO images
		(message_id, filename, content_type, byte_size, sha256, created_at)
		VALUES (?, ?, ?, ?, ?, ?)`, messageID, filename, contentType, byteSize, sha256, ts)
	if err != nil {
		return nil, err
	}
	id, _ := res.LastInsertId()
	t, _ := parseTime(ts)
	return &Image{ID: id, MessageID: messageID, Filename: filename,
		ContentType: contentType, ByteSize: byteSize, SHA256: sha256, CreatedAt: t}, nil
}

func (s *Store) GetImage(id int64) (*Image, error) {
	img := &Image{ID: id}
	var created string
	err := s.db.QueryRow(`SELECT message_id, filename, content_type, byte_size, sha256, created_at
		FROM images WHERE id = ?`, id).
		Scan(&img.MessageID, &img.Filename, &img.ContentType, &img.ByteSize, &img.SHA256, &created)
	if err != nil {
		return nil, err
	}
	img.CreatedAt, _ = parseTime(created)
	return img, nil
}

func (s *Store) ListImages(messageID int64) ([]*Image, error) {
	rows, err := s.db.Query(`SELECT `+imageCols+` FROM images
		WHERE message_id = ? ORDER BY id`, messageID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []*Image
	for rows.Next() {
		img := &Image{}
		var created string
		if err := rows.Scan(&img.ID, &img.MessageID, &img.Filename, &img.ContentType,
			&img.ByteSize, &img.SHA256, &created); err != nil {
			return nil, err
		}
		img.CreatedAt, _ = parseTime(created)
		out = append(out, img)
	}
	return out, rows.Err()
}

// DeleteImagesByMessage removes the rows and returns them so the caller
// can purge the files from disk.
func (s *Store) DeleteImagesByMessage(messageID int64) ([]*Image, error) {
	imgs, err := s.ListImages(messageID)
	if err != nil {
		return nil, err
	}
	_, err = s.db.Exec(`DELETE FROM images WHERE message_id = ?`, messageID)
	return imgs, err
}
