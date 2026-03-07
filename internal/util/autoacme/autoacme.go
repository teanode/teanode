package autoacme

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/rsa"
	"crypto/sha256"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/asn1"
	"encoding/pem"
	"errors"
	"math/big"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/op/go-logging"
	"golang.org/x/crypto/acme"

	"github.com/teanode/teanode/internal/models"
	"github.com/teanode/teanode/internal/store"
	"github.com/teanode/teanode/internal/util/deferutil"
)

var log = logging.MustGetLogger("autoacme")

var (
	ErrNoCertificate      = errors.New("autoacme: no certificate")
	ErrInvalidCertificate = errors.New("autoacme: invalid certificate")
)

// oidACMEIdentifier is the OID for the ACME identifier extension (1.3.6.1.5.5.7.1.31).
var oidACMEIdentifier = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}

// Manager handles automatic certificate management via ACME TLS-ALPN-01.
type Manager struct {
	store store.Store

	certificateMu sync.Mutex
	certificate   *tls.Certificate

	challengeMu   sync.Mutex
	challengeCert *tls.Certificate

	acmeClientMu sync.Mutex
	acmeClient   *acme.Client

	done      chan struct{}
	waitGroup sync.WaitGroup
}

// New creates a new Manager. Call Start to begin the background renewal loop.
func New(s store.Store) *Manager {
	return &Manager{
		store: s,
		done:  make(chan struct{}, 1),
	}
}

// Start begins the background certificate management goroutine.
// It loads any existing certificate from the store, then enters a renewal loop.
func (m *Manager) Start(ctx context.Context) {
	m.loadCertificateFromStore(ctx)

	m.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer m.waitGroup.Done()
		m.run(ctx)
	}()
}

// Close stops the background goroutine and waits for it to finish.
func (m *Manager) Close() {
	close(m.done)
	m.waitGroup.Wait()
}

// GetCertificate implements tls.Config.GetCertificate.
// When the client negotiates acme-tls/1, it returns the challenge certificate.
// Otherwise, it returns the current real certificate.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// Check for ACME TLS-ALPN-01 challenge.
	for _, proto := range hello.SupportedProtos {
		if proto == acme.ALPNProto {
			m.challengeMu.Lock()
			cert := m.challengeCert
			m.challengeMu.Unlock()
			if cert != nil {
				return cert, nil
			}
			return nil, ErrNoCertificate
		}
	}

	m.certificateMu.Lock()
	cert := m.certificate
	m.certificateMu.Unlock()

	if cert != nil {
		return cert, nil
	}
	return nil, ErrNoCertificate
}

func (m *Manager) run(ctx context.Context) {
	m.spin(ctx)
	for {
		select {
		case <-m.done:
			return
		case <-time.After(5 * time.Minute):
			m.spin(ctx)
		}
	}
}

func (m *Manager) spin(ctx context.Context) {
	cfg := m.getConfig(ctx)
	if cfg == nil || cfg.Certificate == nil {
		return
	}
	certCfg := cfg.Certificate
	if certCfg.GetDomain() == "" || certCfg.GetACMEEmail() == "" {
		return
	}
	if !m.shouldRenew(certCfg.GetDomain()) {
		return
	}

	reqCtx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := m.EnsureCertificate(reqCtx); err != nil {
		log.Errorf("failed to ensure certificate: %v", err)
	}
}

func (m *Manager) shouldRenew(domain string) bool {
	m.certificateMu.Lock()
	cert := m.certificate
	m.certificateMu.Unlock()

	if cert == nil {
		return true
	}

	leaf, err := leafCert(cert)
	if err != nil {
		return true
	}
	now := time.Now()
	if now.After(leaf.NotAfter) || now.Before(leaf.NotBefore) {
		return true
	}
	if err := leaf.VerifyHostname(domain); err != nil {
		return true
	}
	// Renew if expiring within 30 days.
	if time.Now().Add(30 * 24 * time.Hour).After(leaf.NotAfter) {
		log.Infof("certificate expiring within 30 days, renewing")
		return true
	}
	return false
}

