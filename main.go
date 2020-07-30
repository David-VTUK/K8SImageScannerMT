package main

import (
	"context"
	"flag"
	"os"
	"path"
	"sort"
	"strings"
	"sync"

	"github.com/jedib0t/go-pretty/v6/table"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
)

type identifiedWorkload struct {
	containerName, namespace, image, pod string
}

// byNamespace implements sort.Interface based on the namespace field.
type byNamespace []identifiedWorkload

func (n byNamespace) Len() int           { return len(n) }
func (n byNamespace) Less(i, j int) bool { return n[i].namespace < n[j].namespace }
func (n byNamespace) Swap(i, j int)      { n[i], n[j] = n[j], n[i] }

// Channel buffer size
const (
	defaultKubeconfig = "~/.kube/config"
	burst             = 50
	qps               = 25
)

func main() {
	kubeconfig, err := getConfig()
	if err != nil {
		handleError(err)
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig)
	if err != nil {
		handleError(err)
	}

	// Increase the Burst and QOS values
	config.Burst = burst
	config.QPS = qps

	// Build client from  config
	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		handleError(err)
	}

	ctx := context.Background()

	// Get the list of containers
	numberOfContainers, err := getTotalNumberOfContainers(ctx, clientset)
	if err != nil {
		handleError(err)
	}

	// Add a buffer of 10% - in case extra containers are spun whilst this app finishes
	numberOfContainers += (numberOfContainers / 10)

	// Grab the list of namespaces in the current context
	listOfNamespaces, err := getNamespaces(ctx, clientset)
	if err != nil {
		handleError(err)
	}

	// Put all threads in a waitgroup so the channel can be closed once all threads have finished
	var wg sync.WaitGroup
	// Match the number of waitgroups to the number of namespaces.
	// as each call to getPodsPerNamespace() will be in its own goroutine
	wg.Add(len(listOfNamespaces))

	// Retrieve responses from threaded calls
	messages := make(chan identifiedWorkload, numberOfContainers)

	//Iterate through the namespaces
	for _, namespace := range listOfNamespaces {
		//For each namespace, inspect the pods that reside it within a dedicated goroutine
		go func(n string) {
			if err := getPodsPerNamespace(ctx, n, clientset, messages); err != nil {
				handleError(err)
			}
			defer wg.Done()
		}(namespace)
	}
	// Wait for all goroutines to finish
	wg.Wait()

	// As this uses a buffered channel, we need to explicitly close
	close(messages)

	var listOfWorkloads []identifiedWorkload
	for element := range messages {
		listOfWorkloads = append(listOfWorkloads, element)
	}

	sort.Sort(byNamespace(listOfWorkloads))
	displayWorkloads(listOfWorkloads)
}

func handleError(err error) {
	panic(err.Error())
}

func getConfig() (string, error) {
	var filename string
	var err error
	kubeconfigFlag := flag.String("kubeconfig", "", "path to the kubeconfig file")
	flag.Parse()

	filename = *kubeconfigFlag
	if filename == "" {
		filename = defaultKubeconfig
	}
	filename, err = homeDir(filename)
	if err != nil {
		return "", err
	}
	if _, err := os.Stat(filename); err != nil {
		return "", err
	}

	return filename, nil
}

func homeDir(filename string) (string, error) {
	if strings.Contains(filename, "~/") {
		homedir, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		filename = strings.Replace(filename, "~/", "", 1)
		filename = path.Join(homedir, filename)
	}
	return filename, nil
}

// Return the list of namespaces in the cluster
func getNamespaces(ctx context.Context, c *kubernetes.Clientset) ([]string, error) {
	var listOfNamespaces []string
	namespaces, err := c.CoreV1().Namespaces().List(ctx, metav1.ListOptions{})
	if err != nil {
		return listOfNamespaces, err
	}

	for _, namespace := range namespaces.Items {
		listOfNamespaces = append(listOfNamespaces, namespace.Name)
	}
	return listOfNamespaces, nil
}

// Iterate through Namespace -> Pod -> Container and identify container images
// using either :latest or no tag
func getPodsPerNamespace(ctx context.Context, namespace string, clientSet *kubernetes.Clientset, c chan identifiedWorkload) error {
	pods, err := clientSet.CoreV1().Pods(namespace).List(ctx, metav1.ListOptions{})
	if err != nil {
		return err
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

	return nil
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

//Get the list of pods in the cluster. This will determine the buffer size of the channel
func getTotalNumberOfContainers(ctx context.Context, clientSet *kubernetes.Clientset) (int, error) {

	numberofcontainers := 0
	pods, err := clientSet.CoreV1().Pods("").List(ctx, metav1.ListOptions{})
	if err != nil {
		return 0, err
	}

	for _, pod := range pods.Items {
		numberofcontainers += len(pod.Spec.Containers)
	}
	return numberofcontainers, nil
}
