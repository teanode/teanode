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
	directory := t.TempDir()
	testStore, err := fsstore.Open(fsstore.Options{DataDirectory: directory})
	if err != nil {
		t.Fatal(err)
	}
	if err := testStore.Migrate(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := testStore.Close(); err != nil {
			t.Errorf("closing test store: %v", err)
		}
	})
	return testStore
}

func TestGetCertificate_ChallengeALPN(t *testing.T) {
	testStore := setupTestStore(t)
	manager := New(testStore)

	// Generate a fake challenge cert and install it.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	fakeCertificate := &tls.Certificate{
		Certificate: [][]byte{[]byte("fake-challenge")},
		PrivateKey:  key,
	}
	manager.challengeMutex.Lock()
	manager.challengeCertificate = fakeCertificate
	manager.challengeMutex.Unlock()

	// When ALPN includes acme-tls/1, should return challenge cert.
	hello := &tls.ClientHelloInfo{
		SupportedProtos: []string{acme.ALPNProto},
		ServerName:      "example.com",
	}
	got, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != fakeCertificate {
		t.Fatal("expected challenge cert to be returned")
	}
}

func TestGetCertificate_NormalRequest(t *testing.T) {
	testStore := setupTestStore(t)
	manager := New(testStore)

	// No certificate loaded yet.
	hello := &tls.ClientHelloInfo{
		SupportedProtos: []string{"h2", "http/1.1"},
		ServerName:      "example.com",
	}
	_, err := manager.GetCertificate(hello)
	if err != ErrNoCertificate {
		t.Fatalf("expected ErrNoCertificate, got %v", err)
	}

	// Install a regular certificate.
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	regularCertificate := &tls.Certificate{
		Certificate: [][]byte{[]byte("real-cert")},
		PrivateKey:  key,
	}
	manager.certificateMutex.Lock()
	manager.certificate = regularCertificate
	manager.certificateMutex.Unlock()

	got, err := manager.GetCertificate(hello)
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	if got != regularCertificate {
		t.Fatal("expected regular cert to be returned")
	}
}

func TestGetCertificate_ALPNWithNoChallenge(t *testing.T) {
	testStore := setupTestStore(t)
	manager := New(testStore)

	hello := &tls.ClientHelloInfo{
		SupportedProtos: []string{acme.ALPNProto},
		ServerName:      "example.com",
	}
	_, err := manager.GetCertificate(hello)
	if err != ErrNoCertificate {
		t.Fatalf("expected ErrNoCertificate when no challenge cert set, got %v", err)
	}
}

func TestConfigPatchOnlyTouchesCertificateFields(t *testing.T) {
	testStore := setupTestStore(t)
	ctx := context.Background()

	// Set up initial configuration with node and tools.
	if err := testStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			port := 9999
			configuration.Node = &models.NodeConfiguration{Port: &port}
			braveKey := "test-brave-key"
			configuration.Tools = &models.ToolsConfiguration{BraveAPIKey: &braveKey}
			return nil
		}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Simulate what autoacme does: modify only Certificate fields.
	certificatePem := "-----BEGIN CERTIFICATE-----\nfake\n-----END CERTIFICATE-----"
	keyPem := "-----BEGIN RSA PRIVATE KEY-----\nfake\n-----END RSA PRIVATE KEY-----"
	if err := testStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			if configuration.Certificate == nil {
				configuration.Certificate = &models.CertificateConfiguration{}
			}
			configuration.Certificate.Certificate = &certificatePem
			configuration.Certificate.PrivateKey = &keyPem
			return nil
		}, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	// Verify node and tools are untouched.
	var configuration *models.Configuration
	if err := testStore.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		configuration, err = tx.GetConfiguration(ctx, nil)
		return err
	}); err != nil {
		t.Fatal(err)
	}

	if configuration.Node == nil || configuration.Node.GetPort() != 9999 {
		t.Fatalf("node port was overwritten: got %v", configuration.Node)
	}
	if configuration.Tools == nil || configuration.Tools.GetBraveAPIKey() != "test-brave-key" {
		t.Fatalf("tools configuration was overwritten: got %v", configuration.Tools)
	}
	if configuration.Certificate == nil || configuration.Certificate.GetCertificate() != certificatePem {
		t.Fatalf("certificate was not persisted correctly")
	}
	if configuration.Certificate.GetPrivateKey() != keyPem {
		t.Fatalf("private key was not persisted correctly")
	}
}
