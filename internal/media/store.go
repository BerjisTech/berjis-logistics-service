package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"image"
	"image/jpeg"
	"os"
	"path/filepath"
	"strings"
	"time"

	_ "golang.org/x/image/webp"

	"cloud.google.com/go/storage"
	awsconf "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/disintegration/imaging"
)

type Result struct {
	URL      string
	ThumbURL string
}

type Store interface {
	Put(ctx context.Context, path string, contentType string, data []byte) (string, error)
	Delete(ctx context.Context, path string) error
}

// NewStore chooses backend based on env
func NewStore(ctx context.Context) (Store, string, error) {
	backend := strings.ToLower(strings.TrimSpace(os.Getenv("STORAGE_BACKEND")))
	if backend == "" {
		backend = "local"
	}
	// Attempt chosen backend; on misconfiguration, gracefully fall back to local drive
	switch backend {
	case "s3":
		s, base, err := newS3Store(ctx)
		if err == nil {
			return s, base, nil
		}
		ls, lbase, _ := newLocalStore()
		return ls, lbase, nil
	case "gcs":
		s, base, err := newGCSStore(ctx)
		if err == nil {
			return s, base, nil
		}
		ls, lbase, _ := newLocalStore()
		return ls, lbase, nil
	default:
		return newLocalStore()
	}
}

// NewLocal explicitly returns the local filesystem-backed store regardless of env.
// Useful as a runtime fallback when cloud storage writes fail.
func NewLocal() (Store, string, error) {
	return newLocalStore()
}

func publicBase() string {
	base := strings.TrimRight(os.Getenv("MEDIA_BASE_URL"), "/")
	if base != "" {
		return base
	}
	return ""
}

// Process resizes to max 1280 on longest side, creates 320 thumb; both JPEG q=85
func Process(data []byte) (full []byte, thumb []byte, err error) {
	img, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, nil, err
	}
	// Full size
	fullImg := imaging.Fit(img, 1280, 1280, imaging.Lanczos)
	var fb bytes.Buffer
	_ = jpeg.Encode(&fb, fullImg, &jpeg.Options{Quality: 85})

	// Thumb
	th := imaging.Fill(img, 320, 320, imaging.Center, imaging.Lanczos)
	var tb bytes.Buffer
	_ = jpeg.Encode(&tb, th, &jpeg.Options{Quality: 80})
	return fb.Bytes(), tb.Bytes(), nil
}

