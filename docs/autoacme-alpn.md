# AutoACME: Automatic TLS Certificates via ACME TLS-ALPN-01

**Status:** Planning / Design
**Author:** (auto-generated)
**Date:** 2026-03-07

---

## Table of Contents

1. [Overview](#1-overview)
2. [User-Facing Configuration](#2-user-facing-configuration)
3. [Architecture](#3-architecture)
4. [Security](#4-security)
5. [Operational Constraints](#5-operational-constraints)
6. [Implementation Details in Go](#6-implementation-details-in-go)
7. [Testing Plan](#7-testing-plan)
8. [Migration / Rollout](#8-migration--rollout)
9. [Summary & Open Questions](#9-summary--open-questions)

---

## 1. Overview

TeaNode currently runs as an HTTP-only server (default port 8833) and relies on
an external reverse proxy (nginx, Caddy, etc.) for TLS termination. This
proposal adds **native HTTPS support** with automatic certificate provisioning
and renewal via the ACME protocol (Let's Encrypt / ZeroSSL / any RFC 8555
provider).

We use the **TLS-ALPN-01** challenge type exclusively. This challenge operates
entirely on port 443 using a special self-signed certificate with the
`acme-tls/1` ALPN protocol during validation, which means:

- No need for port 80 to be open.
- No need for filesystem webroot access (unlike HTTP-01).
- No DNS provider API integration required (unlike DNS-01).
- **Trade-off:** cannot issue wildcard certificates (those require DNS-01).

### Prior Art

This implementation will be **reused and adapted from
`/home/ziyan/projects/ziyan/wei/backend/util/autoacme`**, an existing ACME
certificate manager used in the Wei project. That implementation provides:

- A `Store` interface for loading/saving certificates (adapted here to persist
  through the TeaNode config store interface instead of filesystem).
- A `Manager` with background renewal loop, `GetCertificate` callback, and
  certificate validation logic.
- ACME client setup with EC P-256 account keys and `golang.org/x/crypto/acme`.

Key differences from the Wei implementation:
- **Challenge type:** Wei uses DNS-01 (Route 53); TeaNode uses TLS-ALPN-01.
- **Storage:** Wei uses filesystem files; TeaNode persists through `models.Configuration.Certificate` via the config store interface.
- **Scope:** Wei supports multiple hosts with wildcard subdomains; TeaNode supports a single domain.

---

## 2. User-Facing Configuration

### 2.1 Config Schema

AutoACME is activated when `Configuration.Gateway.TLS` is set to `true`. No
port 443 check is enforced — the operator is responsible for ensuring the
ACME server can reach port 443 (e.g., via port mapping, `setcap`, or a TCP
passthrough proxy).

A single domain is supported. Certificate and ACME account data are persisted
as fields on `models.Configuration.Certificate`.

```yaml
gateway:
  port: 443          # recommended for ALPN challenge, but not enforced
  bind: lan          # typically lan for public-facing TLS
  tls: true          # master switch — enables AutoACME

certificate:
  # ACME account email — used for expiry notifications and ToS agreement.
  acmeEmail: admin@example.com

  # ACME account private key (PEM-encoded EC P-256). Auto-generated if nil.
  # Persisted by the config store; not manually edited.
  acmeAccountKey: null

  # The single domain for which to obtain a certificate (SNI name).
  domain: teanode.example.com

  # Leaf + intermediate chain (PEM-encoded). Managed automatically.
  certificate: null

  # Certificate private key (PEM-encoded). Managed automatically.
  privateKey: null

  # Timestamp when the certificate was issued.
  issuedAt: null

  # Timestamp when the certificate expires.
  expiresAt: null
```

### 2.2 Field Semantics

All certificate fields live under `models.Configuration.Certificate` with
pointer types (`*string` / `*time.Time`). PEM encoding is implied for key and
certificate material.

| Field              | Type         | Required | Default       | Notes                                                    |
|--------------------|--------------|----------|---------------|----------------------------------------------------------|
| `Gateway.TLS`      | `*bool`      | no       | `false`       | Master enable switch for TLS + AutoACME                  |
| `Certificate.ACMEEmail` | `*string` | yes*   | —             | *Required when `Gateway.TLS=true`                        |
| `Certificate.ACMEAccountKey` | `*string` | no | auto-generated | EC P-256 private key, PEM-encoded                    |
| `Certificate.Domain` | `*string`  | yes*     | —             | *Required when `Gateway.TLS=true`; single domain         |
| `Certificate.Certificate` | `*string` | no   | auto-managed  | Leaf + chain PEM; populated by AutoACME                  |
| `Certificate.PrivateKey` | `*string` | no    | auto-managed  | Certificate private key PEM; populated by AutoACME       |
| `Certificate.IssuedAt` | `*time.Time` | no   | auto-managed  | Set on successful issuance                               |
| `Certificate.ExpiresAt` | `*time.Time` | no  | auto-managed  | Parsed from certificate NotAfter                         |

### 2.3 Persistence Through Config Store

Certificates and keys are persisted **through the config store interface**
(`store.ConfigurationOperation`), not as separate files on disk. This means:

- All data lives in `models.Configuration.Certificate` as pointer fields.
- The existing `GetConfiguration` / `ModifyConfiguration` interface handles
  persistence (filesystem YAML or database JSONB, depending on store backend).
- No separate `storePath` directory, no `meta.json`, no per-domain subdirectories.
- Atomic updates are provided by the config store's `ModifyConfiguration` transaction.

This approach:
- Keeps certificate lifecycle consistent with all other TeaNode configuration.
- Works identically across filesystem and database store backends.
- Avoids a second persistence mechanism and the associated permission/path concerns.

### 2.4 Domain Change Detection

When `Certificate.Domain` changes (compared to the domain in the currently
stored certificate), AutoACME **forces a re-issue** — the existing certificate,
private key, and timestamps are cleared and a new certificate is obtained for
the updated domain.

---

## 3. Architecture

### 3.1 Current Listener Model

Today, `cmd/gateway.go` creates a plain TCP listener:

```go
httpListener, err := net.Listen("tcp", address)  // line 366
httpServer.Serve(httpListener)                    // line 404
```

There is no `tls.Config`, no certificate loading, no TLS at all.

### 3.2 Proposed Listener Model

When `Gateway.TLS=true`, the gateway replaces the plain listener with a
**TLS listener** that uses a dynamic `tls.Config` with `GetCertificate`:

```
                 ┌───────────────────────────────────────┐
   TLS port      │          net.Listen("tcp", address)    │
  ───────────►   │                                        │
                 │   tls.NewListener(tcpListener, &cfg)   │
                 │       cfg.GetCertificate = certMgr.Get │
                 │       cfg.NextProtos = ["h2","http/1.1"]│
                 │                                        │
                 │   httpServer.Serve(tlsListener)        │
                 └───────────────────────────────────────┘
```

The key mechanism is `GetCertificate`: a callback invoked on every TLS
handshake, receiving the `*tls.ClientHelloInfo` (which contains the SNI name).
This allows:

1. **Hot-swapping certificates** without restarting the server.
2. **Serving the ALPN challenge** when the ACME server connects.

### 3.3 ALPN Challenge Responder — No Separate Listener Needed

Unlike HTTP-01 (which requires a separate port-80 listener), TLS-ALPN-01
operates on the **same port** as regular HTTPS traffic. The challenge is
distinguished by the client requesting the `acme-tls/1` ALPN protocol in the
TLS ClientHello.

The `GetCertificate` / `GetConfigForClient` callback inspects the ClientHello:

```
  ClientHello arrives
          │
          ▼
  ┌─────────────────────────────┐
  │ Is "acme-tls/1" in          │──── YES ──► Return challenge cert
  │ hello.SupportedProtos?      │             (self-signed, with
  └─────────────────────────────┘              acmeIdentifier ext)
          │
          NO
          ▼
  Return production cert for hello.ServerName
```

**This means no `SO_REUSEPORT`, no connection multiplexer, and no temporary
listener.** The single TLS listener handles both ACME validation and regular
HTTPS traffic via the callback mechanism.

### 3.4 Sequence Diagram — Initial Certificate Issuance

```
  TeaNode                        ACME Server (Let's Encrypt)
  ───────                        ───────────────────────────
     │                                    │
     │  1. POST /acme/new-account         │
     │  ──────────────────────────────►   │
     │  ◄──────────────────────────────   │
     │     201 Account created            │
     │                                    │
     │  2. POST /acme/new-order           │
     │     identifiers: [example.com]     │
     │  ──────────────────────────────►   │
     │  ◄──────────────────────────────   │
     │     201 Order + authorization URLs │
     │                                    │
     │  3. GET authorization URL          │
     │  ──────────────────────────────►   │
     │  ◄──────────────────────────────   │
     │     tls-alpn-01 challenge token    │
     │                                    │
     │  4. Generate self-signed cert:     │
     │     - SAN = example.com            │
     │     - acmeIdentifier extension     │
     │       = SHA-256(token + thumbprint)│
     │     - ALPN = "acme-tls/1"          │
     │     Install into GetCertificate    │
     │                                    │
     │  5. POST challenge URL             │
     │     (signal readiness)             │
     │  ──────────────────────────────►   │
     │                                    │
     │         6. ACME server connects    │
     │            to :443 with ALPN       │
     │            "acme-tls/1" + SNI      │
     │  ◄──────────────────────────────   │
     │  Serve challenge cert              │
     │  ──────────────────────────────►   │
     │                                    │
     │  7. Poll order status              │
     │  ──────────────────────────────►   │
     │  ◄──────────────────────────────   │
     │     status: valid                  │
     │                                    │
     │  8. POST /acme/finalize            │
     │     (submit CSR)                   │
     │  ──────────────────────────────►   │
     │  ◄──────────────────────────────   │
     │     certificate URL                │
     │                                    │
     │  9. GET certificate                │
     │  ──────────────────────────────►   │
     │  ◄──────────────────────────────   │
     │     PEM chain                      │
     │                                    │
     │  10. Persist cert + key + metadata │
     │      via ModifyConfiguration()     │
     │      Update GetCertificate cache   │
     │      Remove challenge cert         │
```

### 3.5 Sequence Diagram — Certificate Renewal

```
  Background routine (started from gateway.go)
          │
          ▼
  Ticker fires (every 12 hours):
    ├─ Load config → check Certificate.ExpiresAt
    ├─ If (ExpiresAt - now) > 30 days → skip
    ├─ If Domain changed vs. stored cert → force re-issue
    └─ Else → run issuance flow (steps 2-10 above)
              ├─ On success: ModifyConfiguration() to persist
              │              update in-memory cert cache
              └─ On failure: log, schedule retry with backoff
```

### 3.6 Hot Certificate Swap — Zero Downtime

Following the pattern from the Wei `autoacme` implementation, the certificate
is protected by a mutex and swapped atomically:

```go
type CertManager struct {
    mu         sync.RWMutex
    cert       *tls.Certificate       // Current production cert
    challenge  *tls.Certificate       // ALPN challenge cert (transient)
    domain     string                 // Configured domain
}

func (m *CertManager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
    m.mu.RLock()
    defer m.mu.RUnlock()

    // Check for ALPN challenge first.
    for _, proto := range hello.SupportedProtos {
        if proto == "acme-tls/1" {
            if m.challenge != nil {
                return m.challenge, nil
            }
        }
    }

    // Serve production certificate.
    if m.cert != nil {
        return m.cert, nil
    }

    return nil, fmt.Errorf("no certificate available")
}
```

No restart, no SIGHUP, no connection drain required.

---

## 4. Security

### 4.1 Private Key Handling

| Concern | Mitigation |
|---------|-----------|
| Persistence | Keys stored as PEM strings in config store (YAML file 0600 or database JSONB) |
| Memory | Keys loaded into `*tls.Certificate` — Go runtime manages memory; no explicit zeroing (Go limitation) |
| Logging | Private key material MUST NEVER appear in logs. All key operations use opaque handles. |

### 4.2 No Encryption at Rest

Certificate private keys and ACME account keys are stored as **plaintext PEM**
in the config store. No at-rest encryption is provided.

Rationale:
- The config store already contains sensitive material (API keys, secrets).
- Adding a separate encryption layer for cert keys alone would be inconsistent.
- Filesystem store files should be protected by OS-level permissions (0600).
- Database store should be protected by database access controls.

### 4.3 ACME Account Key

- Generated once as EC P-256 (recommended by Let's Encrypt).
- Stored as PEM in `Configuration.Certificate.ACMEAccountKey`.
- Reused across renewals to maintain the same ACME account.
- If lost (field cleared), a new account is created (old certs remain valid until expiry).

### 4.4 Rate Limits, Retries, and Backoff

Let's Encrypt enforces rate limits:

| Limit | Value |
|-------|-------|
| Certificates per registered domain | 50/week |
| Duplicate certificates | 5/week |
| Failed validations | 5/hour per account+hostname+hour |
| New orders | 300/3 hours |

**Retry strategy:**

```
attempt 1: immediate
attempt 2: wait 1 minute
attempt 3: wait 5 minutes
attempt 4: wait 30 minutes
attempt 5: wait 2 hours
then: retry every 12 hours (aligns with renewal check interval)
```

On persistent failure (>24h), emit a **warning log** and (if configured) a
notification via the existing channels system (Discord/Telegram).

Rate limit errors (HTTP 429, `urn:ietf:params:acme:error:rateLimited`) trigger
an immediate 1-hour cooldown regardless of the retry schedule.

---

## 5. Operational Constraints

### 5.1 Port 443 Consideration

TLS-ALPN-01 **requires** the challenge be served on port 443 (hardcoded in the
ACME spec, RFC 8737 §3). However, we do **not** enforce a port 443 check at
startup. The operator is responsible for ensuring port 443 reaches the TeaNode
TLS listener (via direct binding, port forwarding, iptables, or TCP proxy).

The TeaNode process may need permission to bind low ports:
- Use `setcap 'cap_net_bind_service=+ep' /path/to/teanode`, or
- Use systemd socket activation, or
- Use port forwarding from 443 to a high port.

### 5.2 No External Load Balancer TLS Termination

If an external LB (AWS ALB, Cloudflare, etc.) terminates TLS before traffic
reaches TeaNode, the ACME server's TLS-ALPN-01 probe will hit the LB, not
TeaNode. **AutoACME will not work in this topology.**

Supported topologies:

```
✅  Internet ──► TeaNode :443 (direct)
✅  Internet ──► TCP/L4 LB (passthrough) ──► TeaNode :443
❌  Internet ──► TLS-terminating LB ──► TeaNode :8833 (HTTP)
```

The docs should clearly state this and recommend TCP passthrough mode for L4
load balancers (e.g., AWS NLB, HAProxy `mode tcp`).

### 5.3 Interaction with Reverse Proxies

If TeaNode sits behind nginx/Caddy that currently handles TLS:

- **Option A:** Remove the reverse proxy. TeaNode handles TLS directly.
- **Option B:** Keep the reverse proxy but configure it for TCP passthrough
  (stream/L4 mode) so TeaNode sees the raw TLS handshake.
- **Option C:** Don't use AutoACME. Let the reverse proxy manage certs (status quo).

AutoACME and external TLS termination are **mutually exclusive** by design.

### 5.4 Single Domain Only

AutoACME supports **exactly one domain** at a time. The `Certificate.Domain`
field is a `*string` (not a list). This simplifies:

- Certificate management (one cert, one key, one lifecycle).
- The `GetCertificate` callback (no SNI-based lookup map).
- Domain change detection (compare one value).

**Wildcard (`*.example.com`):** **Not supported** by TLS-ALPN-01. Wildcard
certs require DNS-01 challenges.

---

## 6. Implementation Details in Go

### 6.1 Library Choice

**Recommended: `golang.org/x/crypto/acme`** (same as the Wei `autoacme` implementation).

| Library | Pros | Cons |
|---------|------|------|
| `x/crypto/acme` | Minimal, stdlib-adjacent, full control over challenge handling | Must wire ALPN challenge manually |
| `go-acme/lego` | Batteries-included, many DNS providers, built-in ALPN solver | Heavy dependency tree, less control |
| `caddyserver/certmagic` | Production-proven, handles all edge cases | Tight coupling to Caddy internals |

`x/crypto/acme` is preferred because:
1. TeaNode vendors dependencies — smaller footprint matters.
2. We only need TLS-ALPN-01 — no DNS provider plugins.
3. Fine-grained control over the challenge responder callback integrates
   cleanly with our existing `GetCertificate` pattern.
4. Consistency with the Wei `autoacme` implementation we are adapting from.

### 6.2 Proposed Structs

```go
// internal/models/configurations.go — additions

type CertificateConfiguration struct {
    ACMEEmail      *string    `json:"acmeEmail,omitempty"      yaml:"acmeEmail,omitempty"`
    ACMEAccountKey *string    `json:"acmeAccountKey,omitempty" yaml:"acmeAccountKey,omitempty"`
    Domain         *string    `json:"domain,omitempty"         yaml:"domain,omitempty"`
    Certificate    *string    `json:"certificate,omitempty"    yaml:"certificate,omitempty"`
    PrivateKey     *string    `json:"privateKey,omitempty"     yaml:"privateKey,omitempty"`
    IssuedAt       *time.Time `json:"issuedAt,omitempty"       yaml:"issuedAt,omitempty"`
    ExpiresAt      *time.Time `json:"expiresAt,omitempty"      yaml:"expiresAt,omitempty"`
}

// Add to GatewayConfiguration:
//   TLS *bool `json:"tls,omitempty" yaml:"tls,omitempty"`

// Add to Configuration:
//   Certificate *CertificateConfiguration `json:"certificate,omitempty" yaml:"certificate,omitempty"`
```

```go
// internal/acme/manager.go
// Adapted from /home/ziyan/projects/ziyan/wei/backend/util/autoacme

package acme

import (
    "context"
    "crypto/tls"
    "sync"
    "time"

    xacme "golang.org/x/crypto/acme"
)

// CertState tracks the certificate lifecycle.
type CertState int

const (
    CertStateEmpty    CertState = iota // No cert yet
    CertStateValid                     // Cert loaded + not near expiry
    CertStateRenewing                  // Renewal in progress
    CertStateError                     // Last attempt failed
)

// Manager coordinates ACME operations and serves certs.
// Modeled after wei/backend/util/autoacme.Manager but adapted for
// single-domain, config-store persistence, and TLS-ALPN-01 challenges.
type Manager struct {
    mu        sync.RWMutex
    cert      *tls.Certificate       // Current production cert
    challenge *tls.Certificate       // Transient ALPN challenge cert
    domain    string                 // Configured domain
    state     CertState

    client    *xacme.Client
    email     string

    // configStore provides persistence via ModifyConfiguration.
    configStore store.ConfigurationOperation

    stopCh    chan struct{}
}

// GetCertificate is wired into tls.Config.GetCertificate.
func (m *Manager) GetCertificate(hello *tls.ClientHelloInfo) (*tls.Certificate, error) {
    // 1. Check for acme-tls/1 ALPN → serve challenge cert
    // 2. Else → serve production cert
    // (see §3.6 for full implementation)
}

// Run starts the background renewal loop.
// Called from gateway.go as: go certManager.Run(ctx)
func (m *Manager) Run(ctx context.Context) {
    // Ticker: check every 12 hours
    // If cert expires within 30 days, trigger ObtainCert
    // If domain changed, force re-issue
}

// ObtainCert runs the full ACME TLS-ALPN-01 issuance flow.
func (m *Manager) ObtainCert(ctx context.Context) error {
    // 1. Create order for m.domain
    // 2. Fetch authorization
    // 3. Find tls-alpn-01 challenge
    // 4. Build self-signed challenge cert with acmeIdentifier extension
    // 5. Install challenge cert (m.challenge)
    // 6. Accept challenge (POST to challenge URL)
    // 7. Poll order until valid
    // 8. Generate CSR, finalize order
    // 9. Download certificate chain
    // 10. Persist via m.configStore.ModifyConfiguration()
    // 11. Update m.cert, clear m.challenge
}
```

### 6.3 Integration with `cmd/gateway.go`

The background routine is **started from `gateway.go`**:

```go
// In the gateway command action, after building the handler:

if configuration.Gateway != nil && configuration.Gateway.GetTLS() {
    // Validate: domain set, email set.
    certCfg := configuration.Certificate
    if certCfg == nil || certCfg.GetDomain() == "" || certCfg.GetACMEEmail() == "" {
        return fmt.Errorf("tls enabled but certificate.domain and certificate.acmeEmail are required")
    }

    // Initialize ACME manager (adapted from wei autoacme.Open).
    certManager, err := acme.NewManager(acme.ManagerConfig{
        Domain:      certCfg.GetDomain(),
        Email:       certCfg.GetACMEEmail(),
        ConfigStore: configStore,
    })
    if err != nil {
        return fmt.Errorf("failed to initialize ACME manager: %w", err)
    }

    // Load existing cert from config store (if any).
    if err := certManager.LoadFromConfig(configuration); err != nil {
        log.Warningf("failed to load existing certificate: %v", err)
    }

    // Build TLS config.
    goTLSConfig := &tls.Config{
        GetCertificate: certManager.GetCertificate,
        NextProtos:     []string{"h2", "http/1.1", "acme-tls/1"},
        MinVersion:     tls.VersionTLS12,
    }

    // Replace plain TCP listener with TLS listener.
    address := listenAddress(configuration)
    tcpListener, err := net.Listen("tcp", address)
    if err != nil {
        return err
    }
    tlsListener := tls.NewListener(tcpListener, goTLSConfig)

    // Start background renewal routine.
    go certManager.Run(ctx)

    // Serve HTTPS.
    log.Infof("TeaNode gateway listening on %s (TLS)", address)
    httpServer := &http.Server{Handler: handler, TLSConfig: goTLSConfig}
    if err := httpServer.Serve(tlsListener); err != nil && err != http.ErrServerClosed {
        log.Errorf("https server exited with error: %v", err)
    }
} else {
    // Existing plain HTTP path (unchanged).
    // ...
}
```

### 6.4 State Machine

The single domain transitions through these states:

```
                    ┌──────────┐
     startup ──────►│  Empty   │
                    └────┬─────┘
                         │ ObtainCert() succeeds
                         ▼
                    ┌──────────┐   renewBefore window   ┌───────────┐
                    │  Valid   │ ──────────────────────► │ Renewing  │
                    └──────────┘                        └─────┬─────┘
                         ▲          domain changed             │
                         │ ◄───────────────────────────        │ ObtainCert() fails
                         │ (force re-issue)                    ▼
                         │                              ┌───────────┐
                         └────── retry timer fires ──── │   Error   │
                                                        └───────────┘
```

- **Empty → Valid:** First successful issuance.
- **Valid → Renewing:** Clock-based; `ExpiresAt - now < 30 days`.
- **Valid → Empty → Renewing:** Domain changed; clear old cert, re-issue.
- **Renewing → Valid:** Successful renewal.
- **Renewing → Error:** Failed renewal; existing cert still served until it
  actually expires.
- **Error → Renewing:** Retry timer fires (exponential backoff).

### 6.5 ALPN Challenge Certificate Construction

Per RFC 8737, the challenge certificate must:

```go
func buildALPNChallengeCert(domain, token string, accountKey crypto.Signer) (*tls.Certificate, error) {
    // 1. Compute key authorization: token + "." + JWK thumbprint of account key
    thumbprint, _ := (&jose.JSONWebKey{Key: accountKey.Public()}).Thumbprint(crypto.SHA256)
    keyAuth := token + "." + base64url(thumbprint)

    // 2. SHA-256 hash of key authorization
    authHash := sha256.Sum256([]byte(keyAuth))

    // 3. Build acmeIdentifier extension (OID 1.3.6.1.5.5.7.1.31)
    //    Value is ASN.1 OCTET STRING of the 32-byte hash
    acmeIdentifierOID := asn1.ObjectIdentifier{1, 3, 6, 1, 5, 5, 7, 1, 31}
    extValue, _ := asn1.Marshal(authHash[:])

    // 4. Self-signed cert, SAN = domain, critical acmeIdentifier extension
    template := &x509.Certificate{
        SerialNumber: randomSerial(),
        Subject:      pkix.Name{CommonName: "ACME challenge"},
        NotBefore:    time.Now().Add(-1 * time.Hour),
        NotAfter:     time.Now().Add(24 * time.Hour),
        DNSNames:     []string{domain},
        ExtraExtensions: []pkix.Extension{{
            Id:       acmeIdentifierOID,
            Critical: true,
            Value:    extValue,
        }},
    }

    // 5. Generate ephemeral key, self-sign
    challengeKey, _ := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
    certDER, _ := x509.CreateCertificate(rand.Reader, template, template, &challengeKey.PublicKey, challengeKey)

    return &tls.Certificate{
        Certificate: [][]byte{certDER},
        PrivateKey:  challengeKey,
    }, nil
}
```

---

## 7. Testing Plan

### 7.1 Unit Tests

| Component | Test |
|-----------|------|
| `CertManager.GetCertificate` | Returns challenge cert when ALPN contains `acme-tls/1`; returns production cert otherwise; returns error when no cert. |
| `buildALPNChallengeCert` | Verify SAN, critical extension OID, acmeIdentifier value matches expected SHA-256. |
| State machine transitions | `Empty→Valid`, `Valid→Renewing`, `Renewing→Error`, `Error→Renewing` with mock timers. |
| Config validation | Rejects: missing domain, missing email when TLS enabled. |
| Domain change detection | Detects when `Certificate.Domain` differs from stored cert's SAN; triggers re-issue. |
| Retry/backoff logic | Verify exponential delays, rate-limit cooldown. |
| Config store persistence | Write cert+key+timestamps via `ModifyConfiguration` → read back → compare. |

### 7.2 Integration Tests with Pebble

[Pebble](https://github.com/letsencrypt/pebble) is Let's Encrypt's
purpose-built test ACME server.

**Test environment:**

```
┌──────────────┐         ┌──────────────┐
│   Pebble     │◄────────│  TeaNode     │
│  ACME server │  ACME   │  (test mode) │
│  :14000      │  proto  │  :5001 (TLS) │
└──────┬───────┘         └──────────────┘
       │                        ▲
       │  TLS-ALPN-01 probe     │
       └────────────────────────┘
         connects to :5001 with
         acme-tls/1 ALPN
```

**Integration test cases:**

1. **Happy path — full issuance:**
   - Start TeaNode with ACME directory pointing to Pebble.
   - Verify certificate is obtained and served on subsequent HTTPS requests.
   - Verify `Certificate.*` fields populated in config store.

2. **Renewal:**
   - Issue a cert, then artificially set `ExpiresAt` to near-future.
   - Trigger renewal check, verify new cert replaces old in config store.

3. **Hot swap:**
   - Establish a long-lived HTTPS connection.
   - Trigger renewal.
   - New connection gets new cert; old connection continues on old cert.

4. **Challenge failure:**
   - Configure Pebble to reject challenges.
   - Verify TeaNode logs error, enters `Error` state, retries with backoff.

5. **Domain change:**
   - Issue cert for `a.example.com`, then change domain to `b.example.com`.
   - Verify old cert is cleared and new cert is issued.

6. **Startup with existing cert:**
   - Pre-populate `Certificate.*` fields in config store.
   - Start gateway, verify cert loaded from config (no ACME call).

### 7.3 Manual / Staging Tests

Before production rollout, test against **Let's Encrypt Staging**:

```yaml
gateway:
  port: 443
  bind: lan
  tls: true

certificate:
  acmeEmail: test@example.com
  domain: test.example.com
```

Staging has much higher rate limits and issues certs from a fake CA — safe for
testing without burning production rate limits.

---

## 8. Migration / Rollout

### 8.1 Interaction with Existing Manual Cert Config

TeaNode currently has **no manual cert configuration** (no TLS support at all).
However, once AutoACME ships, users may later want to provide their own
certificates (e.g., for internal CAs or purchased certs).

**Proposed precedence:**

```
1. Manual cert (if Certificate.Certificate and Certificate.PrivateKey are
   set but ACMEEmail is not) → Use those directly; skip ACME entirely.
2. AutoACME (Gateway.TLS=true, ACMEEmail + Domain set)
   → Manage certs automatically.
3. Neither → plain HTTP (current behavior).
```

This ensures manual certs always take priority, providing an escape hatch.

### 8.2 Rollout Plan

**Phase 1 — Foundation (this PR):**
- Add `CertificateConfiguration` struct and config validation.
- Add `Gateway.TLS` field.
- Implement `CertManager` with `GetCertificate`.
- Config store persistence for cert/key/metadata.
- Wire TLS listener into `cmd/gateway.go`.
- Unit tests for all components.

**Phase 2 — ACME integration:**
- Implement `ObtainCert` with `x/crypto/acme` (adapted from Wei `autoacme`).
- TLS-ALPN-01 challenge cert builder (replacing Wei's DNS-01/Route 53 flow).
- Background renewal loop with state machine, started from `gateway.go`.
- Domain change detection and forced re-issue.
- Integration tests with Pebble.

**Phase 3 — Hardening:**
- Retry/backoff with rate limit awareness.
- Notification integration (Discord/Telegram on cert failure).
- Documentation in `getting-started.md`.

**Phase 4 — Future enhancements (out of scope):**
- Manual cert import (populate `Certificate.*` fields without ACME).
- DNS-01 challenge support for wildcards.
- OCSP stapling.
- Automatic HTTP→HTTPS redirect (optional port-80 listener).

### 8.3 Config Schema Update

Add to `internal/schemas/config.schema.json`:

```json
"gateway": {
  "properties": {
    "tls": { "type": "boolean", "default": false }
  }
},
"certificate": {
  "type": "object",
  "properties": {
    "acmeEmail":      { "type": "string", "format": "email" },
    "acmeAccountKey": { "type": ["string", "null"] },
    "domain":         { "type": ["string", "null"] },
    "certificate":    { "type": ["string", "null"] },
    "privateKey":     { "type": ["string", "null"] },
    "issuedAt":       { "type": ["string", "null"], "format": "date-time" },
    "expiresAt":      { "type": ["string", "null"], "format": "date-time" }
  }
}
```

---

## 9. Summary & Open Questions

### Summary

AutoACME adds native TLS to TeaNode with zero-configuration certificate
management. By using TLS-ALPN-01, we avoid the complexity of port-80 listeners
or DNS provider integrations. The design integrates cleanly with the existing
gateway architecture: a single TLS listener with a `GetCertificate` callback
handles both ACME validation and production traffic.

Key design decisions:
- **Activation:** `Gateway.TLS = true` enables TLS; no port 443 enforcement.
- **Single domain:** `Certificate.Domain` (`*string`), not a list.
- **Persistence:** All cert/key/metadata stored in `models.Configuration.Certificate`
  via the config store interface — no separate filesystem paths.
- **No at-rest encryption:** Keys stored as plaintext PEM in config store.
- **Background routine:** Started from `gateway.go` via `go certManager.Run(ctx)`.
- **Adapted from Wei:** Core ACME flow reused from
  `/home/ziyan/projects/ziyan/wei/backend/util/autoacme`, with DNS-01 replaced
  by TLS-ALPN-01 and filesystem replaced by config store persistence.
- **Domain change:** Changing `Certificate.Domain` forces a re-issue.

### Open Questions

1. **ECDSA vs. RSA for leaf certificates:**
   ECDSA P-256 is smaller and faster for TLS handshakes. RSA 2048 has broader
   (legacy) client compatibility. The Wei implementation uses RSA 2048. Default
   to ECDSA P-256 with an option to configure RSA?

2. **HTTP→HTTPS redirect:**
   Should we optionally bind a second listener on port 80 that returns 301
   redirects to HTTPS? This is common practice but adds another port
   requirement. Could be Phase 4.

3. **Notification on cert failure:**
   The design mentions using existing Discord/Telegram channels. Should this be
   a first-class feature in Phase 2, or deferred to Phase 3? Cert expiry without
   notification is a production risk.

4. **Interaction with `publicUrl`:**
   When AutoACME is enabled, should `publicUrl` be auto-derived from
   `Certificate.Domain` (i.e., `https://<domain>`) if not explicitly set?
