package files

import (
	"bytes"
	"context"
	"io"
	"net/url"
	"os"
	"testing"
	"time"

	"github.com/AGlElectronics/den/backend/internal/config"
)

func TestPublicBaseURL(t *testing.T) {
	t.Setenv("S3_PUBLIC_BASE_URL", "")
	cases := map[string]string{
		"http://s3:9000":            "http://localhost:9000",
		"http://minio:9000":         "http://localhost:9000",
		"https://s3.example.org":    "https://s3.example.org",
		"http://seaweedfs.svc:9000": "http://seaweedfs.svc:9000",
	}
	for endpoint, want := range cases {
		u, err := publicBaseURL(&config.Config{S3Endpoint: endpoint})
		if err != nil || u.String() != want {
			t.Errorf("%s: got %v %v, want %s", endpoint, u, err, want)
		}
	}
	t.Setenv("S3_PUBLIC_BASE_URL", "https://files.example.org")
	if u, _ := publicBaseURL(&config.Config{S3Endpoint: "http://s3:9000"}); u.String() != "https://files.example.org" {
		t.Errorf("override: %v", u)
	}
}

func TestRewriteURL(t *testing.T) {
	in, _ := url.Parse("http://s3:9000/den-data-dev/logo.png?X-Amz-Signature=abc")
	c := &Client{}
	if got := c.rewriteURL(in); got.String() != in.String() {
		t.Fatalf("without a public base the URL is unchanged: %s", got)
	}
	c.publicBaseURL, _ = url.Parse("https://files.example.org")
	if got := c.rewriteURL(in).String(); got != "https://files.example.org/den-data-dev/logo.png?X-Amz-Signature=abc" {
		t.Fatalf("rewritten: %s", got)
	}
}

// Round trip against a real S3 server. Needs DEN_TEST_S3_ENDPOINT (and
// DEN_TEST_S3_ACCESS_KEY / DEN_TEST_S3_SECRET_KEY); skipped otherwise.
func TestS3RoundTrip(t *testing.T) {
	endpoint := os.Getenv("DEN_TEST_S3_ENDPOINT")
	if endpoint == "" {
		t.Skip("DEN_TEST_S3_ENDPOINT not set")
	}
	ctx := context.Background()
	c, err := New(ctx, &config.Config{
		S3Endpoint: endpoint, S3Bucket: "den-files-test", S3Region: "us-east-1", S3UsePathStyle: true,
		S3AccessKeyID: os.Getenv("DEN_TEST_S3_ACCESS_KEY"), S3SecretAccessKey: os.Getenv("DEN_TEST_S3_SECRET_KEY"),
	})
	if err != nil {
		t.Fatal(err)
	}
	key := "test/" + time.Now().Format("150405.000000")
	data := []byte("hello den")
	if err := c.PutObject(ctx, key, bytes.NewReader(data), int64(len(data)), "text/plain"); err != nil {
		t.Fatal(err)
	}
	rc, err := c.GetObject(ctx, key)
	if err != nil {
		t.Fatal(err)
	}
	got, _ := io.ReadAll(rc)
	_ = rc.Close()
	if !bytes.Equal(got, data) {
		t.Fatalf("got %q", got)
	}
	if err := c.Remove(ctx, key); err != nil {
		t.Fatal(err)
	}
	if _, err := c.GetObject(ctx, key); err == nil {
		t.Fatal("a removed object must not be readable")
	}
}
