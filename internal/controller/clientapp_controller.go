/*
Copyright 2023.

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

package controller

import (
	"context"

	"github.com/go-logr/logr"
	cachev1alpha1 "github.com/juliusoh/clinia-test/api/v1alpha1"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/controller"
	"sigs.k8s.io/controller-runtime/pkg/controller/controllerutil"
	"sigs.k8s.io/controller-runtime/pkg/log"
)

// ClientAppReconciler reconciles a ClientApp object
type ClientAppReconciler struct {
	client.Client
	Log logr.Logger
	Scheme *runtime.Scheme
}

//+kubebuilder:rbac:groups=cache.clinia-test.com,resources=clientapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=cache.clinia-test.com,resources=clientapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=cache.clinia-test.com,resources=clientapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the ClientApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.15.0/pkg/reconcile
func (r *ClientAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = log.FromContext(ctx)

	// TODO(user): your logic here
	// Retrieve Client App Instance
	clientApp := &cachev1alpha1.ClientApp{}
	err := r.Get(ctx, req.NamespacedName, clientApp)
	if err != nil {
		if errors.IsNotFound(err) {
			return ctrl.Result{}, nil
		}
		return ctrl.Result{}, err
	}

	// Check if deployment exists or else create new deployment
	found := &appsv1.Deployment{}
	err = r.Get(ctx, types.NamespacedName{Name: clientApp.Name, Namespace: clientApp.Namespace}, found)
	if err != nil {
		if errors.IsNotFound(err) {
			dep := r.deploymentForClientApp(clientApp)
			if err := r.Create(ctx, dep); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else {
			return ctrl.Result{}, err
		}
	}

	// Ensure the deployment size is the same as the spec
	size := clientApp.Spec.Replicas 
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		if err := r.Update(ctx, found); err != nil {
			return ctrl.Result{}, err
		}
		return ctrl.Result{Requeue: true}, nil
	}

	// Update ClientAppStatus Available
	// Todo: monitor deployment itself, pods are running properly (check for img pull err), check for container startup errors
	// Todo: run operator in cluster and change replicas to verify changes
	// Todo: status resource -> valid and makes sense
	clientApp.Status.Available = true
	// Update ClientAppStatus URL
	service := &corev1.Service{}
	err = r.Get(ctx, types.NamespacedName{Name: clientApp.Name, Namespace: clientApp.Namespace}, service)
	if err != nil {
		if errors.IsNotFound(err) {
			dep := r.serviceForClientApp(clientApp)
			if err := r.Create(ctx, dep); err != nil {
				return ctrl.Result{}, err
			}
			return ctrl.Result{Requeue: true}, nil
		} else {
			return ctrl.Result{}, err
		}
	}
	clientApp.Status.URL = "http://" + service.Status.LoadBalancer.Ingress[0].IP

	return ctrl.Result{}, nil
}

func (r *ClientAppReconciler) serviceForClientApp(m *cachev1alpha1.ClientApp) *corev1.Service {
	lbls := labelsForApp(m.Name)

	svc := &corev1.Service {
		ObjectMeta: metav1.ObjectMeta{
			Name: m.Name,
			Namespace: m.Namespace,
		},
		Spec: corev1.ServiceSpec{
			Selector: lbls,
			Type: corev1.ServiceTypeLoadBalancer,
			Ports: []corev1.ServicePort{{
				Port: m.Spec.Port,
				TargetPort: intstr.FromInt(int(m.Spec.Port)),
				Protocol: corev1.ProtocolTCP,
				Name: "clientapp",
			}},
	},
}
	controllerutil.SetControllerReference(m, svc, r.Scheme)
	return svc
}

func (r *ClientAppReconciler) deploymentForClientApp(m *cachev1alpha1.ClientApp) *appsv1.Deployment {
    lbls := labelsForApp(m.Name)
    replicas := m.Spec.Replicas

    dep := &appsv1.Deployment{
        ObjectMeta: metav1.ObjectMeta{
            Name:      m.Name,
            Namespace: m.Namespace,
        },
        Spec: appsv1.DeploymentSpec{
            Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: lbls,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: lbls,
				},
				Spec: corev1.PodSpec{
					Containers: []corev1.Container{{
						Image: m.Spec.Image,
						Name: "clientapp",
						Ports: []corev1.ContainerPort{{
							ContainerPort: m.Spec.Port,
							Name: "clientapp",
						}},
						Env: m.Spec.Env,
						Resources: m.Spec.Resources,
					}},
				},
			},
        },
    }

    // Set Memcached instance as the owner and controller.memcac
    // NOTE: calling SetControllerReference, and setting owner references in
    // general, is important as it allows deleted objects to be garbage collected.
    controllerutil.SetControllerReference(m, dep, r.Scheme)
    return dep
}

// labelsForApp creates a simple set of labels for Memcached.
func labelsForApp(name string) map[string]string {
    return map[string]string{"cr_name": name}
}

// SetupWithManager sets up the controller with the Manager.
func (r *ClientAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&cachev1alpha1.ClientApp{}).
		Owns(&appsv1.Deployment{}).
		WithOptions(controller.Options{MaxConcurrentReconciles: 2}).
		Complete(r)
}
