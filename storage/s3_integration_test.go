package storage

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/aws/aws-sdk-go-v2/aws"
	"github.com/aws/aws-sdk-go-v2/config"
	"github.com/aws/aws-sdk-go-v2/credentials"
	"github.com/aws/aws-sdk-go-v2/service/s3"
)

func TestS3Storage(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test")
	}

	ctx := context.Background()
	accessKeyID := os.Getenv("AWS_ACCESS_KEY_ID")
	if accessKeyID == "" {
		accessKeyID = "minioadmin"
	}
	secretAccessKey := os.Getenv("AWS_SECRET_ACCESS_KEY")
	if secretAccessKey == "" {
		secretAccessKey = "minioadmin123"
	}
	region := os.Getenv("AWS_REGION")
	if region == "" {
		region = "us-east-1"
	}

	endpoint := os.Getenv("AWS_ENDPOINT_URL")
	if endpoint == "" {
		var err error
		endpoint, err = startMinIO(ctx, t, accessKeyID, secretAccessKey)
		if err != nil {
			t.Skipf("skipping integration test: %v", err)
		}
	}

	testBucket := "depot-test-bucket"

	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion(region),
		config.WithCredentialsProvider(
			credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, ""),
		),
	)
	if err != nil {
		t.Fatalf("failed to load AWS config: %v", err)
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})

	if err := waitForS3(ctx, s3Client); err != nil {
		t.Fatalf("s3 endpoint not available: %v", err)
	}

	_, err = s3Client.CreateBucket(ctx, &s3.CreateBucketInput{
		Bucket: aws.String(testBucket),
	})
	if err != nil {
		t.Fatalf("failed to create test bucket: %v", err)
	}

	t.Cleanup(func() {
		listResp, _ := s3Client.ListObjectsV2(ctx, &s3.ListObjectsV2Input{
			Bucket: aws.String(testBucket),
		})
		if listResp != nil {
			for _, obj := range listResp.Contents {
				s3Client.DeleteObject(ctx, &s3.DeleteObjectInput{
					Bucket: aws.String(testBucket),
					Key:    obj.Key,
				})
			}
		}
		s3Client.DeleteBucket(ctx, &s3.DeleteBucketInput{
			Bucket: aws.String(testBucket),
		})
	})

	storage, err := NewS3(ctx, S3Config{
		Bucket:          testBucket,
		Region:          region,
		Endpoint:        endpoint,
		AccessKeyID:     accessKeyID,
		SecretAccessKey: secretAccessKey,
		ForcePathStyle:  true,
	})
	if err != nil {
		t.Fatalf("failed to create S3 storage: %v", err)
	}

	t.Run("stat non-existing file returns not found", func(t *testing.T) {
		size, exists, err := storage.Stat(ctx, "non-existing-file")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if exists {
			t.Errorf("expected exists=false, got true")
		}
		if size != 0 {
			t.Errorf("expected size=0, got %d", size)
		}
	})

	t.Run("get non-existing file returns not found", func(t *testing.T) {
		r, exists, err := storage.Get(ctx, "non-existing-file")
		if err != nil {
			t.Errorf("unexpected error: %v", err)
		}
		if exists {
			t.Errorf("expected exists=false, got true")
		}
		if r != nil {
			t.Errorf("expected nil reader, got non-nil")
			r.Close()
		}
	})

	t.Run("put and get file", func(t *testing.T) {
		testFile := "test-file.txt"
		testContent := []byte("hello world")

		w, err := storage.Put(ctx, testFile)
		if err != nil {
			t.Fatalf("failed to create writer: %v", err)
		}

		n, err := w.Write(testContent)
		if err != nil {
			t.Fatalf("failed to write: %v", err)
		}
		if n != len(testContent) {
			t.Errorf("expected to write %d bytes, wrote %d", len(testContent), n)
		}

		if err := w.Close(); err != nil {
			t.Fatalf("failed to close writer: %v", err)
		}

		size, exists, err := storage.Stat(ctx, testFile)
		if err != nil {
			t.Errorf("stat failed: %v", err)
		}
		if !exists {
			t.Errorf("expected exists=true, got false")
		}
		if size != int64(len(testContent)) {
			t.Errorf("expected size=%d, got %d", len(testContent), size)
		}

		r, exists, err := storage.Get(ctx, testFile)
		if err != nil {
			t.Fatalf("failed to get file: %v", err)
		}
		if !exists {
			t.Fatalf("expected exists=true, got false")
		}
		defer r.Close()

		content, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("failed to read content: %v", err)
		}

		if !bytes.Equal(content, testContent) {
			t.Errorf("expected content %q, got %q", testContent, content)
		}
	})

	t.Run("put large file", func(t *testing.T) {
		testFile := "large-file.bin"
		testContent := make([]byte, 1024*1024)
		for i := range testContent {
			testContent[i] = byte(i % 256)
		}

		w, err := storage.Put(ctx, testFile)
		if err != nil {
			t.Fatalf("failed to create writer: %v", err)
		}

		n, err := w.Write(testContent)
		if err != nil {
			t.Fatalf("failed to write: %v", err)
		}
		if n != len(testContent) {
			t.Errorf("expected to write %d bytes, wrote %d", len(testContent), n)
		}

		if err := w.Close(); err != nil {
			t.Fatalf("failed to close writer: %v", err)
		}

		size, exists, err := storage.Stat(ctx, testFile)
		if err != nil {
			t.Errorf("stat failed: %v", err)
		}
		if !exists {
			t.Errorf("expected exists=true, got false")
		}
		if size != int64(len(testContent)) {
			t.Errorf("expected size=%d, got %d", len(testContent), size)
		}

		r, exists, err := storage.Get(ctx, testFile)
		if err != nil {
			t.Fatalf("failed to get file: %v", err)
		}
		if !exists {
			t.Fatalf("expected exists=true, got false")
		}
		defer r.Close()

		content, err := io.ReadAll(r)
		if err != nil {
			t.Fatalf("failed to read content: %v", err)
		}

		if !bytes.Equal(content, testContent) {
			t.Errorf("content mismatch, expected %d bytes, got %d bytes", len(testContent), len(content))
		}
	})
}

