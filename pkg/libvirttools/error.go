/*
Copyright 2016 Mirantis

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

/*
#include <libvirt/libvirt.h>
#include <libvirt/virterror.h>
#include <stdlib.h>

#include "error.h"
#include "image.h"
#include "virtualization.h"
*/
import "C"

import (
	"fmt"
	"syscall"
)

var (
	cErrorHandler = NewCErrorHandler()
)

func NewErrorMap(libvirtErrorHandler func() error) map[C.int]func() error {
	return map[C.int]func() error{
		C.VIRTLET_OK: func() error {
			return nil
		},

		C.VIRTLET_IMAGE_ERR_SEND_STREAM: func() error {
			return fmt.Errorf("Failed to send/save image stream (VIRTLET_IMAGE_ERR_SEND_STREAM)")
		},
		// We are ignoring already existing images
		C.VIRTLET_IMAGE_ERR_ALREADY_EXISTS: func() error {
			return nil
		},
		C.VIRTLET_IMAGE_ERR_LIBVIRT: func() error {
			return libvirtErrorHandler()
		},

		C.VIRTLET_VIRTUALIZATION_ERR_LIBVIRT: func() error {
			return libvirtErrorHandler()
		},
	}
}

func GetLibvirtLastError() error {
	err := C.virGetLastError()
	if err == nil {
		return nil
	}
	newErr := fmt.Errorf(C.GoString(err.message))
	C.virResetError(err)
	return newErr
}

type CErrorHandler struct {
	errorMap map[C.int]func() error
}

func NewCErrorHandler() *CErrorHandler {
	errors := NewErrorMap(GetLibvirtLastError)
	return &CErrorHandler{errorMap: errors}
}

func (h *CErrorHandler) Convert(cError C.int) error {
	// We assume that errno under 1000 belongs to the kernel.
	if cError > 1 && cError < 1000 {
		return syscall.Errno(cError)
	}

	f, ok := h.errorMap[cError]
	if !ok {
		return fmt.Errorf("Unknown error")
	}

	return f()
}

func (h *CErrorHandler) ConvertGoInt(iError int) error {
	return h.Convert(C.int(iError))
}
