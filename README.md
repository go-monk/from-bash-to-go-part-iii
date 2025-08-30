This is the third part of a series introducing Bash programmers to Go. This part starts showing how to build platforms in Go. See the first part for the language building blocks and the second part for introduction to CLI tools programming.

Platform is a word that means different things to different people. What I mean by platform here is an internally built self-service API (possibly with a CLI tool and/or a web interface) that can be used by autonomous (application) teams.

A platform requires platform engineering - designing, building and operating a platform. A wiki page is not a platform since there's no engineering. "The cloud" (like AWS) is not a platform either because it's an overwhelming array of offerings too big to be seen as a platform that can be used by a team. The main goal of a platform is to reduce the overall system complexity in order to deliver leverage to business. In other words, the platform should be easy to use and compelling thus making application developers more productive. In the real world this also necessitates the difficult task of taking customer-centric approach (i.e. talking to people :-) when deciding on the platform features.

# Easypod API server

To make this more concrete let's start building a sample (and a bit contrived) platform. Easypod is a simple API server wrapping an existing Kubernetes cluster (created by [kind](https://kind.sigs.k8s.io/) or [minikube](https://minikube.sigs.k8s.io) for example) and exposing only the following functionality via HTTP methods and URL paths:

* `POST /pod` - create a new pod (i.e. a running containerized application)
* `GET /pods` - list existing pods
* `DELETE /pod/{name}` - delete a pod

```
                    +------------------------+      +---------------------------------------------------+
 Client (curl) ---> |      Easypod API       | ---> |              Kubernetes Cluster API               |
                    |------------------------|      |---------------------------------------------------|
                    | - Pods                 |      | - Pods           - Deployments      - ReplicaSets |
                    | (create, list, delete) |      | - Services       - StatefulSets     - DaemonSets  |
                    +------------------------+      | - Jobs           - CronJobs         - ConfigMaps  |
                                                    |   ...and many more resources and operations...    |
                                                    +---------------------------------------------------+
```

We'll try to conquer this mountain by starting at the top. This is called top-down design. We know we need to handle three URL paths (`/pod`, `/pods` and `/pod/{name}`). And we want to allow only a specific method for each path (POST, GET and DELETE). Using `http.HandleFunc` we map each `METHOD PATH` combination to a function. And we start the HTTP server at port 8080:

```go
// easypod/1/cmd/api/main.go
http.HandleFunc("POST /pod", addPodHandler)
http.HandleFunc("GET /pods", getPodsHandler)
http.HandleFunc("DELETE /pod/{name}", deletePodHandler)

log.Fatal(http.ListenAndServe(":8080", nil))
```

To learn more about HTTP servers you can have a look at https://github.com/go-monk/http-servers.

The functions handling the incoming requests (`addPodHandler`, `getPodsHandler` and `deletePodHandler`) are called handlers and need to take `http.ResponseWriter` and `*http.Request` as parameters. Let's think about the first one called `addPodHandler`. As the name suggests it should add a pod to the cluster. What would be the function's body? At high level, we need to:

1. Extract information about the pod from the request.
2. Create the pod in the cluster.
3. Send error or success response back.

As for the first step, we'll also need to store the information about the pod somewhere. And in the second step we'll have to talk to the cluster's [API](https://kubernetes.io/docs/concepts/overview/kubernetes-api/). Considering these two steps, and thinking a bit forward about the other two handlers, it looks like a good idea to create a package called `cluster` that will hold the `Pod` data type and `CreatePod` function. With this in mind (and maybe even on a "paper") let's try to write the handler function:

```go
// easypod/1/cmd/api/main.go
func addPodHandler(w http.ResponseWriter, r *http.Request) {
	// Extract pod information from the request body.
	var pod cluster.Pod
	if err := json.NewDecoder(r.Body).Decode(&pod); err != nil {
		http.Error(w, "Invalid JSON: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields.
	if pod.Name == "" {
		http.Error(w, "name is required", http.StatusBadRequest)
		return
	}
	if pod.Image == "" {
		http.Error(w, "image is required", http.StatusBadRequest)
		return
	}

	// Create the pod.
	if err := cluster.CreatePod(pod.Name, pod.Image); err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}

	// Send a success response.
	w.WriteHeader(http.StatusCreated)
}
```

We've used the `cluster.Pod` type and the `cluster.CreatePod` function but they don't exist yet. Let's create the type first:

```go
// easypod/1/cluster/cluster.go
type Pod struct {
	Name  string `json:"name"`
	Image string `json:"image"`
}
```

The text in the backticks is called struct tags and the strings `name` and `image` give names to the JSON fields once we encode the data as JSON.

