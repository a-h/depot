package storage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"path/filepath"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/feature/s3/transfermanager"
	"github.com/aws/aws-sdk-go-v2/service/s3"
	"github.com/aws/aws-sdk-go-v2/service/s3/types"
)

var _ Storage = (*S3)(nil)

type S3Config struct {
	Bucket          string
	Prefix          string
	Region          string
	Endpoint        string
	AccessKeyID     string
	SecretAccessKey string
	ForcePathStyle  bool
}

type S3 struct {
	client   *s3.Client
	uploader *transfermanager.Client
	bucket   string
	prefix   string
}

func NewS3(ctx context.Context, cfg S3Config) (*S3, error) {
	var opts []func(*config.LoadOptions) error

	if cfg.Region != "" {
		opts = append(opts, config.WithRegion(cfg.Region))
	}

	if cfg.AccessKeyID != "" && cfg.SecretAccessKey != "" {
		opts = append(opts, config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(cfg.AccessKeyID, cfg.SecretAccessKey, ""),
		))
	}

	awsCfg, err := config.LoadDefaultConfig(ctx, opts...)
	if err != nil {
		return nil, fmt.Errorf("failed to load AWS config: %w", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		if cfg.Endpoint != "" {
			o.BaseEndpoint = aws.String(cfg.Endpoint)
		}
		o.UsePathStyle = cfg.ForcePathStyle
	})

	uploader := transfermanager.New(s3Client)

	return &S3{
		client:   s3Client,
		uploader: uploader,
		bucket:   cfg.Bucket,
		prefix:   cfg.Prefix,
	}, nil
}

func (s *S3) Stat(ctx context.Context, filename string) (size int64, exists bool, err error) {
	output, err := s.client.HeadObject(ctx, &s3.HeadObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(filepath.Join(s.prefix, filename)),
	})
	if err != nil {
		var notFound *types.NotFound
		if errors.As(err, &notFound) {
			return 0, false, nil
		}
		return 0, false, err
	}
	if output.ContentLength == nil {
		return 0, true, nil
	}
	return *output.ContentLength, true, nil
}

func (s *S3) Get(ctx context.Context, filename string) (r io.ReadCloser, exists bool, err error) {
	output, err := s.client.GetObject(ctx, &s3.GetObjectInput{
		Bucket: aws.String(s.bucket),
		Key:    aws.String(filepath.Join(s.prefix, filename)),
	})
	if err != nil {
		var notFound *types.NoSuchKey
		if errors.As(err, &notFound) {
			return nil, false, nil
		}
		return nil, false, err
	}
	return output.Body, true, nil
}

func (s *S3) Put(ctx context.Context, filename string) (w io.WriteCloser, err error) {
	pr, pw := io.Pipe()

	go func() {
		_, err := s.uploader.UploadObject(ctx, &transfermanager.UploadObjectInput{
			Bucket: aws.String(s.bucket),
			Key:    aws.String(filepath.Join(s.prefix, filename)),
			Body:   pr,
		})
		if err != nil {
			pr.CloseWithError(fmt.Errorf("failed to upload to S3: %w", err))
			return
		}
		pr.Close()
	}()

	return pw, nil
}
