package jiraclient

import (
	"context"
	"errors"
	"net/http"
	"testing"
)

func TestDownloadAttachmentAsksJiraToServeTheContent(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "image/png")
		_, _ = w.Write([]byte("png bytes"))
	})

	content, err := client.DownloadAttachment(context.Background(), "10042", 9)
	if err != nil {
		t.Fatal(err)
	}

	if string(content.Body) != "png bytes" || content.ContentType != "image/png" {
		t.Fatalf("content = %q (%s)", content.Body, content.ContentType)
	}
	if got := (*requests)[0]; got.method != http.MethodGet || got.path != "/rest/api/3/attachment/content/10042" || got.query != "redirect=false" {
		t.Fatalf("request = %+v", got)
	}
}

func TestDownloadAttachmentRefusesContentOverTheLimit(t *testing.T) {
	tests := []struct {
		name    string
		respond func(http.ResponseWriter)
	}{
		{name: "declared length", respond: func(w http.ResponseWriter) { _, _ = w.Write([]byte("0123456789")) }},
		{name: "chunked", respond: func(w http.ResponseWriter) {
			_, _ = w.Write([]byte("01234"))
			w.(http.Flusher).Flush()
			_, _ = w.Write([]byte("56789"))
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client, _ := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) { tt.respond(w) })

			_, err := client.DownloadAttachment(context.Background(), "10042", 9)
			if !errors.Is(err, ErrAttachmentTooLarge) {
				t.Fatalf("err = %v; want ErrAttachmentTooLarge", err)
			}
		})
	}
}
