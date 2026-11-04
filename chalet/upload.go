package chalet

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strings"
)

// Upload puts one local file into Chalet's storage the way a page does — a
// direct upload — and answers its signed id, which a field of files takes.
// Two requests: the blob and its signed URL from the app, with the token; then
// the bytes to that URL, without it.
//
// Only a full path, and never a hidden file or one in a hidden folder: the
// call comes from an agent, and ~/.ssh is no file anyone means to share.
func (c *Client) Upload(ctx context.Context, path string) (string, error) {
	if err := shareable(path); err != nil {
		return "", err
	}

	file, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return "", err
	}
	if !info.Mode().IsRegular() {
		return "", fmt.Errorf("%s is not a file", path)
	}

	checksum, contentType, err := describe(file, path)
	if err != nil {
		return "", err
	}

	resp, err := c.Do(ctx, http.MethodPost, "/rails/active_storage/direct_uploads", nil, map[string]any{
		"blob": map[string]any{"filename": filepath.Base(path), "byte_size": info.Size(), "checksum": checksum, "content_type": contentType},
	}, nil)
	if err != nil {
		return "", err
	}
	if !resp.OK() {
		return "", fmt.Errorf("Chalet would not take %s: %s", filepath.Base(path), resp.Error())
	}

	var answer struct {
		SignedID     string `json:"signed_id"`
		DirectUpload struct {
			URL     string            `json:"url"`
			Headers map[string]string `json:"headers"`
		} `json:"direct_upload"`
	}
	if err := json.Unmarshal(resp.Body, &answer); err != nil || answer.SignedID == "" || answer.DirectUpload.URL == "" {
		return "", fmt.Errorf("Chalet's answer to the upload of %s has no signed id and URL", filepath.Base(path))
	}

	if err := c.put(ctx, answer.DirectUpload.URL, answer.DirectUpload.Headers, file, info.Size()); err != nil {
		return "", fmt.Errorf("could not send %s: %w", filepath.Base(path), err)
	}
	return answer.SignedID, nil
}

func shareable(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%q is not a full path: give the file's whole path, from the root", path)
	}
	for part := range strings.SplitSeq(filepath.Clean(path), string(filepath.Separator)) {
		if strings.HasPrefix(part, ".") {
			return fmt.Errorf("%s is hidden, or in a hidden folder: chalet never sends one", path)
		}
	}
	return nil
}

// The checksum storage checks the bytes against, and the type: by the name,
// else by the first bytes. The file is read whole once and rewound.
func describe(file *os.File, path string) (string, string, error) {
	hash := md5.New()
	if _, err := io.Copy(hash, file); err != nil {
		return "", "", err
	}

	contentType := mime.TypeByExtension(filepath.Ext(path))
	if contentType == "" {
		head := make([]byte, 512)
		n, _ := file.ReadAt(head, 0)
		contentType = http.DetectContentType(head[:n])
	}

	if _, err := file.Seek(0, io.SeekStart); err != nil {
		return "", "", err
	}
	return base64.StdEncoding.EncodeToString(hash.Sum(nil)), contentType, nil
}

// The bytes go to the signed URL with exactly the headers the app gave, never
// the token, and no redirect is followed. No timeout but the call's own: a
// large file takes as long as it takes.
func (c *Client) put(ctx context.Context, url string, headers map[string]string, body io.Reader, size int64) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, url, body)
	if err != nil {
		return err
	}
	req.ContentLength = size
	for name, value := range headers {
		req.Header.Set(name, value)
	}

	client := &http.Client{CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse }}
	if c.HTTP != nil {
		client.Transport = c.HTTP.Transport
	}
	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return errors.New("storage answered " + resp.Status)
	}
	return nil
}
