package chalet

import (
	"context"
	"crypto/md5"
	"encoding/base64"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The blob from the app with the token, the bytes to storage without it, and
// the signed id back.
func TestUploadAsAPageDoes(t *testing.T) {
	var blob map[string]any
	var put *http.Request
	var stored string

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method + " " + r.URL.Path {
		case "POST /1/rails/active_storage/direct_uploads":
			var sent struct{ Blob map[string]any }
			json.NewDecoder(r.Body).Decode(&sent)
			blob = sent.Blob
			io.WriteString(w, `{"signed_id":"signed-1","direct_upload":{"url":"http://`+r.Host+`/storage/key","headers":{"Content-Type":"application/pdf","Content-MD5":"x"}}}`)
		case "PUT /storage/key":
			put = r
			data, _ := io.ReadAll(r.Body)
			stored = string(data)
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer server.Close()

	path := filepath.Join(t.TempDir(), "Venue.pdf")
	os.WriteFile(path, []byte("%PDF-1.7 venue"), 0o600)

	signedID, err := (&Client{BaseURL: server.URL, Account: "1", Token: "chalet_pat_x"}).Upload(context.Background(), path)
	if err != nil {
		t.Fatal(err)
	}

	sum := md5.Sum([]byte("%PDF-1.7 venue"))
	if signedID != "signed-1" || blob["filename"] != "Venue.pdf" || blob["content_type"] != "application/pdf" ||
		blob["checksum"] != base64.StdEncoding.EncodeToString(sum[:]) || blob["byte_size"] != float64(14) {
		t.Fatalf("signed id %q, blob %v", signedID, blob)
	}
	if stored != "%PDF-1.7 venue" || put.Header.Get("Authorization") != "" || put.Header.Get("Content-MD5") != "x" {
		t.Fatalf("stored %q with %v", stored, put.Header)
	}
}

func TestUploadOnlyAFullPathAndNothingHidden(t *testing.T) {
	client := &Client{BaseURL: "http://localhost:1", Token: "chalet_pat_x"}
	for path, says := range map[string]string{
		"notes.txt":                               "full path",
		filepath.Join(t.TempDir(), ".env"):        "hidden",
		filepath.Join(t.TempDir(), ".ssh", "key"): "hidden",
	} {
		if _, err := client.Upload(context.Background(), path); err == nil || !strings.Contains(err.Error(), says) {
			t.Errorf("%s: got %v, want an error saying %q", path, err, says)
		}
	}
}
