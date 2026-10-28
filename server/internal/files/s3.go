package files

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"net/http"
	"path"
	"sort"
	"strings"
	"time"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// ---------- S3 and S3-compatible services (v1.7.1) ----------

// S3Config is where an S3 storage is. Secrets come from the environment
// only (config.Storage) and never show in Describe.
type S3Config struct {
	Endpoint  string // host[:port]
	Secure    bool   // https
	Region    string
	Bucket    string
	Prefix    string // keys go under it, e.g. "kept"
	AccessKey string
	SecretKey string
	PathStyle bool // bucket in the path (MinIO, most NAS) rather than the host name
	// Transport replaces the HTTP transport (tests).
	Transport http.RoundTripper
}

// S3Backend keeps objects in a bucket of an S3-compatible service: AWS S3,
// MinIO, Aliyun OSS, Tencent COS, Cloudflare R2, a NAS's S3 service.
type S3Backend struct {
	c      *minio.Client
	bucket string
	prefix string
	desc   string
}

func NewS3(cfg S3Config) (*S3Backend, error) {
	lookup := minio.BucketLookupAuto
	if cfg.PathStyle {
		lookup = minio.BucketLookupPath
	}
	tr := cfg.Transport
	if tr == nil {
		t, err := minio.DefaultTransport(cfg.Secure)
		if err != nil {
			return nil, err
		}
		// A storage that stops answering fails the request instead of
		// hanging it; large files still stream as long as data flows.
		t.ResponseHeaderTimeout = 2 * time.Minute
		tr = t
	}
	region := cfg.Region
	if region == "" {
		region = "us-east-1" // a region saves asking the service for it
	}
	c, err := minio.New(cfg.Endpoint, &minio.Options{
		Creds: credentials.NewStaticV4(cfg.AccessKey, cfg.SecretKey, ""), Secure: cfg.Secure,
		Region: region, BucketLookup: lookup, Transport: tr,
	})
	if err != nil {
		return nil, err
	}
	scheme := "https"
	if !cfg.Secure {
		scheme = "http"
	}
	desc := "s3://" + path.Join(cfg.Bucket, cfg.Prefix) + "（" + scheme + "://" + cfg.Endpoint + "）"
	return &S3Backend{c: c, bucket: cfg.Bucket, prefix: strings.Trim(cfg.Prefix, "/"), desc: desc}, nil
}

func (b *S3Backend) name(key string) (string, error) {
	clean := path.Clean("/" + key)[1:]
	if clean == "" || clean != key {
		return "", fmt.Errorf("bad key %q", key)
	}
	if b.prefix == "" {
		return clean, nil
	}
	return b.prefix + "/" + clean, nil
}

// notFound turns the service's "no such key" into fs.ErrNotExist.
func notFound(err error) error {
	if err == nil {
		return nil
	}
	r := minio.ToErrorResponse(err)
	if r.Code == "NoSuchKey" || r.StatusCode == http.StatusNotFound {
		return fmt.Errorf("%w: %s", fs.ErrNotExist, r.Message)
	}
	return err
}

type s3File struct {
	*minio.Object
	info Info
}

func (f s3File) Info() Info { return f.info }

// Open reads an object; seeking issues range requests, so a viewer that
// loads a large PDF in pieces only downloads what it shows.
func (b *S3Backend) Open(ctx context.Context, key string) (File, error) {
	name, err := b.name(key)
	if err != nil {
		return nil, err
	}
	obj, err := b.c.GetObject(ctx, b.bucket, name, minio.GetObjectOptions{})
	if err != nil {
		return nil, notFound(err)
	}
	st, err := obj.Stat()
	if err != nil {
		obj.Close()
		return nil, notFound(err)
	}
	return s3File{Object: obj, info: Info{Size: st.Size, ModTime: st.LastModified}}, nil
}

func (b *S3Backend) Stat(ctx context.Context, key string) (Info, error) {
	name, err := b.name(key)
	if err != nil {
		return Info{}, err
	}
	st, err := b.c.StatObject(ctx, b.bucket, name, minio.StatObjectOptions{})
	if err != nil {
		return Info{}, notFound(err)
	}
	return Info{Size: st.Size, ModTime: st.LastModified}, nil
}

func (b *S3Backend) Put(ctx context.Context, key, src string) error {
	name, err := b.name(key)
	if err != nil {
		return err
	}
	// A plain body with its MD5 for the service to check, rather than the
	// chunk-signed stream used over http: NAS and other S3-compatible
	// services understand it more widely (v1.7.1).
	_, err = b.c.FPutObject(ctx, b.bucket, name, src, minio.PutObjectOptions{ContentType: "application/octet-stream",
		DisableContentSha256: true, SendContentMd5: true})
	return err
}

// Rename copies on the service and removes the old object.
func (b *S3Backend) Rename(ctx context.Context, from, to string) error {
	src, err := b.name(from)
	if err != nil {
		return err
	}
	dst, err := b.name(to)
	if err != nil {
		return err
	}
	if _, err := b.c.CopyObject(ctx, minio.CopyDestOptions{Bucket: b.bucket, Object: dst}, minio.CopySrcOptions{Bucket: b.bucket, Object: src}); err != nil {
		return notFound(err)
	}
	return b.c.RemoveObject(ctx, b.bucket, src, minio.RemoveObjectOptions{})
}

func (b *S3Backend) Remove(ctx context.Context, key string) error {
	name, err := b.name(key)
	if err != nil {
		return err
	}
	err = b.c.RemoveObject(ctx, b.bucket, name, minio.RemoveObjectOptions{})
	if errors.Is(notFound(err), fs.ErrNotExist) {
		return nil
	}
	return err
}

func (b *S3Backend) List(ctx context.Context, prefix string) ([]string, error) {
	full := prefix
	if b.prefix != "" {
		full = b.prefix + "/" + prefix
	}
	var out []string
	for obj := range b.c.ListObjects(ctx, b.bucket, minio.ListObjectsOptions{Prefix: full, Recursive: true}) {
		if obj.Err != nil {
			return nil, obj.Err
		}
		key := obj.Key
		if b.prefix != "" {
			key = strings.TrimPrefix(key, b.prefix+"/")
		}
		out = append(out, key)
	}
	sort.Strings(out)
	return out, nil
}

func (b *S3Backend) LocalPath(string) string { return "" }

func (b *S3Backend) Describe() string { return b.desc }

// Versioning reports whether the bucket keeps old versions of objects
// (v1.7.12): a backup target with it on survives overwrites and deletes.
func (b *S3Backend) Versioning(ctx context.Context) (bool, error) {
	cfg, err := b.c.GetBucketVersioning(ctx, b.bucket)
	if err != nil {
		return false, err
	}
	return cfg.Enabled(), nil
}

// EnableVersioning turns versioning on for the bucket (the keys need the
// right to change bucket settings).
func (b *S3Backend) EnableVersioning(ctx context.Context) error {
	return b.c.EnableVersioning(ctx, b.bucket)
}
