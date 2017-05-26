# Environment variables passing to VM

Passing of environment variables set in pod definition, to VM created from this
definition is based on [cloud-init](cloud-init-data-generation.md) support.

## Virtlet aware images

The idea behind passing an environment variables to VM images prepared to run
under Virtlet is based on documented location of file containing these variables.
Using `wtrite_files` option from `user-data` part of cloud-init Virtlet has to
create file (e.g. under location `/etc/cloud/environment`) in standard shell
script format (list of `KEY=value` pairs, one per line) which then can be sourced
by any user shell script.

## Simplified access to environment variables

The common way to set system wide list of environment variables is to add them
to `/etc/environment` - file used by many linux distributions at this time.
Using [script per once](http://cloudinit.readthedocs.io/en/latest/topics/modules.html#scripts-per-once)
feature Virtlet can provide a script, which then will merge existing content
of `/etc/environment` adding the lines from `/etc/cloud/environment` for variables
which are not yet defined in the system environment file.

This could be done by extending above script:
```bash
TMPFILE=/tmp/env_$$
cp /etc/environment ${TMPFILE}
cat /etc/cloud/environment | while read line ; do
  VARIABLE="$(echo $line | sed s/=.*//)"
  if ! egrep -q "^${VARIABLE}=" /etc/environment ; then
    echo "$line" >>${TMPFILE}
  fi
done
mv ${TMPFILE} /etc/environment
```

That way we keep support for original content of system environment file, with
a method to add there variables described in pod definition.

## Forcing overwritting system environment

We may additionally implement a method to pass information, that environment
variables from pod definition should overwrite values already existing in
`/etc/environment`. Assuming that this will be controlled by
annotation `VirtletForcePodEnvironment: true` - merge script should add variables
in reverse order - at start it should copy `/etc/cloud/environment` into
temporary file, when it should add lines from `/etc/environment` not found
in `/etc/cloud/environment`.
