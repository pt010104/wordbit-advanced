package storage

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path"
	"sort"
	"strings"
	"time"

	appconfig "wordbit-advanced-app/backend/internal/config"
)

// R2Storage uses R2's S3-compatible API directly. It intentionally keeps the
// same private-object + presigned-download design used by Study App, without
// making the mobile client aware of R2 credentials.
type R2Storage struct {
	endpoint        *url.URL
	bucket          string
	accessKeyID     string
	secretAccessKey string
	httpClient      *http.Client
}

func NewR2Storage(_ context.Context, cfg appconfig.R2Config) (*R2Storage, error) {
	if !cfg.Enabled() {
		return nil, nil
	}
	endpoint, err := url.Parse(strings.TrimRight(cfg.Endpoint, "/"))
	if err != nil || endpoint.Scheme == "" || endpoint.Host == "" {
		return nil, fmt.Errorf("invalid R2_ENDPOINT")
	}
	return &R2Storage{
		endpoint:        endpoint,
		bucket:          cfg.Bucket,
		accessKeyID:     cfg.AccessKeyID,
		secretAccessKey: cfg.SecretAccessKey,
		httpClient:      &http.Client{Timeout: 30 * time.Second},
	}, nil
}

func (s *R2Storage) Put(ctx context.Context, objectKey string, contentType string, data []byte) error {
	objectURL := s.objectURL(objectKey)
	payloadHash := sha256Hex(data)
	now := time.Now().UTC()
	request, err := http.NewRequestWithContext(ctx, http.MethodPut, objectURL.String(), bytes.NewReader(data))
	if err != nil {
		return err
	}
	request.Header.Set("Content-Type", contentType)
	request.Header.Set("X-Amz-Content-Sha256", payloadHash)
	s.sign(request, payloadHash, now)
	response, err := s.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("put R2 object: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode < http.StatusOK || response.StatusCode >= http.StatusMultipleChoices {
		body, _ := io.ReadAll(io.LimitReader(response.Body, 4<<10))
		return fmt.Errorf("put R2 object: %s: %s", response.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (s *R2Storage) PresignDownload(_ context.Context, objectKey string, expiresIn time.Duration) (string, error) {
	if expiresIn <= 0 || expiresIn > 7*24*time.Hour {
		return "", fmt.Errorf("invalid R2 download expiry")
	}
	now := time.Now().UTC()
	dateStamp := now.Format("20060102")
	credentialScope := dateStamp + "/auto/s3/aws4_request"
	objectURL := s.objectURL(objectKey)
	query := objectURL.Query()
	query.Set("X-Amz-Algorithm", "AWS4-HMAC-SHA256")
	query.Set("X-Amz-Credential", s.accessKeyID+"/"+credentialScope)
	query.Set("X-Amz-Date", now.Format("20060102T150405Z"))
	query.Set("X-Amz-Expires", fmt.Sprintf("%d", int(expiresIn.Seconds())))
	query.Set("X-Amz-SignedHeaders", "host")
	objectURL.RawQuery = canonicalQuery(query)
	canonicalRequest := strings.Join([]string{
		http.MethodGet,
		objectURL.EscapedPath(),
		objectURL.RawQuery,
		"host:" + objectURL.Host + "\n",
		"host",
		"UNSIGNED-PAYLOAD",
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		now.Format("20060102T150405Z"),
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(signingKey(s.secretAccessKey, dateStamp, "auto", "s3", stringToSign))
	objectURL.RawQuery += "&X-Amz-Signature=" + signature
	return objectURL.String(), nil
}

func (s *R2Storage) objectURL(objectKey string) *url.URL {
	copyURL := *s.endpoint
	copyURL.Path = path.Join(copyURL.Path, s.bucket, objectKey)
	copyURL.RawPath = ""
	return &copyURL
}

func (s *R2Storage) sign(request *http.Request, payloadHash string, now time.Time) {
	dateStamp := now.Format("20060102")
	amzDate := now.Format("20060102T150405Z")
	request.Header.Set("X-Amz-Date", amzDate)
	credentialScope := dateStamp + "/auto/s3/aws4_request"
	canonicalHeaders := "content-type:" + strings.TrimSpace(request.Header.Get("Content-Type")) + "\n" +
		"host:" + request.URL.Host + "\n" +
		"x-amz-content-sha256:" + payloadHash + "\n" +
		"x-amz-date:" + amzDate + "\n"
	signedHeaders := "content-type;host;x-amz-content-sha256;x-amz-date"
	canonicalRequest := strings.Join([]string{
		request.Method,
		request.URL.EscapedPath(),
		canonicalQuery(request.URL.Query()),
		canonicalHeaders,
		signedHeaders,
		payloadHash,
	}, "\n")
	stringToSign := strings.Join([]string{
		"AWS4-HMAC-SHA256",
		amzDate,
		credentialScope,
		sha256Hex([]byte(canonicalRequest)),
	}, "\n")
	signature := hex.EncodeToString(signingKey(s.secretAccessKey, dateStamp, "auto", "s3", stringToSign))
	request.Header.Set("Authorization", "AWS4-HMAC-SHA256 Credential="+s.accessKeyID+"/"+credentialScope+", SignedHeaders="+signedHeaders+", Signature="+signature)
}

func canonicalQuery(values url.Values) string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	parts := make([]string, 0, len(keys))
	for _, key := range keys {
		valuesForKey := append([]string(nil), values[key]...)
		sort.Strings(valuesForKey)
		for _, value := range valuesForKey {
			parts = append(parts, url.QueryEscape(key)+"="+url.QueryEscape(value))
		}
	}
	return strings.ReplaceAll(strings.Join(parts, "&"), "+", "%20")
}

func signingKey(secret string, date string, region string, service string, stringToSign string) []byte {
	dateKey := hmacSHA256([]byte("AWS4"+secret), date)
	regionKey := hmacSHA256(dateKey, region)
	serviceKey := hmacSHA256(regionKey, service)
	return hmacSHA256(hmacSHA256(serviceKey, "aws4_request"), stringToSign)
}

func hmacSHA256(key []byte, value string) []byte {
	mac := hmac.New(sha256.New, key)
	_, _ = mac.Write([]byte(value))
	return mac.Sum(nil)
}

func sha256Hex(value []byte) string {
	sum := sha256.Sum256(value)
	return hex.EncodeToString(sum[:])
}
