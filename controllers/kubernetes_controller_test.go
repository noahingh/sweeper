package controllers

import (
	"context"
	"time"

	. "github.com/onsi/ginkgo"
	. "github.com/onsi/gomega"

	selectorv1alpha1 "github.com/hanjunlee/sweeper/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	clientgoscheme "k8s.io/client-go/kubernetes/scheme"
)

var (
	schm = runtime.NewScheme()
)

func init() {
	_ = clientgoscheme.AddToScheme(schm)

	_ = selectorv1alpha1.AddToScheme(schm)
	// +kubebuilder:scaffold:scheme
}

var _ = Describe("Kubernetes controller", func() {

	Context("Kubernetes item", func() {
		It("Should clean up deployments successfully", func() {
			const (
				namespace = "kubernetes-enabled"
			)

			By("create a namespace and label enabled.")
			k8sClient.Create(context.Background(), &corev1.Namespace{
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

			By("create a deployment and set the label")
			Expect(k8sClient.Create(context.Background(), &appsv1.Deployment{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Deployment",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nginx",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "true",
					},
				},
				Spec: appsv1.DeploymentSpec{
					Selector: &metav1.LabelSelector{
						MatchLabels: map[string]string{
							"app": "nginx",
						},
					},
					Template: corev1.PodTemplateSpec{
						ObjectMeta: metav1.ObjectMeta{
							Labels: map[string]string{
								"app": "nginx",
							},
						},
						Spec: corev1.PodSpec{
							Containers: []corev1.Container{
								{
									Name:  "nginx",
									Image: "nginx:latest",
								},
							},
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("create a service but doesn't set the label.")
			Expect(k8sClient.Create(context.Background(), &corev1.Service{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Service",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "nginx",
					Namespace: namespace,
					Labels: map[string]string{
						"sweeper.io/selected": "false",
					},
				},
				Spec: corev1.ServiceSpec{
					Selector: map[string]string{
						"app": "nginx",
					},
					Type: corev1.ServiceTypeClusterIP,
					Ports: []corev1.ServicePort{
						{
							Name:       "static",
							Port:       80,
							TargetPort: intstr.FromInt(80),
						},
					},
				},
			})).NotTo(HaveOccurred())

			By("create a kubernetes selector to clean up after 1 second.")
			Expect(k8sClient.Create(context.Background(), &selectorv1alpha1.Kubernetes{
				TypeMeta: metav1.TypeMeta{
					APIVersion: "v1",
					Kind:       "Kubernetes",
				},
				ObjectMeta: metav1.ObjectMeta{
					Name:      "sweeper",
					Namespace: namespace,
				},
				Spec: selectorv1alpha1.KubernetesSpec{
					GroupVersionKinds: []selectorv1alpha1.GroupVersionKind{
						{
							Group:   "apps",
							Version: "v1",
							Kind:    "Deployment",
						},
					},
					ObjectLabels: map[string]string{
						"sweeper.io/selected": "true",
					},
					TTL: "1s",
				},
			})).NotTo(HaveOccurred())

			By("wait one second.")
			time.Sleep(time.Second)

			By("check the nginx deployment doesn't exist.")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: "nginx", Namespace: namespace,
			}, &appsv1.Deployment{})).To(HaveOccurred())

			By("check the nginx service exit")
			Expect(k8sClient.Get(context.Background(), types.NamespacedName{
				Name: "nginx", Namespace: namespace,
			}, &corev1.Service{})).NotTo(HaveOccurred())

		})
	})

})
