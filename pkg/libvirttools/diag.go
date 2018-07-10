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

package libvirttools

import (
	"fmt"

	libvirtxml "github.com/libvirt/libvirt-go-xml"

	"github.com/Mirantis/virtlet/pkg/diag"
	"github.com/Mirantis/virtlet/pkg/virt"
)

// LibvirtDiagSource dumps libvirt domains, storage pools and storage
// volumes as a set of XML files.
type LibvirtDiagSource struct {
	domainConn  virt.DomainConnection
	storageConn virt.StorageConnection
}

var _ diag.DiagSource = &LibvirtDiagSource{}

// NewLibvirtDiagSource creates a new LibvirtDiagSource.
func NewLibvirtDiagSource(domainConn virt.DomainConnection, storageConn virt.StorageConnection) *LibvirtDiagSource {
	return &LibvirtDiagSource{
		domainConn:  domainConn,
		storageConn: storageConn,
	}
}

func (s *LibvirtDiagSource) listDomains(dr *diag.DiagResult) error {
	domains, err := s.domainConn.ListDomains()
	if err != nil {
		return fmt.Errorf("error listing domains: %v", err)
	}
	for _, d := range domains {
		xml, err := d.XML()
		if err != nil {
			return fmt.Errorf("error getting domain xml: %v", err)
		}
		if err := addLibvirtXMLToDiagResult("domain", xml.Name, xml, dr); err != nil {
			return err
		}
	}
	return nil
}

func (s *LibvirtDiagSource) listPoolsAndVolumes(dr *diag.DiagResult) error {
	pools, err := s.storageConn.ListPools()
	if err != nil {
		return fmt.Errorf("error listing pools: %v", err)
	}
	for _, p := range pools {
		xml, err := p.XML()
		if err != nil {
			return fmt.Errorf("error getting domain xml: %v", err)
		}
		if err := addLibvirtXMLToDiagResult("pool", xml.Name, xml, dr); err != nil {
			return err
		}
		if err := s.listVolumes(p, dr); err != nil {
			return err
		}
	}
	return nil
}

func (s *LibvirtDiagSource) listVolumes(p virt.StoragePool, dr *diag.DiagResult) error {
	vols, err := p.ListVolumes()
	if err != nil {
		return fmt.Errorf("error listing volumes: %v", err)
	}
	for _, v := range vols {
		xml, err := v.XML()
		if err != nil {
			return fmt.Errorf("error getting domain xml: %v", err)
		}
		if err := addLibvirtXMLToDiagResult("volume", xml.Name, xml, dr); err != nil {
			return err
		}
	}
	return nil
}

func (s *LibvirtDiagSource) DiagnosticInfo() (diag.DiagResult, error) {
	dr := diag.DiagResult{
		IsDir:    true,
		Children: make(map[string]diag.DiagResult),
	}
	err := s.listDomains(&dr)
	if err == nil {
		err = s.listPoolsAndVolumes(&dr)
	}
	if err != nil {
		return diag.DiagResult{}, err
	}
	return dr, nil
}

func addLibvirtXMLToDiagResult(objType, name string, xml libvirtxml.Document, dr *diag.DiagResult) error {
	out, err := xml.Marshal()
	if err != nil {
		return fmt.Errorf("error marshalling the pool to xml: %v", err)
	}
	fileName := fmt.Sprintf("%s-%s", objType, name)
	dr.Children[fileName] = diag.DiagResult{
		Name: fileName,
		Ext:  "xml",
		Data: out,
	}
	return nil
}