// EnsureCertificate obtains or renews a certificate via ACME TLS-ALPN-01.
func (m *Manager) EnsureCertificate(ctx context.Context) error {
	cfg := m.getConfig(ctx)
	if cfg == nil || cfg.Certificate == nil {
		return errors.New("autoacme: no certificate configuration")
	}
	certCfg := cfg.Certificate
	domain := certCfg.GetDomain()
	email := certCfg.GetACMEEmail()
	if domain == "" || email == "" {
		return errors.New("autoacme: domain and acme email required")
	}

	client, err := m.ensureACMEClient(ctx, certCfg)
	if err != nil {
		return err
	}

	// Generate certificate private key.
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Create CSR.
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domain},
		DNSNames: []string{domain},
	}, privKey)
	if err != nil {
		return err
	}

	// Create order.
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return err
	}

	// Process authorizations.
	for _, authzURL := range order.AuthzURLs {
		authz, err := client.GetAuthorization(ctx, authzURL)
		if err != nil {
			return err
		}
		if authz.Status != acme.StatusPending {
			continue
		}

		// Find tls-alpn-01 challenge.
		var challenge *acme.Challenge
		for _, c := range authz.Challenges {
			if c.Type == "tls-alpn-01" {
				challenge = c
				break
			}
		}
		if challenge == nil {
			return errors.New("autoacme: no tls-alpn-01 challenge offered")
		}

		// Build and install ALPN challenge certificate.
		challengeCert, err := m.buildALPNCert(domain, challenge.Token, client.Key)
		if err != nil {
			return err
		}

		m.challengeMu.Lock()
		m.challengeCert = challengeCert
		m.challengeMu.Unlock()

		// Accept challenge.
		if _, err := client.Accept(ctx, challenge); err != nil {
			m.clearChallenge()
			return err
		}

		// Wait for authorization.
		if _, err := client.WaitAuthorization(ctx, authzURL); err != nil {
			m.clearChallenge()
			return err
		}

		m.clearChallenge()
	}

	// Wait for order to be ready.
	order, err = client.WaitOrder(ctx, order.URI)
	if err != nil {
		return err
	}

	// Finalize.
	derChain, _, err := client.CreateOrderCert(ctx, order.FinalizeURL, csr, true)
	if err != nil {
		return err
	}

	// Build tls.Certificate.
	tlsCert := &tls.Certificate{
		PrivateKey:  privKey,
		Certificate: derChain,
	}

	// Parse leaf for metadata.
	leaf, err := leafCert(tlsCert)
	if err != nil {
		return err
	}

	// Persist to store.
	if err := m.persistCertificate(ctx, tlsCert, privKey, leaf); err != nil {
		return err
	}

	// Update in-memory.
	m.certificateMu.Lock()
	m.certificate = tlsCert
	m.certificateMu.Unlock()

	log.Infof("certificate obtained for %s, expires %s", domain, leaf.NotAfter)
	return nil
}

func (m *Manager) clearChallenge() {
	m.challengeMu.Lock()
	m.challengeCert = nil
	m.challengeMu.Unlock()
}

// buildALPNCert creates a self-signed certificate for the TLS-ALPN-01 challenge.
// Per RFC 8737, it includes the acmeIdentifier extension with the SHA-256 of the key authorization.
func (m *Manager) buildALPNCert(domain, token string, accountKey crypto.Signer) (*tls.Certificate, error) {
	// Compute key authorization.
	thumbprint, err := acme.JWKThumbprint(accountKey)
	if err != nil {
		return nil, err
	}
	keyAuth := token + "." + thumbprint
	keyAuthHash := sha256.Sum256([]byte(keyAuth))

	// ASN.1 encode the hash as an OCTET STRING.
	acmeExtValue, err := asn1.Marshal(keyAuthHash[:])
	if err != nil {
		return nil, err
	}

	// Generate a throwaway key for the challenge cert.
	certKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		return nil, err
	}

	template := &x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject:      pkix.Name{CommonName: "ACME challenge"},
		NotBefore:    time.Now().Add(-time.Hour),
		NotAfter:     time.Now().Add(24 * time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth},
		DNSNames:     []string{domain},
		ExtraExtensions: []pkix.Extension{{
			Id:       oidACMEIdentifier,
			Critical: true,
			Value:    acmeExtValue,
		}},
	}

	certDER, err := x509.CreateCertificate(rand.Reader, template, template, &certKey.PublicKey, certKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{certDER},
		PrivateKey:  certKey,
	}, nil
}

