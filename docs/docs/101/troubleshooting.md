## Troubleshooting

When having problems with deploying Virtual Machine the firs step is to [troubleshoot](https://kubernetes.io/docs/tasks/debug-application-cluster/debug-application/) it as normal Pods.
If it doesn't help then you need to check Virtlet logs. You can do it manually or by using `virtletctl diag` command:

```bash
mkdir dump_data
virtletctl diag dump dump_data/
```

It will download all required logs and statuses from all nodes where Virtlet is installed:

```bash
ls dump_data/nodes
```

For more info, see [Diagnostics](../../reference/diagnostics/) in Virtlet Reference.