func RandHex(n int) string {
	b := make([]byte, n)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// Local store
type localStore struct {
	base   string
	prefix string
}

func UploadsDir() string {
	if v := strings.TrimSpace(os.Getenv("UPLOADS_DIR")); v != "" {
		return v
	}
	// Default inside container; can be volume-mounted via docker-compose
	return "/data/uploads"
}
func UploadsPublicPath() string {
	p := strings.TrimSpace(os.Getenv("UPLOADS_PUBLIC_PATH"))
	if p == "" {
		p = "/uploads"
	}
	if !strings.HasPrefix(p, "/") {
		p = "/" + p
	}
	return strings.TrimRight(p, "/")
}

func newLocalStore() (Store, string, error) {
	base := UploadsDir()
	_ = os.MkdirAll(base, 0o755)
	return &localStore{base: base, prefix: UploadsPublicPath()}, publicBase(), nil
}
func (s *localStore) Put(_ context.Context, path string, _ string, data []byte) (string, error) {
	fp := filepath.Join(s.base, filepath.FromSlash(path))
	if err := os.MkdirAll(filepath.Dir(fp), 0o755); err != nil {
		return "", err
	}
	if err := os.WriteFile(fp, data, 0o644); err != nil {
		return "", err
	}
	base := publicBase()
	if base != "" {
		return fmt.Sprintf("%s%s/%s", base, s.prefix, strings.TrimLeft(path, "/")), nil
	}
	// Return URL path that maps to the static handler; server will expose s.prefix -> uploadsDir
	return fmt.Sprintf("%s/%s", s.prefix, strings.TrimLeft(path, "/")), nil
}
func (s *localStore) Delete(_ context.Context, path string) error {
	fp := filepath.Join(s.base, filepath.FromSlash(path))
	return os.Remove(fp)
}

// S3 store
type s3Store struct {
	client  *s3.Client
	bucket  string
	urlBase string
}

func newS3Store(ctx context.Context) (Store, string, error) {
	bucket := os.Getenv("S3_BUCKET")
	region := os.Getenv("S3_REGION")
	endpoint := os.Getenv("S3_ENDPOINT")
	if bucket == "" || region == "" {
		return nil, "", fmt.Errorf("S3_BUCKET and S3_REGION required")
	}
	var cfgOpts []func(*awsconf.LoadOptions) error
	if endpoint != "" {
		cfgOpts = append(cfgOpts, awsconf.WithRegion(region))
	}
	if ak := os.Getenv("AWS_ACCESS_KEY_ID"); ak != "" {
		sk := os.Getenv("AWS_SECRET_ACCESS_KEY")
		cfgOpts = append(cfgOpts, awsconf.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(ak, sk, "")))
	}
	cfg, err := awsconf.LoadDefaultConfig(ctx, cfgOpts...)
	if err != nil {
		return nil, "", err
	}
	client := s3.NewFromConfig(cfg, func(o *s3.Options) {
		if endpoint != "" {
			o.BaseEndpoint = &endpoint
		}
	})
	base := os.Getenv("MEDIA_BASE_URL")
	if base == "" {
		base = fmt.Sprintf("https://%s.s3.%s.amazonaws.com", bucket, region)
	}
	return &s3Store{client: client, bucket: bucket, urlBase: strings.TrimRight(base, "/")}, base, nil
}
func (s *s3Store) Put(ctx context.Context, path string, contentType string, data []byte) (string, error) {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      &s.bucket,
		Key:         &path,
		Body:        bytes.NewReader(data),
		ContentType: &contentType,
		ACL:         types.ObjectCannedACLPublicRead,
	})
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%s/%s", s.urlBase, strings.TrimLeft(path, "/")), nil
}
func (s *s3Store) Delete(ctx context.Context, path string) error {
	_, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: &s.bucket, Key: &path})
	return err
}

// GCS store
type gcsStore struct {
	bucket *storage.BucketHandle
	base   string
}

func newGCSStore(ctx context.Context) (Store, string, error) {
	b := os.Getenv("GCS_BUCKET")
	if b == "" {
		return nil, "", fmt.Errorf("GCS_BUCKET required")
	}
	client, err := storage.NewClient(ctx)
	if err != nil {
		return nil, "", err
	}
	base := os.Getenv("MEDIA_BASE_URL")
	if base == "" {
		base = fmt.Sprintf("https://storage.googleapis.com/%s", b)
	}
	return &gcsStore{bucket: client.Bucket(b), base: strings.TrimRight(base, "/")}, base, nil
}
func (s *gcsStore) Put(ctx context.Context, path string, contentType string, data []byte) (string, error) {
	obj := s.bucket.Object(path).If(storage.Conditions{DoesNotExist: false})
	w := obj.NewWriter(ctx)
	w.ContentType = contentType
	w.CacheControl = "public, max-age=31536000"
	w.ChunkSize = 0
	_, err := w.Write(data)
	if err != nil {
		_ = w.Close()
		return "", err
	}
	if err := w.Close(); err != nil {
		return "", err
	}
	// Note: Bucket should be publicly readable or served via CDN in MEDIA_BASE_URL
	return fmt.Sprintf("%s/%s", s.base, strings.TrimLeft(path, "/")), nil
}
func (s *gcsStore) Delete(ctx context.Context, path string) error {
	return s.bucket.Object(path).Delete(ctx)
}

func DatedKey(prefix, ext string) string {
	d := time.Now().UTC()
	return fmt.Sprintf("%s/%04d/%02d/%s%s", strings.TrimRight(prefix, "/"), d.Year(), d.Month(), RandHex(16), ext)
}
