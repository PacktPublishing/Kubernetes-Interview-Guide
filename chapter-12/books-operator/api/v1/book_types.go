package v1

import (
    metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

// EDIT THIS FILE!  THIS IS SCAFFOLDING FOR YOU TO OWN!

// BookSpec defines the desired state of Book
type BookSpec struct {
    Book string `json:"book,omitempty"`
    Year int    `json:"year,omitempty"`
}

// BookStatus defines the observed state of Book
type BookStatus struct {
    // INSERT ADDITIONAL STATUS FIELD - define observed state of cluster
    // Important: Run "make" to regenerate code after modifying this file
}

//+kubebuilder:object:root=true
//+kubebuilder:subresource:status

// Book is the Schema for the books API
type Book struct {
    metav1.TypeMeta   `json:",inline"`
    metav1.ObjectMeta `json:"metadata,omitempty"`

    Spec   BookSpec   `json:"spec,omitempty"`
    Status BookStatus `json:"status,omitempty"`
}

//+kubebuilder:object:root=true

// BookList contains a list of Book
type BookList struct {
    metav1.TypeMeta `json:",inline"`
    metav1.ListMeta `json:"metadata,omitempty"`
    Items           []Book `json:"items"`
}

func init() {
    SchemeBuilder.Register(&Book{}, &BookList{})
}
