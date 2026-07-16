package system

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3BackupConfig struct {
	Endpoint, Region, Bucket, Prefix string
	AccessKey, SecretKey             string
	PathStyle                        bool
	RetentionDays, MaxRetained       int
}

type S3BackupStore struct {
	client *s3.Client
	config S3BackupConfig
}

type S3BackupObject struct {
	Key          string    `json:"key"`
	ID           string    `json:"id"`
	SizeBytes    int64     `json:"size_bytes"`
	LastModified time.Time `json:"last_modified"`
}

func NewS3BackupStore(ctx context.Context, config S3BackupConfig) (*S3BackupStore, error) {
	if strings.TrimSpace(config.Region) == "" {
		config.Region = "auto"
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx, awsconfig.WithRegion(config.Region), awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, "")))
	if err != nil {
		return nil, fmt.Errorf("load S3 config: %w", err)
	}
	client := s3.NewFromConfig(loaded, func(options *s3.Options) {
		if config.Endpoint != "" {
			options.BaseEndpoint = aws.String(config.Endpoint)
		}
		options.UsePathStyle = config.PathStyle
	})
	return &S3BackupStore{client: client, config: config}, nil
}

func (s *S3BackupStore) Test(ctx context.Context) error {
	_, err := s.client.HeadBucket(ctx, &s3.HeadBucketInput{Bucket: aws.String(s.config.Bucket)})
	if err != nil {
		return fmt.Errorf("S3 connection test failed: %w", err)
	}
	return nil
}

func (s *S3BackupStore) Upload(ctx context.Context, id string, body io.Reader) (string, error) {
	if !validArchiveID(id, backupPrefix) || body == nil {
		return "", ErrBackupInvalid
	}
	key := strings.Trim(s.config.Prefix, "/")
	if key != "" {
		key += "/"
	}
	key += id + ".tar.gz"
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key), Body: body, ContentType: aws.String("application/gzip")})
	if err != nil {
		return "", fmt.Errorf("upload S3 backup: %w", err)
	}
	return key, nil
}

func (s *S3BackupStore) List(ctx context.Context) ([]S3BackupObject, error) {
	prefix := strings.Trim(s.config.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	out := make([]S3BackupObject, 0)
	paginator := s3.NewListObjectsV2Paginator(s.client, &s3.ListObjectsV2Input{Bucket: aws.String(s.config.Bucket), Prefix: aws.String(prefix)})
	for paginator.HasMorePages() {
		page, err := paginator.NextPage(ctx)
		if err != nil {
			return nil, fmt.Errorf("list S3 backups: %w", err)
		}
		for _, object := range page.Contents {
			key := aws.ToString(object.Key)
			name := filepath.Base(key)
			if !strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, ".tar.gz") {
				continue
			}
			out = append(out, S3BackupObject{Key: key, ID: strings.TrimSuffix(name, ".tar.gz"), SizeBytes: aws.ToInt64(object.Size), LastModified: aws.ToTime(object.LastModified)})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].LastModified.After(out[j].LastModified) })
	return out, nil
}

func (s *S3BackupStore) Download(ctx context.Context, key string) (io.ReadCloser, error) {
	prefix := strings.Trim(s.config.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	name := filepath.Base(key)
	expectedDir := strings.TrimSuffix(prefix, "/")
	if expectedDir == "" {
		expectedDir = "."
	}
	if key == "" || filepath.Dir(key) != expectedDir ||
		!strings.HasPrefix(name, backupPrefix) || !strings.HasSuffix(name, ".tar.gz") ||
		strings.Contains(key, "..") {
		return nil, errors.New("S3 backup key is invalid")
	}
	result, err := s.client.GetObject(ctx, &s3.GetObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(key)})
	if err != nil {
		return nil, fmt.Errorf("download S3 backup: %w", err)
	}
	return result.Body, nil
}

func (s *S3BackupStore) Cleanup(ctx context.Context, now time.Time) error {
	prefix := strings.Trim(s.config.Prefix, "/")
	if prefix != "" {
		prefix += "/"
	}
	result, err := s.client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{Bucket: aws.String(s.config.Bucket), Prefix: aws.String(prefix)})
	if err != nil {
		return fmt.Errorf("list S3 backups: %w", err)
	}
	objects := result.Contents
	sort.Slice(objects, func(i, j int) bool {
		return aws.ToTime(objects[i].LastModified).After(aws.ToTime(objects[j].LastModified))
	})
	cutoff := now.AddDate(0, 0, -s.config.RetentionDays)
	for index, object := range objects {
		if (s.config.MaxRetained > 0 && index >= s.config.MaxRetained) || (s.config.RetentionDays > 0 && aws.ToTime(object.LastModified).Before(cutoff)) {
			if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: object.Key}); err != nil {
				return fmt.Errorf("delete expired S3 backup: %w", err)
			}
		}
	}
	return nil
}
