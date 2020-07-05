package controllers

import (
	"context"
	"math"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	selectorv1alpha1 "github.com/hanjunlee/sweeper/api/v1alpha1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
)

var (
	schm = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(schm)

	_ = selectorv1alpha1.AddToScheme(schm)
	// +kubebuilder:scaffold:scheme
}

var _ = Describe("Kubernetes resource controller", func() {

	Context("isEnabledNamespace", func() {
		var (
			r *KubernetesReconciler
			// it has the different namespace to prevent the conflict.
			namespace string
		)

		BeforeEach(func() {
			// tearup
			r = &KubernetesReconciler{
				Client: k8sClient,
				Log:    ctrl.Log,
				Scheme: schm,
			}
		})

		AfterEach(func() {
			err := r.Client.Delete(context.Background(), &corev1.Namespace{
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			})
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return false if there isn't the label", func() {
			namespace = "foo"

			r.Client.Create(context.Background(), &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
				},
			})

			enabled, err := r.isEnabledNamespace(context.Background(), namespace)
			Expect(enabled).To(BeFalse())
			Expect(err).NotTo(HaveOccurred())
		})

		It("should return false if the value is wrong", func() {
			namespace = "bar"

			r.Client.Create(context.Background(), &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"sweeper.io/enabled": "bad",
					},
				},
			})

			enabled, err := r.isEnabledNamespace(context.Background(), namespace)
			Expect(enabled).To(BeFalse())
			Expect(err).To(HaveOccurred())
		})

		It("should return true", func() {
			namespace = "baz"

			r.Client.Create(context.Background(), &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"sweeper.io/enabled": "true",
					},
				},
			})

			enabled, err := r.isEnabledNamespace(context.Background(), namespace)
			Expect(enabled).To(BeTrue())
			Expect(err).NotTo(HaveOccurred())
		})
	})

	// For each test case, it has a different namespace
	// to isolate resources.
	Context("deleteResourcesOverTTL", func() {
		It("should delete pods matched with labels", func() {
			const (
				namespace = "delete-selected"
			)
			var (
				r *KubernetesReconciler
			)

			r = &KubernetesReconciler{
				Client: k8sClient,
				Log:    ctrl.Log,
				Scheme: schm,
			}

			By("initialize the reconciler by creating namespace and pods")
			r.Client.Create(context.Background(), &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"sweeper.io/enabled": "true",
					},
				},
			})

			Expect(r.Client.Create(context.Background(), &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "selected",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "true",
					},
					CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Second)),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "bar",
						},
					},
				},
			})).NotTo(HaveOccurred())

			Expect(r.Client.Create(context.Background(), &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "notselected",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "false",
					},
					CreationTimestamp: metav1.NewTime(time.Now().Add(-time.Second)),
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "bar",
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("delete pods which has the value of label is true")
			r.deleteResourcesOverTTL(context.Background(),
				namespace,
				[]selectorv1alpha1.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, map[string]string{
					"sweeper.io/selected": "true",
				},
				"0s",
			)
			p := &corev1.PodList{}
			r.Client.List(context.Background(), p, client.InNamespace(namespace))
			Expect(len(p.Items)).To(Equal(1))
		})

		It("should delete pods only over the expired time", func() {
			const (
				namespace = "delete-expired"
			)
			var (
				r *KubernetesReconciler
			)

			r = &KubernetesReconciler{
				Client: k8sClient,
				Log:    ctrl.Log,
				Scheme: schm,
			}

			By("initialize the reconciler by creating namespace and pods")
			r.Client.Create(context.Background(), &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"sweeper.io/enabled": "true",
					},
				},
			})

			Expect(r.Client.Create(context.Background(), &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "expired",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "bar",
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("wait 2s to delay the created timestamp.")
			time.Sleep(2 * time.Second)
			Expect(r.Client.Create(context.Background(), &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "not-expired",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "bar",
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("delete pods which is over the expired time")
			r.deleteResourcesOverTTL(context.Background(),
				namespace,
				[]selectorv1alpha1.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, map[string]string{
					"sweeper.io/selected": "true",
				},
				"1s",
			)
			p := &corev1.PodList{}
			r.Client.List(context.Background(), p, client.InNamespace(namespace))
			Expect(len(p.Items)).To(Equal(1))
		})
	})

	Context("getNextRun", func() {
		It("returns the latest expired time", func() {
			const (
				namespace = "nextrun-latest"
			)
			var (
				r *KubernetesReconciler
			)

			r = &KubernetesReconciler{
				Client: k8sClient,
				Log:    ctrl.Log,
				Scheme: schm,
			}

			By("initialize the reconciler by creating namespace and pods")
			r.Client.Create(context.Background(), &corev1.Namespace{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Namespace",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name: namespace,
					Labels: map[string]string{
						"sweeper.io/enabled": "true",
					},
				},
			})

			Expect(r.Client.Create(context.Background(), &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-now",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "bar",
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("wait 1s to delay the created timestamp.")
			time.Sleep(1 * time.Second)
			Expect(r.Client.Create(context.Background(), &corev1.Pod{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Pod",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "created-two-before",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "true",
					},
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{
						{
							Name:  "foo",
							Image: "bar",
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("The next should be eight seconds later(10s - 2s = 8s).")
			next := r.getNextRun(context.Background(),
				namespace,
				[]selectorv1alpha1.GroupVersionKind{
					{
						Group:   "",
						Version: "v1",
						Kind:    "Pod",
					},
				}, map[string]string{
					"sweeper.io/selected": "true",
				},
				"10s",
			)

			diff := next.Sub(time.Now())
			Expect(math.Ceil(diff.Seconds())).To(Equal(float64(9)))
		})
	})
})
