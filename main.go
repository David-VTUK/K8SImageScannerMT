package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"

	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"

	"github.com/jedib0t/go-pretty/table"
)

type identifiedWorkload struct {
	containerName, namespace, image string
}

const maxChannelElements = 2000

func main() {

	var listOfNamespaces []string
	var listOfWorkloads []identifiedWorkload

	// Retrieve responses from threaded calls
	messages := make(chan identifiedWorkload, maxChannelElements)

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

	//
	wg.Add(len(listOfNamespaces))

	for _, namespace := range listOfNamespaces {
		go func(n string) {
			getPodsPerNamespace(n, clientset, messages)
			defer wg.Done()
		}(namespace)
	}
	wg.Wait()
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

func getNamespaces(c *kubernetes.Clientset) []string {
	namespaces, err := c.CoreV1().Namespaces().List(context.TODO(), metav1.ListOptions{})

	//context := context
	var listOfNamespaces []string

	if err != nil {
		panic(err.Error())
	}

	for _, namespace := range namespaces.Items {
		listOfNamespaces = append(listOfNamespaces, namespace.Name)
		fmt.Println("Adding namespace", namespace.Name)
	}
	return listOfNamespaces
}

func getPodsPerNamespace(namespace string, clientSet *kubernetes.Clientset, c chan identifiedWorkload) {
	fmt.Println("Getting pods in namespace", namespace)
	pods, err := clientSet.CoreV1().Pods(namespace).List(context.TODO(), metav1.ListOptions{})

	if err != nil {
		panic(err.Error())
	}

	for _, pod := range pods.Items {
		fmt.Println("Getting pod", pod.Name)
		for _, container := range pod.Spec.Containers {
			fmt.Println("Getting container", container.Name)
			if strings.Contains(container.Image, "latest") == true || strings.Contains(container.Image, ":") == false {
				cont := identifiedWorkload{
					containerName: container.Name,
					namespace:     namespace,
					image:         container.Image,
				}
				c <- cont
			}
		}
	}
}

func displayWorkloads(w []identifiedWorkload) {
	t := table.NewWriter()
	t.SetOutputMirror(os.Stdout)
	t.AppendHeader(table.Row{"Namespace", "Container", "Image"})

	for _, container := range w {
		t.AppendRow(table.Row{
			container.namespace, container.containerName, container.image,
		})
		t.AppendSeparator()
	}
	t.SetStyle(table.StyleLight)
	t.Render()
}
