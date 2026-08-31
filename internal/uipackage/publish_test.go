package uipackage

import (
	"context"
	"net/url"
	"strings"
	"testing"
)

func TestPresignPutIsShortLivedAndDoesNotRequireAContentHash(t *testing.T) {
	presigned, err := PresignPut(context.Background(), "request/app.wasm", S3Config{
		Bucket: "druid-ui", Region: "fsn1", Endpoint: "https://s3.example.test", AccessKey: "access", SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(presigned)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("X-Amz-Expires"); got != "1200" {
		t.Fatalf("signature expiry = %q, want 1200 seconds", got)
	}
	signedHeaders := parsed.Query().Get("X-Amz-SignedHeaders")
	if !strings.Contains(signedHeaders, "cache-control") || strings.Contains(signedHeaders, "content-md5") {
		t.Fatalf("signed headers = %q, want cache control without content-md5", signedHeaders)
	}
}

func TestPresignPutRejectsMissingObjectDetails(t *testing.T) {
	_, err := PresignPut(context.Background(), "", S3Config{Bucket: "druid-ui", Region: "fsn1", AccessKey: "access", SecretKey: "secret"})
	if err == nil {
		t.Fatal("missing object name was accepted")
	}
}
