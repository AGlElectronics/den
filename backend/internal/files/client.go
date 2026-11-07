// Package files wraps an S3-compatible object storage client (SeaweedFS in dev,
// a real managed bucket in production). Den owns exactly one bucket and
// never hands out credentials to mated modules; see
// docs/adr/0004-one-bucket-per-module.md.
package files

import (
	"context"
	"fmt"
	"io"
	"net/url"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"

	"github.com/AGlElectronics/den/backend/internal/config"
)

// Client wraps a *minio.Client plus the configured bucket and a public-facing
// base URL used to rewrite presigned URLs so a browser can actually reach
// them (e.g. the compose host `s3:9000` is unreachable from the host OS
// — swap to `localhost:9000` on the way out).
type Client struct {
	mc            *minio.Client
	bucket        string
	publicBaseURL *url.URL
}

// New constructs a Client and ensures Den's bucket exists. The idempotent
// create keeps `docker compose up` painless on a fresh install.
//
// This client is the same code path in dev and prod — the ADR calls out
// that the dev S3 server is a dev convenience, not a prod decision made in Go.
func New(ctx context.Context, cfg *config.Config) (*Client, error) {
	endpointURL, err := url.Parse(cfg.S3Endpoint)
	if err != nil {
		return nil, fmt.Errorf("parse S3_ENDPOINT: %w", err)
	}
	host := endpointURL.Host
	if host == "" {
		host = endpointURL.Path
	}
	useSSL := endpointURL.Scheme == "https"

	// Without explicit BucketLookup the SDK auto-detects, which can flip
	// between path- and virtual-hosted style depending on the host name.
	// Pin to path-style when configured (default for the compose S3 server).
	bucketLookup := minio.BucketLookupAuto
	if cfg.S3UsePathStyle {
		bucketLookup = minio.BucketLookupPath
	}

	mc, err := minio.New(host, &minio.Options{
		Creds:        credentials.NewStaticV4(cfg.S3AccessKeyID, cfg.S3SecretAccessKey, ""),
		Secure:       useSSL,
		Region:       cfg.S3Region,
		BucketLookup: bucketLookup,
	})
	if err != nil {
		return nil, fmt.Errorf("create minio client: %w", err)
	}

	exists, err := mc.BucketExists(ctx, cfg.S3Bucket)
	if err != nil {
		return nil, fmt.Errorf("check bucket: %w", err)
	}
	if !exists {
		if err := mc.MakeBucket(ctx, cfg.S3Bucket, minio.MakeBucketOptions{Region: cfg.S3Region}); err != nil {
			return nil, fmt.Errorf("create bucket: %w", err)
		}
	}

	publicBase, _ := publicBaseURL(cfg)

	return &Client{
		mc:            mc,
		bucket:        cfg.S3Bucket,
		publicBaseURL: publicBase,
	}, nil
}

func publicBaseURL(cfg *config.Config) (*url.URL, error) {
	if v := envLookup("S3_PUBLIC_BASE_URL"); v != "" {
		return url.Parse(v)
	}
	u, err := url.Parse(cfg.S3Endpoint)
	if err != nil {
		return nil, err
	}
	// In the compose network the host `s3` (formerly `minio`) resolves to
	// the container; from the host OS that name is nothing. Rewrite so
	// browsers get a URL they can hit.
	if h := u.Hostname(); h == "s3" || h == "minio" {
		u.Host = strings.Replace(u.Host, h, "localhost", 1)
	}
	return u, nil
}

// PutObject streams bytes into the bucket. Returns the storage key (same
// as the one passed in). Size == -1 is fine, the SDK will chunk.
func (c *Client) PutObject(ctx context.Context, key string, r io.Reader, size int64, contentType string) error {
	_, err := c.mc.PutObject(ctx, c.bucket, key, r, size, minio.PutObjectOptions{
		ContentType: contentType,
	})
	if err != nil {
		return fmt.Errorf("put object: %w", err)
	}
	return nil
}

// GetObject opens a streaming reader for the object. Caller must Close.
func (c *Client) GetObject(ctx context.Context, key string) (io.ReadCloser, error) {
	obj, err := c.mc.GetObject(ctx, c.bucket, key, minio.GetObjectOptions{})
	if err != nil {
		return nil, fmt.Errorf("get object: %w", err)
	}
	// Probe with Stat so missing keys surface as an error here rather than
	// mid-stream (we need to translate to a 404 at the HTTP layer).
	if _, err := obj.Stat(); err != nil {
		_ = obj.Close()
		return nil, fmt.Errorf("stat object: %w", err)
	}
	return obj, nil
}

// Remove deletes an object. Used when a branding_assets row is deleted.
func (c *Client) Remove(ctx context.Context, key string) error {
	return c.mc.RemoveObject(ctx, c.bucket, key, minio.RemoveObjectOptions{})
}

// PresignedGet is only used for admin-side preview (SPA → S3 direct in
// dev). Mated modules never see presigned URLs — they always go through
// Den's integration API so we can authenticate and audit.
func (c *Client) PresignedGet(ctx context.Context, key string, expiry time.Duration) (string, error) {
	u, err := c.mc.PresignedGetObject(ctx, c.bucket, key, expiry, nil)
	if err != nil {
		return "", fmt.Errorf("presign get: %w", err)
	}
	return c.rewriteURL(u).String(), nil
}

// Bucket returns the configured bucket name.
func (c *Client) Bucket() string { return c.bucket }

func (c *Client) rewriteURL(u *url.URL) *url.URL {
	if c.publicBaseURL == nil {
		return u
	}
	out := *u
	out.Scheme = c.publicBaseURL.Scheme
	out.Host = c.publicBaseURL.Host
	return &out
}
