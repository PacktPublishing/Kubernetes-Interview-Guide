package controllers

import (
    "context"
    "fmt"

    "k8s.io/apimachinery/pkg/runtime"
    ctrl "sigs.k8s.io/controller-runtime"
    "sigs.k8s.io/controller-runtime/pkg/client"
    "sigs.k8s.io/controller-runtime/pkg/log"

    packtv1 "github.com/PacktPublishing/Kubernetes-Interview-Guide/chapter-11/books-operator/api/v1"
    corev1 "k8s.io/api/core/v1"
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type BookReconciler struct {
    client.Client
    Scheme *runtime.Scheme
}

// Reconcile is part of the main Kubernetes reconciliation loop
func (r *BookReconciler) Reconcile(ctx context.Context, req ctrl.Request) (ctrl.Result, error) {
    _ = log.FromContext(ctx)

    // Fetch the Book instance
    book := &packtv1.Book{}
    err := r.Get(ctx, req.NamespacedName, book)
    if err != nil {
        return ctrl.Result{}, client.IgnoreNotFound(err)
    }

    // Print the Book spec to stdout
    fmt.Printf("Reconciling Book: %s, Year: %d\n", book.Spec.Book, book.Spec.Year)

    // Define a new Pod object
    pod := &corev1.Pod{
        ObjectMeta: metav1.ObjectMeta{
            Name:      book.Name + "-pod",
            Namespace: "default", // Always create the pod in the default namespace
        },
        Spec: corev1.PodSpec{
            Containers: []corev1.Container{
                {
                    Name:  "busybox",
                    Image: "busybox",
                    Command: []string{
                        "sh",
                        "-c",
                        fmt.Sprintf("while true; do echo Book: %s, Year: %d; sleep 1; done", book.Spec.Book, book.Spec.Year),
                    },
                },
            },
        },
    }

    // Check if the Pod already exists
    found := &corev1.Pod{}
    err = r.Get(ctx, client.ObjectKey{Name: pod.Name, Namespace: pod.Namespace}, found)
    if err != nil && client.IgnoreNotFound(err) != nil {
        return ctrl.Result{}, err
    }

    if err == nil {
        // Pod already exists - don't requeue
        return ctrl.Result{}, nil
    }

    // Create the Pod
    fmt.Printf("Creating Pod %s/%s\n", pod.Namespace, pod.Name)
    err = r.Create(ctx, pod)
    if err != nil {
        return ctrl.Result{}, err
    }

    // Pod created successfully - don't requeue
    return ctrl.Result{}, nil
}

// SetupWithManager sets up the controller with the Manager.
func (r *BookReconciler) SetupWithManager(mgr ctrl.Manager) error {
    return ctrl.NewControllerManagedBy(mgr).
        For(&packtv1.Book{}).
        Complete(r)
}