Now let's think about the function. Obviously, we'll need to talk to a Kubernetes cluster. But how? Well, Kubernetes itself is written in Go (so is the `kubectl` CLI tool). For this reason we might suspect there's a Go SDK for Kubernetes. And indeed there is. Kubernetes is a large and complex piece of software (that incarnates the wisdom of numerous sysadmins) and this is reflected also in the SDK. For one thing we (or the IDE) need to import several packages:

```go
// easypod/1/cluster/cluster.go
corev1 "k8s.io/api/core/v1"
metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
"k8s.io/client-go/kubernetes"
"k8s.io/client-go/rest"
"k8s.io/client-go/tools/clientcmd"
```

Next we get the cluster configuration so we can talk to it. Because the easypod API itself could be running inside a Kubernetes cluster we try in-cluster config first and fallback to out-of-cluster config:

```go
// easypod/1/cluster/cluster.go
func getKubeConfig() (*rest.Config, error) {
	// Try in-cluster config first.
	config, err := rest.InClusterConfig()
	if err == nil {
		return config, nil
	}

	// If in-cluster config fails, try out-of-cluster config.
	kubeconfigPath := os.Getenv("KUBECONFIG")
	if kubeconfigPath == "" {
		// Use default kubeconfig path
		homeDir, err := os.UserHomeDir()
		if err != nil {
			return nil, fmt.Errorf("failed to get user home directory: %w", err)
		}
		kubeconfigPath = filepath.Join(homeDir, ".kube", "config")
	}

	config, err = clientcmd.BuildConfigFromFlags("", kubeconfigPath)
	if err != nil {
		return nil, fmt.Errorf("failed to build config from kubeconfig: %w", err)
	}

	return config, nil
}
```

Now we are ready to create a pod:

```go
func CreatePod(name, image string) error {
	config, err := getKubeConfig()
	if err != nil {
		return fmt.Errorf("failed to get kubernetes config: %w", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		return fmt.Errorf("failed to create clientset: %w", err)
	}

	pod := &corev1.Pod{
		ObjectMeta: metav1.ObjectMeta{
			Name: name,
		},
		Spec: corev1.PodSpec{
			Containers: []corev1.Container{
				{
					Name:  name,
					Image: image,
				},
			},
		},
	}

	_, err = clientset.CoreV1().Pods(namespace).Create(context.Background(), pod, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create pod: %w", err)
	}

	return nil
}
```

Ok, so what's this `clientset` thingy? Why a set not just a client? Kubernetes API being large and modular is split into multiple API groups (e.g. core, apps, batch) each managing different kind of resources (e.g. Pod, Deployment, Job). To see the API groups and the kind of resources they manage run `kubectl api-resources` and check out the APIVERSION and KIND columns (`core` API group is implicit, not shown it in the APIVERSION column). So instead of creating separate clients for each resource, you use a single clientset to access all supported resources like this:

```go
clientset, _ := kubernetes.NewForConfig(config)
podsClient := clientset.CoreV1().Pods(namespace)
deploymentsClient := clientset.AppsV1().Deployments(namespace)
```

We use `corev1.Pod` type to define a pod and filling in the minimum necessary information: pod name, container name and an image to create the container from. If you've worked with Kubernetes manifests before, the fields of the `corev1.Pod` struct will sound familiar. Note that for simplicity we create all pods in the default namespace and we allow only for single-container pods.

We use the same approach for the other two handler functions and the related `cluster` functions. Just have a look at the code in the `easypod` folder.

Now let's test our code. We start our API server:

```sh
$ cd easypod/1
$ go run ./cmd/api
```

And in second terminal we try to use the API server:

```sh
$ curl localhost:8080/pod --json '{ "name": "my-pod", "image": "nginx" }'
$ curl localhost:8080/pods
[
    {
        "name": "my-pod",
        "image": "nginx"
    }
]
$ curl localhost:8080/pod/my-pod -X DELETE
```

Cool, our pod got successfully created, listed and deleted!

We've reduced the complexity significantly for the end-user because the API or the user interface of the easypod is much simpler to understand and use. We have abstracted (hidden) the full complexity of the Kubernetes API and exposed only the functionality supposedly needed by the application teams. Of course, in practice we would need to spend some time and energy really talking to the teams that are to use our platform and tease out the real requirements from them.

Also this code is just a proof of concept or a demo and would need more work (like authentication, authorization, CLI tool and/or web UI, logging and observability) to get production ready.

In conclusion, this platform idea is by no means tied to a Kubernetes cluster. It was just an example. It can be applied to a cloud provider, physical server(s) or just any complex system.
