/*
Copyright 2018 Mirantis

Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or ≈git-agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package v1

import (
	meta_v1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// TranslationRule represents a single translation rule from either name or regexp to Endpoint
type TranslationRule struct {
	// Name defines a mapping from a fixed name
	Name string `yaml:"name,omitempty" json:"name,omitempty"`

	// Regex defines a mapping from all names that match this regexp. In this case replacements can be used for Endpoint.URL
	Regex string `yaml:"regexp,omitempty" json:"regexp,omitempty"`

	// URL is the image URL
	URL string `yaml:"url,omitempty" json:"url,omitempty"`

	// Transport is the optional transport profile name to be used for the downloading
	Transport string `yaml:"transport,omitempty" json:"transport,omitempty"`
}

// ImageTranslation is a single translation config with optional prefix name
type ImageTranslation struct {
	// Prefix allows to have several config-sets and distinguish them by using `prefix/imageName` notation. Optional.
	Prefix string `yaml:"prefix,omitempty" json:"prefix,omitempty"`

	// Rules is a list of translations
	Rules []TranslationRule `yaml:"translations" json:"translations"`

	// Transports is a map of available transport profiles available for endpoints
	Transports map[string]TransportProfile `yaml:"transports" json:"transports"`
}

// TransportProfile contains all the http transport settings
type TransportProfile struct {
	// MaxRedirects is the maximum number of redirects that downloader is allowed to follow. Default is 9 (download fails on request #10)
	MaxRedirects *int `yaml:"maxRedirects,omitempty" json:"maxRedirects,omitempty"`

	// TLS config
	TLS *TLSConfig `yaml:"tls,omitempty" json:"tls,omitempty"`

	// TimeoutMilliseconds specifies a time limit in milliseconds for http(s) download request. <= 0 is no timeout (default)
	TimeoutMilliseconds int `yaml:"timeout,omitempty" json:"timeout,omitempty"`

	// Proxy server to use for downloading
	Proxy string `yaml:"proxy,omitempty" json:"proxy,omitempty"`
}

// TLSConfig has the TLS transport parameters
type TLSConfig struct {
	// Certificates - TLS certificates to use for connection
	Certificates []TLSCertificate `yaml:"certificates,omitempty" json:"certificates,omitempty"`

	// ServerName is used to verify the hostname on the returned certificates. Needed when url points to domain that
	// differs from CN of certificate
	ServerName string `yaml:"serverName,omitempty" json:"serverName,omitempty"`

	// Insecure is a flag to bypass server certificate validation
	Insecure bool `yaml:"insecure,omitempty" json:"insecure,omitempty"`
}

// TLSCertificate has the x509 certificate PEM data with optional PEM private key
type TLSCertificate struct {
	// Cert certificate (PEM) block
	Cert string `yaml:"cert,omitempty" json:"cert,omitempty"`

	// Key - keypair (PEM) block
	Key string `yaml:"key,omitempty" json:"key,omitempty"`
}

// +genclient
// +genclient:noStatus
// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VirtletImageMapping represents an ImageTranslation wrapped in k8s object.
type VirtletImageMapping struct {
	meta_v1.TypeMeta   `json:",inline"`
	meta_v1.ObjectMeta `json:"metadata"`
	Spec               ImageTranslation `json:"spec"`
}

// ConfigName returns the name of the config.
func (vim *VirtletImageMapping) ConfigName() string { return vim.Name }

// Payload returns the actual translation for the mapping.
func (vim *VirtletImageMapping) Payload() (ImageTranslation, error) {
	return vim.Spec, nil
}

// +k8s:deepcopy-gen:interfaces=k8s.io/apimachinery/pkg/runtime.Object

// VirtletImageMappingList is a k8s representation of list of translation configs.
type VirtletImageMappingList struct {
	meta_v1.TypeMeta `json:",inline"`
	meta_v1.ListMeta `json:"metadata"`
	Items            []VirtletImageMapping `json:"items"`
}
