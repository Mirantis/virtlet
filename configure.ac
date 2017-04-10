AC_INIT([virtlet], [0.1])
AM_INIT_AUTOMAKE([-Wall -Werror foreign subdir-objects])

AC_PROG_CC
AC_CHECK_PROG([GO], [go], [go])

PKG_CHECK_MODULES([glib], [glib-2.0])
PKG_CHECK_MODULES([libvirt], [libvirt])
PKG_CHECK_MODULES([libguestfs], [libguestfs])

CFLAGS="$CFLAGS $glib_CFLAGS $libvirt_CFLAGS"
LDFLAGS="$LDFLAGS"
LIBS="$LIBS $glib_LIBS $libvirt_LIBS"

AC_CONFIG_FILES([Makefile pkg/bolttools/Makefile pkg/libvirttools/Makefile pkg/manager/Makefile pkg/nettools/Makefile pkg/criproxy/Makefile tests/integration/Makefile tests/network/Makefile])

AC_OUTPUT
