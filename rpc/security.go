package rpc

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"os"
)

func certPool(path string, system bool) (*x509.CertPool, error) {
	var pool *x509.CertPool
	if system {
		var err error
		pool, err = x509.SystemCertPool()
		if err != nil {
			return nil, errors.New("load system trust store failed")
		}
	} else {
		pool = x509.NewCertPool()
	}
	data, err := os.ReadFile(path)
	if err != nil || !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("load RPC CA failed")
	}
	return pool, nil
}
func keyPair(cert, key string) (tls.Certificate, error) {
	pair, err := tls.LoadX509KeyPair(cert, key)
	if err != nil {
		return tls.Certificate{}, errors.New("load RPC TLS key pair failed")
	}
	return pair, nil
}
func serverTLS(c ServerConfig) (*tls.Config, error) {
	if c.TLSCertFile == "" {
		return nil, nil
	}
	pair, err := keyPair(c.TLSCertFile, c.TLSKeyFile)
	if err != nil {
		return nil, err
	}
	result := &tls.Config{MinVersion: tls.VersionTLS12, Certificates: []tls.Certificate{pair}}
	if c.ClientCAFile != "" {
		result.ClientCAs, err = certPool(c.ClientCAFile, false)
		if err != nil {
			return nil, err
		}
		result.ClientAuth = tls.RequireAndVerifyClientCert
	}
	return result, nil
}
func clientTLS(c ClientConfig) (*tls.Config, error) {
	result := &tls.Config{MinVersion: tls.VersionTLS12}
	var err error
	if c.CAFile != "" {
		result.RootCAs, err = certPool(c.CAFile, true)
		if err != nil {
			return nil, err
		}
	}
	if c.TLSCertFile != "" {
		pair, err := keyPair(c.TLSCertFile, c.TLSKeyFile)
		if err != nil {
			return nil, err
		}
		result.Certificates = []tls.Certificate{pair}
	}
	return result, nil
}
