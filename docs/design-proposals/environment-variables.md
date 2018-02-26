# Passing environment variables to VM

Virtlet uses [cloud-init](../cloud-init-data-generation.md) mechanism to pass
the environment variables from pod definition to the VM.

## Virtlet-aware images

Virtlet uses `write_files` cloud-init module (`write_files` key in `user_data`)
to write the pod-defined environment variables into `/etc/cloud/environment`
using the same `KEY=value` format as used by `/etc/environment`. If the image
was prepared specifically for Virtlet it can then utilize this file to load
these values.

## Simplified access to environment variables

Most Linux distributions use /etc/environment file to store the system-wide
list of environment variables.
Using [bootcmd](http://cloudinit.readthedocs.io/en/latest/topics/modules.html#bootcmd)
feature Virtlet can provide a script, which then will merge the content
of `/etc/environment` adding new lines from `/etc/cloud/environment` and setting
existing variables to values from the last file.

This could be done by extending the following script:
```bash
TMPFILE=/tmp/env_$$
cp /etc/cloud/environment ${TMPFILE}
cat /etc/environment | while read line ; do
  VARIABLE="$(echo $line | sed s/=.*//)"
  if ! egrep -q "^${VARIABLE}=" /etc/cloud/environment ; then
    echo "$line" >>${TMPFILE}
  fi
done
mv ${TMPFILE} /etc/environment
```

This way one can combine the content of `/etc/environment` and
`/etc/cloud/environment` with values from the latter taking precedence.

## Forcing keeping original system environment values

We may additionally implement a method to pass information, that environment
values for variables from `/etc/environment` should be kept even when there
are new values for them in pod definition. Assuming that this will be controlled
with annotation `VirtletKeepVMEnvVars: true` - merge script should add variables
in reverse order - at start it should copy `/etc/environment` into temporary
file, then it should add lines from `/etc/cloud/environment` not found
in `/etc/environment`.
