package destination

import (
	"context"
	"fmt"
	"io"
	"sort"
	"strings"

	"backuper/internal/config"
	"backuper/internal/secrets"

	"github.com/minio/minio-go/v7"
	"github.com/minio/minio-go/v7/pkg/credentials"
)

// S3Destination transfers files to S3-compatible storage (AWS S3, Minio, etc.)
type S3Destination struct {
	cfg   *config.DestinationConfig
	store secrets.Store
}

func newS3(cfg *config.DestinationConfig, store secrets.Store) (*S3Destination, error) {
	return &S3Destination{cfg: cfg, store: store}, nil
}

func (d *S3Destination) Name() string { return d.cfg.Name }
func (d *S3Destination) Type() string { return "s3" }

// client creates a new S3 client with the configured credentials
func (d *S3Destination) client() (*minio.Client, error) {
	// Get credentials from secrets store
	accessKey, err := d.store.Get(d.cfg.AccessKeyRef)
	if err != nil {
		return nil, fmt.Errorf("getting access key: %w", err)
	}
	secretKey, err := d.store.Get(d.cfg.SecretKeyRef)
	if err != nil {
		return nil, fmt.Errorf("getting secret key: %w", err)
	}

	// Get session token if provided (for temporary credentials)
	sessionToken := ""
	if d.cfg.SessionTokenRef != "" {
		sessionToken, err = d.store.Get(d.cfg.SessionTokenRef)
		if err != nil {
			return nil, fmt.Errorf("getting session token: %w", err)
		}
	}

	// Build credential options
	opts := &minio.Options{
		Creds:  credentials.NewStaticV4(accessKey, secretKey, sessionToken),
		Secure: d.cfg.UseSSL,
	}

	// Determine endpoint: use custom endpoint for Minio/self-hosted S3
	endpoint := d.cfg.Endpoint
	if endpoint == "" {
		// AWS S3 default endpoint
		if d.cfg.Region == "" {
			endpoint = "s3.amazonaws.com"
		} else {
			endpoint = fmt.Sprintf("s3.%s.amazonaws.com", d.cfg.Region)
		}
	}

	client, err := minio.New(endpoint, opts)
	if err != nil {
		return nil, fmt.Errorf("creating s3 client: %w", err)
	}

	return client, nil
}

// Transfer uploads the file to S3
func (d *S3Destination) Transfer(ctx context.Context, localPath string, targetDir string) error {
	client, err := d.client()
	if err != nil {
		return err
	}

	// Build S3 object key: prefix with targetDir if specified
	objectName := d.cfg.RemotePath
	if targetDir != "" {
		objectName = targetDir + "/" + d.cfg.RemotePath
	}
	// Add the filename
	objectName = strings.TrimPrefix(objectName, "/") // Remove leading slash
	objectName = objectName + "/" + strings.TrimPrefix(localPath, "/")

	// Upload the file
	_, err = client.FPutObject(ctx, d.cfg.Bucket, objectName, localPath, minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	if err != nil {
		return fmt.Errorf("uploading to s3: %w", err)
	}

	return nil
}

// ListFiles lists all backup files for a target in the S3 bucket
func (d *S3Destination) ListFiles(ctx context.Context, targetName string) ([]string, error) {
	client, err := d.client()
	if err != nil {
		return nil, err
	}

	// Build prefix for listing
	prefix := d.cfg.RemotePath
	prefix = strings.TrimPrefix(prefix, "/")
	if prefix != "" && !strings.HasSuffix(prefix, "/") {
		prefix += "/"
	}
	prefix += targetName + "_"

	// List objects
	objectCh := client.ListObjects(ctx, d.cfg.Bucket, minio.ListObjectsOptions{
		Prefix:    prefix,
		Recursive: true,
	})

	var files []string
	for object := range objectCh {
		if object.Err != nil {
			return nil, fmt.Errorf("listing s3 objects: %w", object.Err)
		}
		// Return full S3 URI for compatibility with delete operation
		files = append(files, "s3://"+d.cfg.Bucket+"/"+object.Key)
	}

	sort.Strings(files)
	return files, nil
}

// DeleteFile deletes a file from S3
func (d *S3Destination) DeleteFile(ctx context.Context, filename string) error {
	client, err := d.client()
	if err != nil {
		return err
	}

	// Parse S3 URI format: s3://bucket/key
	bucket := d.cfg.Bucket
	objectKey := strings.TrimPrefix(filename, "s3://"+bucket+"/")

	// Also handle if filename is just the object key
	if strings.HasPrefix(filename, "s3://") {
		parts := strings.TrimPrefix(filename, "s3://")
		idx := strings.Index(parts, "/")
		if idx > 0 {
			bucket = parts[:idx]
			objectKey = parts[idx+1:]
		}
	}

	err = client.RemoveObject(ctx, bucket, objectKey, minio.RemoveObjectOptions{})
	if err != nil {
		return fmt.Errorf("deleting s3 object %q: %w", filename, err)
	}

	return nil
}

// UploadWithProgress uploads a file to S3 with progress tracking using io.Reader
func (d *S3Destination) UploadWithProgress(ctx context.Context, reader io.Reader, size int64, targetDir string, objectName string) error {
	client, err := d.client()
	if err != nil {
		return err
	}

	// Build S3 object key
	key := d.cfg.RemotePath
	if targetDir != "" {
		key = targetDir + "/" + key
	}
	key = strings.TrimPrefix(key, "/")
	if key != "" && !strings.HasSuffix(key, "/") {
		key += "/"
	}
	key += objectName

	_, err = client.PutObject(ctx, d.cfg.Bucket, key, reader, size, minio.PutObjectOptions{
		ContentType: "application/gzip",
	})
	if err != nil {
		return fmt.Errorf("uploading to s3: %w", err)
	}

	return nil
}
