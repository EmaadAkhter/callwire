package callwire

import (
	"crypto/rand"
	"crypto/rsa"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/pem"
	"errors"
	"fmt"
	"log"
	"math/big"
	"net"
	"os"
	"time"
)

// TLSConfig defines the SSL/TLS configuration for server and client.
type TLSConfig struct {
	// CertFile is the path to the certificate PEM file.
	CertFile string
	// KeyFile is the path to the private key PEM file.
	KeyFile string
	// CAFile is the path to the root CA certificate PEM file.
	// If set on the server, it enforces client certificate verification (mTLS).
	// If set on the client, it verifies the server's certificate against this CA.
	CAFile string
	// InsecureSkipVerify skips certificate verification (for testing only).
	InsecureSkipVerify bool
}

// ToGoTLSConfig converts TLSConfig to standard tls.Config.
func (c TLSConfig) ToGoTLSConfig(isServer bool) (*tls.Config, error) {
	cfg := &tls.Config{}

	if c.CertFile != "" && c.KeyFile != "" {
		cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
		if err != nil {
			return nil, fmt.Errorf("failed to load certificate/key: %w", err)
		}
		cfg.Certificates = []tls.Certificate{cert}
	}

	if c.CAFile != "" {
		caCert, err := os.ReadFile(c.CAFile)
		if err != nil {
			return nil, fmt.Errorf("failed to read CA file: %w", err)
		}
		caCertPool := x509.NewCertPool()
		if !caCertPool.AppendCertsFromPEM(caCert) {
			return nil, fmt.Errorf("failed to parse CA certificate")
		}
		if isServer {
			cfg.ClientCAs = caCertPool
			cfg.ClientAuth = tls.RequireAndVerifyClientCert
		} else {
			cfg.RootCAs = caCertPool
		}
	}

	cfg.InsecureSkipVerify = c.InsecureSkipVerify
	return cfg, nil
}

// ServeWithTLS starts the callwire server on addr listening over TLS.
func ServeWithTLS(addr string, config TLSConfig) error {
	tlsCfg, err := config.ToGoTLSConfig(true)
	if err != nil {
		return err
	}
	listener, err := tls.Listen("tcp", addr, tlsCfg)
	if err != nil {
		return err
	}
	defer listener.Close()
	log.Printf("callwire serving TLS on %s", addr)
	for {
		conn, err := listener.Accept()
		if err != nil {
			if errors.Is(err, net.ErrClosed) {
				return nil
			}
			continue
		}
		go handleConnection(conn)
	}
}

// ConnectWithTLS dials a callwire server over TLS and returns a Client.
func ConnectWithTLS(addr string, config TLSConfig) (*Client, error) {
	tlsCfg, err := config.ToGoTLSConfig(false)
	if err != nil {
		return nil, err
	}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return nil, err
	}
	c := &Client{conn: conn, pending: make(map[uint64]chan wireMessage)}
	go c.readLoop()
	return c, nil
}

// ConnectWithReconnectTLS dials a callwire server over TLS and returns a Client with auto-reconnect.
func ConnectWithReconnectTLS(addr string, config TLSConfig) (*Client, error) {
	tlsCfg, err := config.ToGoTLSConfig(false)
	if err != nil {
		return nil, err
	}
	conn, err := tls.Dial("tcp", addr, tlsCfg)
	if err != nil {
		return nil, err
	}
	c := &Client{
		addr:    addr,
		tlsCfg:  tlsCfg,
		conn:    conn,
		pending: make(map[uint64]chan wireMessage),
	}
	go c.readLoopWithReconnect()
	return c, nil
}

// GenSelfSignedCert generates a self-signed root CA and a leaf certificate,
// writing them to certFile, keyFile, and caFile.
// Returns the paths of the created files or an error.
func GenSelfSignedCert(certFile, keyFile, caFile string) error {
	// 1. Generate CA Private Key and Certificate
	caKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	caTemplate := x509.Certificate{
		SerialNumber: big.NewInt(1),
		Subject: pkix.Name{
			Organization: []string{"Callwire CA"},
			CommonName:   "Callwire Root CA",
		},
		NotBefore:             time.Now().Add(-1 * time.Hour),
		NotAfter:              time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
		BasicConstraintsValid: true,
		IsCA:                  true,
	}

	caBytes, err := x509.CreateCertificate(rand.Reader, &caTemplate, &caTemplate, &caKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	// 2. Generate Server/Client Private Key and Certificate (valid for localhost & 127.0.0.1)
	privKey, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		return err
	}

	certTemplate := x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject: pkix.Name{
			Organization: []string{"Callwire Node"},
			CommonName:   "localhost",
		},
		NotBefore:   time.Now().Add(-1 * time.Hour),
		NotAfter:    time.Now().Add(24 * 365 * time.Hour),
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageServerAuth, x509.ExtKeyUsageClientAuth},
		IPAddresses: []net.IP{net.ParseIP("127.0.0.1"), net.ParseIP("::1")},
		DNSNames:    []string{"localhost"},
	}

	certBytes, err := x509.CreateCertificate(rand.Reader, &certTemplate, &caTemplate, &privKey.PublicKey, caKey)
	if err != nil {
		return err
	}

	// Write CA Cert
	caOut, err := os.Create(caFile)
	if err != nil {
		return err
	}
	defer caOut.Close()
	if err := pem.Encode(caOut, &pem.Block{Type: "CERTIFICATE", Bytes: caBytes}); err != nil {
		return err
	}

	// Write Leaf Cert
	certOut, err := os.Create(certFile)
	if err != nil {
		return err
	}
	defer certOut.Close()
	if err := pem.Encode(certOut, &pem.Block{Type: "CERTIFICATE", Bytes: certBytes}); err != nil {
		return err
	}

	// Write Private Key
	keyOut, err := os.OpenFile(keyFile, os.O_WRONLY|os.O_CREATE|os.O_TRUNC, 0600)
	if err != nil {
		return err
	}
	defer keyOut.Close()
	privBytes := x509.MarshalPKCS1PrivateKey(privKey)
	if err := pem.Encode(keyOut, &pem.Block{Type: "RSA PRIVATE KEY", Bytes: privBytes}); err != nil {
		return err
	}

	return nil
}
