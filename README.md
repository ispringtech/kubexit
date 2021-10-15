# kubexit

Command supervisor for coordinated Kubernetes pod container termination.

**Forked from [kubexit](https://github.com/karlkfi/kubexit)**

## Use Cases

Kubernetes supports multiple containers in a pod, but there is no current feature to manage dependency ordering, so all the containers (other than init containers) start at the same time. This can cause a number of issues with certain configurations, some of which kubexit is designed to mitigate.

1. Kubernetes jobs run until all containers have exited. If a sidecar container is supporting a primary container, the sidecar needs to be gracefully terminated after the primary container has exited, before the job will end. Kubexit mitigates this with death dependencies.
2. Sidecar proxies (e.g. Istio, CloudSQL Proxy) are often designed to handle network traffic to and from a pod's primary container. But if the primary container tries to make egress call or recieve ingress calls before the sidecar proxy is up and ready, those calls may fail. Kubexit mitigates this with birth dependencies.

## Tombstones

kubexit automatically carves (writes to disk) a tombstone (`${KUBEXIT_GRAVEYARD}/${KUBEXIT_NAME}`) to mark the birth and death of the process it supervises:

1. When a wrapped app starts, kubexit will write a tombstone with a `Born` timestamp.
1. When a wrapped app exits, kubexit will update the tombstone with a `Died` timestamp and the `ExitCode`.

These tombstones are written to the graveyard, a folder on the local file system. In Kubernetes, an in-memory volume can be used to share the graveyard between containers in a pod. By watching the file system inodes in the graveyard, kubexit will know when the other containers in the pod start and stop.

Tombstone Content:

```
Born: <timestamp>
Died: <timestamp>
ExitCode: <int>
```

## Birth Dependencies

With kubexit, you can define birth dependencies between processes that are wrapped with kubexit and configured with the same graveyard.

Unlike death dependencies, birth dependencies only work within a Kubernetes pod, because kubexit watches pod container readiness, rather than implementing its own readiness checks.

Kubexit will block the execution of the dependent container process (ex: a stateless webapp) until the dependency container (ex: a sidecar proxy) is ready.

The primary use case for this feature is Kubernetes sidecar proxies, where the proxy needs to come up before the primary container process, otherwise the primary process egress calls will fail unitl the proxy is up.

## Death Dependencies

With kubexit, you can define death dependencies between processes that are wrapped with kubexit and configured with the same graveyard.

If the dependency process (ex: a stateless webapp) exits before the dependent process (ex: a sidecar proxy), kubexit will detect the tombstone update (`Died: <timestamp>`) and send the `TERM` signal to the dependent process.

The primary use case for this feature is Kubernetes Jobs, where a sidecar container needs to be gracefully shutdown when the primary container exits, otherwise the Job will never complete.

## Config

kubexit is configured with environment variables only, to make it easy to configure in Kubernetes and minimize entrypoint/command changes.

Tombstone:
- `KUBEXIT_NAME` - The name of the tombstone file to use. Must match the name of the Kubernetes pod container, if using birth dependency.
- `KUBEXIT_GRAVEYARD` - The file path of the graveyard directory, where tombstones will be read and written.

Death Dependency:
- `KUBEXIT_DEATH_DEPS` - The name(s) of this process death dependencies, comma separated.
- `KUBEXIT_GRACE_PERIOD` - Duration to wait for this process to exit after a graceful termination, before being killed. Default: `30s`.

Birth Dependency:
- `KUBEXIT_BIRTH_DEPS` - The name(s) of this process birth dependencies, comma separated.
- `KUBEXIT_BIRTH_TIMEOUT` - Duration to wait for all birth dependencies to be ready. Default: `30s`.
- `KUBEXIT_POD_NAME` - The name of the Kubernetes pod that this process and all its siblings are in.
- `KUBEXIT_NAMESPACE` - The name of the Kubernetes namespace that this pod is in.

Logging:
- `KUBEXIT_VERBOSE_LEVEL` - Set logger verbose level. If more than 0 all collected logs printed to stdout
- `KUBEXIT_INSTANT_LOGGING` - Makes each event-trace log their events immediately with trace log level. Set to `1` or `true` to enable feature. This is a boolean variable parsed by golang `strconv.ParseBool` 

## Logging

### Initializing

Logging takes place in JSON format. The fact of starting the supervisor with the message that it has been initialized and its initialization config is logged 
```json
{
  "@timestamp": "2021-10-13T15:00:21.708639507+03:00",
  "config": {
    "name": "client",
    "graveyard": "/graveyard",
    "birth_deps": [
      "server-1",
      "server-2"
    ],
    "death_deps": null,
    "birth_timeout": 60000000000,
    "grace_period": 30000000000,
    "pod_name": "client_pod",
    "namespace": "namespace",
    "verbose_level": 1
  },
  "level": "info",
  "message": "kubexit initialized"
}
```

### Info
Logging of the supervisor's work occurs through the so-called event tracing, each supervisor module writes its logs to its event trace in JSON format.

```json
{
  "@timestamp": "2021-10-15T07:44:50.693820393Z",
  "event-traces": [
    {
      "id": "server tombstone",
      "events": [
        {
          "timestamp": "2021-10-15T07:44:37.967685683Z",
          "message": "Creating tombstone: /graveyard/server"
        },
        {
          "timestamp": "2021-10-15T07:44:50.693480158Z",
          "message": "Updating tombstone: /graveyard/server"
        }
      ]
    },
    {
      "id": "supervisor",
      "events": [
        {
          "timestamp": "2021-10-15T07:44:37.964957424Z",
          "message": "Start: /app/bin/server"
        },
        {
          "timestamp": "2021-10-15T07:44:50.190429011Z",
          "message": "Terminating child process"
        },
        {
          "timestamp": "2021-10-15T07:44:50.691591597Z",
          "message": "Received signal: child exited"
        }
      ]
    },
    {
      "id": "death graveyard watcher",
      "events": [
        {
          "timestamp": "2021-10-15T07:44:37.967878254Z",
          "message": "Ignore tombstone server"
        },
        {
          "timestamp": "2021-10-15T07:44:37.96818926Z",
          "message": "Ignore tombstone server"
        },
        {
          "timestamp": "2021-10-15T07:44:49.493992054Z",
          "message": "Reading tombstone: client"
        },
        {
          "timestamp": "2021-10-15T07:44:49.49413779Z",
          "message": "Reading tombstone: client"
        },
        {
          "timestamp": "2021-10-15T07:44:50.189892224Z",
          "message": "Reading tombstone: client"
        },
        {
          "timestamp": "2021-10-15T07:44:50.1901549Z",
          "message": "Reading tombstone: client"
        },
        {
          "timestamp": "2021-10-15T07:44:50.190403278Z",
          "message": "New death: client"
        },
        {
          "timestamp": "2021-10-15T07:44:50.190456246Z",
          "message": "Tombstone Watch(/graveyard): done"
        }
      ]
    }
  ],
  "level": "info",
  "message": "supervising proceed successfully"
}
```

### Error

When error happened supervisor logs all his event-traces ignoring verbose level

```json
{
  "@timestamp": "2021-10-13T15:00:21.710021807+03:00",
  "error": "failed to watch pod: failed to configure kubernetes client: unable to load in-cluster configuration, KUBERNETES_SERVICE_HOST and KUBERNETES_SERVICE_PORT must be defined",
  "event-traces": [
    {
      "id": "client tombstone",
      "events": [
        {
          "timestamp": "2021-10-13T15:00:21.709913409+03:00",
          "message": "Updating tombstone: /graveyard/client"
        }
      ]
    },
    {
      "id": "supervisor",
      "events": []
    },
    {
      "id": "birth dependencies watcher",
      "events": [
        {
          "timestamp": "2021-10-13T15:00:21.709462755+03:00",
          "message": "Watching pod client_pod updates"
        }
      ]
    }
  ],
  "level": "error",
  "message": "",
  "stack": [
    "github.com/ispringtech/kubexit/pkg/kubernetes.WatchPod\n\t/home/microuser/kubexit/pkg/kubernetes/watch.go:29",
    "main.waitForBirthDeps\n\t/home/microuser/kubexit/cmd/kubexit/main.go:157",
    "main.runApp\n\t/home/microuser/kubexit/cmd/kubexit/main.go:111",
    "main.main\n\t/home/microuser/kubexit/cmd/kubexit/main.go:41",
    "runtime.main\n\t/snap/go/8489/src/runtime/proc.go:255",
    "runtime.goexit\n\t/snap/go/8489/src/runtime/asm_amd64.s:1581"
  ]
}
```

## Build

While kubexit can easily be installed on your local machine, the primary use cases require execution within Kubernetes pod containers. So the recommended method of installation is to either side-load kubexit using a shared volume and an init container, or build kubexit into your own container images.

Build from source:

```
go get github.com/ispringtech/kubexit/cmd/kubexit
```

Build docker image with kubexit

```shell
docker build . --tag=kubexit
```
Copy from init container to ephemeral volume:

```yaml
volumes:
- name: kubexit
  emptyDir: {}

initContainers:
- name: kubexit
  image: kubexit:latest
  command: ['cp', '/app/bin/kubexit', '/kubexit/kubexit']
  volumeMounts:
  - mountPath: /kubexit
    name: kubexit
```

## Examples

- [Client Server Job](examples/client-server-job/)
- [CloudSQL Proxy Job](examples/cloudsql-proxy-job/)
