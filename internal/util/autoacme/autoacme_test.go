package autoacme

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"testing"

	"golang.org/x/crypto/acme"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/store/fsstore"
)

func setupTestStore(t *testing.T) store.Store {
	t.Helper()
	dir := t.TempDir()
	s, err := fsstore.Open(fsstore.Options{DataDirectory: dir})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	return s
}

func TestGetCertificate_ChallengeALPN(t *testing.T) {
	s := setupTestStore(t)
	m := New(s)

	// Generate a fake challenge cert and install it.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fakeCert := &tls.Certificate{
		Certificate: [][]byte{[]byte("fake-challenge")},
		PrivateKey:  key,
	}
	m.challengeMu.Lock()
	m.challengeCert = fakeCert
	m.challengeMu.Unlock()

	// When ALPN includes acme-tls/1, should return challenge cert.
	hello := &tls.ClientHelloInfo{
		SupportedProtos: []string{acme.ALPNProto},
		ServerName:      "example.com",
	}
	got, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != fakeCert {
		t.Fatal("expected challenge cert to be returned")
	}
}

func TestGetCertificate_NormalRequest(t *testing.T) {
	s := setupTestStore(t)
	m := New(s)

	// No certificate loaded yet.
	hello := &tls.ClientHelloInfo{
		SupportedProtos: []string{"h2", "http/1.1"},
		ServerName:      "example.com",
	}
	_, err := m.GetCertificate(hello)
	if err != ErrNoCertificate {
		t.Fatalf("expected ErrNoCertificate, got %v", err)
	}

	// Install a regular cert.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	regularCert := &tls.Certificate{
		Certificate: [][]byte{[]byte("real-cert")},
		PrivateKey:  key,
	}
	m.certificateMu.Lock()
	m.certificate = regularCert
	m.certificateMu.Unlock()

	got, err := m.GetCertificate(hello)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != regularCert {
		t.Fatal("expected regular cert to be returned")
	}
}

func TestGetCertificate_ALPNWithNoChallenge(t *testing.T) {
	s := setupTestStore(t)
	m := New(s)

	hello := &tls.ClientHelloInfo{
		SupportedProtos: []string{acme.ALPNProto},
		ServerName:      "example.com",
	}
	_, err := m.GetCertificate(hello)
	if err != ErrNoCertificate {
		t.Fatalf("expected ErrNoCertificate when no challenge cert set, got %v", err)
	}
}

func TestConfigPatchOnlyTouchesCertificateFields(t *testing.T) {
	s := setupTestStore(t)
	ctx := context.Background()

	// Set up initial config with gateway and tools.
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(cfg *models.Configuration) error {
			port := 9999
			cfg.Gateway = &models.GatewayConfiguration{Port: &port}
			braveKey := "test-brave-key"
			cfg.Tools = &models.ToolsConfiguration{BraveAPIKey: &braveKey}
			return nil
		}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate what autoacme does: modify only Certificate fields.
	certPEM := "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----"
	keyPEM := "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----"
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(cfg *models.Configuration) error {
			if cfg.Certificate == nil {
				cfg.Certificate = &models.CertificateConfiguration{}
			}
			cfg.Certificate.Certificate = &certPEM
			cfg.Certificate.PrivateKey = &keyPEM
			return nil
		}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Verify gateway and tools are untouched.
	var cfg *models.Configuration
	if err := s.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		cfg, err = tx.GetConfiguration(ctx, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if cfg.Gateway == nil || cfg.Gateway.GetPort() != 9999 {
		t.Fatalf("gateway port was overwritten: got %v", cfg.Gateway)
	}
	if cfg.Tools == nil || cfg.Tools.GetBraveAPIKey() != "test-brave-key" {
		t.Fatalf("tools config was overwritten: got %v", cfg.Tools)
	}
	if cfg.Certificate == nil || cfg.Certificate.GetCertificate() != certPEM {
		t.Fatalf("certificate was not persisted correctly")
	}
	if cfg.Certificate.GetPrivateKey() != keyPEM {
		t.Fatalf("private key was not persisted correctly")
	}
}
