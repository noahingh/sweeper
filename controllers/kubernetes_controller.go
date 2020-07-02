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
	"sync"
	"time"

	"github.com/go-logr/logr"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/runtime/schema"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	selectorv1alpha1 "github.com/hanjunlee/sweeper/api/v1alpha1"
)

// KubernetesReconciler reconciles a Kubernetes object
type KubernetesReconciler struct {
	client.Client
	Log    logr.Logger
	Scheme *runtime.Scheme
}

// +kubebuilder:rbac:groups=selector.sweeper.io,resources=kubernetes,verbs=get;list;watch;create;update;patch;delete
// +kubebuilder:rbac:groups=selector.sweeper.io,resources=kubernetes/status,verbs=get;update;patch

// Reconcile delete selected resource which has overed TTL.
func (r *KubernetesReconciler) Reconcile(req ctrl.Request) (ctrl.Result, error) {
	ctx := context.Background()
	logger := r.Log.WithValues("kubernetes", req.NamespacedName)

	ks := &selectorv1alpha1.Kubernetes{}
	if err := r.Get(ctx, req.NamespacedName, ks); err != nil {
		logger.Error(err, "failed to get kubernetes selector")
		return ctrl.Result{}, client.IgnoreNotFound(err)
	}

	logger.Info("start to delete selected resources over TTL")
	r.deleteResourcesOverTTL(ctx, ks.GetNamespace(), ks.Spec.GroupVersionKinds, ks.Spec.ObjectLabels, ks.Spec.TTL)

	return ctrl.Result{RequeueAfter: time.Minute}, nil
}

// SetupWithManager -
func (r *KubernetesReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&selectorv1alpha1.Kubernetes{}).
		Complete(r)
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
				ttlAt, err := r.getTTLTime(ttl)
				if err != nil {
					logger.Error(err, "failed to parse ttl")
					continue
				}

				createdAt := i.GetCreationTimestamp()
				if createdAt.After(ttlAt) {
					logger.V(1).Info("delete the resource over ttl", 
						"kind", i.GetKind(), 
						"namespace", i.GetNamespace(), 
						"name", i.GetName())
					r.Delete(ctx, &i)
				} else {
					logger.V(1).Info("pass the resource", 
						"kind", i.GetKind(), 
						"namespace", i.GetNamespace(), 
						"name", i.GetName())
				}
			}
		}(gvk.Group, gvk.Version, gvk.Kind)
	}

	wg.Wait()
}

func (r *KubernetesReconciler) getTTLTime(ttl string) (time.Time, error) {
	d, err := time.ParseDuration(ttl)
	if err != nil {
		return time.Time{}, err
	}

	now := metav1.Now()
	return now.Add(d), nil
}
