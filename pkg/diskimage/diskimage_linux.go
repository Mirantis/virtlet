// +build linux

/*
Copyright 2019 Mirantis

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

package diskimage

import (
	"errors"
	"sort"
	"unsafe"
)

/*
#cgo CFLAGS: -DGUESTFS_PRIVATE=1
#cgo pkg-config: libguestfs

#include <stdio.h>
#include <string.h>
#include <libgen.h>
#include <stdlib.h>
#include <linux/limits.h>
#include "guestfs.h"

#define ERROR_MSG_SIZE 1024

typedef struct _g_wrapper {
	guestfs_h *g;
	char error[ERROR_MSG_SIZE];
	char **devs, **parts, **files;
    char *file_content;
    int launched;
} g_wrapper;

typedef struct _g_file {
    const char* path;
    const char *content;
    size_t size;
} g_file;

static void update_error(g_wrapper* w, const char* msg, int use_g_err)
{
	int n, p;
	const char *g_err = 0, *err;
	if (*w->error)
		return;

	if (use_g_err)
		g_err = guestfs_last_error(w->g);
	if (!g_err && !msg)
		return;

	p = strlen(w->error);
	if (p >= ERROR_MSG_SIZE-2)
		return;
	w->error[p++] = '\n';

	w->error[ERROR_MSG_SIZE-1] = 0;
	if (msg && g_err)
		snprintf(w->error+p, ERROR_MSG_SIZE-p, "%s: %s", msg, g_err);
	else if (msg)
		strncpy(w->error+p, msg, ERROR_MSG_SIZE-1-p);
	else if (g_err)
		strncpy(w->error+p, g_err, ERROR_MSG_SIZE-1-p);
}

g_wrapper* g_wrapper_new()
{
	g_wrapper* w = malloc(sizeof(g_wrapper));
	*w->error = 0;
	w->devs = w->parts = w->files = 0;
	w->file_content = 0;
	w->launched = 0;
	return w;
}

const char* g_wrapper_error(g_wrapper* w)
{
	return w->error[0] ? w->error : 0;
}

int g_wrapper_setup(g_wrapper* w, const char* path, int trace)
{
	w->g = guestfs_create_flags(0);
	if (!w->g) {
		update_error(w, "guestfs_create_flags()", 1);
		return -1;
	}

	guestfs_set_trace(w->g, trace);

	if (guestfs_add_drive_opts(w->g, path,
				   GUESTFS_ADD_DRIVE_OPTS_FORMAT, "qcow2",
				   -1)) {
		update_error(w, "guestfs_add_drive_opts()", 1);
		return -1;
	}

	if (guestfs_launch(w->g) < 0) {
		update_error(w, "guestfs_launch()", 1);
		return -1;
	}
	w->launched = 1;

	w->devs = guestfs_list_devices(w->g);
	if (!w->devs) {
		update_error(w, "guestfs_list_devices()", 1);
		return -1;
	}

	if (!w->devs[0] || w->devs[1]) {
		update_error(w, "exactly one device is expected", 0);
		return -1;
	}

	return 0;
}

int g_wrapper_close(g_wrapper* w)
{
	int r = 0;
	if (!w->g) {
		update_error(w, "guestfs handle already closed", 0);
		return -1;
	}

	if (w->devs)
		while (*w->devs) free(*w->devs++);
	if (w->parts)
		while (*w->parts) free(*w->parts++);
	if (w->files)
		while (*w->files) free(*w->files++);

	if (w->launched && guestfs_shutdown(w->g) < 0) {
		update_error(w, "guestfs_shutdown()", 1);
		r = -1;
	}

	guestfs_close(w->g);
	w->g = 0;
	w->devs = w->parts = w->files = 0;
	w->file_content = 0;
	w->launched = 0;
	return r;
}

static int g_wrapper_part_disk(g_wrapper* w)
{
	if (!w->g || !w->devs) {
		update_error(w, "guestfs setup not done", 0);
		return -1;
	}

	if (guestfs_part_disk(w->g, w->devs[0], "mbr") < 0) {
		update_error(w, "guestfs_part_disk()", 1);
		return -1;
	}

	return 0;
}

static int g_wrapper_get_partitions(g_wrapper* w)
{
	if (!w->g || !w->devs) {
		update_error(w, "guestfs setup not done", 0);
		return -1;
	}

	w->parts = guestfs_list_partitions(w->g);
	if (!w->parts) {
		update_error(w, "guestfs_list_partitions()", 1);
		return -1;
	}

	if (!w->parts[0]) {
		update_error(w, "at least one partition is expected", 0);
		return -1;
	}

	return 0;
}

int g_wrapper_format(g_wrapper* w)
{
	if (g_wrapper_part_disk(w) < 0 || g_wrapper_get_partitions(w) < 0)
		return -1;

	if (guestfs_mkfs(w->g, "ext4", w->parts[0]) < 0) {
		update_error(w, "guestfs_mkfs()", 1);
		return -1;
	}

	return 0;
}

int g_wrapper_put(g_wrapper* w, int n, g_file* files)
{
	static char path[PATH_MAX+1];

	if (g_wrapper_get_partitions(w) < 0)
		return -1;

	if (guestfs_mount(w->g, w->parts[0], "/")) {
		update_error(w, "guestfs_mount()", 1);
		return -1;
	}

	while (n--) {
		if (strlen(files->path) > PATH_MAX) {
			update_error(w, "file path too long", 0);
			return -1;
		}

		// dirname may modify the string, so we need to clone path
		strcpy(path, files->path);
		if (guestfs_mkdir_p(w->g, dirname(path)) < 0) {
			update_error(w, "guestfs_mkdir_p()", 1);
			return -1;
		}

		if (guestfs_write(w->g, files->path, files->content, files->size) < 0) {
			update_error(w, "guestfs_write()", 1);
			return -1;
		}

		files++;
	}

	return 0;
}

char** g_wrapper_ls(g_wrapper* w, const char* dir)
{
	if (w->files)
		while (*w->files) free(*w->files++);
	w->files = 0;

	if (g_wrapper_get_partitions(w) < 0)
		return 0;

	if (guestfs_mount(w->g, w->parts[0], "/")) {
		update_error(w, "guestfs_mount()", 1);
		return 0;
	}

	w->files = guestfs_ls(w->g, dir);
	if (!w->files) {
		update_error(w, "guestfs_ls()", 1);
		return 0;
	}

	return w->files;
}

char* g_wrapper_cat(g_wrapper* w, const char* path)
{
	if (w->file_content)
		free(w->file_content);
	w->file_content = 0;

	if (g_wrapper_get_partitions(w) < 0)
		return 0;

	if (guestfs_mount(w->g, w->parts[0], "/")) {
		update_error(w, "guestfs_mount()", 1);
		return 0;
	}

	w->file_content = guestfs_cat(w->g, path);
	if (w->file_content == 0) {
		update_error(w, "guestfs_cat()", 1);
		return 0;
	}

	return w->file_content;
}
*/
import "C"

