package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
	"github.com/sirupsen/logrus"
)

// Client wraps an S3-compatible storage client.
type Client struct {
	s3     *s3.Client
	bucket string
	log    *logrus.Logger
}

// Config holds S3-compatible storage configuration.
type Config struct {
	Endpoint  string
	Bucket    string
	AccessKey string
	SecretKey string
	Region    string
}

// NewClient creates a new S3-compatible storage client.
func NewClient(cfg Config, log *logrus.Logger) (*Client, error) {
	region := cfg.Region
	if region == "" {
		region = "auto" // Cloudflare R2 uses "auto"
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

// Upload uploads a file to S3-compatible storage using multipart upload for large files.
func (c *Client) Upload(ctx context.Context, key string, filePath string) (string, error) {
	file, err := os.Open(filePath)
	if err != nil {
		return "", fmt.Errorf("failed to open file: %w", err)
	}
	defer file.Close()

	fileInfo, err := file.Stat()
	if err != nil {
		return "", fmt.Errorf("failed to stat file: %w", err)
	}

	// Use multipart upload for files > 5MB
	if fileInfo.Size() > 5*1024*1024 {
		return c.multipartUpload(ctx, key, file, fileInfo.Size())
	}

	// Simple upload for smaller files
	_, err = c.s3.PutObject(ctx, &s3.PutObjectInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		Body:        file,
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to upload: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"key":  key,
		"size": fileInfo.Size(),
	}).Info("File uploaded to storage")

	return fmt.Sprintf("s3://%s/%s", c.bucket, key), nil
}

// multipartUpload handles large file uploads using S3 multipart upload.
func (c *Client) multipartUpload(ctx context.Context, key string, file *os.File, fileSize int64) (string, error) {
	const partSize = 5 * 1024 * 1024 // 5MB parts

	createResp, err := c.s3.CreateMultipartUpload(ctx, &s3.CreateMultipartUploadInput{
		Bucket:      aws.String(c.bucket),
		Key:         aws.String(key),
		ContentType: aws.String("video/mp4"),
	})
	if err != nil {
		return "", fmt.Errorf("failed to create multipart upload: %w", err)
	}

	var completedParts []types.CompletedPart
	buffer := make([]byte, partSize)
	partNumber := int32(1)

	for {
		n, readErr := file.Read(buffer)
		if n == 0 {
			break
		}

		uploadResp, uploadErr := c.s3.UploadPart(ctx, &s3.UploadPartInput{
			Bucket:        aws.String(c.bucket),
			Key:           aws.String(key),
			UploadId:      createResp.UploadId,
			PartNumber:    aws.Int32(partNumber),
			Body:          bytes.NewReader(buffer[:n]),
			ContentLength: aws.Int64(int64(n)),
		})
		if uploadErr != nil {
			c.s3.AbortMultipartUpload(ctx, &s3.AbortMultipartUploadInput{
				Bucket:   aws.String(c.bucket),
				Key:      aws.String(key),
				UploadId: createResp.UploadId,
			})
			return "", fmt.Errorf("failed to upload part %d: %w", partNumber, uploadErr)
		}

		completedParts = append(completedParts, types.CompletedPart{
			ETag:       uploadResp.ETag,
			PartNumber: aws.Int32(partNumber),
		})

		partNumber++
		if readErr == io.EOF {
			break
		}
	}

	_, err = c.s3.CompleteMultipartUpload(ctx, &s3.CompleteMultipartUploadInput{
		Bucket:   aws.String(c.bucket),
		Key:      aws.String(key),
		UploadId: createResp.UploadId,
		MultipartUpload: &types.CompletedMultipartUpload{
			Parts: completedParts,
		},
	})
	if err != nil {
		return "", fmt.Errorf("failed to complete multipart upload: %w", err)
	}

	c.log.WithFields(logrus.Fields{
		"key":   key,
		"size":  fileSize,
		"parts": partNumber - 1,
	}).Info("Multipart upload completed")

	return fmt.Sprintf("s3://%s/%s", c.bucket, key), nil
}

// Delete removes an object from S3-compatible storage.
func (c *Client) Delete(ctx context.Context, key string) error {
	_, err := c.s3.DeleteObject(ctx, &s3.DeleteObjectInput{
		Bucket: aws.String(c.bucket),
		Key:    aws.String(key),
	})
	if err != nil {
		return fmt.Errorf("failed to delete object: %w", err)
	}

	c.log.WithField("key", key).Info("File deleted from storage")
	return nil
}

// GetPresignedURL generates a pre-signed URL for downloading a file.
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
