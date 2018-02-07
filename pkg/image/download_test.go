/*
Copyright 2018 Mirantis

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
	"bytes"
	"context"
	"crypto/tls"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	testutils "github.com/Mirantis/virtlet/pkg/utils/testing"
)

// Note that more of downloader tests are in pkg/imagetranslation/transport_test.go
// They examine other aspects like redirects and proxies in conjunction with
// image translation handling.

func downloadHandler(content string) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/base.qcow2" {
			http.NotFound(w, r)
		} else {
			w.Header().Set("Content-Type", "application/octet-stream")
			w.Write([]byte(content))
		}
	}
}

func verifyDownload(t *testing.T, protocol string, content string, ep Endpoint) {
	downloader := NewDownloader(protocol)
	var buf bytes.Buffer
	if err := downloader.DownloadFile(context.Background(), ep, &buf); err != nil {
		t.Fatalf("DownloadFile(): %v", err)
	}
	if buf.String() != content {
		t.Errorf("bad content: %q instead of %q", buf.String(), content)
	}
}

func TestDownload(t *testing.T) {
	ts := httptest.NewServer(downloadHandler("foobar"))
	defer ts.Close()
	verifyDownload(t, "http", "foobar", Endpoint{
		Url: ts.Listener.Addr().String() + "/base.qcow2",
	})
}

func TestTLSDownload(t *testing.T) {
	ca, caKey := testutils.GenerateCert(t, true, "CA", nil, nil)
	cert, key := testutils.GenerateCert(t, false, "127.0.0.1", ca, caKey)
	ts := httptest.NewUnstartedServer(downloadHandler("foobar"))
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
	verifyDownload(t, "https", "foobar", Endpoint{
		Url: ts.Listener.Addr().String() + "/base.qcow2",
		TLS: &TLSConfig{
			Certificates: []TLSCertificate{
				{Certificate: ca},
			},
		},
	})
}

func TestCancelDownload(t *testing.T) {
	startedWriting := make(chan struct{})
	done := make(chan struct{})
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.String() != "/base.qcow2" {
			http.NotFound(w, r)
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Write([]byte("foo"))
		close(startedWriting)
		select {
		case <-time.After(40 * time.Second):
			t.Errorf("request not cancelled within 40s")
		case <-r.Context().Done():
		}
		close(done)
	}))
	defer ts.Close()
	downloader := NewDownloader("http")
	var buf bytes.Buffer
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		<-startedWriting
		cancel()
	}()

	err := downloader.DownloadFile(ctx, Endpoint{
		Url: ts.Listener.Addr().String() + "/base.qcow2",
	}, &buf)
	switch {
	case err == nil:
		t.Errorf("DownloadFile() didn't return error after being cancelled")
	case !strings.Contains(err.Error(), "context canceled"):
		t.Errorf("DownloadFile() is expected to return Cancelled error but returned %q", err)
	}
	<-done
}
