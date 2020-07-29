# Multi Threaded K8s Image Scanner

## About

This application reads a kubeconfig file in ~/.kube/config and iterates through all namespaces and pods to determine those which have been created with either:

* `:latest` 
* `default` 

![alt text](./images/app.gif)