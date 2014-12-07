## GuestBook example

This example shows how to build a simple multi-tier web application using Kubernetes and Docker.
The example combines a web frontend, a redis master for storage and a replicated set of redis slaves.

### Step Zero: Prerequisites

This example assumes that you have forked the repository and [turned up a Kubernetes-Mesos cluster](https://github.com/mesosphere/kubernetes-mesos#build).
It also assumes that `${KUBERNETES_MASTER}` is the HTTP URI of the host running the Kubernetes-Mesos framework, which currently hosts the Kubernetes API server (e.g. http://10.2.3.4:8888).

### Step One: Turn up the redis master.

Create a file named `redis-master.json` describing a single pod, which runs a redis key-value server in a container.

```javascript
{
  "id": "redis-master-2",
  "kind": "Pod",
  "apiVersion": "v1beta1",
  "desiredState": {
    "manifest": {
      "version": "v1beta1",
      "id": "redis-master-2",
      "containers": [{
        "name": "master",
        "image": "dockerfile/redis",
        "ports": [{
          "containerPort": 6379,
          "hostPort": 31010
        }]
      }]
    }
  },
  "labels": {
    "name": "redis-master"
  }
}
```

Once you have that pod file, you can create the redis pod in your Kubernetes cluster using `kubecfg` or the REST API:

```shell
$ bin/kubecfg -c examples/guestbook/redis-master.json create pods
# -- or --
$ curl ${KUBERNETES_MASTER}/api/v1beta1/pods -XPOST -d@examples/guestbook/redis-master.json
```

Once that's up you can list the pods in the cluster, to verify that the master is running:

```shell
$ bin/kubecfg list pods
# -- or --
$ curl ${KUBERNETES_MASTER}/api/v1beta1/pods
```

You'll see a single redis master pod. It will also display the machine that the pod is running on.

```
ID                  Image(s)            Host                            Labels              Status
----------          ----------          ----------                      ----------          ----------
redis-master-2      dockerfile/redis    10.132.189.243/10.132.189.243   name=redis-master   Waiting
```

If you ssh to that machine, you can run `docker ps` to see the actual pod:

```shell
$ vagrant ssh mesos-2

vagrant@mesos-2:~$ sudo docker ps
CONTAINER ID  IMAGE                    COMMAND               CREATED         STATUS         PORTS                     NAMES
e1f02d73117d  dockerfile/redis:latest  "redis-server /etc/r  24 seconds ago  Up 24 seconds                            k8s--master.bd418b17--redis_-_master_-_2.mesos--abc826dc_-_6692_-_11e4_-_bd1f_-_04012f416701--f0c5341e
487bf8a581e9  kubernetes/pause:go      "/pause"              6 minutes ago   Up 6 minutes   0.0.0.0:31010->6379/tcp   k8s--net.c5d67d7a--redis_-_master_-_2.mesos--abc826dc_-_6692_-_11e4_-_bd1f_-_04012f416701--9acb0442
```

(Note that initial `docker pull` may take a few minutes, depending on network conditions.)

### Step Two: Turn up the master service.
A Kubernetes 'service' is a named load balancer that proxies traffic to one or more containers.
The services in a Kubernetes cluster are discoverable inside other containers via environment variables.
Services find the containers to load balance based on pod labels.

The pod that you created in Step One has the label `name=redis-master`.
The selector field of the service determines which pods will receive the traffic sent to the service.
Create a file named `redis-master-service.json` that contains:

```js
{
  "id": "redismaster",
  "kind": "Service",
  "apiVersion": "v1beta1",
  "port": 10000,
  "selector": {
    "name": "redis-master"
  }
}
```

This will cause all pods to see the redis master apparently running on `localhost:10000`.

Once you have that service description, you can create the service with the REST API:

```shell
$ bin/kubecfg -c examples/guestbook/redis-master-service.json create services
# -- or --
$ curl ${KUBERNETES_MASTER}/api/v1beta1/services -XPOST -d@examples/guestbook/redis-master-service.json
```

Observe that the service has been created.
```
ID                  Labels              Selector            Port
----------          ----------          ----------          ----------
redismaster                             name=redis-master   10000
```

Once created, the service proxy on each minion is configured to set up a proxy on the specified port (in this case port 10000).

### Step Three: Turn up the replicated slave pods.
Although the redis master is a single pod, the redis read slaves are a 'replicated' pod.
In Kubernetes a replication controller is responsible for managing multiple instances of a replicated pod.

Create a file named `redis-slave-controller.json` that contains:

```js
{
  "id": "redisSlaveController",
  "kind": "ReplicationController",
  "apiVersion": "v1beta1",
  "desiredState": {
    "replicas": 2,
    "replicaSelector": {"name": "redisslave"},
    "podTemplate": {
      "desiredState": {
         "manifest": {
           "version": "v1beta1",
           "id": "redisSlaveController",
           "containers": [{
             "name": "slave",
             "image": "jdef/redis-slave",
             "ports": [{"containerPort": 6379, "hostPort": 31020}]
           }]
         }
       },
       "labels": {"name": "redisslave"}
      }},
  "labels": {"name": "redisslave"}
}
```

Then you can create the service by running:

```shell
$ bin/kubecfg -c examples/guestbook/redis-slave-controller.json create replicationControllers
# -- or --
$ curl ${KUBERNETES_MASTER}/api/v1beta1/replicationControllers -XPOST -d@examples/guestbook/redis-slave-controller.json
```
```
Name                   Image(s)                   Selector            Replicas
----------             ----------                 ----------          ----------
redisSlaveController   jdef/redis-slave           name=redisslave     2
```

The redis slave configures itself by looking for the Kubernetes service environment variables in the container environment.
In particular, the redis slave is started with the following command:

```shell
redis-server --slaveof $SERVICE_HOST $REDISMASTER_SERVICE_PORT
```

Once that's up you can list the pods in the cluster, to verify that the master and slaves are running:

```shell
$ bin/kubecfg list pods
ID                                     Image(s)            Host                            Labels                                                       Status
----------                             ----------          ----------                      ----------                                                   ----------
redis-master-2                         dockerfile/redis    10.132.189.243/10.132.189.243   name=redis-master                                            Running
439c1c7c-6694-11e4-bd1f-04012f416701   jdef/redis-slave    10.132.189.242/10.132.189.242   name=redisslave,replicationController=redisSlaveController   Running
439b7b2f-6694-11e4-bd1f-04012f416701   jdef/redis-slave    10.132.189.243/10.132.189.243   name=redisslave,replicationController=redisSlaveController   Running
```

You will see a single redis master pod and two redis slave pods.

### Step Four: Create the redis slave service.

Just like the master, we want to have a service to proxy connections to the read slaves.
In this case, in addition to discovery, the slave service provides transparent load balancing to clients.
As before, create a service specification:

```js
{
  "id": "redisslave",
  "kind": "Service",
  "apiVersion": "v1beta1",
  "port": 10001,
  "labels": {
    "name": "redisslave"
  },
  "selector": {
    "name": "redisslave"
  }
}
```

This time the selector for the service is `name=redisslave`, because that identifies the pods running redis slaves.
It may also be helpful to set labels on your service itself--as we've done here--to make it easy to locate them with:

* `bin/kubecfg -l "name=redisslave" list services`, or
* `curl ${KUBERNETES_MASTER}/api/v1beta1/services?labels=name=redisslave`

Now that you have created the service specification, create it in your cluster via the REST API:

```shell
$ bin/kubecfg -c examples/guestbook/redis-slave-service.json create services
# -- or --
$ curl ${KUBERNETES_MASTER}/api/v1beta1/services -XPOST -d@examples/guestbook/redis-slave-service.json
ID                  Labels              Selector            Port
----------          ----------          ----------          ----------
redisslave          name=redisslave     name=redisslave     10001
```

### Step Five: Create the frontend pod.

This is a simple PHP server that is configured to talk to either the slave or master services depending on whether the request is a read or a write.
It exposes a simple AJAX interface and serves an angular-based UX.
Like the redis read slaves it is a replicated service instantiated by a replication controller.

Create a file named `frontend-controller.json`:

```js
{
  "id": "frontendController",
  "kind": "ReplicationController",
  "apiVersion": "v1beta1",
  "desiredState": {
    "replicas": 3,
    "replicaSelector": {"name": "frontend"},
    "podTemplate": {
      "desiredState": {
         "manifest": {
           "version": "v1beta1",
           "id": "frontendController",
           "containers": [{
             "name": "php-redis",
             "image": "jdef/php-redis",
             "ports": [{"containerPort": 80, "hostPort": 31030}]
           }]
         }
       },
       "labels": {"name": "frontend"}
      }},
  "labels": {"name": "frontend"}
}
```

With this file, you can turn up your frontend with:

```shell
$ bin/kubecfg -c examples/guestbook/frontend-controller.json create replicationControllers
# -- or --
$ curl ${KUBERNETES_MASTER}/api/v1beta1/replicationControllers -XPOST -d@examples/guestbook/frontend-controller.json
ID                   Image(s)                 Selector            Replicas
----------           ----------               ----------          ----------
frontendController   jdef/php-redis           name=frontend       3
```

Once that's up you can list the pods in the cluster, to verify that the master, slaves and frontends are running:

```shell
$ bin/kubecfg list pods
ID                                     Image(s)                 Host                            Labels                                                       Status
----------                             ----------               ----------                      ----------                                                   ----------
redis-master-2                         dockerfile/redis         10.132.189.243/10.132.189.243   name=redis-master                                            Running
439c1c7c-6694-11e4-bd1f-04012f416701   jdef/redis-slave         10.132.189.242/10.132.189.242   name=redisslave,replicationController=redisSlaveController   Running
439b7b2f-6694-11e4-bd1f-04012f416701   jdef/redis-slave         10.132.189.243/10.132.189.243   name=redisslave,replicationController=redisSlaveController   Running
901eb1c1-6695-11e4-bd1f-04012f416701   jdef/php-redis           10.132.189.243/10.132.189.243   name=frontend,replicationController=frontendController       Running
901edf34-6695-11e4-bd1f-04012f416701   jdef/php-redis           10.132.189.240/10.132.189.240   name=frontend,replicationController=frontendController       Running
901e29a7-6695-11e4-bd1f-04012f416701   jdef/php-redis           10.132.189.242/10.132.189.242   name=frontend,replicationController=frontendController       Running
```

You will see a single redis master pod, two redis slaves, and three frontend pods.

The code for the PHP service looks like this:

```php
<?
set_include_path('.:/usr/share/php:/usr/share/pear:/vendor/predis');

error_reporting(E_ALL);
ini_set('display_errors', 1);

require 'predis/autoload.php';

if (isset($_GET['cmd']) === true) {
  header('Content-Type: application/json');
  if ($_GET['cmd'] == 'set') {
    $client = new Predis\Client([
      'scheme' => 'tcp',
      'host'   => getenv('SERVICE_HOST'),
      'port'   => getenv('REDISMASTER_SERVICE_PORT'),
    ]);
    $client->set($_GET['key'], $_GET['value']);
    print('{"message": "Updated"}');
  } else {
    $read_port = getenv('REDISMASTER_SERVICE_PORT');

    if (isset($_ENV['REDISSLAVE_SERVICE_PORT'])) {
      $read_port = getenv('REDISSLAVE_SERVICE_PORT');
    }
    $client = new Predis\Client([
      'scheme' => 'tcp',
      'host'   => getenv('SERVICE_HOST'),
      'port'   => $read_port,
    ]);

    $value = $client->get($_GET['key']);
    print('{"data": "' . $value . '"}');
  }
} else {
  phpinfo();
} ?>
```

To play with the service itself, find the IP address of a Mesos slave that is running a frontend pod and visit `http://<host-ip>:31030`.
```shell
# You'll actually want to interact with this app via a browser but you can test its access using curl:
$ curl http://10.132.189.243:31030
<html ng-app="redis">
  <head>
    <title>Guestbook</title>
...
```

For a list of Mesos tasks associated with the Kubernetes pods, you can use the Mesos CLI interface:

```shell
$ mesos-ps
   TIME   STATE    RSS     CPU    %MEM  COMMAND  USER                   ID
 0:00:17    R    28.16 MB  0.75  44.01    none   root  901eeaba-6695-11e4-bd1f-04012f416701
 0:00:55    R    28.02 MB  0.25  43.78    none   root  901ef7c3-6695-11e4-bd1f-04012f416701
 0:01:11    R    28.36 MB  0.5   44.31    none   root  901e3ee5-6695-11e4-bd1f-04012f416701
 0:01:11    R    28.36 MB  0.5   44.31    none   root  439c21f0-6694-11e4-bd1f-04012f416701
 0:00:17    R    28.16 MB  0.75  44.01    none   root  439b8138-6694-11e4-bd1f-04012f416701
 0:00:17    R    28.16 MB  0.75  44.01    none   root  abc8399f-6692-11e4-bd1f-04012f416701
```

Or for more details, you can use the Mesos REST API (assuming that the Mesos master is running on `$servicehost`):

```shell
$ curl http://${servicehost}:5050/master/state.json
{
    "activated_slaves":3,
...
            "tasks": [
                {
                    "executor_id": "KubeleteExecutorID",
                    "framework_id": "20141104-145004-1698212032-5050-7-0000",
                    "id": "77b1193b-6432-11e4-a2bb-080027a5dbff",
                    "name": "PodTask",
                    "resources": {
                        "cpus": 0.25,
                        "disk": 0,
                        "mem": 64,
                        "ports": "[31030-31030]"
                    },
                    "slave_id": "20141104-145004-1698212032-5050-7-0",
                    "state": "TASK_RUNNING",
                    "statuses": [
                        {
                            "state": "TASK_RUNNING",
                            "timestamp": 1415112883.48378
                        }
                    ]
                },
...
    "slaves": [
        {
            "attributes": {},
            "hostname": "192.168.56.101",
            "id": "20141104-145004-1698212032-5050-7-0",
            "pid": "slave(1)@192.168.56.101:5051",
            "registered_time": 1415112634.18967,
            "resources": {
                "cpus": 1,
                "disk": 20986,
                "mem": 920,
                "ports": "[31000-32000]"
            }
        },
...
```
