package main

import (
	"context"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/jedib0t/go-pretty/table"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type identifiedWorkload struct {
	containerName, namespace, image, pod string
}

// Channel buffer size
const maxChannelItems = 2000

func main() {

	var listOfNamespaces []string
	var listOfWorkloads []identifiedWorkload

	// Retrieve responses from threaded calls
	messages := make(chan identifiedWorkload, maxChannelItems)

	// Put all threads in a waitgroup so the channel can be closed once all threads have finished
	var wg sync.WaitGroup

	//Grab the kubeconfig
	kubeconfig := getConfig()

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	// Increase the Burst and QOS values
	config.Burst = 50
	config.QPS = 25

	// Build client from  config
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	// Grab the list of namespaces in the current context
	listOfNamespaces = getNamespaces(clientset)

	// Match the number of waitgroups to the number of namespaces.
	// as each call to getPodsPerNamespace() will be in its own goroutine
	wg.Add(len(listOfNamespaces))

	//Iterate through the namespaces
	for _, namespace := range listOfNamespaces {
		//For each namespace, inspect the pods that reside it within a dedicated goroutine
		go func(n string) {
			getPodsPerNamespace(n, clientset, messages)
			defer wg.Done()
		}(namespace)
	}
	// Wait for all goroutines to finish
	wg.Wait()

	// As this uses a buffered channel, we need to explicitly close
	close(messages)

	for element := range messages {
		listOfWorkloads = append(listOfWorkloads, element)
	}

	displayWorkloads(listOfWorkloads)
}

func getConfig() *string {
	var kubeconfig *string

	if home := homeDir(); home != "" {
		kubeconfig = flag.String("kubeconfig", filepath.Join(home, ".kube", "config"), "(optional) absolute path to the kubeconfig file")
	} else {
		kubeconfig = flag.String("kubeconfig", "", "absolute path to the kubeconfig file")
	}
	flag.Parse()
	return kubeconfig
}

func homeDir() string {
	if h := os.Getenv("HOME"); h != "" {
		return h
	}
	return os.Getenv("USERPROFILE") // windows
}

// Return the list of namespaces in the cluster
func getNamespaces(c *kubernetes.Clientset) []string {
	namespaces, err := c.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})
	var listOfNamespaces []string

	if err != nil {
		panic(err.Error())
	}

	for _, namespace := range namespaces.Items {
		listOfNamespaces = append(listOfNamespaces, namespace.Name)
	}
	return listOfNamespaces
}

// Iterate through Namespace -> Pod -> Container and identify container images
// using either :latest or no tag
func getPodsPerNamespace(namespace string, clientSet *kubernetes.Clientset, c chan identifiedWorkload) {
	pods, err := clientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods.Items {
		for _, container := range pod.Spec.Containers {
			if strings.Contains(container.Image, "latest") == true || strings.Contains(container.Image, ":") == false {
				cont := identifiedWorkload{
					containerName: container.Name,
					namespace:     namespace,
					image:         container.Image,
					pod:           pod.Name,
				}
				c <- cont
			}
		}
	}
}

//Display the results
func displayWorkloads(w []identifiedWorkload) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Namespace", "Pod", "Container", "Image"})

	for _, container := range w {
		t.AppendRow(table.Row{
			container.namespace, container.pod, container.containerName, container.image,
		})
		t.AppendSeparator()
	}
	t.SetStyle(table.StyleLight)

	//render table
	t.Render()
}
