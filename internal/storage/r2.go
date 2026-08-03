package storage

import (
	"bytes"
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	awsconfig "github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"

	appconfig "wordbit-advanced-app/backend/internal/config"
)

// R2Storage uses Cloudflare R2's S3-compatible API. The SDK owns SigV4
// canonicalization, including download presigning, so mobile replay URLs work
// for object keys containing user and word identifiers.
type R2Storage struct {
	bucket  string
	client  *s3.Client
	presign *s3.PresignClient
}

func NewR2Storage(ctx context.Context, cfg appconfig.R2Config) (*R2Storage, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	endpoint := strings.TrimRight(cfg.Endpoint, "/")
	awsCfg, err := awsconfig.LoadDefaultConfig(ctx,
		awsconfig.WithRegion("auto"),
		awsconfig.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, "")),
	)
	if err != nil {
		return nil, fmt.Errorf("load R2 configuration: %w", err)
	}
	client := s3.NewFromConfig(awsCfg, func(options *s3.Options) {
		options.BaseEndpoint = aws.String(endpoint)
		options.UsePathStyle = true
	})
	return &R2Storage{
		bucket:  cfg.Bucket,
		client:  client,
		presign: s3.NewPresignClient(client),
	}, nil
}

func (s *R2Storage) Put(ctx context.Context, objectKey string, contentType string, data []byte) error {
	_, err := s.client.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(s.bucket),
		Key:         aws.String(objectKey),
		Body:        bytes.NewReader(data),
		ContentType: aws.String(contentType),
	})
	if err != nil {
		return fmt.Errorf("put R2 object: %w", err)
	}
	return nil
}

func (s *R2Storage) PresignDownload(ctx context.Context, objectKey string, expiresIn time.Duration) (string, error) {
	request, err := s.presign.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(objectKey),
	}, s3.WithPresignExpires(expiresIn))
	if err != nil {
		return "", fmt.Errorf("presign R2 download: %w", err)
	}
	return request.URL, nil
}
