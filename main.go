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
)

type identifiedWorkload struct {
	containerName, namespace, image string
}

func main() {

	var listOfNamespaces []string
	//var listOfWorkloads []identifiedWorkload
	messages := make(chan identifiedWorkload, 10)

	var wg sync.WaitGroup

	kubeconfig := getConfig()

	/*
		Package clientcmd provides one stop shopping for building a working client from a fixed config,
		from a .kubeconfig file, from command line flags, or from any merged combination.
	*/

	config, err := clientcmd.BuildConfigFromFlags("", *kubeconfig)
	if err != nil {
		panic(err.Error())
	}

	config.Burst = 50
	config.QPS = 25
	/*
		NewForConfig creates a new Clientset for the given config.
		If config's RateLimiter is not set and QPS and Burst are acceptable,
		NewForConfig will generate a rate-limiter in configShallowCopy.
	*/

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		panic(err.Error())
	}

	listOfNamespaces = getNamespaces(clientset)

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
		fmt.Println(element.containerName)
	}

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
	}
	return listOfNamespaces
}

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
				}
				fmt.Println("in", cont.containerName)
				c <- cont
			}
		}
	}
}
