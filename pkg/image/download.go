/*
Copyright 2017-2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package image

import (
	"context"
	"crypto"
	"crypto/tls"
	"crypto/x509"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/golang/glog"
)

const (
	copyBufferSize = 1024 * 1024
)

// Endpoint contains all the endpoint parameters needed to download a file
type Endpoint struct {
	// Url is the image URL. If protocol is omitted, the
	// configured default one is used.
	Url string

	// MaxRedirects is the maximum number of redirects that downloader is allowed to follow. -1 for stdlib default (fails on request #10)
	MaxRedirects int

	// TLS is the TLS config
	TLS *TLSConfig

	// Timeout specifies a time limit for http(s) download request. <= 0 is no timeout (default)
	Timeout time.Duration

	// Proxy is the proxy server to use. Default = use proxy from HTTP_PROXY environment variable
	Proxy string

	// Transport profile name for this endpoint. Provided for logging/debugging
	ProfileName string
}

// TLSConfig has the TLS transport parameters
type TLSConfig struct {
	// Certificates to use (both CA and for client authentication)
	Certificates []TLSCertificate

	// ServerName is needed when connecting to domain other that certificate was issued for
	ServerName string

	// Insecure skips certificate verification
	Insecure bool
}

// TLSCertificate is a x509 certificate with optional private key
type TLSCertificate struct {
	// Certificate is the x509 certificate
	Certificate *x509.Certificate

	// PrivateKey is the private key needed for certificate-based client authentication
	PrivateKey crypto.PrivateKey
}

// Downloader is an interface for downloading files from web
type Downloader interface {
	// DownloadFile downloads the specified file
	DownloadFile(ctx context.Context, endpoint Endpoint, w io.Writer) error
}

type defaultDownloader struct {
	protocol string
}

// NewDownloader returns the default downloader for 'protocol'.
// The default downloader downloads a file via an URL constructed as
// 'protocol://location' and saves it in temporary file in default
// system directory for temporary files
func NewDownloader(protocol string) Downloader {
	return &defaultDownloader{protocol}
}

func buildTLSConfig(config *TLSConfig, profileName string) (*tls.Config, error) {
	var certificates []tls.Certificate
	roots, err := x509.SystemCertPool()
	if err != nil {
		roots = x509.NewCertPool()
	}
	for _, cert := range config.Certificates {
		if cert.Certificate.IsCA {
			roots.AddCert(cert.Certificate)
		} else if cert.PrivateKey != nil {
			certificates = append(certificates, tls.Certificate{
				Certificate: [][]byte{cert.Certificate.Raw},
				PrivateKey:  cert.PrivateKey,
			})
		} else {
			glog.V(3).Infof("Skipping certificate %q because it is neither CA not has a private key", cert.Certificate.SerialNumber.String())
		}
	}

	return &tls.Config{
		Certificates:       certificates,
		RootCAs:            roots,
		InsecureSkipVerify: config.Insecure,
		ServerName:         config.ServerName,
	}, nil
}

func createTransport(endpoint Endpoint) (*http.Transport, error) {
	var tlsConfig *tls.Config
	var err error
	if endpoint.TLS != nil {
		tlsConfig, err = buildTLSConfig(endpoint.TLS, endpoint.ProfileName)
		if err != nil {
			return nil, err
		}
	}

	proxyFunc := http.ProxyFromEnvironment
	if endpoint.Proxy != "" {
		proxyFunc = func(*http.Request) (*url.URL, error) {
			return url.Parse(endpoint.Proxy)
		}
	}

	return &http.Transport{
		Proxy: proxyFunc,
		DialContext: (&net.Dialer{
			Timeout:   30 * time.Second,
			KeepAlive: 30 * time.Second,
			DualStack: true,
		}).DialContext,
		MaxIdleConns:          100,
		IdleConnTimeout:       90 * time.Second,
		TLSHandshakeTimeout:   10 * time.Second,
		ExpectContinueTimeout: 1 * time.Second,
		TLSClientConfig:       tlsConfig,
	}, nil
}

func createHttpClient(endpoint Endpoint) (*http.Client, error) {
	transport, err := createTransport(endpoint)
	if err != nil {
		return nil, err
	}

	var checkRedirects func(req *http.Request, via []*http.Request) error
	if endpoint.MaxRedirects >= 0 {
		checkRedirects = func(req *http.Request, via []*http.Request) error {
			if len(via) > endpoint.MaxRedirects {
				return fmt.Errorf("stopped after %d redirects", endpoint.MaxRedirects)
			}
			return nil
		}
	}

	return &http.Client{
		Transport:     transport,
		Timeout:       endpoint.Timeout,
		CheckRedirect: checkRedirects,
	}, nil
}

func (d *defaultDownloader) DownloadFile(ctx context.Context, endpoint Endpoint, w io.Writer) error {
	url := endpoint.Url
	if !strings.Contains(url, "://") {
		url = fmt.Sprintf("%s://%s", d.protocol, url)
	}

	client, err := createHttpClient(endpoint)
	if err != nil {
		return err
	}

	glog.V(2).Infof("Start downloading %s", url)

	req, err := http.NewRequest("GET", url, nil)
	if err != nil {
		return err
	}
	req = req.WithContext(ctx)
	resp, err := client.Do(req)
	if err != nil {
		return err
	}

	if _, err = io.CopyBuffer(w, resp.Body, make([]byte, copyBufferSize)); err != nil {
		return err
	}

	if f, ok := w.(*os.File); ok {
		glog.V(2).Infof("Data from url %s saved as %q, sha256 digest = %s", url, f.Name())
	}
	return nil
}

// Note that the tests for defaultDownloader are in 'imagetranslation' package (FIXME)
