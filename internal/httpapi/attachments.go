package httpapi

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"mime"
	"mime/multipart"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

const (
	maxUploadBytes     = 20 << 20
	maxAttachmentCount = 4
	maxAttachmentBytes = 10 << 20
)

type storedAttachment struct {
	ID          string
	Name        string
	ContentType string
	Size        int64
	URL         string
	Path        string
}

func (s *Server) parseSendMessageRequest(r *http.Request, sessionID string) (string, []storedAttachment, error) {
	contentType := r.Header.Get("Content-Type")
	if strings.HasPrefix(contentType, "multipart/form-data") {
		return s.parseMultipartSendMessage(r, sessionID)
	}

	var req sendMessageRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		return "", nil, fmt.Errorf("invalid request body")
	}
	return strings.TrimSpace(req.Message), nil, nil
}

func (s *Server) parseMultipartSendMessage(r *http.Request, sessionID string) (string, []storedAttachment, error) {
	if err := r.ParseMultipartForm(maxUploadBytes); err != nil {
		return "", nil, fmt.Errorf("invalid multipart form")
	}

	message := strings.TrimSpace(r.FormValue("message"))
	files := r.MultipartForm.File["images"]
	if len(files) > maxAttachmentCount {
		return "", nil, fmt.Errorf("too many images; max %d", maxAttachmentCount)
	}

	attachments := make([]storedAttachment, 0, len(files))
	for _, header := range files {
		attachment, err := s.saveAttachment(sessionID, header)
		if err != nil {
			cleanupStoredAttachments(attachments)
			return "", nil, err
		}
		attachments = append(attachments, attachment)
	}
	return message, attachments, nil
}

func (s *Server) saveAttachment(sessionID string, header *multipart.FileHeader) (storedAttachment, error) {
	file, err := header.Open()
	if err != nil {
		return storedAttachment{}, fmt.Errorf("opening uploaded file %q: %w", header.Filename, err)
	}
	defer file.Close()

	if header.Size > maxAttachmentBytes {
		return storedAttachment{}, fmt.Errorf("%q exceeds %d MB", header.Filename, maxAttachmentBytes>>20)
	}
	if err := os.MkdirAll(s.uploadDir, 0o755); err != nil {
		return storedAttachment{}, fmt.Errorf("creating upload dir: %w", err)
	}

	sniff := make([]byte, 512)
	n, err := io.ReadFull(file, sniff)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return storedAttachment{}, fmt.Errorf("reading uploaded file %q: %w", header.Filename, err)
	}
	contentType := http.DetectContentType(sniff[:n])
	if !strings.HasPrefix(contentType, "image/") {
		return storedAttachment{}, fmt.Errorf("%q is not a supported image", header.Filename)
	}
	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return storedAttachment{}, fmt.Errorf("rewinding uploaded file %q: %w", header.Filename, err)
	}

	id, err := randomID()
	if err != nil {
		return storedAttachment{}, fmt.Errorf("generating upload id: %w", err)
	}

	ext := strings.ToLower(filepath.Ext(header.Filename))
	if ext == "" {
		if exts, _ := mime.ExtensionsByType(contentType); len(exts) > 0 {
			ext = exts[0]
		}
	}

	filename := fmt.Sprintf("%s-%s%s", sessionID, id, ext)
	path := filepath.Join(s.uploadDir, filename)

	dst, err := os.Create(path)
	if err != nil {
		return storedAttachment{}, fmt.Errorf("creating attachment file: %w", err)
	}

	written, err := io.Copy(dst, io.LimitReader(file, maxAttachmentBytes+1))
	closeErr := dst.Close()
	if err != nil {
		_ = os.Remove(path)
		return storedAttachment{}, fmt.Errorf("saving attachment file: %w", err)
	}
	if closeErr != nil {
		_ = os.Remove(path)
		return storedAttachment{}, fmt.Errorf("closing attachment file: %w", closeErr)
	}
	if written > maxAttachmentBytes {
		_ = os.Remove(path)
		return storedAttachment{}, fmt.Errorf("%q exceeds %d MB", header.Filename, maxAttachmentBytes>>20)
	}

	return storedAttachment{
		ID:          id,
		Name:        filepath.Base(header.Filename),
		ContentType: contentType,
		Size:        written,
		URL:         "/api/uploads/" + filename,
		Path:        path,
	}, nil
}

func randomID() (string, error) {
	b := make([]byte, 8)
	if _, err := rand.Read(b); err != nil {
		return "", err
	}
	return hex.EncodeToString(b), nil
}
