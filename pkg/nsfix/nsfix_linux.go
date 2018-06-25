// +build linux

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

package nsfix

// Here we use cgo constructor trick to avoid threading-related problems
// (not being able to enter the mount namespace)
// when working with process uids/gids and namespaces
// https://github.com/golang/go/issues/8676#issuecomment-66098496

/*
#define _GNU_SOURCE

#include <stdlib.h>
#include <stdio.h>
#include <fcntl.h>
#include <sched.h>
#include <unistd.h>
#include <sys/mount.h>
#include <sys/types.h>
#include <sys/stat.h>
#include <linux/limits.h>

static void nsfix_perr(const char* msg) {
	perror(msg);
	exit(1);
}

static void nsfix_setns(int my_pid, int target_pid, int nstype, const char* nsname) {
	int my_ns_inode, fd;
        struct stat st;
	char my_ns_path[PATH_MAX], target_ns_path[PATH_MAX];
	snprintf(my_ns_path, sizeof(my_ns_path), "/proc/%u/ns/%s", my_pid, nsname);
	snprintf(target_ns_path, sizeof(target_ns_path), "/proc/%u/ns/%s", target_pid, nsname);
	if (stat(my_ns_path, &st) < 0) {
		nsfix_perr("stat() my ns");
	}
	my_ns_inode = st.st_ino;
	if (stat(target_ns_path, &st) < 0) {
		nsfix_perr("stat() target ns");
	}

	// Check if that's the same namespace
	// (actually only critical for CLONE_NEWUSER)
	if (my_ns_inode == st.st_ino)
		return;

	if ((fd = open(target_ns_path, O_RDONLY)) < 0) {
		nsfix_perr("open() target ns");
	}

	if (setns(fd, nstype) < 0) {
		nsfix_perr("setns()");
	}
}

// This function is a high-priority constructor that will be invoked
// before any Go code starts.
__attribute__((constructor (200))) void nsfix_handle_reexec(void) {
	int my_pid, target_pid, target_uid, target_gid;
	char* pid_str;
	if ((pid_str = getenv("NSFIX_NS_PID")) == NULL)
		return;

	my_pid = getpid();
        target_pid = atoi(pid_str);

	// Other namespaces:
        // cgroup, user - not touching
        // pid - host pid namespace is used by virtlet
        // net - host network is used by virtlet
	fprintf(stderr, "nsfix reexec: pid %d: entering the namespaces of target pid %d\n", getpid(), target_pid);
	nsfix_setns(my_pid, target_pid, CLONE_NEWNS, "mnt");
	nsfix_setns(my_pid, target_pid, CLONE_NEWUTS, "uts");
	nsfix_setns(my_pid, target_pid, CLONE_NEWIPC, "ipc");
	nsfix_setns(my_pid, target_pid, CLONE_NEWNET, "net");

	if (getenv("NSFIX_REMOUNT_SYS") != NULL) {
		// remount /sys for the new netns
		if (umount2("/sys", MNT_DETACH) < 0)
			nsfix_perr("umount2()");
		if (mount("none", "/sys", "sysfs", 0, NULL) < 0)
			nsfix_perr("mount()");
	}

	// Permanently drop privs if asked to do so
	if (getenv("NSFIX_DROP_PRIVS") != NULL) {
		fprintf(stderr, "nsfix reexec: dropping privs\n");
		target_gid = getgid();
		if (setgid(target_gid ? target_gid : 65534) < 0)
			nsfix_perr("setgid()");
		target_uid = getuid();
		if (setuid(target_uid ? target_uid : 65534) < 0)
			nsfix_perr("setuid()");
	} else {
		if (setgid(0) < 0)
			nsfix_perr("setgid()");
		if (setuid(0) < 0)
			nsfix_perr("setuid()");
	}
}
*/
import "C"
