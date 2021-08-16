/*
Copyright 2021.

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

	"github.com/prometheus/common/log"
	appsv1 "k8s.io/api/apps/v1"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/api/errors"
	"k8s.io/apimachinery/pkg/api/resource"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/apimachinery/pkg/types"
	"k8s.io/apimachinery/pkg/util/intstr"
	ctrl "sigs.k8s.io/controller-runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"

	"github.com/go-logr/logr"
	apiv1 "github.com/vinamra28/demo-operator/api/v1"
)

// HelloAppReconciler reconciles a HelloApp object
type HelloAppReconciler struct {
	Client client.Client
	Scheme *runtime.Scheme
	Log    logr.Logger
}

//+kubebuilder:rbac:groups=apps.example.com,resources=helloapps,verbs=get;list;watch;create;update;patch;delete
//+kubebuilder:rbac:groups=apps.example.com,resources=helloapps/status,verbs=get;update;patch
//+kubebuilder:rbac:groups=apps.example.com,resources=helloapps/finalizers,verbs=update

// Reconcile is part of the main kubernetes reconciliation loop which aims to
// move the current state of the cluster closer to the desired state.
// TODO(user): Modify the Reconcile function to compare the state specified by
// the HelloApp object against the actual cluster state, and then
// perform operations to make the cluster state reflect the state specified by
// the user.
//
// For more details, check Reconcile and its Result here:
// - https://pkg.go.dev/sigs.k8s.io/controller-runtime@v0.8.3/pkg/reconcile
func (r *HelloAppReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
	_ = r.Log.WithValues("helloapp", req.NamespacedName)
	log.Info("Processing HelloAppReconciler.")
	helloApp := &apiv1.HelloApp{}
	err := r.Client.Get(ctx, req.NamespacedName, helloApp)
	if err != nil {
		if errors.IsNotFound(err) {
			// Request object not found, could have been deleted after reconcile request.
			// Owned objects are automatically garbage collected. For additional cleanup logic use finalizers.
			// Return and don't requeue
			log.Info("HelloApp resource not found. Ignoring since object must be deleted")
			return ctrl.Result{}, nil
		}
		// Error reading the object - requeue the request.
		log.Error(err, "Failed to get HelloApp")
		return ctrl.Result{}, err
	}

	// check if secret already exists else create one secret
	secretName := helloApp.Spec.SecretName
	dbSecret := &corev1.Secret{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: secretName, Namespace: helloApp.Namespace}, dbSecret)
	if err != nil && errors.IsNotFound(err) {
		res := r.createSecret(helloApp)
		log.Info("Creating a new Secret", "Secret.Namespace", res.Namespace, "Secret.Name", res.Name)
		err = r.Client.Create(ctx, res)
		if err != nil {
			log.Error(err, "Failed to create new Secret", "Secret.Namespace", res.Namespace, "Secret.Name", res.Name)
			return ctrl.Result{}, err
		}
	} else if err != nil {
		log.Error(err, "Failed to get Secret")
		return ctrl.Result{}, err
	}

	ctrl.SetControllerReference(helloApp, dbSecret, r.Scheme)

	pvc := r.createPVC(helloApp)
	err = r.Client.Create(ctx, pvc)
	if err != nil {
		log.Error(err, "Failed to create new PVC", "PVC.Namespace", pvc.Namespace, "PVC.Name", pvc.Name)
		return ctrl.Result{}, err
	}

	svc := r.createService(helloApp)
	err = r.Client.Create(ctx, svc)
	if err != nil {
		log.Error(err, "Failed to create new SVC", "SVC.Namespace", svc.Namespace, "SVC.Name", svc.Name)
		return ctrl.Result{}, err
	}

	// Check if the deployment already exists, if not create a new one
	found := &appsv1.Deployment{}
	err = r.Client.Get(ctx, types.NamespacedName{Name: helloApp.Name, Namespace: helloApp.Namespace}, found)
	if err != nil && errors.IsNotFound(err) {
		dep := r.deployHelloApp(helloApp)
		log.Info("Creating a new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
		err = r.Client.Create(ctx, dep)
		if err != nil {
			log.Error(err, "Failed to create new Deployment", "Deployment.Namespace", dep.Namespace, "Deployment.Name", dep.Name)
			return ctrl.Result{}, err
		}
		// Deployment created successfully - return and requeue
		return ctrl.Result{Requeue: true}, nil
	} else if err != nil {
		log.Error(err, "Failed to get Deployment")
		return ctrl.Result{}, err
	}

	// Check desired amount of deploymets.
	size := helloApp.Spec.Size
	if *found.Spec.Replicas != size {
		found.Spec.Replicas = &size
		err = r.Client.Update(ctx, found)
		if err != nil {
			log.Error(err, "Failed to update Deployment", "Deployment.Namespace", found.Namespace, "Deployment.Name", found.Name)
			return ctrl.Result{}, err
		}
		// Spec updated - return and requeue
		return ctrl.Result{Requeue: true}, nil
	}
	return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *HelloAppReconciler) SetupWithManager(mgr ctrl.Manager) error {
	return ctrl.NewControllerManagedBy(mgr).
		For(&apiv1.HelloApp{}).
		Complete(r)
}

func (c *HelloAppReconciler) createSecret(ha *apiv1.HelloApp) *corev1.Secret {
	secret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db",
			Namespace: ha.Namespace,
			Labels: map[string]string{
				"apps": "db",
			},
		},
		Type: corev1.SecretTypeOpaque,
		StringData: map[string]string{
			"POSTGRES_DB":       "hub",
			"POSTGRES_USER":     "postgres",
			"POSTGRES_PASSWORD": "postgres",
			"POSTGRES_PORT":     "5432",
		},
	}
	ctrl.SetControllerReference(ha, secret, c.Scheme)
	return secret
}

func (c *HelloAppReconciler) createPVC(ha *apiv1.HelloApp) *corev1.PersistentVolumeClaim {
	pvc := &corev1.PersistentVolumeClaim{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db",
			Namespace: ha.Namespace,
			Labels: map[string]string{
				"apps": "db",
			},
		},
		Spec: corev1.PersistentVolumeClaimSpec{
			AccessModes: []corev1.PersistentVolumeAccessMode{"ReadWriteOnce"},
			Resources: corev1.ResourceRequirements{
				Requests: corev1.ResourceList{
					corev1.ResourceName(corev1.ResourceStorage): resource.MustParse("1Gi"),
				},
			},
		},
	}
	ctrl.SetControllerReference(ha, pvc, c.Scheme)
	return pvc
}

func (c *HelloAppReconciler) deployHelloApp(ha *apiv1.HelloApp) *appsv1.Deployment {
	replicas := ha.Spec.Size
	labels := map[string]string{"app": "db"}
	image := ha.Spec.Image
	dep := &appsv1.Deployment{
		ObjectMeta: metav1.ObjectMeta{
			Name:      ha.Name,
			Namespace: ha.Namespace,
		},
		Spec: appsv1.DeploymentSpec{
			Replicas: &replicas,
			Selector: &metav1.LabelSelector{
				MatchLabels: labels,
			},
			Template: corev1.PodTemplateSpec{
				ObjectMeta: metav1.ObjectMeta{
					Labels: labels,
				},
				Spec: corev1.PodSpec{
					Volumes: []corev1.Volume{
						{
							Name: "db",
							VolumeSource: corev1.VolumeSource{
								PersistentVolumeClaim: &corev1.PersistentVolumeClaimVolumeSource{
									ClaimName: "db",
								},
							},
						},
					},
					Containers: []corev1.Container{{
						Image:           image,
						Name:            ha.Name,
						ImagePullPolicy: corev1.PullIfNotPresent,
						Ports: []corev1.ContainerPort{
							{
								ContainerPort: 5432,
								Protocol:      corev1.ProtocolTCP,
							},
						},
						Env: []corev1.EnvVar{
							{
								Name: "POSTGRES_DB",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "db",
										},
										Key: "POSTGRES_DB",
									},
								},
							},
							{
								Name: "POSTGRES_USER",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "db",
										},
										Key: "POSTGRES_USER",
									},
								},
							},
							{
								Name: "POSTGRES_PASSWORD",
								ValueFrom: &corev1.EnvVarSource{
									SecretKeyRef: &corev1.SecretKeySelector{
										LocalObjectReference: corev1.LocalObjectReference{
											Name: "db",
										},
										Key: "POSTGRES_PASSWORD",
									},
								},
							},
							{
								Name:  "PGDATA",
								Value: "/var/lib/postgresql/data/pgdata",
							},
						},
						VolumeMounts: []corev1.VolumeMount{
							{
								Name:      "db",
								MountPath: "/var/lib/postgresql/data",
							},
						},
					}},
				},
			},
		},
	}
	ctrl.SetControllerReference(ha, dep, c.Scheme)
	return dep
}

func (r *HelloAppReconciler) createService(helloApp *apiv1.HelloApp) *corev1.Service {

	svc := &corev1.Service{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "db",
			Namespace: helloApp.Namespace,
			Labels: map[string]string{
				"app": "db",
			},
		},
		Spec: corev1.ServiceSpec{
			Selector: map[string]string{
				"app": "db",
			},
			Type: corev1.ServiceTypeClusterIP,
			Ports: []corev1.ServicePort{
				{
					Port:       5432,
					Protocol:   corev1.ProtocolTCP,
					TargetPort: intstr.FromInt(5432),
				},
			},
		},
	}
	return svc
}
