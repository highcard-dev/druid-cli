package uipackage

import (
	"context"
	"crypto/md5"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"net/url"
	"strings"
	"testing"

	"github.com/highcard-dev/daemon/internal/core/domain"
	"github.com/highcard-dev/daemon/internal/core/ports"
)

func TestPresignPutBindsContentDigests(t *testing.T) {
	contents := []byte("Druid UI package")
	sha256sum := sha256.Sum256(contents)
	md5sum := md5.Sum(contents)
	action := ports.RuntimeUIPackageUploadAction{
		Scope:      domain.RuntimeUIPackageScopePrivate,
		SHA256:     hex.EncodeToString(sha256sum[:]),
		ContentMD5: base64.StdEncoding.EncodeToString(md5sum[:]),
	}
	presigned, err := PresignPut(context.Background(), "request/app.wasm", action, S3Config{
		Bucket: "druid-ui", Region: "fsn1", Endpoint: "https://s3.example.test", AccessKey: "access", SecretKey: "secret",
	})
	if err != nil {
		t.Fatal(err)
	}
	parsed, err := url.Parse(presigned)
	if err != nil {
		t.Fatal(err)
	}
	if got := parsed.Query().Get("X-Amz-Expires"); got != "300" {
		t.Fatalf("signature expiry = %q, want 300 seconds", got)
	}
	signedHeaders := parsed.Query().Get("X-Amz-SignedHeaders")
	if !strings.Contains(signedHeaders, "content-md5") {
		t.Fatalf("signed headers = %q, missing content-md5", signedHeaders)
	}
}

func TestPresignPutRejectsInvalidDigests(t *testing.T) {
	_, err := PresignPut(context.Background(), "request/app.wasm", ports.RuntimeUIPackageUploadAction{
		SHA256: "not-a-sha", ContentMD5: "not-an-md5",
	}, S3Config{Bucket: "druid-ui", Region: "fsn1", AccessKey: "access", SecretKey: "secret"})
	if err == nil {
		t.Fatal("invalid digest action was accepted")
	}
}

func TestEndpointObjectURLUsesConfiguredS3Endpoint(t *testing.T) {
	got, err := EndpointObjectURL(S3Config{
		Endpoint: "http://host.k3d.internal:9000/", Bucket: "druid-ui", KeyPrefix: "/runtime/private/",
	}, "request/app.wasm")
	if err != nil {
		t.Fatal(err)
	}
	if got != "http://host.k3d.internal:9000/druid-ui/runtime/private/request/app.wasm" {
		t.Fatalf("endpoint object URL = %q", got)
	}
}
