AC_INIT([virtlet], [0.1])
AM_INIT_AUTOMAKE([-Wall -Werror foreign subdir-objects])

AC_PROG_CC
AC_CHECK_PROG([GO], [go], [go])

PKG_CHECK_MODULES([glib], [glib-2.0])
PKG_CHECK_MODULES([libvirt], [libvirt])

CFLAGS="$CFLAGS $glib_CFLAGS $libvirt_CFLAGS"
LDFLAGS="$LDFLAGS $glib_LIBS $libvirt_LIBS"

AC_CONFIG_FILES([Makefile pkg/download/Makefile pkg/etcdtools/Makefile pkg/libvirttools/Makefile pkg/manager/Makefile])

AC_OUTPUT