func waitForS3(ctx context.Context, client *s3.Client) error {
	timeout := time.After(10 * time.Second)
	for {
		select {
		case <-timeout:
			return exec.ErrNotFound
		case <-time.After(500 * time.Millisecond):
			_, err := client.ListBuckets(ctx, &s3.ListBucketsInput{})
			if err == nil {
				return nil
			}
		}
	}
}

func startMinIO(ctx context.Context, t *testing.T, accessKeyID, secretAccessKey string) (endpoint string, err error) {
	t.Helper()

	minioCommand, err := getMinIOCommand()
	if err != nil {
		return "", err
	}

	// Find an available port.
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", err
	}
	addr := listener.Addr().String()
	listener.Close()

	dataDir := t.TempDir()
	minioCtx, minioCancel := context.WithCancel(ctx)
	defer func() {
		if err != nil {
			minioCancel()
		}
	}()
	cmd := minioCommand(minioCtx, "server", "--address", addr, dataDir)
	cmd.Env = append(os.Environ(),
		"MINIO_ROOT_USER="+accessKeyID,
		"MINIO_ROOT_PASSWORD="+secretAccessKey,
	)
	output := &bytes.Buffer{}
	cmd.Stdout = output
	cmd.Stderr = output

	if err := cmd.Start(); err != nil {
		return "", err
	}

	t.Cleanup(func() {
		minioCancel()
		if cmd.Process != nil {
			_ = cmd.Process.Kill()
		}
		_ = cmd.Wait()
	})

	endpoint = "http://" + addr
	awsCfg, err := config.LoadDefaultConfig(ctx,
		config.WithRegion("us-east-1"),
		config.WithCredentialsProvider(credentials.NewStaticCredentialsProvider(accessKeyID, secretAccessKey, "")),
	)
	if err != nil {
		return "", err
	}

	s3Client := s3.NewFromConfig(awsCfg, func(o *s3.Options) {
		o.BaseEndpoint = aws.String(endpoint)
		o.UsePathStyle = true
	})
	if err := waitForS3(ctx, s3Client); err != nil {
		return "", fmt.Errorf("failed to start minio: %w: %s", err, bytes.TrimSpace(output.Bytes()))
	}

	return endpoint, nil
}

func getMinIOCommand() (func(ctx context.Context, args ...string) *exec.Cmd, error) {
	if minioPath, err := exec.LookPath("minio"); err == nil {
		return func(ctx context.Context, args ...string) *exec.Cmd {
			return exec.CommandContext(ctx, minioPath, args...)
		}, nil
	}
	if _, err := exec.LookPath("nix"); err == nil {
		return func(ctx context.Context, args ...string) *exec.Cmd {
			nixArgs := append([]string{"run", "nixpkgs#minio", "--"}, args...)
			return exec.CommandContext(ctx, "nix", nixArgs...)
		}, nil
	}
	return nil, exec.ErrNotFound
}
