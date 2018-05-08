## **K8s SyncPod workflow for remote runtime:**

**I. Check Sandbox Changes**

1. Check SandBox statuses (SANDBOX_READY) => If yes, Sandbox container is re-created.
2. Check whether hostNetwork setting has been changed. => If yes, Sandbox container is killed&re-created.
**NOTE:** hostNetwork setting is ignored by virtlet, netns will be created always for new PodSandBox. Thus, hostNetwork setting change is causing killing VM with furhter re-creation of the same network and domain (corresponding issue: https://github.com/Mirantis/virtlet/issues/184).

**II. Process containers**

Container will be killed&re-created&started if:

1. Container status is not RUNNING and RestartPolicy != RestartPolicyNever or (RestartPolicy == RestartOnFailure and container exit code != 0)
2. SandBox has been changed (look at I. above for details) and RestartPolicy != RestartPolicyNever
3. Compares the hash of old and new container's spec. NOTE: Allowed changes in Spec for now only - containers[*].image 
4. if Liveness probe is set and has failure status and  RestartPolicy != RestartPolicyNever
