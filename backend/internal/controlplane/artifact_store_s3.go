package controlplane

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

type S3ArtifactStoreConfig struct {
	Endpoint  string
	Region    string
	Bucket    string
	Prefix    string
	AccessKey string
	SecretKey string
	PathStyle bool
}

type artifactS3Client interface {
	PutObject(context.Context, *s3.PutObjectInput, ...func(*s3.Options)) (*s3.PutObjectOutput, error)
	GetObject(context.Context, *s3.GetObjectInput, ...func(*s3.Options)) (*s3.GetObjectOutput, error)
	DeleteObject(context.Context, *s3.DeleteObjectInput, ...func(*s3.Options)) (*s3.DeleteObjectOutput, error)
}

type S3ArtifactStore struct {
	client artifactS3Client
	config S3ArtifactStoreConfig
}

func NewS3ArtifactStore(ctx context.Context, config S3ArtifactStoreConfig) (*S3ArtifactStore, error) {
	config.Region = strings.TrimSpace(config.Region)
	if config.Region == "" {
		config.Region = "auto"
	}
	config.Bucket = strings.TrimSpace(config.Bucket)
	config.Prefix = strings.Trim(strings.TrimSpace(config.Prefix), "/")
	if config.Bucket == "" || strings.TrimSpace(config.AccessKey) == "" || strings.TrimSpace(config.SecretKey) == "" {
		return nil, errors.New("S3 artifact bucket, access key, and secret key are required")
	}
	loaded, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion(config.Region),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(config.AccessKey, config.SecretKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load S3 artifact config: %w", err)
	}
	client := s3.NewFromConfig(loaded, func(options *s3.Options) {
		if strings.TrimSpace(config.Endpoint) != "" {
			options.BaseEndpoint = aws.String(strings.TrimSpace(config.Endpoint))
		}
		options.UsePathStyle = config.PathStyle
	})
	return &S3ArtifactStore{client: client, config: config}, nil
}

func (*S3ArtifactStore) Driver() string { return ArtifactStoreDriverS3 }

func (s *S3ArtifactStore) Put(ctx context.Context, key string, body io.Reader, sizeBytes int64, mediaType string) (int64, error) {
	if err := validateArtifactStoreWrite(key, body, sizeBytes); err != nil {
		return 0, err
	}
	input := &s3.PutObjectInput{
		Bucket: aws.String(s.config.Bucket), Key: aws.String(s.objectKey(key)), Body: body,
		ContentType: aws.String(strings.TrimSpace(mediaType)),
	}
	if sizeBytes >= 0 {
		input.ContentLength = aws.Int64(sizeBytes)
	}
	if _, err := s.client.PutObject(ctx, input); err != nil {
		return 0, fmt.Errorf("put S3 artifact: %w", err)
	}
	return sizeBytes, nil
}

func (s *S3ArtifactStore) Open(ctx context.Context, key string, byteRange *ArtifactByteRange) (ArtifactRead, error) {
	if !validArtifactStoreKey(key) {
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	input := &s3.GetObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(s.objectKey(key))}
	if byteRange != nil {
		if byteRange.Offset < 0 || byteRange.Length < 0 {
			return ArtifactRead{}, ErrArtifactUnavailable
		}
		end := ""
		if byteRange.Length > 0 {
			end = strconv.FormatInt(byteRange.Offset+byteRange.Length-1, 10)
		}
		input.Range = aws.String("bytes=" + strconv.FormatInt(byteRange.Offset, 10) + "-" + end)
	}
	result, err := s.client.GetObject(ctx, input)
	if err != nil {
		return ArtifactRead{}, fmt.Errorf("get S3 artifact: %w", err)
	}
	offset := int64(0)
	total := aws.ToInt64(result.ContentLength)
	if byteRange != nil {
		offset = byteRange.Offset
		if parsed, ok := parseS3ContentRange(aws.ToString(result.ContentRange)); ok {
			total = parsed
		}
	}
	size := aws.ToInt64(result.ContentLength)
	if size < 0 || total < size {
		_ = result.Body.Close()
		return ArtifactRead{}, ErrArtifactUnavailable
	}
	return ArtifactRead{Body: result.Body, Offset: offset, SizeBytes: size, TotalBytes: total}, nil
}

func (s *S3ArtifactStore) Delete(ctx context.Context, key string) error {
	if !validArtifactStoreKey(key) {
		return ErrArtifactUnavailable
	}
	if _, err := s.client.DeleteObject(ctx, &s3.DeleteObjectInput{Bucket: aws.String(s.config.Bucket), Key: aws.String(s.objectKey(key))}); err != nil {
		return fmt.Errorf("delete S3 artifact: %w", err)
	}
	return nil
}

func (s *S3ArtifactStore) objectKey(key string) string {
	if s.config.Prefix == "" {
		return key
	}
	return s.config.Prefix + "/" + key
}

func parseS3ContentRange(value string) (int64, bool) {
	value = strings.TrimSpace(value)
	separator := strings.LastIndexByte(value, '/')
	if !strings.HasPrefix(value, "bytes ") || separator < 0 || separator == len(value)-1 {
		return 0, false
	}
	total, err := strconv.ParseInt(value[separator+1:], 10, 64)
	return total, err == nil && total >= 0
}
