/*


Licensed under the Apache License, Version 2.0 (the "License");
you may not use this file except in compliance with the License.
You may obtain a copy of the License at

    http://www.apache.org/licenses/LICENSE-2.0

Unless required by applicable law or agreed to in writing, software
distributed under the License is distributed on an "AS IS" BASIS,
WITHOUT WARRANTIES OR CONDITIONS OF ANY KIND, either express or implied.
See the License for the specific language governing permissions and
limitations under the License.
*/

package controllers

import (
	"context"
	"fmt"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/logr"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/util/workqueue"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	selectorv1alpha1 "github.com/hanjunlee/sweeper/api/v1alpha1"
)

const (
	// TODO: comment on it.
	namespaceEnableLabel = "sweeper.io/enabled"
)

// KubernetesReconciler reconciles a Kubernetes object
type KubernetesReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme

	Workqueue workqueue.RateLimitingInterface
}

// +kubebuilder:rbac:groups=selector.sweeper.io,resources=kubernetes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=selector.sweeper.io,resources=kubernetes/status,verbs=get;update;patch

// Reconcile delete selected resource which has overed TTL.
func (r *KubernetesReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("kubernetes", req.NamespacedName)

	ok, err := r.isEnabledNamespace(ctx, req.Namespace)
	if err != nil {
		logger.Error(err, "failed check the namespace is valid")
	}
	if !ok {
		logger.Info("it's in the disabled namespace, set the sweeper.io/enabled label true if you want to be enabled")
		return ctrl.Result{}, nil
	}

	ks := &selectorv1alpha1.Kubernetes{}
	if err := r.Get(ctx, req.NamespacedName, ks); err != nil {
		logger.Error(err, "failed to get kubernetes selector")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	ttl, err := time.ParseDuration(ks.Spec.TTL)
	if err != nil {
		logger.Error(err, "failed to parse the duration")
		return ctrl.Result{}, nil
	}

	logger.Info("gather all items of GVK.")
	resources := r.getGVKResources(ctx, ks.GetNamespace(), ks.Spec.GroupVersionKinds, ks.Spec.ObjectLabels)

	for _, resource := range resources {
		_, err := r.enqueueResourcesOverTTL(ctx, resource, ttl)
		if err != nil {
			logger.Error(err, "failed to enqueue the resource")
			continue
		}
	}

	next := r.getNextRunTime(ctx, resources, ttl)
	logger.Info("wait to the next run",
		"wait(s)", next.Sub(time.Now()).Seconds(),
	)

	return ctrl.Result{
		RequeueAfter: next.Sub(time.Now()),
	}, nil
}

// SetupWithManager -
func (r *KubernetesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	const (
		thread = 2
	)

	// run workers
	for i := 0; i < thread; i++ {
		go r.runWorker()
	}

	return ctrl.NewControllerManagedBy(mgr).
		For(&selectorv1alpha1.Kubernetes{}).
		Complete(r)
}

// isEnabledNamespace check that the namespace has the label sweeper.io/enabled.
func (r *KubernetesReconciler) isEnabledNamespace(ctx context.Context, namespace string) (bool, error) {
	ns := &corev1.Namespace{}
	err := r.Get(ctx, client.ObjectKey{
		Namespace: "",
		Name:      namespace,
	}, ns)
	if err != nil {
		return false, err
	}

	v, ok := ns.GetLabels()[namespaceEnableLabel]
	if !ok {
		return false, nil
	}

	isEnabled, err := strconv.ParseBool(v)
	if err != nil {
		return false, err
	}

	return isEnabled, nil
}

func (r *KubernetesReconciler) getGVKResources(
	ctx context.Context,
	namespace string,
	gvks []selectorv1alpha1.GroupVersionKind,
	labels map[string]string,
) []unstructured.Unstructured {
	var (
		logger = r.Log

		wg    = sync.WaitGroup{}
		mutex = sync.Mutex{}
		ret   = make([]unstructured.Unstructured, 0)
	)

	for _, gvk := range gvks {
		wg.Add(1)

		go func(group, version, kind string) {
			defer wg.Done()

			u := unstructured.UnstructuredList{}
			u.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    kind,
			})

			if err := r.List(ctx, &u, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
				logger.Error(err, "failed to list for gvk", "group", group, "version", version, "kind", kind)
				return
			}

			// gathering all items of GVK.
			for _, i := range u.Items {
				mutex.Lock()
				ret = append(ret, i)
				mutex.Unlock()
			}
		}(gvk.Group, gvk.Version, gvk.Kind)
	}

	wg.Wait()

	return ret
}

func (r *KubernetesReconciler) enqueueResourcesOverTTL(
	ctx context.Context,
	obj unstructured.Unstructured,
	ttl time.Duration,
) (bool, error) {
	createdAt := obj.GetCreationTimestamp()
	expiredAt := createdAt.Add(ttl)

	if expiredAt.Before(time.Now()) {
		r.Workqueue.Add(obj)
		return true, nil
	}

	return false, nil
}