func (m *Manager) ensureACMEClient(ctx context.Context, certCfg *models.CertificateConfiguration) (*acme.Client, error) {
	m.acmeClientMu.Lock()
	defer m.acmeClientMu.Unlock()

	if m.acmeClient != nil {
		return m.acmeClient, nil
	}

	var key crypto.Signer

	// Try to load existing account key from config.
	if accountKeyPEM := certCfg.GetACMEAccountKey(); accountKeyPEM != "" {
		block, _ := pem.Decode([]byte(accountKeyPEM))
		if block != nil && strings.Contains(block.Type, "PRIVATE") {
			if parsed, err := x509.ParseECPrivateKey(block.Bytes); err == nil {
				key = parsed
			}
		}
	}

	// Generate new key if needed.
	if key == nil {
		newKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if err != nil {
			return nil, err
		}
		key = newKey

		// Persist the new account key.
		encoded, err := x509.MarshalECPrivateKey(newKey)
		if err != nil {
			return nil, err
		}
		var buf bytes.Buffer
		if err := pem.Encode(&buf, &pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded}); err != nil {
			return nil, err
		}
		accountKeyPEM := buf.String()

		if err := m.store.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
			_, err := tx.ModifyConfiguration(ctx, func(cfg *models.Configuration) error {
				if cfg.Certificate == nil {
					cfg.Certificate = &models.CertificateConfiguration{}
				}
				cfg.Certificate.ACMEAccountKey = &accountKeyPEM
				return nil
			}, nil)
			return err
		}); err != nil {
			return nil, err
		}
	}

	client := &acme.Client{
		DirectoryURL: "https://acme-v02.api.letsencrypt.org/directory",
		Key:          key,
		UserAgent:    "teanode-autoacme",
	}

	account := &acme.Account{Contact: []string{
		"mailto:" + certCfg.GetACMEEmail(),
	}}
	if _, err := client.Register(ctx, account, func(string) bool {
		return true
	}); err != nil {
		if err != acme.ErrAccountAlreadyExists {
			if acmeErr, ok := err.(*acme.Error); ok && acmeErr.StatusCode == http.StatusConflict {
				// Already registered, that's fine.
			} else {
				return nil, err
			}
		}
	}

	m.acmeClient = client
	return client, nil
}

func (m *Manager) persistCertificate(ctx context.Context, tlsCert *tls.Certificate, privKey *rsa.PrivateKey, leaf *x509.Certificate) error {
	// Encode cert chain as PEM.
	var certBuf bytes.Buffer
	for _, der := range tlsCert.Certificate {
		if err := pem.Encode(&certBuf, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return err
		}
	}

	// Encode private key as PEM.
	var keyBuf bytes.Buffer
	if err := pem.Encode(&keyBuf, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privKey),
	}); err != nil {
		return err
	}

	certPEM := certBuf.String()
	keyPEM := keyBuf.String()
	issuedAt := leaf.NotBefore
	expiresAt := leaf.NotAfter

	return m.store.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(cfg *models.Configuration) error {
			if cfg.Certificate == nil {
				cfg.Certificate = &models.CertificateConfiguration{}
			}
			cfg.Certificate.Certificate = &certPEM
			cfg.Certificate.PrivateKey = &keyPEM
			cfg.Certificate.IssuedAt = &issuedAt
			cfg.Certificate.ExpiresAt = &expiresAt
			return nil
		}, nil)
		return err
	})
}

func (m *Manager) loadCertificateFromStore(ctx context.Context) {
	cfg := m.getConfig(ctx)
	if cfg == nil || cfg.Certificate == nil {
		return
	}
	certCfg := cfg.Certificate
	certPEM := certCfg.GetCertificate()
	keyPEM := certCfg.GetPrivateKey()
	if certPEM == "" || keyPEM == "" {
		return
	}

	tlsCert, err := tls.X509KeyPair([]byte(certPEM), []byte(keyPEM))
	if err != nil {
		log.Warningf("failed to load stored certificate: %v", err)
		return
	}

	m.certificateMu.Lock()
	m.certificate = &tlsCert
	m.certificateMu.Unlock()
}

func (m *Manager) getConfig(ctx context.Context) *models.Configuration {
	var cfg *models.Configuration
	_ = m.store.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		cfg, err = tx.GetConfiguration(ctx, nil)
		return err
	})
	return cfg
}

func leafCert(tlsCert *tls.Certificate) (*x509.Certificate, error) {
	if len(tlsCert.Certificate) == 0 {
		return nil, ErrInvalidCertificate
	}
	return x509.ParseCertificate(tlsCert.Certificate[0])
}
