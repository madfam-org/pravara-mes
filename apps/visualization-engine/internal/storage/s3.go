package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"path/filepath"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/sirupsen/logrus"
)

// Config holds S3-compatible storage configuration.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
}

// Client wraps an S3-compatible storage client for 3D model files.
type Client struct {
	s3     *s3.Client
	bucket string
	log    *logrus.Logger
}

// NewClient creates a new S3-compatible storage client.
func NewClient(cfg Config, log *logrus.Logger) (*Client, error) {
	region := cfg.Region
	if region == "" {
		region = "auto"
	}

	resolver := aws.EndpointResolverWithOptionsFunc(
		func(service, resolvedRegion string, options ...interface{}) (aws.Endpoint, error) {
			return aws.Endpoint{
				URL:               cfg.Endpoint,
				HostnameImmutable: true,
			}, nil
		},
	)

	awsCfg, err := config.LoadDefaultConfig(context.Background(),
		config.WithRegion(region),
		config.WithEndpointResolverWithOptions(resolver),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKey, cfg.SecretKey, ""),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	return &Client{
		s3:     s3.NewFromConfig(awsCfg),
		bucket: cfg.Bucket,
		log:    log,
	}, nil
}

// AllowedModelExtensions lists the valid 3D model file extensions.
var AllowedModelExtensions = map[string]string{
	".gltf": "model/gltf+json",
	".glb":  "model/gltf-binary",
	".stl":  "model/stl",
}

// ValidateModelFile checks if the file extension is a supported 3D model format.
func ValidateModelFile(filename string) (string, error) {
	ext := filepath.Ext(filename)
	contentType, ok := AllowedModelExtensions[ext]
	if !ok {
		return "", fmt.Errorf("unsupported file type %q; allowed: .gltf, .glb, .stl", ext)
	}
	// Fallback to generic content type if mime doesn't know the extension
	if ct := mime.TypeByExtension(ext); ct != "" {
		contentType = ct
	}
	return contentType, nil
}

// UploadModel uploads a 3D model file to S3 storage.
func (c *Client) UploadModel(ctx context.Context, key string, data io.Reader, size int64, contentType string) (string, error) {
	buf, err := io.ReadAll(data)
	if err != nil {
		return "", fmt.Errorf("failed to read model data: %w", err)
	}

	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:        aws.String(c.bucket),
		Key:           aws.String(key),
		Body:          bytes.NewReader(buf),
		ContentType:   aws.String(contentType),
		ContentLength: aws.Int64(int64(len(buf))),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload model: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"key":  key,
		"size": len(buf),
		"type": contentType,
	}).Info("3D model uploaded to storage")

	return fmt.Sprintf("s3://%s/%s", c.bucket, key), nil
}

// GetPresignedURL generates a pre-signed URL for downloading a model.
func (c *Client) GetPresignedURL(ctx context.Context, key string, expiry time.Duration) (string, error) {
	presignClient := s3.NewPresignClient(c.s3)

	resp, err := presignClient.PresignGetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	}, s3.WithPresignExpires(expiry))
	if err != nil {
		return "", fmt.Errorf("failed to generate presigned URL: %w", err)
	}

	return resp.URL, nil
}

// Delete removes a model from storage.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete model: %w", err)
	}
	return nil
}
