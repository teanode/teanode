// Package autoacme manages automatic ACME certificate provisioning.
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

// oidAcmeIdentifier is the OID for the ACME identifier extension (1.3.6.1.5.5.7.1.31).
var oidAcmeIdentifier = asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}

// Manager handles automatic certificate management via ACME TLS-ALPN-01.
type Manager struct {
	store store.Store

	certificateMutex sync.Mutex
	certificate      *tls.Certificate

	challengeMutex       sync.Mutex
	challengeCertificate *tls.Certificate

	acmeClientMutex sync.Mutex
	acmeClient      *acme.Client

	done      chan struct{}
	waitGroup sync.WaitGroup
}

// New creates a new Manager. Call Start to begin the background renewal loop.
func New(dataStore store.Store) *Manager {
	return &Manager{
		store: dataStore,
		done:  make(chan struct{}, 1),
	}
}

// Start begins the background certificate management goroutine.
// It loads any existing certificate from the store, then enters a renewal loop.
func (self *Manager) Start(ctx context.Context) {
	self.loadCertificateFromStore(ctx)

	self.waitGroup.Add(1)
	go func() {
		defer deferutil.Recover()
		defer self.waitGroup.Done()
		self.run(ctx)
	}()
}

// Close stops the background goroutine and waits for it to finish.
func (self *Manager) Close() {
	close(self.done)
	self.waitGroup.Wait()
}

// GetCertificate implements tls.Config.GetCertificate.
// When the client negotiates acme-tls/1, it returns the challenge certificate.
// Otherwise, it returns the current real certificate.
func (self *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
	// Check for ACME TLS-ALPN-01 challenge.
	for _, proto := range hello.SupportedProtos {
		if proto == acme.ALPNProto {
			self.challengeMutex.Lock()
			certificate := self.challengeCertificate
			self.challengeMutex.Unlock()
			if certificate != nil {
				return certificate, nil
			}
			return nil, ErrNoCertificate
		}
	}

	self.certificateMutex.Lock()
	certificate := self.certificate
	self.certificateMutex.Unlock()

	if certificate != nil {
		return certificate, nil
	}
	return nil, ErrNoCertificate
}

func (self *Manager) run(ctx context.Context) {
	self.spin(ctx)
	for {
		select {
		case <-self.done:
			return
		case <-time.After(5 * time.Minute):
			self.spin(ctx)
		}
	}
}

func (self *Manager) spin(ctx context.Context) {
	configuration := self.getConfiguration(ctx)
	if configuration == nil || configuration.Certificate == nil {
		return
	}
	certificateConfiguration := configuration.Certificate
	if certificateConfiguration.GetDomain() == "" || certificateConfiguration.GetACMEEmail() == "" {
		return
	}
	if !self.shouldRenew(certificateConfiguration.GetDomain()) {
		return
	}

	requestContext, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()

	if err := self.EnsureCertificate(requestContext); err != nil {
		log.Errorf("failed to ensure certificate: %v", err)
	}
}

