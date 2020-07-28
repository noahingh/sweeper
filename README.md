
![sweeper](./docs/img/sweeper.jpeg)

# Sweeper

[![GoDoc](https://godoc.org/github.com/hanjunlee/sweeper?status.svg)](https://pkg.go.dev/github.com/hanjunlee/sweeper)

---

Sweeper is a tool to support developers organize the emphemeral environment on Kubernetes.

## Concept

In many compnies, the environment of development is organized with the static environment, for example, we might have the following environment: `dev`, `staging`, `prod`. But as the time goes the static environment has [some issues](https://pipelinedriven.org/article/the-four-biggest-issues-with-having-static-environments). To cover these issues, the **ephemeral environment** could be one of solutions.  

The emphemeral envrionment contains two extra actions: one at the beginning, and one at the end, that spin up an environment for a purpose, and spin down an environment at the end. Sweeper is contained in the spin-down step, It cleans up selected resources after the TTL time. For example, if you apply a `deployment` and a `service`, which is kubernetes resource, the sweeper will delete resources which is labeled after the ttl time you defined.

## Installation

1. Create the kubernetes CRD.

```shell
$ kustomize build config/crd | kubectl apply -f - 

$ kubectl api-resources | grep sweeper
kubernetes   selector.sweeper.io   true   Kubernetes
...
```

2. Install the controller.

```shell
$ kustomize build config/manager | kubectl apply -f  -
```

## Usage

### Kubernetes 

The `kubernetes` resource is active in the namespace which has the label `sweeper.io/enabled=true`. You should set the label the namespace which you want to activate like below.

```shell
$ kubectl label ns sweeper sweeper.io/enabled=true --overwrite
```

Create the `kubernetes` manifest on the namespace. You reference the sample directory(in `./config/samples`) and you can read [godoc]().

## Contribute

TBD

## License

TBD