func (r *KubernetesReconciler) getNextRunTime(
	ctx context.Context,
	objs []unstructured.Unstructured,
	ttl time.Duration,
) time.Time {
	var (
		nextRun = time.Now().Add(time.Minute)
	)

	for _, obj := range objs {
		createdAt := obj.GetCreationTimestamp()
		expiredAt := createdAt.Add(ttl)

		if expiredAt.Before(nextRun) {
			nextRun = expiredAt
		}
	}

	return nextRun
}

func (r *KubernetesReconciler) deleteResourcesOverTTL(
	ctx context.Context,
	namespace string,
	gvks []selectorv1alpha1.GroupVersionKind,
	labels map[string]string,
	ttl string,
) {
	logger := r.Log
	wg := sync.WaitGroup{}

	for _, gvk := range gvks {
		wg.Add(1)

		go func(group, version, kind string) {
			defer wg.Done()

			u := unstructured.UnstructuredList{}
			u.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    kind,
			})

			if err := r.List(ctx, &u, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
				logger.Error(err, "failed to list for gvk", "group", group, "version", version, "kind", kind)
				return
			}

			for _, i := range u.Items {
				d, err := time.ParseDuration(ttl)
				if err != nil {
					logger.Error(err, "failed to parse ttl")
					continue
				}

				createdAt := i.GetCreationTimestamp()
				expiredAt := createdAt.Add(d)

				if expiredAt.Before(time.Now()) {
					logger.V(1).Info("delete the resource over ttl",
						"kind", i.GetKind(),
						"namespace", i.GetNamespace(),
						"name", i.GetName(),
						"expired_at", expiredAt,
					)
					r.Delete(ctx, &i)
				} else {
					logger.V(1).Info("pass the resource",
						"kind", i.GetKind(),
						"namespace", i.GetNamespace(),
						"name", i.GetName(),
						"expired_at", expiredAt,
					)
				}
			}
		}(gvk.Group, gvk.Version, gvk.Kind)
	}

	wg.Wait()
}

// getNextRun return the lastest expired time. But it returns one minute
// if it doesn't exist.
func (r *KubernetesReconciler) getNextRun(
	ctx context.Context,
	namespace string,
	gvks []selectorv1alpha1.GroupVersionKind,
	labels map[string]string,
	ttl string,
) time.Time {
	var (
		logger  = r.Log
		wg      = sync.WaitGroup{}
		mutex   = &sync.Mutex{}
		nextRun = time.Now().Add(time.Minute)
	)

	for _, gvk := range gvks {
		wg.Add(1)

		go func(group, version, kind string) {
			defer wg.Done()

			u := unstructured.UnstructuredList{}
			u.SetGroupVersionKind(schema.GroupVersionKind{
				Group:   group,
				Version: version,
				Kind:    kind,
			})

			if err := r.List(ctx, &u, client.InNamespace(namespace), client.MatchingLabels(labels)); err != nil {
				logger.Error(err, "failed to list for gvk", "group", group, "version", version, "kind", kind)
				return
			}

			for _, i := range u.Items {
				d, err := time.ParseDuration(ttl)
				if err != nil {
					logger.Error(err, "failed to parse ttl")
					continue
				}

				createdAt := i.GetCreationTimestamp()
				expiredAt := createdAt.Add(d)

				mutex.Lock()
				if expiredAt.Before(nextRun) {
					nextRun = expiredAt
				}
				mutex.Unlock()
			}
		}(gvk.Group, gvk.Version, gvk.Kind)
	}

	wg.Wait()
	return nextRun
}

// run workers to delete resources which is over TTL.
func (r *KubernetesReconciler) runWorker() error {
	for r.processNextWorkItem() {
	}
	return nil
}

func (r *KubernetesReconciler) processNextWorkItem() bool {
	logger := r.Log

	item, shutdown := r.Workqueue.Get()
	if shutdown {
		logger.Info("the workqueue is shutdown.")
		return false
	}

	err := func(item interface{}) error {
		defer r.Workqueue.Done(item)

		obj, ok := item.(unstructured.Unstructured)
		if !ok {
			r.Workqueue.Forget(item)
			return fmt.Errorf("failed to convert type")
		}

		ctx, cancel := context.WithTimeout(context.Background(), time.Second)
		defer cancel()

		if err := r.Delete(ctx, &obj); err != nil {
			r.Workqueue.Add(obj)
			return fmt.Errorf("failed to delete the item: %s", obj.GetName())
		}
		logger.Info("delete the object: %s", obj.GetName())

		return nil
	}(item)
	if err != nil {
		logger.Error(err, "failed to process the item")
		return true
	}

	return true
}