func (self *Manager) shouldRenew(domain string) bool {
	self.certificateMutex.Lock()
	certificate := self.certificate
	self.certificateMutex.Unlock()

	if certificate == nil {
		return true
	}

	leaf, err := leafCertificate(certificate)
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
func (self *Manager) EnsureCertificate(ctx context.Context) error {
	configuration := self.getConfiguration(ctx)
	if configuration == nil || configuration.Certificate == nil {
		return errors.New("autoacme: no certificate configuration")
	}
	certificateConfiguration := configuration.Certificate
	domain := certificateConfiguration.GetDomain()
	email := certificateConfiguration.GetACMEEmail()
	if domain == "" || email == "" {
		return errors.New("autoacme: domain and acme email required")
	}

	client, err := self.ensureACMEClient(ctx, certificateConfiguration)
	if err != nil {
		return err
	}

	// Generate certificate private key.
	privateKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	// Create CSR.
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{
		Subject:  pkix.Name{CommonName: domain},
		DNSNames: []string{domain},
	}, privateKey)
	if err != nil {
		return err
	}

	// Create order.
	order, err := client.AuthorizeOrder(ctx, acme.DomainIDs(domain))
	if err != nil {
		return err
	}

	// Process authorizations.
	for _, authorizationURL := range order.AuthzURLs {
		authorization, err := client.GetAuthorization(ctx, authorizationURL)
		if err != nil {
			return err
		}
		if authorization.Status != acme.StatusPending {
			continue
		}

		// Find tls-alpn-01 challenge.
		var challenge *acme.Challenge
		for _, candidate := range authorization.Challenges {
			if candidate.Type == "tls-alpn-01" {
				challenge = candidate
				break
			}
		}
		if challenge == nil {
			return errors.New("autoacme: no tls-alpn-01 challenge offered")
		}

		// Build and install ALPN challenge certificate.
		challengeCertificate, err := self.buildAlpnCertificate(domain, challenge.Token, client.Key)
		if err != nil {
			return err
		}

		self.challengeMutex.Lock()
		self.challengeCertificate = challengeCertificate
		self.challengeMutex.Unlock()

		// Accept challenge.
		if _, err := client.Accept(ctx, challenge); err != nil {
			self.clearChallenge()
			return err
		}

		// Wait for authorization.
		if _, err := client.WaitAuthorization(ctx, authorizationURL); err != nil {
			self.clearChallenge()
			return err
		}

		self.clearChallenge()
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
	tlsCertificate := &tls.Certificate{
		PrivateKey:  privateKey,
		Certificate: derChain,
	}

	// Parse leaf for metadata.
	leaf, err := leafCertificate(tlsCertificate)
	if err != nil {
		return err
	}

	// Persist to store.
	if err := self.persistCertificate(ctx, tlsCertificate, privateKey, leaf); err != nil {
		return err
	}

	// Update in-memory.
	self.certificateMutex.Lock()
	self.certificate = tlsCertificate
	self.certificateMutex.Unlock()

	log.Infof("certificate obtained for %s, expires %s", domain, leaf.NotAfter)
	return nil
}

func (self *Manager) clearChallenge() {
	self.challengeMutex.Lock()
	self.challengeCertificate = nil
	self.challengeMutex.Unlock()
}

// buildAlpnCertificate creates a self-signed certificate for the TLS-ALPN-01 challenge.
// Per RFC 8737, it includes the acmeIdentifier extension with the SHA-256 of the key authorization.
func (self *Manager) buildAlpnCertificate(domain, token string, accountKey crypto.Signer) (*tls.Certificate, error) {
	// Compute key authorization.
	thumbprint, err := acme.JWKThumbprint(accountKey)
	if err != nil {
		return nil, err
	}
	keyAuth := token + "." + thumbprint
	keyAuthHash := sha256.Sum256([]byte(keyAuth))

	// ASN.1 encode the hash as an OCTET STRING.
	acmeExtensionValue, err := asn1.Marshal(keyAuthHash[:])
	if err != nil {
		return nil, err
	}

	// Generate a throwaway key for the challenge cert.
	certificateKey, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
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
			Id:       oidAcmeIdentifier,
			Critical: true,
			Value:    acmeExtensionValue,
		}},
	}

	certificateDer, err := x509.CreateCertificate(rand.Reader, template, template, &certificateKey.PublicKey, certificateKey)
	if err != nil {
		return nil, err
	}

	return &tls.Certificate{
		Certificate: [][]byte{certificateDer},
		PrivateKey:  certificateKey,
	}, nil
}

