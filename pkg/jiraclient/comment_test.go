package jiraclient

import (
	"context"
	"net/http"
	"testing"
)

func TestAddCommentPostsTheADFBody(t *testing.T) {
	client, requests := newTestClient(t, func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusCreated)
		_, _ = w.Write([]byte(`{"id":"10000","body":{}}`))
	})

	comment, err := client.AddComment(context.Background(), "INIT-1", ADFDescription("Looks **good**", nil, ""))
	if err != nil {
		t.Fatal(err)
	}

	if comment.ID != "10000" {
		t.Fatalf("comment = %+v", comment)
	}
	want := `{"body":{"content":[{"content":[{"text":"Looks ","type":"text"},{"marks":[{"type":"strong"}],"text":"good","type":"text"}],"type":"paragraph"}],"type":"doc","version":1}}`
	if got := (*requests)[0]; got.method != http.MethodPost || got.path != "/rest/api/3/issue/INIT-1/comment" || got.body != want {
		t.Fatalf("request = %+v", got)
	}
}

func TestAddCommentRejectsAnEmptyBody(t *testing.T) {
	client, requests := newTestClient(t, func(http.ResponseWriter, *http.Request) {})

	if _, err := client.AddComment(context.Background(), "INIT-1", nil); err == nil {
		t.Fatal("err = nil; want an error for an empty body")
	}
	if len(*requests) != 0 {
		t.Fatalf("requests = %+v; want none", *requests)
	}
}