func handleGuestfsError(w *C.g_wrapper, r int) error {
	if r == 0 {
		return nil
	}

	if errStr := C.g_wrapper_error(w); errStr != nil {
		return errors.New(C.GoString(errStr))
	}

	// this shouldn't happen, but let's be safe here
	return errors.New("unknown libguestfs error")
}

func callWithGWrapper(imagePath string, toCall func(*C.g_wrapper) int) error {
	w, err := C.g_wrapper_new()
	if err != nil {
		return err
	}

	cPath := C.CString(imagePath)
	defer func() {
		C.free(unsafe.Pointer(cPath))
		if w != nil {
			C.g_wrapper_close(w)
		}
	}()

	r := int(C.g_wrapper_setup(w, cPath, C.int(1)))
	if err = handleGuestfsError(w, r); err != nil {
		return err
	}

	if err = handleGuestfsError(w, toCall(w)); err != nil {
		return err
	}

	curW := w
	w = nil
	return handleGuestfsError(curW, int(C.g_wrapper_close(curW)))
}

// FormatDisk partitions the specified image file by writing an MBR with
// a single partition and then formatting that partition as an ext4 filesystem.
func FormatDisk(imagePath string) error {
	return callWithGWrapper(imagePath, func(w *C.g_wrapper) int {
		return int(C.g_wrapper_format(w))
	})
}

// Put writes files to the image, making all the necessary subdirs
func Put(imagePath string, files map[string][]byte) error {
	return callWithGWrapper(imagePath, func(w *C.g_wrapper) int {
		filenames := make([]string, 0, len(files))
		for filename := range files {
			filenames = append(filenames, filename)
		}
		sort.Strings(filenames)
		gFiles := make([]C.g_file, len(filenames))
		for n, filename := range filenames {
			gFiles[n].path = C.CString(filename)
			content := files[filename]
			gFiles[n].content = C.CString(string(content))
			gFiles[n].size = C.size_t(len(content))
		}
		defer func() {
			for _, f := range gFiles {
				C.free(unsafe.Pointer(f.path))
				C.free(unsafe.Pointer(f.content))
			}
		}()
		return int(C.g_wrapper_put(w, C.int(len(gFiles)), (*C.g_file)(unsafe.Pointer(&gFiles[0]))))
	})
}

// ListFiles returns the list of files in the specified directory.
func ListFiles(imagePath, dir string) ([]string, error) {
	var r []string
	if err := callWithGWrapper(imagePath, func(w *C.g_wrapper) int {
		cDir := C.CString(dir)
		defer func() {
			C.free(unsafe.Pointer(cDir))
		}()
		strs := C.g_wrapper_ls(w, cDir)
		if strs == nil {
			return -1
		}
		for *strs != nil {
			r = append(r, C.GoString(*strs))
			strs = (**C.char)(unsafe.Pointer(uintptr(unsafe.Pointer(strs)) + unsafe.Sizeof(*strs)))
		}
		return 0
	}); err != nil {
		return nil, err
	}

	return r, nil
}

// Cat returns the contents of the file as a string.
// NOTE: this function is only suited for text files and doesn't
// handle zero bytes properly.
func Cat(imagePath, filePath string) (string, error) {
	var r string
	if err := callWithGWrapper(imagePath, func(w *C.g_wrapper) int {
		cPath := C.CString(filePath)
		defer func() {
			C.free(unsafe.Pointer(cPath))
		}()
		str := C.g_wrapper_cat(w, cPath)
		if str == nil {
			return -1
		}
		r = C.GoString(str)
		return 0
	}); err != nil {
		return "", err
	}

	return r, nil
}