func (self *Manager) ensureACMEClient(ctx context.Context, certificateConfiguration *models.CertificateConfiguration) (*acme.Client, error) {
	self.acmeClientMutex.Lock()
	defer self.acmeClientMutex.Unlock()

	if self.acmeClient != nil {
		return self.acmeClient, nil
	}

	var key crypto.Signer

	// Try to load existing account key from config.
	if accountKeyPem := certificateConfiguration.GetACMEAccountKey(); accountKeyPem != "" {
		block, _ := pem.Decode([]byte(accountKeyPem))
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
		var buffer bytes.Buffer
		if err := pem.Encode(&buffer, &pem.Block{Type: "EC PRIVATE KEY", Bytes: encoded}); err != nil {
			return nil, err
		}
		accountKeyPem := buffer.String()

		if err := self.store.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
			_, err := tx.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
				if configuration.Certificate == nil {
					configuration.Certificate = &models.CertificateConfiguration{}
				}
				configuration.Certificate.ACMEAccountKey = &accountKeyPem
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
		"mailto:" + certificateConfiguration.GetACMEEmail(),
	}}
	if _, err := client.Register(ctx, account, func(string) bool {
		return true
	}); err != nil {
		if err != acme.ErrAccountAlreadyExists {
			if acmeError, ok := err.(*acme.Error); ok && acmeError.StatusCode == http.StatusConflict {
				// Already registered, that's fine.
			} else {
				return nil, err
			}
		}
	}

	self.acmeClient = client
	return client, nil
}

func (self *Manager) persistCertificate(ctx context.Context, tlsCertificate *tls.Certificate, privateKey *rsa.PrivateKey, leaf *x509.Certificate) error {
	// Encode cert chain as PEM.
	var certificateBuffer bytes.Buffer
	for _, der := range tlsCertificate.Certificate {
		if err := pem.Encode(&certificateBuffer, &pem.Block{Type: "CERTIFICATE", Bytes: der}); err != nil {
			return err
		}
	}

	// Encode private key as PEM.
	var keyBuffer bytes.Buffer
	if err := pem.Encode(&keyBuffer, &pem.Block{
		Type:  "RSA PRIVATE KEY",
		Bytes: x509.MarshalPKCS1PrivateKey(privateKey),
	}); err != nil {
		return err
	}

	certificatePem := certificateBuffer.String()
	keyPem := keyBuffer.String()
	issuedAt := leaf.NotBefore
	expiresAt := leaf.NotAfter

	return self.store.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		_, err := tx.ModifyConfiguration(ctx, func(configuration *models.Configuration) error {
			if configuration.Certificate == nil {
				configuration.Certificate = &models.CertificateConfiguration{}
			}
			configuration.Certificate.Certificate = &certificatePem
			configuration.Certificate.PrivateKey = &keyPem
			configuration.Certificate.IssuedAt = &issuedAt
			configuration.Certificate.ExpiresAt = &expiresAt
			return nil
		}, nil)
		return err
	})
}

func (self *Manager) loadCertificateFromStore(ctx context.Context) {
	configuration := self.getConfiguration(ctx)
	if configuration == nil || configuration.Certificate == nil {
		return
	}
	certificateConfiguration := configuration.Certificate
	certificatePem := certificateConfiguration.GetCertificate()
	keyPem := certificateConfiguration.GetPrivateKey()
	if certificatePem == "" || keyPem == "" {
		return
	}

	tlsCertificate, err := tls.X509KeyPair([]byte(certificatePem), []byte(keyPem))
	if err != nil {
		log.Warningf("failed to load stored certificate: %v", err)
		return
	}

	self.certificateMutex.Lock()
	self.certificate = &tlsCertificate
	self.certificateMutex.Unlock()
}

func (self *Manager) getConfiguration(ctx context.Context) *models.Configuration {
	var configuration *models.Configuration
	_ = self.store.Transaction(ctx, func(ctx context.Context, tx store.Transaction) error {
		var err error
		configuration, err = tx.GetConfiguration(ctx, nil)
		return err
	})
	return configuration
}

func leafCertificate(tlsCertificate *tls.Certificate) (*x509.Certificate, error) {
	if len(tlsCertificate.Certificate) == 0 {
		return nil, ErrInvalidCertificate
	}
	return x509.ParseCertificate(tlsCertificate.Certificate[0])
}
