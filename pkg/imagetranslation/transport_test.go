/*
Copyright 2017 Mirantis

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

package imagetranslation

import (
	"context"
	"crypto/tls"
	"fmt"
	"io/ioutil"
	"net/http"
	"net/http/httptest"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Mirantis/virtlet/pkg/image"
	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

func translate(config ImageTranslation, name string, server *httptest.Server) image.Endpoint {
	for i, rule := range config.Rules {
		config.Rules[i].Url = strings.Replace(rule.Url, "%", server.Listener.Addr().String(), 1)
	}
	configs := map[string]ImageTranslation{"config": config}

	translator := NewImageNameTranslator()
	translator.LoadConfigs(context.Background(), NewFakeConfigSource(configs))
	return translator.Translate(name)
}

func intptr(v int) *int {
	return &v
}

func download(t *testing.T, proto string, config ImageTranslation, name string, server *httptest.Server) {
	downloader := image.NewDownloader(proto)
	if err := downloader.DownloadFile(context.Background(), translate(config, name, server), ioutil.Discard); err != nil {
		t.Fatal(err)
	}
}

func TestMain(m *testing.M) {
	os.Unsetenv("HTTP_PROXY")
	os.Unsetenv("HTTPS_PROXY")
	m.Run()
}

func TestImageDownload(t *testing.T) {
	handled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = true
		if r.URL.String() != "/base.qcow2" {
			t.Fatalf("unexpected URL %s", r.URL)
		}
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	config := ImageTranslation{
		Prefix: "test",
		Rules: []TranslationRule{
			{
				Name: "image1",
				Url:  "http://%/base.qcow2",
			},
		},
	}

	download(t, "https", config, "test/image1", ts)
	if !handled {
		t.Fatal("image was not downloaded")
	}
}

func TestImageDownloadRedirects(t *testing.T) {
	var urls []string
	var handledCount int
	var maxRedirects int

	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		urls = append(urls, r.URL.String())
		if handledCount < maxRedirects {
			w.Header().Add("Location", fmt.Sprintf("/file%d", handledCount+1))
			w.WriteHeader(301)
		}
		handledCount++
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name:      "image1",
				Url:       "http://%/base.qcow2",
				Transport: "profile1",
			},
			{
				Name:      "image2",
				Url:       "http://%/base.qcow2",
				Transport: "profile2",
			},
			{
				Name:      "image3",
				Url:       "http://%/base.qcow2",
				Transport: "profile3",
			},
			{
				Name:      "image4",
				Url:       "http://%/base.qcow2",
				Transport: "profile4",
			},
		},
		Transports: map[string]TransportProfile{
			"profile1": {MaxRedirects: intptr(0)},
			"profile2": {MaxRedirects: intptr(1)},
			"profile3": {MaxRedirects: intptr(5)},
			"profile4": {MaxRedirects: nil},
		},
	}

	downloader := image.NewDownloader("http")
	for _, tst := range []struct {
		name         string
		image        string
		mr           int
		expectedUrls int
		mustFail     bool
		message      string
	}{
		{
			name:         "0 redirects, 0 allowed",
			image:        "image1",
			mr:           0,
			expectedUrls: 1,
			mustFail:     false,
			message:      "image download without redirects must succeed even if no redirects allowed",
		},
		{
			name:         "1 redirect, 0 allowed",
			image:        "image1",
			mr:           1,
			expectedUrls: 1,
			mustFail:     true,
			message:      "image download with redirects cannot succeed when no redirects allowed",
		},
		{
			name:         "1 redirect, 1 allowed",
			image:        "image2",
			mr:           1,
			expectedUrls: 2,
			mustFail:     false,
			message:      "image download must succeed when number of redirects doesn't exceed maximum",
		},
		{
			name:         "5 redirect, 5 allowed",
			image:        "image3",
			mr:           5,
			expectedUrls: 6,
			mustFail:     false,
			message:      "image download must succeed when number of redirects doesn't exceed maximum",
		},
		{
			name:         "2 redirect, 1 allowed",
			image:        "image2",
			mr:           2,
			expectedUrls: 2,
			mustFail:     true,
			message:      "image download must fail when number of redirects exceeds maximum value",
		},
		{
			name:         "10 redirect, 5 allowed",
			image:        "image3",
			mr:           10,
			expectedUrls: 6,
			mustFail:     true,
			message:      "image download must fail when number of redirects exceeds maximum value",
		},
		{
			name:         "9 redirect, 9 (default) allowed",
			image:        "image4",
			mr:           9,
			expectedUrls: 10,
			mustFail:     false,
			message:      "image download must not fail when number of redirects doesn't exceed maximum value",
		},
		{
			name:         "10 redirect, 9 (default) allowed",
			image:        "image4",
			mr:           10,
			expectedUrls: 10,
			mustFail:     true,
			message:      "image download must fail when number of redirects exceeds maximum value",
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			urls = nil
			handledCount = 0
			maxRedirects = tst.mr
			err := downloader.DownloadFile(context.Background(), translate(config, tst.image, ts), ioutil.Discard)
			if handledCount == 0 {
				t.Error("http handler wasn't called")
			} else if (err != nil) != tst.mustFail {
				t.Error(tst.message)
			}

			if len(urls) != tst.expectedUrls {
				t.Errorf("unexpected number of redirects for %q: %d != %d", tst.image, len(urls), tst.expectedUrls)
			} else {
				for i, r := range urls {
					if i == 0 && r != "/base.qcow2" || i > 0 && r != fmt.Sprintf("/file%d", i) {
						t.Errorf("unexpected URL #%d %s for %q", i, r, tst.image)
					}
				}
			}
		})
	}
}

func TestImageDownloadWithProxy(t *testing.T) {
	handled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = true
		if r.URL.String() != "http://example.net/base.qcow2" {
			t.Fatalf("proxy server was used for wrong URL %v", r.URL)
		}
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name: "image1",
				Url:  "example.net/base.qcow2",
			},
		},
		Transports: map[string]TransportProfile{
			"": {Proxy: "http://" + ts.Listener.Addr().String()},
		},
	}

	download(t, "http", config, "image1", ts)
	if !handled {
		t.Fatal("image was not downloaded")
	}
}

func TestImageDownloadWithTimeout(t *testing.T) {
	handled := false
	var timeout time.Duration
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = true
		time.Sleep(timeout)
	})
	ts := httptest.NewServer(handler)
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name: "image",
				Url:  "%/base.qcow2",
			},
		},
		Transports: map[string]TransportProfile{
			"": {TimeoutMilliseconds: 250},
		},
	}

	downloader := image.NewDownloader("http")
	for _, tst := range []struct {
		name     string
		timeout  time.Duration
		mustFail bool
	}{
		{
			name:     "positive test",
			timeout:  time.Millisecond * 50,
			mustFail: false,
		},
		{
			name:     "negative test",
			timeout:  time.Millisecond * 350,
			mustFail: true,
		},
	} {
		t.Run(tst.name, func(t *testing.T) {
			handled = false
			timeout = tst.timeout
			err := downloader.DownloadFile(context.Background(), translate(config, "image", ts), ioutil.Discard)
			if err == nil && tst.mustFail {
				t.Error("no error happened when timeout was expected")
			} else if err != nil && !tst.mustFail {
				t.Fatal(err)
			}
			if !handled {
				t.Fatal("image was not downloaded")
			}
		})
	}
}

func TestImageDownloadTLS(t *testing.T) {
	ca, caKey := testutils.GenerateCert(t, true, "CA", nil, nil)
	cert, key := testutils.GenerateCert(t, false, "127.0.0.1", ca, caKey)

	handled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = r.TLS != nil
	})
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{cert.Raw},
				PrivateKey:  key,
			},
		},
	}
	ts.StartTLS()
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name:      "image1",
				Url:       "%/base.qcow2",
				Transport: "tlsProfile",
			},
		},
		Transports: map[string]TransportProfile{
			"tlsProfile": {
				TLS: &TLSConfig{
					Certificates: []TLSCertificate{
						{Cert: testutils.EncodePEMCert(ca)},
					},
				},
			},
		},
	}

	download(t, "https", config, "image1", ts)
	if !handled {
		t.Fatal("image was not downloaded")
	}
}

func TestImageDownloadTLSWithClientCerts(t *testing.T) {
	ca, caKey := testutils.GenerateCert(t, true, "CA", nil, nil)
	serverCert, serverKey := testutils.GenerateCert(t, false, "127.0.0.1", ca, caKey)
	clientCert, clientKey := testutils.GenerateCert(t, false, "127.0.0.1", serverCert, serverKey)

	handled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = r.TLS != nil
		if len(r.TLS.PeerCertificates) != 1 {
			t.Fatal("client certificate wasn't used")
		}
		if r.TLS.PeerCertificates[0].SerialNumber.Cmp(clientCert.SerialNumber) != 0 {
			t.Error("wrong certificate was used")
		}
	})
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{serverCert.Raw},
				PrivateKey:  serverKey,
			},
		},
		ClientAuth: tls.RequestClientCert,
	}
	ts.StartTLS()
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name:      "image",
				Url:       "%/base.qcow2",
				Transport: "tlsProfile",
			},
		},
		Transports: map[string]TransportProfile{
			"tlsProfile": {
				TLS: &TLSConfig{
					Certificates: []TLSCertificate{
						{
							Cert: testutils.EncodePEMCert(ca),
						},
						{
							Cert: testutils.EncodePEMCert(clientCert),
							Key:  testutils.EncodePEMKey(clientKey),
						},
					},
				},
			},
		},
	}

	download(t, "https", config, "image", ts)
	if !handled {
		t.Fatal("image was not downloaded")
	}
}

func TestImageDownloadTLSWithServerName(t *testing.T) {
	ca, caKey := testutils.GenerateCert(t, true, "CA", nil, nil)
	cert, key := testutils.GenerateCert(t, false, "test.corp", ca, caKey)

	handled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = r.TLS != nil
	})
	ts := httptest.NewUnstartedServer(handler)
	ts.TLS = &tls.Config{
		Certificates: []tls.Certificate{
			{
				Certificate: [][]byte{cert.Raw},
				PrivateKey:  key,
			},
		},
	}
	ts.StartTLS()
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name:      "image",
				Url:       "%/base.qcow2",
				Transport: "tlsProfile",
			},
		},
		Transports: map[string]TransportProfile{
			"tlsProfile": {
				TLS: &TLSConfig{
					Certificates: []TLSCertificate{
						{Cert: testutils.EncodePEMCert(ca)},
					},
					ServerName: "test.corp",
				},
			},
		},
	}

	download(t, "https", config, "image", ts)
	if !handled {
		t.Fatal("image was not downloaded")
	}
}

func TestImageDownloadTLSWithoutCertValidation(t *testing.T) {
	handled := false
	handler := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		handled = r.TLS != nil
	})
	ts := httptest.NewUnstartedServer(handler)
	ts.StartTLS()
	defer ts.Close()

	config := ImageTranslation{
		Rules: []TranslationRule{
			{
				Name:      "image",
				Url:       "%/base.qcow2",
				Transport: "tlsProfile",
			},
		},
		Transports: map[string]TransportProfile{
			"tlsProfile": {
				TLS: &TLSConfig{Insecure: true},
			},
		},
	}

	download(t, "https", config, "image", ts)
	if !handled {
		t.Fatal("image was not downloaded")
	}
}
