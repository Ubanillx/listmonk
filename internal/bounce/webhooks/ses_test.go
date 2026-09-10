package webhooks

import (
	"bytes"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"fmt"
	"io"
	"math/big"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

// testCertPEM returns a self-signed certificate in PEM form.
func testCertPEM(t *testing.T) []byte {
	t.Helper()

	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	tmpl := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "sns.amazonaws.com"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(time.Hour),
	}
	der, err := x509.CreateCertificate(rand.Reader, tmpl, tmpl, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}

	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

// certServer builds an SES whose certificate fetches are served from memory, and
// counts them.
func certServer(t *testing.T, body []byte, status int) (*SES, *int64) {
	t.Helper()

	var fetches int64
	s := NewSES()
	s.client = &http.Client{
		Timeout: time.Second,
		Transport: roundTripFunc(func(r *http.Request) (*http.Response, error) {
			atomic.AddInt64(&fetches, 1)
			return &http.Response{
				StatusCode: status,
				Body:       io.NopCloser(bytes.NewReader(body)),
				Header:     make(http.Header),
			}, nil
		}),
	}

	return s, &fetches
}

const (
	certURL       = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-aaa.pem"
	otherCertURL  = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-bbb.pem"
	certURLFormat = "https://sns.us-east-1.amazonaws.com/SimpleNotificationService-%s.pem"
)

// TestGetCertConcurrentMissesFetchOnce covers the shared certificate cache. The
// public webhook handler calls this concurrently, and the cache used to be a
// plain map read and written without a lock, which is a fatal runtime error
// rather than a stale read. Concurrent misses must also fetch once, not once per
// request.
func TestGetCertConcurrentMissesFetchOnce(t *testing.T) {
	s, fetches := certServer(t, testCertPEM(t), http.StatusOK)

	const callers = 32
	var (
		wg   sync.WaitGroup
		errs = make(chan error, callers)
	)
	for i := 0; i < callers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			cert, err := s.getCert(certURL)
			if err != nil {
				errs <- err
				return
			}
			if cert == nil {
				errs <- fmt.Errorf("nil certificate with no error")
			}
		}()
	}
	wg.Wait()
	close(errs)
	for err := range errs {
		t.Fatalf("getCert failed: %v", err)
	}

	if got := atomic.LoadInt64(fetches); got != 1 {
		t.Fatalf("fetched the certificate %d times, want 1", got)
	}

	// A cached lookup must not fetch again.
	if _, err := s.getCert(certURL); err != nil {
		t.Fatal(err)
	}
	if got := atomic.LoadInt64(fetches); got != 1 {
		t.Fatalf("cache hit fetched again: %d fetches", got)
	}
}

// TestGetCertDoesNotCacheUnparseableCertificate covers a nil cached entry: the
// certificate was cached before its parse error was checked, so a later lookup
// returned a "hit" holding nil and the caller panicked on CheckSignature.
func TestGetCertDoesNotCacheUnparseableCertificate(t *testing.T) {
	bad := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: []byte("not a certificate")})
	s, fetches := certServer(t, bad, http.StatusOK)

	for i := 0; i < 2; i++ {
		cert, err := s.getCert(certURL)
		if err == nil {
			t.Fatal("expected an unparseable certificate to be rejected")
		}
		if cert != nil {
			t.Fatalf("expected no certificate alongside the error, got %v", cert)
		}
	}

	if got := atomic.LoadInt64(fetches); got != 2 {
		t.Fatalf("expected each lookup to retry the fetch, got %d fetches", got)
	}
}

// TestGetCertRejectsNonAWSUrl keeps the existing allowlist intact: only SNS
// certificate URLs may be fetched.
func TestGetCertRejectsNonAWSUrl(t *testing.T) {
	s, fetches := certServer(t, testCertPEM(t), http.StatusOK)

	for _, u := range []string{
		"https://evil.example.com/SimpleNotificationService-aaa.pem",
		"http://sns.us-east-1.amazonaws.com/SimpleNotificationService-aaa.pem",
		"https://sns.us-east-1.amazonaws.com/other.pem",
	} {
		if _, err := s.getCert(u); err == nil {
			t.Fatalf("expected %q to be rejected", u)
		}
	}
	if got := atomic.LoadInt64(fetches); got != 0 {
		t.Fatalf("expected no fetch for a rejected URL, got %d", got)
	}
}

// TestGetCertBoundsCache covers the cache bound, so a crafted set of certificate
// URLs cannot grow it without limit.
func TestGetCertBoundsCache(t *testing.T) {
	s, _ := certServer(t, testCertPEM(t), http.StatusOK)

	for i := 0; i < maxSESCerts+5; i++ {
		url := fmt.Sprintf(certURLFormat, fmt.Sprintf("%d", i))
		if _, err := s.getCert(url); err != nil {
			t.Fatalf("getCert(%q) failed: %v", url, err)
		}
	}

	s.certsMu.RLock()
	size := len(s.certs)
	s.certsMu.RUnlock()

	if size > maxSESCerts {
		t.Fatalf("certificate cache grew to %d entries, max is %d", size, maxSESCerts)
	}
}

// TestGetCertRejectsFailureStatus covers a non-200 response from the certificate
// endpoint.
func TestGetCertRejectsFailureStatus(t *testing.T) {
	s, _ := certServer(t, []byte("nope"), http.StatusForbidden)

	if _, err := s.getCert(otherCertURL); err == nil {
		t.Fatal("expected a non-200 certificate response to be rejected")
	}
}
